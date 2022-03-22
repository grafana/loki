/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to you under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package avatica

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/xerrors"
)

type authentication int

const (
	none authentication = iota
	basic
	digest
	spnego
)

// Config is a configuration parsed from a DSN string
type Config struct {
	endpoint             string
	maxRowsTotal         int64
	frameMaxSize         int32
	location             *time.Location
	schema               string
	transactionIsolation uint32
	batching             bool

	authentication      authentication
	avaticaUser         string
	avaticaPassword     string
	principal           krb5Principal
	keytab              string
	krb5Conf            string
	krb5CredentialCache string
}

type krb5Principal struct {
	username string
	realm    string
}

// ParseDSN parses a DSN string to a Config
func ParseDSN(dsn string) (*Config, error) {

	conf := &Config{
		maxRowsTotal:         -1,
		frameMaxSize:         -1,
		location:             time.UTC,
		transactionIsolation: 0,
		batching:             false,
	}

	parsed, err := url.ParseRequestURI(dsn)

	if err != nil {
		return nil, xerrors.Errorf("unable to parse DSN: %v", err)
	}

	queries := parsed.Query()

	if v := queries.Get("maxRowsTotal"); v != "" {

		maxRowTotal, err := strconv.Atoi(v)

		if err != nil {
			return nil, xerrors.Errorf("invalid value for maxRowsTotal: %v", err)
		}

		conf.maxRowsTotal = int64(maxRowTotal)
	}

	if v := queries.Get("frameMaxSize"); v != "" {

		maxRowTotal, err := strconv.Atoi(v)

		if err != nil {
			return nil, xerrors.Errorf("invalid value for frameMaxSize: %v", err)
		}

		conf.frameMaxSize = int32(maxRowTotal)
	}

	if v := queries.Get("location"); v != "" {

		loc, err := time.LoadLocation(v)

		if err != nil {
			return nil, xerrors.Errorf("invalid value for location: %v", err)
		}

		conf.location = loc
	}

	if v := queries.Get("transactionIsolation"); v != "" {

		isolation, err := strconv.Atoi(v)

		if err != nil {
			return nil, xerrors.Errorf("invalid value for transactionIsolation: %v", err)
		}

		if isolation < 0 || isolation > 8 || isolation&(isolation-1) != 0 {
			return nil, xerrors.Errorf("transactionIsolation must be 0, 1, 2, 4 or 8, %d given", isolation)
		}

		conf.transactionIsolation = uint32(isolation)
	}

	if v := queries.Get("batching"); v != "" {
		if v == "true" {
			conf.batching = true
		}
	}

	if v := queries.Get("authentication"); v != "" {

		auth := strings.ToUpper(v)

		if auth == "BASIC" {
			conf.authentication = basic
		} else if auth == "DIGEST" {
			conf.authentication = digest
		} else if auth == "SPNEGO" {
			conf.authentication = spnego
		} else {
			return nil, xerrors.New("authentication must be either BASIC, DIGEST or SPNEGO")
		}

		if conf.authentication == basic || conf.authentication == digest {

			user := queries.Get("avaticaUser")

			if user == "" {
				return nil, xerrors.Errorf("authentication is set to %s, but avaticaUser is empty", v)
			}

			conf.avaticaUser = user

			pass := queries.Get("avaticaPassword")

			if pass == "" {
				return nil, xerrors.Errorf("authentication is set to %s, but avaticaPassword is empty", v)
			}

			conf.avaticaPassword = pass

		} else if conf.authentication == spnego {
			principal := queries.Get("principal")

			keytab := queries.Get("keytab")

			krb5Conf := queries.Get("krb5Conf")

			krb5CredentialCache := queries.Get("krb5CredentialCache")

			if principal == "" && keytab == "" && krb5Conf == "" && krb5CredentialCache == "" {
				return nil, xerrors.New("when using SPNEGO authetication, you must provide the principal, keytab and krb5Conf parameters or a krb5TicketCache parameter")
			}

			if !((principal != "" && keytab != "" && krb5Conf != "") || (principal == "" && keytab == "" && krb5Conf == "")) {
				return nil, xerrors.New("when using SPNEGO authentication with a principal and keytab, the principal, keytab and krb5Conf parameters are required")
			}

			if (principal != "" || keytab != "" || krb5Conf != "") && krb5CredentialCache != "" {
				return nil, xerrors.New("ambigious configuration for SPNEGO authentication: use either principal, keytab and krb5Conf or krb5TicketCache")
			}

			if principal != "" {

				splittedPrincipal := strings.Split(principal, "@")

				if len(splittedPrincipal) != 2 {
					return nil, xerrors.Errorf("invalid kerberos principal (%s): the principal should be in the format primary/instance@realm where instance is optional", principal)
				}

				conf.principal = krb5Principal{
					username: splittedPrincipal[0],
					realm:    splittedPrincipal[1],
				}

				conf.keytab = keytab
				conf.krb5Conf = krb5Conf
			} else if krb5CredentialCache != "" {
				conf.krb5CredentialCache = krb5CredentialCache
			}
		}
	}

	if parsed.Path != "" {
		conf.schema = strings.TrimPrefix(parsed.Path, "/")
	}

	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""

	conf.endpoint = parsed.String()

	return conf, nil
}
