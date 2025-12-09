// Copyright 2018 by David A. Golden. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package scram

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// NonceGeneratorFcn defines a function that returns a string of high-quality
// random printable ASCII characters EXCLUDING the comma (',') character.  The
// default nonce generator provides Base64 encoding of 24 bytes from
// crypto/rand.
type NonceGeneratorFcn func() string

// derivedKeys collects the three cryptographically derived values
// into one struct for caching.
type derivedKeys struct {
	ClientKey []byte
	StoredKey []byte
	ServerKey []byte
}

// KeyFactors represent the two server-provided factors needed to compute
// client credentials for authentication.  Salt is decoded bytes (i.e. not
// base64), but in string form so that KeyFactors can be used as a map key for
// cached credentials.
type KeyFactors struct {
	Salt  string
	Iters int
}

// StoredCredentials are the values that a server must store for a given
// username to allow authentication.  They include the salt and iteration
// count, plus the derived values to authenticate a client and for the server
// to authenticate itself back to the client.
//
// NOTE: these are specific to a given hash function.  To allow a user to
// authenticate with either SCRAM-SHA-1 or SCRAM-SHA-256, two sets of
// StoredCredentials must be created and stored, one for each hash function.
type StoredCredentials struct {
	KeyFactors
	StoredKey []byte
	ServerKey []byte
}

// CredentialLookup is a callback to provide StoredCredentials for a given
// username.  This is used to configure Server objects.
//
// NOTE: these are specific to a given hash function.  The callback provided
// to a Server with a given hash function must provide the corresponding
// StoredCredentials.
type CredentialLookup func(string) (StoredCredentials, error)

// Server error values as defined in RFC-5802 and RFC-7677. These are returned
// by the server in error responses as "e=<value>".
const (
	// ErrInvalidEncoding indicates the client message had invalid encoding
	ErrInvalidEncoding = "e=invalid-encoding"

	// ErrExtensionsNotSupported indicates unrecognized 'm' value
	ErrExtensionsNotSupported = "e=extensions-not-supported"

	// ErrInvalidProof indicates the authentication proof from the client was invalid
	ErrInvalidProof = "e=invalid-proof"

	// ErrChannelBindingsDontMatch indicates channel binding data didn't match expected value
	ErrChannelBindingsDontMatch = "e=channel-bindings-dont-match"

	// ErrServerDoesSupportChannelBinding indicates server does support channel
	// binding. This is returned if a downgrade attack is detected or if the
	// client does not support binding and channel binding is required.
	ErrServerDoesSupportChannelBinding = "e=server-does-support-channel-binding"

	// ErrChannelBindingNotSupported indicates channel binding is not supported
	ErrChannelBindingNotSupported = "e=channel-binding-not-supported"

	// ErrUnsupportedChannelBindingType indicates the requested channel binding type is not supported
	ErrUnsupportedChannelBindingType = "e=unsupported-channel-binding-type"

	// ErrUnknownUser indicates the specified user does not exist
	ErrUnknownUser = "e=unknown-user"

	// ErrInvalidUsernameEncoding indicates invalid username encoding (invalid UTF-8 or SASLprep failed)
	ErrInvalidUsernameEncoding = "e=invalid-username-encoding"

	// ErrNoResources indicates the server is out of resources
	ErrNoResources = "e=no-resources"

	// ErrOtherError is a catch-all for unspecified errors. The server may substitute
	// the real reason with this error to prevent information disclosure.
	ErrOtherError = "e=other-error"
)

func defaultNonceGenerator() string {
	raw := make([]byte, 24)
	nonce := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
	_, _ = rand.Read(raw)
	base64.StdEncoding.Encode(nonce, raw)
	return string(nonce)
}

func encodeName(s string) string {
	return strings.Replace(strings.Replace(s, "=", "=3D", -1), ",", "=2C", -1)
}

func computeHash(hg HashGeneratorFcn, b []byte) []byte {
	h := hg()
	h.Write(b)
	return h.Sum(nil)
}

func computeHMAC(hg HashGeneratorFcn, key, data []byte) []byte {
	mac := hmac.New(hg, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func xorBytes(a, b []byte) ([]byte, error) {
	if len(a) != len(b) {
		return nil, errors.New("internal error: xorBytes arguments must have equal length")
	}
	xor := make([]byte, len(a))
	for i := range a {
		xor[i] = a[i] ^ b[i]
	}
	return xor, nil
}
