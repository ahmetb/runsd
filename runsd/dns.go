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
	"net"
	"strings"

	"github.com/miekg/dns"
	"k8s.io/klog/v2"
)

type dnsHijack struct {
	domain     string
	nameserver string
	dots       int
	serveIPv6  bool
}

func (d *dnsHijack) handler() dns.Handler {
	mux := dns.NewServeMux()
	mux.HandleFunc(d.domain, d.handleLocal)
	mux.HandleFunc(".", d.recurse)
	return mux
}

func loggingHandler(d dns.HandlerFunc) dns.HandlerFunc {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		for i, q := range r.Question {
			klog.V(5).Infof("[dns] > Q%d: type=%v name=%v", i, dns.TypeToString[q.Qtype], q.Name)
		}
		d(w, r)
	}
}

func (d *dnsHijack) newServer(net, addr string) *dns.Server {
	return &dns.Server{
		Addr:    addr,
		Net:     net,
		Handler: loggingHandler(d.handler().ServeDNS),
	}
}

func (d *dnsHijack) handleLocal(w dns.ResponseWriter, msg *dns.Msg) {
	for _, q := range msg.Question {
		dots := strings.Count(q.Name, ".")
		if q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA {
			klog.V(4).Infof("[dns] < unsupported dns msg type: %s, defer", dns.TypeToString[q.Qtype])
			d.recurse(w, msg) // TODO probably should not do this since original resolver won’t know about local domains
			return
		}

		if dots != d.dots {
			klog.V(4).Infof("[dns] < type=%v name=%v is too short or long (need ndots=%d; got=%d), nxdomain", dns.TypeToString[q.Qtype], q.Name, d.dots, dots)
			nxdomain(w, msg)
			return
		}

		parts := strings.SplitN(strings.TrimSuffix(q.Name, "."+d.domain), ".", 2)
		if len(parts) < 2 {
			klog.V(4).Infof("[dns] < name=%q not enough segments to parse", q.Name)
			return
		}
		region := parts[1]
		_, ok := cloudRunRegionCodes[region]
		if !ok {
			klog.V(4).Infof("[dns] < unknown region=%q from name=%q, nxdomain", region, q.Name)
			nxdomain(w, msg)
			return
		}
	}

	r := new(dns.Msg)
	r.SetReply(msg)
	r.Authoritative = true
	for _, q := range msg.Question {
		klog.V(5).Infof("[dns] < MATCH type=%v name=%v", dns.TypeToString[q.Qtype], q.Name)
		switch q.Qtype {
		case dns.TypeA:
			r.Answer = append(r.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    10, // TODO think about this
				},
				A: ipv4Loopback,
			})
		case dns.TypeAAAA:
			if d.serveIPv6 {
				r.Answer = append(r.Answer, &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    10, // TODO think about this
					},
					AAAA: net.IPv6loopback,
				})
			}
		}
	}
	w.WriteMsg(r)
}

// recurse proxies the message to the backend nameserver.
func (d *dnsHijack) recurse(w dns.ResponseWriter, msg *dns.Msg) {
	klog.V(5).Infof("[dns] >> recursing type=%s name=%v", dns.TypeToString[msg.Question[0].Qtype], msg.Question[0].Name)
	r, rtt, err := new(dns.Client).Exchange(msg, net.JoinHostPort(d.nameserver, "53"))
	if err != nil {
		klog.V(4).Infof("[dns] << WARNING: recursive dns fail: %v, servfail", err)
		servfail(w, msg)
		return
	}
	klog.V(5).Infof("[dns] << recursed  type=%s name=%v rcode=%s answers=%d rtt=%v",
		dns.TypeToString[msg.Question[0].Qtype],
		msg.Question[0].Name,
		dns.RcodeToString[r.Rcode], len(r.Answer), rtt)

	// r.SetReply(msg) // TODO(ahmetb): not sure why but removing this actually preserves the response hdrs and other sections well
	w.WriteMsg(r)
}

// nxdomain sends an authoritative NXDOMAIN (domain not found) reply
func nxdomain(w dns.ResponseWriter, msg *dns.Msg) {
	r := new(dns.Msg)
	r.SetReply(msg)
	r.Authoritative = true
	r.Rcode = dns.RcodeNameError
	w.WriteMsg(r)
	return
}

//  servfail an authoritative SERVFAIL (error) reply
func servfail(w dns.ResponseWriter, msg *dns.Msg) {
	r := new(dns.Msg)
	r.SetReply(msg)
	r.Rcode = dns.RcodeServerFailure
	w.WriteMsg(r)
	return
}
