#!/usr/bin/env bash
# setup.sh — run from enygma_retail_payments/ root after:
#   1. npx hardhat node          (Terminal 1, keep running — run from enygma_dvp/)
#   2. cd gnark_circuits && go run generation.go   (run once when circuit changes)
#   3. bash setup.sh             (this script)
# Then separately:
#   4. cd gnark_circuits && go run main.go         (Terminal 2, keep running)
#   5. cd test && CC=/usr/bin/clang go test ./... -v -timeout 600s

set -e

ROOT="$(cd "$(dirname "$0")" && pwd)"
DVP_ROOT="$(cd "$ROOT/../enygma_dvp" && pwd)"
cd "$ROOT"

echo "==> [1/5] Compiling Solidity contracts (enygma_dvp)..."
cd "$DVP_ROOT"
npx hardhat compile

echo "==> [2/5] Regenerating Poseidon artifacts..."
node "$DVP_ROOT/regen_poseidon.mjs"

echo "==> [3/5] Copying updated ABI to retail payments..."
cp "$DVP_ROOT/artifacts/contracts/core/contracts/vaults/Erc20CoinVault.sol/Erc20CoinVault.json" \
   "$ROOT/contracts/abis/Erc20CoinVault.json"
cd "$ROOT"

echo "==> [4/5] Exporting Payment VK to build/Payment.json..."
cd gnark_circuits
go run ./cmd/export_vk/ ../build
cd "$ROOT"

echo "==> [5/5] Deploying and initialising contracts..."
CC=/usr/bin/clang go build -C scripts -o /tmp/rp_deploy deploy.go && /tmp/rp_deploy
CC=/usr/bin/clang go build -C scripts -o /tmp/rp_init init.go   && /tmp/rp_init

echo ""
echo "Setup complete."
echo ""
echo "Next steps:"
echo "  Terminal A: cd gnark_circuits && go run main.go"
echo "  Terminal B: cd test && CC=/usr/bin/clang go test ./... -v -timeout 600s"
