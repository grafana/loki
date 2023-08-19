package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// WaitingRoom describes a WaitingRoom object.
type WaitingRoom struct {
	ID                    string    `json:"id,omitempty"`
	CreatedOn             time.Time `json:"created_on,omitempty"`
	ModifiedOn            time.Time `json:"modified_on,omitempty"`
	Name                  string    `json:"name"`
	Description           string    `json:"description,omitempty"`
	Suspended             bool      `json:"suspended"`
	Host                  string    `json:"host"`
	Path                  string    `json:"path"`
	QueueAll              bool      `json:"queue_all"`
	NewUsersPerMinute     int       `json:"new_users_per_minute"`
	TotalActiveUsers      int       `json:"total_active_users"`
	SessionDuration       int       `json:"session_duration"`
	DisableSessionRenewal bool      `json:"disable_session_renewal"`
	CustomPageHTML        string    `json:"custom_page_html,omitempty"`
	JsonResponseEnabled   bool      `json:"json_response_enabled"`
}

// WaitingRoomDetailResponse is the API response, containing a single WaitingRoom.
type WaitingRoomDetailResponse struct {
	Response
	Result WaitingRoom `json:"result"`
}

// WaitingRoomsResponse is the API response, containing an array of WaitingRooms.
type WaitingRoomsResponse struct {
	Response
	Result []WaitingRoom `json:"result"`
}

// CreateWaitingRoom creates a new Waiting Room for a zone.
//
// API reference: https://api.cloudflare.com/#waiting-room-create-waiting-room
func (api *API) CreateWaitingRoom(ctx context.Context, zoneID string, waitingRoom WaitingRoom) (*WaitingRoom, error) {
	uri := fmt.Sprintf("/zones/%s/waiting_rooms", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, waitingRoom)
	if err != nil {
		return nil, err
	}
	var r WaitingRoomDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return &r.Result, nil
}

// ListWaitingRooms returns all Waiting Room for a zone.
//
// API reference: https://api.cloudflare.com/#waiting-room-list-waiting-rooms
func (api *API) ListWaitingRooms(ctx context.Context, zoneID string) ([]WaitingRoom, error) {
	uri := fmt.Sprintf("/zones/%s/waiting_rooms", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []WaitingRoom{}, err
	}
	var r WaitingRoomsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []WaitingRoom{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// WaitingRoom fetches detail about one Waiting room for a zone.
//
// API reference: https://api.cloudflare.com/#waiting-room-waiting-room-details
func (api *API) WaitingRoom(ctx context.Context, zoneID, waitingRoomID string) (WaitingRoom, error) {
	uri := fmt.Sprintf("/zones/%s/waiting_rooms/%s", zoneID, waitingRoomID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return WaitingRoom{}, err
	}
	var r WaitingRoomDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WaitingRoom{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ChangeWaitingRoom lets you change individual settings for a Waiting room. This is
// in contrast to UpdateWaitingRoom which replaces the entire Waiting room.
//
// API reference: https://api.cloudflare.com/#waiting-room-update-waiting-room
func (api *API) ChangeWaitingRoom(ctx context.Context, zoneID, waitingRoomID string, waitingRoom WaitingRoom) (WaitingRoom, error) {
	uri := fmt.Sprintf("/zones/%s/waiting_rooms/%s", zoneID, waitingRoomID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, waitingRoom)
	if err != nil {
		return WaitingRoom{}, err
	}
	var r WaitingRoomDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WaitingRoom{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdateWaitingRoom lets you replace a Waiting Room. This is in contrast to
// ChangeWaitingRoom which lets you change individual settings.
//
// API reference: https://api.cloudflare.com/#waiting-room-update-waiting-room
func (api *API) UpdateWaitingRoom(ctx context.Context, zoneID string, waitingRoom WaitingRoom) (WaitingRoom, error) {
	uri := fmt.Sprintf("/zones/%s/waiting_rooms/%s", zoneID, waitingRoom.ID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, waitingRoom)
	if err != nil {
		return WaitingRoom{}, err
	}
	var r WaitingRoomDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WaitingRoom{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// DeleteWaitingRoom deletes a Waiting Room for a zone.
//
// API reference: https://api.cloudflare.com/#waiting-room-delete-waiting-room
func (api *API) DeleteWaitingRoom(ctx context.Context, zoneID, waitingRoomID string) error {
	uri := fmt.Sprintf("/zones/%s/waiting_rooms/%s", zoneID, waitingRoomID)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}
	var r WaitingRoomDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}
	return nil
}
