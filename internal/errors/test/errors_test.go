package errors_test

import (
	stderrors "errors"
	"net/http"
	"testing"

	"forgeiq/internal/errors"
)

func TestAppError_ErrorAndUnwrap(t *testing.T) {
	inner := stderrors.New("inner")
	e := errors.WrapError(inner, errors.ErrCodeTimeout, "msg", http.StatusRequestTimeout)
	if e.Error() == "" {
		t.Fatalf("expected string")
	}
	if !stderrors.Is(e, inner) {
		t.Fatalf("expected unwrap to work")
	}
}

func TestIsRetryable(t *testing.T) {
	if !errors.IsRetryable(errors.NewError(errors.ErrCodeTimeout, "t", http.StatusRequestTimeout)) {
		t.Fatalf("timeout should be retryable")
	}
	if errors.IsRetryable(errors.NewError(errors.ErrCodeInvalidInput, "x", http.StatusBadRequest)) {
		t.Fatalf("invalid input should not be retryable")
	}
}

func TestGetHTTPStatus(t *testing.T) {
	if errors.GetHTTPStatus(errors.ErrForbidden) != http.StatusForbidden {
		t.Fatalf("expected forbidden")
	}
	if errors.GetHTTPStatus(stderrors.New("x")) != http.StatusInternalServerError {
		t.Fatalf("expected 500")
	}
}
