// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package confmap // import "go.opentelemetry.io/collector/confmap"

type moduleFactory[T any, S any] interface {
	Create(s S) T
}

type createConfmapFunc[T any, S any] func(s S) T

type confmapModuleFactory[T any, S any] struct {
	f createConfmapFunc[T, S]
}

func (c confmapModuleFactory[T, S]) Create(s S) T {
	return c.f(s)
}

func newConfmapModuleFactory[T, S any](f createConfmapFunc[T, S]) moduleFactory[T, S] {
	return confmapModuleFactory[T, S]{
		f: f,
	}
}
