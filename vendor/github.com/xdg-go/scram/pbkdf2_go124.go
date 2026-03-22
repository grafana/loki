// Copyright 2025 by David A. Golden. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

//go:build go1.24

package scram

import (
	"crypto/pbkdf2"
	"hash"
)

func pbkdf2Key(h func() hash.Hash, password string, salt []byte, iter, keyLength int) ([]byte, error) {
	return pbkdf2.Key(h, password, salt, iter, keyLength)
}
