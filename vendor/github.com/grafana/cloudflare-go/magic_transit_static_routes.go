package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"net/http"
	"time"
)

// Magic Transit Static Routes Error messages
const (
	errMagicTransitStaticRouteNotModified = "When trying to modify static route, API returned modified: false"
	errMagicTransitStaticRouteNotDeleted  = "When trying to delete static route, API returned deleted: false"
)

// MagicTransitStaticRouteScope contains information about a static route's scope
type MagicTransitStaticRouteScope struct {
	ColoRegions []string `json:"colo_regions,omitempty"`
	ColoNames   []string `json:"colo_names,omitempty"`
}

// MagicTransitStaticRoute contains information about a static route
type MagicTransitStaticRoute struct {
	ID          string                       `json:"id,omitempty"`
	Prefix      string                       `json:"prefix"`
	CreatedOn   *time.Time                   `json:"created_on,omitempty"`
	ModifiedOn  *time.Time                   `json:"modified_on,omitempty"`
	Nexthop     string                       `json:"nexthop"`
	Priority    int                          `json:"priority,omitempty"`
	Description string                       `json:"description,omitempty"`
	Weight      int                          `json:"weight,omitempty"`
	Scope       MagicTransitStaticRouteScope `json:"scope,omitempty"`
}

// ListMagicTransitStaticRoutesResponse contains a response including Magic Transit static routes
type ListMagicTransitStaticRoutesResponse struct {
	Response
	Result struct {
		Routes []MagicTransitStaticRoute `json:"routes"`
	} `json:"result"`
}

// GetMagicTransitStaticRouteResponse contains a response including exactly one static route
type GetMagicTransitStaticRouteResponse struct {
	Response
	Result struct {
		Route MagicTransitStaticRoute `json:"route"`
	} `json:"result"`
}

// UpdateMagicTransitStaticRouteResponse contains a static route update response
type UpdateMagicTransitStaticRouteResponse struct {
	Response
	Result struct {
		Modified      bool                    `json:"modified"`
		ModifiedRoute MagicTransitStaticRoute `json:"modified_route"`
	} `json:"result"`
}

// DeleteMagicTransitStaticRouteResponse contains a static route deletion response
type DeleteMagicTransitStaticRouteResponse struct {
	Response
	Result struct {
		Deleted      bool                    `json:"deleted"`
		DeletedRoute MagicTransitStaticRoute `json:"deleted_route"`
	} `json:"result"`
}

// CreateMagicTransitStaticRoutesRequest is an array of static routes to create
type CreateMagicTransitStaticRoutesRequest struct {
	Routes []MagicTransitStaticRoute `json:"routes"`
}

// ListMagicTransitStaticRoutes lists all static routes for a given account
//
// API reference: https://api.cloudflare.com/#magic-transit-static-routes-list-routes
func (api *API) ListMagicTransitStaticRoutes(ctx context.Context) ([]MagicTransitStaticRoute, error) {
	if err := api.checkAccountID(); err != nil {
		return []MagicTransitStaticRoute{}, err
	}

	uri := fmt.Sprintf("/accounts/%s/magic/routes", api.AccountID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []MagicTransitStaticRoute{}, err
	}

	result := ListMagicTransitStaticRoutesResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return []MagicTransitStaticRoute{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result.Routes, nil
}

// GetMagicTransitStaticRoute returns exactly one static route
//
// API reference: https://api.cloudflare.com/#magic-transit-static-routes-route-details
func (api *API) GetMagicTransitStaticRoute(ctx context.Context, id string) (MagicTransitStaticRoute, error) {
	if err := api.checkAccountID(); err != nil {
		return MagicTransitStaticRoute{}, err
	}

	uri := fmt.Sprintf("/accounts/%s/magic/routes/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return MagicTransitStaticRoute{}, err
	}

	result := GetMagicTransitStaticRouteResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return MagicTransitStaticRoute{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result.Route, nil
}

// CreateMagicTransitStaticRoute creates a new static route
//
// API reference: https://api.cloudflare.com/#magic-transit-static-routes-create-routes
func (api *API) CreateMagicTransitStaticRoute(ctx context.Context, route MagicTransitStaticRoute) ([]MagicTransitStaticRoute, error) {
	if err := api.checkAccountID(); err != nil {
		return []MagicTransitStaticRoute{}, err
	}

	uri := fmt.Sprintf("/accounts/%s/magic/routes", api.AccountID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, CreateMagicTransitStaticRoutesRequest{
		Routes: []MagicTransitStaticRoute{
			route,
		},
	})

	if err != nil {
		return []MagicTransitStaticRoute{}, err
	}

	result := ListMagicTransitStaticRoutesResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return []MagicTransitStaticRoute{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result.Routes, nil
}

// UpdateMagicTransitStaticRoute updates a static route
//
// API reference: https://api.cloudflare.com/#magic-transit-static-routes-update-route
func (api *API) UpdateMagicTransitStaticRoute(ctx context.Context, id string, route MagicTransitStaticRoute) (MagicTransitStaticRoute, error) {
	if err := api.checkAccountID(); err != nil {
		return MagicTransitStaticRoute{}, err
	}

	uri := fmt.Sprintf("/accounts/%s/magic/routes/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, route)

	if err != nil {
		return MagicTransitStaticRoute{}, err
	}

	result := UpdateMagicTransitStaticRouteResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return MagicTransitStaticRoute{}, errors.Wrap(err, errUnmarshalError)
	}

	if !result.Result.Modified {
		return MagicTransitStaticRoute{}, errors.New(errMagicTransitStaticRouteNotModified)
	}

	return result.Result.ModifiedRoute, nil
}

// DeleteMagicTransitStaticRoute deletes a static route
//
// API reference: https://api.cloudflare.com/#magic-transit-static-routes-delete-route
func (api *API) DeleteMagicTransitStaticRoute(ctx context.Context, id string) (MagicTransitStaticRoute, error) {
	if err := api.checkAccountID(); err != nil {
		return MagicTransitStaticRoute{}, err
	}

	uri := fmt.Sprintf("/accounts/%s/magic/routes/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)

	if err != nil {
		return MagicTransitStaticRoute{}, err
	}

	result := DeleteMagicTransitStaticRouteResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return MagicTransitStaticRoute{}, errors.Wrap(err, errUnmarshalError)
	}

	if !result.Result.Deleted {
		return MagicTransitStaticRoute{}, errors.New(errMagicTransitStaticRouteNotDeleted)
	}

	return result.Result.DeletedRoute, nil
}
