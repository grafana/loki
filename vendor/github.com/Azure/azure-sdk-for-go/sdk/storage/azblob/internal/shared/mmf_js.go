//go:build js && wasm

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package shared

import (
	"fmt"
	"math"
)

// Mmb is a plain heap-allocated buffer used on platforms without mmap support (e.g. WASM).
type Mmb []byte

// NewMMB allocates a plain byte slice. No mmap is used.
func NewMMB(size int64) (Mmb, error) {
	if size < 0 || size > math.MaxInt {
		return nil, fmt.Errorf("size %d out of range", size)
	}

	return make([]byte, int(size)), nil
}

// Delete is a no-op; GC handles reclamation.
func (m *Mmb) Delete() {
	*m = nil
}
