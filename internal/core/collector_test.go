package core

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
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
	// mutex guards requests: the tag changes call the collector from
	// several goroutines.
	mutex sync.Mutex
}

type fakeRequest struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
}

func (f *fakeCollector) GetJSON(_ context.Context, path string, query url.Values, output any) error {
	f.record(fakeRequest{method: "GET", path: path, query: query})
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

func (f *fakeCollector) PostJSON(_ context.Context, path string, input any, output any) error {
	payload, _ := json.Marshal(input)
	var body map[string]any
	_ = json.Unmarshal(payload, &body)
	f.record(fakeRequest{method: "POST", path: path, body: body})
	if f.err != nil {
		return f.err
	}
	return f.reply(path, nil, output)
}

func (f *fakeCollector) PutJSON(_ context.Context, path string, input any, output any) error {
	payload, _ := json.Marshal(input)
	var body map[string]any
	_ = json.Unmarshal(payload, &body)
	f.record(fakeRequest{method: "PUT", path: path, body: body})
	if f.err != nil {
		return f.err
	}
	return f.reply(path, nil, output)
}

func (f *fakeCollector) DeleteJSON(_ context.Context, path string, output any) error {
	f.record(fakeRequest{method: "DELETE", path: path})
	if f.err != nil {
		return f.err
	}
	return f.reply(path, nil, output)
}

func (f *fakeCollector) record(request fakeRequest) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.requests = append(f.requests, request)
}

// reply answers a request of the given path as GetJSON does.
func (f *fakeCollector) reply(path string, query url.Values, output any) error {
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
