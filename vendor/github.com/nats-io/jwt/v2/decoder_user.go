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

type v1User struct {
	Permissions
	Limits
	BearerToken bool `json:"bearer_token,omitempty"`
	// Limit values deprecated inv v2
	Max int64 `json:"max,omitempty"`
}

type v1UserClaimsDataDeletedFields struct {
	v1ClaimsDataDeletedFields
	IssuerAccount string `json:"issuer_account,omitempty"`
}

type v1UserClaims struct {
	ClaimsData
	v1UserClaimsDataDeletedFields
	v1User `json:"nats,omitempty"`
}

func loadUser(data []byte, version int) (*UserClaims, error) {
	switch version {
	case 1:
		var v1a v1UserClaims
		v1a.Limits = Limits{NatsLimits: NatsLimits{NoLimit, NoLimit, NoLimit}}
		v1a.Max = NoLimit
		if err := json.Unmarshal(data, &v1a); err != nil {
			return nil, err
		}
		return v1a.Migrate()
	case 2:
		var v2a UserClaims
		if err := json.Unmarshal(data, &v2a); err != nil {
			return nil, err
		}
		return &v2a, nil
	default:
		return nil, fmt.Errorf("library supports version %d or less - received %d", libVersion, version)
	}
}

func (oa v1UserClaims) Migrate() (*UserClaims, error) {
	return oa.migrateV1()
}

func (oa v1UserClaims) migrateV1() (*UserClaims, error) {
	var u UserClaims
	// copy the base claim
	u.ClaimsData = oa.ClaimsData
	// move the moved fields
	u.User.Type = oa.v1ClaimsDataDeletedFields.Type
	u.User.Tags = oa.v1ClaimsDataDeletedFields.Tags
	u.User.IssuerAccount = oa.IssuerAccount
	// copy the user data
	u.User.Permissions = oa.v1User.Permissions
	u.User.Limits = oa.v1User.Limits
	u.User.BearerToken = oa.v1User.BearerToken
	u.Version = 1
	return &u, nil
}
