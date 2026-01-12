package main

import (
	"testing"
	"time"
)

func TestParsePrometheusSignalsRange_SetsValueToLast(t *testing.T) {
	raw := map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "matrix",
			"result": []any{
				map[string]any{
					"values": []any{
						[]any{1.0, "1"},
						[]any{2.0, "3"},
					},
				},
			},
		},
	}

	s := parsePrometheusSignalsRange(raw, true)
	if s["last"] != 3 {
		t.Fatalf("expected last=3, got %v", s["last"])
	}
	if s["value"] != s["last"] {
		t.Fatalf("expected value==last, got value=%v last=%v", s["value"], s["last"])
	}
	if _, ok := s["min"]; !ok {
		t.Fatalf("expected min")
	}
	if _, ok := s["avg"]; !ok {
		t.Fatalf("expected avg")
	}
}

func TestParseLokiSignals_StreamsCountsLines(t *testing.T) {
	raw := map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "streams",
			"result": []any{
				map[string]any{
					"values": []any{
						[]any{"1", "line1"},
						[]any{"2", "line2"},
					},
				},
				map[string]any{
					"values": []any{
						[]any{"3", "line3"},
					},
				},
			},
		},
	}

	s := parseLokiSignals(raw, time.Time{}, time.Time{}, false)
	// can't pass time.Time without importing; we only care about counts here.
	if s["streams"] != 2 {
		t.Fatalf("expected streams=2 got %v", s["streams"])
	}
	if s["lines"] != 3 {
		t.Fatalf("expected lines=3 got %v", s["lines"])
	}
	if s["value"] != 3 {
		t.Fatalf("expected value=3 got %v", s["value"])
	}
}
