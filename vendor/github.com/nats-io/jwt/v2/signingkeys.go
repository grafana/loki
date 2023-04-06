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
	"errors"
	"fmt"

	"github.com/nats-io/nkeys"
)

type Scope interface {
	SigningKey() string
	ValidateScopedSigner(claim Claims) error
	Validate(vr *ValidationResults)
}

type ScopeType int

const (
	UserScopeType ScopeType = iota + 1
)

func (t ScopeType) String() string {
	switch t {
	case UserScopeType:
		return "user_scope"
	}
	return "unknown"
}

func (t *ScopeType) MarshalJSON() ([]byte, error) {
	switch *t {
	case UserScopeType:
		return []byte("\"user_scope\""), nil
	}
	return nil, fmt.Errorf("unknown scope type %q", t)
}

func (t *ScopeType) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}
	switch s {
	case "user_scope":
		*t = UserScopeType
		return nil
	}
	return fmt.Errorf("unknown scope type %q", t)
}

type UserScope struct {
	Kind     ScopeType            `json:"kind"`
	Key      string               `json:"key"`
	Role     string               `json:"role"`
	Template UserPermissionLimits `json:"template"`
}

func NewUserScope() *UserScope {
	var s UserScope
	s.Kind = UserScopeType
	s.Template.NatsLimits = NatsLimits{NoLimit, NoLimit, NoLimit}
	return &s
}

func (us UserScope) SigningKey() string {
	return us.Key
}

func (us UserScope) Validate(vr *ValidationResults) {
	if !nkeys.IsValidPublicAccountKey(us.Key) {
		vr.AddError("%s is not an account public key", us.Key)
	}
}

func (us UserScope) ValidateScopedSigner(c Claims) error {
	uc, ok := c.(*UserClaims)
	if !ok {
		return fmt.Errorf("not an user claim - scoped signing key requires user claim")
	}
	if uc.Claims().Issuer != us.Key {
		return errors.New("issuer not the scoped signer")
	}
	if !uc.HasEmptyPermissions() {
		return errors.New("scoped users require no permissions or limits set")
	}
	return nil
}

// SigningKeys is a map keyed by a public account key
type SigningKeys map[string]Scope

func (sk SigningKeys) Validate(vr *ValidationResults) {
	for k, v := range sk {
		// regular signing keys won't have a scope
		if v != nil {
			v.Validate(vr)
		} else {
			if !nkeys.IsValidPublicAccountKey(k) {
				vr.AddError("%q is not a valid account signing key", k)
			}
		}
	}
}

// MarshalJSON serializes the scoped signing keys as an array
func (sk *SigningKeys) MarshalJSON() ([]byte, error) {
	if sk == nil {
		return nil, nil
	}
	var a []interface{}
	for k, v := range *sk {
		if v != nil {
			a = append(a, v)
		} else {
			a = append(a, k)
		}
	}
	return json.Marshal(a)
}

func (sk *SigningKeys) UnmarshalJSON(data []byte) error {
	if *sk == nil {
		*sk = make(SigningKeys)
	}
	// read an array - we can have a string or an map
	var a []interface{}
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	for _, i := range a {
		switch v := i.(type) {
		case string:
			(*sk)[v] = nil
		case map[string]interface{}:
			d, err := json.Marshal(v)
			if err != nil {
				return err
			}
			switch v["kind"] {
			case UserScopeType.String():
				us := NewUserScope()
				if err := json.Unmarshal(d, &us); err != nil {
					return err
				}
				(*sk)[us.Key] = us
			default:
				return fmt.Errorf("unknown signing key scope %q", v["type"])
			}
		}
	}
	return nil
}

func (sk SigningKeys) Keys() []string {
	var keys []string
	for k := range sk {
		keys = append(keys, k)
	}
	return keys
}

// GetScope returns nil if the key is not associated
func (sk SigningKeys) GetScope(k string) (Scope, bool) {
	v, ok := sk[k]
	if !ok {
		return nil, false
	}
	return v, true
}

func (sk SigningKeys) Contains(k string) bool {
	_, ok := sk[k]
	return ok
}

func (sk SigningKeys) Add(keys ...string) {
	for _, k := range keys {
		sk[k] = nil
	}
}

func (sk SigningKeys) AddScopedSigner(s Scope) {
	sk[s.SigningKey()] = s
}

func (sk SigningKeys) Remove(keys ...string) {
	for _, k := range keys {
		delete(sk, k)
	}
}
