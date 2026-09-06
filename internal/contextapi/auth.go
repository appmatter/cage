package contextapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// NewToken returns a random bearer token for one serve process.
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// TokenGrant is the set of configs and VMs a bearer token may use.
// Empty Configs means all allowlisted configs. Empty VMs means all VMs under allowed configs.
type TokenGrant struct {
	Configs []string
	VMs     []string
}

// BearerTokens authorizes requests by Authorization: Bearer <token>.
func BearerTokens(tokens map[string]TokenGrant) Authorize {
	return func(r *http.Request, config, vm string) bool {
		token := bearerToken(r)
		if token == "" {
			return false
		}
		grant, ok := tokens[token]
		if !ok {
			return false
		}
		if config == "" {
			return true
		}
		if len(grant.Configs) > 0 && !stringIn(grant.Configs, config) {
			return false
		}
		if vm != "" && len(grant.VMs) > 0 && !stringIn(grant.VMs, vm) {
			return false
		}
		return true
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, prefix))
}

func stringIn(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
