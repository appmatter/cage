package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInteractiveLoginEnv(t *testing.T) {
	t.Setenv("CAGE_SECRETS_INTERACTIVE", "0")
	if interactiveLoginAllowed() {
		t.Fatal("expected non-interactive")
	}
	if !canAttemptLogin("browser") {
		t.Fatal("browser login should work from detached proxy")
	}
	if canAttemptLogin("device_code") {
		t.Fatal("device_code should stay foreground-only when non-interactive")
	}
	t.Setenv("CAGE_SECRETS_INTERACTIVE", "")
	if !interactiveLoginAllowed() {
		t.Fatal("expected interactive by default")
	}
	if !canAttemptLogin("device_code") {
		t.Fatal("device_code ok when interactive")
	}
}

func TestEnsureTokensNoLoginWhenNonInteractive(t *testing.T) {
	t.Setenv("CAGE_SECRETS_INTERACTIVE", "0")
	p := &OpenAIOAuth{client: &http.Client{Timeout: time.Second}}
	_, err := p.ensureTokens(filepath.Join(t.TempDir(), "missing.json"), "device_code", p.client)
	if err == nil {
		t.Fatal("expected error without auth file")
	}
}

func TestEnsureTokensRefreshFailDeviceCodeNonInteractive(t *testing.T) {
	t.Setenv("CAGE_SECRETS_INTERACTIVE", "0")
	path := filepath.Join(t.TempDir(), "auth.json")
	access := testJWT(map[string]any{
		jwtAuthClaim: map[string]any{"chatgpt_account_id": "acct"},
		"exp":        time.Now().Add(-time.Hour).Unix(),
	})
	if err := saveAuth(path, tokenBundle{
		AccessToken:  access,
		RefreshToken: "dead",
		IDToken:      access,
		AccountID:    "acct",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       http.NoBody,
				Header:     make(http.Header),
			}, nil
		}),
	}
	p := &OpenAIOAuth{client: client}
	_, err := p.ensureTokens(path, "device_code", client)
	if err == nil {
		t.Fatal("expected refresh+device_code failure")
	}
	if !strings.Contains(err.Error(), "device_code") {
		t.Fatalf("want device_code hint, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFieldValue(t *testing.T) {
	b := tokenBundle{AccessToken: "a", AccountID: "acct", IDToken: "id"}
	got, err := fieldValue(b, "access_token")
	if err != nil || got != "a" {
		t.Fatalf("access_token: %v %q", err, got)
	}
	got, err = fieldValue(b, "account_id")
	if err != nil || got != "acct" {
		t.Fatalf("account_id: %v %q", err, got)
	}
	if _, err := fieldValue(b, "refresh_token"); err == nil {
		t.Fatal("expected refresh_token refusal")
	}
	if _, err := fieldValue(b, "nope"); err == nil {
		t.Fatal("expected unknown field")
	}
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	if !needsRefresh(tokenBundle{}, now) {
		t.Fatal("empty should refresh")
	}
	fresh := tokenBundle{
		AccessToken:  "x",
		RefreshToken: "y",
		ExpiresAt:    now.Add(20 * time.Minute),
	}
	if needsRefresh(fresh, now) {
		t.Fatal("fresh token should not refresh")
	}
	near := tokenBundle{
		AccessToken:  "x",
		RefreshToken: "y",
		ExpiresAt:    now.Add(2 * time.Minute),
	}
	if !needsRefresh(near, now) {
		t.Fatal("near-expiry should refresh")
	}
}

func testJWT(claims map[string]any) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	raw, _ := json.Marshal(claims)
	return hdr + "." + base64.RawURLEncoding.EncodeToString(raw) + "."
}

func TestAccountIDFromJWT(t *testing.T) {
	token := testJWT(map[string]any{
		jwtAuthClaim: map[string]any{"chatgpt_account_id": "acct-123"},
	})
	got, err := accountIDFromJWT(token)
	if err != nil || got != "acct-123" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestSaveLoadAuth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	in := tokenBundle{
		AccessToken:  "access",
		RefreshToken: "refresh",
		IDToken:      "id",
		AccountID:    "acct",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := saveAuth(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := loadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.AccessToken != in.AccessToken || out.RefreshToken != in.RefreshToken || out.AccountID != in.AccountID {
		t.Fatalf("roundtrip %#v", out)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("auth file too open: %v", fi.Mode())
	}
}

func TestConfigureAndResolveFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	access := testJWT(map[string]any{
		jwtAuthClaim: map[string]any{"chatgpt_account_id": "acct-xyz"},
		"exp":        time.Now().Add(time.Hour).Unix(),
	})
	if err := saveAuth(path, tokenBundle{
		AccessToken:  access,
		RefreshToken: "refresh",
		IDToken:      access,
		AccountID:    "acct-xyz",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	p := &OpenAIOAuth{}
	cfg := "path: " + path + "\nlogin: browser\n"
	if err := p.Configure([]byte(cfg)); err != nil {
		t.Fatal(err)
	}
	got, err := p.Resolve(map[string]string{
		"ACCESS_TOKEN": "access_token",
		"ACCOUNT_ID":   "account_id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["ACCESS_TOKEN"] != access || got["ACCOUNT_ID"] != "acct-xyz" {
		t.Fatalf("got %#v", got)
	}
}

func TestPostTokenClaims(t *testing.T) {
	access := testJWT(map[string]any{
		jwtAuthClaim: map[string]any{"chatgpt_account_id": "acct-r"},
		"exp":        time.Now().Add(time.Hour).Unix(),
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("grant_type=%s", r.Form.Get("grant_type"))
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  access,
			RefreshToken: "new-refresh",
			IDToken:      access,
			ExpiresIn:    3600,
		})
	}))
	defer srv.Close()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", "old")
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatal(err)
	}
	id, err := accountIDFromJWT(tr.AccessToken)
	if err != nil || id != "acct-r" {
		t.Fatalf("account id %q %v", id, err)
	}
}
