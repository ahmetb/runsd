package main

import (
	"os"
	"strings"
)

func identityToken(audience string) (string, error) {
	if v := os.Getenv("CLOUD_RUN_ID_TOKEN"); v != "" {
		return strings.TrimSpace(v), nil
	}
	return identityTokenFromMetadata(audience)
}

func identityTokenFromMetadata(audience string) (string, error) {
	return queryMetadata("http://metadata/computeMetadata/v1/instance/service-accounts/default/identity?audience=" + audience)
}
