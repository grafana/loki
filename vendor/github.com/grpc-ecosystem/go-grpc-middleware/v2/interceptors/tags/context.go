package tags

import (
	"context"
)

type ctxMarker struct{}

var (
	// ctxMarkerKey is the Context value marker used by *all* middlewares that supports unique fields e.g tracing and logging.
	ctxMarkerKey = &ctxMarker{}
	// NoopTags is a trivial, minimum overhead implementation of Tags for which all operations are no-ops.
	NoopTags = &noopTags{}
)

// Tags is the interface used for storing request tags between Context calls.
// The default implementation is *not* thread safe, and should be handled only in the context of the request.
type Tags interface {
	// Set sets the given key in the metadata tags.
	Set(key string, value string) Tags
	// Has checks if the given key exists.
	Has(key string) bool
	// Values returns a map of key to values.
	// Do not modify the underlying map, use Set instead.
	Values() map[string]string
}

type mapTags struct {
	values map[string]string
}

func (t *mapTags) Set(key string, value string) Tags {
	t.values[key] = value
	return t
}

func (t *mapTags) Has(key string) bool {
	_, ok := t.values[key]
	return ok
}

func (t *mapTags) Values() map[string]string {
	return t.values
}

type noopTags struct{}

func (t *noopTags) Set(key string, value string) Tags { return t }

func (t *noopTags) Has(key string) bool { return false }

func (t *noopTags) Values() map[string]string { return nil }

// Extracts returns a pre-existing Tags object in the Context.
// If the context wasn't set in a tag interceptor, a no-op Tag storage is returned that will *not* be propagated in context.
func Extract(ctx context.Context) Tags {
	t, ok := ctx.Value(ctxMarkerKey).(Tags)
	if !ok {
		return NoopTags
	}

	return t
}

// extractOrCreate returns a pre-existing Tags object in the Context.
// If the context wasn't set in a tag interceptor, a new Tag (map) storage is returned that will be propagated in context.
func extractOrCreate(ctx context.Context) Tags {
	t, ok := ctx.Value(ctxMarkerKey).(Tags)
	if !ok {
		return NewTags()
	}

	return t
}

func SetInContext(ctx context.Context, tags Tags) context.Context {
	return context.WithValue(ctx, ctxMarkerKey, tags)
}

func NewTags() Tags {
	return &mapTags{values: make(map[string]string)}
}
