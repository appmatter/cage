#!/bin/sh
# Example on-create: guest provision hook (runs once per VM).
set -eu
echo "on-create: start $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "your startup scripts here"

# Example — install npm (slow on stock ubuntu; prefer a baked image later):
# export DEBIAN_FRONTEND=noninteractive
# export NEEDRESTART_MODE=a
# export NEEDRESTART_SUSPEND=1
# export APT_LISTCHANGES_FRONTEND=none
# echo 'debconf debconf/frontend select Noninteractive' | sudo debconf-set-selections
# sudo -E apt-get update -y
# sudo -E apt-get install -y \
#   -o Dpkg::Options::="--force-confdef" \
#   -o Dpkg::Options::="--force-confold" \
#   -o APT::Color=0 \
#   npm
# npm -v

echo "on-create: done $(date -u +%Y-%m-%dT%H:%M:%SZ)"
