//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package shared

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/uuid"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
)

const (
	TokenScope = "https://storage.azure.com/.default"
)

const (
	HeaderAuthorization     = "Authorization"
	HeaderXmsDate           = "x-ms-date"
	HeaderContentLength     = "Content-Length"
	HeaderContentEncoding   = "Content-Encoding"
	HeaderContentLanguage   = "Content-Language"
	HeaderContentType       = "Content-Type"
	HeaderContentMD5        = "Content-MD5"
	HeaderIfModifiedSince   = "If-Modified-Since"
	HeaderIfMatch           = "If-Match"
	HeaderIfNoneMatch       = "If-None-Match"
	HeaderIfUnmodifiedSince = "If-Unmodified-Since"
	HeaderRange             = "Range"
)

// CopyOptions returns a zero-value T if opts is nil.
// If opts is not nil, a copy is made and its address returned.
func CopyOptions[T any](opts *T) *T {
	if opts == nil {
		return new(T)
	}
	cp := *opts
	return &cp
}

var errConnectionString = errors.New("connection string is either blank or malformed. The expected connection string " +
	"should contain key value pairs separated by semicolons. For example 'DefaultEndpointsProtocol=https;AccountName=<accountName>;" +
	"AccountKey=<accountKey>;EndpointSuffix=core.windows.net'")

type ParsedConnectionString struct {
	ServiceURL  string
	AccountName string
	AccountKey  string
}

func ParseConnectionString(connectionString string) (ParsedConnectionString, error) {
	const (
		defaultScheme = "https"
		defaultSuffix = "core.windows.net"
	)

	connStrMap := make(map[string]string)
	connectionString = strings.TrimRight(connectionString, ";")

	splitString := strings.Split(connectionString, ";")
	if len(splitString) == 0 {
		return ParsedConnectionString{}, errConnectionString
	}
	for _, stringPart := range splitString {
		parts := strings.SplitN(stringPart, "=", 2)
		if len(parts) != 2 {
			return ParsedConnectionString{}, errConnectionString
		}
		connStrMap[parts[0]] = parts[1]
	}

	accountName, ok := connStrMap["AccountName"]
	if !ok {
		return ParsedConnectionString{}, errors.New("connection string missing AccountName")
	}

	accountKey, ok := connStrMap["AccountKey"]
	if !ok {
		sharedAccessSignature, ok := connStrMap["SharedAccessSignature"]
		if !ok {
			return ParsedConnectionString{}, errors.New("connection string missing AccountKey and SharedAccessSignature")
		}
		return ParsedConnectionString{
			ServiceURL: fmt.Sprintf("%v://%v.blob.%v/?%v", defaultScheme, accountName, defaultSuffix, sharedAccessSignature),
		}, nil
	}

	protocol, ok := connStrMap["DefaultEndpointsProtocol"]
	if !ok {
		protocol = defaultScheme
	}

	suffix, ok := connStrMap["EndpointSuffix"]
	if !ok {
		suffix = defaultSuffix
	}

	if blobEndpoint, ok := connStrMap["BlobEndpoint"]; ok {
		return ParsedConnectionString{
			ServiceURL:  blobEndpoint,
			AccountName: accountName,
			AccountKey:  accountKey,
		}, nil
	}

	return ParsedConnectionString{
		ServiceURL:  fmt.Sprintf("%v://%v.blob.%v", protocol, accountName, suffix),
		AccountName: accountName,
		AccountKey:  accountKey,
	}, nil
}

// SerializeBlobTags converts tags to generated.BlobTags
func SerializeBlobTags(tagsMap map[string]string) *generated.BlobTags {
	if tagsMap == nil {
		return nil
	}
	blobTagSet := make([]*generated.BlobTag, 0)
	for key, val := range tagsMap {
		newKey, newVal := key, val
		blobTagSet = append(blobTagSet, &generated.BlobTag{Key: &newKey, Value: &newVal})
	}
	return &generated.BlobTags{BlobTagSet: blobTagSet}
}

func SerializeBlobTagsToStrPtr(tagsMap map[string]string) *string {
	if tagsMap == nil {
		return nil
	}
	tags := make([]string, 0)
	for key, val := range tagsMap {
		tags = append(tags, url.QueryEscape(key)+"="+url.QueryEscape(val))
	}
	blobTagsString := strings.Join(tags, "&")
	return &blobTagsString
}

func ValidateSeekableStreamAt0AndGetCount(body io.ReadSeeker) (int64, error) {
	if body == nil { // nil body's are "logically" seekable to 0 and are 0 bytes long
		return 0, nil
	}

	err := validateSeekableStreamAt0(body)
	if err != nil {
		return 0, err
	}

	count, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, errors.New("body stream must be seekable")
	}

	_, err = body.Seek(0, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// return an error if body is not a valid seekable stream at 0
func validateSeekableStreamAt0(body io.ReadSeeker) error {
	if body == nil { // nil body's are "logically" seekable to 0
		return nil
	}
	if pos, err := body.Seek(0, io.SeekCurrent); pos != 0 || err != nil {
		// Help detect programmer error
		if err != nil {
			return errors.New("body stream must be seekable")
		}
		return errors.New("body stream must be set to position 0")
	}
	return nil
}

func RangeToString(offset, count int64) string {
	return "bytes=" + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(offset+count-1, 10)
}

type nopCloser struct {
	io.ReadSeeker
}

func (n nopCloser) Close() error {
	return nil
}

// NopCloser returns a ReadSeekCloser with a no-op close method wrapping the provided io.ReadSeeker.
func NopCloser(rs io.ReadSeeker) io.ReadSeekCloser {
	return nopCloser{rs}
}

func GenerateLeaseID(leaseID *string) (*string, error) {
	if leaseID == nil {
		generatedUuid, err := uuid.New()
		if err != nil {
			return nil, err
		}
		leaseID = to.Ptr(generatedUuid.String())
	}
	return leaseID, nil
}

func GetClientOptions[T any](o *T) *T {
	if o == nil {
		return new(T)
	}
	return o
}

// IsIPEndpointStyle checkes if URL's host is IP, in this case the storage account endpoint will be composed as:
// http(s)://IP(:port)/storageaccount/container/...
// As url's Host property, host could be both host or host:port
func IsIPEndpointStyle(host string) bool {
	if host == "" {
		return false
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	// For IPv6, there could be case where SplitHostPort fails for cannot finding port.
	// In this case, eliminate the '[' and ']' in the URL.
	// For details about IPv6 URL, please refer to https://tools.ietf.org/html/rfc2732
	if host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	return net.ParseIP(host) != nil
}
