/*
Copyright 2020 The Kubernetes Authors.

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

package cluster

import (
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClientBuilder builder is the interface for the client builder.
type ClientBuilder interface {
	// WithUncached takes a list of runtime objects (plain or lists) that users don't want to cache
	// for this client. This function can be called multiple times, it should append to an internal slice.
	WithUncached(objs ...client.Object) ClientBuilder

	// Build returns a new client.
	Build(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error)
}

// NewClientBuilder returns a builder to build new clients to be passed when creating a Manager.
func NewClientBuilder() ClientBuilder {
	return &newClientBuilder{}
}

type newClientBuilder struct {
	uncached []client.Object
}

func (n *newClientBuilder) WithUncached(objs ...client.Object) ClientBuilder {
	n.uncached = append(n.uncached, objs...)
	return n
}

func (n *newClientBuilder) Build(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error) {
	// Create the Client for Write operations.
	c, err := client.New(config, options)
	if err != nil {
		return nil, err
	}

	return client.NewDelegatingClient(client.NewDelegatingClientInput{
		CacheReader:     cache,
		Client:          c,
		UncachedObjects: n.uncached,
	})
}
