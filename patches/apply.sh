#!/usr/bin/env bash
set -euo pipefail

# Apply patches to submodules from a clean upstream checkout.
#
# Usage: ./patches/apply.sh
#   Run from the repo root. Submodules must be initialized (git submodule update --init).

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Upstream base commits the patches were generated against.
GETH_BASE="8a3a309fa97bff7252da3e7e8cac47d024d2e281"
PRYSM_BASE="0bfe7367302d5e83575ee4be6291eb6690dcb256"

echo "==> Applying go-ethereum patches (base: ${GETH_BASE:0:12})"
cd "$REPO_ROOT/submodules/go-ethereum"
git checkout -q "$GETH_BASE"
git am "$SCRIPT_DIR/go-ethereum/"*.patch
echo "    Applied $(ls "$SCRIPT_DIR/go-ethereum/"*.patch | wc -l) patches"

echo "==> Applying prysm patches (base: ${PRYSM_BASE:0:12})"
cd "$REPO_ROOT/submodules/prysm"
git checkout -q "$PRYSM_BASE"
git am "$SCRIPT_DIR/prysm/"*.patch
echo "    Applied $(ls "$SCRIPT_DIR/prysm/"*.patch | wc -l) patches"

echo "Done."
