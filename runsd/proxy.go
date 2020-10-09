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
	"strings"
	"time"

	"github.com/elazarl/goproxy"
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

func (rp *reverseProxy) newHandler() http.Handler {
	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		ctx.UserData = time.Now()
		klog.V(5).Infof("[proxy] start: method=%s url=%s headers=%d trailers=%d", req.Method, req.URL, len(req.Header), len(req.Trailer))
		for k, v := range req.Header {
			klog.V(5).Infof("[proxy]       > hdr=%s v=%#v", k, v)
		}
		runHost, err := resolveCloudRunHost(rp.internalDomain, req.Host, rp.currentRegion, rp.projectHash)
		if err != nil {
			// this only fails due to region code not being registered –which would be handled
			// by the DNS resolver so the request should not come here with an invalid region.
			klog.Warningf("WARN: reverse proxy failed to find a Cloud Run URL for host=%s: %v", req.Host, err)
			return nil, goproxy.NewResponse(req, "text/plain", http.StatusInternalServerError,
				fmt.Sprintf("runsd doesn't know how to handle host=%q: %v", req.Host, err))
		}
		origHost := req.Host
		req.URL.Scheme = "https"
		req.URL.Host = runHost
		req.Host = runHost
		req.Header.Set("host", runHost)
		klog.V(5).Infof("[proxy] rewrote host=%s to=%s newurl=%q", origHost, runHost, req.URL)

		idToken, err := identityToken("https://" + req.Host)
		if err != nil {
			klog.V(1).Infof("WARN: failed to get ID token for host=%s: %v", req.Host, err)
			resp := new(http.Response)
			resp.Body = ioutil.NopCloser(strings.NewReader(fmt.Sprintf("failed to fetch metadata token: %v", err)))
			resp.StatusCode = http.StatusInternalServerError
			return nil, resp
		}
		if req.Header.Get("authorization") == "" {
			req.Header.Set("authorization", "Bearer "+idToken)
		}
		ua := req.Header.Get("user-agent")
		req.Header.Set("user-agent", fmt.Sprintf("runsd version=%s", version))
		if ua != "" {
			req.Header.Set("user-agent", req.Header.Get("user-agent")+"; "+ua)
		}
		return req, nil
	})
	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		start := ctx.UserData
		var took time.Duration
		if v, ok := start.(time.Time); ok {
			took = time.Since(v)
		}
		var rcode, hdrs, trailers int
		if resp != nil {
			rcode = resp.StatusCode
			hdrs = len(resp.Header)
			trailers = len(resp.Trailer)
			for k, v := range resp.Header {
				klog.V(7).Infof("[proxy]       < hdr=%s v=%#v", k, v)
			}
			for k, v := range resp.Trailer {
				klog.V(7).Infof("[proxy]       < trailer=%s v=%#v", k, v)
			}
		}
		klog.V(5).Infof("[proxy]   end: method=%s url=%s resp_status=%d headers=%d trailers=%d took=%v",
			ctx.Req.Method, ctx.Req.URL, rcode, hdrs, trailers, took)
		return resp
	})
	return proxy
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

func absolutify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// make the request URL absolute by adding a schema
		// otherwise goproxy complains it "does not respond to non-proxy requests".
		r.URL.Scheme = "http"
		next.ServeHTTP(w, r)
	})
}

func allowh2c(next http.Handler) http.Handler {
	h2server := &http2.Server{IdleTimeout: time.Second * 60}
	return h2c.NewHandler(next, h2server)
}
