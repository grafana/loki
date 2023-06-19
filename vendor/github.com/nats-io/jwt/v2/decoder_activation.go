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

// Migration adds GenericFields
type v1NatsActivation struct {
	ImportSubject Subject    `json:"subject,omitempty"`
	ImportType    ExportType `json:"type,omitempty"`
	// Limit values deprecated inv v2
	Max     int64       `json:"max,omitempty"`
	Payload int64       `json:"payload,omitempty"`
	Src     string      `json:"src,omitempty"`
	Times   []TimeRange `json:"times,omitempty"`
}

type v1ActivationClaims struct {
	ClaimsData
	v1ClaimsDataDeletedFields
	v1NatsActivation `json:"nats,omitempty"`
}

func loadActivation(data []byte, version int) (*ActivationClaims, error) {
	switch version {
	case 1:
		var v1a v1ActivationClaims
		v1a.Max = NoLimit
		v1a.Payload = NoLimit
		if err := json.Unmarshal(data, &v1a); err != nil {
			return nil, err
		}
		return v1a.Migrate()
	case 2:
		var v2a ActivationClaims
		if err := json.Unmarshal(data, &v2a); err != nil {
			return nil, err
		}
		return &v2a, nil
	default:
		return nil, fmt.Errorf("library supports version %d or less - received %d", libVersion, version)
	}
}

func (oa v1ActivationClaims) Migrate() (*ActivationClaims, error) {
	return oa.migrateV1()
}

func (oa v1ActivationClaims) migrateV1() (*ActivationClaims, error) {
	var a ActivationClaims
	// copy the base claim
	a.ClaimsData = oa.ClaimsData
	// move the moved fields
	a.Activation.Type = oa.v1ClaimsDataDeletedFields.Type
	a.Activation.Tags = oa.v1ClaimsDataDeletedFields.Tags
	a.Activation.IssuerAccount = oa.v1ClaimsDataDeletedFields.IssuerAccount
	// copy the activation data
	a.ImportSubject = oa.ImportSubject
	a.ImportType = oa.ImportType
	a.Version = 1
	return &a, nil
}
