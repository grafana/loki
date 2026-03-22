package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

const (
	// IPListTypeIP specifies a list containing IP addresses
	IPListTypeIP = "ip"
)

// IPListBulkOperation contains information about a Bulk Operation
type IPListBulkOperation struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Error     string     `json:"error"`
	Completed *time.Time `json:"completed"`
}

// IPList contains information about an IP List
type IPList struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Description           string     `json:"description"`
	Kind                  string     `json:"kind"`
	NumItems              int        `json:"num_items"`
	NumReferencingFilters int        `json:"num_referencing_filters"`
	CreatedOn             *time.Time `json:"created_on"`
	ModifiedOn            *time.Time `json:"modified_on"`
}

// IPListItem contains information about a single IP List Item
type IPListItem struct {
	ID         string     `json:"id"`
	IP         string     `json:"ip"`
	Comment    string     `json:"comment"`
	CreatedOn  *time.Time `json:"created_on"`
	ModifiedOn *time.Time `json:"modified_on"`
}

// IPListCreateRequest contains data for a new IP List
type IPListCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
}

// IPListItemCreateRequest contains data for a new IP List Item
type IPListItemCreateRequest struct {
	IP      string `json:"ip"`
	Comment string `json:"comment"`
}

// IPListItemDeleteRequest wraps IP List Items that shall be deleted
type IPListItemDeleteRequest struct {
	Items []IPListItemDeleteItemRequest `json:"items"`
}

// IPListItemDeleteItemRequest contains single IP List Items that shall be deleted
type IPListItemDeleteItemRequest struct {
	ID string `json:"id"`
}

// IPListUpdateRequest contains data for an IP List update
type IPListUpdateRequest struct {
	Description string `json:"description"`
}

// IPListResponse contains a single IP List
type IPListResponse struct {
	Response
	Result IPList `json:"result"`
}

// IPListItemCreateResponse contains information about the creation of an IP List Item
type IPListItemCreateResponse struct {
	Response
	Result struct {
		OperationID string `json:"operation_id"`
	} `json:"result"`
}

// IPListListResponse contains a slice of IP Lists
type IPListListResponse struct {
	Response
	Result []IPList `json:"result"`
}

// IPListBulkOperationResponse contains information about a Bulk Operation
type IPListBulkOperationResponse struct {
	Response
	Result IPListBulkOperation `json:"result"`
}

// IPListDeleteResponse contains information about the deletion of an IP List
type IPListDeleteResponse struct {
	Response
	Result struct {
		ID string `json:"id"`
	} `json:"result"`
}

// IPListItemsListResponse contains information about IP List Items
type IPListItemsListResponse struct {
	Response
	ResultInfo `json:"result_info"`
	Result     []IPListItem `json:"result"`
}

// IPListItemDeleteResponse contains information about the deletion of an IP List Item
type IPListItemDeleteResponse struct {
	Response
	Result struct {
		OperationID string `json:"operation_id"`
	} `json:"result"`
}

// IPListItemsGetResponse contains information about a single IP List Item
type IPListItemsGetResponse struct {
	Response
	Result IPListItem `json:"result"`
}

// ListIPLists lists all IP Lists
//
// API reference: https://api.cloudflare.com/#rules-lists-list-lists
func (api *API) ListIPLists(ctx context.Context) ([]IPList, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists", api.AccountID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []IPList{}, err
	}

	result := IPListListResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return []IPList{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// CreateIPList creates a new IP List
//
// API reference: https://api.cloudflare.com/#rules-lists-create-list
func (api *API) CreateIPList(ctx context.Context, name string, description string, kind string) (IPList,
	error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists", api.AccountID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri,
		IPListCreateRequest{Name: name, Description: description, Kind: kind})
	if err != nil {
		return IPList{}, err
	}

	result := IPListResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPList{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// GetIPList returns a single IP List
//
// API reference: https://api.cloudflare.com/#rules-lists-get-list
func (api *API) GetIPList(ctx context.Context, id string) (IPList, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return IPList{}, err
	}

	result := IPListResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPList{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// UpdateIPList updates the description of an existing IP List
//
// API reference: https://api.cloudflare.com/#rules-lists-update-list
func (api *API) UpdateIPList(ctx context.Context, id string, description string) (IPList, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, IPListUpdateRequest{Description: description})
	if err != nil {
		return IPList{}, err
	}

	result := IPListResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPList{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// DeleteIPList deletes an IP List
//
// API reference: https://api.cloudflare.com/#rules-lists-delete-list
func (api *API) DeleteIPList(ctx context.Context, id string) (IPListDeleteResponse, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return IPListDeleteResponse{}, err
	}

	result := IPListDeleteResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPListDeleteResponse{}, errors.Wrap(err, errUnmarshalError)
	}

	return result, nil
}

// ListIPListItems returns a list with all items in an IP List
//
// API reference: https://api.cloudflare.com/#rules-lists-list-list-items
func (api *API) ListIPListItems(ctx context.Context, id string) ([]IPListItem, error) {
	var list []IPListItem
	var cursor string
	var cursorQuery string

	for {
		if len(cursor) > 0 {
			cursorQuery = fmt.Sprintf("?cursor=%s", cursor)
		}
		uri := fmt.Sprintf("/accounts/%s/rules/lists/%s/items%s", api.AccountID, id, cursorQuery)
		res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return []IPListItem{}, err
		}

		result := IPListItemsListResponse{}
		if err := json.Unmarshal(res, &result); err != nil {
			return []IPListItem{}, errors.Wrap(err, errUnmarshalError)
		}

		list = append(list, result.Result...)
		if cursor = result.ResultInfo.Cursors.After; cursor == "" {
			break
		}
	}

	return list, nil
}

// CreateIPListItemAsync creates a new IP List Item asynchronously. Users have to poll the operation status by
// using the operation_id returned by this function.
//
// API reference: https://api.cloudflare.com/#rules-lists-create-list-items
func (api *API) CreateIPListItemAsync(ctx context.Context, id, ip, comment string) (IPListItemCreateResponse, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s/items", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, []IPListItemCreateRequest{{IP: ip, Comment: comment}})
	if err != nil {
		return IPListItemCreateResponse{}, err
	}

	result := IPListItemCreateResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPListItemCreateResponse{}, errors.Wrap(err, errUnmarshalError)
	}

	return result, nil
}

// CreateIPListItem creates a new IP List Item synchronously and returns the current set of IP List Items
func (api *API) CreateIPListItem(ctx context.Context, id, ip, comment string) ([]IPListItem, error) {
	result, err := api.CreateIPListItemAsync(ctx, id, ip, comment)

	if err != nil {
		return []IPListItem{}, err
	}

	err = api.pollIPListBulkOperation(ctx, result.Result.OperationID)
	if err != nil {
		return []IPListItem{}, err
	}

	return api.ListIPListItems(ctx, id)
}

// CreateIPListItemsAsync bulk creates many IP List Items asynchronously. Users have to poll the operation status by
// using the operation_id returned by this function.
//
// API reference: https://api.cloudflare.com/#rules-lists-create-list-items
func (api *API) CreateIPListItemsAsync(ctx context.Context, id string, items []IPListItemCreateRequest) (
	IPListItemCreateResponse, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s/items", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, items)
	if err != nil {
		return IPListItemCreateResponse{}, err
	}

	result := IPListItemCreateResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPListItemCreateResponse{}, errors.Wrap(err, errUnmarshalError)
	}

	return result, nil
}

// CreateIPListItems bulk creates many IP List Items synchronously and returns the current set of IP List Items
func (api *API) CreateIPListItems(ctx context.Context, id string, items []IPListItemCreateRequest) (
	[]IPListItem, error) {
	result, err := api.CreateIPListItemsAsync(ctx, id, items)
	if err != nil {
		return []IPListItem{}, err
	}

	err = api.pollIPListBulkOperation(ctx, result.Result.OperationID)
	if err != nil {
		return []IPListItem{}, err
	}

	return api.ListIPListItems(ctx, id)
}

// ReplaceIPListItemsAsync replaces all IP List Items asynchronously. Users have to poll the operation status by
// using the operation_id returned by this function.
//
// API reference: https://api.cloudflare.com/#rules-lists-replace-list-items
func (api *API) ReplaceIPListItemsAsync(ctx context.Context, id string, items []IPListItemCreateRequest) (
	IPListItemCreateResponse, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s/items", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, items)
	if err != nil {
		return IPListItemCreateResponse{}, err
	}

	result := IPListItemCreateResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPListItemCreateResponse{}, errors.Wrap(err, errUnmarshalError)
	}

	return result, nil
}

// ReplaceIPListItems replaces all IP List Items synchronously and returns the current set of IP List Items
func (api *API) ReplaceIPListItems(ctx context.Context, id string, items []IPListItemCreateRequest) (
	[]IPListItem, error) {
	result, err := api.ReplaceIPListItemsAsync(ctx, id, items)
	if err != nil {
		return []IPListItem{}, err
	}

	err = api.pollIPListBulkOperation(ctx, result.Result.OperationID)
	if err != nil {
		return []IPListItem{}, err
	}

	return api.ListIPListItems(ctx, id)
}

// DeleteIPListItemsAsync removes specific Items of an IP List by their ID asynchronously. Users have to poll the
// operation status by using the operation_id returned by this function.
//
// API reference: https://api.cloudflare.com/#rules-lists-delete-list-items
func (api *API) DeleteIPListItemsAsync(ctx context.Context, id string, items IPListItemDeleteRequest) (
	IPListItemDeleteResponse, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s/items", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, items)
	if err != nil {
		return IPListItemDeleteResponse{}, err
	}

	result := IPListItemDeleteResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPListItemDeleteResponse{}, errors.Wrap(err, errUnmarshalError)
	}

	return result, nil
}

// DeleteIPListItems removes specific Items of an IP List by their ID synchronously and returns the current set
// of IP List Items
func (api *API) DeleteIPListItems(ctx context.Context, id string, items IPListItemDeleteRequest) (
	[]IPListItem, error) {
	result, err := api.DeleteIPListItemsAsync(ctx, id, items)
	if err != nil {
		return []IPListItem{}, err
	}

	err = api.pollIPListBulkOperation(ctx, result.Result.OperationID)
	if err != nil {
		return []IPListItem{}, err
	}

	return api.ListIPListItems(ctx, id)
}

// GetIPListItem returns a single IP List Item
//
// API reference: https://api.cloudflare.com/#rules-lists-get-list-item
func (api *API) GetIPListItem(ctx context.Context, listID, id string) (IPListItem, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/%s/items/%s", api.AccountID, listID, id)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return IPListItem{}, err
	}

	result := IPListItemsGetResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPListItem{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// GetIPListBulkOperation returns the status of a bulk operation
//
// API reference: https://api.cloudflare.com/#rules-lists-get-bulk-operation
func (api *API) GetIPListBulkOperation(ctx context.Context, id string) (IPListBulkOperation, error) {
	uri := fmt.Sprintf("/accounts/%s/rules/lists/bulk_operations/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return IPListBulkOperation{}, err
	}

	result := IPListBulkOperationResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPListBulkOperation{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// pollIPListBulkOperation implements synchronous behaviour for some asynchronous endpoints.
// bulk-operation status can be either pending, running, failed or completed
func (api *API) pollIPListBulkOperation(ctx context.Context, id string) error {
	for i := uint8(0); i < 16; i++ {
		sleepDuration := 1 << (i / 2) * time.Second
		select {
		case <-time.After(sleepDuration):
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "operation aborted during backoff")
		}

		bulkResult, err := api.GetIPListBulkOperation(ctx, id)
		if err != nil {
			return err
		}

		switch bulkResult.Status {
		case "failed":
			return errors.New(bulkResult.Error)
		case "pending", "running":
			continue
		case "completed":
			return nil
		default:
			return errors.New(fmt.Sprintf("%s: %s", errOperationUnexpectedStatus, bulkResult.Status))
		}
	}

	return errors.New(errOperationStillRunning)
}
