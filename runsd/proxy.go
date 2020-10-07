// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

type reverseProxy struct {
	projectHash    string
	currentRegion  string
	internalDomain string
}

func newReverseProxy(projectHash, currentRegion, internalDomain string) *reverseProxy {
	return &reverseProxy{
		projectHash:    projectHash,
		currentRegion:  currentRegion,
		internalDomain: internalDomain,
	}
}

func (rp *reverseProxy) newHandler() http.Handler {
	director := func(req *http.Request) {
		runHost, err := resolveCloudRunHost(rp.internalDomain, req.Host, rp.currentRegion, rp.projectHash)
		if err != nil {
			// this only fails due to region code not being registered –which would be handled
			// by the DNS resolver so the request should not come here with an invalid region.
			klog.Warningf("WARN: reverse proxy director failed to find cloud run URL for host=%s: %v", req.Host, err)
			return // do not rewrite request
		}
		origHost := req.Host
		req.URL.Scheme = "https"
		req.URL.Host = runHost
		req.Host = runHost
		req.Header.Set("host", runHost)
		klog.V(5).Infof("[proxy] rewrite host=%s to=%q", origHost, req.URL)
	}

	tokenInject := authenticatingTransport{next: http.DefaultTransport}
	transport := loggingTransport{next: tokenInject}
	v := &httputil.ReverseProxy{
		Director:  director,
		Transport: transport,
	}
	return v
}

func resolveCloudRunHost(internalDomain, hostname, curRegion, projectHash string) (string, error) {
	hostname = strings.ToLower(hostname) // TODO surprisingly not canonicalized by now

	if !strings.Contains(hostname, ".") {
		// in the same region
		rc, ok := cloudRunRegionCodes[curRegion]
		if !ok {
			return "", fmt.Errorf("region %q is not handled", curRegion)
		}
		return mkCloudRunHost(hostname, rc, projectHash), nil
	}

	trimmed := strings.TrimSuffix(hostname, "."+strings.Trim(internalDomain, "."))
	if strings.Count(trimmed, ".") != 1 {
		return "", fmt.Errorf("found too many dots in hostname %q, (trimmed: %s)", hostname, trimmed)
	}

	splits := strings.SplitN(trimmed, ".", 2)
	svc, svcRegion := splits[0], splits[1]

	rc, ok := cloudRunRegionCodes[svcRegion]
	if !ok {
		return "", fmt.Errorf("region %q is not handled (inferred from hostname %s)", svcRegion, hostname)
	}
	return mkCloudRunHost(svc, rc, projectHash), nil
}

func mkCloudRunHost(svc, regionCode, projectHash string) string {
	return fmt.Sprintf("%s-%s-%s.a.run.app", svc, projectHash, regionCode)
}

type authenticatingTransport struct {
	next http.RoundTripper
}

func (a authenticatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// TODO convert to roundtripper, maybe?
	idToken, err := identityToken("https://" + req.Host)
	if err != nil {
		klog.V(1).Infof("WARN: failed to get ID token for host=%s: %v", req.Host, err)
		r := new(http.Response)
		r.Body = ioutil.NopCloser(strings.NewReader(fmt.Sprintf("failed to fetch metadata token: %v", err)))
		r.StatusCode = http.StatusInternalServerError
		return r, nil
	}
	if req.Header.Get("authorization") == "" {
		req.Header.Set("authorization", "Bearer "+idToken)
	}
	ua := req.Header.Get("user-agent")
	req.Header.Set("user-agent", fmt.Sprintf("runsd version=%s", version))
	if ua != "" {
		req.Header.Set("user-agent", req.Header.Get("user-agent")+"; "+ua)
	}
	return a.next.RoundTrip(req)
}

type loggingTransport struct {
	next http.RoundTripper
}

func (l loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	klog.V(5).Infof("[proxy] start: %s url=%s", req.Method, req.URL)
	defer func() {
		klog.V(5).Infof("[proxy]   end: %s url=%s took=%s",
			req.Method, req.URL, time.Since(start).Truncate(time.Millisecond))
	}()
	return l.next.RoundTrip(req)
}
