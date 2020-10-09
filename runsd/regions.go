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
)

var (
	// TODO needs updating
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
