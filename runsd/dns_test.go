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
	"context"
	"fmt"
	"net"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var loopbackIPs = []string{ipv4Loopback.String(), net.IPv6loopback.String()}

func TestDNSInternalLookups(t *testing.T) {
	dnsSrv, shutdown := newTestDNSServer(t, &dnsHijack{
		nameserver: "192.0.2.255", // invalid ip (https://tools.ietf.org/html/rfc5737) as we don't want accidental recursion
		domain:     "foo.bar.",
		dots:       4,
		serveIPv6:  true,
	})
	defer shutdown()
	r := resolver(dnsSrv)

	cases := []struct {
		addr    string
		wantErr bool
		wantRR  []string
	}{
		{addr: "localhost", wantRR: loopbackIPs},
		{addr: "a.foo.bar.", wantErr: true},       // not enough dots
		{addr: "a.b.c.foo.bar.", wantErr: true},   // too many dots
		{addr: "abc.def.foo.bar.", wantErr: true}, // invalid region name 'def'
		{addr: "abc.us-central1.foo.bar.", wantRR: loopbackIPs},
		{addr: "foo.asia-northeast1.foo.bar.", wantRR: loopbackIPs},
	}

	for _, tt := range cases {
		t.Run(tt.addr, func(t *testing.T) {
			got, err := r.LookupHost(context.TODO(), tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("LookupHost(%s) error = %v, wantErr = %v", tt.addr, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			// ignore order of records before comparing
			sort.Strings(tt.wantRR)
			sort.Strings(got)

			if diff := cmp.Diff(got, tt.wantRR); diff != "" {
				t.Errorf("got a wrong RR set: %s", diff)
			}
		})
	}
}

func TestDNSInternalIPv4Only(t *testing.T) {
	ds := &dnsHijack{
		nameserver: "192.0.2.255", // invalid ip (https://tools.ietf.org/html/rfc5737) as we don't want accidental recursion
		domain:     "foo.bar.",
		dots:       4,
		serveIPv6:  false}

	dnsSrv, shutdown := newTestDNSServer(t, ds)
	defer shutdown()
	r := resolver(dnsSrv)

	v, err := r.LookupHost(context.TODO(), "abc.us-central1.foo.bar.")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{ipv4Loopback.String()}
	if diff := cmp.Diff(expected, v); diff != "" {
		t.Fatal(diff)
	}
}

func TestDNSExternalRecursion(t *testing.T) {
	dnsSrv, shutdown := newTestDNSServer(t, &dnsHijack{nameserver: "8.8.8.8",
		domain: "foo.bar.",
		dots:   4})
	defer shutdown()
	r := resolver(dnsSrv)

	// resolve an external domain
	v, err := r.LookupHost(context.TODO(), "localtest.me") // via https://weblogs.asp.net/owscott/introducing-testing-domain-localtest-me
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{ipv4Loopback.String()}, v); diff != "" {
		t.Fatalf("got different RRs: %s", diff)
	}

	// resolve a NS
	ns, err := r.LookupNS(context.TODO(), "example.com")
	if err != nil {
		t.Fatalf("NS query fail: %v", err)
	}
	if len(ns) == 0 {
		t.Fatalf("no RRs returned for NS query")
	}

	// resolve a MX
	mx, err := r.LookupMX(context.TODO(), "example.com")
	if err != nil {
		t.Fatalf("MX query fail: %v", err)
	}
	if len(mx) == 0 {
		t.Fatalf("no RRs returned for MX query")
	}
}

// newTestDNSServer starts a new DNS server with the provided
func newTestDNSServer(t *testing.T, d *dnsHijack) (string, func()) {
	t.Helper()

	srv := d.newServer("udp", "127.0.0.1:9053")
	ch := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(ch) }
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			panic(fmt.Sprintf("failed to start test dns server: %v", err))
		}
	}()
	<-ch
	return srv.PacketConn.LocalAddr().String(), func() { srv.Shutdown() }
}
