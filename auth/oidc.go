package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go-plus/config"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// oidcTokenURL 构造 idc/builderId 刷新 endpoint。测试可替换以拦截网络调用。
var oidcTokenURL = func(region string) string {
	return fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
}

// socialTokenURL 构造 social 刷新 endpoint。测试可替换以拦截网络调用。
var socialTokenURL = func() string {
	return "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
}

// RefreshToken 刷新 access token
// Returns: accessToken, refreshToken, expiresAt, profileArn, error
func RefreshToken(account *config.Account) (string, string, int64, string, error) {
	// Resolve per-account proxy: account.ProxyURL > global config
	proxyURL := account.ProxyURL
	if proxyURL == "" {
		proxyURL = config.GetProxyURL()
	}
	client := GetAuthClientForProxy(proxyURL)

	switch strings.ToLower(strings.TrimSpace(account.AuthMethod)) {
	case "social":
		return refreshSocialToken(account.RefreshToken, client)
	case "external_idp", "external-idp", "externalidp":
		return refreshExternalIDPToken(account.RefreshToken, account.ClientID, account.TokenEndpoint, account.Scopes, client)
	default:
		return refreshOIDCToken(account.RefreshToken, account.ClientID, account.ClientSecret, account.Region, client)
	}
}

// refreshOIDCToken IdC/Builder ID token 刷新
func refreshOIDCToken(refreshToken, clientID, clientSecret, region string, client *http.Client) (string, string, int64, string, error) {
	if clientID == "" || clientSecret == "" {
		return "", "", 0, "", fmt.Errorf("OIDC refresh requires clientId and clientSecret")
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = "us-east-1"
	}

	accessToken, newRefreshToken, expiresAt, profileArn, err := refreshOIDCTokenInRegion(
		refreshToken, clientID, clientSecret, region, client,
	)
	if err == nil || region == "us-east-1" {
		return accessToken, newRefreshToken, expiresAt, profileArn, err
	}

	accessToken, newRefreshToken, expiresAt, profileArn, fallbackErr := refreshOIDCTokenInRegion(
		refreshToken, clientID, clientSecret, "us-east-1", client,
	)
	if fallbackErr == nil {
		return accessToken, newRefreshToken, expiresAt, profileArn, nil
	}
	return "", "", 0, "", fmt.Errorf(
		"OIDC refresh failed in %s: %v; fallback us-east-1: %w",
		region, err, fallbackErr,
	)
}

func refreshOIDCTokenInRegion(refreshToken, clientID, clientSecret, region string, client *http.Client) (string, string, int64, string, error) {
	url := oidcTokenURL(region)

	payload := map[string]string{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"refreshToken": refreshToken,
		"grantType":    "refresh_token",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", 0, "", fmt.Errorf("refresh failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		ProfileArn   string `json:"profileArn"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, "", err
	}

	expiresAt := time.Now().Unix() + int64(result.ExpiresIn)
	return result.AccessToken, result.RefreshToken, expiresAt, result.ProfileArn, nil
}

// refreshSocialToken Social (GitHub/Google) token 刷新
func refreshSocialToken(refreshToken string, client *http.Client) (string, string, int64, string, error) {
	url := socialTokenURL()

	payload := map[string]string{
		"refreshToken": refreshToken,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", "", 0, "", fmt.Errorf("refresh failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		ProfileArn   string `json:"profileArn"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, "", err
	}

	expiresAt := time.Now().Unix() + int64(result.ExpiresIn)
	return result.AccessToken, result.RefreshToken, expiresAt, result.ProfileArn, nil
}

// refreshExternalIDPToken refreshes enterprise SSO tokens through the external
// IdP OAuth token endpoint. This flow is a public client refresh and does not
// have a clientSecret.
func refreshExternalIDPToken(refreshToken, clientID, tokenEndpoint, scopes string, client *http.Client) (string, string, int64, string, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	clientID = strings.TrimSpace(clientID)
	tokenEndpoint = strings.TrimSpace(tokenEndpoint)
	if refreshToken == "" {
		return "", "", 0, "", fmt.Errorf("external IdP refresh requires refreshToken")
	}
	if clientID == "" {
		return "", "", 0, "", fmt.Errorf("external IdP refresh requires clientId")
	}
	if tokenEndpoint == "" {
		return "", "", 0, "", fmt.Errorf("external IdP refresh requires tokenEndpoint")
	}

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if strings.TrimSpace(scopes) != "" {
		form.Set("scope", scopes)
	}

	req, err := http.NewRequest("POST", tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(respBody, &result)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.AccessToken == "" {
		if result.Error != "" {
			return "", "", 0, "", fmt.Errorf("external IdP refresh failed: %d %s: %s", resp.StatusCode, result.Error, result.ErrorDescription)
		}
		return "", "", 0, "", fmt.Errorf("external IdP refresh failed: %d %s", resp.StatusCode, string(respBody))
	}
	if result.ExpiresIn <= 0 {
		result.ExpiresIn = 3600
	}

	expiresAt := time.Now().Unix() + int64(result.ExpiresIn)
	return result.AccessToken, result.RefreshToken, expiresAt, "", nil
}
