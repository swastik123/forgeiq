#!/usr/bin/env bash
set -euo pipefail

cd /workspace

echo "[py_verify] start"
python3 --version || true

if [ -f /out/patch.diff ] && [ -s /out/patch.diff ]; then
  echo "[py_verify] applying patch.diff"
  git apply /out/patch.diff
fi

python3 -m venv /workspace/.venv
source /workspace/.venv/bin/activate

if [ -f requirements.txt ]; then
  pip install -r requirements.txt | tee /out/pip_install.txt
elif [ -f pyproject.toml ]; then
  # Best-effort: install build + pytest. For real use, prefer a lockfile/poetry/pdm.
  pip install -U pip setuptools wheel | tee /out/pip_bootstrap.txt
  pip install pytest ruff mypy bandit | tee /out/pip_tools.txt
else
  pip install pytest ruff mypy bandit | tee /out/pip_tools.txt
fi

python3 -m compileall -q . | tee /out/compileall.txt

pytest -q | tee /out/pytest.txt

tar -czf /out/artifacts.tgz -C /out .
echo "[py_verify] done"

