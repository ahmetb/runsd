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
