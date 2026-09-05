package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Codex CLI public OAuth client. Same registration used by Codex, pi, and other
// tools OpenAI lists under Codex for Open Source.
// See: https://developers.openai.com/community/codex-for-oss
const (
	clientID              = "app_EMoamEEZ73f0CkXaXp7hrann"
	authBaseURL           = "https://auth.openai.com"
	authorizeURL          = authBaseURL + "/oauth/authorize"
	tokenURL              = authBaseURL + "/oauth/token"
	redirectURI           = "http://localhost:1455/auth/callback"
	deviceUserCodeURL     = authBaseURL + "/api/accounts/deviceauth/usercode"
	deviceTokenURL        = authBaseURL + "/api/accounts/deviceauth/token"
	deviceVerificationURI = authBaseURL + "/codex/device"
	deviceRedirectURI     = authBaseURL + "/deviceauth/callback"
	oauthScope            = "openid profile email offline_access"
	originator            = "cage"
	callbackPort          = 1455
	deviceCodeTimeout     = 15 * time.Minute
	refreshSkew           = 5 * time.Minute
	jwtAuthClaim          = "https://api.openai.com/auth"
)

type tokenBundle struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	AccountID    string
	ExpiresAt    time.Time
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type pkce struct {
	Verifier  string
	Challenge string
}

func newPKCE() (pkce, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return pkce{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return pkce{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

func randomState() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hexEncode(raw), nil
}

func hexEncode(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

func authorizationURL(challenge, state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", oauthScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", originator)
	return authorizeURL + "?" + q.Encode()
}

func exchangeCode(httpClient *http.Client, code, verifier, redirect string) (tokenBundle, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirect)
	return postToken(httpClient, form)
}

func refreshTokens(httpClient *http.Client, refreshToken string) (tokenBundle, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	return postToken(httpClient, form)
}

func postToken(httpClient *http.Client, form url.Values) (tokenBundle, error) {
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenBundle{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return tokenBundle{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenBundle{}, fmt.Errorf("token request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenBundle{}, fmt.Errorf("token response: %w", err)
	}
	if tr.AccessToken == "" || tr.RefreshToken == "" {
		return tokenBundle{}, fmt.Errorf("token response missing access or refresh token")
	}
	bundle := tokenBundle{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		IDToken:      tr.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}
	if bundle.IDToken == "" {
		bundle.IDToken = tr.AccessToken
	}
	accountID, err := accountIDFromJWT(bundle.AccessToken)
	if err != nil || accountID == "" {
		accountID, _ = accountIDFromJWT(bundle.IDToken)
	}
	if accountID == "" {
		return tokenBundle{}, fmt.Errorf("failed to extract chatgpt_account_id from token")
	}
	bundle.AccountID = accountID
	return bundle, nil
}

type deviceAuthStart struct {
	DeviceAuthID   string
	UserCode       string
	Interval       time.Duration
	VerificationURI string
}

func startDeviceAuth(httpClient *http.Client) (deviceAuthStart, error) {
	req, err := http.NewRequest(http.MethodPost, deviceUserCodeURL, strings.NewReader(`{"client_id":"`+clientID+`"}`))
	if err != nil {
		return deviceAuthStart{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return deviceAuthStart{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return deviceAuthStart{}, fmt.Errorf("device code login not enabled; use login: browser")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deviceAuthStart{}, fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw struct {
		DeviceAuthID string      `json:"device_auth_id"`
		UserCode     string      `json:"user_code"`
		Interval     json.Number `json:"interval"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return deviceAuthStart{}, err
	}
	intervalSec, _ := raw.Interval.Float64()
	if raw.DeviceAuthID == "" || raw.UserCode == "" || intervalSec < 0 {
		return deviceAuthStart{}, fmt.Errorf("invalid device code response")
	}
	return deviceAuthStart{
		DeviceAuthID:    raw.DeviceAuthID,
		UserCode:        raw.UserCode,
		Interval:        time.Duration(intervalSec) * time.Second,
		VerificationURI: deviceVerificationURI,
	}, nil
}

func pollDeviceAuth(httpClient *http.Client, device deviceAuthStart) (code, verifier string, err error) {
	deadline := time.Now().Add(deviceCodeTimeout)
	interval := device.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for time.Now().Before(deadline) {
		payload := fmt.Sprintf(`{"device_auth_id":%q,"user_code":%q}`, device.DeviceAuthID, device.UserCode)
		req, reqErr := http.NewRequest(http.MethodPost, deviceTokenURL, strings.NewReader(payload))
		if reqErr != nil {
			return "", "", reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := httpClient.Do(req)
		if doErr != nil {
			return "", "", doErr
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var ok struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.Unmarshal(body, &ok); err != nil {
				return "", "", err
			}
			if ok.AuthorizationCode == "" || ok.CodeVerifier == "" {
				return "", "", fmt.Errorf("invalid device auth token response")
			}
			return ok.AuthorizationCode, ok.CodeVerifier, nil
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			time.Sleep(interval)
			continue
		}
		var errBody struct {
			Error any `json:"error"`
		}
		_ = json.Unmarshal(body, &errBody)
		codeStr := ""
		switch v := errBody.Error.(type) {
		case string:
			codeStr = v
		case map[string]any:
			if c, ok := v["code"].(string); ok {
				codeStr = c
			}
		}
		switch codeStr {
		case "deviceauth_authorization_pending":
			time.Sleep(interval)
			continue
		case "slow_down":
			interval += time.Second
			time.Sleep(interval)
			continue
		default:
			return "", "", fmt.Errorf("device auth failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
	return "", "", fmt.Errorf("device code login timed out")
}

// Tokens come from auth.openai.com; parse claims without local signature verify.
func parseClaims(token string) (jwt.MapClaims, error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("unexpected jwt claims type")
	}
	return claims, nil
}

func accountIDFromJWT(token string) (string, error) {
	claims, err := parseClaims(token)
	if err != nil {
		return "", err
	}
	auth, _ := claims[jwtAuthClaim].(map[string]any)
	if auth == nil {
		return "", nil
	}
	id, _ := auth["chatgpt_account_id"].(string)
	return id, nil
}

func jwtExpiresAt(token string) (time.Time, bool) {
	claims, err := parseClaims(token)
	if err != nil {
		return time.Time{}, false
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return time.Time{}, false
	}
	return exp.Time, true
}

func needsRefresh(bundle tokenBundle, now time.Time) bool {
	if bundle.AccessToken == "" || bundle.RefreshToken == "" {
		return true
	}
	exp := bundle.ExpiresAt
	if exp.IsZero() {
		if t, ok := jwtExpiresAt(bundle.AccessToken); ok {
			exp = t
		}
	}
	if exp.IsZero() {
		return false
	}
	return !now.Before(exp.Add(-refreshSkew))
}
