package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"
)

// AuditLogAction is a member of AuditLog, the action that was taken.
type AuditLogAction struct {
	Result bool   `json:"result"`
	Type   string `json:"type"`
}

// AuditLogActor is a member of AuditLog, who performed the action.
type AuditLogActor struct {
	Email string `json:"email"`
	ID    string `json:"id"`
	IP    string `json:"ip"`
	Type  string `json:"type"`
}

// AuditLogOwner is a member of AuditLog, who owns this audit log.
type AuditLogOwner struct {
	ID string `json:"id"`
}

// AuditLogResource is a member of AuditLog, what was the action performed on.
type AuditLogResource struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// AuditLog is an resource that represents an update in the cloudflare dash
type AuditLog struct {
	Action       AuditLogAction         `json:"action"`
	Actor        AuditLogActor          `json:"actor"`
	ID           string                 `json:"id"`
	Metadata     map[string]interface{} `json:"metadata"`
	NewValue     string                 `json:"newValue"`
	NewValueJSON map[string]interface{} `json:"newValueJson"`
	OldValue     string                 `json:"oldValue"`
	OldValueJSON map[string]interface{} `json:"oldValueJson"`
	Owner        AuditLogOwner          `json:"owner"`
	Resource     AuditLogResource       `json:"resource"`
	When         time.Time              `json:"when"`
}

// AuditLogResponse is the response returned from the cloudflare v4 api
type AuditLogResponse struct {
	Response   Response
	Result     []AuditLog `json:"result"`
	ResultInfo `json:"result_info"`
}

// AuditLogFilter is an object for filtering the audit log response from the api.
type AuditLogFilter struct {
	ID         string
	ActorIP    string
	ActorEmail string
	Direction  string
	ZoneName   string
	Since      string
	Before     string
	PerPage    int
	Page       int
}

// ToQuery turns an audit log filter in to an HTTP Query Param
// list, suitable for use in a url.URL.RawQuery. It will not include empty
// members of the struct in the query parameters.
func (a AuditLogFilter) ToQuery() url.Values {
	v := url.Values{}

	if a.ID != "" {
		v.Add("id", a.ID)
	}
	if a.ActorIP != "" {
		v.Add("actor.ip", a.ActorIP)
	}
	if a.ActorEmail != "" {
		v.Add("actor.email", a.ActorEmail)
	}
	if a.ZoneName != "" {
		v.Add("zone.name", a.ZoneName)
	}
	if a.Direction != "" {
		v.Add("direction", a.Direction)
	}
	if a.Since != "" {
		v.Add("since", a.Since)
	}
	if a.Before != "" {
		v.Add("before", a.Before)
	}
	if a.PerPage > 0 {
		v.Add("per_page", strconv.Itoa(a.PerPage))
	}
	if a.Page > 0 {
		v.Add("page", strconv.Itoa(a.Page))
	}

	return v
}

// GetOrganizationAuditLogs will return the audit logs of a specific
// organization, based on the ID passed in. The audit logs can be
// filtered based on any argument in the AuditLogFilter
//
// API Reference: https://api.cloudflare.com/#audit-logs-list-organization-audit-logs
func (api *API) GetOrganizationAuditLogs(ctx context.Context, organizationID string, a AuditLogFilter) (AuditLogResponse, error) {
	uri := url.URL{
		Path:       path.Join("/accounts", organizationID, "audit_logs"),
		ForceQuery: true,
		RawQuery:   a.ToQuery().Encode(),
	}
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri.String(), nil)
	if err != nil {
		return AuditLogResponse{}, err
	}
	return unmarshalReturn(res)
}

// unmarshalReturn will unmarshal bytes and return an auditlogresponse
func unmarshalReturn(res []byte) (AuditLogResponse, error) {
	var auditResponse AuditLogResponse
	err := json.Unmarshal(res, &auditResponse)
	if err != nil {
		return auditResponse, err
	}
	return auditResponse, nil
}

// GetUserAuditLogs will return your user's audit logs. The audit logs can be
// filtered based on any argument in the AuditLogFilter
//
// API Reference: https://api.cloudflare.com/#audit-logs-list-user-audit-logs
func (api *API) GetUserAuditLogs(ctx context.Context, a AuditLogFilter) (AuditLogResponse, error) {
	uri := url.URL{
		Path:       path.Join("/user", "audit_logs"),
		ForceQuery: true,
		RawQuery:   a.ToQuery().Encode(),
	}
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri.String(), nil)
	if err != nil {
		return AuditLogResponse{}, err
	}
	return unmarshalReturn(res)
}
