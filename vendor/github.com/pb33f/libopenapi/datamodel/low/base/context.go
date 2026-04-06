// Copyright 2023-2024 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package base

import (
	"context"
	"sync"
)

// ModelContext is a struct that holds various persistent data structures for the model
// that passes through the entire model building process.
type ModelContext struct {
	SchemaCache *sync.Map
}

// GetModelContext will return the ModelContext from a context.Context object
// if it is available, otherwise it will return nil.
func GetModelContext(ctx context.Context) *ModelContext {
	if ctx == nil {
		return nil
	}
	if ctx.Value("modelCtx") == nil {
		return nil
	}
	if c, ok := ctx.Value("modelCtx").(*ModelContext); ok {
		return c
	}
	return nil
}
