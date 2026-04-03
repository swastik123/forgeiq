## MCP `llm.chat:v2` provider backends (LiteLLM / OpenAI / Anthropic)

`llm.chat:v2` is the recommended interface for production because it supports:
- typed `result` object output
- schema validation + retries
- provider routing behind MCP

### Environment variables (data-mcp-server)

#### Choose provider
- `LLM_PROVIDER`: `demo` (default) | `litellm` | `openai` | `anthropic`

#### LiteLLM (recommended)
- `LITELLM_BASE_URL`: e.g. `http://litellm:4000` (must expose OpenAI-compatible `/chat/completions`)
- `LITELLM_API_KEY`: optional (if your LiteLLM requires it)

#### OpenAI (direct)
- `OPENAI_BASE_URL`: default `https://api.openai.com/v1`
- `OPENAI_API_KEY`: required for direct OpenAI

#### Anthropic (direct)
- `ANTHROPIC_BASE_URL`: default `https://api.anthropic.com`
- `ANTHROPIC_API_KEY`: required
- `ANTHROPIC_VERSION`: default `2023-06-01`

### Request mapping

`llm.chat:v2` accepts:
- `messages`: OpenAI-style messages `{role, content}`
- `model.model`: model name string (optional)
- `response_format`: passed through to OpenAI-compatible providers when possible

Notes:
- Some providers may ignore/deny `response_format`. MCP still enforces schema locally and retries.

### Fallback behavior

If:
- `LLM_PROVIDER` is unset/unknown, or
- provider call fails, or
- output cannot be parsed into an object,

MCP falls back to the existing deterministic demo behavior so local dev still works.

