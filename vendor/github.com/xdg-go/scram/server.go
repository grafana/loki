// Copyright 2018 by David A. Golden. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package scram

import "sync"

// Server implements the server side of SCRAM authentication.  It holds
// configuration values needed to initialize new server-side conversations.
// Generally, this can be persistent within an application.
type Server struct {
	sync.RWMutex
	credentialCB CredentialLookup
	nonceGen     NonceGeneratorFcn
	hashGen      HashGeneratorFcn
}

func newServer(cl CredentialLookup, fcn HashGeneratorFcn) (*Server, error) {
	return &Server{
		credentialCB: cl,
		nonceGen:     defaultNonceGenerator,
		hashGen:      fcn,
	}, nil
}

// WithNonceGenerator replaces the default nonce generator (base64 encoding of
// 24 bytes from crypto/rand) with a custom generator.  This is provided for
// testing or for users with custom nonce requirements.
func (s *Server) WithNonceGenerator(ng NonceGeneratorFcn) *Server {
	s.Lock()
	defer s.Unlock()
	s.nonceGen = ng
	return s
}

// NewConversation constructs a server-side authentication conversation.
// Conversations cannot be reused, so this must be called for each new
// authentication attempt.
func (s *Server) NewConversation() *ServerConversation {
	s.RLock()
	defer s.RUnlock()
	return &ServerConversation{
		nonceGen:     s.nonceGen,
		hashGen:      s.hashGen,
		credentialCB: s.credentialCB,
	}
}

// NewConversationWithChannelBinding constructs a server-side authentication
// conversation with channel binding for SCRAM-PLUS authentication.
//
// This signals that the server advertised PLUS mechanism variants (e.g.,
// SCRAM-SHA-256-PLUS) during SASL negotiation, but channel binding is NOT required.
// Clients may authenticate using either the base mechanism (e.g., SCRAM-SHA-256)
// or the PLUS variant (e.g., SCRAM-SHA-256-PLUS).
//
// The server will:
//   - Accept clients without channel binding support (using "n" flag)
//   - Accept clients with matching channel binding (using "p" flag)
//   - Reject downgrade attacks (clients using "y" flag when PLUS was advertised)
//
// Channel binding is connection-specific, so a new conversation should be
// created for each connection being authenticated.
// Conversations cannot be reused, so this must be called for each new
// authentication attempt.
func (s *Server) NewConversationWithChannelBinding(cb ChannelBinding) *ServerConversation {
	s.RLock()
	defer s.RUnlock()
	return &ServerConversation{
		nonceGen:       s.nonceGen,
		hashGen:        s.hashGen,
		credentialCB:   s.credentialCB,
		channelBinding: cb,
	}
}

// NewConversationWithChannelBindingRequired constructs a server-side authentication
// conversation with mandatory channel binding for SCRAM-PLUS authentication.
//
// This signals that the server advertised ONLY SCRAM-PLUS mechanism variants
// (e.g., only SCRAM-SHA-256-PLUS, not the base SCRAM-SHA-256) during SASL negotiation.
// Channel binding is required for all authentication attempts.
//
// The server will:
//   - Accept only clients with matching channel binding (using "p" flag)
//   - Reject clients without channel binding support (using "n" flag)
//   - Reject downgrade attacks (clients using "y" flag when PLUS was advertised)
//
// This is intended for high-security deployments that advertise only SCRAM-PLUS
// variants and want to enforce channel binding as mandatory.
//
// Channel binding is connection-specific, so a new conversation should be
// created for each connection being authenticated.
// Conversations cannot be reused, so this must be called for each new
// authentication attempt.
func (s *Server) NewConversationWithChannelBindingRequired(cb ChannelBinding) *ServerConversation {
	s.RLock()
	defer s.RUnlock()
	return &ServerConversation{
		nonceGen:              s.nonceGen,
		hashGen:               s.hashGen,
		credentialCB:          s.credentialCB,
		channelBinding:        cb,
		requireChannelBinding: true,
	}
}
