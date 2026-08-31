// Package guestenv installs operator-specified runtime.env into the guest.
// Host process environment is never copied — only the explicit map.
package guestenv

import (
	"fmt"
	"sort"
	"strings"
)

// InstallScript writes only keys from env into the guest. Does not read os.Environ.
func InstallScript(env map[string]string) string {
	if len(env) == 0 {
		return "true\n"
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "true\n"
	}

	var exports strings.Builder
	var etcLines strings.Builder
	for _, k := range keys {
		v := env[k]
		fmt.Fprintf(&exports, "export %s=%s\n", k, shellSingleQuote(v))
		// /etc/environment is KEY=value (no export); quote if needed for spaces.
		fmt.Fprintf(&etcLines, "%s=%s\n", k, v)
	}

	keyAlt := strings.Join(escapeRegexAlts(keys), "|")

	return fmt.Sprintf(`
mkdir -p /var/lib/cage /etc/profile.d
cat > /var/lib/cage/runtime.env <<'ENVEOF'
#!/bin/sh
%s
ENVEOF
chmod 0644 /var/lib/cage/runtime.env

hook='[ -r /var/lib/cage/runtime.env ] && . /var/lib/cage/runtime.env'
printf '%%s\n' '# Managed by Cage — runtime.env (explicit keys only)' "$hook" > /etc/profile.d/cage-runtime-env.sh
chmod 0644 /etc/profile.d/cage-runtime-env.sh

if [ -f /etc/bash.bashrc ] && ! grep -qF '/var/lib/cage/runtime.env' /etc/bash.bashrc 2>/dev/null; then
  printf '\n# Managed by Cage runtime.env\n%%s\n' "$hook" >> /etc/bash.bashrc
fi

tmp=$(mktemp)
if [ -f /etc/environment ]; then
  grep -Ev '^(%s)=' /etc/environment >"$tmp" || true
fi
{
  cat "$tmp"
  cat <<'ETCEOF'
%s
ETCEOF
} >/etc/environment
rm -f "$tmp"

if [ -f /var/lib/cage/shell ] && ! grep -qF '/var/lib/cage/runtime.env' /var/lib/cage/shell 2>/dev/null; then
  printf '\n[ -r /var/lib/cage/runtime.env ] && . /var/lib/cage/runtime.env\n' >> /var/lib/cage/shell
fi
`, exports.String(), keyAlt, etcLines.String())
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func escapeRegexAlts(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		var b strings.Builder
		for _, r := range k {
			switch r {
			case '.', '+', '*', '?', '(', ')', '[', ']', '{', '}', '\\', '|', '^', '$':
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		out[i] = b.String()
	}
	return out
}
