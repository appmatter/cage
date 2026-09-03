package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Codex-compatible auth.json shape so path: ~/.codex/auth.json works.
type authFile struct {
	AuthMode    string     `json:"auth_mode,omitempty"`
	Tokens      *authTokens `json:"tokens,omitempty"`
	LastRefresh string     `json:"last_refresh,omitempty"`
}

type authTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
}

func defaultAuthPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cage", "secrets", "openai-oauth", "auth.json"), nil
}

func loadAuth(path string) (tokenBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tokenBundle{}, err
	}
	var af authFile
	if err := json.Unmarshal(data, &af); err != nil {
		return tokenBundle{}, fmt.Errorf("parse auth file: %w", err)
	}
	if af.Tokens == nil || af.Tokens.AccessToken == "" || af.Tokens.RefreshToken == "" {
		return tokenBundle{}, fmt.Errorf("auth file has no chatgpt tokens")
	}
	bundle := tokenBundle{
		AccessToken:  af.Tokens.AccessToken,
		RefreshToken: af.Tokens.RefreshToken,
		IDToken:      af.Tokens.IDToken,
		AccountID:    af.Tokens.AccountID,
	}
	if bundle.AccountID == "" {
		bundle.AccountID, _ = accountIDFromJWT(bundle.AccessToken)
	}
	if bundle.AccountID == "" {
		bundle.AccountID, _ = accountIDFromJWT(bundle.IDToken)
	}
	if t, ok := jwtExpiresAt(bundle.AccessToken); ok {
		bundle.ExpiresAt = t
	}
	return bundle, nil
}

func saveAuth(path string, bundle tokenBundle) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	af := authFile{
		AuthMode: "chatgpt",
		Tokens: &authTokens{
			IDToken:      bundle.IDToken,
			AccessToken:  bundle.AccessToken,
			RefreshToken: bundle.RefreshToken,
			AccountID:    bundle.AccountID,
		},
		LastRefresh: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(af, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
