package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"

	secretsplugin "github.com/appmatter/cage/pkg/plugin/v1/secrets"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: secretsplugin.Handshake,
		Plugins:         secretsplugin.PluginMap(&OpenAIOAuth{}),
	})
}

// OpenAIOAuth resolves ChatGPT/Codex OAuth tokens for host-side proxy inject.
// Flow matches Codex CLI / pi (public Codex client id, localhost:1455, device code).
type OpenAIOAuth struct {
	mu     sync.Mutex
	path   string
	login  string // browser | device_code
	client *http.Client
}

func (o *OpenAIOAuth) Name() string { return "openai-oauth" }

func (o *OpenAIOAuth) Configure(raw []byte) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.path = cfg.Path
	o.login = cfg.Login
	if o.client == nil {
		o.client = &http.Client{Timeout: 30 * time.Second}
	}
	o.mu.Unlock()
	return nil
}

func (o *OpenAIOAuth) Resolve(refs map[string]string) (map[string]string, error) {
	o.mu.Lock()
	path := o.path
	login := o.login
	client := o.client
	o.mu.Unlock()
	if path == "" {
		return nil, fmt.Errorf("openai-oauth: not configured")
	}
	if len(refs) == 0 {
		return map[string]string{}, nil
	}

	bundle, err := o.ensureTokens(path, login, client)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(refs))
	for name, ref := range refs {
		field := strings.ToLower(strings.TrimSpace(ref))
		if field == "" {
			field = strings.ToLower(name)
		}
		val, err := fieldValue(bundle, field)
		if err != nil {
			return nil, fmt.Errorf("openai-oauth: %s: %w", name, err)
		}
		out[name] = val
	}
	return out, nil
}

func (o *OpenAIOAuth) ensureTokens(path, login string, client *http.Client) (tokenBundle, error) {
	bundle, err := loadAuth(path)
	if err == nil && !needsRefresh(bundle, time.Now()) {
		return bundle, nil
	}
	if err == nil && bundle.RefreshToken != "" {
		refreshed, refreshErr := refreshTokens(client, bundle.RefreshToken)
		if refreshErr == nil {
			if err := saveAuth(path, refreshed); err != nil {
				return tokenBundle{}, fmt.Errorf("openai-oauth: save refreshed tokens: %w", err)
			}
			return refreshed, nil
		}
		fmt.Fprintf(os.Stderr, "openai-oauth: refresh failed (%v)\n", refreshErr)
		if !canAttemptLogin(login) {
			return tokenBundle{}, fmt.Errorf("openai-oauth: token refresh failed (login=%s needs a foreground cage vm start): %w", login, refreshErr)
		}
		fmt.Fprintf(os.Stderr, "openai-oauth: starting login\n")
	} else if !canAttemptLogin(login) {
		if err != nil {
			return tokenBundle{}, fmt.Errorf("openai-oauth: no auth file (%v); run cage vm start to sign in", err)
		}
		return tokenBundle{}, fmt.Errorf("openai-oauth: access token expired and refresh unavailable; run cage vm start to sign in")
	}

	var loggedIn tokenBundle
	switch login {
	case "device_code":
		loggedIn, err = loginDevice(client)
	default:
		loggedIn, err = loginBrowser(client)
	}
	if err != nil {
		return tokenBundle{}, fmt.Errorf("openai-oauth: login: %w", err)
	}
	if err := saveAuth(path, loggedIn); err != nil {
		return tokenBundle{}, fmt.Errorf("openai-oauth: save tokens: %w", err)
	}
	return loggedIn, nil
}

// interactiveLoginAllowed is true unless the host sets CAGE_SECRETS_INTERACTIVE=0
// (detached proxy-serve mid-session refresh).
func interactiveLoginAllowed() bool {
	v := strings.TrimSpace(os.Getenv("CAGE_SECRETS_INTERACTIVE"))
	if v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "no") {
		return false
	}
	return true
}

// canAttemptLogin: browser can open a callback URL from the detached proxy;
// device_code needs a visible prompt, so it stays foreground-only when non-interactive.
func canAttemptLogin(login string) bool {
	if interactiveLoginAllowed() {
		return true
	}
	return login == "browser"
}

func fieldValue(bundle tokenBundle, field string) (string, error) {
	switch field {
	case "access_token", "access", "token":
		return bundle.AccessToken, nil
	case "account_id", "chatgpt_account_id":
		return bundle.AccountID, nil
	case "id_token":
		return bundle.IDToken, nil
	case "refresh_token":
		return "", fmt.Errorf("refusing to export refresh_token")
	default:
		return "", fmt.Errorf("unknown field %q (use access_token or account_id)", field)
	}
}
