package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/run/v1"
)

func getProjectHash(region string) (string, error) {
	project, err := metadata.ProjectID()
	if err != nil {
		return "", err
	}

	runAdminURL := fmt.Sprintf(
		"https://us-%s-run.googleapis.com/apis/serving.knative.dev/v1/namespaces/%s/services/%s",
		region, project, os.Getenv("K_SERVICE"))
	req, err := http.NewRequest(http.MethodGet, runAdminURL, nil)
	if err != nil {
		return "", err
	}
	httpClient, err := google.DefaultClient(context.Background(), run.CloudPlatformScope)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("admin server responded with code=%d %s", resp.StatusCode, resp.Status)
	}
	var svc run.Service
	err = json.NewDecoder(resp.Body).Decode(&svc)
	if err != nil {
		return "", err
	}
	return hashFromURL(svc.Status.Url), nil
}

func hashFromURL(url string) string {
	s := strings.TrimSuffix(url, ".a.run.app")
	tkns := strings.Split(s, "-")
	hash := tkns[len(tkns)-2]
	return hash
}
