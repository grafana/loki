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
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BucketInfo container for bucket metadata.
type BucketInfo struct {
	// The name of the bucket.
	Name string `json:"name"`
	// Date the bucket was created.
	CreationDate time.Time `json:"creationDate"`
}

// StringMap represents map with custom UnmarshalXML
type StringMap map[string]string

// UnmarshalXML unmarshals the XML into a map of string to strings,
// creating a key in the map for each tag and setting it's value to the
// tags contents.
//
// The fact this function is on the pointer of Map is important, so that
// if m is nil it can be initialized, which is often the case if m is
// nested in another xml structural. This is also why the first thing done
// on the first line is initialize it.
func (m *StringMap) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	*m = StringMap{}
	for {
		// Format is <key>value</key>
		var e struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		}
		err := d.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		(*m)[e.XMLName.Local] = e.Value
	}
	return nil
}

// URLMap represents map with custom UnmarshalXML
type URLMap map[string]string

// UnmarshalXML unmarshals the XML into a map of string to strings,
// creating a key in the map for each tag and setting it's value to the
// tags contents.
//
// The fact this function is on the pointer of Map is important, so that
// if m is nil it can be initialized, which is often the case if m is
// nested in another xml structural. This is also why the first thing done
// on the first line is initialize it.
func (m *URLMap) UnmarshalXML(d *xml.Decoder, se xml.StartElement) error {
	*m = URLMap{}
	var tgs string
	if err := d.DecodeElement(&tgs, &se); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	for tgs != "" {
		var key string
		key, tgs, _ = stringsCut(tgs, "&")
		if key == "" {
			continue
		}
		key, value, _ := stringsCut(key, "=")
		key, err := url.QueryUnescape(key)
		if err != nil {
			return err
		}

		value, err = url.QueryUnescape(value)
		if err != nil {
			return err
		}
		(*m)[key] = value
	}
	return nil
}

// stringsCut slices s around the first instance of sep,
// returning the text before and after sep.
// The found result reports whether sep appears in s.
// If sep does not appear in s, cut returns s, "", false.
func stringsCut(s, sep string) (before, after string, found bool) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// Owner name.
type Owner struct {
	XMLName     xml.Name `xml:"Owner" json:"owner"`
	DisplayName string   `xml:"ID" json:"name"`
	ID          string   `xml:"DisplayName" json:"id"`
}

// UploadInfo contains information about the
// newly uploaded or copied object.
type UploadInfo struct {
	Bucket       string
	Key          string
	ETag         string
	Size         int64
	LastModified time.Time
	Location     string
	VersionID    string

	// Lifecycle expiry-date and ruleID associated with the expiry
	// not to be confused with `Expires` HTTP header.
	Expiration       time.Time
	ExpirationRuleID string

	// Verified checksum values, if any.
	// Values are base64 (standard) encoded.
	// For multipart objects this is a checksum of the checksum of each part.
	ChecksumCRC32  string
	ChecksumCRC32C string
	ChecksumSHA1   string
	ChecksumSHA256 string
}

// RestoreInfo contains information of the restore operation of an archived object
type RestoreInfo struct {
	// Is the restoring operation is still ongoing
	OngoingRestore bool
	// When the restored copy of the archived object will be removed
	ExpiryTime time.Time
}

// ObjectInfo container for object metadata.
type ObjectInfo struct {
	// An ETag is optionally set to md5sum of an object.  In case of multipart objects,
	// ETag is of the form MD5SUM-N where MD5SUM is md5sum of all individual md5sums of
	// each parts concatenated into one string.
	ETag string `json:"etag"`

	Key          string    `json:"name"`         // Name of the object
	LastModified time.Time `json:"lastModified"` // Date and time the object was last modified.
	Size         int64     `json:"size"`         // Size in bytes of the object.
	ContentType  string    `json:"contentType"`  // A standard MIME type describing the format of the object data.
	Expires      time.Time `json:"expires"`      // The date and time at which the object is no longer able to be cached.

	// Collection of additional metadata on the object.
	// eg: x-amz-meta-*, content-encoding etc.
	Metadata http.Header `json:"metadata" xml:"-"`

	// x-amz-meta-* headers stripped "x-amz-meta-" prefix containing the first value.
	// Only returned by MinIO servers.
	UserMetadata StringMap `json:"userMetadata,omitempty"`

	// x-amz-tagging values in their k/v values.
	// Only returned by MinIO servers.
	UserTags URLMap `json:"userTags,omitempty" xml:"UserTags"`

	// x-amz-tagging-count value
	UserTagCount int

	// Owner name.
	Owner Owner

	// ACL grant.
	Grant []Grant

	// The class of storage used to store the object.
	StorageClass string `json:"storageClass"`

	// Versioning related information
	IsLatest       bool
	IsDeleteMarker bool
	VersionID      string `xml:"VersionId"`

	// x-amz-replication-status value is either in one of the following states
	// - COMPLETED
	// - PENDING
	// - FAILED
	// - REPLICA (on the destination)
	ReplicationStatus string `xml:"ReplicationStatus"`
	// set to true if delete marker has backing object version on target, and eligible to replicate
	ReplicationReady bool
	// Lifecycle expiry-date and ruleID associated with the expiry
	// not to be confused with `Expires` HTTP header.
	Expiration       time.Time
	ExpirationRuleID string

	Restore *RestoreInfo

	// Checksum values
	ChecksumCRC32  string
	ChecksumCRC32C string
	ChecksumSHA1   string
	ChecksumSHA256 string

	Internal *struct {
		K int // Data blocks
		M int // Parity blocks
	} `xml:"Internal"`

	// Error
	Err error `json:"-"`
}

// ObjectMultipartInfo container for multipart object metadata.
type ObjectMultipartInfo struct {
	// Date and time at which the multipart upload was initiated.
	Initiated time.Time `type:"timestamp" timestampFormat:"iso8601"`

	Initiator initiator
	Owner     owner

	// The type of storage to use for the object. Defaults to 'STANDARD'.
	StorageClass string

	// Key of the object for which the multipart upload was initiated.
	Key string

	// Size in bytes of the object.
	Size int64

	// Upload ID that identifies the multipart upload.
	UploadID string `xml:"UploadId"`

	// Error
	Err error
}
