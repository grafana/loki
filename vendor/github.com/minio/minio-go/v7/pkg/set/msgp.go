/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2025 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package set

import "github.com/tinylib/msgp/msgp"

// EncodeMsg encodes the message to the writer.
// Values are stored as a slice of strings or nil.
func (s StringSet) EncodeMsg(writer *msgp.Writer) error {
	if s == nil {
		return writer.WriteNil()
	}
	err := writer.WriteArrayHeader(uint32(len(s)))
	if err != nil {
		return err
	}
	sorted := s.ToByteSlices()
	for _, k := range sorted {
		err = writer.WriteStringFromBytes(k)
		if err != nil {
			return err
		}
	}
	return nil
}

// MarshalMsg encodes the message to the bytes.
// Values are stored as a slice of strings or nil.
func (s StringSet) MarshalMsg(bytes []byte) ([]byte, error) {
	if s == nil {
		return msgp.AppendNil(bytes), nil
	}
	if len(s) == 0 {
		return msgp.AppendArrayHeader(bytes, 0), nil
	}
	bytes = msgp.AppendArrayHeader(bytes, uint32(len(s)))
	sorted := s.ToByteSlices()
	for _, k := range sorted {
		bytes = msgp.AppendStringFromBytes(bytes, k)
	}
	return bytes, nil
}

// DecodeMsg decodes the message from the reader.
func (s *StringSet) DecodeMsg(reader *msgp.Reader) error {
	if reader.IsNil() {
		*s = nil
		return reader.Skip()
	}
	sz, err := reader.ReadArrayHeader()
	if err != nil {
		return err
	}
	dst := *s
	if dst == nil {
		dst = make(StringSet, sz)
	} else {
		for k := range dst {
			delete(dst, k)
		}
	}
	for i := uint32(0); i < sz; i++ {
		var k string
		k, err = reader.ReadString()
		if err != nil {
			return err
		}
		dst[k] = struct{}{}
	}
	*s = dst
	return nil
}

// UnmarshalMsg decodes the message from the bytes.
func (s *StringSet) UnmarshalMsg(bytes []byte) ([]byte, error) {
	if msgp.IsNil(bytes) {
		*s = nil
		return bytes[msgp.NilSize:], nil
	}
	// Read the array header
	sz, bytes, err := msgp.ReadArrayHeaderBytes(bytes)
	if err != nil {
		return nil, err
	}
	dst := *s
	if dst == nil {
		dst = make(StringSet, sz)
	} else {
		for k := range dst {
			delete(dst, k)
		}
	}
	for i := uint32(0); i < sz; i++ {
		var k string
		k, bytes, err = msgp.ReadStringBytes(bytes)
		if err != nil {
			return nil, err
		}
		dst[k] = struct{}{}
	}
	*s = dst
	return bytes, nil
}

// Msgsize returns the maximum size of the message.
func (s StringSet) Msgsize() int {
	if s == nil {
		return msgp.NilSize
	}
	if len(s) == 0 {
		return msgp.ArrayHeaderSize
	}
	size := msgp.ArrayHeaderSize
	for key := range s {
		size += msgp.StringPrefixSize + len(key)
	}
	return size
}

// MarshalBinary encodes the receiver into a binary form and returns the result.
func (s StringSet) MarshalBinary() ([]byte, error) {
	return s.MarshalMsg(nil)
}

// AppendBinary appends the binary representation of itself to the end of b
func (s StringSet) AppendBinary(b []byte) ([]byte, error) {
	return s.MarshalMsg(b)
}

// UnmarshalBinary decodes the binary representation of itself from b
func (s *StringSet) UnmarshalBinary(b []byte) error {
	_, err := s.UnmarshalMsg(b)
	return err
}
