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
	"flag"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/miekg/dns"
	"k8s.io/klog/v2"
)

const (
	resolvConf            = "/etc/resolv.conf"
	defaultInternalDomain = "run.internal"
	defaultNdots          = 3
	defaultDnsPort        = "53"
	defaultHTTPProxyPort  = "80"
)

var (
	flInternalDomain string
	flNdots          int
	flResolvConf     string
	flNameserver     string
	flRegion         string
	flProjectHash    string
	flHTTPProxyPort  string
	flDNSPort        string

	flSkipDNSServer       bool
	flSkipHTTPProxyServer bool

	ipv4Loopback = net.IPv4(127, 0, 0, 1)

	ipv6OK bool
)

var (
	version string = "unknown" // populated by goreleaser
	commit  string = "unknown" // populated by goreleaser
)

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()
	flag.StringVar(&flResolvConf, "resolv_conf_file", resolvConf, "[debug-only] path to resolv.conf(5) file to read/write")
	flag.StringVar(&flInternalDomain, "domain", defaultInternalDomain, "internal zone (without a trailing dot)")
	flag.IntVar(&flNdots, "ndots", defaultNdots, "ndots setting for resolv conf (e.g. for -domain=a.b this should be 3)")
	flag.StringVar(&flNameserver, "nameserver", "", "override used nameserver (default: from -resolv_conf_file)")
	flag.StringVar(&flRegion, "gcp_region", "", "[debug-only] override GCP region (do not infer from metadata svc)")
	flag.BoolVar(&flSkipDNSServer, "skip_dns_hijack", false, "[debug-only] do not start a DNS server for service discovery")
	flag.BoolVar(&flSkipHTTPProxyServer, "skip_http_proxy", false, "[debug-only] do not start a HTTP proxy server")
	flag.StringVar(&flProjectHash, "gcp_project_hash", "", "gcp cloud run project hash (or use CLOUD_RUN_PROJECT_HASH")
	flag.StringVar(&flHTTPProxyPort, "http_proxy_port", defaultHTTPProxyPort, "[debug-only] reverse proxy port to listen on for loopback interface(s)")
	flag.StringVar(&flDNSPort, "dns_port", defaultDnsPort, "[debug-only] custom port to start dns server on loopback interface(s), note resolv.conf doesn't support custom ports")
	flag.Set("logtostderr", "true")
	flag.Parse()

	klog.V(1).Infof("starting runsd version=%s commit=%s", version, commit)

	new(sync.Once).Do(func() {
		ipv6OK = ipv6Available()
	})

	if os.Getenv("PORT") == "80" {
		klog.Exit("your Cloud Run application is set to run on PORT=80, this conflicts with runsd")
	}

	posArgs := flag.Args()
	if len(posArgs) == 0 {
		klog.Exit("specify subprocess as positional args, e.g: '/runsd -- python3 server.py'")
	}

	rc, err := dns.ClientConfigFromFile(flResolvConf)
	if err != nil {
		klog.Exitf("failed to read dns client configuration from %s: %v", flResolvConf, err)
	}

	var useNameserver string
	if flNameserver != "" {
		useNameserver = flNameserver
	} else if len(rc.Servers) > 0 {
		useNameserver = rc.Servers[0]
	} else {
		klog.Exitf("no nameservers in %s and no nameserver is specified as option", flResolvConf)
	}
	klog.V(3).Infof("using backend nameserver in process: %s", useNameserver)

	// do not hijack dns for this process
	net.DefaultResolver = resolver(net.JoinHostPort(useNameserver, "53"))

	onCloudRun := flRegion != "" || useNameserver == "169.254.169.254"
	klog.V(1).Infof("on cloudrun: %v", onCloudRun)
	projectHash := os.Getenv("CLOUD_RUN_PROJECT_HASH") // TODO find a way to infer this from runtime environment
	if flProjectHash != "" {
		projectHash = flProjectHash
	}
	if onCloudRun && projectHash == "" {
		klog.Exit("error: CLOUD_RUN_PROJECT_HASH environment variable is not set" +
			"(e.g. this value is 'dpyb4duzqq' if the URLs for your project are like 'foo-dpyb4duzqq-uc.run.app')")
	}

	var region string
	if !onCloudRun || flRegion != "" {
		region = flRegion
	} else {
		klog.V(4).Info("inferring cloud run region from metadata server")
		region, err = regionFromMetadata()
		if err != nil {
			klog.Exitf("failed to infer region from metadata service: %v", err)
		}
	}
	if onCloudRun {
		klog.V(3).Infof("using cloud run region: %s", region)
		_, ok := cloudRunRegionCodes[region]
		if !ok {
			klog.Exitf("cloud run region %q does not have a region code in this tool yet", region)
		}
	}

	if !onCloudRun || flSkipDNSServer {
		klog.V(1).Infof("skipping dns servers initialization")
	} else {
		// start dns server
		dnsSrv := &dnsHijack{
			nameserver: useNameserver,
			domain:     flInternalDomain,
			dots:       4,
			serveIPv6:  ipv6OK,
		}

		addrv4 := net.JoinHostPort(ipv4Loopback.String(), flDNSPort)
		addrv6 := net.JoinHostPort(net.IPv6loopback.String(), flDNSPort)
		go func() {
			klog.V(1).Infof("starting dns ipv4 server at %s", addrv4)
			if err := dnsSrv.newServer(addrv4).ListenAndServe(); err != nil {
				klog.Fatalf("dns server start failure: %v", err)
			}
		}()
		if !ipv6OK {
			klog.V(1).Infof("skipping ipv6 dns server, stack not available")
		} else {
			klog.V(1).Infof("starting dns ipv6 server at %s", addrv6)
			go func() {
				if err := dnsSrv.newServer(addrv6).ListenAndServe(); err != nil {
					klog.Fatalf("ipv6 dns server start failure: %v", err)
				}
			}()
		}

		klog.V(4).Infof("hijacking resolv.conf file=%s", flResolvConf)
		searchDomains := append(cloudRunZones(region, flInternalDomain), rc.Search...)
		if err := configureResolvConf(flResolvConf, []string{
			ipv4Loopback.String(),     // to resolve local domains
			net.IPv6loopback.String(), // to resolve local domains
			//useNameserver,             // TODO: probably not necessary to resolve external domains since we return NS records?
		}, searchDomains, flNdots); err != nil {
			klog.Fatal(err)
		}
		klog.V(1).Info("dns hijack setup complete")
	}

	// start local proxy
	if !onCloudRun || flSkipHTTPProxyServer {
		klog.V(1).Infof("skipping http proxy server initialization")
	} else {
		proxy := newReverseProxy(projectHash, region, flInternalDomain)
		handler := proxy.newHandler()
		go func() {
			addr := net.JoinHostPort(net.IPv4(127, 0, 0, 1).String(), flHTTPProxyPort)
			klog.Fatalf("reverse proxy (ipv4) fail: %v", http.ListenAndServe(addr, handler))
		}()
		go func() {
			if !ipv6OK {
				klog.V(1).Infof("skipping http proxy server on ipv6, stack not available")
				return
			}
			addr := net.JoinHostPort(net.IPv6loopback.String(), flHTTPProxyPort)
			klog.Fatalf("reverse proxy (ipv6) fail: %v", http.ListenAndServe(addr, handler))
		}()
		klog.V(1).Info("started reverse proxy server(s)")
	}

	// start subprocess
	var (
		cmd  string
		argv []string
	)
	if len(posArgs) > 1 {
		cmd, argv = posArgs[0], posArgs[1:]
	} else {
		cmd = posArgs[0]
	}
	klog.V(1).Infof("starting subprocess. cmd=%q argv=%#v", cmd, argv)
	if err := run(cmd, argv); err != nil {
		klog.V(1).Info("subprocess terminated")
		if v, ok := err.(*exec.ExitError); ok {
			ec := v.ExitCode()
			klog.V(1).Infof("exit_code=%d, pid=%d", ec, v.Pid())
			os.Exit(ec)
		} else {
			klog.V(1).Infof("error not a proper exec.ExitError")
			klog.Exitf("subprocess exited: %v", err)
		}
	}
	klog.V(1).Infof("subprocess exited successfully")
}

func run(command string, argv []string) error {
	cmd := exec.Command(command, argv...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func ipv6Available() bool {
	lis, err := net.Listen("tcp6", net.JoinHostPort(net.IPv6loopback.String(), "0"))
	if err != nil {
		klog.V(4).Infof("ipv6 stack not available: %v", err)
		return false
	}
	lis.Close()
	return true
}

func isOnCloudRun() {

}
