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

func (d *dnsHijack) newServer(addr string) *dns.Server {
	return &dns.Server{
		Addr:    addr,
		Net:     "udp",
		Handler: d.handler(),
	}
}

func (d *dnsHijack) handleLocal(w dns.ResponseWriter, msg *dns.Msg) {
	for _, q := range msg.Question {
		dots := strings.Count(q.Name, ".")
		klog.V(5).Infof("[dns] type=%v name=%v dots=%d", dns.TypeToString[q.Qtype], q.Name, dots)
		if q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA {
			klog.V(4).Infof("unsupported dns msg type: %s, defer", dns.TypeToString[q.Qtype])
			d.recurse(w, msg)
			return
		}

		if dots != d.dots {
			klog.V(4).Infof("[dns] type=%v name=%v is too short or long (need ndots=%d; got=%d), nxdomain", dns.TypeToString[q.Qtype], q.Name, d.dots, dots)
			nxdomain(w, msg)
			return
		}

		parts := strings.SplitN(strings.TrimSuffix(q.Name, "."+d.domain), ".", 2)
		region := parts[1]
		_, ok := cloudRunRegionCodes[region]
		if !ok {
			klog.V(4).Infof("[dns] unknown region=%q from name=%q, nxdomain", region, q.Name)
			nxdomain(w, msg)
			return
		}
	}

	r := new(dns.Msg)
	r.SetReply(msg)
	r.Authoritative = true
	for _, q := range msg.Question {
		klog.V(5).Infof("[dns] MATCH type=%v name=%v", dns.TypeToString[q.Qtype], q.Name)
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
	klog.V(5).Infof("[dns] recursing type=%s name=%v", dns.TypeToString[msg.Question[0].Qtype], msg.Question[0].Name)
	r, err := dns.Exchange(msg, net.JoinHostPort(d.nameserver, "53"))
	if err != nil {
		klog.V(4).Infof("WARNING: recursive dns fail: %v, servfail", err)
		servfail(w, msg)
		return
	}
	r.SetReply(msg)
	r.RecursionAvailable = true
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
