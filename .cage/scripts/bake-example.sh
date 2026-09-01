#!/bin/sh
# Example bake script: runs once per content hash into a derived image.
# Prefer this for heavy installs (npm, pi, …). Use on-create only for per-VM leftovers.
set -eu
echo "bake: start $(date -u +%Y-%m-%dT%H:%M:%SZ)"

export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

# Go toolchain (matches go.mod)
GO_VERSION=1.26.2
case "$(uname -m)" in
	x86_64) GOARCH=amd64 ;;
	aarch64|arm64) GOARCH=arm64 ;;
	*) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" -o /tmp/go.tgz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tgz
rm -f /tmp/go.tgz
echo 'export PATH=/usr/local/go/bin:$PATH' | sudo tee /etc/profile.d/go.sh >/dev/null
/usr/local/go/bin/go version

echo "bake: done $(date -u +%Y-%m-%dT%H:%M:%SZ)"
