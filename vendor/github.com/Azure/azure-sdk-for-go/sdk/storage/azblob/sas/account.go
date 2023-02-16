//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package sas

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
)

// SharedKeyCredential contains an account's name and its primary or secondary key.
type SharedKeyCredential = exported.SharedKeyCredential

// UserDelegationCredential contains an account's name and its user delegation key.
type UserDelegationCredential = exported.UserDelegationCredential

// AccountSignatureValues is used to generate a Shared Access Signature (SAS) for an Azure Storage account.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/constructing-an-account-sas
type AccountSignatureValues struct {
	Version       string    `param:"sv"`  // If not specified, this format to SASVersion
	Protocol      Protocol  `param:"spr"` // See the SASProtocol* constants
	StartTime     time.Time `param:"st"`  // Not specified if IsZero
	ExpiryTime    time.Time `param:"se"`  // Not specified if IsZero
	Permissions   string    `param:"sp"`  // Create by initializing a AccountSASPermissions and then call String()
	IPRange       IPRange   `param:"sip"`
	Services      string    `param:"ss"`  // Create by initializing AccountSASServices and then call String()
	ResourceTypes string    `param:"srt"` // Create by initializing AccountSASResourceTypes and then call String()
}

// SignWithSharedKey uses an account's shared key credential to sign this signature values to produce
// the proper SAS query parameters.
func (v AccountSignatureValues) SignWithSharedKey(sharedKeyCredential *SharedKeyCredential) (QueryParameters, error) {
	// https://docs.microsoft.com/en-us/rest/api/storageservices/Constructing-an-Account-SAS
	if v.ExpiryTime.IsZero() || v.Permissions == "" || v.ResourceTypes == "" || v.Services == "" {
		return QueryParameters{}, errors.New("account SAS is missing at least one of these: ExpiryTime, Permissions, Service, or ResourceType")
	}
	if v.Version == "" {
		v.Version = Version
	}
	perms, err := parseAccountPermissions(v.Permissions)
	if err != nil {
		return QueryParameters{}, err
	}
	v.Permissions = perms.String()

	startTime, expiryTime, _ := formatTimesForSigning(v.StartTime, v.ExpiryTime, time.Time{})

	stringToSign := strings.Join([]string{
		sharedKeyCredential.AccountName(),
		v.Permissions,
		v.Services,
		v.ResourceTypes,
		startTime,
		expiryTime,
		v.IPRange.String(),
		string(v.Protocol),
		v.Version,
		""}, // That is right, the account SAS requires a terminating extra newline
		"\n")

	signature, err := exported.ComputeHMACSHA256(sharedKeyCredential, stringToSign)
	if err != nil {
		return QueryParameters{}, err
	}
	p := QueryParameters{
		// Common SAS parameters
		version:     v.Version,
		protocol:    v.Protocol,
		startTime:   v.StartTime,
		expiryTime:  v.ExpiryTime,
		permissions: v.Permissions,
		ipRange:     v.IPRange,

		// Account-specific SAS parameters
		services:      v.Services,
		resourceTypes: v.ResourceTypes,

		// Calculated SAS signature
		signature: signature,
	}

	return p, nil
}

// SignWithUserDelegation uses an account's UserDelegationKey to sign this signature values to produce the proper SAS query parameters.
func (v AccountSignatureValues) SignWithUserDelegation(userDelegationCredential *UserDelegationCredential) (QueryParameters, error) {
	// https://docs.microsoft.com/en-us/rest/api/storageservices/Constructing-an-Account-SAS
	if v.ExpiryTime.IsZero() || v.Permissions == "" || v.ResourceTypes == "" || v.Services == "" {
		return QueryParameters{}, errors.New("account SAS is missing at least one of these: ExpiryTime, Permissions, Service, or ResourceType")
	}
	if v.Version == "" {
		v.Version = Version
	}

	perms, err := parseAccountPermissions(v.Permissions)
	if err != nil {
		return QueryParameters{}, err
	}
	v.Permissions = perms.String()

	startTime, expiryTime, _ := formatTimesForSigning(v.StartTime, v.ExpiryTime, time.Time{})

	stringToSign := strings.Join([]string{
		exported.GetAccountName(userDelegationCredential),
		v.Permissions,
		v.Services,
		v.ResourceTypes,
		startTime,
		expiryTime,
		v.IPRange.String(),
		string(v.Protocol),
		v.Version,
		""}, // That is right, the account SAS requires a terminating extra newline
		"\n")

	signature, err := exported.ComputeUDCHMACSHA256(userDelegationCredential, stringToSign)
	if err != nil {
		return QueryParameters{}, err
	}
	p := QueryParameters{
		// Common SAS parameters
		version:     v.Version,
		protocol:    v.Protocol,
		startTime:   v.StartTime,
		expiryTime:  v.ExpiryTime,
		permissions: v.Permissions,
		ipRange:     v.IPRange,

		// Account-specific SAS parameters
		services:      v.Services,
		resourceTypes: v.ResourceTypes,

		// Calculated SAS signature
		signature: signature,
	}

	udk := exported.GetUDKParams(userDelegationCredential)

	//User delegation SAS specific parameters
	p.signedOID = *udk.SignedOID
	p.signedTID = *udk.SignedTID
	p.signedStart = *udk.SignedStart
	p.signedExpiry = *udk.SignedExpiry
	p.signedService = *udk.SignedService
	p.signedVersion = *udk.SignedVersion

	return p, nil
}

// AccountPermissions type simplifies creating the permissions string for an Azure Storage Account SAS.
// Initialize an instance of this type and then call its String method to set AccountSASSignatureValues's Permissions field.
type AccountPermissions struct {
	Read, Write, Delete, DeletePreviousVersion, List, Add, Create, Update, Process, Tag, FilterByTags bool
}

// String produces the SAS permissions string for an Azure Storage account.
// Call this method to set AccountSASSignatureValues's Permissions field.
func (p *AccountPermissions) String() string {
	var buffer bytes.Buffer
	if p.Read {
		buffer.WriteRune('r')
	}
	if p.Write {
		buffer.WriteRune('w')
	}
	if p.Delete {
		buffer.WriteRune('d')
	}
	if p.DeletePreviousVersion {
		buffer.WriteRune('x')
	}
	if p.List {
		buffer.WriteRune('l')
	}
	if p.Add {
		buffer.WriteRune('a')
	}
	if p.Create {
		buffer.WriteRune('c')
	}
	if p.Update {
		buffer.WriteRune('u')
	}
	if p.Process {
		buffer.WriteRune('p')
	}
	if p.Tag {
		buffer.WriteRune('t')
	}
	if p.FilterByTags {
		buffer.WriteRune('f')
	}
	return buffer.String()
}

// Parse initializes the AccountSASPermissions' fields from a string.
func parseAccountPermissions(s string) (AccountPermissions, error) {
	p := AccountPermissions{} // Clear out the flags
	for _, r := range s {
		switch r {
		case 'r':
			p.Read = true
		case 'w':
			p.Write = true
		case 'd':
			p.Delete = true
		case 'l':
			p.List = true
		case 'a':
			p.Add = true
		case 'c':
			p.Create = true
		case 'u':
			p.Update = true
		case 'p':
			p.Process = true
		case 'x':
			p.Process = true
		case 't':
			p.Tag = true
		case 'f':
			p.FilterByTags = true
		default:
			return AccountPermissions{}, fmt.Errorf("invalid permission character: '%v'", r)
		}
	}
	return p, nil
}

// AccountServices type simplifies creating the services string for an Azure Storage Account SAS.
// Initialize an instance of this type and then call its String method to set AccountSASSignatureValues's Services field.
type AccountServices struct {
	Blob, Queue, File bool
}

// String produces the SAS services string for an Azure Storage account.
// Call this method to set AccountSASSignatureValues's Services field.
func (s *AccountServices) String() string {
	var buffer bytes.Buffer
	if s.Blob {
		buffer.WriteRune('b')
	}
	if s.Queue {
		buffer.WriteRune('q')
	}
	if s.File {
		buffer.WriteRune('f')
	}
	return buffer.String()
}

// Parse initializes the AccountSASServices' fields from a string.
/*func parseAccountServices(str string) (AccountServices, error) {
	s := AccountServices{} // Clear out the flags
	for _, r := range str {
		switch r {
		case 'b':
			s.Blob = true
		case 'q':
			s.Queue = true
		case 'f':
			s.File = true
		default:
			return AccountServices{}, fmt.Errorf("invalid service character: '%v'", r)
		}
	}
	return s, nil
}*/

// AccountResourceTypes type simplifies creating the resource types string for an Azure Storage Account SAS.
// Initialize an instance of this type and then call its String method to set AccountSASSignatureValues's ResourceTypes field.
type AccountResourceTypes struct {
	Service, Container, Object bool
}

// String produces the SAS resource types string for an Azure Storage account.
// Call this method to set AccountSASSignatureValues's ResourceTypes field.
func (rt *AccountResourceTypes) String() string {
	var buffer bytes.Buffer
	if rt.Service {
		buffer.WriteRune('s')
	}
	if rt.Container {
		buffer.WriteRune('c')
	}
	if rt.Object {
		buffer.WriteRune('o')
	}
	return buffer.String()
}

// Parse initializes the AccountResourceTypes's fields from a string.
/*func parseAccountResourceTypes(s string) (AccountResourceTypes, error) {
	rt := AccountResourceTypes{} // Clear out the flags
	for _, r := range s {
		switch r {
		case 's':
			rt.Service = true
		case 'c':
			rt.Container = true
		case 'o':
			rt.Object = true
		default:
			return AccountResourceTypes{}, fmt.Errorf("invalid resource type: '%v'", r)
		}
	}
	return rt, nil
}*/
