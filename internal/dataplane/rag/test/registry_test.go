package dataplanerag_test

import (
	"forgeiq/internal/dataplane/rag"
	"testing"
)

func TestLoadRegistryFromJSON_EmptyOK(t *testing.T) {
	r, err := rag.LoadRegistryFromJSON("")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(r.All()) != 0 {
		t.Fatalf("expected empty registry")
	}
}

func TestLoadRegistryFromJSON_InvalidJSON(t *testing.T) {
	_, err := rag.LoadRegistryFromJSON("{bad")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoadRegistryFromJSON_InternalExternal(t *testing.T) {
	js := `[
	  {"id":"internal_docs","type":"internal","name":"Internal","description":"x","internal":{"namespace":"ns"}},
	  {"id":"external_logs","type":"external","name":"External","description":"y","external":{"adapter":"elastic","index":"logs-*"}}
	]`
	r, err := rag.LoadRegistryFromJSON(js)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := r.Get("internal_docs"); !ok {
		t.Fatalf("expected internal_docs")
	}
	if _, ok := r.Get("external_logs"); !ok {
		t.Fatalf("expected external_logs")
	}
	_, err = r.List([]string{"internal_docs", "external_logs"})
	if err != nil {
		t.Fatalf("expected list ok, got %v", err)
	}
}
