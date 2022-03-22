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
	"net/http"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/keytab"
	gokrbSPNEGO "github.com/jcmturner/gokrb5/v8/spnego"
	digest_auth_client "github.com/xinsnake/go-http-digest-auth-client"
	"golang.org/x/xerrors"
)

// WithDigestAuth takes an http client and prepares it to authenticate using digest authentication
func WithDigestAuth(cli *http.Client, username, password string) *http.Client {
	rt := digest_auth_client.NewTransport(username, password)
	cli.Transport = &rt
	return cli
}

// WithBasicAuth takes an http client and prepares it to authenticate using basic authentication
func WithBasicAuth(cli *http.Client, username, password string) *http.Client {
	rt := &basicAuthTransport{cli.Transport, username, password}
	cli.Transport = rt
	return cli
}

// WithKerberosAuth takes an http client prepares it to authenticate using kerberos
func WithKerberosAuth(cli *http.Client, username, realm, keyTab, krb5Conf, krb5CredentialCache string) (*http.Client, error) {
	var kerberosClient *client.Client
	if krb5CredentialCache != "" {
		tc, err := credentials.LoadCCache(krb5CredentialCache)
		if err != nil {
			return nil, xerrors.Errorf("error reading kerberos ticket cache: %v", err)
		}
		kc, err := client.NewFromCCache(tc, config.New())
		if err != nil {
			return nil, xerrors.Errorf("error creating kerberos client: %v", err)
		}
		kerberosClient = kc
	} else {
		cfg, err := config.Load(krb5Conf)
		if err != nil {
			return nil, xerrors.Errorf("error reading kerberos config: %v", err)
		}
		kt, err := keytab.Load(keyTab)
		if err != nil {
			return nil, xerrors.Errorf("error reading kerberos keytab: %v", err)
		}
		kc := client.NewWithKeytab(username, realm, kt, cfg)
		err = kc.Login()
		if err != nil {
			return nil, xerrors.Errorf("error performing kerberos login with keytab: %v", err)
		}
		kerberosClient = kc
	}
	rt := &krb5Transport{cli.Transport, kerberosClient}
	cli.Transport = rt
	return cli, nil
}

// WithAdditionalHeaders wraps a http client to always include the given set of headers
func WithAdditionalHeaders(cli *http.Client, headers http.Header) *http.Client {
	rt := &additionalHeaderTransport{cli.Transport, headers}
	cli.Transport = rt
	return cli
}

type basicAuthTransport struct {
	baseTransport      http.RoundTripper
	username, password string
}

// RoundTrip implements the http.RoundTripper interface
func (t *basicAuthTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	req.SetBasicAuth(t.username, t.password)
	return t.baseTransport.RoundTrip(req)
}

type krb5Transport struct {
	baseTransport  http.RoundTripper
	kerberosClient *client.Client
}

// RoundTrip implements the http.RoundTripper interface
func (t *krb5Transport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	err = gokrbSPNEGO.SetSPNEGOHeader(t.kerberosClient, req, "")
	if err != nil {
		return nil, xerrors.Errorf("error setting SPNEGO header: %v", err)
	}
	return t.baseTransport.RoundTrip(req)
}

type additionalHeaderTransport struct {
	baseTransport http.RoundTripper
	headers       http.Header
}

func (t *additionalHeaderTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	for key, vals := range t.headers {
		req.Header[key] = append(req.Header[key], vals...)
	}
	return t.baseTransport.RoundTrip(req)
}
