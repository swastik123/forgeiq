#!/usr/bin/env bash
set -euo pipefail

MCP_URL="${MCP_URL:-http://localhost:8090/rpc}"
ELASTIC_INDEX="${ELASTIC_INDEX:-logs-forgeiq}"

echo "Using MCP_URL=$MCP_URL"
echo "Using ELASTIC_INDEX=$ELASTIC_INDEX"
echo

echo "1) tools.list"
curl -fsS "$MCP_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":"1","method":"tools.list"}' | jq .
echo

echo "2) logs.ingest (into Elasticsearch) - creating a doc you can then search"
curl -fsS "$MCP_URL" \
  -H 'Content-Type: application/json' \
  -d @- <<JSON | jq .
{
  "jsonrpc": "2.0",
  "id": "2",
  "method": "tools.call",
  "params": {
    "name": "logs.ingest",
    "version": "v1",
    "args": {
      "index": "${ELASTIC_INDEX}",
      "doc": {
        "@timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
        "level": "ERROR",
        "service": "payments",
        "message": "synthetic error for mcp elastic test"
      }
    }
  }
}
JSON
echo

echo "3) logs.search (from Elasticsearch)"
curl -fsS "$MCP_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":"3","method":"tools.call","params":{"name":"logs.search","version":"v1","args":{"query":"payments AND synthetic","limit":5}}}' | jq .
echo

echo "4) vectors.upsert (pgvector) - NOTE: docker-compose sets VECTOR_DIM=8 by default, so embedding must be length 8"
curl -fsS "$MCP_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":"4","method":"tools.call","params":{"name":"vectors.upsert","version":"v1","args":{"id":"doc-1","namespace":"default","content":"hello vector world","metadata":{"source":"test"},"embedding":[0.1,0.1,0.1,0.1,0.1,0.1,0.1,0.1]}}}' | jq .
echo

echo "5) vectors.search (pgvector)"
curl -fsS "$MCP_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":"5","method":"tools.call","params":{"name":"vectors.search","version":"v1","args":{"namespace":"default","top_k":5,"query_embedding":[0.1,0.1,0.1,0.1,0.1,0.1,0.1,0.1]}}}' | jq .
echo

echo "6) rag.single_retrieve (router chooses one dataset; internal vs external depends on dataset type)"
curl -fsS "$MCP_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":"6","method":"tools.call","params":{"name":"rag.single_retrieve","version":"v1","args":{"query":"payments synthetic error","available_datasets":["internal_docs","external_logs"],"router_mode":"rule_based","top_k":5}}}' | jq .
echo

echo "7) rag.multiple_retrieve (queries all datasets, merges + reranks)"
curl -fsS "$MCP_URL" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":"7","method":"tools.call","params":{"name":"rag.multiple_retrieve","version":"v1","args":{"query":"payments synthetic error","available_datasets":["internal_docs","external_logs"],"top_k":5,"score_threshold":0,"reranking_mode":"weighted_score","weights":{"internal":1.0,"external":1.0}}}}' | jq .
echo

echo "Done."


