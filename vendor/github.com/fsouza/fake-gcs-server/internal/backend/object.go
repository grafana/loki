// Copyright 2018 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

// Object represents the object that is stored within the fake server.
type Object struct {
	BucketName string `json:"-"`
	Name       string `json:"-"`
	Content    []byte
	Crc32c     string
	Md5Hash    string
}

// ID is useful for comparing objects
func (o *Object) ID() string {
	return o.BucketName + "/" + o.Name
}
