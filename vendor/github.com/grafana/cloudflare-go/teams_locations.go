package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

type TeamsLocationsListResponse struct {
	Response
	ResultInfo `json:"result_info"`
	Result     []TeamsLocation `json:"result"`
}

type TeamsLocationDetailResponse struct {
	Response
	Result TeamsLocation `json:"result"`
}

type TeamsLocationNetwork struct {
	ID      string `json:"id"`
	Network string `json:"network"`
}

type TeamsLocation struct {
	ID                    string                 `json:"id"`
	Name                  string                 `json:"name"`
	Networks              []TeamsLocationNetwork `json:"networks"`
	PolicyIDs             []string               `json:"policy_ids"`
	Ip                    string                 `json:"ip,omitempty"`
	Subdomain             string                 `json:"doh_subdomain"`
	AnonymizedLogsEnabled bool                   `json:"anonymized_logs_enabled"`
	IPv4Destination       string                 `json:"ipv4_destination"`
	ClientDefault         bool                   `json:"client_default"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// TeamsLocations returns all locations within an account.
//
// API reference: https://api.cloudflare.com/#teams-locations-list-teams-locations
func (api *API) TeamsLocations(ctx context.Context, accountID string) ([]TeamsLocation, ResultInfo, error) {
	uri := fmt.Sprintf("/%s/%s/gateway/locations", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []TeamsLocation{}, ResultInfo{}, err
	}

	var teamsLocationsListResponse TeamsLocationsListResponse
	err = json.Unmarshal(res, &teamsLocationsListResponse)
	if err != nil {
		return []TeamsLocation{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsLocationsListResponse.Result, teamsLocationsListResponse.ResultInfo, nil
}

// TeamsLocation returns a single location based on the ID.
//
// API reference: https://api.cloudflare.com/#teams-locations-teams-location-details
func (api *API) TeamsLocation(ctx context.Context, accountID, locationID string) (TeamsLocation, error) {
	uri := fmt.Sprintf(
		"/%s/%s/gateway/locations/%s",
		AccountRouteRoot,
		accountID,
		locationID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return TeamsLocation{}, err
	}

	var teamsLocationDetailResponse TeamsLocationDetailResponse
	err = json.Unmarshal(res, &teamsLocationDetailResponse)
	if err != nil {
		return TeamsLocation{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsLocationDetailResponse.Result, nil
}

// CreateTeamsLocation creates a new teams location.
//
// API reference: https://api.cloudflare.com/#teams-locations-create-teams-location
func (api *API) CreateTeamsLocation(ctx context.Context, accountID string, teamsLocation TeamsLocation) (TeamsLocation, error) {
	uri := fmt.Sprintf("/%s/%s/gateway/locations", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, teamsLocation)
	if err != nil {
		return TeamsLocation{}, err
	}

	var teamsLocationDetailResponse TeamsLocationDetailResponse
	err = json.Unmarshal(res, &teamsLocationDetailResponse)
	if err != nil {
		return TeamsLocation{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsLocationDetailResponse.Result, nil
}

// UpdateTeamsLocation updates an existing teams location.
//
// API reference: https://api.cloudflare.com/#teams-locations-update-teams-location
func (api *API) UpdateTeamsLocation(ctx context.Context, accountID string, teamsLocation TeamsLocation) (TeamsLocation, error) {
	if teamsLocation.ID == "" {
		return TeamsLocation{}, errors.Errorf("teams location ID cannot be empty")
	}

	uri := fmt.Sprintf(
		"/%s/%s/gateway/locations/%s",
		AccountRouteRoot,
		accountID,
		teamsLocation.ID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, teamsLocation)
	if err != nil {
		return TeamsLocation{}, err
	}

	var teamsLocationDetailResponse TeamsLocationDetailResponse
	err = json.Unmarshal(res, &teamsLocationDetailResponse)
	if err != nil {
		return TeamsLocation{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsLocationDetailResponse.Result, nil
}

// DeleteTeamsLocation deletes a teams location.
//
// API reference: https://api.cloudflare.com/#teams-locations-delete-teams-location
func (api *API) DeleteTeamsLocation(ctx context.Context, accountID, teamsLocationID string) error {
	uri := fmt.Sprintf(
		"/%s/%s/gateway/locations/%s",
		AccountRouteRoot,
		accountID,
		teamsLocationID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
