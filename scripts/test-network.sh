#!/usr/bin/env bash
# Headless network smoke: proxy on + softnet host-only + guest proxy.env.
# Skips (exit 0) when tart/softnet/privileges/image missing.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VM_ID="${CAGE_NET_TEST_ID:-cage-net-test-$$}"
CONFIG_SRC="${ROOT}/testdata/network/cage.yaml"
IMAGE="${CAGE_TART_IMAGE:-ubuntu}"
CAGE="${ROOT}/bin/cage"
EXEC_TIMEOUT="${CAGE_NET_EXEC_TIMEOUT:-25}"

log() { printf 'test-network: %s\n' "$*"; }
die() { printf 'test-network: FAIL: %s\n' "$*" >&2; exit 1; }
skip() { printf 'test-network: SKIP: %s\n' "$*"; exit 0; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || skip "$1 not on PATH"
}

# Run argv with alarm(seconds). Exit 142/others on timeout (perl alarm).
exec_timeout() {
  local secs="$1"
  shift
  perl -e 'alarm shift; exec @ARGV' "$secs" "$@"
}

softnet_privileged() {
  local bin real
  bin="$(command -v softnet)" || return 1
  real="$(python3 -c "import os,sys; print(os.path.realpath(sys.argv[1]))" "$bin")"
  python3 -c "
import os, stat, sys
st = os.stat(sys.argv[1])
ok = (st.st_uid == 0) and (st.st_mode & stat.S_ISUID)
sys.exit(0 if ok else 1)
" "$real" 2>/dev/null && return 0
  sudo -n true >/dev/null 2>&1
}

tart_has_image() {
  tart list --format json 2>/dev/null | python3 -c "
import json,sys
name=sys.argv[1]
rows=json.load(sys.stdin)
sys.exit(0 if any(r.get('Name')==name for r in rows) else 1)
" "$1"
}

need_cmd tart
need_cmd softnet
need_cmd go
need_cmd perl
need_cmd python3
softnet_privileged || skip "softnet needs root-owned setuid (see docs/cli/quick-starts/macos/softnet.md) or passwordless sudo"
tart_has_image "$IMAGE" || skip "image $IMAGE not local (set CAGE_TART_IMAGE or tart pull/clone)"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/cage-net-test.XXXXXX")"
CONFIG="${WORKDIR}/cage.yaml"

stop_proxy() {
  if [[ -f ".cage/run/${VM_ID}/proxy.json" ]]; then
    python3 -c "
import json, os, signal, sys
p = '.cage/run/%s/proxy.json' % sys.argv[1]
try:
    st = json.load(open(p))
    os.kill(int(st.get('pid') or 0), signal.SIGTERM)
except Exception:
    pass
" "$VM_ID" 2>/dev/null || true
  fi
}

cleanup() {
  if [[ -x "$CAGE" ]]; then
    "$CAGE" vm stop --config "$CONFIG" --id "$VM_ID" >/dev/null 2>&1 || true
    "$CAGE" vm delete --config "$CONFIG" --id "$VM_ID" >/dev/null 2>&1 || true
  fi
  stop_proxy || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

log "building cage + plugins"
mkdir -p bin
go build -o "$CAGE" ./cmd/cage
"$CAGE" plugin install -l ./plugins/runtime/tart
"$CAGE" plugin install -l ./plugins/network/egress

CONFIG_SRC="$CONFIG_SRC" CONFIG="$CONFIG" IMAGE="$IMAGE" python3 -c "
from pathlib import Path
import os
src = Path(os.environ['CONFIG_SRC']).read_text()
src = src.replace('__IMAGE__', os.environ['IMAGE'])
Path(os.environ['CONFIG']).write_text(src)
"

log "create/start $VM_ID (headless, proxy on)"
"$CAGE" vm delete --config "$CONFIG" --id "$VM_ID" >/dev/null 2>&1 || true
"$CAGE" vm create --config "$CONFIG" --id "$VM_ID"
"$CAGE" vm start --config "$CONFIG" --id "$VM_ID"

st="$("$CAGE" vm status --config "$CONFIG" --id "$VM_ID" | awk '{print $2}')"
[[ "$st" == "running" ]] || die "expected running, got $st"

log "assert proxy.env + profile.d"
ok=0
for attempt in 1 2 3 4 5; do
  if exec_timeout "$EXEC_TIMEOUT" "$CAGE" vm exec --config "$CONFIG" --id "$VM_ID" -- \
      sh -c 'test -f /var/lib/cage/proxy.env && test -x /var/lib/cage/shell && test -f /etc/profile.d/cage-proxy.sh && grep -q ALL_PROXY= /etc/environment'; then
    ok=1
    break
  fi
  log "vm exec proxy.env attempt $attempt failed; retrying"
  sleep 2
done
[[ "$ok" == 1 ]] || die "proxy.env / profile.d / environment missing after retries"

log "assert direct egress blocked"
ok=0
for attempt in 1 2 3 4 5; do
  if out="$(exec_timeout "$EXEC_TIMEOUT" "$CAGE" vm exec --config "$CONFIG" --id "$VM_ID" -- \
      bash -c "timeout 3 bash -c 'echo >/dev/tcp/1.1.1.1/53' && echo DIRECT_OK || echo DIRECT_BLOCKED")"; then
    if echo "$out" | grep -q DIRECT_BLOCKED; then
      ok=1
      break
    fi
    log "unexpected direct probe output: $out"
  fi
  log "vm exec direct probe attempt $attempt failed; retrying"
  sleep 2
done
[[ "$ok" == 1 ]] || die "expected DIRECT_BLOCKED after retries"

log "OK"
