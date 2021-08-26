/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache/internal"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	logf "sigs.k8s.io/controller-runtime/pkg/internal/log"
)

var log = logf.RuntimeLog.WithName("object-cache")

// Cache knows how to load Kubernetes objects, fetch informers to request
// to receive events for Kubernetes objects (at a low-level),
// and add indices to fields on the objects stored in the cache.
type Cache interface {
	// Cache acts as a client to objects stored in the cache.
	client.Reader

	// Cache loads informers and adds field indices.
	Informers
}

// Informers knows how to create or fetch informers for different
// group-version-kinds, and add indices to those informers.  It's safe to call
// GetInformer from multiple threads.
type Informers interface {
	// GetInformer fetches or constructs an informer for the given object that corresponds to a single
	// API kind and resource.
	GetInformer(ctx context.Context, obj client.Object) (Informer, error)

	// GetInformerForKind is similar to GetInformer, except that it takes a group-version-kind, instead
	// of the underlying object.
	GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind) (Informer, error)

	// Start runs all the informers known to this cache until the context is closed.
	// It blocks.
	Start(ctx context.Context) error

	// WaitForCacheSync waits for all the caches to sync.  Returns false if it could not sync a cache.
	WaitForCacheSync(ctx context.Context) bool

	// Informers knows how to add indices to the caches (informers) that it manages.
	client.FieldIndexer
}

// Informer - informer allows you interact with the underlying informer.
type Informer interface {
	// AddEventHandler adds an event handler to the shared informer using the shared informer's resync
	// period.  Events to a single handler are delivered sequentially, but there is no coordination
	// between different handlers.
	AddEventHandler(handler toolscache.ResourceEventHandler)
	// AddEventHandlerWithResyncPeriod adds an event handler to the shared informer using the
	// specified resync period.  Events to a single handler are delivered sequentially, but there is
	// no coordination between different handlers.
	AddEventHandlerWithResyncPeriod(handler toolscache.ResourceEventHandler, resyncPeriod time.Duration)
	// AddIndexers adds more indexers to this store.  If you call this after you already have data
	// in the store, the results are undefined.
	AddIndexers(indexers toolscache.Indexers) error
	// HasSynced return true if the informers underlying store has synced.
	HasSynced() bool
}

// SelectorsByObject associate a client.Object's GVK to a field/label selector.
type SelectorsByObject map[client.Object]internal.Selector

// Options are the optional arguments for creating a new InformersMap object.
type Options struct {
	// Scheme is the scheme to use for mapping objects to GroupVersionKinds
	Scheme *runtime.Scheme

	// Mapper is the RESTMapper to use for mapping GroupVersionKinds to Resources
	Mapper meta.RESTMapper

	// Resync is the base frequency the informers are resynced.
	// Defaults to defaultResyncTime.
	// A 10 percent jitter will be added to the Resync period between informers
	// So that all informers will not send list requests simultaneously.
	Resync *time.Duration

	// Namespace restricts the cache's ListWatch to the desired namespace
	// Default watches all namespaces
	Namespace string

	// SelectorsByObject restricts the cache's ListWatch to the desired
	// fields per GVK at the specified object, the map's value must implement
	// Selector [1] using for example a Set [2]
	// [1] https://pkg.go.dev/k8s.io/apimachinery/pkg/fields#Selector
	// [2] https://pkg.go.dev/k8s.io/apimachinery/pkg/fields#Set
	SelectorsByObject SelectorsByObject
}

var defaultResyncTime = 10 * time.Hour

// New initializes and returns a new Cache.
func New(config *rest.Config, opts Options) (Cache, error) {
	opts, err := defaultOpts(config, opts)
	if err != nil {
		return nil, err
	}
	selectorsByGVK, err := convertToSelectorsByGVK(opts.SelectorsByObject, opts.Scheme)
	if err != nil {
		return nil, err
	}
	im := internal.NewInformersMap(config, opts.Scheme, opts.Mapper, *opts.Resync, opts.Namespace, selectorsByGVK)
	return &informerCache{InformersMap: im}, nil
}

// BuilderWithOptions returns a Cache constructor that will build the a cache
// honoring the options argument, this is useful to specify options like
// SelectorsByObject
// WARNING: if SelectorsByObject is specified. filtered out resources are not
//          returned.
func BuilderWithOptions(options Options) NewCacheFunc {
	return func(config *rest.Config, opts Options) (Cache, error) {
		if opts.Scheme == nil {
			opts.Scheme = options.Scheme
		}
		if opts.Mapper == nil {
			opts.Mapper = options.Mapper
		}
		if opts.Resync == nil {
			opts.Resync = options.Resync
		}
		if opts.Namespace == "" {
			opts.Namespace = options.Namespace
		}
		opts.SelectorsByObject = options.SelectorsByObject
		return New(config, opts)
	}
}

func defaultOpts(config *rest.Config, opts Options) (Options, error) {
	// Use the default Kubernetes Scheme if unset
	if opts.Scheme == nil {
		opts.Scheme = scheme.Scheme
	}

	// Construct a new Mapper if unset
	if opts.Mapper == nil {
		var err error
		opts.Mapper, err = apiutil.NewDiscoveryRESTMapper(config)
		if err != nil {
			log.WithName("setup").Error(err, "Failed to get API Group-Resources")
			return opts, fmt.Errorf("could not create RESTMapper from config")
		}
	}

	// Default the resync period to 10 hours if unset
	if opts.Resync == nil {
		opts.Resync = &defaultResyncTime
	}
	return opts, nil
}

func convertToSelectorsByGVK(selectorsByObject SelectorsByObject, scheme *runtime.Scheme) (internal.SelectorsByGVK, error) {
	selectorsByGVK := internal.SelectorsByGVK{}
	for object, selector := range selectorsByObject {
		gvk, err := apiutil.GVKForObject(object, scheme)
		if err != nil {
			return nil, err
		}
		selectorsByGVK[gvk] = selector
	}
	return selectorsByGVK, nil
}
