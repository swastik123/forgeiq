# DSPy Reasoning Service (stub)

This service is a **production-friendly seam** for integrating [DSPy](https://github.com/stanfordnlp/dspy) into ForgeIQ:

- Go control-plane / agents call this service for **model decisions** (final answer vs tool call)
- The service can later be upgraded to run **DSPy programs**, teleprompting, optimization loops, etc.

## API

- `GET /health`
- `POST /v1/decide` → returns a ForgeIQ `ModelDecision`-compatible payload

## Run locally

```bash
cd services/dspy-service
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
uvicorn app.main:app --host 0.0.0.0 --port 8099
```





