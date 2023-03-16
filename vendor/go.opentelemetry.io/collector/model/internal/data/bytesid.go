// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package data

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// marshalJSON converts trace id into a hex string enclosed in quotes.
// Called by Protobuf JSON deserialization.
func marshalJSON(id []byte) ([]byte, error) {
	if len(id) == 0 {
		return []byte(`""`), nil
	}

	// 2 chars per byte plus 2 quote chars at the start and end.
	hexLen := 2*len(id) + 2

	b := make([]byte, hexLen)
	hex.Encode(b[1:hexLen-1], id)
	b[0], b[hexLen-1] = '"', '"'

	return b, nil
}

// unmarshalJSON inflates trace id from hex string, possibly enclosed in quotes.
// Called by Protobuf JSON deserialization.
func unmarshalJSON(dst []byte, src []byte) error {
	if l := len(src); l >= 2 && src[0] == '"' && src[l-1] == '"' {
		src = src[1 : l-1]
	}
	nLen := len(src)
	if nLen == 0 {
		return nil
	}

	if len(dst) != hex.DecodedLen(nLen) {
		return errors.New("invalid length for ID")
	}

	_, err := hex.Decode(dst, src)
	if err != nil {
		return fmt.Errorf("cannot unmarshal ID from string '%s': %w", string(src), err)
	}
	return nil
}

func marshalBytes(dst []byte, src []byte) (n int, err error) {
	if len(dst) < len(src) {
		return 0, errors.New("buffer is too short")
	}
	return copy(dst, src), nil
}
