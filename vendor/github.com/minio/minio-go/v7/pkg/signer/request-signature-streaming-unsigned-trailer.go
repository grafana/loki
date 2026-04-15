/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2022 MinIO, Inc.
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

package signer

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// getUnsignedChunkLength - calculates the length of chunk metadata
func getUnsignedChunkLength(chunkDataSize int64) int64 {
	return int64(len(fmt.Sprintf("%x", chunkDataSize))) +
		crlfLen +
		chunkDataSize +
		crlfLen
}

// getUSStreamLength - calculates the length of the overall stream (data + metadata)
func getUSStreamLength(dataLen, chunkSize int64, trailers http.Header) int64 {
	if dataLen <= 0 {
		return 0
	}

	chunksCount := int64(dataLen / chunkSize)
	remainingBytes := int64(dataLen % chunkSize)
	streamLen := int64(0)
	streamLen += chunksCount * getUnsignedChunkLength(chunkSize)
	if remainingBytes > 0 {
		streamLen += getUnsignedChunkLength(remainingBytes)
	}
	streamLen += getUnsignedChunkLength(0)
	if len(trailers) > 0 {
		for name, placeholder := range trailers {
			if len(placeholder) > 0 {
				streamLen += int64(len(name) + len(trailerKVSeparator) + len(placeholder[0]) + 1)
			}
		}
		streamLen += crlfLen
	}

	return streamLen
}

// prepareStreamingRequest - prepares a request with appropriate
// headers before computing the seed signature.
func prepareUSStreamingRequest(req *http.Request, sessionToken string, dataLen int64, timestamp time.Time) {
	req.TransferEncoding = []string{"aws-chunked"}
	if sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", sessionToken)
	}

	req.Header.Set("X-Amz-Date", timestamp.Format(iso8601DateFormat))
	// Set content length with streaming signature for each chunk included.
	req.ContentLength = getUSStreamLength(dataLen, int64(payloadChunkSize), req.Trailer)
}

// StreamingUSReader implements chunked upload signature as a reader on
// top of req.Body's ReaderCloser chunk header;data;... repeat
type StreamingUSReader struct {
	contentLen     int64         // Content-Length from req header
	baseReadCloser io.ReadCloser // underlying io.Reader
	bytesRead      int64         // bytes read from underlying io.Reader
	buf            bytes.Buffer  // holds signed chunk
	chunkBuf       []byte        // holds raw data read from req Body
	chunkBufLen    int           // no. of bytes read so far into chunkBuf
	done           bool          // done reading the underlying reader to EOF
	chunkNum       int
	totalChunks    int
	lastChunkSize  int
	trailer        http.Header
}

// writeChunk - signs a chunk read from s.baseReader of chunkLen size.
func (s *StreamingUSReader) writeChunk(chunkLen int, addCrLf bool) {
	s.buf.WriteString(strconv.FormatInt(int64(chunkLen), 16) + "\r\n")

	// Write chunk data into streaming buffer
	s.buf.Write(s.chunkBuf[:chunkLen])

	// Write the chunk trailer.
	if addCrLf {
		s.buf.Write([]byte("\r\n"))
	}

	// Reset chunkBufLen for next chunk read.
	s.chunkBufLen = 0
	s.chunkNum++
}

// addSignedTrailer - adds a trailer with the provided headers,
// then signs a chunk and adds it to output.
func (s *StreamingUSReader) addTrailer(h http.Header) {
	olen := len(s.chunkBuf)
	s.chunkBuf = s.chunkBuf[:0]
	for k, v := range h {
		s.chunkBuf = append(s.chunkBuf, []byte(strings.ToLower(k)+trailerKVSeparator+v[0]+"\n")...)
	}

	s.buf.Write(s.chunkBuf)
	s.buf.WriteString("\r\n\r\n")

	// Reset chunkBufLen for next chunk read.
	s.chunkBuf = s.chunkBuf[:olen]
	s.chunkBufLen = 0
	s.chunkNum++
}

// StreamingUnsignedV4 - provides chunked upload
func StreamingUnsignedV4(req *http.Request, sessionToken string, dataLen int64, reqTime time.Time) *http.Request {
	// Set headers needed for streaming signature.
	prepareUSStreamingRequest(req, sessionToken, dataLen, reqTime)

	if req.Body == nil {
		req.Body = io.NopCloser(bytes.NewReader([]byte("")))
	}

	stReader := &StreamingUSReader{
		baseReadCloser: req.Body,
		chunkBuf:       make([]byte, payloadChunkSize),
		contentLen:     dataLen,
		chunkNum:       1,
		totalChunks:    int((dataLen+payloadChunkSize-1)/payloadChunkSize) + 1,
		lastChunkSize:  int(dataLen % payloadChunkSize),
	}
	if len(req.Trailer) > 0 {
		stReader.trailer = req.Trailer
		// Remove...
		req.Trailer = nil
	}

	req.Body = stReader

	return req
}

// Read - this method performs chunk upload signature providing a
// io.Reader interface.
func (s *StreamingUSReader) Read(buf []byte) (int, error) {
	switch {
	// After the last chunk is read from underlying reader, we
	// never re-fill s.buf.
	case s.done:

	// s.buf will be (re-)filled with next chunk when has lesser
	// bytes than asked for.
	case s.buf.Len() < len(buf):
		s.chunkBufLen = 0
		for {
			n1, err := s.baseReadCloser.Read(s.chunkBuf[s.chunkBufLen:])
			// Usually we validate `err` first, but in this case
			// we are validating n > 0 for the following reasons.
			//
			// 1. n > 0, err is one of io.EOF, nil (near end of stream)
			// A Reader returning a non-zero number of bytes at the end
			// of the input stream may return either err == EOF or err == nil
			//
			// 2. n == 0, err is io.EOF (actual end of stream)
			//
			// Callers should always process the n > 0 bytes returned
			// before considering the error err.
			if n1 > 0 {
				s.chunkBufLen += n1
				s.bytesRead += int64(n1)

				if s.chunkBufLen == payloadChunkSize ||
					(s.chunkNum == s.totalChunks-1 &&
						s.chunkBufLen == s.lastChunkSize) {
					// Sign the chunk and write it to s.buf.
					s.writeChunk(s.chunkBufLen, true)
					break
				}
			}
			if err != nil {
				if err == io.EOF {
					// No more data left in baseReader - last chunk.
					// Done reading the last chunk from baseReader.
					s.done = true

					// bytes read from baseReader different than
					// content length provided.
					if s.bytesRead != s.contentLen {
						return 0, fmt.Errorf("http: ContentLength=%d with Body length %d", s.contentLen, s.bytesRead)
					}

					// Sign the chunk and write it to s.buf.
					s.writeChunk(0, len(s.trailer) == 0)
					if len(s.trailer) > 0 {
						// Trailer must be set now.
						s.addTrailer(s.trailer)
					}
					break
				}
				return 0, err
			}
		}
	}
	return s.buf.Read(buf)
}

// Close - this method makes underlying io.ReadCloser's Close method available.
func (s *StreamingUSReader) Close() error {
	return s.baseReadCloser.Close()
}
