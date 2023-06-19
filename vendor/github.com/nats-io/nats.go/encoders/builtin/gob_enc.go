// Copyright 2013-2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package builtin

import (
	"bytes"
	"encoding/gob"
)

// GobEncoder is a Go specific GOB Encoder implementation for EncodedConn.
// This encoder will use the builtin encoding/gob to Marshal
// and Unmarshal most types, including structs.
type GobEncoder struct {
	// Empty
}

// FIXME(dlc) - This could probably be more efficient.

// Encode
func (ge *GobEncoder) Encode(subject string, v interface{}) ([]byte, error) {
	b := new(bytes.Buffer)
	enc := gob.NewEncoder(b)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Decode
func (ge *GobEncoder) Decode(subject string, data []byte, vPtr interface{}) (err error) {
	dec := gob.NewDecoder(bytes.NewBuffer(data))
	err = dec.Decode(vPtr)
	return
}
