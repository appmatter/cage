package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type oauthCallbackResult struct {
	code string
	err  error
}

func loginBrowser(httpClient *http.Client) (tokenBundle, error) {
	codes, err := newPKCE()
	if err != nil {
		return tokenBundle{}, err
	}
	state, err := randomState()
	if err != nil {
		return tokenBundle{}, err
	}
	authURL := authorizationURL(codes.Challenge, state)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		return tokenBundle{}, fmt.Errorf("listen on :%d for OAuth callback: %w (is another Codex/pi login using the port?)", callbackPort, err)
	}
	defer ln.Close()

	done := make(chan oauthCallbackResult, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler:           oauthCallbackHandler(state, done),
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	fmt.Fprintf(os.Stderr, "openai-oauth: open this URL to sign in with ChatGPT (Codex):\n%s\n", authURL)
	_ = openBrowser(authURL)

	var res oauthCallbackResult
	select {
	case res = <-done:
	case <-time.After(deviceCodeTimeout):
		return tokenBundle{}, fmt.Errorf("oauth login timed out")
	}
	if res.err != nil {
		return tokenBundle{}, res.err
	}
	return exchangeCode(httpClient, res.code, codes.Verifier, redirectURI)
}

// oauthCallbackHandler serves the localhost OAuth redirect and sends one result on done.
func oauthCallbackHandler(state string, done chan<- oauthCallbackResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/callback" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case done <- oauthCallbackResult{err: fmt.Errorf("oauth state mismatch")}:
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case done <- oauthCallbackResult{err: fmt.Errorf("oauth callback missing code")}:
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>Cage</title><p>OpenAI sign-in complete. You can close this window.</p>`))
		select {
		case done <- oauthCallbackResult{code: code}:
		default:
		}
	})
}

func loginDevice(httpClient *http.Client) (tokenBundle, error) {
	device, err := startDeviceAuth(httpClient)
	if err != nil {
		return tokenBundle{}, err
	}
	fmt.Fprintf(os.Stderr, "openai-oauth: device code login\n  visit %s\n  enter code: %s\n", device.VerificationURI, device.UserCode)
	code, verifier, err := pollDeviceAuth(httpClient, device)
	if err != nil {
		return tokenBundle{}, err
	}
	return exchangeCode(httpClient, code, verifier, deviceRedirectURI)
}

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}
