// Copyright 2019 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import "time"

// Bucket represents the bucket that is stored within the fake server.
type Bucket struct {
	Name                  string
	VersioningEnabled     bool
	TimeCreated           time.Time
	DefaultEventBasedHold bool
}

const bucketMetadataSuffix = ".bucketMetadata"

type BucketAttrs struct {
	DefaultEventBasedHold bool
	VersioningEnabled     bool
}
