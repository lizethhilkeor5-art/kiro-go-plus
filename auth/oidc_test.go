package auth

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestRefreshOIDCTokenFallsBackToUSEast1(t *testing.T) {
	var requestedRegions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		region := r.URL.Path[1:]
		requestedRegions = append(requestedRegions, region)
		w.Header().Set("Content-Type", "application/json")
		if region == "eu-central-1" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_request","error_description":"Invalid token provided"}`)
			return
		}
		fmt.Fprint(w, `{"accessToken":"access","refreshToken":"refresh-rotated","expiresIn":3600,"profileArn":"arn:aws:codewhisperer:eu-central-1:123:profile/ABC"}`)
	}))
	defer server.Close()

	oldOIDC := oidcTokenURL
	oidcTokenURL = func(region string) string { return server.URL + "/" + region }
	defer func() { oidcTokenURL = oldOIDC }()

	accessToken, refreshToken, _, profileArn, err := refreshOIDCToken(
		"refresh", "client", "secret", "eu-central-1", server.Client(),
	)
	if err != nil {
		t.Fatalf("refreshOIDCToken: %v", err)
	}
	if accessToken != "access" || refreshToken != "refresh-rotated" {
		t.Fatalf("unexpected tokens returned")
	}
	if profileArn != "arn:aws:codewhisperer:eu-central-1:123:profile/ABC" {
		t.Fatalf("unexpected profile ARN: %q", profileArn)
	}
	if want := []string{"eu-central-1", "us-east-1"}; !reflect.DeepEqual(requestedRegions, want) {
		t.Fatalf("requested regions = %v, want %v", requestedRegions, want)
	}
}

func TestRefreshExternalIDPTokenUsesFormEncodedRefresh(t *testing.T) {
	var gotBody string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"access-external","refresh_token":"refresh-rotated","expires_in":3600}`)
	}))
	defer server.Close()

	accessToken, refreshToken, expiresAt, profileArn, err := refreshExternalIDPToken(
		"refresh-original",
		"client-id",
		server.URL,
		"openid offline_access profile",
		server.Client(),
	)
	if err != nil {
		t.Fatalf("refreshExternalIDPToken: %v", err)
	}
	if accessToken != "access-external" || refreshToken != "refresh-rotated" {
		t.Fatalf("unexpected tokens: %q %q", accessToken, refreshToken)
	}
	if expiresAt <= 0 {
		t.Fatalf("expected expiresAt to be set")
	}
	if profileArn != "" {
		t.Fatalf("external IdP refresh should not fabricate profileArn, got %q", profileArn)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	for _, want := range []string{
		"client_id=client-id",
		"grant_type=refresh_token",
		"refresh_token=refresh-original",
		"scope=openid+offline_access+profile",
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("request body %q missing %q", gotBody, want)
		}
	}
}
