/*
 * Copyright 2017 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

// request.go - defines the BCE servies request

package bce

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
)

// Body defines the data structure used in BCE request.
// Every BCE request that sets the body field must set its content-length and content-md5 headers
// to ensure the correctness of the body content forcely, and users can also set the content-sha256
// header to strengthen the correctness with the "SetHeader" method.
type Body struct {
	stream     io.ReadCloser
	size       int64
	contentMD5 string
}

func (b *Body) Stream() io.ReadCloser { return b.stream }

func (b *Body) SetStream(stream io.ReadCloser) { b.stream = stream }

func (b *Body) Size() int64 { return b.size }

func (b *Body) ContentMD5() string { return b.contentMD5 }

// NewBodyFromBytes - build a Body object from the byte stream to be used in the http request, it
// calculates the content-md5 of the byte stream and store the size as well as the stream.
//
// PARAMS:
//     - stream: byte stream
// RETURNS:
//     - *Body: the return Body object
//     - error: error if any specific error occurs
func NewBodyFromBytes(stream []byte) (*Body, error) {
	buf := bytes.NewBuffer(stream)
	size := int64(buf.Len())
	contentMD5, err := util.CalculateContentMD5(buf, size)
	if err != nil {
		return nil, err
	}
	buf = bytes.NewBuffer(stream)
	return &Body{ioutil.NopCloser(buf), size, contentMD5}, nil
}

// NewBodyFromString - build a Body object from the string to be used in the http request, it
// calculates the content-md5 of the byte stream and store the size as well as the stream.
//
// PARAMS:
//     - str: the input string
// RETURNS:
//     - *Body: the return Body object
//     - error: error if any specific error occurs
func NewBodyFromString(str string) (*Body, error) {
	buf := bytes.NewBufferString(str)
	size := int64(len(str))
	contentMD5, err := util.CalculateContentMD5(buf, size)
	if err != nil {
		return nil, err
	}
	buf = bytes.NewBufferString(str)
	return &Body{ioutil.NopCloser(buf), size, contentMD5}, nil
}

// NewBodyFromFile - build a Body object from the given file name to be used in the http request,
// it calculates the content-md5 of the byte stream and store the size as well as the stream.
//
// PARAMS:
//     - fname: the given file name
// RETURNS:
//     - *Body: the return Body object
//     - error: error if any specific error occurs
func NewBodyFromFile(fname string) (*Body, error) {
	file, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	fileInfo, infoErr := file.Stat()
	if infoErr != nil {
		return nil, infoErr
	}
	contentMD5, md5Err := util.CalculateContentMD5(file, fileInfo.Size())
	if md5Err != nil {
		return nil, md5Err
	}
	if _, err = file.Seek(0, 0); err != nil {
		return nil, err
	}
	return &Body{file, fileInfo.Size(), contentMD5}, nil
}

// NewBodyFromSectionFile - build a Body object from the given file pointer with offset and size.
// It calculates the content-md5 of the given content and store the size as well as the stream.
//
// PARAMS:
//     - file: the input file pointer
//     - off: offset of current section body
//     - size: current section body size
// RETURNS:
//     - *Body: the return Body object
//     - error: error if any specific error occurs
func NewBodyFromSectionFile(file *os.File, off, size int64) (*Body, error) {
	if _, err := file.Seek(off, 0); err != nil {
		return nil, err
	}
	contentMD5, md5Err := util.CalculateContentMD5(file, size)
	if md5Err != nil {
		return nil, md5Err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	section := io.NewSectionReader(file, off, size)
	return &Body{ioutil.NopCloser(section), size, contentMD5}, nil
}

// NewBodyFromSizedReader - build a Body object from the given reader with size.
// It calculates the content-md5 of the given content and store the size as well as the stream.
//
// PARAMS:
//     - r: the input reader
//     - size: the size to be read, -1 is read all
// RETURNS:
//     - *Body: the return Body object
//     - error: error if any specific error occurs
func NewBodyFromSizedReader(r io.Reader, size int64) (*Body, error) {
	var buffer bytes.Buffer
	var rlen int64
	var err error
	if size >= 0 {
		rlen, err = io.CopyN(&buffer, r, size)
	} else {
		rlen, err = io.Copy(&buffer, r)
	}
	if err != nil {
		return nil, err
	}
	if rlen != int64(buffer.Len()) { // must be equal
		return nil, NewBceClientError("unexpected reader")
	}
	if size >= 0 {
		if rlen < size {
			return nil, NewBceClientError("size is great than reader actual size")
		}
	}
	contentMD5, err := util.CalculateContentMD5(bytes.NewBuffer(buffer.Bytes()), rlen)
	if err != nil {
		return nil, err
	}
	body := &Body{
		stream:     ioutil.NopCloser(&buffer),
		size:       rlen,
		contentMD5: contentMD5,
	}
	return body, nil
}

// BceRequest defines the request structure for accessing BCE services
type BceRequest struct {
	http.Request
	requestId   string
	clientError *BceClientError
}

func (b *BceRequest) RequestId() string { return b.requestId }

func (b *BceRequest) SetRequestId(val string) { b.requestId = val }

func (b *BceRequest) ClientError() *BceClientError { return b.clientError }

func (b *BceRequest) SetClientError(err *BceClientError) { b.clientError = err }

func (b *BceRequest) SetBody(body *Body) { // override SetBody derived from http.Request
	b.Request.SetBody(body.Stream())
	b.SetLength(body.Size()) // set field of "net/http.Request.ContentLength"
	if body.Size() > 0 {
		b.SetHeader(http.CONTENT_MD5, body.ContentMD5())
		b.SetHeader(http.CONTENT_LENGTH, fmt.Sprintf("%d", body.Size()))
	}
}

func (b *BceRequest) BuildHttpRequest() {
	// Only need to build the specific `requestId` field for BCE, other fields are same as the
	// `http.Request` as well as its methods.
	if len(b.requestId) == 0 {
		// Construct the request ID with UUID
		b.requestId = util.NewRequestId()
	}
	b.SetHeader(http.BCE_REQUEST_ID, b.requestId)
}

func (b *BceRequest) String() string {
	requestIdStr := "requestId=" + b.requestId
	if b.clientError != nil {
		return requestIdStr + ", client error: " + b.ClientError().Error()
	}
	return requestIdStr + "\n" + b.Request.String()
}
