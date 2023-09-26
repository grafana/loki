/*
 * Copyright 2017 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

// credentials.go - the credentials data structure definition

// Package auth implements the authorization functionality for BCE.
// It use the BCE access key ID and secret access key with the specific sign algorithm to generate
// the authorization string. It also supports the temporary authorization by the STS token.
package auth

import "errors"

// BceCredentials define the data structure for authorization
type BceCredentials struct {
	AccessKeyId     string // access key id to the service
	SecretAccessKey string // secret access key to the service
	SessionToken    string // session token generate by the STS service
}

func (b *BceCredentials) String() string {
	str := "ak: " + b.AccessKeyId + ", sk: " + b.SecretAccessKey
	if len(b.SessionToken) != 0 {
		return str + ", sessionToken: " + b.SessionToken
	}
	return str
}

func NewBceCredentials(ak, sk string) (*BceCredentials, error) {
	if len(ak) == 0 {
		return nil, errors.New("accessKeyId should not be empty")
	}
	if len(sk) == 0 {
		return nil, errors.New("secretKey should not be empty")
	}

	return &BceCredentials{ak, sk, ""}, nil
}

func NewSessionBceCredentials(ak, sk, token string) (*BceCredentials, error) {
	if len(token) == 0 {
		return nil, errors.New("sessionToken should not be empty")
	}

	result, err := NewBceCredentials(ak, sk)
	if err != nil {
		return nil, err
	}
	result.SessionToken = token

	return result, nil
}
