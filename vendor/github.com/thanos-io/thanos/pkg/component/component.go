// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package component

import (
	"strings"

	"github.com/thanos-io/thanos/pkg/store/storepb"
)

// Component is a generic component interface.
type Component interface {
	String() string
}

// StoreAPI is a component that implements Thanos' gRPC StoreAPI.
type StoreAPI interface {
	implementsStoreAPI()
	String() string
	ToProto() storepb.StoreType
}

// Source is a Thanos component that produce blocks of metrics.
type Source interface {
	producesBlocks()
	String() string
}

// SourceStoreAPI is a component that implements Thanos' gRPC StoreAPI
// and produce blocks of metrics.
type SourceStoreAPI interface {
	implementsStoreAPI()
	producesBlocks()
	String() string
	ToProto() storepb.StoreType
}

type component struct {
	name string
}

func (c component) String() string { return c.name }

type storeAPI struct {
	component
}

func (storeAPI) implementsStoreAPI() {}

func (s sourceStoreAPI) ToProto() storepb.StoreType {
	return storepb.StoreType(storepb.StoreType_value[strings.ToUpper(s.String())])
}

func (s storeAPI) ToProto() storepb.StoreType {
	return storepb.StoreType(storepb.StoreType_value[strings.ToUpper(s.String())])
}

type source struct {
	component
}

func (source) producesBlocks() {}

type sourceStoreAPI struct {
	component
	source
	storeAPI
}

// FromProto converts from a gRPC StoreType to StoreAPI.
func FromProto(storeType storepb.StoreType) StoreAPI {
	switch storeType {
	case storepb.StoreType_QUERY:
		return Query
	case storepb.StoreType_RULE:
		return Rule
	case storepb.StoreType_SIDECAR:
		return Sidecar
	case storepb.StoreType_STORE:
		return Store
	case storepb.StoreType_RECEIVE:
		return Receive
	case storepb.StoreType_DEBUG:
		return Debug
	default:
		return UnknownStoreAPI
	}
}

var (
	Bucket          = source{component: component{name: "bucket"}}
	Cleanup         = source{component: component{name: "cleanup"}}
	Mark            = source{component: component{name: "mark"}}
	Rewrite         = source{component: component{name: "rewrite"}}
	Compact         = source{component: component{name: "compact"}}
	Downsample      = source{component: component{name: "downsample"}}
	Replicate       = source{component: component{name: "replicate"}}
	QueryFrontend   = source{component: component{name: "query-frontend"}}
	Debug           = sourceStoreAPI{component: component{name: "debug"}}
	Receive         = sourceStoreAPI{component: component{name: "receive"}}
	Rule            = sourceStoreAPI{component: component{name: "rule"}}
	Sidecar         = sourceStoreAPI{component: component{name: "sidecar"}}
	Store           = storeAPI{component: component{name: "store"}}
	UnknownStoreAPI = storeAPI{component: component{name: "unknown-store-api"}}
	Query           = storeAPI{component: component{name: "query"}}
)
