package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

type authenticatingTransport struct {
	next http.RoundTripper
}

var _ http.Flusher = authenticatingTransport{} // ensure it's a Flusher

func (a authenticatingTransport) Flush() {
	if v, ok := a.next.(http.Flusher); ok {
		v.Flush()
	}
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

var _ http.Flusher = loggingTransport{} // ensure it's a Flusher

func (l loggingTransport) Flush() {
	if v, ok := l.next.(http.Flusher); ok {
		v.Flush()
	}
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
