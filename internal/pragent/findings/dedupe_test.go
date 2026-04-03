package findings

import (
	"testing"

	"forgeiq/internal/pragent"
)

func TestDedupeAndCap(t *testing.T) {
	f1 := pragent.Finding{RuleID: "R1", Severity: pragent.SeverityHigh, Path: "a.go", Line: 10, Message: "bad"}
	f2 := pragent.Finding{RuleID: "R1", Severity: pragent.SeverityLow, Path: "a.go", Line: 10, Message: "bad"} // dup lower sev
	f3 := pragent.Finding{RuleID: "R2", Severity: pragent.SeverityMedium, Path: "a.go", Line: 11, Message: "meh"}
	f4 := pragent.Finding{RuleID: "R3", Severity: pragent.SeverityNit, Path: "b.go", Line: 1, Message: "nit"}

	out := DedupeAndCap([]pragent.Finding{f2, f1, f3, f4}, 0, 0)
	if len(out) != 3 {
		t.Fatalf("expected 3 findings after dedupe, got %d", len(out))
	}
	// First should be highest severity
	if out[0].Severity != pragent.SeverityHigh {
		t.Fatalf("expected first severity high, got %s", out[0].Severity)
	}

	// Cap per file
	out2 := DedupeAndCap([]pragent.Finding{f1, f3, f4}, 0, 1)
	// a.go should be capped to 1, b.go remains 1 => total 2
	if len(out2) != 2 {
		t.Fatalf("expected 2 after per-file cap, got %d", len(out2))
	}
}

