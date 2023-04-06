// Copyright 2018-2019 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package nkeys is an Ed25519 based public-key signature system that simplifies keys and seeds
// and performs signing and verification.
package nkeys

import (
	"errors"
)

// Version is our current version
const Version = "0.3.0"

// Errors
var (
	ErrInvalidPrefixByte = errors.New("nkeys: invalid prefix byte")
	ErrInvalidKey        = errors.New("nkeys: invalid key")
	ErrInvalidPublicKey  = errors.New("nkeys: invalid public key")
	ErrInvalidSeedLen    = errors.New("nkeys: invalid seed length")
	ErrInvalidSeed       = errors.New("nkeys: invalid seed")
	ErrInvalidEncoding   = errors.New("nkeys: invalid encoded key")
	ErrInvalidSignature  = errors.New("nkeys: signature verification failed")
	ErrCannotSign        = errors.New("nkeys: can not sign, no private key available")
	ErrPublicKeyOnly     = errors.New("nkeys: no seed or private key available")
	ErrIncompatibleKey   = errors.New("nkeys: incompatible key")
)

// KeyPair provides the central interface to nkeys.
type KeyPair interface {
	Seed() ([]byte, error)
	PublicKey() (string, error)
	PrivateKey() ([]byte, error)
	Sign(input []byte) ([]byte, error)
	Verify(input []byte, sig []byte) error
	Wipe()
}

// CreateUser will create a User typed KeyPair.
func CreateUser() (KeyPair, error) {
	return CreatePair(PrefixByteUser)
}

// CreateAccount will create an Account typed KeyPair.
func CreateAccount() (KeyPair, error) {
	return CreatePair(PrefixByteAccount)
}

// CreateServer will create a Server typed KeyPair.
func CreateServer() (KeyPair, error) {
	return CreatePair(PrefixByteServer)
}

// CreateCluster will create a Cluster typed KeyPair.
func CreateCluster() (KeyPair, error) {
	return CreatePair(PrefixByteCluster)
}

// CreateOperator will create an Operator typed KeyPair.
func CreateOperator() (KeyPair, error) {
	return CreatePair(PrefixByteOperator)
}

// FromPublicKey will create a KeyPair capable of verifying signatures.
func FromPublicKey(public string) (KeyPair, error) {
	raw, err := decode([]byte(public))
	if err != nil {
		return nil, err
	}
	pre := PrefixByte(raw[0])
	if err := checkValidPublicPrefixByte(pre); err != nil {
		return nil, ErrInvalidPublicKey
	}
	return &pub{pre, raw[1:]}, nil
}

// FromSeed will create a KeyPair capable of signing and verifying signatures.
func FromSeed(seed []byte) (KeyPair, error) {
	_, _, err := DecodeSeed(seed)
	if err != nil {
		return nil, err
	}
	copy := append([]byte{}, seed...)
	return &kp{copy}, nil
}

// FromRawSeed will create a KeyPair from the raw 32 byte seed for a given type.
func FromRawSeed(prefix PrefixByte, rawSeed []byte) (KeyPair, error) {
	seed, err := EncodeSeed(prefix, rawSeed)
	if err != nil {
		return nil, err
	}
	return &kp{seed}, nil
}
