package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrIncompatible marks an answer of the collector this MCP cannot use because
// the API of the collector is not the one it was built for: a route the
// collector does not know, a response of another shape, another content type.
// A collector upgraded after the MCP answers so: its routes moved on.
// errors.Is finds it under the errors the tools wrap.
var ErrIncompatible = errors.New("collector_incompatible")

// IncompatibleError is an answer of the collector this MCP cannot use, the
// request it answered and what in the answer was not expected.
type IncompatibleError struct {
	Method string
	Path   string
	Reason string
}

func (e *IncompatibleError) Error() string {
	return fmt.Sprintf("OpenSVC collector %s %s: %s", e.Method, e.Path, e.Reason)
}

func (e *IncompatibleError) Is(target error) bool { return target == ErrIncompatible }

// decodeError is the error of a JSON answer that could not be read: one of
// another shape than the one this MCP reads is incompatible.
func decodeError(method, path string, err error) error {
	var syntax *json.SyntaxError
	var unmarshal *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &unmarshal) {
		return &IncompatibleError{Method: method, Path: path, Reason: "the answer is not in the form this MCP reads: " + err.Error()}
	}
	return fmt.Errorf("decode OpenSVC collector %s response: %w", path, err)
}

// responseError is the error of a non-success answer. A 404 or a 405 of the
// router, which answers {"message": ...} and no problem, means the collector
// knows no such route, or not with this method: its API moved on. A 404 of a
// handler, a problem {"text": ...}, is an item not found, which the tools
// read as such.
func responseError(method, path string, response *http.Response) error {
	apiError := newAPIError(method, path, response)
	if (apiError.StatusCode == http.StatusNotFound || apiError.StatusCode == http.StatusMethodNotAllowed) && apiError.routed {
		reason := "the collector knows no such route"
		if apiError.StatusCode == http.StatusMethodNotAllowed {
			reason = "the collector does not take this method on this route"
		}
		return &IncompatibleError{Method: method, Path: path, Reason: reason}
	}
	return apiError
}
