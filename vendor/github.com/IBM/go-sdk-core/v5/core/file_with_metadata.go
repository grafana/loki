package core

// (C) Copyright IBM Corp. 2021.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"encoding/json"
	"io"
	"os"
	"reflect"
)

// FileWithMetadata : A file with its associated metadata.
type FileWithMetadata struct {
	// The data / content for the file.
	Data io.ReadCloser `json:"data" validate:"required"`

	// The filename of the file.
	Filename *string `json:"filename,omitempty"`

	// The content type of the file.
	ContentType *string `json:"content_type,omitempty"`
}

// NewFileWithMetadata : Instantiate FileWithMetadata (Generic Model Constructor)
func NewFileWithMetadata(data io.ReadCloser) (model *FileWithMetadata, err error) {
	model = &FileWithMetadata{
		Data: data,
	}
	err = ValidateStruct(model, "required parameters")
	return
}

// UnmarshalFileWithMetadata unmarshals an instance of FileWithMetadata from the specified map of raw messages.
// The "data" field is assumed to be a string, the value of which is assumed to be a path to the file that
// contains the data intended for the FileWithMetadata struct.
func UnmarshalFileWithMetadata(m map[string]json.RawMessage, result interface{}) (err error) {
	obj := new(FileWithMetadata)

	// unmarshal the data field as a filename and read the contents
	// then explicitly set the Data field to the contents of the file
	var data io.ReadCloser
	var pathToData string
	err = UnmarshalPrimitive(m, "data", &pathToData)
	if err != nil {
		return
	}
	data, err = os.Open(pathToData) // #nosec G304
	if err != nil {
		return
	}
	obj.Data = data

	// unmarshal the other fields as usual
	err = UnmarshalPrimitive(m, "filename", &obj.Filename)
	if err != nil {
		return
	}
	err = UnmarshalPrimitive(m, "content_type", &obj.ContentType)
	if err != nil {
		return
	}
	reflect.ValueOf(result).Elem().Set(reflect.ValueOf(obj))
	return
}
