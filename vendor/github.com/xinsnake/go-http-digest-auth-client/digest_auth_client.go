package digest_auth_client

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"
)

type DigestRequest struct {
	Body       string
	Method     string
	Password   string
	URI        string
	Username   string
	Header     http.Header
	Auth       *authorization
	Wa         *wwwAuthenticate
	CertVal    bool
	HTTPClient *http.Client
}

type DigestTransport struct {
	Password   string
	Username   string
	HTTPClient *http.Client
}

// NewRequest creates a new DigestRequest object
func NewRequest(username, password, method, uri, body string) DigestRequest {
	dr := DigestRequest{}
	dr.UpdateRequest(username, password, method, uri, body)
	dr.CertVal = true
	return dr
}

// NewTransport creates a new DigestTransport object
func NewTransport(username, password string) DigestTransport {
	dt := DigestTransport{}
	dt.Password = password
	dt.Username = username
	return dt
}

func (dr *DigestRequest) getHTTPClient() *http.Client {
	if dr.HTTPClient != nil {
		return dr.HTTPClient
	}
	tlsConfig := tls.Config{}
	if !dr.CertVal {
		tlsConfig.InsecureSkipVerify = true
	}

	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tlsConfig,
		},
	}
}

// UpdateRequest is called when you want to reuse an existing
//  DigestRequest connection with new request information
func (dr *DigestRequest) UpdateRequest(username, password, method, uri, body string) *DigestRequest {
	dr.Body = body
	dr.Method = method
	dr.Password = password
	dr.URI = uri
	dr.Username = username
	dr.Header = make(map[string][]string)
	return dr
}

// RoundTrip implements the http.RoundTripper interface
func (dt *DigestTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	username := dt.Username
	password := dt.Password
	method := req.Method
	uri := req.URL.String()

	var body string
	if req.Body != nil {
		buf := new(bytes.Buffer)
		buf.ReadFrom(req.Body)
		body = buf.String()
	}

	dr := NewRequest(username, password, method, uri, body)
	if dt.HTTPClient != nil {
		dr.HTTPClient = dt.HTTPClient
	}

	return dr.Execute()
}

// Execute initialise the request and get a response
func (dr *DigestRequest) Execute() (resp *http.Response, err error) {

	if dr.Auth != nil {
		return dr.executeExistingDigest()
	}

	var req *http.Request
	if req, err = http.NewRequest(dr.Method, dr.URI, bytes.NewReader([]byte(dr.Body))); err != nil {
		return nil, err
	}
	req.Header = dr.Header

	client := dr.getHTTPClient()

	if resp, err = client.Do(req); err != nil {
		return nil, err
	}

	if resp.StatusCode == 401 {
		return dr.executeNewDigest(resp)
	}

	// return the resp to user to handle resp.body.Close()
	return resp, nil
}

func (dr *DigestRequest) executeNewDigest(resp *http.Response) (resp2 *http.Response, err error) {
	var (
		auth     *authorization
		wa       *wwwAuthenticate
		waString string
	)

	// body not required for authentication, closing
	resp.Body.Close()

	if waString = resp.Header.Get("WWW-Authenticate"); waString == "" {
		return nil, fmt.Errorf("failed to get WWW-Authenticate header, please check your server configuration")
	}
	wa = newWwwAuthenticate(waString)
	dr.Wa = wa

	if auth, err = newAuthorization(dr); err != nil {
		return nil, err
	}

	if resp2, err = dr.executeRequest(auth.toString()); err != nil {
		return nil, err
	}

	dr.Auth = auth
	return resp2, nil
}

func (dr *DigestRequest) executeExistingDigest() (resp *http.Response, err error) {
	var auth *authorization

	if auth, err = dr.Auth.refreshAuthorization(dr); err != nil {
		return nil, err
	}
	dr.Auth = auth

	return dr.executeRequest(dr.Auth.toString())
}

func (dr *DigestRequest) executeRequest(authString string) (resp *http.Response, err error) {
	var req *http.Request

	if req, err = http.NewRequest(dr.Method, dr.URI, bytes.NewReader([]byte(dr.Body))); err != nil {
		return nil, err
	}
	req.Header = dr.Header
	req.Header.Add("Authorization", authString)

	client := dr.getHTTPClient()
	return client.Do(req)
}
