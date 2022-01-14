package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// PerZoneAuthenticatedOriginPullsSettings represents the settings for Per Zone AuthenticatedOriginPulls.
type PerZoneAuthenticatedOriginPullsSettings struct {
	Enabled bool `json:"enabled"`
}

// PerZoneAuthenticatedOriginPullsSettingsResponse represents the response from the Per Zone AuthenticatedOriginPulls settings endpoint.
type PerZoneAuthenticatedOriginPullsSettingsResponse struct {
	Response
	Result PerZoneAuthenticatedOriginPullsSettings `json:"result"`
}

// PerZoneAuthenticatedOriginPullsCertificateDetails represents the metadata for a Per Zone AuthenticatedOriginPulls client certificate.
type PerZoneAuthenticatedOriginPullsCertificateDetails struct {
	ID          string    `json:"id"`
	Certificate string    `json:"certificate"`
	Issuer      string    `json:"issuer"`
	Signature   string    `json:"signature"`
	ExpiresOn   time.Time `json:"expires_on"`
	Status      string    `json:"status"`
	UploadedOn  time.Time `json:"uploaded_on"`
}

// PerZoneAuthenticatedOriginPullsCertificateResponse represents the response from endpoints relating to creating and deleting a per zone AuthenticatedOriginPulls certificate.
type PerZoneAuthenticatedOriginPullsCertificateResponse struct {
	Response
	Result PerZoneAuthenticatedOriginPullsCertificateDetails `json:"result"`
}

// PerZoneAuthenticatedOriginPullsCertificatesResponse represents the response from the per zone AuthenticatedOriginPulls certificate list endpoint.
type PerZoneAuthenticatedOriginPullsCertificatesResponse struct {
	Response
	Result []PerZoneAuthenticatedOriginPullsCertificateDetails `json:"result"`
}

// PerZoneAuthenticatedOriginPullsCertificateParams represents the required data related to the client certificate being uploaded to be used in Per Zone AuthenticatedOriginPulls.
type PerZoneAuthenticatedOriginPullsCertificateParams struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

// GetPerZoneAuthenticatedOriginPullsStatus returns whether per zone AuthenticatedOriginPulls is enabled or not. It is false by default.
//
// API reference: https://api.cloudflare.com/#zone-level-authenticated-origin-pulls-get-enablement-setting-for-zone
func (api *API) GetPerZoneAuthenticatedOriginPullsStatus(ctx context.Context, zoneID string) (PerZoneAuthenticatedOriginPullsSettings, error) {
	uri := fmt.Sprintf("/zones/%s/origin_tls_client_auth/settings", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return PerZoneAuthenticatedOriginPullsSettings{}, err
	}
	var r PerZoneAuthenticatedOriginPullsSettingsResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return PerZoneAuthenticatedOriginPullsSettings{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// SetPerZoneAuthenticatedOriginPullsStatus will update whether Per Zone AuthenticatedOriginPulls is enabled for the zone.
//
// API reference: https://api.cloudflare.com/#zone-level-authenticated-origin-pulls-set-enablement-for-zone
func (api *API) SetPerZoneAuthenticatedOriginPullsStatus(ctx context.Context, zoneID string, enable bool) (PerZoneAuthenticatedOriginPullsSettings, error) {
	uri := fmt.Sprintf("/zones/%s/origin_tls_client_auth/settings", zoneID)
	params := struct {
		Enabled bool `json:"enabled"`
	}{
		Enabled: enable,
	}
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, params)
	if err != nil {
		return PerZoneAuthenticatedOriginPullsSettings{}, err
	}
	var r PerZoneAuthenticatedOriginPullsSettingsResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return PerZoneAuthenticatedOriginPullsSettings{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UploadPerZoneAuthenticatedOriginPullsCertificate will upload a provided client certificate and enable it to be used in all AuthenticatedOriginPulls requests for the zone.
//
// API reference: https://api.cloudflare.com/#zone-level-authenticated-origin-pulls-upload-certificate
func (api *API) UploadPerZoneAuthenticatedOriginPullsCertificate(ctx context.Context, zoneID string, params PerZoneAuthenticatedOriginPullsCertificateParams) (PerZoneAuthenticatedOriginPullsCertificateDetails, error) {
	uri := fmt.Sprintf("/zones/%s/origin_tls_client_auth", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, params)
	if err != nil {
		return PerZoneAuthenticatedOriginPullsCertificateDetails{}, err
	}
	var r PerZoneAuthenticatedOriginPullsCertificateResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return PerZoneAuthenticatedOriginPullsCertificateDetails{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListPerZoneAuthenticatedOriginPullsCertificates returns a list of all user uploaded client certificates to Per Zone AuthenticatedOriginPulls.
//
// API reference: https://api.cloudflare.com/#zone-level-authenticated-origin-pulls-list-certificates
func (api *API) ListPerZoneAuthenticatedOriginPullsCertificates(ctx context.Context, zoneID string) ([]PerZoneAuthenticatedOriginPullsCertificateDetails, error) {
	uri := fmt.Sprintf("/zones/%s/origin_tls_client_auth", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []PerZoneAuthenticatedOriginPullsCertificateDetails{}, err
	}
	var r PerZoneAuthenticatedOriginPullsCertificatesResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return []PerZoneAuthenticatedOriginPullsCertificateDetails{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// GetPerZoneAuthenticatedOriginPullsCertificateDetails returns the metadata associated with a user uploaded client certificate to Per Zone AuthenticatedOriginPulls.
//
// API reference: https://api.cloudflare.com/#zone-level-authenticated-origin-pulls-get-certificate-details
func (api *API) GetPerZoneAuthenticatedOriginPullsCertificateDetails(ctx context.Context, zoneID, certificateID string) (PerZoneAuthenticatedOriginPullsCertificateDetails, error) {
	uri := fmt.Sprintf("/zones/%s/origin_tls_client_auth/%s", zoneID, certificateID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return PerZoneAuthenticatedOriginPullsCertificateDetails{}, err
	}
	var r PerZoneAuthenticatedOriginPullsCertificateResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return PerZoneAuthenticatedOriginPullsCertificateDetails{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// DeletePerZoneAuthenticatedOriginPullsCertificate removes the specified client certificate from the edge.
//
// API reference: https://api.cloudflare.com/#zone-level-authenticated-origin-pulls-delete-certificate
func (api *API) DeletePerZoneAuthenticatedOriginPullsCertificate(ctx context.Context, zoneID, certificateID string) (PerZoneAuthenticatedOriginPullsCertificateDetails, error) {
	uri := fmt.Sprintf("/zones/%s/origin_tls_client_auth/%s", zoneID, certificateID)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return PerZoneAuthenticatedOriginPullsCertificateDetails{}, err
	}
	var r PerZoneAuthenticatedOriginPullsCertificateResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return PerZoneAuthenticatedOriginPullsCertificateDetails{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}
