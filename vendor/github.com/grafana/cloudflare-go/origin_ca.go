package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
)

// OriginCACertificate represents a Cloudflare-issued certificate.
//
// API reference: https://api.cloudflare.com/#cloudflare-ca
type OriginCACertificate struct {
	ID              string    `json:"id"`
	Certificate     string    `json:"certificate"`
	Hostnames       []string  `json:"hostnames"`
	ExpiresOn       time.Time `json:"expires_on"`
	RequestType     string    `json:"request_type"`
	RequestValidity int       `json:"requested_validity"`
	RevokedAt       time.Time `json:"revoked_at,omitempty"`
	CSR             string    `json:"csr"`
}

// UnmarshalJSON handles custom parsing from an API response to an OriginCACertificate
// http://choly.ca/post/go-json-marshalling/
func (c *OriginCACertificate) UnmarshalJSON(data []byte) error {
	type alias OriginCACertificate

	aux := &struct {
		ExpiresOn string `json:"expires_on"`
		*alias
	}{
		alias: (*alias)(c),
	}

	var err error

	if err = json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// This format comes from time.Time.String() source
	c.ExpiresOn, err = time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", aux.ExpiresOn)

	if err != nil {
		c.ExpiresOn, err = time.Parse(time.RFC3339, aux.ExpiresOn)
	}

	if err != nil {
		return err
	}

	return nil
}

// OriginCACertificateListOptions represents the parameters used to list Cloudflare-issued certificates.
type OriginCACertificateListOptions struct {
	ZoneID string
}

// OriginCACertificateID represents the ID of the revoked certificate from the Revoke Certificate endpoint.
type OriginCACertificateID struct {
	ID string `json:"id"`
}

// originCACertificateResponse represents the response from the Create Certificate and the Certificate Details endpoints.
type originCACertificateResponse struct {
	Response
	Result OriginCACertificate `json:"result"`
}

// originCACertificateResponseList represents the response from the List Certificates endpoint.
type originCACertificateResponseList struct {
	Response
	Result     []OriginCACertificate `json:"result"`
	ResultInfo ResultInfo            `json:"result_info"`
}

// originCACertificateResponseRevoke represents the response from the Revoke Certificate endpoint.
type originCACertificateResponseRevoke struct {
	Response
	Result OriginCACertificateID `json:"result"`
}

// CreateOriginCertificate creates a Cloudflare-signed certificate.
//
// This function requires api.APIUserServiceKey be set to your Certificates API key.
//
// API reference: https://api.cloudflare.com/#cloudflare-ca-create-certificate
func (api *API) CreateOriginCertificate(ctx context.Context, certificate OriginCACertificate) (*OriginCACertificate, error) {
	uri := "/certificates"
	res, err := api.makeRequestWithAuthType(ctx, http.MethodPost, uri, certificate, AuthUserService)

	if err != nil {
		return nil, err
	}

	var originResponse *originCACertificateResponse

	err = json.Unmarshal(res, &originResponse)

	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	if !originResponse.Success {
		return nil, errors.New(errRequestNotSuccessful)
	}

	return &originResponse.Result, nil
}

// OriginCertificates lists all Cloudflare-issued certificates.
//
// This function requires api.APIUserServiceKey be set to your Certificates API key.
//
// API reference: https://api.cloudflare.com/#cloudflare-ca-list-certificates
func (api *API) OriginCertificates(ctx context.Context, options OriginCACertificateListOptions) ([]OriginCACertificate, error) {
	v := url.Values{}
	if options.ZoneID != "" {
		v.Set("zone_id", options.ZoneID)
	}
	uri := fmt.Sprintf("/certificates?%s", v.Encode())
	res, err := api.makeRequestWithAuthType(ctx, http.MethodGet, uri, nil, AuthUserService)

	if err != nil {
		return nil, err
	}

	var originResponse *originCACertificateResponseList

	err = json.Unmarshal(res, &originResponse)

	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	if !originResponse.Success {
		return nil, errors.New(errRequestNotSuccessful)
	}

	return originResponse.Result, nil
}

// OriginCertificate returns the details for a Cloudflare-issued certificate.
//
// This function requires api.APIUserServiceKey be set to your Certificates API key.
//
// API reference: https://api.cloudflare.com/#cloudflare-ca-certificate-details
func (api *API) OriginCertificate(ctx context.Context, certificateID string) (*OriginCACertificate, error) {
	uri := fmt.Sprintf("/certificates/%s", certificateID)
	res, err := api.makeRequestWithAuthType(ctx, http.MethodGet, uri, nil, AuthUserService)

	if err != nil {
		return nil, err
	}

	var originResponse *originCACertificateResponse

	err = json.Unmarshal(res, &originResponse)

	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	if !originResponse.Success {
		return nil, errors.New(errRequestNotSuccessful)
	}

	return &originResponse.Result, nil
}

// RevokeOriginCertificate revokes a created certificate for a zone.
//
// This function requires api.APIUserServiceKey be set to your Certificates API key.
//
// API reference: https://api.cloudflare.com/#cloudflare-ca-revoke-certificate
func (api *API) RevokeOriginCertificate(ctx context.Context, certificateID string) (*OriginCACertificateID, error) {
	uri := fmt.Sprintf("/certificates/%s", certificateID)
	res, err := api.makeRequestWithAuthType(ctx, http.MethodDelete, uri, nil, AuthUserService)

	if err != nil {
		return nil, err
	}

	var originResponse *originCACertificateResponseRevoke

	err = json.Unmarshal(res, &originResponse)

	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	if !originResponse.Success {
		return nil, errors.New(errRequestNotSuccessful)
	}

	return &originResponse.Result, nil
}

// Gets the Cloudflare Origin CA Root Certificate for a given algorithm in PEM format.
// Algorithm must be one of ['ecc', 'rsa'].
func OriginCARootCertificate(algorithm string) ([]byte, error) {
	var url string
	switch algorithm {
	case "ecc":
		url = originCARootCertEccURL
	case "rsa":
		url = originCARootCertRsaURL
	default:
		return nil, fmt.Errorf("invalid algorithm: must be one of ['ecc', 'rsa']")
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, errors.Wrap(err, "HTTP request failed")
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "Response body could not be read")
	}

	return body, nil
}
