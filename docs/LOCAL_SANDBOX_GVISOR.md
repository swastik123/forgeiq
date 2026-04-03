## Local sandbox verification (Mac) with Multipass + Docker (gVisor/runsc) + rsync + sandbox-worker

This enables PR verification runs (go test / pytest, etc.) inside a Linux VM using **Docker with the gVisor runtime (`runsc`)**, and stages code via **git archive / rsync**.

### 1) Create VM

```bash
brew install --cask multipass
multipass launch --name forgeiq-sbx --cpus 4 --memory 8G --disk 40G 22.04
```

### 2) Mount your repo into the VM (read-only recommended)

```bash
multipass mount /Users/swastiksabat/Documents/agentic-app/forgeiq forgeiq-sbx:/host/forgeiq
```

### 3) Install Docker + gVisor runtime + prerequisites inside VM

```bash
multipass shell forgeiq-sbx
sudo apt-get update
sudo apt-get install -y git curl ca-certificates tar gzip coreutils bash rsync
```

Install Docker (engine) and gVisor `runsc`, then configure Docker runtime:

- Ensure `runsc` is on PATH (per gVisor install docs)
- Configure `/etc/docker/daemon.json`:

```json
{
  "runtimes": {
    "runsc": {
      "path": "runsc"
    }
  }
}
```

Restart Docker after editing:

```bash
sudo systemctl restart docker
```

### 4) Prepare base directory (slots, runs)

Inside VM:

```bash
sudo mkdir -p /var/lib/forgeiq-sbx/slots
sudo chown -R $USER /var/lib/forgeiq-sbx
```

You do **not** need pre-exported rootfs when using Docker mode; the worker uses images:
- Go image: `golang:1.22-bookworm`
- Python image: `python:3.12-bookworm`

### 5) Run sandbox-worker inside VM

From host (recommended), build a Linux binary and copy it into VM:

```bash
cd /Users/swastiksabat/Documents/agentic-app/forgeiq
GOOS=linux GOARCH=amd64 go build -o sandbox-worker ./cmd/sandbox-worker
multipass transfer ./sandbox-worker forgeiq-sbx:/home/ubuntu/sandbox-worker
```

Inside VM:

```bash
chmod +x ./sandbox-worker
export PORT=8091
export SANDBOX_BASE_DIR=/var/lib/forgeiq-sbx
export SANDBOX_SLOTS=2
export SANDBOX_MODE=docker
export DOCKER_BIN=docker
export DOCKER_RUNTIME=runsc
export DOCKER_IMAGE_GO=golang:1.22-bookworm
export DOCKER_IMAGE_PY=python:3.12-bookworm
./sandbox-worker
```

### 6) Point the PR agent at the sandbox-worker

On host (where `control-pr-agent` runs):

```bash
export SANDBOX_WORKER_URL="http://localhost:8091"
# Optional (best for local dev): tell the worker to use your mounted repo path (git archive / rsync)
export SANDBOX_REPO_PATH="/host/forgeiq"
```

If the worker runs inside VM, expose port (Multipass forwards localhost by default for bridged; if not, use the VM IP).

### 7) Trigger sandbox verification from PR agent

Send PR review request with:
- `"verify_sandbox": true`

If sandbox is configured correctly, the PR agent response will include:
- `payload.meta.sandbox`
- and a high-severity finding if verification fails.

### Notes

- **Commit checkout**: with `SANDBOX_REPO_PATH` set, the worker prefers `git archive <sha>` into the workspace (no “git checkout” on your mounted repo). It falls back to rsync if archive fails.
- **Old runsc-bundle mode**: still supported via `SANDBOX_MODE=runsc` + `ROOTFS_GO/ROOTFS_PY`, but Docker mode is simpler if you already have Docker+runsc.

