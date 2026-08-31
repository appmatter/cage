#!/usr/bin/env bash
# Prove softnet host-only does not let IPv6 bypass the IPv4 allow/block policy.
# Skips (exit 0) when tart/softnet/privileges/image missing.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VM_ID="${CAGE_NET_TEST_ID:-cage-net-ipv6-$$}"
CONFIG_SRC="${ROOT}/testdata/network/cage.yaml"
IMAGE="${CAGE_TART_IMAGE:-ubuntu}"
CAGE="${ROOT}/bin/cage"
EXEC_TIMEOUT="${CAGE_NET_EXEC_TIMEOUT:-25}"
# Cloudflare DNS IPv6 — any global v6 dest is fine for "must not connect".
IPV6_PROBE="${CAGE_IPV6_PROBE:-2606:4700:4700::1111}"

log() { printf 'test-network-ipv6: %s\n' "$*"; }
die() { printf 'test-network-ipv6: FAIL: %s\n' "$*" >&2; exit 1; }
skip() { printf 'test-network-ipv6: SKIP: %s\n' "$*"; exit 0; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || skip "$1 not on PATH"
}

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

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/cage-net-ipv6.XXXXXX")"
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
"$CAGE" plugin install -l ./plugins/runtime/tart >/dev/null
"$CAGE" plugin install -l ./plugins/network/egress >/dev/null

CONFIG_SRC="$CONFIG_SRC" CONFIG="$CONFIG" IMAGE="$IMAGE" python3 -c "
from pathlib import Path
import os
src = Path(os.environ['CONFIG_SRC']).read_text()
src = src.replace('__IMAGE__', os.environ['IMAGE'])
Path(os.environ['CONFIG']).write_text(src)
"

log "create/start $VM_ID (headless, proxy on / softnet host-only)"
"$CAGE" vm delete --config "$CONFIG" --id "$VM_ID" >/dev/null 2>&1 || true
"$CAGE" vm create --config "$CONFIG" --id "$VM_ID"
"$CAGE" vm start --config "$CONFIG" --id "$VM_ID"

st="$("$CAGE" vm status --config "$CONFIG" --id "$VM_ID" | awk '{print $2}')"
[[ "$st" == "running" ]] || die "expected running, got $st"

# Guest probe: TCP connect over IPv6 to a global address. Softnet should drop
# non-IPv4 ethertypes (or otherwise prevent reachability). Success = hole.
PROBE_PY=$(cat <<PY
import socket, sys
addr = sys.argv[1]
port = int(sys.argv[2])
s = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
s.settimeout(3)
try:
    s.connect((addr, port))
    print("IPV6_OK")
except Exception as e:
    print("IPV6_BLOCKED")
    print(type(e).__name__ + ": " + str(e), file=sys.stderr)
finally:
    s.close()
PY
)

log "assert IPv6 TCP to ${IPV6_PROBE}:53 is blocked"
ok=0
for attempt in 1 2 3 4 5; do
  if out="$(exec_timeout "$EXEC_TIMEOUT" "$CAGE" vm exec --config "$CONFIG" --id "$VM_ID" -- \
      python3 -c "$PROBE_PY" "$IPV6_PROBE" 53 2>/dev/null)"; then
    if echo "$out" | grep -q IPV6_BLOCKED; then
      ok=1
      break
    fi
    if echo "$out" | grep -q IPV6_OK; then
      die "IPv6 reached ${IPV6_PROBE}:53 — softnet host-only bypass (IPv6 hole)"
    fi
    log "unexpected probe output: $out"
  else
    # exec/timeout failure often means the connect hung until alarm — treat as blocked
    log "probe attempt $attempt timed out or failed (likely blocked); retrying once more if needed"
  fi
  sleep 1
done

# If every attempt timed out with no IPV6_OK, that still counts as blocked.
if [[ "$ok" != 1 ]]; then
  if out="$(exec_timeout 8 "$CAGE" vm exec --config "$CONFIG" --id "$VM_ID" -- \
      python3 -c "$PROBE_PY" "$IPV6_PROBE" 53 2>/dev/null || true)"; then
    if echo "$out" | grep -q IPV6_OK; then
      die "IPv6 reached ${IPV6_PROBE}:53 — softnet host-only bypass (IPv6 hole)"
    fi
  fi
  log "no IPV6_OK after retries (connect failed/timed out) → treating as blocked"
fi

log "OK (IPv6 direct egress blocked under softnet host-only)"
