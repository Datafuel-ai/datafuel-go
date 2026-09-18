package datafuel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// APIError is a non-2xx answer from the API itself (bad key, no credits,
// invalid attributes, ...). A page that could not be scraped is not an
// APIError, see [TaskError].
type APIError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("datafuel: %s (%d %s)", e.Message, e.StatusCode, e.Code)
}

// Is lets errors.Is match the sentinels below: by Code when the sentinel has
// one, otherwise by HTTP status.
func (e *APIError) Is(target error) bool {
	t, ok := target.(*APIError)
	if !ok {
		return false
	}
	if t.Code != "" {
		return t.Code == e.Code
	}
	return t.StatusCode == e.StatusCode
}

// Sentinels for errors.Is.
var (
	ErrUnauthorized         error = &APIError{StatusCode: http.StatusUnauthorized}
	ErrNotFound             error = &APIError{StatusCode: http.StatusNotFound}
	ErrRateLimited          error = &APIError{StatusCode: http.StatusTooManyRequests}
	ErrInsufficientCredits  error = &APIError{Code: "INSUFFICIENT_CREDITS"}
	ErrInvalidAttributes    error = &APIError{Code: "INVALID_ATTRIBUTES"}
	ErrIdempotencyKeyReused error = &APIError{Code: "IDEMPOTENCY_KEY_REUSED"}
	// ErrModuleUnavailable and ErrEngineUnavailable: an operator switched the
	// task type or the LLM engine off; APIError.Message carries the reason.
	// Nothing was charged. They are never retried: see Client.Capabilities.
	ErrModuleUnavailable error = &APIError{Code: "MODULE_UNAVAILABLE"}
	ErrEngineUnavailable error = &APIError{Code: "ENGINE_UNAVAILABLE"}

	// ErrNoAPIKey is returned before any request when the client has no key.
	ErrNoAPIKey = errors.New("datafuel: no API key: pass one to New or set DATAFUEL_API_KEY")

	// ErrNilRequest is returned when a request argument is nil.
	ErrNilRequest = errors.New("datafuel: nil request")

	// ErrTaskFailed matches every [TaskError].
	ErrTaskFailed = errors.New("datafuel: task failed")
	// ErrBlocked matches a [TaskError] whose target refused or challenged the
	// request (403/429/503, anti-bot wall). The task was refunded; retry with
	// JSRendering or a Premium proxy.
	ErrBlocked = errors.New("datafuel: target blocked the request")
)

func newAPIError(status int, body []byte) *APIError {
	e := &APIError{StatusCode: status}
	if json.Unmarshal(body, e) != nil || e.Message == "" {
		e.Message = http.StatusText(status)
		if len(body) > 0 && len(body) <= 300 {
			e.Message = string(body)
		}
	}
	return e
}

// TaskError reports a task the API accepted but could not complete. Failed
// tasks are refunded. Result carries the envelope: StatusCode, Blocked,
// Protection, Error.
type TaskError struct {
	Result *Result
}

func (e *TaskError) Error() string {
	detail := e.Result.Error
	if detail == "" {
		detail = e.Result.Payload.ErrorDetail
	}
	if detail == "" {
		detail = "no detail"
	}
	return "datafuel: task failed: " + detail
}

func (e *TaskError) Is(target error) bool {
	return target == ErrTaskFailed || (target == ErrBlocked && e.Result.Blocked)
}

type transportError struct{ err error }

func (e *transportError) Error() string { return "datafuel: " + e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

func shouldRetry(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var te *transportError
	if errors.As(err, &te) {
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		// A deliberate switch-off is a 503 too, but retrying it only adds
		// load: it stays off until an operator turns it back on.
		if ae.Code == "MODULE_UNAVAILABLE" || ae.Code == "ENGINE_UNAVAILABLE" {
			return false
		}
		switch ae.StatusCode {
		case http.StatusTooManyRequests, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}
