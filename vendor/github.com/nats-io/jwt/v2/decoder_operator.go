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

type v1NatsOperator struct {
	SigningKeys         StringList `json:"signing_keys,omitempty"`
	AccountServerURL    string     `json:"account_server_url,omitempty"`
	OperatorServiceURLs StringList `json:"operator_service_urls,omitempty"`
	SystemAccount       string     `json:"system_account,omitempty"`
}

func loadOperator(data []byte, version int) (*OperatorClaims, error) {
	switch version {
	case 1:
		var v1a v1OperatorClaims
		if err := json.Unmarshal(data, &v1a); err != nil {
			return nil, err
		}
		return v1a.Migrate()
	case 2:
		var v2a OperatorClaims
		if err := json.Unmarshal(data, &v2a); err != nil {
			return nil, err
		}
		return &v2a, nil
	default:
		return nil, fmt.Errorf("library supports version %d or less - received %d", libVersion, version)
	}
}

type v1OperatorClaims struct {
	ClaimsData
	v1ClaimsDataDeletedFields
	v1NatsOperator `json:"nats,omitempty"`
}

func (oa v1OperatorClaims) Migrate() (*OperatorClaims, error) {
	return oa.migrateV1()
}

func (oa v1OperatorClaims) migrateV1() (*OperatorClaims, error) {
	var a OperatorClaims
	// copy the base claim
	a.ClaimsData = oa.ClaimsData
	// move the moved fields
	a.Operator.Type = oa.v1ClaimsDataDeletedFields.Type
	a.Operator.Tags = oa.v1ClaimsDataDeletedFields.Tags
	// copy the account data
	a.Operator.SigningKeys = oa.v1NatsOperator.SigningKeys
	a.Operator.AccountServerURL = oa.v1NatsOperator.AccountServerURL
	a.Operator.OperatorServiceURLs = oa.v1NatsOperator.OperatorServiceURLs
	a.Operator.SystemAccount = oa.v1NatsOperator.SystemAccount
	a.Version = 1
	return &a, nil
}
