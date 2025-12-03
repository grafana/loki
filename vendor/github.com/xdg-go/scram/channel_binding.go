// Copyright 2018 by David A. Golden. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package scram

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"hash"
)

// ChannelBindingType represents the type of channel binding to use with
// SCRAM-PLUS authentication variants.  The type must match one of the
// channel binding types defined in RFC 5056, RFC 5929, or RFC 9266.
type ChannelBindingType string

const (
	// ChannelBindingNone indicates no channel binding is used.
	ChannelBindingNone ChannelBindingType = ""

	// ChannelBindingTLSUnique uses the TLS Finished message from the first
	// TLS handshake (RFC 5929).  This is considered insecure, but is included
	// as required by RFC 5802.
	ChannelBindingTLSUnique ChannelBindingType = "tls-unique"

	// ChannelBindingTLSServerEndpoint uses a hash of the server's certificate
	// (RFC 5929).  This works with all TLS versions including TLS 1.3.
	ChannelBindingTLSServerEndpoint ChannelBindingType = "tls-server-end-point"

	// ChannelBindingTLSExporter uses TLS Exported Keying Material with the
	// label "EXPORTER-Channel-Binding" (RFC 9266).  This is the recommended
	// channel binding type for TLS 1.3.
	ChannelBindingTLSExporter ChannelBindingType = "tls-exporter"
)

// ChannelBinding holds the channel binding type and data for SCRAM-PLUS
// authentication. Use constructors to create type-specific bindings.
type ChannelBinding struct {
	Type ChannelBindingType
	Data []byte
}

// IsSupported returns true if the channel binding is configured with a
// non-empty type and data.
func (cb ChannelBinding) IsSupported() bool {
	return cb.Type != ChannelBindingNone && len(cb.Data) > 0
}

// Matches returns true if this channel binding matches another channel
// binding in both type and data.
func (cb ChannelBinding) Matches(other ChannelBinding) bool {
	if cb.Type != other.Type {
		return false
	}
	return hmac.Equal(cb.Data, other.Data)
}

// NewTLSUniqueBinding creates a ChannelBinding for tls-unique channel binding.
// Since Go's standard library doesn't expose the TLS Finished message,
// applications must provide the data directly.
//
// Note: tls-unique is considered insecure and should generally be avoided.
func NewTLSUniqueBinding(data []byte) ChannelBinding {
	// Create a defensive copy to prevent caller from modifying the data
	cbData := make([]byte, len(data))
	copy(cbData, data)
	return ChannelBinding{
		Type: ChannelBindingTLSUnique,
		Data: cbData,
	}
}

// NewTLSServerEndpointBinding creates a ChannelBinding for tls-server-end-point
// channel binding per RFC 5929. It extracts the server's certificate from
// the TLS connection state and hashes it using the appropriate hash function
// based on the certificate's signature algorithm.
//
// This works with all TLS versions including TLS 1.3.
func NewTLSServerEndpointBinding(connState *tls.ConnectionState) (ChannelBinding, error) {
	if connState == nil {
		return ChannelBinding{}, errors.New("connection state is nil")
	}

	if len(connState.PeerCertificates) == 0 {
		return ChannelBinding{}, errors.New("no peer certificates")
	}

	cert := connState.PeerCertificates[0]

	// Determine hash algorithm per RFC 5929
	var h hash.Hash
	switch cert.SignatureAlgorithm {
	case x509.MD5WithRSA, x509.SHA1WithRSA, x509.DSAWithSHA1,
		x509.ECDSAWithSHA1:
		h = sha256.New() // Use SHA-256 for MD5/SHA-1
	case x509.SHA256WithRSA, x509.SHA256WithRSAPSS,
		x509.ECDSAWithSHA256:
		h = sha256.New()
	case x509.SHA384WithRSA, x509.SHA384WithRSAPSS,
		x509.ECDSAWithSHA384:
		h = sha512.New384()
	case x509.SHA512WithRSA, x509.SHA512WithRSAPSS,
		x509.ECDSAWithSHA512:
		h = sha512.New()
	default:
		return ChannelBinding{}, fmt.Errorf("unsupported signature algorithm: %v",
			cert.SignatureAlgorithm)
	}

	h.Write(cert.Raw)
	return ChannelBinding{
		Type: ChannelBindingTLSServerEndpoint,
		Data: h.Sum(nil),
	}, nil
}

// NewTLSExporterBinding creates a ChannelBinding for tls-exporter channel binding
// per RFC 9266. It uses TLS Exported Keying Material with the label
// "EXPORTER-Channel-Binding" and an empty context.
//
// This is the recommended channel binding type for TLS 1.3+.
func NewTLSExporterBinding(connState *tls.ConnectionState) (ChannelBinding, error) {
	if connState == nil {
		return ChannelBinding{}, errors.New("connection state is nil")
	}

	cbData, err := connState.ExportKeyingMaterial("EXPORTER-Channel-Binding", nil, 32)
	if err != nil {
		return ChannelBinding{}, fmt.Errorf("failed to export keying material: %w", err)
	}

	return ChannelBinding{
		Type: ChannelBindingTLSExporter,
		Data: cbData,
	}, nil
}
