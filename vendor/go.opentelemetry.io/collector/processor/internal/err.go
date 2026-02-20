// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/processor/internal"

import (
	"fmt"

	"go.opentelemetry.io/collector/component"
)

func ErrIDMismatch(id component.ID, typ component.Type) error {
	return fmt.Errorf("component type mismatch: component ID %q does not have type %q", id, typ)
}
