package core

import (
	"context"
	"net/url"
	"time"
)

// CollectorClient is the collector API the core uses. PostJSON, PutJSON
// and DeleteJSON change the collector state: only the action use cases call
// them.
type CollectorClient interface {
	GetJSON(context.Context, string, url.Values, any) error
	PostJSON(context.Context, string, any, any) error
	PutJSON(context.Context, string, any, any) error
	DeleteJSON(context.Context, string, any) error
}

// Service holds the collector use cases. It reads the collector through
// client and never holds caller credentials.
type Service struct {
	client CollectorClient
	now    func() time.Time
}

func New(client CollectorClient) *Service {
	return &Service{client: client, now: time.Now}
}
