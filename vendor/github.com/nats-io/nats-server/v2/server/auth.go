// Copyright 2012-2022 The NATS Authors
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

package server

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/internal/ldap"
	"github.com/nats-io/nkeys"
	"golang.org/x/crypto/bcrypt"
)

// Authentication is an interface for implementing authentication
type Authentication interface {
	// Check if a client is authorized to connect
	Check(c ClientAuthentication) bool
}

// ClientAuthentication is an interface for client authentication
type ClientAuthentication interface {
	// GetOpts gets options associated with a client
	GetOpts() *ClientOpts
	// GetTLSConnectionState if TLS is enabled, TLS ConnectionState, nil otherwise
	GetTLSConnectionState() *tls.ConnectionState
	// RegisterUser optionally map a user after auth.
	RegisterUser(*User)
	// RemoteAddress expose the connection information of the client
	RemoteAddress() net.Addr
	// GetNonce is the nonce presented to the user in the INFO line
	GetNonce() []byte
	// Kind indicates what type of connection this is matching defined constants like CLIENT, ROUTER, GATEWAY, LEAF etc
	Kind() int
}

// NkeyUser is for multiple nkey based users
type NkeyUser struct {
	Nkey                   string              `json:"user"`
	Permissions            *Permissions        `json:"permissions,omitempty"`
	Account                *Account            `json:"account,omitempty"`
	SigningKey             string              `json:"signing_key,omitempty"`
	AllowedConnectionTypes map[string]struct{} `json:"connection_types,omitempty"`
}

// User is for multiple accounts/users.
type User struct {
	Username               string              `json:"user"`
	Password               string              `json:"password"`
	Permissions            *Permissions        `json:"permissions,omitempty"`
	Account                *Account            `json:"account,omitempty"`
	AllowedConnectionTypes map[string]struct{} `json:"connection_types,omitempty"`
}

// clone performs a deep copy of the User struct, returning a new clone with
// all values copied.
func (u *User) clone() *User {
	if u == nil {
		return nil
	}
	clone := &User{}
	*clone = *u
	clone.Permissions = u.Permissions.clone()
	return clone
}

// clone performs a deep copy of the NkeyUser struct, returning a new clone with
// all values copied.
func (n *NkeyUser) clone() *NkeyUser {
	if n == nil {
		return nil
	}
	clone := &NkeyUser{}
	*clone = *n
	clone.Permissions = n.Permissions.clone()
	return clone
}

// SubjectPermission is an individual allow and deny struct for publish
// and subscribe authorizations.
type SubjectPermission struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// ResponsePermission can be used to allow responses to any reply subject
// that is received on a valid subscription.
type ResponsePermission struct {
	MaxMsgs int           `json:"max"`
	Expires time.Duration `json:"ttl"`
}

// Permissions are the allowed subjects on a per
// publish or subscribe basis.
type Permissions struct {
	Publish   *SubjectPermission  `json:"publish"`
	Subscribe *SubjectPermission  `json:"subscribe"`
	Response  *ResponsePermission `json:"responses,omitempty"`
}

// RoutePermissions are similar to user permissions
// but describe what a server can import/export from and to
// another server.
type RoutePermissions struct {
	Import *SubjectPermission `json:"import"`
	Export *SubjectPermission `json:"export"`
}

// clone will clone an individual subject permission.
func (p *SubjectPermission) clone() *SubjectPermission {
	if p == nil {
		return nil
	}
	clone := &SubjectPermission{}
	if p.Allow != nil {
		clone.Allow = make([]string, len(p.Allow))
		copy(clone.Allow, p.Allow)
	}
	if p.Deny != nil {
		clone.Deny = make([]string, len(p.Deny))
		copy(clone.Deny, p.Deny)
	}
	return clone
}

// clone performs a deep copy of the Permissions struct, returning a new clone
// with all values copied.
func (p *Permissions) clone() *Permissions {
	if p == nil {
		return nil
	}
	clone := &Permissions{}
	if p.Publish != nil {
		clone.Publish = p.Publish.clone()
	}
	if p.Subscribe != nil {
		clone.Subscribe = p.Subscribe.clone()
	}
	if p.Response != nil {
		clone.Response = &ResponsePermission{
			MaxMsgs: p.Response.MaxMsgs,
			Expires: p.Response.Expires,
		}
	}
	return clone
}

// checkAuthforWarnings will look for insecure settings and log concerns.
// Lock is assumed held.
func (s *Server) checkAuthforWarnings() {
	warn := false
	if s.opts.Password != _EMPTY_ && !isBcrypt(s.opts.Password) {
		warn = true
	}
	for _, u := range s.users {
		// Skip warn if using TLS certs based auth
		// unless a password has been left in the config.
		if u.Password == _EMPTY_ && s.opts.TLSMap {
			continue
		}
		// Check if this is our internal sys client created on the fly.
		if s.sysAccOnlyNoAuthUser != _EMPTY_ && u.Username == s.sysAccOnlyNoAuthUser {
			continue
		}
		if !isBcrypt(u.Password) {
			warn = true
			break
		}
	}
	if warn {
		// Warning about using plaintext passwords.
		s.Warnf("Plaintext passwords detected, use nkeys or bcrypt")
	}
}

// If Users or Nkeys options have definitions without an account defined,
// assign them to the default global account.
// Lock should be held.
func (s *Server) assignGlobalAccountToOrphanUsers(nkeys map[string]*NkeyUser, users map[string]*User) {
	for _, u := range users {
		if u.Account == nil {
			u.Account = s.gacc
		}
	}
	for _, u := range nkeys {
		if u.Account == nil {
			u.Account = s.gacc
		}
	}
}

// If the given permissions has a ResponsePermission
// set, ensure that defaults are set (if values are 0)
// and that a Publish permission is set, and Allow
// is disabled if not explicitly set.
func validateResponsePermissions(p *Permissions) {
	if p == nil || p.Response == nil {
		return
	}
	if p.Publish == nil {
		p.Publish = &SubjectPermission{}
	}
	if p.Publish.Allow == nil {
		// We turn off the blanket allow statement.
		p.Publish.Allow = []string{}
	}
	// If there is a response permission, ensure
	// that if value is 0, we set the default value.
	if p.Response.MaxMsgs == 0 {
		p.Response.MaxMsgs = DEFAULT_ALLOW_RESPONSE_MAX_MSGS
	}
	if p.Response.Expires == 0 {
		p.Response.Expires = DEFAULT_ALLOW_RESPONSE_EXPIRATION
	}
}

// configureAuthorization will do any setup needed for authorization.
// Lock is assumed held.
func (s *Server) configureAuthorization() {
	opts := s.getOpts()
	if opts == nil {
		return
	}

	// Check for multiple users first
	// This just checks and sets up the user map if we have multiple users.
	if opts.CustomClientAuthentication != nil {
		s.info.AuthRequired = true
	} else if s.trustedKeys != nil {
		s.info.AuthRequired = true
	} else if opts.Nkeys != nil || opts.Users != nil {
		s.nkeys, s.users = s.buildNkeysAndUsersFromOptions(opts.Nkeys, opts.Users)
		s.info.AuthRequired = true
	} else if opts.Username != "" || opts.Authorization != "" {
		s.info.AuthRequired = true
	} else {
		s.users = nil
		s.nkeys = nil
		s.info.AuthRequired = false
	}

	// Do similar for websocket config
	s.wsConfigAuth(&opts.Websocket)
	// And for mqtt config
	s.mqttConfigAuth(&opts.MQTT)
}

// Takes the given slices of NkeyUser and User options and build
// corresponding maps used by the server. The users are cloned
// so that server does not reference options.
// The global account is assigned to users that don't have an
// existing account.
// Server lock is held on entry.
func (s *Server) buildNkeysAndUsersFromOptions(nko []*NkeyUser, uo []*User) (map[string]*NkeyUser, map[string]*User) {
	var nkeys map[string]*NkeyUser
	var users map[string]*User

	if nko != nil {
		nkeys = make(map[string]*NkeyUser, len(nko))
		for _, u := range nko {
			copy := u.clone()
			if u.Account != nil {
				if v, ok := s.accounts.Load(u.Account.Name); ok {
					copy.Account = v.(*Account)
				}
			}
			if copy.Permissions != nil {
				validateResponsePermissions(copy.Permissions)
			}
			nkeys[u.Nkey] = copy
		}
	}
	if uo != nil {
		users = make(map[string]*User, len(uo))
		for _, u := range uo {
			copy := u.clone()
			if u.Account != nil {
				if v, ok := s.accounts.Load(u.Account.Name); ok {
					copy.Account = v.(*Account)
				}
			}
			if copy.Permissions != nil {
				validateResponsePermissions(copy.Permissions)
			}
			users[u.Username] = copy
		}
	}
	s.assignGlobalAccountToOrphanUsers(nkeys, users)
	return nkeys, users
}

// checkAuthentication will check based on client type and
// return boolean indicating if client is authorized.
func (s *Server) checkAuthentication(c *client) bool {
	switch c.kind {
	case CLIENT:
		return s.isClientAuthorized(c)
	case ROUTER:
		return s.isRouterAuthorized(c)
	case GATEWAY:
		return s.isGatewayAuthorized(c)
	case LEAF:
		return s.isLeafNodeAuthorized(c)
	default:
		return false
	}
}

// isClientAuthorized will check the client against the proper authorization method and data.
// This could be nkey, token, or username/password based.
func (s *Server) isClientAuthorized(c *client) bool {
	opts := s.getOpts()

	// Check custom auth first, then jwts, then nkeys, then
	// multiple users with TLS map if enabled, then token,
	// then single user/pass.
	if opts.CustomClientAuthentication != nil && !opts.CustomClientAuthentication.Check(c) {
		return false
	}

	if opts.CustomClientAuthentication == nil && !s.processClientOrLeafAuthentication(c, opts) {
		return false
	}

	if c.kind == CLIENT || c.kind == LEAF {
		// Generate an event if we have a system account.
		s.accountConnectEvent(c)
	}

	return true
}

// returns false if the client needs to be disconnected
func (c *client) matchesPinnedCert(tlsPinnedCerts PinnedCertSet) bool {
	if tlsPinnedCerts == nil {
		return true
	}
	tlsState := c.GetTLSConnectionState()
	if tlsState == nil || len(tlsState.PeerCertificates) == 0 || tlsState.PeerCertificates[0] == nil {
		c.Debugf("Failed pinned cert test as client did not provide a certificate")
		return false
	}
	sha := sha256.Sum256(tlsState.PeerCertificates[0].RawSubjectPublicKeyInfo)
	keyId := hex.EncodeToString(sha[:])
	if _, ok := tlsPinnedCerts[keyId]; !ok {
		c.Debugf("Failed pinned cert test for key id: %s", keyId)
		return false
	}
	return true
}

func processUserPermissionsTemplate(lim jwt.UserPermissionLimits, ujwt *jwt.UserClaims, acc *Account) (jwt.UserPermissionLimits, error) {
	nArrayCartesianProduct := func(a ...[]string) [][]string {
		c := 1
		for _, a := range a {
			c *= len(a)
		}
		if c == 0 {
			return nil
		}
		p := make([][]string, c)
		b := make([]string, c*len(a))
		n := make([]int, len(a))
		s := 0
		for i := range p {
			e := s + len(a)
			pi := b[s:e]
			p[i] = pi
			s = e
			for j, n := range n {
				pi[j] = a[j][n]
			}
			for j := len(n) - 1; j >= 0; j-- {
				n[j]++
				if n[j] < len(a[j]) {
					break
				}
				n[j] = 0
			}
		}
		return p
	}
	applyTemplate := func(list jwt.StringList, failOnBadSubject bool) (jwt.StringList, error) {
		found := false
	FOR_FIND:
		for i := 0; i < len(list); i++ {
			// check if templates are present
			for _, tk := range strings.Split(list[i], tsep) {
				if strings.HasPrefix(tk, "{{") && strings.HasSuffix(tk, "}}") {
					found = true
					break FOR_FIND
				}
			}
		}
		if !found {
			return list, nil
		}
		// process the templates
		emittedList := make([]string, 0, len(list))
		for i := 0; i < len(list); i++ {
			tokens := strings.Split(list[i], tsep)

			newTokens := make([]string, len(tokens))
			tagValues := [][]string{}

			for tokenNum, tk := range tokens {
				if strings.HasPrefix(tk, "{{") && strings.HasSuffix(tk, "}}") {
					op := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(tk, "{{"), "}}"))
					switch {
					case op == "name()":
						tk = ujwt.Name
					case op == "subject()":
						tk = ujwt.Subject
					case op == "account-name()":
						acc.mu.RLock()
						name := acc.nameTag
						acc.mu.RUnlock()
						tk = name
					case op == "account-subject()":
						tk = ujwt.IssuerAccount
					case (strings.HasPrefix(op, "tag(") || strings.HasPrefix(op, "account-tag(")) &&
						strings.HasSuffix(op, ")"):
						// insert dummy tav value that will throw of subject validation (in case nothing is found)
						tk = _EMPTY_
						// collect list of matching tag values

						var tags jwt.TagList
						var tagPrefix string
						if strings.HasPrefix(op, "account-tag(") {
							acc.mu.RLock()
							tags = acc.tags
							acc.mu.RUnlock()
							tagPrefix = fmt.Sprintf("%s:", strings.ToLower(
								strings.TrimSuffix(strings.TrimPrefix(op, "account-tag("), ")")))
						} else {
							tags = ujwt.Tags
							tagPrefix = fmt.Sprintf("%s:", strings.ToLower(
								strings.TrimSuffix(strings.TrimPrefix(op, "tag("), ")")))
						}

						valueList := []string{}
						for _, tag := range tags {
							if strings.HasPrefix(tag, tagPrefix) {
								tagValue := strings.TrimPrefix(tag, tagPrefix)
								valueList = append(valueList, tagValue)
							}
						}
						if len(valueList) != 0 {
							tagValues = append(tagValues, valueList)
						}
					default:
						// if macro is not recognized, throw off subject check on purpose
						tk = " "
					}
				}
				newTokens[tokenNum] = tk
			}
			// fill in tag value placeholders
			if len(tagValues) == 0 {
				emitSubj := strings.Join(newTokens, tsep)
				if IsValidSubject(emitSubj) {
					emittedList = append(emittedList, emitSubj)
				} else if failOnBadSubject {
					return nil, fmt.Errorf("generated invalid subject")
				}
				// else skip emitting
			} else {
				// compute the cartesian product and compute subject to emit for each combination
				for _, valueList := range nArrayCartesianProduct(tagValues...) {
					b := strings.Builder{}
					for i, token := range newTokens {
						if token == _EMPTY_ {
							b.WriteString(valueList[0])
							valueList = valueList[1:]
						} else {
							b.WriteString(token)
						}
						if i != len(newTokens)-1 {
							b.WriteString(tsep)
						}
					}
					emitSubj := b.String()
					if IsValidSubject(emitSubj) {
						emittedList = append(emittedList, emitSubj)
					} else if failOnBadSubject {
						return nil, fmt.Errorf("generated invalid subject")
					}
					// else skip emitting
				}
			}
		}
		return emittedList, nil
	}

	subAllowWasNotEmpty := len(lim.Permissions.Sub.Allow) > 0
	pubAllowWasNotEmpty := len(lim.Permissions.Pub.Allow) > 0

	var err error
	if lim.Permissions.Sub.Allow, err = applyTemplate(lim.Permissions.Sub.Allow, false); err != nil {
		return jwt.UserPermissionLimits{}, err
	} else if lim.Permissions.Sub.Deny, err = applyTemplate(lim.Permissions.Sub.Deny, true); err != nil {
		return jwt.UserPermissionLimits{}, err
	} else if lim.Permissions.Pub.Allow, err = applyTemplate(lim.Permissions.Pub.Allow, false); err != nil {
		return jwt.UserPermissionLimits{}, err
	} else if lim.Permissions.Pub.Deny, err = applyTemplate(lim.Permissions.Pub.Deny, true); err != nil {
		return jwt.UserPermissionLimits{}, err
	}

	// if pub/sub allow were not empty, but are empty post template processing, add in a "deny >" to compensate
	if subAllowWasNotEmpty && len(lim.Permissions.Sub.Allow) == 0 {
		lim.Permissions.Sub.Deny.Add(">")
	}
	if pubAllowWasNotEmpty && len(lim.Permissions.Pub.Allow) == 0 {
		lim.Permissions.Pub.Deny.Add(">")
	}
	return lim, nil
}

func (s *Server) processClientOrLeafAuthentication(c *client, opts *Options) bool {
	var (
		nkey *NkeyUser
		juc  *jwt.UserClaims
		acc  *Account
		user *User
		ok   bool
		err  error
		ao   bool // auth override
	)
	s.mu.Lock()
	authRequired := s.info.AuthRequired
	if !authRequired {
		// If no auth required for regular clients, then check if
		// we have an override for MQTT or Websocket clients.
		switch c.clientType() {
		case MQTT:
			authRequired = s.mqtt.authOverride
		case WS:
			authRequired = s.websocket.authOverride
		}
	}
	if !authRequired {
		// TODO(dlc) - If they send us credentials should we fail?
		s.mu.Unlock()
		return true
	}
	var (
		username      string
		password      string
		token         string
		noAuthUser    string
		pinnedAcounts map[string]struct{}
	)
	tlsMap := opts.TLSMap
	if c.kind == CLIENT {
		switch c.clientType() {
		case MQTT:
			mo := &opts.MQTT
			// Always override TLSMap.
			tlsMap = mo.TLSMap
			// The rest depends on if there was any auth override in
			// the mqtt's config.
			if s.mqtt.authOverride {
				noAuthUser = mo.NoAuthUser
				username = mo.Username
				password = mo.Password
				token = mo.Token
				ao = true
			}
		case WS:
			wo := &opts.Websocket
			// Always override TLSMap.
			tlsMap = wo.TLSMap
			// The rest depends on if there was any auth override in
			// the websocket's config.
			if s.websocket.authOverride {
				noAuthUser = wo.NoAuthUser
				username = wo.Username
				password = wo.Password
				token = wo.Token
				ao = true
			}
		}
	} else {
		tlsMap = opts.LeafNode.TLSMap
	}

	if !ao {
		noAuthUser = opts.NoAuthUser
		// If a leaf connects using websocket, and websocket{} block has a no_auth_user
		// use that one instead.
		if c.kind == LEAF && c.isWebsocket() && opts.Websocket.NoAuthUser != _EMPTY_ {
			noAuthUser = opts.Websocket.NoAuthUser
		}
		username = opts.Username
		password = opts.Password
		token = opts.Authorization
	}

	// Check if we have trustedKeys defined in the server. If so we require a user jwt.
	if s.trustedKeys != nil {
		if c.opts.JWT == _EMPTY_ {
			s.mu.Unlock()
			c.Debugf("Authentication requires a user JWT")
			return false
		}
		// So we have a valid user jwt here.
		juc, err = jwt.DecodeUserClaims(c.opts.JWT)
		if err != nil {
			s.mu.Unlock()
			c.Debugf("User JWT not valid: %v", err)
			return false
		}
		vr := jwt.CreateValidationResults()
		juc.Validate(vr)
		if vr.IsBlocking(true) {
			s.mu.Unlock()
			c.Debugf("User JWT no longer valid: %+v", vr)
			return false
		}
		pinnedAcounts = opts.resolverPinnedAccounts
	}

	// Check if we have nkeys or users for client.
	hasNkeys := len(s.nkeys) > 0
	hasUsers := len(s.users) > 0
	if hasNkeys && c.opts.Nkey != _EMPTY_ {
		nkey, ok = s.nkeys[c.opts.Nkey]
		if !ok || !c.connectionTypeAllowed(nkey.AllowedConnectionTypes) {
			s.mu.Unlock()
			return false
		}
	} else if hasUsers {
		// Check if we are tls verify and are mapping users from the client_certificate.
		if tlsMap {
			authorized := checkClientTLSCertSubject(c, func(u string, certDN *ldap.DN, _ bool) (string, bool) {
				// First do literal lookup using the resulting string representation
				// of RDNSequence as implemented by the pkix package from Go.
				if u != _EMPTY_ {
					usr, ok := s.users[u]
					if !ok || !c.connectionTypeAllowed(usr.AllowedConnectionTypes) {
						return _EMPTY_, false
					}
					user = usr
					return usr.Username, true
				}

				if certDN == nil {
					return _EMPTY_, false
				}

				// Look through the accounts for a DN that is equal to the one
				// presented by the certificate.
				dns := make(map[*User]*ldap.DN)
				for _, usr := range s.users {
					if !c.connectionTypeAllowed(usr.AllowedConnectionTypes) {
						continue
					}
					// TODO: Use this utility to make a full validation pass
					// on start in case tlsmap feature is being used.
					inputDN, err := ldap.ParseDN(usr.Username)
					if err != nil {
						continue
					}
					if inputDN.Equal(certDN) {
						user = usr
						return usr.Username, true
					}

					// In case it did not match exactly, then collect the DNs
					// and try to match later in case the DN was reordered.
					dns[usr] = inputDN
				}

				// Check in case the DN was reordered.
				for usr, inputDN := range dns {
					if inputDN.RDNsMatch(certDN) {
						user = usr
						return usr.Username, true
					}
				}
				return _EMPTY_, false
			})
			if !authorized {
				s.mu.Unlock()
				return false
			}
			if c.opts.Username != _EMPTY_ {
				s.Warnf("User %q found in connect proto, but user required from cert", c.opts.Username)
			}
			// Already checked that the client didn't send a user in connect
			// but we set it here to be able to identify it in the logs.
			c.opts.Username = user.Username
		} else {
			if (c.kind == CLIENT || c.kind == LEAF) && noAuthUser != _EMPTY_ &&
				c.opts.Username == _EMPTY_ && c.opts.Password == _EMPTY_ && c.opts.Token == _EMPTY_ {
				if u, exists := s.users[noAuthUser]; exists {
					c.mu.Lock()
					c.opts.Username = u.Username
					c.opts.Password = u.Password
					c.mu.Unlock()
				}
			}
			if c.opts.Username != _EMPTY_ {
				user, ok = s.users[c.opts.Username]
				if !ok || !c.connectionTypeAllowed(user.AllowedConnectionTypes) {
					s.mu.Unlock()
					return false
				}
			}
		}
	}
	s.mu.Unlock()

	// If we have a jwt and a userClaim, make sure we have the Account, etc associated.
	// We need to look up the account. This will use an account resolver if one is present.
	if juc != nil {
		allowedConnTypes, err := convertAllowedConnectionTypes(juc.AllowedConnectionTypes)
		if err != nil {
			// We got an error, which means some connection types were unknown. As long as
			// a valid one is returned, we proceed with auth. If not, we have to reject.
			// In other words, suppose that JWT allows "WEBSOCKET" in the array. No error
			// is returned and allowedConnTypes will contain "WEBSOCKET" only.
			// Client will be rejected if not a websocket client, or proceed with rest of
			// auth if it is.
			// Now suppose JWT allows "WEBSOCKET, MQTT" and say MQTT is not known by this
			// server. In this case, allowedConnTypes would contain "WEBSOCKET" and we
			// would get `err` indicating that "MQTT" is an unknown connection type.
			// If a websocket client connects, it should still be allowed, since after all
			// the admin wanted to allow websocket and mqtt connection types.
			// However, say that the JWT only allows "MQTT" (and again suppose this server
			// does not know about MQTT connection type), then since the allowedConnTypes
			// map would be empty (no valid types found), and since empty means allow-all,
			// then we should reject because the intent was to allow connections for this
			// user only as an MQTT client.
			c.Debugf("%v", err)
			if len(allowedConnTypes) == 0 {
				return false
			}
		}
		if !c.connectionTypeAllowed(allowedConnTypes) {
			c.Debugf("Connection type not allowed")
			return false
		}
		issuer := juc.Issuer
		if juc.IssuerAccount != _EMPTY_ {
			issuer = juc.IssuerAccount
		}
		if pinnedAcounts != nil {
			if _, ok := pinnedAcounts[issuer]; !ok {
				c.Debugf("Account %s not listed as operator pinned account", issuer)
				atomic.AddUint64(&s.pinnedAccFail, 1)
				return false
			}
		}
		if acc, err = s.LookupAccount(issuer); acc == nil {
			c.Debugf("Account JWT lookup error: %v", err)
			return false
		}
		if !s.isTrustedIssuer(acc.Issuer) {
			c.Debugf("Account JWT not signed by trusted operator")
			return false
		}
		if scope, ok := acc.hasIssuer(juc.Issuer); !ok {
			c.Debugf("User JWT issuer is not known")
			return false
		} else if scope != nil {
			if err := scope.ValidateScopedSigner(juc); err != nil {
				c.Debugf("User JWT is not valid: %v", err)
				return false
			} else if uSc, ok := scope.(*jwt.UserScope); !ok {
				c.Debugf("User JWT is not valid")
				return false
			} else if juc.UserPermissionLimits, err = processUserPermissionsTemplate(uSc.Template, juc, acc); err != nil {
				c.Debugf("User JWT generated invalid permissions")
				return false
			}
		}
		if acc.IsExpired() {
			c.Debugf("Account JWT has expired")
			return false
		}
		if juc.BearerToken && acc.failBearer() {
			c.Debugf("Account does not allow bearer token")
			return false
		}
		// skip validation of nonce when presented with a bearer token
		// FIXME: if BearerToken is only for WSS, need check for server with that port enabled
		if !juc.BearerToken {
			// Verify the signature against the nonce.
			if c.opts.Sig == _EMPTY_ {
				c.Debugf("Signature missing")
				return false
			}
			sig, err := base64.RawURLEncoding.DecodeString(c.opts.Sig)
			if err != nil {
				// Allow fallback to normal base64.
				sig, err = base64.StdEncoding.DecodeString(c.opts.Sig)
				if err != nil {
					c.Debugf("Signature not valid base64")
					return false
				}
			}
			pub, err := nkeys.FromPublicKey(juc.Subject)
			if err != nil {
				c.Debugf("User nkey not valid: %v", err)
				return false
			}
			if err := pub.Verify(c.nonce, sig); err != nil {
				c.Debugf("Signature not verified")
				return false
			}
		}
		if acc.checkUserRevoked(juc.Subject, juc.IssuedAt) {
			c.Debugf("User authentication revoked")
			return false
		}
		if !validateSrc(juc, c.host) {
			c.Errorf("Bad src Ip %s", c.host)
			return false
		}
		allowNow, validFor := validateTimes(juc)
		if !allowNow {
			c.Errorf("Outside connect times")
			return false
		}

		nkey = buildInternalNkeyUser(juc, allowedConnTypes, acc)
		if err := c.RegisterNkeyUser(nkey); err != nil {
			return false
		}

		// Warn about JetStream restrictions
		if c.perms != nil {
			deniedPub := []string{}
			deniedSub := []string{}
			for _, sub := range denyAllJs {
				if c.perms.pub.deny != nil {
					if r := c.perms.pub.deny.Match(sub); len(r.psubs)+len(r.qsubs) > 0 {
						deniedPub = append(deniedPub, sub)
					}
				}
				if c.perms.sub.deny != nil {
					if r := c.perms.sub.deny.Match(sub); len(r.psubs)+len(r.qsubs) > 0 {
						deniedSub = append(deniedSub, sub)
					}
				}
			}
			if len(deniedPub) > 0 || len(deniedSub) > 0 {
				c.Noticef("Connected %s has JetStream denied on pub: %v sub: %v", c.kindString(), deniedPub, deniedSub)
			}
		}

		// Hold onto the user's public key.
		c.mu.Lock()
		c.pubKey = juc.Subject
		c.tags = juc.Tags
		c.nameTag = juc.Name
		c.mu.Unlock()

		// Check if we need to set an auth timer if the user jwt expires.
		c.setExpiration(juc.Claims(), validFor)

		acc.mu.RLock()
		c.Debugf("Authenticated JWT: %s %q (claim-name: %q, claim-tags: %q) "+
			"signed with %q by Account %q (claim-name: %q, claim-tags: %q) signed with %q has mappings %t accused %p",
			c.kindString(), juc.Subject, juc.Name, juc.Tags, juc.Issuer, issuer, acc.nameTag, acc.tags, acc.Issuer, acc.hasMappingsLocked(), acc)
		acc.mu.RUnlock()
		return true
	}

	if nkey != nil {
		if c.opts.Sig == _EMPTY_ {
			c.Debugf("Signature missing")
			return false
		}
		sig, err := base64.RawURLEncoding.DecodeString(c.opts.Sig)
		if err != nil {
			// Allow fallback to normal base64.
			sig, err = base64.StdEncoding.DecodeString(c.opts.Sig)
			if err != nil {
				c.Debugf("Signature not valid base64")
				return false
			}
		}
		pub, err := nkeys.FromPublicKey(c.opts.Nkey)
		if err != nil {
			c.Debugf("User nkey not valid: %v", err)
			return false
		}
		if err := pub.Verify(c.nonce, sig); err != nil {
			c.Debugf("Signature not verified")
			return false
		}
		if err := c.RegisterNkeyUser(nkey); err != nil {
			return false
		}
		return true
	}
	if user != nil {
		ok = comparePasswords(user.Password, c.opts.Password)
		// If we are authorized, register the user which will properly setup any permissions
		// for pub/sub authorizations.
		if ok {
			c.RegisterUser(user)
		}
		return ok
	}

	if c.kind == CLIENT {
		if token != _EMPTY_ {
			return comparePasswords(token, c.opts.Token)
		} else if username != _EMPTY_ {
			if username != c.opts.Username {
				return false
			}
			return comparePasswords(password, c.opts.Password)
		}
	} else if c.kind == LEAF {
		// There is no required username/password to connect and
		// there was no u/p in the CONNECT or none that matches the
		// know users. Register the leaf connection with global account
		// or the one specified in config (if provided).
		return s.registerLeafWithAccount(c, opts.LeafNode.Account)
	}
	return false
}

func getTLSAuthDCs(rdns *pkix.RDNSequence) string {
	dcOID := asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 25}
	dcs := []string{}
	for _, rdn := range *rdns {
		if len(rdn) == 0 {
			continue
		}
		for _, atv := range rdn {
			value, ok := atv.Value.(string)
			if !ok {
				continue
			}
			if atv.Type.Equal(dcOID) {
				dcs = append(dcs, "DC="+value)
			}
		}
	}
	return strings.Join(dcs, ",")
}

type tlsMapAuthFn func(string, *ldap.DN, bool) (string, bool)

func checkClientTLSCertSubject(c *client, fn tlsMapAuthFn) bool {
	tlsState := c.GetTLSConnectionState()
	if tlsState == nil {
		c.Debugf("User required in cert, no TLS connection state")
		return false
	}
	if len(tlsState.PeerCertificates) == 0 {
		c.Debugf("User required in cert, no peer certificates found")
		return false
	}
	cert := tlsState.PeerCertificates[0]
	if len(tlsState.PeerCertificates) > 1 {
		c.Debugf("Multiple peer certificates found, selecting first")
	}

	hasSANs := len(cert.DNSNames) > 0
	hasEmailAddresses := len(cert.EmailAddresses) > 0
	hasSubject := len(cert.Subject.String()) > 0
	hasURIs := len(cert.URIs) > 0
	if !hasEmailAddresses && !hasSubject && !hasURIs {
		c.Debugf("User required in cert, none found")
		return false
	}

	switch {
	case hasEmailAddresses:
		for _, u := range cert.EmailAddresses {
			if match, ok := fn(u, nil, false); ok {
				c.Debugf("Using email found in cert for auth [%q]", match)
				return true
			}
		}
		fallthrough
	case hasSANs:
		for _, u := range cert.DNSNames {
			if match, ok := fn(u, nil, true); ok {
				c.Debugf("Using SAN found in cert for auth [%q]", match)
				return true
			}
		}
		fallthrough
	case hasURIs:
		for _, u := range cert.URIs {
			if match, ok := fn(u.String(), nil, false); ok {
				c.Debugf("Using URI found in cert for auth [%q]", match)
				return true
			}
		}
	}

	// Use the string representation of the full RDN Sequence including
	// the domain components in case there are any.
	rdn := cert.Subject.ToRDNSequence().String()

	// Match using the raw subject to avoid ignoring attributes.
	// https://github.com/golang/go/issues/12342
	dn, err := ldap.FromRawCertSubject(cert.RawSubject)
	if err == nil {
		if match, ok := fn("", dn, false); ok {
			c.Debugf("Using DistinguishedNameMatch for auth [%q]", match)
			return true
		}
		c.Debugf("DistinguishedNameMatch could not be used for auth [%q]", rdn)
	}

	var rdns pkix.RDNSequence
	if _, err := asn1.Unmarshal(cert.RawSubject, &rdns); err == nil {
		// If found domain components then include roughly following
		// the order from https://tools.ietf.org/html/rfc2253
		//
		// NOTE: The original sequence from string representation by ToRDNSequence does not follow
		// the correct ordering, so this addition ofdomainComponents would likely be deprecated in
		// another release in favor of using the correct ordered as parsed by the go-ldap library.
		//
		dcs := getTLSAuthDCs(&rdns)
		if len(dcs) > 0 {
			u := strings.Join([]string{rdn, dcs}, ",")
			if match, ok := fn(u, nil, false); ok {
				c.Debugf("Using RDNSequence for auth [%q]", match)
				return true
			}
			c.Debugf("RDNSequence could not be used for auth [%q]", u)
		}
	}

	// If no match, then use the string representation of the RDNSequence
	// from the subject without the domainComponents.
	if match, ok := fn(rdn, nil, false); ok {
		c.Debugf("Using certificate subject for auth [%q]", match)
		return true
	}

	c.Debugf("User in cert [%q], not found", rdn)
	return false
}

func dnsAltNameLabels(dnsAltName string) []string {
	return strings.Split(strings.ToLower(dnsAltName), ".")
}

// Check DNS name according to https://tools.ietf.org/html/rfc6125#section-6.4.1
func dnsAltNameMatches(dnsAltNameLabels []string, urls []*url.URL) bool {
URLS:
	for _, url := range urls {
		if url == nil {
			continue URLS
		}
		hostLabels := strings.Split(strings.ToLower(url.Hostname()), ".")
		// Following https://tools.ietf.org/html/rfc6125#section-6.4.3, should not => will not, may => will not
		// The wildcard * never matches multiple label and only matches the left most label.
		if len(hostLabels) != len(dnsAltNameLabels) {
			continue URLS
		}
		i := 0
		// only match wildcard on left most label
		if dnsAltNameLabels[0] == "*" {
			i++
		}
		for ; i < len(dnsAltNameLabels); i++ {
			if dnsAltNameLabels[i] != hostLabels[i] {
				continue URLS
			}
		}
		return true
	}
	return false
}

// checkRouterAuth checks optional router authorization which can be nil or username/password.
func (s *Server) isRouterAuthorized(c *client) bool {
	// Snapshot server options.
	opts := s.getOpts()

	// Check custom auth first, then TLS map if enabled
	// then single user/pass.
	if s.opts.CustomRouterAuthentication != nil {
		return s.opts.CustomRouterAuthentication.Check(c)
	}

	if opts.Cluster.TLSMap || opts.Cluster.TLSCheckKnownURLs {
		return checkClientTLSCertSubject(c, func(user string, _ *ldap.DN, isDNSAltName bool) (string, bool) {
			if user == "" {
				return "", false
			}
			if opts.Cluster.TLSCheckKnownURLs && isDNSAltName {
				if dnsAltNameMatches(dnsAltNameLabels(user), opts.Routes) {
					return "", true
				}
			}
			if opts.Cluster.TLSMap && opts.Cluster.Username == user {
				return "", true
			}
			return "", false
		})
	}

	if opts.Cluster.Username == "" {
		return true
	}

	if opts.Cluster.Username != c.opts.Username {
		return false
	}
	if !comparePasswords(opts.Cluster.Password, c.opts.Password) {
		return false
	}
	return true
}

// isGatewayAuthorized checks optional gateway authorization which can be nil or username/password.
func (s *Server) isGatewayAuthorized(c *client) bool {
	// Snapshot server options.
	opts := s.getOpts()

	// Check whether TLS map is enabled, otherwise use single user/pass.
	if opts.Gateway.TLSMap || opts.Gateway.TLSCheckKnownURLs {
		return checkClientTLSCertSubject(c, func(user string, _ *ldap.DN, isDNSAltName bool) (string, bool) {
			if user == "" {
				return "", false
			}
			if opts.Gateway.TLSCheckKnownURLs && isDNSAltName {
				labels := dnsAltNameLabels(user)
				for _, gw := range opts.Gateway.Gateways {
					if gw != nil && dnsAltNameMatches(labels, gw.URLs) {
						return "", true
					}
				}
			}
			if opts.Gateway.TLSMap && opts.Gateway.Username == user {
				return "", true
			}
			return "", false
		})
	}

	if opts.Gateway.Username == "" {
		return true
	}

	if opts.Gateway.Username != c.opts.Username {
		return false
	}
	return comparePasswords(opts.Gateway.Password, c.opts.Password)
}

func (s *Server) registerLeafWithAccount(c *client, account string) bool {
	var err error
	acc := s.globalAccount()
	if account != _EMPTY_ {
		acc, err = s.lookupAccount(account)
		if err != nil {
			s.Errorf("authentication of user %q failed, unable to lookup account %q: %v",
				c.opts.Username, account, err)
			return false
		}
	}
	if err = c.registerWithAccount(acc); err != nil {
		return false
	}
	return true
}

// isLeafNodeAuthorized will check for auth for an inbound leaf node connection.
func (s *Server) isLeafNodeAuthorized(c *client) bool {
	opts := s.getOpts()

	isAuthorized := func(username, password, account string) bool {
		if username != c.opts.Username {
			return false
		}
		if !comparePasswords(password, c.opts.Password) {
			return false
		}
		return s.registerLeafWithAccount(c, account)
	}

	// If leafnodes config has an authorization{} stanza, this takes precedence.
	// The user in CONNECT must match. We will bind to the account associated
	// with that user (from the leafnode's authorization{} config).
	if opts.LeafNode.Username != _EMPTY_ {
		return isAuthorized(opts.LeafNode.Username, opts.LeafNode.Password, opts.LeafNode.Account)
	} else if len(opts.LeafNode.Users) > 0 {
		if opts.LeafNode.TLSMap {
			var user *User
			found := checkClientTLSCertSubject(c, func(u string, _ *ldap.DN, _ bool) (string, bool) {
				// This is expected to be a very small array.
				for _, usr := range opts.LeafNode.Users {
					if u == usr.Username {
						user = usr
						return u, true
					}
				}
				return _EMPTY_, false
			})
			if !found {
				return false
			}
			if c.opts.Username != _EMPTY_ {
				s.Warnf("User %q found in connect proto, but user required from cert", c.opts.Username)
			}
			c.opts.Username = user.Username
			// EMPTY will result in $G
			accName := _EMPTY_
			if user.Account != nil {
				accName = user.Account.GetName()
			}
			// This will authorize since are using an existing user,
			// but it will also register with proper account.
			return isAuthorized(user.Username, user.Password, accName)
		}

		// This is expected to be a very small array.
		for _, u := range opts.LeafNode.Users {
			if u.Username == c.opts.Username {
				var accName string
				if u.Account != nil {
					accName = u.Account.Name
				}
				return isAuthorized(u.Username, u.Password, accName)
			}
		}
		return false
	}

	// We are here if we accept leafnode connections without any credentials.

	// Still, if the CONNECT has some user info, we will bind to the
	// user's account or to the specified default account (if provided)
	// or to the global account.
	return s.isClientAuthorized(c)
}

// Support for bcrypt stored passwords and tokens.
var validBcryptPrefix = regexp.MustCompile(`^\$2[abxy]\$\d{2}\$.*`)

// isBcrypt checks whether the given password or token is bcrypted.
func isBcrypt(password string) bool {
	if strings.HasPrefix(password, "$") {
		return validBcryptPrefix.MatchString(password)
	}

	return false
}

func comparePasswords(serverPassword, clientPassword string) bool {
	// Check to see if the server password is a bcrypt hash
	if isBcrypt(serverPassword) {
		if err := bcrypt.CompareHashAndPassword([]byte(serverPassword), []byte(clientPassword)); err != nil {
			return false
		}
	} else if serverPassword != clientPassword {
		return false
	}
	return true
}

func validateAuth(o *Options) error {
	if err := validatePinnedCerts(o.TLSPinnedCerts); err != nil {
		return err
	}
	for _, u := range o.Users {
		if err := validateAllowedConnectionTypes(u.AllowedConnectionTypes); err != nil {
			return err
		}
	}
	for _, u := range o.Nkeys {
		if err := validateAllowedConnectionTypes(u.AllowedConnectionTypes); err != nil {
			return err
		}
	}
	return validateNoAuthUser(o, o.NoAuthUser)
}

func validateAllowedConnectionTypes(m map[string]struct{}) error {
	for ct := range m {
		ctuc := strings.ToUpper(ct)
		switch ctuc {
		case jwt.ConnectionTypeStandard, jwt.ConnectionTypeWebsocket,
			jwt.ConnectionTypeLeafnode, jwt.ConnectionTypeLeafnodeWS,
			jwt.ConnectionTypeMqtt, jwt.ConnectionTypeMqttWS:
		default:
			return fmt.Errorf("unknown connection type %q", ct)
		}
		if ctuc != ct {
			delete(m, ct)
			m[ctuc] = struct{}{}
		}
	}
	return nil
}

func validateNoAuthUser(o *Options, noAuthUser string) error {
	if noAuthUser == _EMPTY_ {
		return nil
	}
	if len(o.TrustedOperators) > 0 {
		return fmt.Errorf("no_auth_user not compatible with Trusted Operator")
	}
	if o.Users == nil {
		return fmt.Errorf(`no_auth_user: "%s" present, but users are not defined`, noAuthUser)
	}
	for _, u := range o.Users {
		if u.Username == noAuthUser {
			return nil
		}
	}
	return fmt.Errorf(
		`no_auth_user: "%s" not present as user in authorization block or account configuration`,
		noAuthUser)
}
