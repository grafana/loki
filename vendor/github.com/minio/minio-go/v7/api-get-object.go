/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2020 MinIO, Inc.
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

package minio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// GetObject wrapper function that accepts a request context
func (c *Client) GetObject(ctx context.Context, bucketName, objectName string, opts GetObjectOptions) (*Object, error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Code:       InvalidBucketName,
			Message:    err.Error(),
		}
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return nil, ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Code:       XMinioInvalidObjectName,
			Message:    err.Error(),
		}
	}

	if opts.RDMABuffer != nil && c.rdmaEnabled {
		n, err := c.getObjectRDMA(ctx, bucketName, objectName, opts)
		if err != nil {
			return nil, err
		}
		return &Object{
			mutex:    &sync.Mutex{},
			isClosed: true,
			objectInfo: ObjectInfo{
				Key:  objectName,
				Size: n,
			},
			objectInfoSet: true,
		}, nil
	}

	gctx, cancel := context.WithCancel(ctx)

	// Detect if snowball is server location we are talking to.
	var snowball bool
	if location, ok := c.bucketLocCache.Get(bucketName); ok {
		snowball = location == "snowball"
	}

	var (
		err        error
		httpReader io.ReadCloser
		objectInfo ObjectInfo
		totalRead  int
	)

	// Create request channel.
	reqCh := make(chan getRequest)
	// Create response channel.
	resCh := make(chan getResponse)
	// record original range header for stat operation.
	originalRangeHeader := opts.Header().Get("Range")

	// This routine feeds partial object data as and when the caller reads.
	go func() {
		defer close(resCh)
		defer func() {
			// Close the http response body before returning.
			// This ends the connection with the server.
			if httpReader != nil {
				httpReader.Close()
			}
		}()
		defer cancel()

		// Used to verify if etag of object has changed since last read.
		var etag string

		for req := range reqCh {
			// If this is the first request we may not need to do a getObject request yet.
			if req.isFirstReq {
				// First request is a Read/ReadAt.
				if req.isReadOp {
					// Differentiate between wanting the whole object and just a range.
					if req.isReadAt {
						// If this is a ReadAt request only get the specified range.
						// Range is set with respect to the offset and length of the buffer requested.
						// Do not set objectInfo from the first readAt request because it will not get
						// the whole object.
						opts.SetRange(req.Offset, req.Offset+int64(len(req.Buffer))-1)
					} else if req.Offset > 0 {
						opts.SetRange(req.Offset, 0)
					}
					httpReader, objectInfo, _, err = c.getObject(gctx, bucketName, objectName, opts)
					if err != nil {
						resCh <- getResponse{Error: err}
						// An unsatisfiable range is recoverable: the caller
						// may Seek or ReadAt within bounds next, so keep
						// serving requests instead of ending the stream.
						if ToErrorResponse(err).Code == InvalidRange {
							continue
						}
						return
					}
					etag = objectInfo.ETag
					// Read at least firstReq.Buffer bytes, if not we have
					// reached our EOF.
					size, err := readFull(httpReader, req.Buffer)
					totalRead += size
					if size > 0 && err == io.ErrUnexpectedEOF {
						if int64(size) < objectInfo.Size {
							// In situations when returned size
							// is less than the expected content
							// length set by the server, make sure
							// we return io.ErrUnexpectedEOF
							err = io.ErrUnexpectedEOF
						} else {
							// If an EOF happens after reading some but not
							// all the bytes ReadFull returns ErrUnexpectedEOF
							err = io.EOF
						}
					} else if size == 0 && err == io.EOF && objectInfo.Size > 0 {
						// Special cases when server writes more data
						// than the content-length, net/http response
						// body returns an error, instead of converting
						// it to io.EOF - return unexpected EOF.
						err = io.ErrUnexpectedEOF
					}
					// when doing a readAt, we can't reuse the httpReader for next read action.
					if req.isReadAt {
						httpReader.Close()
						httpReader = nil
					}

					objectSize := int64(0)
					// case for ReadFull with range request
					if !req.isReadAt && req.Offset == 0 {
						objectSize = objectInfo.Size
					}
					// Send back the first response.
					resCh <- getResponse{
						ObjectSize: objectSize,
						objectInfo: objectInfo,
						Size:       size,
						Error:      err,
						didRead:    true,
					}
				} else {
					if originalRangeHeader == "" {
						// First request is a Seek call.
						// Only need to run a StatObject until an actual Read request comes through.

						// Remove range header if already set, for stat Operations to get original file size.
						delete(opts.headers, "Range")
					} else {
						opts.headers["Range"] = originalRangeHeader
					}
					objectInfo, err = c.StatObject(gctx, bucketName, objectName, StatObjectOptions(opts))
					if err != nil {
						resCh <- getResponse{
							Error: err,
						}
						// Exit the go-routine.
						return
					}
					etag = objectInfo.ETag
					// Send back the first response.
					resCh <- getResponse{
						ObjectSize: objectInfo.Size,
						objectInfo: objectInfo,
					}
				}
			} else if req.settingObjectInfo { // Request is just to get objectInfo.
				// Remove range header if already set, for stat Operations to get original file size.
				if originalRangeHeader == "" {
					delete(opts.headers, "Range")
				} else {
					opts.headers["Range"] = originalRangeHeader
				}
				// Check whether this is snowball
				// if yes do not use If-Match feature
				// it doesn't work.
				if etag != "" && !snowball {
					opts.SetMatchETag(etag)
				}
				objectInfo, err := c.StatObject(gctx, bucketName, objectName, StatObjectOptions(opts))
				if err != nil {
					resCh <- getResponse{
						Error: err,
					}
					// Exit the goroutine.
					return
				}
				// Send back the objectInfo.
				resCh <- getResponse{
					ObjectSize: objectInfo.Size,
					objectInfo: objectInfo,
				}
			} else {
				objectSize := int64(0)
				renewReader := false
				// Offset changes fetch the new object at an Offset.
				// Because the httpReader may not be set by the first
				// request if it was a stat or seek it must be checked
				// if the object has been read or not to only initialize
				// new ones when they haven't been already.
				// All readAt requests are new requests.
				// A nil httpReader means the previous fetch failed and
				// left no stream, so a new one must be established
				// regardless of the offset bookkeeping.
				if req.DidOffsetChange || !req.beenRead || httpReader == nil {
					// Check whether this is snowball
					// if yes do not use If-Match feature
					// it doesn't work.
					if etag != "" && !snowball {
						opts.SetMatchETag(etag)
					}
					if httpReader != nil {
						// Close previously opened http reader.
						httpReader.Close()
					}
					// If this request is a readAt only get the specified range.
					if req.isReadAt {
						// Range is set with respect to the offset and length of the buffer requested.
						opts.SetRange(req.Offset, req.Offset+int64(len(req.Buffer))-1)
					} else if req.Offset > 0 { // Range is set with respect to the offset.
						opts.SetRange(req.Offset, 0)
					} else {
						if originalRangeHeader == "" {
							delete(opts.headers, "Range")
						} else {
							opts.headers["Range"] = originalRangeHeader
						}
					}
					httpReader, objectInfo, _, err = c.getObject(gctx, bucketName, objectName, opts)
					if err != nil {
						resCh <- getResponse{
							Error: err,
						}
						// An unsatisfiable range is recoverable: the caller
						// may Seek or ReadAt within bounds next, so keep
						// serving requests instead of ending the stream.
						if ToErrorResponse(err).Code == InvalidRange {
							continue
						}
						return
					}
					// case for ReadFull with range request
					if !req.isReadAt && req.Offset == 0 {
						objectSize = objectInfo.Size
					}
					renewReader = true
					totalRead = 0
				}

				// Read at least req.Buffer bytes, if not we have
				// reached our EOF.
				size, err := readFull(httpReader, req.Buffer)
				totalRead += size
				if size > 0 && err == io.ErrUnexpectedEOF {
					if int64(totalRead) < objectInfo.Size {
						// In situations when returned size
						// is less than the expected content
						// length set by the server, make sure
						// we return io.ErrUnexpectedEOF
						err = io.ErrUnexpectedEOF
					} else {
						// If an EOF happens after reading some but not
						// all the bytes ReadFull returns ErrUnexpectedEOF
						err = io.EOF
					}
				} else if size == 0 && err == io.EOF && objectInfo.Size > 0 {
					// Special cases when server writes more data
					// than the content-length, net/http response
					// body returns an error, instead of converting
					// it to io.EOF - return unexpected EOF.
					err = io.ErrUnexpectedEOF
				}
				if renewReader && req.isReadAt {
					httpReader.Close()
					httpReader = nil
				}
				// Reply back how much was read.
				resCh <- getResponse{
					ObjectSize: objectSize,
					Size:       size,
					Error:      err,
					didRead:    true,
					objectInfo: objectInfo,
				}
			}
		}
	}()

	// Create a newObject through the information sent back by reqCh.
	return newObject(gctx, cancel, reqCh, resCh), nil
}

// get request message container to communicate with internal
// go-routine.
type getRequest struct {
	Buffer            []byte
	Offset            int64 // readAt offset.
	DidOffsetChange   bool  // Tracks the offset changes for Seek requests.
	beenRead          bool  // Determines if this is the first time an object is being read.
	isReadAt          bool  // Determines if this request is a request to a specific range
	isReadOp          bool  // Determines if this request is a Read or Read/At request.
	isFirstReq        bool  // Determines if this request is the first time an object is being accessed.
	settingObjectInfo bool  // Determines if this request is to set the objectInfo of an object.
}

// get response message container to reply back for the request.
type getResponse struct {
	ObjectSize int64
	Size       int
	Error      error
	didRead    bool       // Lets subsequent calls know whether or not httpReader has been initiated.
	objectInfo ObjectInfo // Used for the first request.
}

// Object represents an open object. It implements
// Reader, ReaderAt, Seeker, Closer for a HTTP stream.
type Object struct {
	// Mutex.
	mutex *sync.Mutex

	// User allocated and defined.
	reqCh      chan<- getRequest
	resCh      <-chan getResponse
	ctx        context.Context
	cancel     context.CancelFunc
	currOffset int64
	totalSize  int64
	objectInfo ObjectInfo

	// Ask lower level to initiate data fetching based on currOffset
	seekData bool

	// Keeps track of closed call.
	isClosed bool

	// Keeps track of if this is the first call.
	isStarted bool

	// Previous error saved for future calls.
	prevErr error

	// Keeps track of if this object has been read yet.
	beenRead bool

	// Keeps track of if objectInfo has been set yet.
	objectInfoSet bool
}

// doGetRequest - sends and blocks on the firstReqCh and reqCh of an object.
// Returns back the size of the buffer read, if anything was read, as well
// as any error encountered. For all first requests sent on the object
// it is also responsible for sending back the objectInfo.
func (o *Object) doGetRequest(request getRequest) (getResponse, error) {
	select {
	case <-o.ctx.Done():
		return getResponse{}, o.ctx.Err()
	case o.reqCh <- request:
	}

	response := <-o.resCh

	// Return any error to the top level.
	if response.Error != nil && response.Error != io.EOF {
		return response, response.Error
	}

	// This was the first request.
	if !o.isStarted {
		// The object has been operated on.
		o.isStarted = true
	}
	// Set the objectInfo if the request was not readAt
	// and it hasn't been set before.
	if !o.objectInfoSet && !request.isReadAt {
		o.objectInfo = response.objectInfo
		o.objectInfoSet = true
	}
	// Set beenRead only if it has not been set before.
	if !o.beenRead {
		o.beenRead = response.didRead
	}
	// Data are ready on the wire, no need to reinitiate connection in lower level
	o.seekData = false

	if response.ObjectSize != 0 {
		o.totalSize = response.ObjectSize
	}

	return response, response.Error
}

// setOffset - handles the setting of offsets for
// Read/ReadAt/Seek requests.
func (o *Object) setOffset(bytesRead int64) error {
	// Update the currentOffset.
	o.currOffset += bytesRead

	if o.totalSize > -1 && o.currOffset >= o.totalSize {
		return io.EOF
	}
	return nil
}

// Read reads up to len(b) bytes into b. It returns the number of
// bytes read (0 <= n <= len(b)) and any error encountered. Returns
// io.EOF upon end of file.
func (o *Object) Read(b []byte) (n int, err error) {
	if o == nil {
		return 0, errInvalidArgument("Object is nil")
	}

	// Locking.
	o.mutex.Lock()
	defer o.mutex.Unlock()

	// prevErr is previous error saved from previous operation.
	if o.prevErr != nil || o.isClosed {
		return 0, o.prevErr
	}
	// If the current offset is at or beyond the known object size, there is
	// nothing more to read. Return io.EOF directly instead of issuing a range
	// request from EOF (which the server rejects as an unsatisfiable range).
	if o.objectInfoSet && o.objectInfo.Size > -1 && o.currOffset >= o.objectInfo.Size {
		o.prevErr = io.EOF
		return 0, io.EOF
	}

	// Create a new request.
	readReq := getRequest{
		isReadOp: true,
		beenRead: o.beenRead,
		Buffer:   b,
	}

	// Alert that this is the first request.
	if !o.isStarted {
		readReq.isFirstReq = true
	}

	// Ask to establish a new data fetch routine based on seekData flag
	readReq.DidOffsetChange = o.seekData
	readReq.Offset = o.currOffset

	// Send and receive from the first request.
	response, err := o.doGetRequest(readReq)
	if err != nil && err != io.EOF {
		// An InvalidRange response to a range generated from a non-zero
		// read offset means the position is at or past EOF for the object
		// as it currently exists. Offset zero sends only a caller-supplied
		// range, whose InvalidRange must surface untranslated.
		if o.currOffset > 0 && ToErrorResponse(err).Code == InvalidRange {
			o.prevErr = io.EOF
			return 0, io.EOF
		}
		// Save the error for future calls.
		o.prevErr = err
		return response.Size, err
	}

	// Bytes read.
	bytesRead := int64(response.Size)

	// Set the new offset.
	oerr := o.setOffset(bytesRead)
	if oerr != nil {
		// Save the error for future calls.
		o.prevErr = oerr
		return response.Size, oerr
	}

	// Return the response.
	return response.Size, err
}

// Stat returns the ObjectInfo structure describing Object.
// When requesting a partial object or reading has started,
// the size returned will reflect the remaining size.
func (o *Object) Stat() (ObjectInfo, error) {
	if o == nil {
		return ObjectInfo{}, errInvalidArgument("Object is nil")
	}
	// Locking.
	o.mutex.Lock()
	defer o.mutex.Unlock()

	if o.prevErr != nil && o.prevErr != io.EOF {
		return ObjectInfo{}, o.prevErr
	}

	// When the object info is already known (e.g. the RDMA GET path, whose
	// payload is delivered out-of-band so the object is created closed, or a
	// previously completed request) report it, even for a closed object.
	if o.objectInfoSet {
		if o.currOffset > o.totalSize {
			return ObjectInfo{}, io.EOF
		}
		if o.currOffset <= o.totalSize {
			o.objectInfo.Size = o.totalSize - o.currOffset
		}
		return o.objectInfo, nil
	}

	if o.isClosed {
		return ObjectInfo{}, o.prevErr
	}

	// This is the first request.
	if !o.isStarted || !o.objectInfoSet {
		// Send the request and get the response.
		_, err := o.doGetRequest(getRequest{
			isFirstReq:        !o.isStarted,
			settingObjectInfo: !o.objectInfoSet,
		})
		if err != nil {
			o.prevErr = err
			return ObjectInfo{}, err
		}
	}
	if o.currOffset > o.totalSize {
		return ObjectInfo{}, io.EOF
	}
	if o.currOffset <= o.totalSize {
		o.objectInfo.Size = o.totalSize - o.currOffset
	}
	return o.objectInfo, nil
}

// ReadAt reads len(b) bytes from the File starting at byte offset
// off. It returns the number of bytes read and the error, if any.
// ReadAt always returns a non-nil error when n < len(b). At end of
// file, that error is io.EOF.
func (o *Object) ReadAt(b []byte, offset int64) (n int, err error) {
	if o == nil {
		return 0, errInvalidArgument("Object is nil")
	}

	// Locking.
	o.mutex.Lock()
	defer o.mutex.Unlock()

	// prevErr is error which was saved in previous operation.
	if o.prevErr != nil && o.prevErr != io.EOF || o.isClosed {
		return 0, o.prevErr
	}

	// Save and restore currOffset so ReadAt doesn't affect sequential Read operations.
	// Per io.ReaderAt: "ReadAt should not affect nor be affected by the underlying seek offset."
	savedOffset := o.currOffset
	defer func() {
		o.currOffset = savedOffset
		o.seekData = true // Force next Read to re-establish stream at correct position
	}()

	o.currOffset = offset

	// Can only compare offsets to size when size has been set.
	if o.objectInfoSet {
		// If offset is negative than we return io.EOF.
		// If offset is greater than or equal to object size we return io.EOF.
		if (o.totalSize > -1 && offset >= o.totalSize) || offset < 0 {
			return 0, io.EOF
		}
	}

	// Create the new readAt request.
	readAtReq := getRequest{
		isReadOp:        true,
		isReadAt:        true,
		DidOffsetChange: true,       // Offset always changes.
		beenRead:        o.beenRead, // Set if this is the first request to try and read.
		Offset:          offset,     // Set the offset.
		Buffer:          b,
	}

	// Alert that this is the first request.
	if !o.isStarted {
		readAtReq.isFirstReq = true
	}

	// Send and receive from the first request.
	response, err := o.doGetRequest(readAtReq)
	if err != nil && err != io.EOF {
		// Reading at an offset at or beyond the object size yields
		// InvalidRange from the server: report io.EOF, matching the
		// io.ReaderAt contract for reads past the end.
		if ToErrorResponse(err).Code == InvalidRange {
			return 0, io.EOF
		}
		// Save the error.
		o.prevErr = err
		return response.Size, err
	}
	// Bytes read.
	bytesRead := int64(response.Size)
	// There is no valid objectInfo yet
	// 	to compare against for EOF.
	if !o.objectInfoSet {
		// Update the currentOffset.
		o.currOffset += bytesRead
	} else {
		// If this was not the first request update
		// the offsets and compare against objectInfo
		// for EOF.
		oerr := o.setOffset(bytesRead)
		if oerr != nil {
			o.prevErr = oerr
			return response.Size, oerr
		}
	}
	return response.Size, err
}

// Seek sets the offset for the next Read or Write to offset,
// interpreted according to whence: 0 means relative to the
// origin of the file, 1 means relative to the current offset,
// and 2 means relative to the end.
// Seek returns the new offset and an error, if any.
//
// Seeking to a position before the start of the object is an error.
// Seeking to the end of the object is legal; the subsequent Read reports
// io.EOF. When the object size is known, seeking past the end returns
// io.EOF from Seek itself and leaves the offset unchanged.
func (o *Object) Seek(offset int64, whence int) (n int64, err error) {
	if o == nil {
		return 0, errInvalidArgument("Object is nil")
	}

	// Locking.
	o.mutex.Lock()
	defer o.mutex.Unlock()

	// At EOF seeking is legal allow only io.EOF, for any other errors we return.
	if o.prevErr != nil && o.prevErr != io.EOF {
		return 0, o.prevErr
	}

	// Negative absolute offsets are invalid for SeekStart. Negative offsets
	// for SeekCurrent/SeekEnd are valid as long as the computed position is
	// not before the start of the object (checked after applying whence).
	if offset < 0 && whence == 0 {
		return 0, errInvalidArgument("Negative position not allowed for 0")
	}

	// This is the first request. So before anything else
	// get the ObjectInfo.
	if !o.isStarted || !o.objectInfoSet {
		// Create the new Seek request.
		seekReq := getRequest{
			isReadOp:   false,
			Offset:     offset,
			isFirstReq: true,
		}
		// Send and receive from the seek request.
		_, err := o.doGetRequest(seekReq)
		if err != nil {
			// Save the error.
			o.prevErr = err
			return 0, err
		}
	}

	newOffset := o.currOffset

	// Switch through whence.
	switch whence {
	default:
		return 0, errInvalidArgument(fmt.Sprintf("Invalid whence %d", whence))
	case 0:
		newOffset = offset
	case 1:
		newOffset += offset
	case 2:
		// If we don't know the object size return an error for io.SeekEnd
		if o.totalSize < 0 {
			return 0, errInvalidArgument("Whence END is not supported when the object size is unknown")
		}
		newOffset = o.totalSize + offset
	}
	// Seeking to a position before the start of the object is not allowed for
	// any whence.
	if newOffset < 0 {
		return 0, errInvalidArgument(fmt.Sprintf("Seeking at negative offset not allowed for %d", whence))
	}
	// Seeking past the end of the object is rejected with io.EOF, preserving
	// long-standing behavior. Seeking to exactly the end is legal and the
	// subsequent Read reports io.EOF.
	if o.objectInfo.Size > -1 && newOffset > o.objectInfo.Size {
		return 0, io.EOF
	}
	// Reset the saved error since we successfully seeked, let the Read
	// and ReadAt decide.
	if o.prevErr == io.EOF {
		o.prevErr = nil
	}

	// Ask lower level to fetch again from source when necessary
	o.seekData = (newOffset != o.currOffset) || o.seekData
	o.currOffset = newOffset

	// Return the effective offset.
	return o.currOffset, nil
}

// Close - The behavior of Close after the first call returns error
// for subsequent Close() calls.
func (o *Object) Close() (err error) {
	if o == nil {
		return errInvalidArgument("Object is nil")
	}

	// Locking.
	o.mutex.Lock()
	defer o.mutex.Unlock()

	// if already closed return an error.
	if o.isClosed {
		return o.prevErr
	}

	// Close successfully.
	o.cancel()

	// Close the request channel to indicate the internal go-routine to exit.
	close(o.reqCh)

	// Save for future operations.
	errMsg := "Object is already closed. Bad file descriptor."
	o.prevErr = errors.New(errMsg)
	// Save here that we closed done channel successfully.
	o.isClosed = true
	return nil
}

// newObject instantiates a new *minio.Object*
// ObjectInfo will be set by setObjectInfo
func newObject(ctx context.Context, cancel context.CancelFunc, reqCh chan<- getRequest, resCh <-chan getResponse) *Object {
	return &Object{
		ctx:    ctx,
		cancel: cancel,
		mutex:  &sync.Mutex{},
		reqCh:  reqCh,
		resCh:  resCh,
	}
}

// getObject - retrieve object from Object Storage.
//
// Additionally this function also takes range arguments to download the specified
// range bytes of an object. Setting offset and length = 0 will download the full object.
//
// For more information about the HTTP Range header.
// go to http://www.w3.org/Protocols/rfc2616/rfc2616-sec14.html#sec14.35.
func (c *Client) getObject(ctx context.Context, bucketName, objectName string, opts GetObjectOptions) (io.ReadCloser, ObjectInfo, http.Header, error) {
	// Validate input arguments.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, ObjectInfo{}, nil, ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Code:       InvalidBucketName,
			Message:    err.Error(),
		}
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return nil, ObjectInfo{}, nil, ErrorResponse{
			StatusCode: http.StatusBadRequest,
			Code:       XMinioInvalidObjectName,
			Message:    err.Error(),
		}
	}

	// Execute GET on objectName.
	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:       bucketName,
		objectName:       objectName,
		queryValues:      opts.toQueryValues(),
		customHeader:     opts.Header(),
		contentSHA256Hex: emptySHA256Hex,
	})
	if err != nil {
		return nil, ObjectInfo{}, nil, err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			return nil, ObjectInfo{}, nil, httpRespToErrorResponse(resp, bucketName, objectName)
		}
	}

	objectStat, err := ToObjectInfo(bucketName, objectName, resp.Header)
	if err != nil {
		closeResponse(resp)
		return nil, ObjectInfo{}, nil, err
	}

	body := resp.Body
	if opts.Checksum {
		if opts.checkSumReader != nil {
			opts.checkSumReader.SetReader(resp.Body)
			body = opts.checkSumReader
		} else if objectStat.ChecksumMode == ChecksumFullObjectMode.String() && opts.headers["Range"] == "" {
			if hasherReader := c.newChecksumVerifyingReader(objectStat); hasherReader != nil {
				hasherReader.SetReader(resp.Body)
				body = hasherReader
			}
		}
	}

	// do not close body here, caller will close
	return body, objectStat, resp.Header, nil
}
