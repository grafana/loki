// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Package json provides internal JSON utilities.

package json

import (
	"bytes"

	"github.com/segmentio/encoding/json"
)

func Unmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DontMatchCaseInsensitiveStructFields()
	return dec.Decode(v)
}
