// Copyright 2022 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

type metadataHandler interface {
	write(path string, encoded []byte) error
	read(path string) ([]byte, error)
	remove(path string) error
	isSpecialFile(path string) bool
	rename(pathSrc, pathDst string) error
}
