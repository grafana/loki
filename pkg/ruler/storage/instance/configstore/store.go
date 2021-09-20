// Package configstore abstracts the concepts of where instance files get
// retrieved.
package configstore

import (
	"context"

	"github.com/grafana/agent/pkg/metrics/instance"
)

// Store is some interface to retrieving instance configurations.
type Store interface {
	// List gets the list of config names.
	List(ctx context.Context) ([]string, error)

	// Get gets an individual config by name.
	Get(ctx context.Context, key string) (instance.Config, error)

	// Put applies a new instance Config to the store.
	// If the config already exists, created will be false to indicate an
	// update.
	Put(ctx context.Context, c instance.Config) (created bool, err error)

	// Delete deletes a config from the store.
	Delete(ctx context.Context, key string) error

	// All retrieves the entire list of instance configs currently
	// in the store. A filtering "keep" function can be provided to ignore some
	// configs, which can significantly speed up the operation in some cases.
	All(ctx context.Context, keep func(key string) bool) (<-chan instance.Config, error)

	// Watch watches for changed instance Configs.
	// All callers of Watch receive the same Channel.
	//
	// It is not guaranteed that Watch will emit all store events, and Watch
	// should only be used for best-effort quick convergence with the remote
	// store. Watch should always be paired with polling All.
	Watch() <-chan WatchEvent

	// Close closes the store.
	Close() error
}

// WatchEvent is returned by Watch. The Key is the name of the config that was
// added, updated, or deleted. If the Config was deleted, Config will be nil.
type WatchEvent struct {
	Key    string
	Config *instance.Config
}
