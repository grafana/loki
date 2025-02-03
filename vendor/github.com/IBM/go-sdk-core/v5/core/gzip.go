package core

// (C) Copyright IBM Corp. 2020.
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
	"compress/gzip"
	"io"
)

// NewGzipCompressionReader will return an io.Reader instance that will deliver
// the gzip-compressed version of the "uncompressedReader" argument.
// This function was inspired by this github gist:
//
//	https://gist.github.com/tomcatzh/cf8040820962e0f8c04700eb3b2f26be
func NewGzipCompressionReader(uncompressedReader io.Reader) (io.Reader, error) {
	// Create a pipe whose reader will effectively replace "uncompressedReader"
	// to deliver the gzip-compressed byte stream.
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		defer pipeWriter.Close()

		// Wrap the pipe's writer with a gzip writer that will
		// write the gzip-compressed bytes to the Pipe.
		compressedWriter := gzip.NewWriter(pipeWriter)
		defer compressedWriter.Close()

		// To trigger the operation of the pipe, we'll simply start
		// to copy bytes from "uncompressedReader" to "compressedWriter".
		// This copy operation will block as needed in order to write bytes
		// to the pipe only when the pipe reader is called to retrieve more bytes.
		_, err := io.Copy(compressedWriter, uncompressedReader)
		if err != nil {
			sdkErr := SDKErrorf(err, "", "compression-failed", getComponentInfo())
			_ = pipeWriter.CloseWithError(sdkErr)
		}
	}()
	return pipeReader, nil
}

// NewGzipDecompressionReader will return an io.Reader instance that will deliver
// the gzip-decompressed version of the "compressedReader" argument.
func NewGzipDecompressionReader(compressedReader io.Reader) (io.Reader, error) {
	res, err := gzip.NewReader(compressedReader)
	if err != nil {
		err = SDKErrorf(err, "", "decompress-read-error", getComponentInfo())
	}
	return res, err
}
