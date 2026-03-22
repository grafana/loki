package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// TeamsList represents a Teams List.
type TeamsList struct {
	ID          string          `json:"id,omitempty"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Items       []TeamsListItem `json:"items,omitempty"`
	Count       uint64          `json:"count,omitempty"`
	CreatedAt   *time.Time      `json:"created_at,omitempty"`
	UpdatedAt   *time.Time      `json:"updated_at,omitempty"`
}

// TeamsListItem represents a single list item.
type TeamsListItem struct {
	Value     string     `json:"value"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

// PatchTeamsList represents a patch request for appending/removing list items.
type PatchTeamsList struct {
	ID     string          `json:"id"`
	Append []TeamsListItem `json:"append"`
	Remove []string        `json:"remove"`
}

// TeamsListListResponse represents the response from the list
// teams lists endpoint.
type TeamsListListResponse struct {
	Result []TeamsList `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// TeamsListItemsListResponse represents the response from the list
// teams list items endpoint.
type TeamsListItemsListResponse struct {
	Result []TeamsListItem `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// TeamsListDetailResponse is the API response, containing a single
// teams list.
type TeamsListDetailResponse struct {
	Response
	Result TeamsList `json:"result"`
}

// TeamsLists returns all lists within an account.
//
// API reference: https://api.cloudflare.com/#teams-lists-list-teams-lists
func (api *API) TeamsLists(ctx context.Context, accountID string) ([]TeamsList, ResultInfo, error) {
	uri := fmt.Sprintf("/%s/%s/gateway/lists", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []TeamsList{}, ResultInfo{}, err
	}

	var teamsListListResponse TeamsListListResponse
	err = json.Unmarshal(res, &teamsListListResponse)
	if err != nil {
		return []TeamsList{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsListListResponse.Result, teamsListListResponse.ResultInfo, nil
}

// TeamsList returns a single list based on the list ID.
//
// API reference: https://api.cloudflare.com/#teams-lists-teams-list-details
func (api *API) TeamsList(ctx context.Context, accountID, listID string) (TeamsList, error) {
	uri := fmt.Sprintf(
		"/%s/%s/gateway/lists/%s",
		AccountRouteRoot,
		accountID,
		listID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return TeamsList{}, err
	}

	var teamsListDetailResponse TeamsListDetailResponse
	err = json.Unmarshal(res, &teamsListDetailResponse)
	if err != nil {
		return TeamsList{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsListDetailResponse.Result, nil
}

// TeamsListItems returns all list items for a list.
//
// API reference: https://api.cloudflare.com/#teams-lists-teams-list-items
func (api *API) TeamsListItems(ctx context.Context, accountID, listID string) ([]TeamsListItem, ResultInfo, error) {
	uri := fmt.Sprintf("/%s/%s/gateway/lists/%s/items", AccountRouteRoot, accountID, listID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []TeamsListItem{}, ResultInfo{}, err
	}

	var teamsListItemsListResponse TeamsListItemsListResponse
	err = json.Unmarshal(res, &teamsListItemsListResponse)
	if err != nil {
		return []TeamsListItem{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsListItemsListResponse.Result, teamsListItemsListResponse.ResultInfo, nil
}

// CreateTeamsList creates a new teams list.
//
// API reference: https://api.cloudflare.com/#teams-lists-create-teams-list
func (api *API) CreateTeamsList(ctx context.Context, accountID string, teamsList TeamsList) (TeamsList, error) {
	uri := fmt.Sprintf("/%s/%s/gateway/lists", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, teamsList)
	if err != nil {
		return TeamsList{}, err
	}

	var teamsListDetailResponse TeamsListDetailResponse
	err = json.Unmarshal(res, &teamsListDetailResponse)
	if err != nil {
		return TeamsList{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsListDetailResponse.Result, nil
}

// UpdateTeamsList updates an existing teams list.
//
// API reference: https://api.cloudflare.com/#teams-lists-update-teams-list
func (api *API) UpdateTeamsList(ctx context.Context, accountID string, teamsList TeamsList) (TeamsList, error) {
	if teamsList.ID == "" {
		return TeamsList{}, errors.Errorf("teams list ID cannot be empty")
	}

	uri := fmt.Sprintf(
		"/%s/%s/gateway/lists/%s",
		AccountRouteRoot,
		accountID,
		teamsList.ID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, teamsList)
	if err != nil {
		return TeamsList{}, err
	}

	var teamsListDetailResponse TeamsListDetailResponse
	err = json.Unmarshal(res, &teamsListDetailResponse)
	if err != nil {
		return TeamsList{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsListDetailResponse.Result, nil
}

// PatchTeamsList updates the items in an existing teams list.
//
// API reference: https://api.cloudflare.com/#teams-lists-patch-teams-list
func (api *API) PatchTeamsList(ctx context.Context, accountID string, listPatch PatchTeamsList) (TeamsList, error) {
	if listPatch.ID == "" {
		return TeamsList{}, errors.Errorf("teams list ID cannot be empty")
	}

	uri := fmt.Sprintf(
		"/%s/%s/gateway/lists/%s",
		AccountRouteRoot,
		accountID,
		listPatch.ID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, listPatch)
	if err != nil {
		return TeamsList{}, err
	}

	var teamsListDetailResponse TeamsListDetailResponse
	err = json.Unmarshal(res, &teamsListDetailResponse)
	if err != nil {
		return TeamsList{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsListDetailResponse.Result, nil
}

// DeleteTeamsList deletes a teams list.
//
// API reference: https://api.cloudflare.com/#teams-lists-delete-teams-list
func (api *API) DeleteTeamsList(ctx context.Context, accountID, teamsListID string) error {
	uri := fmt.Sprintf(
		"/%s/%s/gateway/lists/%s",
		AccountRouteRoot,
		accountID,
		teamsListID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
