/*
 * Copyright 2018 The NATS Authors
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
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/nats-io/nkeys"
)

// Operator specific claims
type Operator struct {
	// Slice of other operator NKeys that can be used to sign on behalf of the main
	// operator identity.
	SigningKeys StringList `json:"signing_keys,omitempty"`
	// AccountServerURL is a partial URL like "https://host.domain.org:<port>/jwt/v1"
	// tools will use the prefix and build queries by appending /accounts/<account_id>
	// or /operator to the path provided. Note this assumes that the account server
	// can handle requests in a nats-account-server compatible way. See
	// https://github.com/nats-io/nats-account-server.
	AccountServerURL string `json:"account_server_url,omitempty"`
	// A list of NATS urls (tls://host:port) where tools can connect to the server
	// using proper credentials.
	OperatorServiceURLs StringList `json:"operator_service_urls,omitempty"`
	// Identity of the system account
	SystemAccount string `json:"system_account,omitempty"`
	// Min Server version
	AssertServerVersion string `json:"assert_server_version,omitempty"`
	// Signing of subordinate objects will require signing keys
	StrictSigningKeyUsage bool `json:"strict_signing_key_usage,omitempty"`
	GenericFields
}

func ParseServerVersion(version string) (int, int, int, error) {
	if version == "" {
		return 0, 0, 0, nil
	}
	split := strings.Split(version, ".")
	if len(split) != 3 {
		return 0, 0, 0, fmt.Errorf("asserted server version must be of the form <major>.<minor>.<update>")
	} else if major, err := strconv.Atoi(split[0]); err != nil {
		return 0, 0, 0, fmt.Errorf("asserted server version cant parse %s to int", split[0])
	} else if minor, err := strconv.Atoi(split[1]); err != nil {
		return 0, 0, 0, fmt.Errorf("asserted server version cant parse %s to int", split[1])
	} else if update, err := strconv.Atoi(split[2]); err != nil {
		return 0, 0, 0, fmt.Errorf("asserted server version cant parse %s to int", split[2])
	} else if major < 0 || minor < 0 || update < 0 {
		return 0, 0, 0, fmt.Errorf("asserted server version can'b contain negative values: %s", version)
	} else {
		return major, minor, update, nil
	}
}

// Validate checks the validity of the operators contents
func (o *Operator) Validate(vr *ValidationResults) {
	if err := o.validateAccountServerURL(); err != nil {
		vr.AddError(err.Error())
	}

	for _, v := range o.validateOperatorServiceURLs() {
		if v != nil {
			vr.AddError(v.Error())
		}
	}

	for _, k := range o.SigningKeys {
		if !nkeys.IsValidPublicOperatorKey(k) {
			vr.AddError("%s is not an operator public key", k)
		}
	}
	if o.SystemAccount != "" {
		if !nkeys.IsValidPublicAccountKey(o.SystemAccount) {
			vr.AddError("%s is not an account public key", o.SystemAccount)
		}
	}
	if _, _, _, err := ParseServerVersion(o.AssertServerVersion); err != nil {
		vr.AddError("assert server version error: %s", err)
	}
}

func (o *Operator) validateAccountServerURL() error {
	if o.AccountServerURL != "" {
		// We don't care what kind of URL it is so long as it parses
		// and has a protocol. The account server may impose additional
		// constraints on the type of URLs that it is able to notify to
		u, err := url.Parse(o.AccountServerURL)
		if err != nil {
			return fmt.Errorf("error parsing account server url: %v", err)
		}
		if u.Scheme == "" {
			return fmt.Errorf("account server url %q requires a protocol", o.AccountServerURL)
		}
	}
	return nil
}

// ValidateOperatorServiceURL returns an error if the URL is not a valid NATS or TLS url.
func ValidateOperatorServiceURL(v string) error {
	// should be possible for the service url to not be expressed
	if v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return fmt.Errorf("error parsing operator service url %q: %v", v, err)
	}

	if u.User != nil {
		return fmt.Errorf("operator service url %q - credentials are not supported", v)
	}

	if u.Path != "" {
		return fmt.Errorf("operator service url %q - paths are not supported", v)
	}

	lcs := strings.ToLower(u.Scheme)
	switch lcs {
	case "nats":
		return nil
	case "tls":
		return nil
	default:
		return fmt.Errorf("operator service url %q - protocol not supported (only 'nats' or 'tls' only)", v)
	}
}

func (o *Operator) validateOperatorServiceURLs() []error {
	var errs []error
	for _, v := range o.OperatorServiceURLs {
		if v != "" {
			if err := ValidateOperatorServiceURL(v); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// OperatorClaims define the data for an operator JWT
type OperatorClaims struct {
	ClaimsData
	Operator `json:"nats,omitempty"`
}

// NewOperatorClaims creates a new operator claim with the specified subject, which should be an operator public key
func NewOperatorClaims(subject string) *OperatorClaims {
	if subject == "" {
		return nil
	}
	c := &OperatorClaims{}
	c.Subject = subject
	c.Issuer = subject
	return c
}

// DidSign checks the claims against the operator's public key and its signing keys
func (oc *OperatorClaims) DidSign(op Claims) bool {
	if op == nil {
		return false
	}
	issuer := op.Claims().Issuer
	if issuer == oc.Subject {
		if !oc.StrictSigningKeyUsage {
			return true
		}
		return op.Claims().Subject == oc.Subject
	}
	return oc.SigningKeys.Contains(issuer)
}

// Encode the claims into a JWT string
func (oc *OperatorClaims) Encode(pair nkeys.KeyPair) (string, error) {
	if !nkeys.IsValidPublicOperatorKey(oc.Subject) {
		return "", errors.New("expected subject to be an operator public key")
	}
	err := oc.validateAccountServerURL()
	if err != nil {
		return "", err
	}
	oc.Type = OperatorClaim
	return oc.ClaimsData.encode(pair, oc)
}

func (oc *OperatorClaims) ClaimType() ClaimType {
	return oc.Type
}

// DecodeOperatorClaims tries to create an operator claims from a JWt string
func DecodeOperatorClaims(token string) (*OperatorClaims, error) {
	claims, err := Decode(token)
	if err != nil {
		return nil, err
	}
	oc, ok := claims.(*OperatorClaims)
	if !ok {
		return nil, errors.New("not operator claim")
	}
	return oc, nil
}

func (oc *OperatorClaims) String() string {
	return oc.ClaimsData.String(oc)
}

// Payload returns the operator specific data for an operator JWT
func (oc *OperatorClaims) Payload() interface{} {
	return &oc.Operator
}

// Validate the contents of the claims
func (oc *OperatorClaims) Validate(vr *ValidationResults) {
	oc.ClaimsData.Validate(vr)
	oc.Operator.Validate(vr)
}

// ExpectedPrefixes defines the nkey types that can sign operator claims, operator
func (oc *OperatorClaims) ExpectedPrefixes() []nkeys.PrefixByte {
	return []nkeys.PrefixByte{nkeys.PrefixByteOperator}
}

// Claims returns the generic claims data
func (oc *OperatorClaims) Claims() *ClaimsData {
	return &oc.ClaimsData
}

func (oc *OperatorClaims) updateVersion() {
	oc.GenericFields.Version = libVersion
}
