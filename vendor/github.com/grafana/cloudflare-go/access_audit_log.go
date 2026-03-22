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

// AccessAuditLogRecord is the structure of a single Access Audit Log entry.
type AccessAuditLogRecord struct {
	UserEmail  string     `json:"user_email"`
	IPAddress  string     `json:"ip_address"`
	AppUID     string     `json:"app_uid"`
	AppDomain  string     `json:"app_domain"`
	Action     string     `json:"action"`
	Connection string     `json:"connection"`
	Allowed    bool       `json:"allowed"`
	CreatedAt  *time.Time `json:"created_at"`
	RayID      string     `json:"ray_id"`
}

// AccessAuditLogListResponse represents the response from the list
// access applications endpoint.
type AccessAuditLogListResponse struct {
	Result []AccessAuditLogRecord `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// AccessAuditLogFilterOptions provides the structure of available audit log
// filters.
type AccessAuditLogFilterOptions struct {
	Direction string
	Since     *time.Time
	Until     *time.Time
	Limit     int
}

// AccessAuditLogs retrieves all audit logs for the Access service.
//
// API reference: https://api.cloudflare.com/#access-requests-access-requests-audit
func (api *API) AccessAuditLogs(ctx context.Context, accountID string, opts AccessAuditLogFilterOptions) ([]AccessAuditLogRecord, error) {
	uri := fmt.Sprintf("/accounts/%s/access/logs/access-requests?%s", accountID, opts.Encode())

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []AccessAuditLogRecord{}, err
	}

	var accessAuditLogListResponse AccessAuditLogListResponse
	err = json.Unmarshal(res, &accessAuditLogListResponse)
	if err != nil {
		return []AccessAuditLogRecord{}, errors.Wrap(err, errUnmarshalError)
	}

	return accessAuditLogListResponse.Result, nil
}

// Encode is a custom method for encoding the filter options into a usable HTTP
// query parameter string.
func (a AccessAuditLogFilterOptions) Encode() string {
	v := url.Values{}

	if a.Direction != "" {
		v.Set("direction", a.Direction)
	}

	if a.Limit > 0 {
		v.Set("limit", strconv.Itoa(a.Limit))
	}

	if a.Since != nil {
		v.Set("since", (*a.Since).Format(time.RFC3339))
	}

	if a.Until != nil {
		v.Set("until", (*a.Until).Format(time.RFC3339))
	}

	return v.Encode()
}
