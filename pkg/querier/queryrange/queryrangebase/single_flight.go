package queryrangebase

import (
	"context"

	"github.com/mailgun/groupcache/v2"

	"github.com/go-kit/log"
)

type singleFlightCache struct {
	logger log.Logger
	next   Handler
	codec  Codec
}

// NewSingleFlightMiddleware ...
func NewSingleFlightMiddleware(
	logger log.Logger,
	c Codec,
) (Middleware, error) {
	return MiddlewareFunc(func(next Handler) Handler {
		return &singleFlightCache{
			logger: logger,
			next:   next,
			codec:  c,
		}
	}), nil
}

func (s singleFlightCache) Do(ctx context.Context, r Request) (Response, error) {
	return nil, nil
}

func (s singleFlightCache) requestGetter(h Handler) groupcache.GetterFunc {
	return func(ctx context.Context, key string, dest groupcache.Sink) error {
		s.codec.DecodeRequest()
		return nil
	}
}
