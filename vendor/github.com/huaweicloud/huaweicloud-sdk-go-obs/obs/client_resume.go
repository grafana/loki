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

// UploadFile resume uploads.
//
// This API is an encapsulated and enhanced version of multipart upload, and aims to eliminate large file
// upload failures caused by poor network conditions and program breakdowns.
func (obsClient ObsClient) UploadFile(input *UploadFileInput, extensions ...extensionOptions) (output *CompleteMultipartUploadOutput, err error) {
	if input.EnableCheckpoint && input.CheckpointFile == "" {
		input.CheckpointFile = input.UploadFile + ".uploadfile_record"
	}

	if input.TaskNum <= 0 {
		input.TaskNum = 1
	}
	if input.PartSize < MIN_PART_SIZE {
		input.PartSize = MIN_PART_SIZE
	} else if input.PartSize > MAX_PART_SIZE {
		input.PartSize = MAX_PART_SIZE
	}

	output, err = obsClient.resumeUpload(input, extensions)
	return
}

// DownloadFile resume downloads.
//
// This API is an encapsulated and enhanced version of partial download, and aims to eliminate large file
// download failures caused by poor network conditions and program breakdowns.
func (obsClient ObsClient) DownloadFile(input *DownloadFileInput, extensions ...extensionOptions) (output *GetObjectMetadataOutput, err error) {
	if input.DownloadFile == "" {
		input.DownloadFile = input.Key
	}

	if input.EnableCheckpoint && input.CheckpointFile == "" {
		input.CheckpointFile = input.DownloadFile + ".downloadfile_record"
	}

	if input.TaskNum <= 0 {
		input.TaskNum = 1
	}
	if input.PartSize <= 0 {
		input.PartSize = DEFAULT_PART_SIZE
	}

	output, err = obsClient.resumeDownload(input, extensions)
	return
}
