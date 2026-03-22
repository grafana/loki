package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

// AccessCACertificate is the structure of the CA certificate used for
// short lived certificates.
type AccessCACertificate struct {
	ID        string `json:"id"`
	Aud       string `json:"aud"`
	PublicKey string `json:"public_key"`
}

// AccessCACertificateListResponse represents the response of all CA
// certificates within Access.
type AccessCACertificateListResponse struct {
	Response
	Result []AccessCACertificate `json:"result"`
}

// AccessCACertificateResponse represents the response of a single CA
// certificate.
type AccessCACertificateResponse struct {
	Response
	Result AccessCACertificate `json:"result"`
}

// AccessCACertificates returns all CA certificates within Access.
//
// API reference: https://api.cloudflare.com/#access-short-lived-certificates-list-short-lived-certificates
func (api *API) AccessCACertificates(ctx context.Context, accountID string) ([]AccessCACertificate, error) {
	return api.accessCACertificates(ctx, accountID, AccountRouteRoot)
}

// ZoneLevelAccessCACertificates returns all zone level CA certificates within Access.
//
// API reference: https://api.cloudflare.com/#zone-level-access-short-lived-certificates-list-short-lived-certificates
func (api *API) ZoneLevelAccessCACertificates(ctx context.Context, zoneID string) ([]AccessCACertificate, error) {
	return api.accessCACertificates(ctx, zoneID, ZoneRouteRoot)
}

func (api *API) accessCACertificates(ctx context.Context, id string, routeRoot RouteRoot) ([]AccessCACertificate, error) {
	uri := fmt.Sprintf("/%s/%s/access/apps/ca", routeRoot, id)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []AccessCACertificate{}, err
	}

	var accessCAListResponse AccessCACertificateListResponse
	err = json.Unmarshal(res, &accessCAListResponse)
	if err != nil {
		return []AccessCACertificate{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessCAListResponse.Result, nil
}

// AccessCACertificate returns a single CA certificate associated with an Access
// Application.
//
// API reference: https://api.cloudflare.com/#access-short-lived-certificates-short-lived-certificate-details
func (api *API) AccessCACertificate(ctx context.Context, accountID, applicationID string) (AccessCACertificate, error) {
	return api.accessCACertificate(ctx, accountID, applicationID, AccountRouteRoot)
}

// ZoneLevelAccessCACertificate returns a single zone level CA certificate associated with an Access
// Application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-short-lived-certificates-short-lived-certificate-details
func (api *API) ZoneLevelAccessCACertificate(ctx context.Context, zoneID, applicationID string) (AccessCACertificate, error) {
	return api.accessCACertificate(ctx, zoneID, applicationID, ZoneRouteRoot)
}

func (api *API) accessCACertificate(ctx context.Context, id, applicationID string, routeRoot RouteRoot) (AccessCACertificate, error) {
	uri := fmt.Sprintf("/%s/%s/access/apps/%s/ca", routeRoot, id, applicationID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return AccessCACertificate{}, err
	}

	var accessCAResponse AccessCACertificateResponse
	err = json.Unmarshal(res, &accessCAResponse)
	if err != nil {
		return AccessCACertificate{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessCAResponse.Result, nil
}

// CreateAccessCACertificate creates a new CA certificate for an Access
// Application.
//
// API reference: https://api.cloudflare.com/#access-short-lived-certificates-create-short-lived-certificate
func (api *API) CreateAccessCACertificate(ctx context.Context, accountID, applicationID string) (AccessCACertificate, error) {
	return api.createAccessCACertificate(ctx, accountID, applicationID, AccountRouteRoot)
}

// CreateZoneLevelAccessCACertificate creates a new zone level CA certificate for an Access
// Application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-short-lived-certificates-create-short-lived-certificate
func (api *API) CreateZoneLevelAccessCACertificate(ctx context.Context, zoneID string, applicationID string) (AccessCACertificate, error) {
	return api.createAccessCACertificate(ctx, zoneID, applicationID, ZoneRouteRoot)
}

func (api *API) createAccessCACertificate(ctx context.Context, id string, applicationID string, routeRoot RouteRoot) (AccessCACertificate, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/apps/%s/ca",
		routeRoot,
		id,
		applicationID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, nil)
	if err != nil {
		return AccessCACertificate{}, err
	}

	var accessCACertificate AccessCACertificateResponse
	err = json.Unmarshal(res, &accessCACertificate)
	if err != nil {
		return AccessCACertificate{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessCACertificate.Result, nil
}

// DeleteAccessCACertificate deletes an Access CA certificate on a defined
// Access Application.
//
// API reference: https://api.cloudflare.com/#access-short-lived-certificates-delete-access-certificate
func (api *API) DeleteAccessCACertificate(ctx context.Context, accountID, applicationID string) error {
	return api.deleteAccessCACertificate(ctx, accountID, applicationID, AccountRouteRoot)
}

// DeleteZoneLevelAccessCACertificate deletes a zone level Access CA certificate on a defined
// Access Application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-short-lived-certificates-delete-access-certificate
func (api *API) DeleteZoneLevelAccessCACertificate(ctx context.Context, zoneID, applicationID string) error {
	return api.deleteAccessCACertificate(ctx, zoneID, applicationID, ZoneRouteRoot)
}

func (api *API) deleteAccessCACertificate(ctx context.Context, id string, applicationID string, routeRoot RouteRoot) error {
	uri := fmt.Sprintf(
		"/%s/%s/access/apps/%s/ca",
		routeRoot,
		id,
		applicationID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
