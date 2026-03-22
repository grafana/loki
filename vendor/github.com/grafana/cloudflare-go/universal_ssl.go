package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

// UniversalSSLSetting represents a universal ssl setting's properties.
type UniversalSSLSetting struct {
	Enabled bool `json:"enabled"`
}

type universalSSLSettingResponse struct {
	Response
	Result UniversalSSLSetting `json:"result"`
}

// UniversalSSLVerificationDetails represents a universal ssl verification's properties.
type UniversalSSLVerificationDetails struct {
	CertificateStatus  string                       `json:"certificate_status"`
	VerificationType   string                       `json:"verification_type"`
	ValidationMethod   string                       `json:"validation_method"`
	CertPackUUID       string                       `json:"cert_pack_uuid"`
	VerificationStatus bool                         `json:"verification_status"`
	BrandCheck         bool                         `json:"brand_check"`
	VerificationInfo   UniversalSSLVerificationInfo `json:"verification_info"`
}

// UniversalSSLVerificationInfo represents DCV record.
type UniversalSSLVerificationInfo struct {
	RecordName   string `json:"record_name"`
	RecordTarget string `json:"record_target"`
}

type universalSSLVerificationResponse struct {
	Response
	Result []UniversalSSLVerificationDetails `json:"result"`
}

type UniversalSSLCertificatePackValidationMethodSetting struct {
	ValidationMethod string `json:"validation_method"`
}

type universalSSLCertificatePackValidationMethodSettingResponse struct {
	Response
	Result UniversalSSLCertificatePackValidationMethodSetting `json:"result"`
}

// UniversalSSLSettingDetails returns the details for a universal ssl setting
//
// API reference: https://api.cloudflare.com/#universal-ssl-settings-for-a-zone-universal-ssl-settings-details
func (api *API) UniversalSSLSettingDetails(ctx context.Context, zoneID string) (UniversalSSLSetting, error) {
	uri := fmt.Sprintf("/zones/%s/ssl/universal/settings", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return UniversalSSLSetting{}, err
	}
	var r universalSSLSettingResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return UniversalSSLSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// EditUniversalSSLSetting edits the universal ssl setting for a zone
//
// API reference: https://api.cloudflare.com/#universal-ssl-settings-for-a-zone-edit-universal-ssl-settings
func (api *API) EditUniversalSSLSetting(ctx context.Context, zoneID string, setting UniversalSSLSetting) (UniversalSSLSetting, error) {
	uri := fmt.Sprintf("/zones/%s/ssl/universal/settings", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, setting)
	if err != nil {
		return UniversalSSLSetting{}, err
	}
	var r universalSSLSettingResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return UniversalSSLSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil

}

// UniversalSSLVerificationDetails returns the details for a universal ssl verification
//
// API reference: https://api.cloudflare.com/#ssl-verification-ssl-verification-details
func (api *API) UniversalSSLVerificationDetails(ctx context.Context, zoneID string) ([]UniversalSSLVerificationDetails, error) {
	uri := fmt.Sprintf("/zones/%s/ssl/verification", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []UniversalSSLVerificationDetails{}, err
	}
	var r universalSSLVerificationResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return []UniversalSSLVerificationDetails{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdateUniversalSSLCertificatePackValidationMethod changes the validation method for a certificate pack
//
// API reference: https://api.cloudflare.com/#ssl-verification-ssl-verification-details
func (api *API) UpdateUniversalSSLCertificatePackValidationMethod(ctx context.Context, zoneID string, certPackUUID string, setting UniversalSSLCertificatePackValidationMethodSetting) (UniversalSSLCertificatePackValidationMethodSetting, error) {
	uri := fmt.Sprintf("/zones/%s/ssl/verification/%s", zoneID, certPackUUID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, setting)
	if err != nil {
		return UniversalSSLCertificatePackValidationMethodSetting{}, err
	}
	var r universalSSLCertificatePackValidationMethodSettingResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return UniversalSSLCertificatePackValidationMethodSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}
