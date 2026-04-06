// Copyright 2022-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package overlay

import (
	highoverlay "github.com/pb33f/libopenapi/datamodel/high/overlay"
	"go.yaml.in/yaml/v4"
)

// validateOverlay checks that the overlay has all required fields.
func validateOverlay(overlay *highoverlay.Overlay) error {
	if overlay.Overlay == "" {
		return ErrMissingOverlayField
	}
	if overlay.Info == nil {
		return ErrMissingInfo
	}
	if len(overlay.Actions) == 0 {
		return ErrEmptyActions
	}
	return nil
}

// validateTarget checks that a target node is a valid target (object or array).
// Per the Overlay Spec, primitive/null targets are invalid.
func validateTarget(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return ErrPrimitiveTarget
	}
	return nil
}
