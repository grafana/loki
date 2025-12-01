// Copyright 2019 Huawei Technologies Co.,Ltd.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License.  You may obtain a copy of the
// License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations under the License.

package obs

import (
	"errors"
	"io"
	"os"
	"sort"
	"strings"
)

// ListMultipartUploads lists the multipart uploads.
//
// You can use this API to list the multipart uploads that are initialized but not combined or aborted in a specified bucket.
func (obsClient ObsClient) ListMultipartUploads(input *ListMultipartUploadsInput, extensions ...extensionOptions) (output *ListMultipartUploadsOutput, err error) {
	if input == nil {
		return nil, errors.New("ListMultipartUploadsInput is nil")
	}
	output = &ListMultipartUploadsOutput{}
	err = obsClient.doActionWithBucket("ListMultipartUploads", HTTP_GET, input.Bucket, input, output, extensions)
	if err != nil {
		output = nil
	} else if output.EncodingType == "url" {
		err = decodeListMultipartUploadsOutput(output)
		if err != nil {
			doLog(LEVEL_ERROR, "Failed to get ListMultipartUploadsOutput with error: %v.", err)
			output = nil
		}
	}
	return
}

// AbortMultipartUpload aborts a multipart upload in a specified bucket by using the multipart upload ID.
func (obsClient ObsClient) AbortMultipartUpload(input *AbortMultipartUploadInput, extensions ...extensionOptions) (output *BaseModel, err error) {
	if input == nil {
		return nil, errors.New("AbortMultipartUploadInput is nil")
	}
	if input.UploadId == "" {
		return nil, errors.New("UploadId is empty")
	}
	output = &BaseModel{}
	err = obsClient.doActionWithBucketAndKey("AbortMultipartUpload", HTTP_DELETE, input.Bucket, input.Key, input, output, extensions)
	if err != nil {
		output = nil
	}
	return
}

// InitiateMultipartUpload initializes a multipart upload.
func (obsClient ObsClient) InitiateMultipartUpload(input *InitiateMultipartUploadInput, extensions ...extensionOptions) (output *InitiateMultipartUploadOutput, err error) {
	if input == nil {
		return nil, errors.New("InitiateMultipartUploadInput is nil")
	}

	if input.ContentType == "" && input.Key != "" {
		if contentType, ok := mimeTypes[strings.ToLower(input.Key[strings.LastIndex(input.Key, ".")+1:])]; ok {
			input.ContentType = contentType
		}
	}

	output = &InitiateMultipartUploadOutput{}
	err = obsClient.doActionWithBucketAndKey("InitiateMultipartUpload", HTTP_POST, input.Bucket, input.Key, input, output, extensions)
	if err != nil {
		output = nil
	} else {
		ParseInitiateMultipartUploadOutput(output)
		if output.EncodingType == "url" {
			err = decodeInitiateMultipartUploadOutput(output)
			if err != nil {
				doLog(LEVEL_ERROR, "Failed to get InitiateMultipartUploadOutput with error: %v.", err)
				output = nil
			}
		}
	}
	return
}

// UploadPart uploads a part to a specified bucket by using a specified multipart upload ID.
//
// After a multipart upload is initialized, you can use this API to upload a part to a specified bucket
// by using the multipart upload ID. Except for the last uploaded part whose size ranges from 0 to 5 GB,
// sizes of the other parts range from 100 KB to 5 GB. The upload part ID ranges from 1 to 10000.
func (obsClient ObsClient) UploadPart(_input *UploadPartInput, extensions ...extensionOptions) (output *UploadPartOutput, err error) {
	if _input == nil {
		return nil, errors.New("UploadPartInput is nil")
	}

	if _input.UploadId == "" {
		return nil, errors.New("UploadId is empty")
	}

	input := &UploadPartInput{}
	input.Bucket = _input.Bucket
	input.Key = _input.Key
	input.PartNumber = _input.PartNumber
	input.UploadId = _input.UploadId
	input.ContentMD5 = _input.ContentMD5
	input.ContentSHA256 = _input.ContentSHA256
	input.SourceFile = _input.SourceFile
	input.Offset = _input.Offset
	input.PartSize = _input.PartSize
	input.SseHeader = _input.SseHeader
	input.Body = _input.Body

	output = &UploadPartOutput{}
	var repeatable bool
	if input.Body != nil {
		if _, ok := input.Body.(*strings.Reader); ok {
			repeatable = true
		}
		if _, ok := input.Body.(*readerWrapper); !ok && input.PartSize > 0 {
			input.Body = &readerWrapper{reader: input.Body, totalCount: input.PartSize}
		}
	} else if sourceFile := strings.TrimSpace(input.SourceFile); sourceFile != "" {
		fd, _err := os.Open(sourceFile)
		if _err != nil {
			err = _err
			return nil, err
		}
		defer func() {
			errMsg := fd.Close()
			if errMsg != nil {
				doLog(LEVEL_WARN, "Failed to close file with reason: %v", errMsg)
			}
		}()

		stat, _err := fd.Stat()
		if _err != nil {
			err = _err
			return nil, err
		}
		fileSize := stat.Size()
		fileReaderWrapper := &fileReaderWrapper{filePath: sourceFile}
		fileReaderWrapper.reader = fd

		if input.Offset < 0 || input.Offset > fileSize {
			input.Offset = 0
		}

		if input.PartSize <= 0 || input.PartSize > (fileSize-input.Offset) {
			input.PartSize = fileSize - input.Offset
		}
		fileReaderWrapper.totalCount = input.PartSize
		fileReaderWrapper.mark = input.Offset
		if _, err = fd.Seek(input.Offset, io.SeekStart); err != nil {
			return nil, err
		}
		input.Body = fileReaderWrapper
		repeatable = true
	}
	if repeatable {
		err = obsClient.doActionWithBucketAndKey("UploadPart", HTTP_PUT, input.Bucket, input.Key, input, output, extensions)
	} else {
		err = obsClient.doActionWithBucketAndKeyUnRepeatable("UploadPart", HTTP_PUT, input.Bucket, input.Key, input, output, extensions)
	}
	if err != nil {
		output = nil
	} else {
		ParseUploadPartOutput(output)
		output.PartNumber = input.PartNumber
	}
	return
}

// CompleteMultipartUpload combines the uploaded parts in a specified bucket by using the multipart upload ID.
func (obsClient ObsClient) CompleteMultipartUpload(input *CompleteMultipartUploadInput, extensions ...extensionOptions) (output *CompleteMultipartUploadOutput, err error) {
	if input == nil {
		return nil, errors.New("CompleteMultipartUploadInput is nil")
	}

	if input.UploadId == "" {
		return nil, errors.New("UploadId is empty")
	}

	var parts partSlice = input.Parts
	sort.Sort(parts)

	output = &CompleteMultipartUploadOutput{}
	err = obsClient.doActionWithBucketAndKey("CompleteMultipartUpload", HTTP_POST, input.Bucket, input.Key, input, output, extensions)
	if err != nil {
		output = nil
	} else {
		ParseCompleteMultipartUploadOutput(output)
		if output.EncodingType == "url" {
			err = decodeCompleteMultipartUploadOutput(output)
			if err != nil {
				doLog(LEVEL_ERROR, "Failed to get CompleteMultipartUploadOutput with error: %v.", err)
				output = nil
			}
		}
	}
	return
}

// ListParts lists the uploaded parts in a bucket by using the multipart upload ID.
func (obsClient ObsClient) ListParts(input *ListPartsInput, extensions ...extensionOptions) (output *ListPartsOutput, err error) {
	if input == nil {
		return nil, errors.New("ListPartsInput is nil")
	}
	if input.UploadId == "" {
		return nil, errors.New("UploadId is empty")
	}
	output = &ListPartsOutput{}
	err = obsClient.doActionWithBucketAndKey("ListParts", HTTP_GET, input.Bucket, input.Key, input, output, extensions)
	if err != nil {
		output = nil
	} else if output.EncodingType == "url" {
		err = decodeListPartsOutput(output)
		if err != nil {
			doLog(LEVEL_ERROR, "Failed to get ListPartsOutput with error: %v.", err)
			output = nil
		}
	}
	return
}

// CopyPart copy a part to a specified bucket by using a specified multipart upload ID.
//
// After a multipart upload is initialized, you can use this API to copy a part to a specified bucket by using the multipart upload ID.
func (obsClient ObsClient) CopyPart(input *CopyPartInput, extensions ...extensionOptions) (output *CopyPartOutput, err error) {
	if input == nil {
		return nil, errors.New("CopyPartInput is nil")
	}
	if input.UploadId == "" {
		return nil, errors.New("UploadId is empty")
	}
	if strings.TrimSpace(input.CopySourceBucket) == "" {
		return nil, errors.New("Source bucket is empty")
	}
	if strings.TrimSpace(input.CopySourceKey) == "" {
		return nil, errors.New("Source key is empty")
	}
	if input.CopySourceRange != "" && !strings.HasPrefix(input.CopySourceRange, "bytes=") {
		return nil, errors.New("Source Range should start with [bytes=]")
	}

	output = &CopyPartOutput{}
	err = obsClient.doActionWithBucketAndKey("CopyPart", HTTP_PUT, input.Bucket, input.Key, input, output, extensions)
	if err != nil {
		output = nil
	} else {
		ParseCopyPartOutput(output)
		output.PartNumber = input.PartNumber
	}
	return
}
