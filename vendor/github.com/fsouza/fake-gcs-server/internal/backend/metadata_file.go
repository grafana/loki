// Copyright 2022 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import (
	"os"
	"strings"
)

const metadataSuffix = ".metadata"

type metadataFile struct{}

func (m metadataFile) write(path string, encoded []byte) error {
	return writeFile(path+metadataSuffix, encoded, 0o600)
}

func (m metadataFile) read(path string) ([]byte, error) {
	return os.ReadFile(path + metadataSuffix)
}

func (m metadataFile) isSpecialFile(path string) bool {
	return strings.HasSuffix(path, metadataSuffix)
}

func (m metadataFile) remove(path string) error {
	return os.Remove(path + metadataSuffix)
}

func (m metadataFile) rename(pathSrc, pathDst string) error {
	return os.Rename(pathSrc+metadataSuffix, pathDst+metadataSuffix)
}
