package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode represents error types
type ErrorCode string

const (
	ErrCodeInvalidInput     ErrorCode = "INVALID_INPUT"
	ErrCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden        ErrorCode = "FORBIDDEN"
	ErrCodeInternal         ErrorCode = "INTERNAL_ERROR"
	ErrCodeExternalService  ErrorCode = "EXTERNAL_SERVICE_ERROR"
	ErrCodeTimeout          ErrorCode = "TIMEOUT"
	ErrCodePolicyDenied     ErrorCode = "POLICY_DENIED"
	ErrCodeWorkflowFailed   ErrorCode = "WORKFLOW_FAILED"
)

// AppError represents an application error
type AppError struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	Details    string    `json:"details,omitempty"`
	HTTPStatus int       `json:"-"`
	Err        error     `json:"-"`
}

func (e *AppError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// NewError creates a new application error
func NewError(code ErrorCode, message string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

// NewErrorWithDetails creates a new error with details
func NewErrorWithDetails(code ErrorCode, message, details string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		Details:    details,
		HTTPStatus: httpStatus,
	}
}

// WrapError wraps an existing error
func WrapError(err error, code ErrorCode, message string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Err:        err,
	}
}

// Predefined errors
var (
	ErrInvalidInput = NewError(ErrCodeInvalidInput, "Invalid input", http.StatusBadRequest)
	ErrNotFound     = NewError(ErrCodeNotFound, "Resource not found", http.StatusNotFound)
	ErrUnauthorized = NewError(ErrCodeUnauthorized, "Unauthorized", http.StatusUnauthorized)
	ErrForbidden    = NewError(ErrCodeForbidden, "Forbidden", http.StatusForbidden)
	ErrInternal     = NewError(ErrCodeInternal, "Internal server error", http.StatusInternalServerError)
	ErrTimeout      = NewError(ErrCodeTimeout, "Request timeout", http.StatusRequestTimeout)
	ErrPolicyDenied = NewError(ErrCodePolicyDenied, "Policy denied", http.StatusForbidden)
)

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		switch appErr.Code {
		case ErrCodeTimeout, ErrCodeExternalService:
			return true
		}
	}
	return false
}

// GetHTTPStatus returns HTTP status code for an error
func GetHTTPStatus(err error) int {
	if appErr, ok := err.(*AppError); ok {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}


