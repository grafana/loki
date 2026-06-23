// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package azblob

import (
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
)

// SharedKeyCredential contains an account's name and its primary or secondary key.
type SharedKeyCredential = exported.SharedKeyCredential

// NewSharedKeyCredential creates an immutable SharedKeyCredential containing the
// storage account's name and either its primary or secondary key.
func NewSharedKeyCredential(accountName, accountKey string) (*SharedKeyCredential, error) {
	return exported.NewSharedKeyCredential(accountName, accountKey)
}

// URLParts object represents the components that make up an Azure Storage Container/Blob URL.
// NOTE: Changing any SAS-related field requires computing a new SAS signature.
type URLParts = sas.URLParts

// ParseURL parses a URL initializing URLParts' fields including any SAS-related & snapshot query parameters. Any other
// query parameters remain in the UnparsedParams field. This method overwrites all fields in the URLParts object.
func ParseURL(u string) (URLParts, error) {
	return sas.ParseURL(u)
}

// HTTPRange defines a range of bytes within an HTTP resource, starting at offset and
// ending at offset+count. A zero-value HTTPRange indicates the entire resource. An HTTPRange
// which has an offset and zero value count indicates from the offset to the resource's end.
type HTTPRange = exported.HTTPRange

// ExpectContinueMode is the mode for applying the HTTP "Expect: 100-continue" header to
// operations that include a request body.
type ExpectContinueMode = exported.ExpectContinueMode

const (
	// ExpectContinueModeApplyOnThrottle indicates that Expect-Continue will not be applied
	// until specific errors are encountered from the service, at which point it will be
	// applied for a fixed window of time after the last triggering error. This is the default.
	ExpectContinueModeApplyOnThrottle = exported.ExpectContinueModeApplyOnThrottle

	// ExpectContinueModeOn indicates Expect-Continue will be applied regardless of recent
	// error status. The ContentLengthThreshold option still applies.
	ExpectContinueModeOn = exported.ExpectContinueModeOn

	// ExpectContinueModeOff indicates Expect-Continue will never be applied.
	ExpectContinueModeOff = exported.ExpectContinueModeOff
)

// ExpectContinueOptions configures the behavior for applying the HTTP "Expect: 100-continue"
// header to operations that include a request body.
type ExpectContinueOptions = exported.ExpectContinueOptions
