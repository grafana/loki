// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import (
	"strconv"

	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// validateArray checks an array value against a schema for undeclared properties
// within array items. It handles:
//   - items (schema for all items or boolean)
//   - prefixItems (tuple validation with positional schemas)
//   - unevaluatedItems (items not covered by items/prefixItems)
func (v *Validator) validateArray(ctx *traversalContext, schema *base.Schema, data []any) []UndeclaredValue {
	if len(data) == 0 {
		return nil
	}

	var undeclared []UndeclaredValue

	// Check for items: false
	// When items: false, no items are allowed. If base validation passed, the
	// array should be empty. But we explicitly check in case it wasn't caught.
	if schema.Items != nil && schema.Items.IsB() && !schema.Items.B {
		for i := range data {
			itemPath := buildArrayPath(ctx.path, i)
			undeclared = append(undeclared,
				newUndeclaredItem(itemPath, strconv.Itoa(i), data[i], ctx.direction))
		}
		return undeclared
	}

	prefixLen := 0

	// handle prefixItems first (tuple validation)
	if len(schema.PrefixItems) > 0 {
		for i, itemProxy := range schema.PrefixItems {
			if i >= len(data) {
				break
			}

			itemPath := buildArrayPath(ctx.path, i)
			itemCtx := ctx.withPath(itemPath)

			if itemCtx.shouldIgnore() {
				prefixLen++
				continue
			}

			itemSchema := itemProxy.Schema()
			if itemSchema != nil {
				undeclared = append(undeclared, v.validateValue(itemCtx, itemSchema, data[i])...)
			}
			prefixLen++
		}
	}

	// handle items for remaining elements (after prefixItems)
	if schema.Items != nil && schema.Items.A != nil {
		itemProxy := schema.Items.A
		itemSchema := itemProxy.Schema()

		if itemSchema != nil {
			for i := prefixLen; i < len(data); i++ {
				itemPath := buildArrayPath(ctx.path, i)
				itemCtx := ctx.withPath(itemPath)

				if itemCtx.shouldIgnore() {
					continue
				}

				undeclared = append(undeclared, v.validateValue(itemCtx, itemSchema, data[i])...)
			}
		}
	}

	// handle unevaluatedItems with schema.
	// unevaluatedItems: false is handled by base validation.
	// unevaluatedItems: {schema} means items matching the schema are valid.
	// note: this doesn't account for items evaluated by `contains`. for strict
	// validation this is acceptable as we check conservatively.
	if schema.UnevaluatedItems != nil && schema.UnevaluatedItems.Schema() != nil {
		// this applies to items not covered by items or prefixItems.
		// if there's no items schema, unevaluatedItems applies to:
		// - items after prefixItems (if prefixItems exists)
		// - all items (if neither items nor prefixItems exists)
		if schema.Items == nil {
			unevalSchema := schema.UnevaluatedItems.Schema()
			startIndex := len(schema.PrefixItems) // 0 if no prefixItems
			for i := startIndex; i < len(data); i++ {
				itemPath := buildArrayPath(ctx.path, i)
				itemCtx := ctx.withPath(itemPath)

				if itemCtx.shouldIgnore() {
					continue
				}

				undeclared = append(undeclared, v.validateValue(itemCtx, unevalSchema, data[i])...)
			}
		}
	}

	return undeclared
}
