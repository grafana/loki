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

// Package pdata (pipeline data) implements data structures that represent telemetry data in-memory.
// All data received is converted into this format, travels through the pipeline
// in this format, and is converted from this format by exporters when sending.
//
// Current implementation primarily uses OTLP ProtoBuf structs as the underlying data
// structures for many of of the declared structs. We keep a pointer to OTLP protobuf
// in the "orig" member field. This allows efficient translation to/from OTLP wire
// protocol. Note that the underlying data structure is kept private so that we are
// free to make changes to it in the future.
//
// Most of the internal data structures must be created via New* functions. Zero-initialized
// structures, in most cases, are not valid (read comments for each struct to know if that
// is the case). This is a slight deviation from idiomatic Go to avoid unnecessary
// pointer checks in dozens of functions which assume the invariant that "orig" member
// is non-nil. Several structures also provide New*Slice functions that allow creating
// more than one instance of the struct more efficiently instead of calling New*
// repeatedly. Use it where appropriate.
//
// This package also provides common ways for decoding serialized bytes into protocol-specific
// in-memory data models (e.g. Zipkin Span). These data models can then be translated to pdata
// representations. Similarly, pdata types can be translated from a data model which can then
// be serialized into bytes.
//
// * Encoding: Common interfaces for serializing/deserializing bytes from/to protocol-specific data models.
// * Translation: Common interfaces for translating protocol-specific data models from/to pdata types.
// * Marshaling: Common higher level APIs that do both encoding and translation of bytes and data model if going directly pdata types to bytes.
package pdata
