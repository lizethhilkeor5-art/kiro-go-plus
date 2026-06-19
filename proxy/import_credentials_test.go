package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go-plus/auth"
	"kiro-go-plus/config"
	accountpool "kiro-go-plus/pool"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testJWTWithExpiry(expiresAt int64) string {
	payload, _ := json.Marshal(map[string]interface{}{"exp": expiresAt})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

// installCleanAuthClient replaces the global auth HTTP client with one whose
// Transport does not consult http.ProxyFromEnvironment — that function caches
// env vars on first call and would otherwise poison TestBuildKiroTransport*
// when tests run in the default order. Returns a cleanup that restores the
// previous client.
func installCleanAuthClient(t *testing.T) func() {
	t.Helper()
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}}
	prev := auth.SetGlobalAuthClientForTest(c)
	return func() { auth.SetGlobalAuthClientForTest(prev) }
}

// TestApiImportCredentialsRejectsWhenRefreshFails verifies the regression:
// previously, when auth.RefreshToken failed and the user supplied an accessToken,
// the handler stored that accessToken with ExpiresAt = now+300, producing an
// account that the pool would skip (Pick uses now > ExpiresAt-120 → ~3 min) and
// that the on-demand refresh path could never repair (Pick filters it out before
// ensureValidToken runs). The fix is to reject the import outright; the caller
// must provide a refreshToken that actually works.
func TestApiImportCredentialsRejectsWhenRefreshFails(t *testing.T) {
	cfgFile := t.TempDir() + "/config.json"
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	defer installCleanAuthClient(t)()

	// Stand up a fake OIDC endpoint that always 400s, simulating an unreachable
	// or invalid refresh.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer fake.Close()

	oldOIDC := authOidcURL()
	auth.SetOIDCTokenURLForTest(func(string) string { return fake.URL })
	defer auth.SetOIDCTokenURLForTest(oldOIDC)

	h := &Handler{pool: accountpool.GetPool()}

	body := `{"refreshToken":"rt-broken","accessToken":"at-still-valid-elsewhere","clientId":"c","clientSecret":"s","authMethod":"idc","region":"us-east-1"}`
	req := httptest.NewRequest("POST", "/auth/credentials", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.apiImportCredentials(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when refresh fails, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp["error"], "Token refresh failed") {
		t.Fatalf("expected refresh-failed error, got %q", resp["error"])
	}

	// Crucial: no account should have been created. The previous bug stored a
	// half-broken account with ExpiresAt ~now+300 that would die in 3 minutes.
	if accs := config.GetAccounts(); len(accs) != 0 {
		t.Fatalf("expected no accounts to be persisted on failed import, got %+v", accs)
	}
}

// TestApiImportCredentialsUsesUpstreamExpiresAt verifies the happy path: when
// refresh succeeds, the persisted ExpiresAt reflects the upstream expiresIn,
// not a hard-coded 300s.
func TestApiImportCredentialsUsesUpstreamExpiresAt(t *testing.T) {
	cfgFile := t.TempDir() + "/config.json"
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	defer installCleanAuthClient(t)()

	const upstreamExpiresIn = 3600
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"accessToken":"at-new","refreshToken":"rt-rotated","expiresIn":%d,"profileArn":"arn:aws:codewhisperer:profile/test"}`, upstreamExpiresIn)
	}))
	defer fake.Close()

	oldOIDC := authOidcURL()
	auth.SetOIDCTokenURLForTest(func(string) string { return fake.URL })
	defer auth.SetOIDCTokenURLForTest(oldOIDC)

	h := &Handler{pool: accountpool.GetPool()}

	before := time.Now().Unix()
	body := `{"refreshToken":"rt-good","clientId":"c","clientSecret":"s","authMethod":"idc","region":"us-east-1"}`
	req := httptest.NewRequest("POST", "/auth/credentials", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.apiImportCredentials(rec, req)
	after := time.Now().Unix()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful refresh, got %d body=%s", rec.Code, rec.Body.String())
	}

	accs := config.GetAccounts()
	if len(accs) != 1 {
		t.Fatalf("expected exactly one account persisted, got %d", len(accs))
	}
	got := accs[0]
	if got.AccessToken != "at-new" {
		t.Fatalf("expected upstream-issued accessToken, got %q", got.AccessToken)
	}
	if got.RefreshToken != "rt-rotated" {
		t.Fatalf("expected rotated refreshToken to be persisted, got %q", got.RefreshToken)
	}
	// Allow ±5s of drift but require the value to clearly come from upstream's
	// expiresIn rather than the old 300s fallback.
	expectMin := before + upstreamExpiresIn - 5
	expectMax := after + upstreamExpiresIn + 5
	if got.ExpiresAt < expectMin || got.ExpiresAt > expectMax {
		t.Fatalf("expected ExpiresAt ≈ now+%d ([%d..%d]), got %d", upstreamExpiresIn, expectMin, expectMax, got.ExpiresAt)
	}
	if got.ExpiresAt-time.Now().Unix() < 1500 {
		t.Fatalf("ExpiresAt too short — looks like the 300s fallback is still in play: %d (delta %d)", got.ExpiresAt, got.ExpiresAt-time.Now().Unix())
	}
}

func TestApiImportCredentialsPreservesProvidedProfileArn(t *testing.T) {
	cfgFile := t.TempDir() + "/config.json"
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	defer installCleanAuthClient(t)()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"accessToken":"at-new","refreshToken":"rt-rotated","expiresIn":3600}`)
	}))
	defer fake.Close()

	oldOIDC := authOidcURL()
	auth.SetOIDCTokenURLForTest(func(string) string { return fake.URL })
	defer auth.SetOIDCTokenURLForTest(oldOIDC)

	h := &Handler{pool: accountpool.GetPool()}
	body := `{"refreshToken":"rt-good","clientId":"c","clientSecret":"s","authMethod":"idc","region":"eu-central-1","profileArn":"arn:aws:codewhisperer:eu-central-1:123:profile/ABC"}`
	req := httptest.NewRequest("POST", "/auth/credentials", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.apiImportCredentials(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful import, got %d body=%s", rec.Code, rec.Body.String())
	}
	accs := config.GetAccounts()
	if len(accs) != 1 {
		t.Fatalf("expected exactly one account persisted, got %d", len(accs))
	}
	if got := accs[0].ProfileArn; got != "arn:aws:codewhisperer:eu-central-1:123:profile/ABC" {
		t.Fatalf("profileArn = %q", got)
	}
}

func TestApiImportCredentialsAcceptsExternalIDPFullExport(t *testing.T) {
	cfgFile := t.TempDir() + "/config.json"
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	defer installCleanAuthClient(t)()

	var gotForm string
	var gotContentType string
	fakeTokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotForm = string(body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"at-external","refresh_token":"rt-external-rotated","expires_in":3600}`)
	}))
	defer fakeTokenEndpoint.Close()

	h := &Handler{pool: accountpool.GetPool()}
	body := fmt.Sprintf(`[{
		"type":"kiro",
		"email":"external@example.test",
		"accessToken":"old-at",
		"refreshToken":"rt-external",
		"clientId":"client-external",
		"clientSecret":"",
		"region":"us-east-1",
		"provider":"ExternalIdp",
		"authMethod":"external_idp",
		"profileArn":"arn:aws:codewhisperer:us-east-1:123:profile/ABC",
		"tokenEndpoint":%q,
		"issuerUrl":"https://idp.example.test/tenant/v2.0",
		"scopes":"openid offline_access profile"
	}]`, fakeTokenEndpoint.URL)
	req := httptest.NewRequest("POST", "/auth/credentials", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.apiImportCredentials(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful external_idp import, got %d body=%s", rec.Code, rec.Body.String())
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	for _, want := range []string{
		"client_id=client-external",
		"grant_type=refresh_token",
		"refresh_token=rt-external",
		"scope=openid+offline_access+profile",
	} {
		if !strings.Contains(gotForm, want) {
			t.Fatalf("external IdP refresh body %q missing %q", gotForm, want)
		}
	}

	accs := config.GetAccounts()
	if len(accs) != 1 {
		t.Fatalf("expected exactly one account persisted, got %d", len(accs))
	}
	got := accs[0]
	if got.AccessToken != "at-external" || got.RefreshToken != "rt-external-rotated" {
		t.Fatalf("tokens not persisted from external IdP refresh: %+v", got)
	}
	if got.AuthMethod != "external_idp" || got.Provider != "ExternalIdp" {
		t.Fatalf("external IdP method/provider not preserved: %+v", got)
	}
	if got.ClientID != "client-external" || got.ClientSecret != "" {
		t.Fatalf("client fields not preserved: clientID=%q clientSecret=%q", got.ClientID, got.ClientSecret)
	}
	if got.TokenEndpoint != fakeTokenEndpoint.URL || got.IssuerURL != "https://idp.example.test/tenant/v2.0" || got.Scopes != "openid offline_access profile" {
		t.Fatalf("external IdP metadata not preserved: %+v", got)
	}
	if got.ProfileArn != "arn:aws:codewhisperer:us-east-1:123:profile/ABC" {
		t.Fatalf("profileArn = %q", got.ProfileArn)
	}
}

func TestApiImportCredentialsPreservesValidExternalIDPAccessToken(t *testing.T) {
	cfgFile := t.TempDir() + "/config.json"
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	defer installCleanAuthClient(t)()

	tokenEndpointCalls := 0
	fakeTokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenEndpointCalls++
		http.Error(w, "{\"error\":\"refresh_must_not_run\"}", http.StatusBadRequest)
	}))
	defer fakeTokenEndpoint.Close()

	expiresAt := time.Now().Unix() + 1800
	accessToken := testJWTWithExpiry(expiresAt)
	body, err := json.Marshal(map[string]interface{}{
		"email":         "external@example.test",
		"accessToken":   accessToken,
		"refreshToken":  "rt-external",
		"clientId":      "client-external",
		"authMethod":    "external_idp",
		"region":        "us-east-1",
		"profileArn":    "arn:aws:codewhisperer:us-east-1:123:profile/ABC",
		"tokenEndpoint": fakeTokenEndpoint.URL,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	h := &Handler{pool: accountpool.GetPool()}
	req := httptest.NewRequest("POST", "/auth/credentials", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	h.apiImportCredentials(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid external_idp import, got %d body=%s", rec.Code, rec.Body.String())
	}
	if tokenEndpointCalls != 0 {
		t.Fatalf("external IdP token endpoint called %d times; wanted 0", tokenEndpointCalls)
	}

	accs := config.GetAccounts()
	if len(accs) != 1 {
		t.Fatalf("expected exactly one account persisted, got %d", len(accs))
	}
	got := accs[0]
	if got.Email != "external@example.test" {
		t.Fatalf("email = %q", got.Email)
	}
	if got.AccessToken != accessToken {
		t.Fatalf("interactive external IdP access token was replaced")
	}
	if got.RefreshToken != "rt-external" {
		t.Fatalf("refreshToken = %q", got.RefreshToken)
	}
	if got.ExpiresAt != expiresAt {
		t.Fatalf("ExpiresAt = %d, want %d", got.ExpiresAt, expiresAt)
	}
}

func TestImportedJWTExpiresAtRejectsOpaqueAndMalformedTokens(t *testing.T) {
	for _, token := range []string{"opaque", "a.not-base64.c", "a.e30.c"} {
		if expiresAt, ok := importedJWTExpiresAt(token); ok {
			t.Fatalf("importedJWTExpiresAt(%q) = (%d, true)", token, expiresAt)
		}
	}
}

func TestDecodeCredentialImportRequestAcceptsOriginalExportArray(t *testing.T) {
	body := `[{"type":"kiro","access_token":"at","refresh_token":"rt","client_id":"client","client_secret":"secret","region":"eu-central-1","auth_method":"idc","profile_arn":"arn:aws:codewhisperer:eu-central-1:123:profile/ABC"}]`

	got, err := decodeCredentialImportRequest(strings.NewReader(body))
	if err != nil {
		t.Fatalf("decodeCredentialImportRequest: %v", err)
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" || got.ClientID != "client" || got.ClientSecret != "secret" {
		t.Fatalf("credential fields were not mapped: %+v", got)
	}
	if got.Region != "eu-central-1" || got.AuthMethod != "idc" {
		t.Fatalf("region/authMethod were not mapped: %+v", got)
	}
	if got.ProfileArn != "arn:aws:codewhisperer:eu-central-1:123:profile/ABC" {
		t.Fatalf("profileArn = %q", got.ProfileArn)
	}
}

func TestDecodeCredentialImportRequestAcceptsKAMExportEnvelope(t *testing.T) {
	body := `{"version":"merged","accounts":[{"email":"external@example.test","idp":"Enterprise","profileArn":"arn:aws:codewhisperer:us-east-1:123:profile/ABC","credentials":{"accessToken":"at","refreshToken":"rt","clientId":"client","authMethod":"external_idp","provider":"ExternalIdp","region":"us-east-1","tokenEndpoint":"https://idp.example.test/token","issuerUrl":"https://idp.example.test","scopes":"openid offline_access"}}]}`

	got, err := decodeCredentialImportRequest(strings.NewReader(body))
	if err != nil {
		t.Fatalf("decodeCredentialImportRequest: %v", err)
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" || got.ClientID != "client" {
		t.Fatalf("credential fields were not mapped: %+v", got)
	}
	if got.AuthMethod != "external_idp" || got.Provider != "ExternalIdp" {
		t.Fatalf("auth fields were not mapped: %+v", got)
	}
	if got.TokenEndpoint != "https://idp.example.test/token" || got.IssuerURL != "https://idp.example.test" || got.Scopes != "openid offline_access" {
		t.Fatalf("external IdP metadata was not mapped: %+v", got)
	}
	if got.ProfileArn != "arn:aws:codewhisperer:us-east-1:123:profile/ABC" {
		t.Fatalf("profileArn = %q", got.ProfileArn)
	}
}

func TestDecodeCredentialImportRequestRejectsMultiAccountArray(t *testing.T) {
	_, err := decodeCredentialImportRequest(strings.NewReader(`[{"refresh_token":"one"},{"refresh_token":"two"}]`))
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected multi-account array error, got %v", err)
	}
}

// authOidcURL captures the current oidc URL builder so the test can restore it.
func authOidcURL() func(string) string { return auth.GetOIDCTokenURLForTest() }
