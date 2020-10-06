package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
)

func configureResolvConf(path string, nameservers []string, searchDomains []string, ndots int) error {
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY|os.O_SYNC, 0)
	if err != nil {
		return err // TODO wrap
	}
	for _, n := range nameservers {
		if _, err := fmt.Fprintf(f, "nameserver %s\n", n); err != nil {
			return err // TODO wrap
		}
	}
	if _, err := fmt.Fprintf(f, "search %s\n", strings.Join(searchDomains, " ")); err != nil {
		return err // TODO wrap
	}
	if _, err := fmt.Fprintf(f, "options ndots:%d\n", ndots); err != nil {
		return err // TODO wrap
	}
	return f.Close()
}

func cloudRunZones(region, domain string) []string {
	return []string{
		fmt.Sprintf("%s.%s", region, domain),
		domain,
	}
}

func resolver(nameserver string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "udp", nameserver)
		},
	}
}
