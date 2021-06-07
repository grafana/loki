package persist

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path"
	"strings"
	"sync"
)

type (
	// FilePersister implements CheckpointPersister for saving to the file system
	FilePersister struct {
		directory string
		mu        sync.Mutex
	}
)

// NewFilePersister creates a FilePersister for saving to a given directory
func NewFilePersister(directory string) (*FilePersister, error) {
	err := os.MkdirAll(directory, 0777)
	return &FilePersister{
		directory: directory,
	}, err
}

func (fp *FilePersister) Write(namespace, name, consumerGroup, partitionID string, checkpoint Checkpoint) error {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	key := getFilePath(namespace, name, consumerGroup, partitionID)
	filePath := path.Join(fp.directory, key)
	bits, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	_, err = file.Write(bits)
	if err != nil {
		return err
	}

	return file.Close()
}

func (fp *FilePersister) Read(namespace, name, consumerGroup, partitionID string) (Checkpoint, error) {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	key := getFilePath(namespace, name, consumerGroup, partitionID)
	filePath := path.Join(fp.directory, key)

	f, err := os.Open(filePath)
	if err != nil {
		return NewCheckpointFromStartOfStream(), err
	}

	buf := bytes.NewBuffer(nil)
	_, err = io.Copy(buf, f)
	if err != nil {
		return NewCheckpointFromStartOfStream(), err
	}

	var checkpoint Checkpoint
	err = json.Unmarshal(buf.Bytes(), &checkpoint)
	return checkpoint, err
}

func getFilePath(namespace, name, consumerGroup, partitionID string) string {
	key := strings.Join([]string{namespace, name, consumerGroup, partitionID}, "_")
	return strings.Replace(key, "$", "", -1)
}
