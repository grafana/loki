// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build go1.24

package mcp

import (
	"errors"
	"os"
)

// withFile calls f on the file at join(dir, rel),
// protecting against path traversal attacks.
func withFile(dir, rel string, f func(*os.File) error) (err error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	file, err := r.Open(rel)
	if err != nil {
		return err
	}
	// Record error, in case f writes.
	defer func() { err = errors.Join(err, file.Close()) }()
	return f(file)
}
