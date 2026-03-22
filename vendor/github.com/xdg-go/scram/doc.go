// Copyright 2018 by David A. Golden. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Package scram provides client and server implementations of the Salted
// Challenge Response Authentication Mechanism (SCRAM) described in RFC-5802,
// RFC-7677, and RFC-9266.
//
// # Usage
//
// The scram package provides variables, `SHA1`, `SHA256`, and `SHA512`, that
// are used to construct Client or Server objects.
//
//	clientSHA1,   err := scram.SHA1.NewClient(username, password, authID)
//	clientSHA256, err := scram.SHA256.NewClient(username, password, authID)
//	clientSHA512, err := scram.SHA512.NewClient(username, password, authID)
//
//	serverSHA1,   err := scram.SHA1.NewServer(credentialLookupFcn)
//	serverSHA256, err := scram.SHA256.NewServer(credentialLookupFcn)
//	serverSHA512, err := scram.SHA512.NewServer(credentialLookupFcn)
//
// These objects are used to construct ClientConversation or
// ServerConversation objects that are used to carry out authentication.
//
//	clientConv := client.NewConversation()
//	serverConv := server.NewConversation()
//
// # Channel Binding (SCRAM-PLUS)
//
// The scram package supports channel binding for SCRAM-PLUS authentication
// variants as described in RFC-5802, RFC-5929, and RFC-9266. Channel binding
// cryptographically binds the SCRAM authentication to an underlying TLS
// connection, preventing man-in-the-middle attacks.
//
// To use channel binding, create conversations with channel binding data
// obtained from the TLS connection:
//
//	// Client example with tls-exporter (TLS 1.3+)
//	client, _ := scram.SHA256.NewClient(username, password, "")
//	channelBinding, _ := scram.NewTLSExporterBinding(&tlsConn.ConnectionState())
//	clientConv := client.NewConversationWithChannelBinding(channelBinding)
//
//	// Server conversation with the same channel binding
//	server, _ := scram.SHA256.NewServer(credentialLookupFcn)
//	serverConv := server.NewConversationWithChannelBinding(channelBinding)
//
// Helper functions are provided to create ChannelBinding values from TLS connections:
//   - NewTLSServerEndpointBinding: Uses server certificate hash (RFC 5929, all TLS versions)
//   - NewTLSExporterBinding: Uses exported keying material (RFC 9266, recommended for TLS 1.3+)
//
// Channel binding is configured on conversations rather than clients or servers
// because binding data is connection-specific.
//
// Channel binding type negotiation is not defined by the SCRAM protocol.
// Applications must ensure both client and server agree on the same channel binding
// type.
package scram
