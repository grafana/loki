package cache

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/golang/snappy"
)

type snappyCache struct {
	next   Cache
	logger log.Logger
}

// NewSnappy makes a new snappy encoding cache wrapper.
func NewSnappy(next Cache, logger log.Logger) Cache {
	return &snappyCache{
		next:   next,
		logger: logger,
	}
}

func (s *snappyCache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	cs := make([][]byte, 0, len(bufs))
	for _, buf := range bufs {
		c := snappy.Encode(nil, buf)
		cs = append(cs, c)
	}
	s.next.Store(ctx, keys, cs)
}

func (s *snappyCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string) {
	found, bufs, missing := s.next.Fetch(ctx, keys)
	ds := make([][]byte, 0, len(bufs))
	for _, buf := range bufs {
		d, err := snappy.Decode(nil, buf)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to decode cache entry", "err", err)
			return nil, nil, keys
		}
		ds = append(ds, d)
	}
	return found, ds, missing
}

func (s *snappyCache) Stop() {
	s.next.Stop()
}
