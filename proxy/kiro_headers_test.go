package proxy

import (
	"kiro-go-plus/config"
	"net/http"
	"testing"
)

func TestApplyKiroBaseHeadersSetsTokenTypeForExternalIDP(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	account := &config.Account{AccessToken: "at", AuthMethod: "external_idp"}

	applyKiroBaseHeaders(req, account, kiroHeaderValues{})

	if got := req.Header.Get("TokenType"); got != "EXTERNAL_IDP" {
		t.Fatalf("TokenType = %q", got)
	}
}

func TestApplyKiroBaseHeadersDoesNotSetTokenTypeForIDC(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	account := &config.Account{AccessToken: "at", AuthMethod: "idc"}

	applyKiroBaseHeaders(req, account, kiroHeaderValues{})

	if got := req.Header.Get("TokenType"); got != "" {
		t.Fatalf("TokenType = %q", got)
	}
}
