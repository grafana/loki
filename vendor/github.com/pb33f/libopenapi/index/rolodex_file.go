// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"os"
	"time"

	"github.com/pb33f/libopenapi/datamodel"
	"go.yaml.in/yaml/v4"
)

type rolodexFile struct {
	location   string
	rolodex    *Rolodex
	index      *SpecIndex
	localFile  *LocalFile
	remoteFile *RemoteFile
}

func (rf *rolodexFile) Name() string {
	if rf.localFile != nil {
		return rf.localFile.filename
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.filename
	}
	return ""
}

func (rf *rolodexFile) GetIndex() *SpecIndex {
	if rf.localFile != nil {
		return rf.localFile.GetIndex()
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.GetIndex()
	}
	return nil
}

func (rf *rolodexFile) Index(config *SpecIndexConfig) (*SpecIndex, error) {
	if rf.index != nil {
		return rf.index, nil
	}
	var content []byte
	if rf.localFile != nil {
		content = rf.localFile.data
	}
	if rf.remoteFile != nil {
		content = rf.remoteFile.data
	}

	// first, we must parse the content of the file
	info, err := datamodel.ExtractSpecInfoWithDocumentCheckSync(content, config.SkipDocumentCheck)
	if err != nil {
		return nil, err
	}

	// create a new index for this file and link it to this rolodex.
	config.Rolodex = rf.rolodex
	index := NewSpecIndexWithConfig(info.RootNode, config)
	rf.index = index
	return index, nil
}

func (rf *rolodexFile) GetContent() string {
	if rf.localFile != nil {
		return string(rf.localFile.data)
	}
	if rf.remoteFile != nil {
		return string(rf.remoteFile.data)
	}
	return ""
}

func (rf *rolodexFile) GetContentAsYAMLNode() (*yaml.Node, error) {
	if rf.localFile != nil {
		return rf.localFile.GetContentAsYAMLNode()
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.GetContentAsYAMLNode()
	}
	return nil, nil
}

func (rf *rolodexFile) GetFileExtension() FileExtension {
	if rf.localFile != nil {
		return rf.localFile.extension
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.extension
	}
	return UNSUPPORTED
}

func (rf *rolodexFile) GetFullPath() string {
	if rf.localFile != nil {
		return rf.localFile.fullPath
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.fullPath
	}
	return ""
}

func (rf *rolodexFile) ModTime() time.Time {
	if rf.localFile != nil {
		return rf.localFile.lastModified
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.lastModified
	}
	return time.Now()
}

func (rf *rolodexFile) Size() int64 {
	if rf.localFile != nil {
		return rf.localFile.Size()
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.Size()
	}
	return 0
}

func (rf *rolodexFile) IsDir() bool {
	// always false.
	return false
}

func (rf *rolodexFile) Sys() interface{} {
	// not implemented.
	return nil
}

func (rf *rolodexFile) Mode() os.FileMode {
	if rf.localFile != nil {
		return rf.localFile.Mode()
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.Mode()
	}
	return os.FileMode(0)
}

func (rf *rolodexFile) GetErrors() []error {
	if rf.localFile != nil {
		return rf.localFile.readingErrors
	}
	if rf.remoteFile != nil {
		return rf.remoteFile.seekingErrors
	}
	return nil
}

func (rf *rolodexFile) WaitForIndexing() {
	if rf.localFile != nil {
		rf.localFile.WaitForIndexing()
		return
	}
	if rf.remoteFile != nil {
		rf.remoteFile.WaitForIndexing()
		return
	}
}
