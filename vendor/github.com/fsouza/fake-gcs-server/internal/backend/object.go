// Copyright 2018 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"cloud.google.com/go/storage"
)

// ObjectAttrs represents the meta-data without its contents.
type ObjectAttrs struct {
	BucketName         string `json:"-"`
	Name               string `json:"-"`
	Size               int64  `json:"-"`
	StorageClass       string
	ContentType        string
	ContentEncoding    string
	ContentDisposition string
	ContentLanguage    string
	CacheControl       string
	Crc32c             string
	Md5Hash            string
	Etag               string
	ACL                []storage.ACLRule
	Metadata           map[string]string
	Created            string
	Deleted            string
	Updated            string
	CustomTime         string
	Generation         int64
}

// ID is used for comparing objects.
func (o *ObjectAttrs) ID() string {
	return fmt.Sprintf("%s#%d", o.IDNoGen(), o.Generation)
}

// IDNoGen does not consider the generation field.
func (o *ObjectAttrs) IDNoGen() string {
	return fmt.Sprintf("%s/%s", o.BucketName, o.Name)
}

// Object represents the object that is stored within the fake server.
type Object struct {
	ObjectAttrs
	Content []byte
}

type noopSeekCloser struct {
	io.ReadSeeker
}

func (n noopSeekCloser) Close() error {
	return nil
}

func (o Object) StreamingObject() StreamingObject {
	return StreamingObject{
		ObjectAttrs: o.ObjectAttrs,
		Content:     noopSeekCloser{bytes.NewReader(o.Content)},
	}
}

type StreamingObject struct {
	ObjectAttrs
	Content io.ReadSeekCloser
}

func (o *StreamingObject) Close() error {
	if o != nil && o.Content != nil {
		return o.Content.Close()
	}
	return nil
}

// Convert this StreamingObject to a (buffered) Object.
func (o *StreamingObject) BufferedObject() (Object, error) {
	data, err := io.ReadAll(o.Content)
	return Object{
		ObjectAttrs: o.ObjectAttrs,
		Content:     data,
	}, err
}

func (o *StreamingObject) patch(attrsToUpdate ObjectAttrs) {
	currObjValues := reflect.ValueOf(&(o.ObjectAttrs)).Elem()
	currObjType := currObjValues.Type()
	newObjValues := reflect.ValueOf(attrsToUpdate)
	for i := 0; i < newObjValues.NumField(); i++ {
		if reflect.Value.IsZero(newObjValues.Field(i)) {
			continue
		} else if currObjType.Field(i).Name == "Metadata" {
			if o.Metadata == nil {
				o.Metadata = map[string]string{}
			}
			for k, v := range attrsToUpdate.Metadata {
				o.Metadata[k] = v
			}
		} else {
			currObjValues.Field(i).Set(newObjValues.Field(i))
		}
	}
}
