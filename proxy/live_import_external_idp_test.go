package proxy

import (
	"kiro-go-plus/config"
	accountpool "kiro-go-plus/pool"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveImportExternalIDPExport(t *testing.T) {
	exportPath := strings.TrimSpace(os.Getenv("KIRO_GO_LIVE_IMPORT_JSON"))
	if exportPath == "" {
		t.Skip("set KIRO_GO_LIVE_IMPORT_JSON to run live external_idp import validation")
	}
	raw, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read live import JSON: %v", err)
	}

	cfgFile := t.TempDir() + "/config.json"
	if err := config.Init(cfgFile); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	defer installCleanAuthClient(t)()

	h := &Handler{pool: accountpool.GetPool()}
	req := httptest.NewRequest("POST", "/auth/credentials", strings.NewReader(string(raw)))
	rec := httptest.NewRecorder()

	start := time.Now()
	h.apiImportCredentials(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on live external_idp import, got %d body=%s", rec.Code, rec.Body.String())
	}
	if time.Since(start) > 20*time.Second {
		t.Fatalf("live import took too long: %s", time.Since(start))
	}

	accs := config.GetAccounts()
	if len(accs) != 1 {
		t.Fatalf("expected exactly one imported account, got %d", len(accs))
	}
	got := accs[0]
	if got.AuthMethod != "external_idp" {
		t.Fatalf("authMethod = %q", got.AuthMethod)
	}
	if got.TokenEndpoint == "" || got.IssuerURL == "" || got.Scopes == "" {
		t.Fatalf("external IdP metadata missing after import")
	}
	if got.AccessToken == "" || got.RefreshToken == "" {
		t.Fatalf("tokens missing after import")
	}
}
