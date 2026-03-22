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

// AccessGroup defines a group for allowing or disallowing access to
// one or more Access applications.
type AccessGroup struct {
	ID        string     `json:"id,omitempty"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	Name      string     `json:"name"`

	// The include group works like an OR logical operator. The user must
	// satisfy one of the rules.
	Include []interface{} `json:"include"`

	// The exclude group works like a NOT logical operator. The user must
	// not satisfy all of the rules in exclude.
	Exclude []interface{} `json:"exclude"`

	// The require group works like a AND logical operator. The user must
	// satisfy all of the rules in require.
	Require []interface{} `json:"require"`
}

// AccessGroupEmail is used for managing access based on the email.
// For example, restrict access to users with the email addresses
// `test@example.com` or `someone@example.com`.
type AccessGroupEmail struct {
	Email struct {
		Email string `json:"email"`
	} `json:"email"`
}

// AccessGroupEmailDomain is used for managing access based on an email
// domain domain such as `example.com` instead of individual addresses.
type AccessGroupEmailDomain struct {
	EmailDomain struct {
		Domain string `json:"domain"`
	} `json:"email_domain"`
}

// AccessGroupIP is used for managing access based in the IP. It
// accepts individual IPs or CIDRs.
type AccessGroupIP struct {
	IP struct {
		IP string `json:"ip"`
	} `json:"ip"`
}

// AccessGroupGeo is used for managing access based on the country code.
type AccessGroupGeo struct {
	Geo struct {
		CountryCode string `json:"country_code"`
	} `json:"geo"`
}

// AccessGroupEveryone is used for managing access to everyone.
type AccessGroupEveryone struct {
	Everyone struct{} `json:"everyone"`
}

// AccessGroupServiceToken is used for managing access based on a specific
// service token.
type AccessGroupServiceToken struct {
	ServiceToken struct {
		ID string `json:"token_id"`
	} `json:"service_token"`
}

// AccessGroupAnyValidServiceToken is used for managing access for all valid
// service tokens (not restricted).
type AccessGroupAnyValidServiceToken struct {
	AnyValidServiceToken struct{} `json:"any_valid_service_token"`
}

// AccessGroupAccessGroup is used for managing access based on an
// access group.
type AccessGroupAccessGroup struct {
	Group struct {
		ID string `json:"id"`
	} `json:"group"`
}

// AccessGroupCertificate is used for managing access to based on a valid
// mTLS certificate being presented.
type AccessGroupCertificate struct {
	Certificate struct{} `json:"certificate"`
}

// AccessGroupCertificateCommonName is used for managing access based on a
// common name within a certificate.
type AccessGroupCertificateCommonName struct {
	CommonName struct {
		CommonName string `json:"common_name"`
	} `json:"common_name"`
}

// AccessGroupGSuite is used to configure access based on GSuite group.
type AccessGroupGSuite struct {
	Gsuite struct {
		Email              string `json:"email"`
		IdentityProviderID string `json:"identity_provider_id"`
	} `json:"gsuite"`
}

// AccessGroupGitHub is used to configure access based on a GitHub organisation.
type AccessGroupGitHub struct {
	GitHubOrganization struct {
		Name               string `json:"name"`
		Team               string `json:"team,omitempty"`
		IdentityProviderID string `json:"identity_provider_id"`
	} `json:"github-organization"`
}

// AccessGroupAzure is used to configure access based on a Azure group.
type AccessGroupAzure struct {
	AzureAD struct {
		ID                 string `json:"id"`
		IdentityProviderID string `json:"identity_provider_id"`
	} `json:"azureAD"`
}

// AccessGroupOkta is used to configure access based on a Okta group.
type AccessGroupOkta struct {
	Okta struct {
		Name               string `json:"name"`
		IdentityProviderID string `json:"identity_provider_id"`
	} `json:"okta"`
}

// AccessGroupSAML is used to allow SAML users with a specific attribute
// configuration.
type AccessGroupSAML struct {
	Saml struct {
		AttributeName      string `json:"attribute_name"`
		AttributeValue     string `json:"attribute_value"`
		IdentityProviderID string `json:"identity_provider_id"`
	} `json:"saml"`
}

// AccessGroupAuthMethod is used for managing access by the "amr"
// (Authentication Methods References) identifier. For example, an
// application may want to require that users authenticate using a hardware
// key by setting the "auth_method" to "swk". A list of values are listed
// here: https://tools.ietf.org/html/rfc8176#section-2. Custom values are
// supported as well.
type AccessGroupAuthMethod struct {
	AuthMethod struct {
		AuthMethod string `json:"auth_method"`
	} `json:"auth_method"`
}

// AccessGroupLoginMethod restricts the application to specific IdP instances.
type AccessGroupLoginMethod struct {
	LoginMethod struct {
		ID string `json:"id"`
	} `json:"login_method"`
}

// AccessGroupDevicePosture restricts the application to specific devices
type AccessGroupDevicePosture struct {
	DevicePosture struct {
		ID string `json:"integration_uid"`
	} `json:"device_posture"`
}

// AccessGroupListResponse represents the response from the list
// access group endpoint.
type AccessGroupListResponse struct {
	Result []AccessGroup `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// AccessGroupDetailResponse is the API response, containing a single
// access group.
type AccessGroupDetailResponse struct {
	Success  bool        `json:"success"`
	Errors   []string    `json:"errors"`
	Messages []string    `json:"messages"`
	Result   AccessGroup `json:"result"`
}

// AccessGroups returns all access groups for an access application.
//
// API reference: https://api.cloudflare.com/#access-groups-list-access-groups
func (api *API) AccessGroups(ctx context.Context, accountID string, pageOpts PaginationOptions) ([]AccessGroup, ResultInfo, error) {
	return api.accessGroups(ctx, accountID, pageOpts, AccountRouteRoot)
}

// ZoneLevelAccessGroups returns all zone level access groups for an access application.
//
// API reference: https://api.cloudflare.com/#zone-level-access-groups-list-access-groups
func (api *API) ZoneLevelAccessGroups(ctx context.Context, zoneID string, pageOpts PaginationOptions) ([]AccessGroup, ResultInfo, error) {
	return api.accessGroups(ctx, zoneID, pageOpts, ZoneRouteRoot)
}

func (api *API) accessGroups(ctx context.Context, id string, pageOpts PaginationOptions, routeRoot RouteRoot) ([]AccessGroup, ResultInfo, error) {
	v := url.Values{}
	if pageOpts.PerPage > 0 {
		v.Set("per_page", strconv.Itoa(pageOpts.PerPage))
	}
	if pageOpts.Page > 0 {
		v.Set("page", strconv.Itoa(pageOpts.Page))
	}

	uri := fmt.Sprintf(
		"/%s/%s/access/groups",
		routeRoot,
		id,
	)

	if len(v) > 0 {
		uri = fmt.Sprintf("%s?%s", uri, v.Encode())
	}

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []AccessGroup{}, ResultInfo{}, err
	}

	var accessGroupListResponse AccessGroupListResponse
	err = json.Unmarshal(res, &accessGroupListResponse)
	if err != nil {
		return []AccessGroup{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessGroupListResponse.Result, accessGroupListResponse.ResultInfo, nil
}

// AccessGroup returns a single group based on the group ID.
//
// API reference: https://api.cloudflare.com/#access-groups-access-group-details
func (api *API) AccessGroup(ctx context.Context, accountID, groupID string) (AccessGroup, error) {
	return api.accessGroup(ctx, accountID, groupID, AccountRouteRoot)
}

// ZoneLevelAccessGroup returns a single zone level group based on the group ID.
//
// API reference: https://api.cloudflare.com/#zone-level-access-groups-access-group-details
func (api *API) ZoneLevelAccessGroup(ctx context.Context, zoneID, groupID string) (AccessGroup, error) {
	return api.accessGroup(ctx, zoneID, groupID, ZoneRouteRoot)
}

func (api *API) accessGroup(ctx context.Context, id, groupID string, routeRoot RouteRoot) (AccessGroup, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/groups/%s",
		routeRoot,
		id,
		groupID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return AccessGroup{}, err
	}

	var accessGroupDetailResponse AccessGroupDetailResponse
	err = json.Unmarshal(res, &accessGroupDetailResponse)
	if err != nil {
		return AccessGroup{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessGroupDetailResponse.Result, nil
}

// CreateAccessGroup creates a new access group.
//
// API reference: https://api.cloudflare.com/#access-groups-create-access-group
func (api *API) CreateAccessGroup(ctx context.Context, accountID string, accessGroup AccessGroup) (AccessGroup, error) {
	return api.createAccessGroup(ctx, accountID, accessGroup, AccountRouteRoot)
}

// CreateZoneLevelAccessGroup creates a new zone level access group.
//
// API reference: https://api.cloudflare.com/#zone-level-access-groups-create-access-group
func (api *API) CreateZoneLevelAccessGroup(ctx context.Context, zoneID string, accessGroup AccessGroup) (AccessGroup, error) {
	return api.createAccessGroup(ctx, zoneID, accessGroup, ZoneRouteRoot)
}

func (api *API) createAccessGroup(ctx context.Context, id string, accessGroup AccessGroup, routeRoot RouteRoot) (AccessGroup, error) {
	uri := fmt.Sprintf(
		"/%s/%s/access/groups",
		routeRoot,
		id,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, accessGroup)
	if err != nil {
		return AccessGroup{}, err
	}

	var accessGroupDetailResponse AccessGroupDetailResponse
	err = json.Unmarshal(res, &accessGroupDetailResponse)
	if err != nil {
		return AccessGroup{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessGroupDetailResponse.Result, nil
}

// UpdateAccessGroup updates an existing access group.
//
// API reference: https://api.cloudflare.com/#access-groups-update-access-group
func (api *API) UpdateAccessGroup(ctx context.Context, accountID string, accessGroup AccessGroup) (AccessGroup, error) {
	return api.updateAccessGroup(ctx, accountID, accessGroup, AccountRouteRoot)
}

// UpdateZoneLevelAccessGroup updates an existing zone level access group.
//
// API reference: https://api.cloudflare.com/#zone-level-access-groups-update-access-group
func (api *API) UpdateZoneLevelAccessGroup(ctx context.Context, zoneID string, accessGroup AccessGroup) (AccessGroup, error) {
	return api.updateAccessGroup(ctx, zoneID, accessGroup, ZoneRouteRoot)
}

func (api *API) updateAccessGroup(ctx context.Context, id string, accessGroup AccessGroup, routeRoot RouteRoot) (AccessGroup, error) {
	if accessGroup.ID == "" {
		return AccessGroup{}, errors.Errorf("access group ID cannot be empty")
	}
	uri := fmt.Sprintf(
		"/%s/%s/access/groups/%s",
		routeRoot,
		id,
		accessGroup.ID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, accessGroup)
	if err != nil {
		return AccessGroup{}, err
	}

	var accessGroupDetailResponse AccessGroupDetailResponse
	err = json.Unmarshal(res, &accessGroupDetailResponse)
	if err != nil {
		return AccessGroup{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessGroupDetailResponse.Result, nil
}

// DeleteAccessGroup deletes an access group.
//
// API reference: https://api.cloudflare.com/#access-groups-delete-access-group
func (api *API) DeleteAccessGroup(ctx context.Context, accountID, groupID string) error {
	return api.deleteAccessGroup(ctx, accountID, groupID, AccountRouteRoot)
}

// DeleteZoneLevelAccessGroup deletes a zone level access group.
//
// API reference: https://api.cloudflare.com/#zone-level-access-groups-delete-access-group
func (api *API) DeleteZoneLevelAccessGroup(ctx context.Context, zoneID, groupID string) error {
	return api.deleteAccessGroup(ctx, zoneID, groupID, ZoneRouteRoot)
}

func (api *API) deleteAccessGroup(ctx context.Context, id string, groupID string, routeRoot RouteRoot) error {
	uri := fmt.Sprintf(
		"/%s/%s/access/groups/%s",
		routeRoot,
		id,
		groupID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
