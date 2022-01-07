package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// AccessMutualTLSCertificate is the structure of a single Access Mutual TLS
// certificate.
type AccessMutualTLSCertificate struct {
	ID                  string    `json:"id,omitempty"`
	CreatedAt           time.Time `json:"created_at,omitempty"`
	UpdatedAt           time.Time `json:"updated_at,omitempty"`
	ExpiresOn           time.Time `json:"expires_on,omitempty"`
	Name                string    `json:"name,omitempty"`
	Fingerprint         string    `json:"fingerprint,omitempty"`
	Certificate         string    `json:"certificate,omitempty"`
	AssociatedHostnames []string  `json:"associated_hostnames,omitempty"`
}

// AccessMutualTLSCertificateListResponse is the API response for all Access
// Mutual TLS certificates.
type AccessMutualTLSCertificateListResponse struct {
	Response
	Result []AccessMutualTLSCertificate `json:"result"`
}

// AccessMutualTLSCertificateDetailResponse is the API response for a single
// Access Mutual TLS certificate.
type AccessMutualTLSCertificateDetailResponse struct {
	Response
	Result AccessMutualTLSCertificate `json:"result"`
}

// AccessMutualTLSCertificates returns all Access TLS certificates for the account
// level.
//
// API reference: https://api.cloudflare.com/#access-mutual-tls-authentication-properties
func (api *API) AccessMutualTLSCertificates(ctx context.Context, accountID string) ([]AccessMutualTLSCertificate, error) {
	return api.accessMutualTLSCertificates(ctx, accountID, AccountRouteRoot)
}

// ZoneAccessMutualTLSCertificates returns all Access TLS certificates for the
// zone level.
//
// API reference: https://api.cloudflare.com/#zone-level-access-mutual-tls-authentication-properties
func (api *API) ZoneAccessMutualTLSCertificates(ctx context.Context, zoneID string) ([]AccessMutualTLSCertificate, error) {
	return api.accessMutualTLSCertificates(ctx, zoneID, ZoneRouteRoot)
}

func (api *API) accessMutualTLSCertificates(ctx context.Context, id string, routeRoot RouteRoot) ([]AccessMutualTLSCertificate, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/certificates",
		routeRoot,
		id,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []AccessMutualTLSCertificate{}, err
	}

	var accessMutualTLSCertificateListResponse AccessMutualTLSCertificateListResponse
	err = json.Unmarshal(res, &accessMutualTLSCertificateListResponse)
	if err != nil {
		return []AccessMutualTLSCertificate{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessMutualTLSCertificateListResponse.Result, nil
}

// AccessMutualTLSCertificate returns a single account level Access Mutual TLS
// certificate.
//
// API reference: https://api.cloudflare.com/#access-mutual-tls-authentication-access-certificate-details
func (api *API) AccessMutualTLSCertificate(ctx context.Context, accountID, certificateID string) (AccessMutualTLSCertificate, error) {
	return api.accessMutualTLSCertificate(ctx, accountID, certificateID, AccountRouteRoot)
}

// ZoneAccessMutualTLSCertificate returns a single zone level Access Mutual TLS
// certificate.
//
// API reference: https://api.cloudflare.com/#zone-level-access-mutual-tls-authentication-access-certificate-details
func (api *API) ZoneAccessMutualTLSCertificate(ctx context.Context, zoneID, certificateID string) (AccessMutualTLSCertificate, error) {
	return api.accessMutualTLSCertificate(ctx, zoneID, certificateID, ZoneRouteRoot)
}

func (api *API) accessMutualTLSCertificate(ctx context.Context, id, certificateID string, routeRoot RouteRoot) (AccessMutualTLSCertificate, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/certificates/%s",
		routeRoot,
		id,
		certificateID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return AccessMutualTLSCertificate{}, err
	}

	var accessMutualTLSCertificateDetailResponse AccessMutualTLSCertificateDetailResponse
	err = json.Unmarshal(res, &accessMutualTLSCertificateDetailResponse)
	if err != nil {
		return AccessMutualTLSCertificate{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessMutualTLSCertificateDetailResponse.Result, nil
}

// CreateAccessMutualTLSCertificate creates an account level Access TLS Mutual
// certificate.
//
// API reference: https://api.cloudflare.com/#access-mutual-tls-authentication-create-access-certificate
func (api *API) CreateAccessMutualTLSCertificate(ctx context.Context, accountID string, certificate AccessMutualTLSCertificate) (AccessMutualTLSCertificate, error) {
	return api.createAccessMutualTLSCertificate(ctx, accountID, certificate, AccountRouteRoot)
}

// CreateZoneAccessMutualTLSCertificate creates a zone level Access TLS Mutual
// certificate.
//
// API reference: https://api.cloudflare.com/#zone-level-access-mutual-tls-authentication-create-access-certificate
func (api *API) CreateZoneAccessMutualTLSCertificate(ctx context.Context, zoneID string, certificate AccessMutualTLSCertificate) (AccessMutualTLSCertificate, error) {
	return api.createAccessMutualTLSCertificate(ctx, zoneID, certificate, ZoneRouteRoot)
}

func (api *API) createAccessMutualTLSCertificate(ctx context.Context, id string, certificate AccessMutualTLSCertificate, routeRoot RouteRoot) (AccessMutualTLSCertificate, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/certificates",
		routeRoot,
		id,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, certificate)
	if err != nil {
		return AccessMutualTLSCertificate{}, err
	}

	var accessMutualTLSCertificateDetailResponse AccessMutualTLSCertificateDetailResponse
	err = json.Unmarshal(res, &accessMutualTLSCertificateDetailResponse)
	if err != nil {
		return AccessMutualTLSCertificate{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessMutualTLSCertificateDetailResponse.Result, nil
}

// UpdateAccessMutualTLSCertificate updates an account level Access TLS Mutual
// certificate.
//
// API reference: https://api.cloudflare.com/#access-mutual-tls-authentication-update-access-certificate
func (api *API) UpdateAccessMutualTLSCertificate(ctx context.Context, accountID, certificateID string, certificate AccessMutualTLSCertificate) (AccessMutualTLSCertificate, error) {
	return api.updateAccessMutualTLSCertificate(ctx, accountID, certificateID, certificate, AccountRouteRoot)
}

// UpdateZoneAccessMutualTLSCertificate updates a zone level Access TLS Mutual
// certificate.
//
// API reference: https://api.cloudflare.com/#zone-level-access-mutual-tls-authentication-update-access-certificate
func (api *API) UpdateZoneAccessMutualTLSCertificate(ctx context.Context, zoneID, certificateID string, certificate AccessMutualTLSCertificate) (AccessMutualTLSCertificate, error) {
	return api.updateAccessMutualTLSCertificate(ctx, zoneID, certificateID, certificate, ZoneRouteRoot)
}

func (api *API) updateAccessMutualTLSCertificate(ctx context.Context, id string, certificateID string, certificate AccessMutualTLSCertificate, routeRoot RouteRoot) (AccessMutualTLSCertificate, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/certificates/%s",
		routeRoot,
		id,
		certificateID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, certificate)
	if err != nil {
		return AccessMutualTLSCertificate{}, err
	}

	var accessMutualTLSCertificateDetailResponse AccessMutualTLSCertificateDetailResponse
	err = json.Unmarshal(res, &accessMutualTLSCertificateDetailResponse)
	if err != nil {
		return AccessMutualTLSCertificate{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessMutualTLSCertificateDetailResponse.Result, nil
}

// DeleteAccessMutualTLSCertificate destroys an account level Access Mutual
// TLS certificate.
//
// API reference: https://api.cloudflare.com/#access-mutual-tls-authentication-update-access-certificate
func (api *API) DeleteAccessMutualTLSCertificate(ctx context.Context, accountID, certificateID string) error {
	return api.deleteAccessMutualTLSCertificate(ctx, accountID, certificateID, AccountRouteRoot)
}

// DeleteZoneAccessMutualTLSCertificate destroys a zone level Access Mutual TLS
// certificate.
//
// API reference: https://api.cloudflare.com/#zone-level-access-mutual-tls-authentication-update-access-certificate
func (api *API) DeleteZoneAccessMutualTLSCertificate(ctx context.Context, zoneID, certificateID string) error {
	return api.deleteAccessMutualTLSCertificate(ctx, zoneID, certificateID, ZoneRouteRoot)
}

func (api *API) deleteAccessMutualTLSCertificate(ctx context.Context, id, certificateID string, routeRoot RouteRoot) error {
	uri := fmt.Sprintf(
		"/%s/%s/access/certificates/%s",
		routeRoot,
		id,
		certificateID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	var accessMutualTLSCertificateDetailResponse AccessMutualTLSCertificateDetailResponse
	err = json.Unmarshal(res, &accessMutualTLSCertificateDetailResponse)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}

	return nil
}
