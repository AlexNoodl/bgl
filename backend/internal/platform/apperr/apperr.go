package apperr

import (
	"context"
	"net/http"

	"bgl/internal/platform/httpx"

	"github.com/danielgtaylor/huma/v2"
)

type envelope struct {
	httpx.ErrorEnvelope
	status int
}

func (e *envelope) Error() string {
	return e.ErrorEnvelope.Error.Message
}

func (e *envelope) GetStatus() int {
	return e.status
}

func New(ctx context.Context, status int, code, message, field string) huma.StatusError {
	return &envelope{status: status, ErrorEnvelope: httpx.LogAndBuildError(ctx, status, code, message, field)}
}

func OverrideHumaErrors() {
	huma.NewError = func(status int, msg string, _ ...error) huma.StatusError {
		return &envelope{status: status, ErrorEnvelope: httpx.ErrorEnvelope{Error: httpx.ErrorBody{Message: msg}}}
	}
	huma.NewErrorWithContext = func(hctx huma.Context, status int, msg string, _ ...error) huma.StatusError {
		return New(hctx.Context(), status, codeForStatus(status), msg, "")
	}
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "VALIDATION_ERROR"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusMethodNotAllowed:
		return "METHOD_NOT_ALLOWED"
	case http.StatusServiceUnavailable:
		return "NOT_READY"
	case http.StatusInternalServerError:
		return "INTERNAL_ERROR"
	default:
		return "ERROR"
	}
}
