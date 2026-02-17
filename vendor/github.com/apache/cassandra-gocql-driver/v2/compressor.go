/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2016, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

// Compressor defines the interface for frame compression and decompression.
// Implementations provide compression algorithms like Snappy and LZ4 that can be used
// to reduce network traffic between the driver and Cassandra nodes.
type Compressor interface {
	Name() string

	// AppendCompressedWithLength compresses src bytes, appends the length of the compressed bytes to dst
	// and then appends the compressed bytes to dst.
	// It returns a new byte slice that is the result of the append operation.
	AppendCompressedWithLength(dst, src []byte) ([]byte, error)

	// AppendDecompressedWithLength reads the length of the decompressed bytes from src,
	// decompressed bytes from src and appends the decompressed bytes to dst.
	// It returns a new byte slice that is the result of the append operation.
	AppendDecompressedWithLength(dst, src []byte) ([]byte, error)

	// AppendCompressed compresses src bytes and appends the compressed bytes to dst.
	// It returns a new byte slice that is the result of the append operation.
	AppendCompressed(dst, src []byte) ([]byte, error)

	// AppendDecompressed decompresses bytes from src and appends the decompressed bytes to dst.
	// It returns a new byte slice that is the result of the append operation.
	AppendDecompressed(dst, src []byte, decompressedLength uint32) ([]byte, error)
}
