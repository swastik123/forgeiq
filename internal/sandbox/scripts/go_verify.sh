#!/usr/bin/env bash
set -euo pipefail

cd /workspace

echo "[go_verify] start"
go version || true

if [ -f /out/patch.diff ] && [ -s /out/patch.diff ]; then
  echo "[go_verify] applying patch.diff"
  git apply /out/patch.diff
fi

echo "[go_verify] go vet ./..."
go vet ./... | tee /out/go_vet.txt

echo "[go_verify] go test ./..."
go test ./... | tee /out/go_test.txt

echo "[go_verify] go build ./..."
go build ./... | tee /out/go_build.txt

tar -czf /out/artifacts.tgz -C /out .
echo "[go_verify] done"

