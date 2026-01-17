from __future__ import annotations

from typing import Any, Dict, List, Optional

from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel, Field


app = FastAPI(title="ForgeIQ DSPy Reasoning Service", version="0.1")


class Message(BaseModel):
    role: str
    content: str = ""
    name: Optional[str] = None
    data: Optional[Dict[str, Any]] = None


class ConversationState(BaseModel):
    thread_id: Optional[str] = None
    messages: List[Message]


class ToolInfo(BaseModel):
    name: str
    version: str
    description: Optional[str] = None
    schema: Optional[Dict[str, Any]] = None


class ModelConfig(BaseModel):
    provider: Optional[str] = None
    model: Optional[str] = None
    temperature: Optional[float] = None


class ToolCall(BaseModel):
    name: str
    version: str = "v1"
    args: Dict[str, Any] = Field(default_factory=dict)


class DecideRequest(BaseModel):
    state: ConversationState
    tools: List[ToolInfo] = Field(default_factory=list)
    model: ModelConfig = Field(default_factory=ModelConfig)
    meta: Dict[str, Any] = Field(default_factory=dict)
    # Future:
    program_id: Optional[str] = None
    program_version: Optional[str] = None


class DecideResponse(BaseModel):
    final_answer: Optional[str] = None
    tool_call: Optional[ToolCall] = None
    summary: Optional[str] = None


@app.get("/health")
def health() -> Dict[str, str]:
    return {"status": "ok"}


@app.post("/v1/decide")
def decide(req: DecideRequest, authorization: Optional[str] = Header(default=None)) -> DecideResponse:
    # Optional shared-secret auth
    expected = (req.meta or {}).get("expected_api_key")
    if expected:
        if not authorization or not authorization.lower().startswith("bearer "):
            raise HTTPException(status_code=401, detail="missing bearer token")
        token = authorization.split(" ", 1)[1].strip()
        if token != expected:
            raise HTTPException(status_code=403, detail="invalid token")

    # Stub behavior:
    # - If last user message contains "call_tool:<toolname>", emit a tool call (if available)
    # - Else return a simple final answer.
    last_user = ""
    for m in reversed(req.state.messages):
        if m.role == "user" and (m.content or "").strip():
            last_user = m.content.strip()
            break

    if last_user.lower().startswith("call_tool:"):
        toolname = last_user.split(":", 1)[1].strip()
        for t in req.tools:
            if t.name == toolname:
                return DecideResponse(
                    tool_call=ToolCall(name=t.name, version=t.version, args={}),
                    summary=f"Requested tool call to {t.name}",
                )
        raise HTTPException(status_code=400, detail=f"unknown tool: {toolname}")

    return DecideResponse(
        final_answer=f"[DSPy stub] I received {len(req.state.messages)} messages and {len(req.tools)} tools.",
        summary="Stub decision (replace with DSPy program execution).",
    )





