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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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

const (
	ctxKeyEarlyResponse = `early-response`
)

func (rp *reverseProxy) newReverseProxyHandler(tr http.RoundTripper) http.Handler {
	tokenInject := authenticatingTransport{next: tr}
	transport := loggingTransport{next: tokenInject}

	return &httputil.ReverseProxy{
		Transport: transport,
		FlushInterval: -1, // to support grpc streaming responses
		Director: func(req *http.Request) {
			klog.V(5).Infof("[proxy] start: method=%s url=%s headers=%d trailers=%d", req.Method, req.URL, len(req.Header), len(req.Trailer))
			runHost, err := resolveCloudRunHost(rp.internalDomain, req.Host, rp.currentRegion, rp.projectHash)
			if err != nil {
				// this only fails due to region code not being registered –which would be handled
				// by the DNS resolver so the request should not come here with an invalid region.
				klog.Warningf("WARN: reverse proxy failed to find a Cloud Run URL for host=%s: %v", req.Host, err)
				resp := &http.Response{
					Request:    req,
					StatusCode: http.StatusInternalServerError,
					Body: ioutil.NopCloser(bytes.NewReader([]byte(
						fmt.Sprintf("runsd doesn't know how to handle host=%q: %v", req.Host, err)))),
				}
				newReq := req.WithContext(context.WithValue(req.Context(), ctxKeyEarlyResponse, resp))
				*req = *newReq
				return
			}
			origHost := req.Host
			req.URL.Scheme = "https"
			req.URL.Host = runHost
			req.Host = runHost
			req.Header.Set("host", runHost)
			klog.V(5).Infof("[proxy] rewrote host=%s to=%s newurl=%q", origHost, runHost, req.URL)
		},
	}
}

type authenticatingTransport struct {
	next http.RoundTripper
}

func (a authenticatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if v, ok := req.Context().Value(ctxKeyEarlyResponse).(*http.Response); ok {
		return v, nil
	}

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
	for k, v := range req.Header {
		klog.V(6).Infof("[proxy]       > hdr=%s v=%#v", k, v)
	}
	defer func() {
		klog.V(5).Infof("[proxy]   end: %s url=%s took=%s",
			req.Method, req.URL, time.Since(start).Truncate(time.Millisecond))
	}()

	resp, err := l.next.RoundTrip(req)
	if err != nil {
		for k, v := range req.Header {
			klog.V(6).Infof("[proxy]       < hdr=%s v=%#v", k, v)
		}
	}
	return resp, err
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
		return "", fmt.Errorf("region %q is not handled (inferred from hostname %s), try upgrading runsd", svcRegion, hostname)
	}
	return mkCloudRunHost(svc, rc, projectHash), nil
}

func mkCloudRunHost(svc, regionCode, projectHash string) string {
	return fmt.Sprintf("%s-%s-%s.a.run.app", svc, projectHash, regionCode)
}

func allowh2c(next http.Handler) http.Handler {
	h2server := &http2.Server{IdleTimeout: time.Second * 60}
	return h2c.NewHandler(next, h2server)
}
