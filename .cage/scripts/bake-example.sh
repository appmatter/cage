#!/bin/sh
# Example bake script: runs once per content hash into a derived image.
# Prefer this for heavy installs (npm, pi, …). Use on-create only for per-VM leftovers.
set -eu
echo "bake: start $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Example — uncomment to bake npm into the derived image:
# export DEBIAN_FRONTEND=noninteractive
# export NEEDRESTART_MODE=a
# sudo -E apt-get update -y
# sudo -E apt-get install -y npm
# npm -v

echo "bake: done $(date -u +%Y-%m-%dT%H:%M:%SZ)"
