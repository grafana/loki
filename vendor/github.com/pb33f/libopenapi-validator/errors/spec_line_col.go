// Copyright 2023-2026 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package errors

import (
	"go.yaml.in/yaml/v4"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// SafeNodeLineCol safely extracts line and column from a yaml.Node pointer.
// Returns (1, 0) if the node is nil.
func SafeNodeLineCol(node *yaml.Node) (int, int) {
	if node == nil {
		return 1, 0
	}
	return node.Line, node.Column
}

// paramExplodeLineCol safely extracts SpecLine/SpecCol from param.GoLow().Explode.ValueNode.
func paramExplodeLineCol(param *v3.Parameter) (int, int) {
	if low := param.GoLow(); low != nil && low.Explode.ValueNode != nil {
		return low.Explode.ValueNode.Line, low.Explode.ValueNode.Column
	}
	return 1, 0
}

// paramStyleLineCol safely extracts SpecLine/SpecCol from param.GoLow().Style.ValueNode.
func paramStyleLineCol(param *v3.Parameter) (int, int) {
	if low := param.GoLow(); low != nil && low.Style.ValueNode != nil {
		return low.Style.ValueNode.Line, low.Style.ValueNode.Column
	}
	return 1, 0
}

// paramRequiredLineCol safely extracts SpecLine/SpecCol from param.GoLow().Required.KeyNode.
func paramRequiredLineCol(param *v3.Parameter) (int, int) {
	if low := param.GoLow(); low != nil && low.Required.KeyNode != nil {
		return low.Required.KeyNode.Line, low.Required.KeyNode.Column
	}
	return 1, 0
}

// paramSchemaKeyLineCol safely extracts SpecLine/SpecCol from param.GoLow().Schema.KeyNode.
func paramSchemaKeyLineCol(param *v3.Parameter) (int, int) {
	if low := param.GoLow(); low != nil && low.Schema.KeyNode != nil {
		return low.Schema.KeyNode.Line, low.Schema.KeyNode.Column
	}
	return 1, 0
}

// paramSchemaTypeLineCol safely extracts SpecLine/SpecCol from
// param.GoLow().Schema.Value.Schema().Type.KeyNode.
func paramSchemaTypeLineCol(param *v3.Parameter) (int, int) {
	if low := param.GoLow(); low != nil {
		if sv := low.Schema.Value; sv != nil {
			if s := sv.Schema(); s != nil && s.Type.KeyNode != nil {
				return s.Type.KeyNode.Line, s.Type.KeyNode.Column
			}
		}
	}
	return 1, 0
}

// paramSchemaEnumLineCol safely extracts SpecLine/SpecCol from
// param.GoLow().Schema.Value.Schema().Enum.KeyNode.
func paramSchemaEnumLineCol(param *v3.Parameter) (int, int) {
	if low := param.GoLow(); low != nil {
		if sv := low.Schema.Value; sv != nil {
			if s := sv.Schema(); s != nil && s.Enum.KeyNode != nil {
				return s.Enum.KeyNode.Line, s.Enum.KeyNode.Column
			}
		}
	}
	return 1, 0
}

// paramContentLineCol safely extracts SpecLine/SpecCol from
// param.GoLow().FindContent(ct).ValueNode.
func paramContentLineCol(param *v3.Parameter, contentType string) (int, int) {
	if low := param.GoLow(); low != nil {
		if found := low.FindContent(contentType); found != nil && found.ValueNode != nil {
			return found.ValueNode.Line, found.ValueNode.Column
		}
	}
	return 1, 0
}

// schemaItemsTypeLineCol safely extracts SpecLine/SpecCol from
// sch.Items.A.GoLow().Schema().Type.KeyNode.
func schemaItemsTypeLineCol(sch *base.Schema) (int, int) {
	if sch.Items != nil && sch.Items.A != nil {
		if low := sch.Items.A.GoLow(); low != nil {
			if s := low.Schema(); s != nil && s.Type.KeyNode != nil {
				return s.Type.KeyNode.Line, s.Type.KeyNode.Column
			}
		}
	}
	return 1, 0
}
