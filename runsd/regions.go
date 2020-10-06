package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

var (
	cloudRunRegionCodes = map[string]string{
		"asia-east1":      "de",
		"asia-northeast1": "an",
		"europe-north1":   "lz",
		"europe-west1":    "ew",
		"europe-west4":    "ez",
		"us-central1":     "uc",
		"us-east1":        "ue",
		"us-east4":        "uk",
		"us-west1":        "uw",
	}
)

func regionFromMetadata() (string, error) {
	v, err := queryMetadata("http://metadata.google.internal/computeMetadata/v1/instance/zone")
	if err != nil {
		return "", err // TODO wrap
	}
	vs := strings.SplitAfter(v, "/zones/")
	if len(vs) != 2 {
		return "", fmt.Errorf("malformed zone value split into %#v", vs)
	}
	return strings.TrimSuffix(vs[1], "-1"), nil
}

func queryMetadata(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err // TODO wrap
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err // TODO wrap
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata server responeded with code=%d %s", resp.StatusCode, resp.Status)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err // TODO wrap
	}
	return strings.TrimSpace(string(b)), err
}
