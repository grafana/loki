/*
 * Copyright 2020 The NATS Authors
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jwt

import (
	"encoding/json"
	"fmt"
)

type v1NatsAccount struct {
	Imports Imports `json:"imports,omitempty"`
	Exports Exports `json:"exports,omitempty"`
	Limits  struct {
		NatsLimits
		AccountLimits
	} `json:"limits,omitempty"`
	SigningKeys StringList     `json:"signing_keys,omitempty"`
	Revocations RevocationList `json:"revocations,omitempty"`
}

func loadAccount(data []byte, version int) (*AccountClaims, error) {
	switch version {
	case 1:
		var v1a v1AccountClaims
		if err := json.Unmarshal(data, &v1a); err != nil {
			return nil, err
		}
		return v1a.Migrate()
	case 2:
		var v2a AccountClaims
		v2a.SigningKeys = make(SigningKeys)
		if err := json.Unmarshal(data, &v2a); err != nil {
			return nil, err
		}
		if len(v2a.Limits.JetStreamTieredLimits) > 0 {
			v2a.Limits.JetStreamLimits = JetStreamLimits{}
		}
		return &v2a, nil
	default:
		return nil, fmt.Errorf("library supports version %d or less - received %d", libVersion, version)
	}
}

type v1AccountClaims struct {
	ClaimsData
	v1ClaimsDataDeletedFields
	v1NatsAccount `json:"nats,omitempty"`
}

func (oa v1AccountClaims) Migrate() (*AccountClaims, error) {
	return oa.migrateV1()
}

func (oa v1AccountClaims) migrateV1() (*AccountClaims, error) {
	var a AccountClaims
	// copy the base claim
	a.ClaimsData = oa.ClaimsData
	// move the moved fields
	a.Account.Type = oa.v1ClaimsDataDeletedFields.Type
	a.Account.Tags = oa.v1ClaimsDataDeletedFields.Tags
	// copy the account data
	a.Account.Imports = oa.v1NatsAccount.Imports
	a.Account.Exports = oa.v1NatsAccount.Exports
	a.Account.Limits.AccountLimits = oa.v1NatsAccount.Limits.AccountLimits
	a.Account.Limits.NatsLimits = oa.v1NatsAccount.Limits.NatsLimits
	a.Account.Limits.JetStreamLimits = JetStreamLimits{}
	a.Account.SigningKeys = make(SigningKeys)
	for _, v := range oa.SigningKeys {
		a.Account.SigningKeys.Add(v)
	}
	a.Account.Revocations = oa.v1NatsAccount.Revocations
	a.Version = 1
	return &a, nil
}
