package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

// AccessApplicationType represents the application type.
type AccessApplicationType string

// These constants represent all valid application types.
const (
	SelfHosted AccessApplicationType = "self_hosted"
	SSH        AccessApplicationType = "ssh"
	VNC        AccessApplicationType = "vnc"
	File       AccessApplicationType = "file"
)

// AccessApplication represents an Access application.
type AccessApplication struct {
	ID                      string                        `json:"id,omitempty"`
	CreatedAt               *time.Time                    `json:"created_at,omitempty"`
	UpdatedAt               *time.Time                    `json:"updated_at,omitempty"`
	AUD                     string                        `json:"aud,omitempty"`
	Name                    string                        `json:"name"`
	Domain                  string                        `json:"domain"`
	Type                    AccessApplicationType         `json:"type,omitempty"`
	SessionDuration         string                        `json:"session_duration,omitempty"`
	AutoRedirectToIdentity  bool                          `json:"auto_redirect_to_identity,omitempty"`
	EnableBindingCookie     bool                          `json:"enable_binding_cookie,omitempty"`
	AllowedIdps             []string                      `json:"allowed_idps,omitempty"`
	CorsHeaders             *AccessApplicationCorsHeaders `json:"cors_headers,omitempty"`
	CustomDenyMessage       string                        `json:"custom_deny_message,omitempty"`
	CustomDenyURL           string                        `json:"custom_deny_url,omitempty"`
	HttpOnlyCookieAttribute bool                          `json:"http_only_cookie_attribute,omitempty"`
	SameSiteCookieAttribute string                        `json:"same_site_cookie_attribute,omitempty"`
	LogoURL                 string                        `json:"logo_url,omitempty"`
	SkipInterstitial        bool                          `json:"skip_interstitial,omitempty"`
}

// AccessApplicationCorsHeaders represents the CORS HTTP headers for an Access
// Application.
type AccessApplicationCorsHeaders struct {
	AllowedMethods   []string `json:"allowed_methods,omitempty"`
	AllowedOrigins   []string `json:"allowed_origins,omitempty"`
	AllowedHeaders   []string `json:"allowed_headers,omitempty"`
	AllowAllMethods  bool     `json:"allow_all_methods,omitempty"`
	AllowAllHeaders  bool     `json:"allow_all_headers,omitempty"`
	AllowAllOrigins  bool     `json:"allow_all_origins,omitempty"`
	AllowCredentials bool     `json:"allow_credentials,omitempty"`
	MaxAge           int      `json:"max_age,omitempty"`
}

// AccessApplicationListResponse represents the response from the list
// access applications endpoint.
type AccessApplicationListResponse struct {
	Result []AccessApplication `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// AccessApplicationDetailResponse is the API response, containing a single
// access application.
type AccessApplicationDetailResponse struct {
	Success  bool              `json:"success"`
	Errors   []string          `json:"errors"`
	Messages []string          `json:"messages"`
	Result   AccessApplication `json:"result"`
}

// AccessApplications returns all applications within an account.
//
// API reference: https://api.cloudflare.com/#access-applications-list-access-applications
func (api *API) AccessApplications(ctx context.Context, accountID string, pageOpts PaginationOptions) ([]AccessApplication, ResultInfo, error) {
	return api.accessApplications(ctx, accountID, pageOpts, AccountRouteRoot)
}

// ZoneLevelAccessApplications returns all applications within a zone.
//
// API reference: https://api.cloudflare.com/#zone-level-access-applications-list-access-applications
func (api *API) ZoneLevelAccessApplications(ctx context.Context, zoneID string, pageOpts PaginationOptions) ([]AccessApplication, ResultInfo, error) {
	return api.accessApplications(ctx, zoneID, pageOpts, ZoneRouteRoot)
}

func (api *API) accessApplications(ctx context.Context, id string, pageOpts PaginationOptions, routeRoot RouteRoot) ([]AccessApplication, ResultInfo, error) {
	v := url.Values{}
	if pageOpts.PerPage > 0 {
		v.Set("per_page", strconv.Itoa(pageOpts.PerPage))
	}
	if pageOpts.Page > 0 {
		v.Set("page", strconv.Itoa(pageOpts.Page))
	}

	uri := fmt.Sprintf("/%s/%s/access/apps", routeRoot, id)
	if len(v) > 0 {
		uri = fmt.Sprintf("%s?%s", uri, v.Encode())
	}

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []AccessApplication{}, ResultInfo{}, err
	}

	var accessApplicationListResponse AccessApplicationListResponse
	err = json.Unmarshal(res, &accessApplicationListResponse)
	if err != nil {
		return []AccessApplication{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessApplicationListResponse.Result, accessApplicationListResponse.ResultInfo, nil
}

// AccessApplication returns a single application based on the
// application ID.
//
// API reference: https://api.cloudflare.com/#access-applications-access-applications-details
func (api *API) AccessApplication(ctx context.Context, accountID, applicationID string) (AccessApplication, error) {
	return api.accessApplication(ctx, accountID, applicationID, AccountRouteRoot)
}

// ZoneLevelAccessApplication returns a single zone level application based on the
// application ID.
//
// API reference: https://api.cloudflare.com/#zone-level-access-applications-access-applications-details
func (api *API) ZoneLevelAccessApplication(ctx context.Context, zoneID, applicationID string) (AccessApplication, error) {
	return api.accessApplication(ctx, zoneID, applicationID, ZoneRouteRoot)
}

func (api *API) accessApplication(ctx context.Context, id, applicationID string, routeRoot RouteRoot) (AccessApplication, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/apps/%s",
		routeRoot,
		id,
		applicationID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return AccessApplication{}, err
	}

	var accessApplicationDetailResponse AccessApplicationDetailResponse
	err = json.Unmarshal(res, &accessApplicationDetailResponse)
	if err != nil {
		return AccessApplication{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessApplicationDetailResponse.Result, nil
}

// CreateAccessApplication creates a new access application.
//
// API reference: https://api.cloudflare.com/#access-applications-create-access-application
func (api *API) CreateAccessApplication(ctx context.Context, accountID string, accessApplication AccessApplication) (AccessApplication, error) {
	return api.createAccessApplication(ctx, accountID, accessApplication, AccountRouteRoot)
}

// CreateZoneLevelAccessApplication creates a new zone level access application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-applications-create-access-application
func (api *API) CreateZoneLevelAccessApplication(ctx context.Context, zoneID string, accessApplication AccessApplication) (AccessApplication, error) {
	return api.createAccessApplication(ctx, zoneID, accessApplication, ZoneRouteRoot)
}

func (api *API) createAccessApplication(ctx context.Context, id string, accessApplication AccessApplication, routeRoot RouteRoot) (AccessApplication, error) {
	uri := fmt.Sprintf("/%s/%s/access/apps", routeRoot, id)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, accessApplication)
	if err != nil {
		return AccessApplication{}, err
	}

	var accessApplicationDetailResponse AccessApplicationDetailResponse
	err = json.Unmarshal(res, &accessApplicationDetailResponse)
	if err != nil {
		return AccessApplication{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessApplicationDetailResponse.Result, nil
}

// UpdateAccessApplication updates an existing access application.
//
// API reference: https://api.cloudflare.com/#access-applications-update-access-application
func (api *API) UpdateAccessApplication(ctx context.Context, accountID string, accessApplication AccessApplication) (AccessApplication, error) {
	return api.updateAccessApplication(ctx, accountID, accessApplication, AccountRouteRoot)
}

// UpdateZoneLevelAccessApplication updates an existing zone level access application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-applications-update-access-application
func (api *API) UpdateZoneLevelAccessApplication(ctx context.Context, zoneID string, accessApplication AccessApplication) (AccessApplication, error) {
	return api.updateAccessApplication(ctx, zoneID, accessApplication, ZoneRouteRoot)
}

func (api *API) updateAccessApplication(ctx context.Context, id string, accessApplication AccessApplication, routeRoot RouteRoot) (AccessApplication, error) {
	if accessApplication.ID == "" {
		return AccessApplication{}, errors.Errorf("access application ID cannot be empty")
	}

	uri := fmt.Sprintf(
		"/%s/%s/access/apps/%s",
		routeRoot,
		id,
		accessApplication.ID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, accessApplication)
	if err != nil {
		return AccessApplication{}, err
	}

	var accessApplicationDetailResponse AccessApplicationDetailResponse
	err = json.Unmarshal(res, &accessApplicationDetailResponse)
	if err != nil {
		return AccessApplication{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessApplicationDetailResponse.Result, nil
}

// DeleteAccessApplication deletes an access application.
//
// API reference: https://api.cloudflare.com/#access-applications-delete-access-application
func (api *API) DeleteAccessApplication(ctx context.Context, accountID, applicationID string) error {
	return api.deleteAccessApplication(ctx, accountID, applicationID, AccountRouteRoot)
}

// DeleteZoneLevelAccessApplication deletes a zone level access application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-applications-delete-access-application
func (api *API) DeleteZoneLevelAccessApplication(ctx context.Context, zoneID, applicationID string) error {
	return api.deleteAccessApplication(ctx, zoneID, applicationID, ZoneRouteRoot)
}

func (api *API) deleteAccessApplication(ctx context.Context, id, applicationID string, routeRoot RouteRoot) error {
	uri := fmt.Sprintf(
		"/%s/%s/access/apps/%s",
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

// RevokeAccessApplicationTokens revokes tokens associated with an
// access application.
//
// API reference: https://api.cloudflare.com/#access-applications-revoke-access-tokens
func (api *API) RevokeAccessApplicationTokens(ctx context.Context, accountID, applicationID string) error {
	return api.revokeAccessApplicationTokens(ctx, accountID, applicationID, AccountRouteRoot)
}

// RevokeZoneLevelAccessApplicationTokens revokes tokens associated with a zone level
// access application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-applications-revoke-access-tokens
func (api *API) RevokeZoneLevelAccessApplicationTokens(ctx context.Context, zoneID, applicationID string) error {
	return api.revokeAccessApplicationTokens(ctx, zoneID, applicationID, ZoneRouteRoot)
}

func (api *API) revokeAccessApplicationTokens(ctx context.Context, id string, applicationID string, routeRoot RouteRoot) error {
	uri := fmt.Sprintf(
		"/%s/%s/access/apps/%s/revoke-tokens",
		routeRoot,
		id,
		applicationID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodPost, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
