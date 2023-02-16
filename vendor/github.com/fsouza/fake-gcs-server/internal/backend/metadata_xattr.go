// Copyright 2022 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import (
	"github.com/pkg/xattr"
)

const xattrKey = "user.metadata"

type metadataXattr struct{}

func (m metadataXattr) write(path string, encoded []byte) error {
	return xattr.Set(path, xattrKey, encoded)
}

func (m metadataXattr) read(path string) ([]byte, error) {
	return xattr.Get(path, xattrKey)
}

func (m metadataXattr) isSpecialFile(path string) bool {
	return false
}

func (m metadataXattr) remove(path string) error {
	return nil
}

func (m metadataXattr) rename(pathSrc, pathDst string) error {
	return nil
}
