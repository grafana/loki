// Copyright 2018 by David A. Golden. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package scram

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
)

type serverState int

const (
	serverFirst serverState = iota
	serverFinal
	serverDone
)

// ServerConversation implements the server-side of an authentication
// conversation with a client.  A new conversation must be created for
// each authentication attempt.
type ServerConversation struct {
	nonceGen              NonceGeneratorFcn
	hashGen               HashGeneratorFcn
	credentialCB          CredentialLookup
	state                 serverState
	credential            StoredCredentials
	valid                 bool
	gs2Header             string
	username              string
	authzID               string
	nonce                 string
	c1b                   string
	s1                    string
	channelBinding        ChannelBinding
	requireChannelBinding bool
	clientCBType          string
	clientCBFlag          string
}

// Step takes a string provided from a client and attempts to move the
// authentication conversation forward.  It returns a string to be sent to the
// client or an error if the client message is invalid.  Calling Step after a
// conversation completes is also an error.
func (sc *ServerConversation) Step(challenge string) (response string, err error) {
	switch sc.state {
	case serverFirst:
		sc.state = serverFinal
		response, err = sc.firstMsg(challenge)
	case serverFinal:
		sc.state = serverDone
		response, err = sc.finalMsg(challenge)
	default:
		response, err = "", errors.New("Conversation already completed")
	}
	return
}

// Done returns true if the conversation is completed or has errored.
func (sc *ServerConversation) Done() bool {
	return sc.state == serverDone
}

// Valid returns true if the conversation successfully authenticated the
// client.
func (sc *ServerConversation) Valid() bool {
	return sc.valid
}

// Username returns the client-provided username.  This is valid to call
// if the first conversation Step() is successful.
func (sc *ServerConversation) Username() string {
	return sc.username
}

// AuthzID returns the (optional) client-provided authorization identity, if
// any.  If one was not provided, it returns the empty string.  This is valid
// to call if the first conversation Step() is successful.
func (sc *ServerConversation) AuthzID() string {
	return sc.authzID
}

// validateChannelBindingFlag validates the client's channel binding flag against
// server configuration. The validation logic follows RFC 5802 section 6, but
// extends those semantics to cover the case of required channel binding.
//
// Client flag validation:
//   - "n": Client doesn't support channel binding
//   - "y": Client supports channel binding but server didn't advertise PLUS
//   - "p": Client requires channel binding with specific type
//
// Returns server error string (empty if validation passes) and error.
func (sc *ServerConversation) validateChannelBindingFlag() (string, error) {
	advertised := sc.channelBinding.IsSupported()

	switch sc.clientCBFlag {
	case "n":
		// Client doesn't support channel binding
		if sc.requireChannelBinding {
			// Policy violation: server requires channel binding
			// Use ErrServerDoesSupportChannelBinding (defined for downgrade attacks)
			// as the best available match to signal that server requires channel binding
			return ErrServerDoesSupportChannelBinding,
				errors.New("server requires channel binding but client doesn't support it")
		}
		// OK: server either doesn't advertise PLUS or advertises it optionally
		return "", nil

	case "y":
		// Client supports channel binding but thinks server doesn't advertise PLUS
		if advertised {
			// Downgrade attack: we advertised PLUS but client didn't see it
			return ErrServerDoesSupportChannelBinding,
				errors.New("downgrade attack detected: client used 'y' but server advertised PLUS")
		}
		// OK: we didn't advertise PLUS, client correctly detected this
		return "", nil

	case "p":
		// Client requires channel binding with specific type
		if !advertised {
			// Server doesn't support channel binding
			return ErrChannelBindingNotSupported,
				errors.New("client requires channel binding but server doesn't support it")
		}
		if ChannelBindingType(sc.clientCBType) != sc.channelBinding.Type {
			// Server supports channel binding but not the requested type
			return ErrUnsupportedChannelBindingType,
				fmt.Errorf("client requested %s but server only supports %s",
					sc.clientCBType, sc.channelBinding.Type)
		}
		// OK: channel binding type matches
		return "", nil

	default:
		// Invalid flag (should have been caught by parser)
		return ErrOtherError,
			fmt.Errorf("invalid channel binding flag: %s", sc.clientCBFlag)
	}
}

func (sc *ServerConversation) firstMsg(c1 string) (string, error) {
	msg, err := parseClientFirst(c1)
	if err != nil {
		sc.state = serverDone
		return "", err
	}

	sc.gs2Header = msg.gs2Header
	sc.clientCBFlag = msg.gs2BindFlag
	sc.clientCBType = msg.channelBinding
	sc.username = msg.username
	sc.authzID = msg.authzID

	// Validate channel binding flag against server configuration
	if serverErr, err := sc.validateChannelBindingFlag(); err != nil {
		sc.state = serverDone
		return serverErr, err
	}

	sc.credential, err = sc.credentialCB(msg.username)
	if err != nil {
		sc.state = serverDone
		return ErrUnknownUser, err
	}

	sc.nonce = msg.nonce + sc.nonceGen()
	sc.c1b = msg.c1b
	sc.s1 = fmt.Sprintf("r=%s,s=%s,i=%d",
		sc.nonce,
		base64.StdEncoding.EncodeToString([]byte(sc.credential.Salt)),
		sc.credential.Iters,
	)

	return sc.s1, nil
}

// For errors, returns server error message as well as non-nil error.  Callers
// can choose whether to send server error or not.
func (sc *ServerConversation) finalMsg(c2 string) (string, error) {
	msg, err := parseClientFinal(c2)
	if err != nil {
		return "", err
	}

	// Check channel binding data matches what we expect
	var expectedCBind []byte
	if sc.clientCBFlag == "p" {
		// Client used channel binding - expect gs2 header + channel binding data
		expectedCBind = append([]byte(sc.gs2Header), sc.channelBinding.Data...)
	} else {
		// Client didn't use channel binding - just expect gs2 header
		expectedCBind = []byte(sc.gs2Header)
	}

	if !hmac.Equal(msg.cbind, expectedCBind) {
		return ErrChannelBindingsDontMatch,
			fmt.Errorf("channel binding mismatch: expected %x, got %x",
				expectedCBind, msg.cbind)
	}

	// Check nonce received matches what we sent
	if msg.nonce != sc.nonce {
		return ErrOtherError, errors.New("nonce received did not match nonce sent")
	}

	// Create auth message
	authMsg := sc.c1b + "," + sc.s1 + "," + msg.c2wop

	// Retrieve ClientKey from proof and verify it
	clientSignature := computeHMAC(sc.hashGen, sc.credential.StoredKey, []byte(authMsg))
	clientKey, err := xorBytes([]byte(msg.proof), clientSignature)
	if err != nil {
		return ErrOtherError, err
	}
	storedKey := computeHash(sc.hashGen, clientKey)

	// Compare with constant-time function
	if !hmac.Equal(storedKey, sc.credential.StoredKey) {
		return ErrInvalidProof, errors.New("challenge proof invalid")
	}

	sc.valid = true

	// Compute and return server verifier
	serverSignature := computeHMAC(sc.hashGen, sc.credential.ServerKey, []byte(authMsg))
	return "v=" + base64.StdEncoding.EncodeToString(serverSignature), nil
}
