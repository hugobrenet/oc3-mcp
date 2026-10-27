package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
)

const (
	maxErrorResponseBodySize = 64 << 10
	maxErrorTextRunes        = 2048
)

// APIError describes a non-success response from the collector API.
// StatusCode and Status come from the HTTP response; Text is the bounded
// message of the collector problem body, {"text": "..."}, and Index the
// position of the entry it refuses in a list the request sent, when it
// names one.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Status     string
	Text       string
	Index      *int
}

func (e *APIError) Error() string {
	status := e.Status
	if status == "" {
		status = strings.TrimSpace(fmt.Sprintf("%d %s", e.StatusCode, http.StatusText(e.StatusCode)))
	}
	message := fmt.Sprintf("OpenSVC collector %s %s returned HTTP %s", e.Method, e.Path, status)
	if e.Text != "" {
		return message + ": " + e.Text
	}
	return message
}

// HTTPStatus is the HTTP status the collector answered with.
func (e *APIError) HTTPStatus() int { return e.StatusCode }

// Message is the message of the collector problem body, empty when none.
func (e *APIError) Message() string { return e.Text }

// NotFound tells whether the collector found no such item, for the core to
// suggest a search.
func (e *APIError) NotFound() bool {
	return e.StatusCode == http.StatusNotFound
}

func newAPIError(method string, path string, response *http.Response) *APIError {
	apiError := &APIError{Method: method, Path: path, StatusCode: response.StatusCode, Status: response.Status}
	if response.StatusCode == http.StatusUnauthorized {
		// The collector lists its authentication strategies here: the only
		// useful fact is that it refused the caller's access token.
		apiError.Text = "the collector refused the access token of the caller"
		return apiError
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorResponseBodySize+1))
	if err != nil || len(body) > maxErrorResponseBodySize {
		return apiError
	}
	var problem struct {
		Text  string `json:"text"`
		Index *int   `json:"index"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		return apiError
	}
	apiError.Text = normalizeProblemText(problem.Text, maxErrorTextRunes)
	if problem.Index != nil && *problem.Index >= 0 {
		apiError.Index = problem.Index
	}
	return apiError
}

func normalizeProblemText(value string, maxRunes int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}
