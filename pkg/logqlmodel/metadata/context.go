/*
Package metadata provides primitives for recording metadata across the query path.
Metadata is passed through the query context.
*/
package metadata

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"
)

type (
	ctxKeyType string
)

const (
	metadataKey ctxKeyType = "metadata"
)

var (
	ErrNoCtxData = errors.New("unable to add headers to context: no existing context data")
)

// Context is the metadata context. It is passed through the query path and accumulates metadata.
type Context struct {
	mtx     sync.Mutex
	headers map[string][]string
}

// NewContext creates a new metadata context
func NewContext(ctx context.Context) (*Context, context.Context) {
	contextData := &Context{
		headers: map[string][]string{},
	}
	ctx = context.WithValue(ctx, metadataKey, contextData)
	return contextData, ctx
}

// FromContext returns the metadata context.
func FromContext(ctx context.Context) *Context {
	v, ok := ctx.Value(metadataKey).(*Context)
	if !ok {
		return &Context{
			headers: map[string][]string{},
		}
	}
	return v
}

// Headers returns the cache headers accumulated in the context so far.
func (c *Context) Headers() []*definitions.PrometheusResponseHeader {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	headers := make([]*definitions.PrometheusResponseHeader, 0, len(c.headers))
	for k, vs := range c.headers {
		header := definitions.PrometheusResponseHeader{
			Name:   k,
			Values: vs,
		}
		headers = append(headers, &header)
	}

	sort.Slice(headers, func(i, j int) bool {
		return headers[i].Name < headers[j].Name
	})

	return headers
}

// JoinHeaders merges a Headers with the embedded Headers in a context in a concurrency-safe manner.
// JoinHeaders will consolidate all distinct headers but will override same-named headers in an
// undefined way
func JoinHeaders(ctx context.Context, headers []*definitions.PrometheusResponseHeader) error {
	context, ok := ctx.Value(metadataKey).(*Context)
	if !ok {
		return ErrNoCtxData
	}

	context.mtx.Lock()
	defer context.mtx.Unlock()

	ExtendHeaders(context.headers, headers)

	return nil
}

func ExtendHeaders(dst map[string][]string, src []*definitions.PrometheusResponseHeader) {
	for _, header := range src {
		dst[header.Name] = header.Values
	}
}
