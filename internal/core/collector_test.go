package core

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

// fakeCollector answers the GetJSON calls of the core with a fixed body and
// records the requests.
type fakeCollector struct {
	body string
	// bodies answers a path with its own body instead of body.
	bodies map[string]string
	// answer, when set, answers instead of bodies and body.
	answer   func(path string, query url.Values) string
	err      error
	requests []fakeRequest
}

type fakeRequest struct {
	path  string
	query url.Values
}

func (f *fakeCollector) GetJSON(_ context.Context, path string, query url.Values, output any) error {
	f.requests = append(f.requests, fakeRequest{path: path, query: query})
	if f.err != nil {
		return f.err
	}
	if f.answer != nil {
		return json.Unmarshal([]byte(f.answer(path, query)), output)
	}
	if body, ok := f.bodies[path]; ok {
		return json.Unmarshal([]byte(body), output)
	}
	return json.Unmarshal([]byte(f.body), output)
}

func newTestService(collector *fakeCollector) *Service {
	service := New(collector)
	service.now = func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) }
	return service
}
