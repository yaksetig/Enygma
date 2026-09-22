#!/usr/bin/env bash
# Enygma Retail Payments — one-command demo launcher.
#
#   cd demo && bash run.sh
#   open http://localhost:9091
#
# Brings up the whole stack from a clean local state, in order:
#
#   1. Hardhat node          :8545   (fresh chain, every run)
#   2. Contract deploy + init         (writes build/receipts.json)
#   3. gnark prover          :8082
#   4. Relayer               :8090   (wired to the tag registries just deployed)
#   5. This demo server      :9091
#
# Every component logs to demo/logs/<name>.log. Ctrl-C stops everything this
# script started; anything that was already running is left alone (and refused
# up front, because a stale chain would not match the fresh receipts).
#
# Expect ~60-90 s on a warm Go build cache, longer on the first run while the
# prover and go-ethereum compile.
#
# WARNING: every key in this demo comes from the repo's public Hardhat mnemonic,
# and the demo server serves those private keys to its own UI. It binds to
# 127.0.0.1 only. Never point it at a network holding value.

set -euo pipefail

DEMO_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DEMO_DIR/.." && pwd)"
DVP_ROOT="$(cd "$ROOT/../enygma_dvp" 2>/dev/null && pwd || true)"
LOG_DIR="$DEMO_DIR/logs"

CHAIN_PORT=8545
GNARK_PORT=8082
RELAYER_PORT=8090
DEMO_PORT=${DEMO_PORT:-9091}

# Hardhat account[2] — the relayer's signing key. Public, demo-only.
RELAYER_KEY=9883c26cc126a37158c4ffcc9d401d3ffa41187d9b1a18ce4912398d22597cda
RELAYER_API_KEY=test-api-key-dev-only

# go-ethereum needs CGo, and Homebrew's clang points at a macOS SDK path that
# may not exist. The system clang always does.
if [ -x /usr/bin/clang ]; then export CC=/usr/bin/clang; fi

PIDS=()

say()  { printf '\033[36m[run]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[run]\033[0m %s\n' "$*"; }
die()  { printf '\033[31m[run] %s\033[0m\n' "$*" >&2; exit 1; }

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    if [ ${#PIDS[@]} -gt 0 ]; then
        echo
        say "shutting down..."
        for pid in "${PIDS[@]}"; do
            kill "$pid" 2>/dev/null || true
        done
        for pid in "${PIDS[@]}"; do
            wait "$pid" 2>/dev/null || true
        done
        # npx forks the real node process, so killing the tracked pid can leave a
        # listener behind. Sweep the ports this script claimed, and only those --
        # restricted to LISTEN so a browser or curl holding a client connection to
        # one of these ports is never caught in the sweep.
        if command -v lsof >/dev/null 2>&1; then
            for port in "$CHAIN_PORT" "$GNARK_PORT" "$RELAYER_PORT" "$DEMO_PORT"; do
                local leftover
                leftover=$(lsof -ti "tcp:$port" -sTCP:LISTEN 2>/dev/null || true)
                [ -n "$leftover" ] && kill $leftover 2>/dev/null || true
            done
        fi
    fi
    exit "$status"
}
trap cleanup EXIT INT TERM

port_busy() { nc -z 127.0.0.1 "$1" >/dev/null 2>&1; }

# wait_port <port> <name> <logfile> <timeout-seconds>
wait_port() {
    local port=$1 name=$2 logfile=$3 timeout=$4 waited=0
    while ! port_busy "$port"; do
        if [ "$waited" -ge "$timeout" ]; then
            echo
            warn "last 20 lines of $logfile:"
            tail -20 "$logfile" 2>/dev/null || true
            die "$name did not come up on port $port within ${timeout}s"
        fi
        sleep 1
        waited=$((waited + 1))
        if [ $((waited % 5)) -eq 0 ]; then printf '.'; fi
    done
    [ "$waited" -ge 5 ] && echo
    say "$name is up on :$port (${waited}s)"
}

# ── 1. Preflight ──────────────────────────────────────────────────────────────

say "checking prerequisites…"

for tool in go node npx nc; do
    command -v "$tool" >/dev/null 2>&1 || die "'$tool' is required but not on PATH"
done
[ -n "$DVP_ROOT" ] || die "enygma_dvp must be cloned next to enygma_retail_payments (it provides the Hardhat node)"

for entry in "$CHAIN_PORT:Hardhat node" "$GNARK_PORT:gnark prover" "$RELAYER_PORT:relayer" "$DEMO_PORT:demo server"; do
    port=${entry%%:*}; label=${entry#*:}
    if port_busy "$port"; then
        die "port $port is already in use ($label). This launcher deploys a fresh chain, so a leftover process would serve stale contract addresses. Stop it and re-run."
    fi
done

mkdir -p "$LOG_DIR"

# lattice_zk is listed in src/go.mod but never imported. Go's lazy module
# loading still wants to resolve it, so keep a placeholder module present.
if [ ! -d "$ROOT/../lattice_zk" ] && [ ! -d "$ROOT/lattice_zk" ]; then
    mkdir -p "$ROOT/lattice_zk"
    printf 'module lattice_zk\n\ngo 1.24.0\n' > "$ROOT/lattice_zk/go.mod"
    say "created lattice_zk placeholder"
fi

if [ ! -d "$DVP_ROOT/node_modules/hardhat" ]; then
    say "installing Hardhat dependencies in enygma_dvp (first run only)…"
    (cd "$DVP_ROOT" && npm install --no-audit --no-fund) > "$LOG_DIR/npm-install.log" 2>&1 \
        || { warn "npm install failed — see $LOG_DIR/npm-install.log"; die "cannot start the Hardhat node"; }
fi

# ── 2. Hardhat node ───────────────────────────────────────────────────────────

say "starting a fresh Hardhat node on :${CHAIN_PORT}…"
(cd "$DVP_ROOT" && exec npx hardhat node --port "$CHAIN_PORT") > "$LOG_DIR/hardhat.log" 2>&1 &
PIDS+=($!)
wait_port "$CHAIN_PORT" "Hardhat node" "$LOG_DIR/hardhat.log" 120

# ── 3. Deploy + initialise contracts ──────────────────────────────────────────

say "exporting verifying keys…"
(cd "$ROOT/gnark_circuits" && go run ./cmd/export_vk/ ../build) > "$LOG_DIR/deploy.log" 2>&1 \
    || { tail -20 "$LOG_DIR/deploy.log"; die "VK export failed — see $LOG_DIR/deploy.log"; }

say "deploying contracts…"
go build -C "$ROOT/scripts" -o /tmp/rp_deploy deploy.go >> "$LOG_DIR/deploy.log" 2>&1 \
    || { tail -20 "$LOG_DIR/deploy.log"; die "deploy build failed — see $LOG_DIR/deploy.log"; }
(cd "$ROOT" && /tmp/rp_deploy) >> "$LOG_DIR/deploy.log" 2>&1 \
    || { tail -20 "$LOG_DIR/deploy.log"; die "deploy failed — see $LOG_DIR/deploy.log"; }

say "initialising EnygmaDvp with the payment verifying key…"
go build -C "$ROOT/scripts" -o /tmp/rp_init init.go >> "$LOG_DIR/deploy.log" 2>&1 \
    || { tail -20 "$LOG_DIR/deploy.log"; die "init build failed — see $LOG_DIR/deploy.log"; }
(cd "$ROOT" && /tmp/rp_init) >> "$LOG_DIR/deploy.log" 2>&1 \
    || { tail -20 "$LOG_DIR/deploy.log"; die "init failed — see $LOG_DIR/deploy.log"; }

RECEIPTS="$ROOT/build/receipts.json"
[ -f "$RECEIPTS" ] || die "build/receipts.json was not written — see $LOG_DIR/deploy.log"

read_addr() {
    python3 -c "import json,sys; print(json.load(open('$RECEIPTS')).get('$1',{}).get('contractAddress',''))"
}
TAG_REGISTRY_ADDR=$(read_addr TagRegistry)
TAG_CHANNEL_ADDR=$(read_addr TagChannelRegistry)
[ -n "$TAG_REGISTRY_ADDR" ] && [ -n "$TAG_CHANNEL_ADDR" ] \
    || die "TagRegistry / TagChannelRegistry addresses missing from receipts.json"
say "TagRegistry $TAG_REGISTRY_ADDR · TagChannelRegistry $TAG_CHANNEL_ADDR"

# ── 4. gnark prover ───────────────────────────────────────────────────────────

say "building the gnark prover..."
# gnark_circuits/ has two files declaring main (server + key generation), so the
# server must be built from main.go alone, never from the package.
go build -C "$ROOT/gnark_circuits" -o /tmp/rp_gnark main.go > "$LOG_DIR/gnark-build.log" 2>&1 \
    || { tail -30 "$LOG_DIR/gnark-build.log"; die "gnark build failed - see $LOG_DIR/gnark-build.log"; }

say "starting the gnark prover on :${GNARK_PORT} (loads the Groth16 proving key)..."
(cd "$ROOT/gnark_circuits" && exec /tmp/rp_gnark) > "$LOG_DIR/gnark.log" 2>&1 &
PIDS+=($!)
wait_port "$GNARK_PORT" "gnark prover" "$LOG_DIR/gnark.log" 300

# ── 5. Relayer ────────────────────────────────────────────────────────────────

say "building the relayer..."
go build -C "$ROOT/relayer" -o /tmp/rp_relayer . > "$LOG_DIR/relayer-build.log" 2>&1 \
    || { tail -30 "$LOG_DIR/relayer-build.log"; die "relayer build failed - see $LOG_DIR/relayer-build.log"; }

say "starting the relayer on :${RELAYER_PORT}..."
(cd "$ROOT/relayer" && \
    RELAYER_PRIVATE_KEY="$RELAYER_KEY" \
    RELAYER_API_KEY="$RELAYER_API_KEY" \
    RELAYER_TAG_REGISTRY_ADDR="$TAG_REGISTRY_ADDR" \
    RELAYER_TAG_CHANNEL_REGISTRY_ADDR="$TAG_CHANNEL_ADDR" \
    exec /tmp/rp_relayer) > "$LOG_DIR/relayer.log" 2>&1 &
PIDS+=($!)
wait_port "$RELAYER_PORT" "relayer" "$LOG_DIR/relayer.log" 180

# ── 6. Demo server ────────────────────────────────────────────────────────────

say "building the demo server…"
(cd "$DEMO_DIR" && go build -o /tmp/rp_retail_demo .) > "$LOG_DIR/demo-build.log" 2>&1 \
    || { tail -30 "$LOG_DIR/demo-build.log"; die "demo build failed — see $LOG_DIR/demo-build.log"; }

say "starting the demo server on :${DEMO_PORT}…"
(cd "$DEMO_DIR" && DEMO_PORT="$DEMO_PORT" exec /tmp/rp_retail_demo) > "$LOG_DIR/demo.log" 2>&1 &
PIDS+=($!)
wait_port "$DEMO_PORT" "demo server" "$LOG_DIR/demo.log" 60

URL="http://localhost:${DEMO_PORT}"
BOX_W=58
box_line() { printf '  \xe2\x94\x82 %-*s \xe2\x94\x82\n' "$BOX_W" "$1"; }
printf '\n  \xe2\x94\x8c'; printf '\xe2\x94\x80%.0s' $(seq 1 $((BOX_W + 2))); printf '\xe2\x94\x90\n'
box_line "Enygma Retail Payments - live demo"
box_line ""
box_line "    $URL"
box_line ""
box_line "Start with \"Register 8 parties\", pick who you are acting"
box_line "as, open a channel, then send a payment."
box_line ""
box_line "Logs:  demo/logs/{hardhat,gnark,relayer,demo}.log"
box_line "Ctrl-C stops every component this script started."
printf '  \xe2\x94\x94'; printf '\xe2\x94\x80%.0s' $(seq 1 $((BOX_W + 2))); printf '\xe2\x94\x98\n\n'

# Hold the foreground until a component dies, so a half-dead stack surfaces
# instead of lingering. `wait -n` is bash 4+; macOS ships bash 3.2.
while :; do
    for pid in "${PIDS[@]}"; do
        if ! kill -0 "$pid" 2>/dev/null; then
            warn "a component (pid $pid) exited - check demo/logs/"
            exit 1
        fi
    done
    sleep 2
done
