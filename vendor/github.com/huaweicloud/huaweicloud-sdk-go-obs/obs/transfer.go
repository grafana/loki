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
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
)

var errAbort = errors.New("AbortError")

// FileStatus defines the upload file properties
type FileStatus struct {
	XMLName      xml.Name `xml:"FileInfo"`
	LastModified int64    `xml:"LastModified"`
	Size         int64    `xml:"Size"`
}

// UploadPartInfo defines the upload part properties
type UploadPartInfo struct {
	XMLName     xml.Name `xml:"UploadPart"`
	PartNumber  int      `xml:"PartNumber"`
	Etag        string   `xml:"Etag"`
	PartSize    int64    `xml:"PartSize"`
	Offset      int64    `xml:"Offset"`
	IsCompleted bool     `xml:"IsCompleted"`
}

// UploadCheckpoint defines the upload checkpoint file properties
type UploadCheckpoint struct {
	XMLName     xml.Name         `xml:"UploadFileCheckpoint"`
	Bucket      string           `xml:"Bucket"`
	Key         string           `xml:"Key"`
	UploadId    string           `xml:"UploadId,omitempty"`
	UploadFile  string           `xml:"FileUrl"`
	FileInfo    FileStatus       `xml:"FileInfo"`
	UploadParts []UploadPartInfo `xml:"UploadParts>UploadPart"`
}

func (ufc *UploadCheckpoint) isValid(bucket, key, uploadFile string, fileStat os.FileInfo) bool {
	if ufc.Bucket != bucket || ufc.Key != key || ufc.UploadFile != uploadFile {
		doLog(LEVEL_INFO, "Checkpoint file is invalid, the bucketName or objectKey or uploadFile was changed. clear the record.")
		return false
	}

	if ufc.FileInfo.Size != fileStat.Size() || ufc.FileInfo.LastModified != fileStat.ModTime().Unix() {
		doLog(LEVEL_INFO, "Checkpoint file is invalid, the uploadFile was changed. clear the record.")
		return false
	}

	if ufc.UploadId == "" {
		doLog(LEVEL_INFO, "UploadId is invalid. clear the record.")
		return false
	}

	return true
}

type uploadPartTask struct {
	UploadPartInput
	obsClient        *ObsClient
	abort            *int32
	extensions       []extensionOptions
	enableCheckpoint bool
}

func (task *uploadPartTask) Run() interface{} {
	if atomic.LoadInt32(task.abort) == 1 {
		return errAbort
	}

	input := &UploadPartInput{}
	input.Bucket = task.Bucket
	input.Key = task.Key
	input.PartNumber = task.PartNumber
	input.UploadId = task.UploadId
	input.SseHeader = task.SseHeader
	input.SourceFile = task.SourceFile
	input.Offset = task.Offset
	input.PartSize = task.PartSize
	input.ContentMD5 = task.ContentMD5
	input.ContentSHA256 = task.ContentSHA256

	extensions := task.extensions

	var output *UploadPartOutput
	var err error
	if len(extensions) != 0 {
		output, err = task.obsClient.UploadPart(input, extensions...)
	} else {
		output, err = task.obsClient.UploadPart(input)
	}

	if err == nil {
		if output.ETag == "" {
			doLog(LEVEL_WARN, "Get invalid etag value after uploading part [%d].", task.PartNumber)
			if !task.enableCheckpoint {
				atomic.CompareAndSwapInt32(task.abort, 0, 1)
				doLog(LEVEL_WARN, "Task is aborted, part number is [%d]", task.PartNumber)
			}
			return fmt.Errorf("get invalid etag value after uploading part [%d]", task.PartNumber)
		}
		return output
	} else if obsError, ok := err.(ObsError); ok && obsError.StatusCode >= 400 && obsError.StatusCode < 500 {
		atomic.CompareAndSwapInt32(task.abort, 0, 1)
		doLog(LEVEL_WARN, "Task is aborted, part number is [%d]", task.PartNumber)
	}
	return err
}

func loadCheckpointFile(checkpointFile string, result interface{}) error {
	ret, err := ioutil.ReadFile(checkpointFile)
	if err != nil {
		return err
	}
	if len(ret) == 0 {
		return nil
	}
	return xml.Unmarshal(ret, result)
}

func updateCheckpointFile(fc interface{}, checkpointFilePath string) error {
	result, err := xml.Marshal(fc)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(checkpointFilePath, result, 0640)
	return err
}

func getCheckpointFile(ufc *UploadCheckpoint, uploadFileStat os.FileInfo, input *UploadFileInput, obsClient *ObsClient, extensions []extensionOptions) (needCheckpoint bool, err error) {
	checkpointFilePath := input.CheckpointFile
	checkpointFileStat, err := os.Stat(checkpointFilePath)
	if err != nil {
		doLog(LEVEL_DEBUG, fmt.Sprintf("Stat checkpoint file failed with error: [%v].", err))
		return true, nil
	}
	if checkpointFileStat.IsDir() {
		doLog(LEVEL_ERROR, "Checkpoint file can not be a folder.")
		return false, errors.New("checkpoint file can not be a folder")
	}
	err = loadCheckpointFile(checkpointFilePath, ufc)
	if err != nil {
		doLog(LEVEL_WARN, fmt.Sprintf("Load checkpoint file failed with error: [%v].", err))
		return true, nil
	} else if !ufc.isValid(input.Bucket, input.Key, input.UploadFile, uploadFileStat) {
		if ufc.Bucket != "" && ufc.Key != "" && ufc.UploadId != "" {
			_err := abortTask(ufc.Bucket, ufc.Key, ufc.UploadId, obsClient, extensions)
			if _err != nil {
				doLog(LEVEL_WARN, "Failed to abort upload task [%s].", ufc.UploadId)
			}
		}
		_err := os.Remove(checkpointFilePath)
		if _err != nil {
			doLog(LEVEL_WARN, fmt.Sprintf("Failed to remove checkpoint file with error: [%v].", _err))
		}
	} else {
		return false, nil
	}

	return true, nil
}

func prepareUpload(ufc *UploadCheckpoint, uploadFileStat os.FileInfo, input *UploadFileInput, obsClient *ObsClient, extensions []extensionOptions) error {
	initiateInput := &InitiateMultipartUploadInput{}
	initiateInput.ObjectOperationInput = input.ObjectOperationInput
	initiateInput.HttpHeader = HttpHeader{
		CacheControl:       input.CacheControl,
		ContentEncoding:    input.ContentEncoding,
		ContentType:        input.ContentType,
		ContentDisposition: input.ContentDisposition,
		ContentLanguage:    input.ContentLanguage,
		HttpExpires:        input.HttpExpires,
	}
	initiateInput.EncodingType = input.EncodingType
	var output *InitiateMultipartUploadOutput
	var err error
	if len(extensions) != 0 {
		output, err = obsClient.InitiateMultipartUpload(initiateInput, extensions...)
	} else {
		output, err = obsClient.InitiateMultipartUpload(initiateInput)
	}
	if err != nil {
		return err
	}

	ufc.Bucket = input.Bucket
	ufc.Key = input.Key
	ufc.UploadFile = input.UploadFile
	ufc.FileInfo = FileStatus{}
	ufc.FileInfo.Size = uploadFileStat.Size()
	ufc.FileInfo.LastModified = uploadFileStat.ModTime().Unix()
	ufc.UploadId = output.UploadId

	err = sliceFile(input.PartSize, ufc)
	return err
}

func sliceFile(partSize int64, ufc *UploadCheckpoint) error {
	fileSize := ufc.FileInfo.Size
	cnt := fileSize / partSize
	if cnt >= 10000 {
		partSize = fileSize / 10000
		if fileSize%10000 != 0 {
			partSize++
		}
		cnt = fileSize / partSize
	}
	if fileSize%partSize != 0 {
		cnt++
	}

	if partSize > MAX_PART_SIZE {
		doLog(LEVEL_ERROR, "The source upload file is too large")
		return fmt.Errorf("The source upload file is too large")
	}

	if cnt == 0 {
		uploadPart := UploadPartInfo{}
		uploadPart.PartNumber = 1
		ufc.UploadParts = []UploadPartInfo{uploadPart}
	} else {
		uploadParts := make([]UploadPartInfo, 0, cnt)
		var i int64
		for i = 0; i < cnt; i++ {
			uploadPart := UploadPartInfo{}
			uploadPart.PartNumber = int(i) + 1
			uploadPart.PartSize = partSize
			uploadPart.Offset = i * partSize
			uploadParts = append(uploadParts, uploadPart)
		}
		if value := fileSize % partSize; value != 0 {
			uploadParts[cnt-1].PartSize = value
		}
		ufc.UploadParts = uploadParts
	}
	return nil
}

func abortTask(bucket, key, uploadID string, obsClient *ObsClient, extensions []extensionOptions) error {
	input := &AbortMultipartUploadInput{}
	input.Bucket = bucket
	input.Key = key
	input.UploadId = uploadID
	if len(extensions) != 0 {
		_, err := obsClient.AbortMultipartUpload(input, extensions...)
		return err
	}
	_, err := obsClient.AbortMultipartUpload(input)
	return err
}

func handleUploadFileResult(uploadPartError error, ufc *UploadCheckpoint, enableCheckpoint bool, obsClient *ObsClient, extensions []extensionOptions) error {
	if uploadPartError != nil {
		if enableCheckpoint {
			return uploadPartError
		}
		_err := abortTask(ufc.Bucket, ufc.Key, ufc.UploadId, obsClient, extensions)
		if _err != nil {
			doLog(LEVEL_WARN, "Failed to abort task [%s].", ufc.UploadId)
		}
		return uploadPartError
	}
	return nil
}

func completeParts(ufc *UploadCheckpoint, enableCheckpoint bool, checkpointFilePath string, obsClient *ObsClient, encodingType string, extensions []extensionOptions) (output *CompleteMultipartUploadOutput, err error) {
	completeInput := &CompleteMultipartUploadInput{}
	completeInput.Bucket = ufc.Bucket
	completeInput.Key = ufc.Key
	completeInput.UploadId = ufc.UploadId
	completeInput.EncodingType = encodingType
	parts := make([]Part, 0, len(ufc.UploadParts))
	for _, uploadPart := range ufc.UploadParts {
		part := Part{}
		part.PartNumber = uploadPart.PartNumber
		part.ETag = uploadPart.Etag
		parts = append(parts, part)
	}
	completeInput.Parts = parts
	var completeOutput *CompleteMultipartUploadOutput
	if len(extensions) != 0 {
		completeOutput, err = obsClient.CompleteMultipartUpload(completeInput, extensions...)
	} else {
		completeOutput, err = obsClient.CompleteMultipartUpload(completeInput)
	}

	if err == nil {
		if enableCheckpoint {
			_err := os.Remove(checkpointFilePath)
			if _err != nil {
				doLog(LEVEL_WARN, "Upload file successfully, but remove checkpoint file failed with error [%v].", _err)
			}
		}
		return completeOutput, err
	}
	if !enableCheckpoint {
		_err := abortTask(ufc.Bucket, ufc.Key, ufc.UploadId, obsClient, extensions)
		if _err != nil {
			doLog(LEVEL_WARN, "Failed to abort task [%s].", ufc.UploadId)
		}
	}
	return completeOutput, err
}

func (obsClient ObsClient) resumeUpload(input *UploadFileInput, extensions []extensionOptions) (output *CompleteMultipartUploadOutput, err error) {
	uploadFileStat, err := os.Stat(input.UploadFile)
	if err != nil {
		doLog(LEVEL_ERROR, fmt.Sprintf("Failed to stat uploadFile with error: [%v].", err))
		return nil, err
	}
	if uploadFileStat.IsDir() {
		doLog(LEVEL_ERROR, "UploadFile can not be a folder.")
		return nil, errors.New("uploadFile can not be a folder")
	}

	ufc := &UploadCheckpoint{}

	var needCheckpoint = true
	var checkpointFilePath = input.CheckpointFile
	var enableCheckpoint = input.EnableCheckpoint
	if enableCheckpoint {
		needCheckpoint, err = getCheckpointFile(ufc, uploadFileStat, input, &obsClient, extensions)
		if err != nil {
			return nil, err
		}
	}
	if needCheckpoint {
		err = prepareUpload(ufc, uploadFileStat, input, &obsClient, extensions)
		if err != nil {
			return nil, err
		}

		if enableCheckpoint {
			err = updateCheckpointFile(ufc, checkpointFilePath)
			if err != nil {
				doLog(LEVEL_ERROR, "Failed to update checkpoint file with error [%v].", err)
				_err := abortTask(ufc.Bucket, ufc.Key, ufc.UploadId, &obsClient, extensions)
				if _err != nil {
					doLog(LEVEL_WARN, "Failed to abort task [%s].", ufc.UploadId)
				}
				return nil, err
			}
		}
	}

	uploadPartError := obsClient.uploadPartConcurrent(ufc, checkpointFilePath, input, extensions)
	err = handleUploadFileResult(uploadPartError, ufc, enableCheckpoint, &obsClient, extensions)
	if err != nil {
		return nil, err
	}

	completeOutput, err := completeParts(ufc, enableCheckpoint, checkpointFilePath, &obsClient, input.EncodingType, extensions)

	return completeOutput, err
}

func handleUploadTaskResult(result interface{}, ufc *UploadCheckpoint, partNum int, enableCheckpoint bool, checkpointFilePath string, lock *sync.Mutex, completedBytes *int64, listener ProgressListener) (err error) {
	if uploadPartOutput, ok := result.(*UploadPartOutput); ok {
		lock.Lock()
		defer lock.Unlock()
		ufc.UploadParts[partNum-1].Etag = uploadPartOutput.ETag
		ufc.UploadParts[partNum-1].IsCompleted = true

		atomic.AddInt64(completedBytes, ufc.UploadParts[partNum-1].PartSize)

		event := newProgressEvent(TransferDataEvent, *completedBytes, ufc.FileInfo.Size)
		publishProgress(listener, event)

		if enableCheckpoint {
			_err := updateCheckpointFile(ufc, checkpointFilePath)
			if _err != nil {
				doLog(LEVEL_WARN, "Failed to update checkpoint file with error [%v].", _err)
			}
		}
	} else if result != errAbort {
		if _err, ok := result.(error); ok {
			err = _err
		}
	}
	return
}

func (obsClient ObsClient) uploadPartConcurrent(ufc *UploadCheckpoint, checkpointFilePath string, input *UploadFileInput, extensions []extensionOptions) error {
	pool := NewRoutinePool(input.TaskNum, MAX_PART_NUM)
	var uploadPartError atomic.Value
	var errFlag int32
	var abort int32
	lock := new(sync.Mutex)

	var completedBytes int64
	listener := obsClient.getProgressListener(extensions)
	totalBytes := ufc.FileInfo.Size
	event := newProgressEvent(TransferStartedEvent, 0, totalBytes)
	publishProgress(listener, event)

	for _, uploadPart := range ufc.UploadParts {
		if atomic.LoadInt32(&abort) == 1 {
			break
		}
		if uploadPart.IsCompleted {
			atomic.AddInt64(&completedBytes, uploadPart.PartSize)
			event := newProgressEvent(TransferDataEvent, completedBytes, ufc.FileInfo.Size)
			publishProgress(listener, event)
			continue
		}
		task := uploadPartTask{
			UploadPartInput: UploadPartInput{
				Bucket:     ufc.Bucket,
				Key:        ufc.Key,
				PartNumber: uploadPart.PartNumber,
				UploadId:   ufc.UploadId,
				SseHeader:  input.SseHeader,
				SourceFile: input.UploadFile,
				Offset:     uploadPart.Offset,
				PartSize:   uploadPart.PartSize,
			},
			obsClient:        &obsClient,
			abort:            &abort,
			extensions:       extensions,
			enableCheckpoint: input.EnableCheckpoint,
		}
		pool.ExecuteFunc(func() interface{} {
			result := task.Run()
			err := handleUploadTaskResult(result, ufc, task.PartNumber, input.EnableCheckpoint, input.CheckpointFile, lock, &completedBytes, listener)
			if err != nil && atomic.CompareAndSwapInt32(&errFlag, 0, 1) {
				uploadPartError.Store(err)
			}
			return nil
		})
	}
	pool.ShutDown()
	if err, ok := uploadPartError.Load().(error); ok {

		event := newProgressEvent(TransferFailedEvent, completedBytes, ufc.FileInfo.Size)
		publishProgress(listener, event)

		return err
	}
	event = newProgressEvent(TransferCompletedEvent, completedBytes, ufc.FileInfo.Size)
	publishProgress(listener, event)
	return nil
}

// ObjectInfo defines download object info
type ObjectInfo struct {
	XMLName      xml.Name `xml:"ObjectInfo"`
	LastModified int64    `xml:"LastModified"`
	Size         int64    `xml:"Size"`
	ETag         string   `xml:"ETag"`
}

// TempFileInfo defines temp download file properties
type TempFileInfo struct {
	XMLName     xml.Name `xml:"TempFileInfo"`
	TempFileUrl string   `xml:"TempFileUrl"`
	Size        int64    `xml:"Size"`
}

// DownloadPartInfo defines download part properties
type DownloadPartInfo struct {
	XMLName     xml.Name `xml:"DownloadPart"`
	PartNumber  int64    `xml:"PartNumber"`
	RangeEnd    int64    `xml:"RangeEnd"`
	Offset      int64    `xml:"Offset"`
	IsCompleted bool     `xml:"IsCompleted"`
}

// DownloadCheckpoint defines download checkpoint file properties
type DownloadCheckpoint struct {
	XMLName       xml.Name           `xml:"DownloadFileCheckpoint"`
	Bucket        string             `xml:"Bucket"`
	Key           string             `xml:"Key"`
	VersionId     string             `xml:"VersionId,omitempty"`
	DownloadFile  string             `xml:"FileUrl"`
	ObjectInfo    ObjectInfo         `xml:"ObjectInfo"`
	TempFileInfo  TempFileInfo       `xml:"TempFileInfo"`
	DownloadParts []DownloadPartInfo `xml:"DownloadParts>DownloadPart"`
}

func (dfc *DownloadCheckpoint) isValid(input *DownloadFileInput, output *GetObjectMetadataOutput) bool {
	if dfc.Bucket != input.Bucket || dfc.Key != input.Key || dfc.VersionId != input.VersionId || dfc.DownloadFile != input.DownloadFile {
		doLog(LEVEL_INFO, "Checkpoint file is invalid, the bucketName or objectKey or downloadFile was changed. clear the record.")
		return false
	}
	if dfc.ObjectInfo.LastModified != output.LastModified.Unix() || dfc.ObjectInfo.ETag != output.ETag || dfc.ObjectInfo.Size != output.ContentLength {
		doLog(LEVEL_INFO, "Checkpoint file is invalid, the object info was changed. clear the record.")
		return false
	}
	if dfc.TempFileInfo.Size != output.ContentLength {
		doLog(LEVEL_INFO, "Checkpoint file is invalid, size was changed. clear the record.")
		return false
	}
	stat, err := os.Stat(dfc.TempFileInfo.TempFileUrl)
	if err != nil || stat.Size() != dfc.ObjectInfo.Size {
		doLog(LEVEL_INFO, "Checkpoint file is invalid, the temp download file was changed. clear the record.")
		return false
	}

	return true
}

type downloadPartTask struct {
	GetObjectInput
	obsClient        *ObsClient
	extensions       []extensionOptions
	abort            *int32
	partNumber       int64
	tempFileURL      string
	enableCheckpoint bool
}

func (task *downloadPartTask) Run() interface{} {
	if atomic.LoadInt32(task.abort) == 1 {
		return errAbort
	}
	getObjectInput := &GetObjectInput{}
	getObjectInput.GetObjectMetadataInput = task.GetObjectMetadataInput
	getObjectInput.IfMatch = task.IfMatch
	getObjectInput.IfNoneMatch = task.IfNoneMatch
	getObjectInput.IfModifiedSince = task.IfModifiedSince
	getObjectInput.IfUnmodifiedSince = task.IfUnmodifiedSince
	getObjectInput.RangeStart = task.RangeStart
	getObjectInput.RangeEnd = task.RangeEnd
	getObjectInput.Range = fmt.Sprintf("bytes=%d-%d", getObjectInput.RangeStart, getObjectInput.RangeEnd)
	var output *GetObjectOutput
	var err error
	if len(task.extensions) != 0 {
		output, err = task.obsClient.GetObjectWithoutProgress(getObjectInput, task.extensions...)
	} else {
		output, err = task.obsClient.GetObjectWithoutProgress(getObjectInput)
	}

	if err == nil {
		defer func() {
			errMsg := output.Body.Close()
			if errMsg != nil {
				doLog(LEVEL_WARN, "Failed to close response body.")
			}
		}()
		_err := updateDownloadFile(task.tempFileURL, task.RangeStart, output)
		if _err != nil {
			if !task.enableCheckpoint {
				atomic.CompareAndSwapInt32(task.abort, 0, 1)
				doLog(LEVEL_WARN, "Task is aborted, part number is [%d]", task.partNumber)
			}
			return _err
		}
		return output
	} else if obsError, ok := err.(ObsError); ok && obsError.StatusCode >= 400 && obsError.StatusCode < 500 {
		atomic.CompareAndSwapInt32(task.abort, 0, 1)
		doLog(LEVEL_WARN, "Task is aborted, part number is [%d]", task.partNumber)
	}
	return err
}

func getObjectInfo(input *DownloadFileInput, obsClient *ObsClient, extensions []extensionOptions) (getObjectmetaOutput *GetObjectMetadataOutput, err error) {
	if len(extensions) != 0 {
		getObjectmetaOutput, err = obsClient.GetObjectMetadata(&input.GetObjectMetadataInput, extensions...)
	} else {
		getObjectmetaOutput, err = obsClient.GetObjectMetadata(&input.GetObjectMetadataInput)
	}

	return
}

func getDownloadCheckpointFile(dfc *DownloadCheckpoint, input *DownloadFileInput, output *GetObjectMetadataOutput) (needCheckpoint bool, err error) {
	checkpointFilePath := input.CheckpointFile
	checkpointFileStat, err := os.Stat(checkpointFilePath)
	if err != nil {
		doLog(LEVEL_DEBUG, fmt.Sprintf("Stat checkpoint file failed with error: [%v].", err))
		return true, nil
	}
	if checkpointFileStat.IsDir() {
		doLog(LEVEL_ERROR, "Checkpoint file can not be a folder.")
		return false, errors.New("checkpoint file can not be a folder")
	}
	err = loadCheckpointFile(checkpointFilePath, dfc)
	if err != nil {
		doLog(LEVEL_WARN, fmt.Sprintf("Load checkpoint file failed with error: [%v].", err))
		return true, nil
	} else if !dfc.isValid(input, output) {
		if dfc.TempFileInfo.TempFileUrl != "" {
			_err := os.Remove(dfc.TempFileInfo.TempFileUrl)
			if _err != nil {
				doLog(LEVEL_WARN, "Failed to remove temp download file with error [%v].", _err)
			}
		}
		_err := os.Remove(checkpointFilePath)
		if _err != nil {
			doLog(LEVEL_WARN, "Failed to remove checkpoint file with error [%v].", _err)
		}
	} else {
		return false, nil
	}

	return true, nil
}

func sliceObject(objectSize, partSize int64, dfc *DownloadCheckpoint) {
	cnt := objectSize / partSize
	if objectSize%partSize > 0 {
		cnt++
	}

	if cnt == 0 {
		downloadPart := DownloadPartInfo{}
		downloadPart.PartNumber = 1
		dfc.DownloadParts = []DownloadPartInfo{downloadPart}
	} else {
		downloadParts := make([]DownloadPartInfo, 0, cnt)
		var i int64
		for i = 0; i < cnt; i++ {
			downloadPart := DownloadPartInfo{}
			downloadPart.PartNumber = i + 1
			downloadPart.Offset = i * partSize
			downloadPart.RangeEnd = (i+1)*partSize - 1
			downloadParts = append(downloadParts, downloadPart)
		}
		dfc.DownloadParts = downloadParts
		if value := objectSize % partSize; value > 0 {
			dfc.DownloadParts[cnt-1].RangeEnd = dfc.ObjectInfo.Size - 1
		}
	}
}

func createFile(tempFileURL string, fileSize int64) error {
	fd, err := syscall.Open(tempFileURL, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		doLog(LEVEL_WARN, "Failed to open temp download file [%s].", tempFileURL)
		return err
	}
	defer func() {
		errMsg := syscall.Close(fd)
		if errMsg != nil {
			doLog(LEVEL_WARN, "Failed to close file with error [%v].", errMsg)
		}
	}()
	err = syscall.Ftruncate(fd, fileSize)
	if err != nil {
		doLog(LEVEL_WARN, "Failed to create file with error [%v].", err)
	}
	return err
}

func prepareTempFile(tempFileURL string, fileSize int64) error {
	parentDir := filepath.Dir(tempFileURL)
	stat, err := os.Stat(parentDir)
	if err != nil {
		doLog(LEVEL_DEBUG, "Failed to stat path with error [%v].", err)
		_err := os.MkdirAll(parentDir, os.ModePerm)
		if _err != nil {
			doLog(LEVEL_ERROR, "Failed to make dir with error [%v].", _err)
			return _err
		}
	} else if !stat.IsDir() {
		doLog(LEVEL_ERROR, "Cannot create folder [%s] due to a same file exists.", parentDir)
		return fmt.Errorf("cannot create folder [%s] due to a same file exists", parentDir)
	}

	err = createFile(tempFileURL, fileSize)
	if err == nil {
		return nil
	}
	fd, err := os.OpenFile(tempFileURL, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		doLog(LEVEL_ERROR, "Failed to open temp download file [%s].", tempFileURL)
		return err
	}
	defer func() {
		errMsg := fd.Close()
		if errMsg != nil {
			doLog(LEVEL_WARN, "Failed to close file with error [%v].", errMsg)
		}
	}()
	if fileSize > 0 {
		_, err = fd.WriteAt([]byte("a"), fileSize-1)
		if err != nil {
			doLog(LEVEL_ERROR, "Failed to create temp download file with error [%v].", err)
			return err
		}
	}

	return nil
}

func handleDownloadFileResult(tempFileURL string, enableCheckpoint bool, downloadFileError error) error {
	if downloadFileError != nil {
		if !enableCheckpoint {
			_err := os.Remove(tempFileURL)
			if _err != nil {
				doLog(LEVEL_WARN, "Failed to remove temp download file with error [%v].", _err)
			}
		}
		return downloadFileError
	}
	return nil
}

func (obsClient ObsClient) resumeDownload(input *DownloadFileInput, extensions []extensionOptions) (output *GetObjectMetadataOutput, err error) {
	getObjectmetaOutput, err := getObjectInfo(input, &obsClient, extensions)
	if err != nil {
		return nil, err
	}

	objectSize := getObjectmetaOutput.ContentLength
	partSize := input.PartSize
	dfc := &DownloadCheckpoint{}

	var needCheckpoint = true
	var checkpointFilePath = input.CheckpointFile
	var enableCheckpoint = input.EnableCheckpoint
	if enableCheckpoint {
		needCheckpoint, err = getDownloadCheckpointFile(dfc, input, getObjectmetaOutput)
		if err != nil {
			return nil, err
		}
	}

	if needCheckpoint {
		dfc.Bucket = input.Bucket
		dfc.Key = input.Key
		dfc.VersionId = input.VersionId
		dfc.DownloadFile = input.DownloadFile
		dfc.ObjectInfo = ObjectInfo{}
		dfc.ObjectInfo.LastModified = getObjectmetaOutput.LastModified.Unix()
		dfc.ObjectInfo.Size = getObjectmetaOutput.ContentLength
		dfc.ObjectInfo.ETag = getObjectmetaOutput.ETag
		dfc.TempFileInfo = TempFileInfo{}
		dfc.TempFileInfo.TempFileUrl = input.DownloadFile + ".tmp"
		dfc.TempFileInfo.Size = getObjectmetaOutput.ContentLength

		sliceObject(objectSize, partSize, dfc)
		_err := prepareTempFile(dfc.TempFileInfo.TempFileUrl, dfc.TempFileInfo.Size)
		if _err != nil {
			return nil, _err
		}

		if enableCheckpoint {
			_err := updateCheckpointFile(dfc, checkpointFilePath)
			if _err != nil {
				doLog(LEVEL_ERROR, "Failed to update checkpoint file with error [%v].", _err)
				_errMsg := os.Remove(dfc.TempFileInfo.TempFileUrl)
				if _errMsg != nil {
					doLog(LEVEL_WARN, "Failed to remove temp download file with error [%v].", _errMsg)
				}
				return nil, _err
			}
		}
	}

	downloadFileError := obsClient.downloadFileConcurrent(input, dfc, extensions)
	err = handleDownloadFileResult(dfc.TempFileInfo.TempFileUrl, enableCheckpoint, downloadFileError)
	if err != nil {
		return nil, err
	}

	err = os.Rename(dfc.TempFileInfo.TempFileUrl, input.DownloadFile)
	if err != nil {
		doLog(LEVEL_ERROR, "Failed to rename temp download file [%s] to download file [%s] with error [%v].", dfc.TempFileInfo.TempFileUrl, input.DownloadFile, err)
		return nil, err
	}
	if enableCheckpoint {
		err = os.Remove(checkpointFilePath)
		if err != nil {
			doLog(LEVEL_WARN, "Download file successfully, but remove checkpoint file failed with error [%v].", err)
		}
	}

	return getObjectmetaOutput, nil
}

func updateDownloadFile(filePath string, rangeStart int64, output *GetObjectOutput) error {
	fd, err := os.OpenFile(filePath, os.O_WRONLY, 0640)
	if err != nil {
		doLog(LEVEL_ERROR, "Failed to open file [%s].", filePath)
		return err
	}
	defer func() {
		errMsg := fd.Close()
		if errMsg != nil {
			doLog(LEVEL_WARN, "Failed to close file with error [%v].", errMsg)
		}
	}()
	_, err = fd.Seek(rangeStart, 0)
	if err != nil {
		doLog(LEVEL_ERROR, "Failed to seek file with error [%v].", err)
		return err
	}
	fileWriter := bufio.NewWriterSize(fd, 65536)
	part := make([]byte, 8192)
	var readErr error
	var readCount int
	for {
		readCount, readErr = output.Body.Read(part)
		if readCount > 0 {
			wcnt, werr := fileWriter.Write(part[0:readCount])
			if werr != nil {
				doLog(LEVEL_ERROR, "Failed to write to file with error [%v].", werr)
				return werr
			}
			if wcnt != readCount {
				doLog(LEVEL_ERROR, "Failed to write to file [%s], expect: [%d], actual: [%d]", filePath, readCount, wcnt)
				return fmt.Errorf("Failed to write to file [%s], expect: [%d], actual: [%d]", filePath, readCount, wcnt)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				doLog(LEVEL_ERROR, "Failed to read response body with error [%v].", readErr)
				return readErr
			}
			break
		}
	}
	err = fileWriter.Flush()
	if err != nil {
		doLog(LEVEL_ERROR, "Failed to flush file with error [%v].", err)
		return err
	}
	return nil
}

func handleDownloadTaskResult(result interface{}, dfc *DownloadCheckpoint, partNum int64, enableCheckpoint bool, checkpointFile string, lock *sync.Mutex, completedBytes *int64, listener ProgressListener) (err error) {
	if output, ok := result.(*GetObjectOutput); ok {
		lock.Lock()
		defer lock.Unlock()
		dfc.DownloadParts[partNum-1].IsCompleted = true

		atomic.AddInt64(completedBytes, output.ContentLength)

		event := newProgressEvent(TransferDataEvent, *completedBytes, dfc.ObjectInfo.Size)
		publishProgress(listener, event)

		if enableCheckpoint {
			_err := updateCheckpointFile(dfc, checkpointFile)
			if _err != nil {
				doLog(LEVEL_WARN, "Failed to update checkpoint file with error [%v].", _err)
			}
		}
	} else if result != errAbort {
		if _err, ok := result.(error); ok {
			err = _err
		}
	}
	return
}

func (obsClient ObsClient) downloadFileConcurrent(input *DownloadFileInput, dfc *DownloadCheckpoint, extensions []extensionOptions) error {
	pool := NewRoutinePool(input.TaskNum, MAX_PART_NUM)
	var downloadPartError atomic.Value
	var errFlag int32
	var abort int32
	lock := new(sync.Mutex)

	var completedBytes int64
	listener := obsClient.getProgressListener(extensions)
	totalBytes := dfc.ObjectInfo.Size
	event := newProgressEvent(TransferStartedEvent, 0, totalBytes)
	publishProgress(listener, event)

	for _, downloadPart := range dfc.DownloadParts {
		if atomic.LoadInt32(&abort) == 1 {
			break
		}
		if downloadPart.IsCompleted {
			atomic.AddInt64(&completedBytes, downloadPart.RangeEnd-downloadPart.Offset+1)
			event := newProgressEvent(TransferDataEvent, completedBytes, dfc.ObjectInfo.Size)
			publishProgress(listener, event)
			continue
		}
		task := downloadPartTask{
			GetObjectInput: GetObjectInput{
				GetObjectMetadataInput: input.GetObjectMetadataInput,
				IfMatch:                input.IfMatch,
				IfNoneMatch:            input.IfNoneMatch,
				IfUnmodifiedSince:      input.IfUnmodifiedSince,
				IfModifiedSince:        input.IfModifiedSince,
				RangeStart:             downloadPart.Offset,
				RangeEnd:               downloadPart.RangeEnd,
			},
			obsClient:        &obsClient,
			extensions:       extensions,
			abort:            &abort,
			partNumber:       downloadPart.PartNumber,
			tempFileURL:      dfc.TempFileInfo.TempFileUrl,
			enableCheckpoint: input.EnableCheckpoint,
		}
		pool.ExecuteFunc(func() interface{} {
			result := task.Run()
			err := handleDownloadTaskResult(result, dfc, task.partNumber, input.EnableCheckpoint, input.CheckpointFile, lock, &completedBytes, listener)
			if err != nil && atomic.CompareAndSwapInt32(&errFlag, 0, 1) {
				downloadPartError.Store(err)
			}
			return nil
		})
	}
	pool.ShutDown()
	if err, ok := downloadPartError.Load().(error); ok {
		event := newProgressEvent(TransferFailedEvent, completedBytes, dfc.ObjectInfo.Size)
		publishProgress(listener, event)
		return err
	}
	event = newProgressEvent(TransferCompletedEvent, completedBytes, dfc.ObjectInfo.Size)
	publishProgress(listener, event)
	return nil
}
