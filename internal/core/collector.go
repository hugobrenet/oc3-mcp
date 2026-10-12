package core

import (
	"context"
	"net/url"
	"time"
)

type JSONGetter interface {
	GetJSON(context.Context, string, url.Values, any) error
}

// Service holds the collector use cases. It reads the collector through
// client and never holds caller credentials.
type Service struct {
	client JSONGetter
	now    func() time.Time
}

func New(client JSONGetter) *Service {
	return &Service{client: client, now: time.Now}
}
