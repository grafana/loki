package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// ProxyProtocol implements json.Unmarshaler in order to support deserializing of the deprecated boolean
// value for `proxy_protocol`
type ProxyProtocol string

// UnmarshalJSON handles deserializing of both the deprecated boolean value and the current string value
// for the `proxy_protocol` field.
func (p *ProxyProtocol) UnmarshalJSON(data []byte) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch pp := raw.(type) {
	case string:
		*p = ProxyProtocol(pp)
	case bool:
		if pp {
			*p = "v1"
		} else {
			*p = "off"
		}
	default:
		return fmt.Errorf("invalid type for proxy_protocol field: %T", pp)
	}
	return nil
}

// SpectrumApplicationOriginPort defines a union of a single port or range of ports
type SpectrumApplicationOriginPort struct {
	Port, Start, End uint16
}

// ErrOriginPortInvalid is a common error for failing to parse a single port or port range
var ErrOriginPortInvalid = errors.New("invalid origin port")

func (p *SpectrumApplicationOriginPort) parse(s string) error {
	switch split := strings.Split(s, "-"); len(split) {
	case 1:
		i, err := strconv.ParseUint(split[0], 10, 16)
		if err != nil {
			return err
		}
		p.Port = uint16(i)
	case 2:
		start, err := strconv.ParseUint(split[0], 10, 16)
		if err != nil {
			return err
		}
		end, err := strconv.ParseUint(split[1], 10, 16)
		if err != nil {
			return err
		}
		if start >= end {
			return ErrOriginPortInvalid
		}
		p.Start = uint16(start)
		p.End = uint16(end)
	default:
		return ErrOriginPortInvalid
	}
	return nil
}

// UnmarshalJSON converts a byte slice into a single port or port range
func (p *SpectrumApplicationOriginPort) UnmarshalJSON(b []byte) error {
	var port interface{}
	if err := json.Unmarshal(b, &port); err != nil {
		return err
	}

	switch i := port.(type) {
	case float64:
		p.Port = uint16(i)
	case string:
		if err := p.parse(i); err != nil {
			return err
		}
	}

	return nil
}

// MarshalJSON converts a single port or port range to a suitable byte slice
func (p *SpectrumApplicationOriginPort) MarshalJSON() ([]byte, error) {
	if p.End > 0 {
		return json.Marshal(fmt.Sprintf("%d-%d", p.Start, p.End))
	}
	return json.Marshal(p.Port)
}

// SpectrumApplication defines a single Spectrum Application.
type SpectrumApplication struct {
	ID               string                         `json:"id,omitempty"`
	Protocol         string                         `json:"protocol,omitempty"`
	IPv4             bool                           `json:"ipv4,omitempty"`
	DNS              SpectrumApplicationDNS         `json:"dns,omitempty"`
	OriginDirect     []string                       `json:"origin_direct,omitempty"`
	OriginPort       *SpectrumApplicationOriginPort `json:"origin_port,omitempty"`
	OriginDNS        *SpectrumApplicationOriginDNS  `json:"origin_dns,omitempty"`
	IPFirewall       bool                           `json:"ip_firewall,omitempty"`
	ProxyProtocol    ProxyProtocol                  `json:"proxy_protocol,omitempty"`
	TLS              string                         `json:"tls,omitempty"`
	TrafficType      string                         `json:"traffic_type,omitempty"`
	EdgeIPs          *SpectrumApplicationEdgeIPs    `json:"edge_ips,omitempty"`
	ArgoSmartRouting bool                           `json:"argo_smart_routing,omitempty"`
	CreatedOn        *time.Time                     `json:"created_on,omitempty"`
	ModifiedOn       *time.Time                     `json:"modified_on,omitempty"`
}

// UnmarshalJSON handles setting the `ProxyProtocol` field based on the value of the deprecated `spp` field.
func (a *SpectrumApplication) UnmarshalJSON(data []byte) error {
	var body map[string]interface{}
	if err := json.Unmarshal(data, &body); err != nil {
		return err
	}

	var app spectrumApplicationRaw
	if err := json.Unmarshal(data, &app); err != nil {
		return err
	}

	if spp, ok := body["spp"]; ok && spp.(bool) {
		app.ProxyProtocol = "simple"
	}

	*a = SpectrumApplication(app)
	return nil
}

// spectrumApplicationRaw is used to inspect an application body to support the deprecated boolean value for `spp`
type spectrumApplicationRaw SpectrumApplication

// SpectrumApplicationDNS holds the external DNS configuration for a Spectrum
// Application.
type SpectrumApplicationDNS struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// SpectrumApplicationOriginDNS holds the origin DNS configuration for a Spectrum
// Application.
type SpectrumApplicationOriginDNS struct {
	Name string `json:"name"`
}

// SpectrumApplicationDetailResponse is the structure of the detailed response
// from the API.
type SpectrumApplicationDetailResponse struct {
	Response
	Result SpectrumApplication `json:"result"`
}

// SpectrumApplicationsDetailResponse is the structure of the detailed response
// from the API.
type SpectrumApplicationsDetailResponse struct {
	Response
	Result []SpectrumApplication `json:"result"`
}

// SpectrumApplicationEdgeIPs represents configuration for Bring-Your-Own-IP
// https://developers.cloudflare.com/spectrum/getting-started/byoip/
type SpectrumApplicationEdgeIPs struct {
	Type         SpectrumApplicationEdgeType      `json:"type"`
	Connectivity *SpectrumApplicationConnectivity `json:"connectivity,omitempty"`
	IPs          []net.IP                         `json:"ips,omitempty"`
}

// SpectrumApplicationEdgeType for possible Edge configurations
type SpectrumApplicationEdgeType string

const (
	// SpectrumEdgeTypeDynamic IP config
	SpectrumEdgeTypeDynamic SpectrumApplicationEdgeType = "dynamic"
	// SpectrumEdgeTypeStatic IP config
	SpectrumEdgeTypeStatic SpectrumApplicationEdgeType = "static"
)

// UnmarshalJSON function for SpectrumApplicationEdgeType enum
func (t *SpectrumApplicationEdgeType) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}

	newEdgeType := SpectrumApplicationEdgeType(strings.ToLower(s))
	switch newEdgeType {
	case SpectrumEdgeTypeDynamic, SpectrumEdgeTypeStatic:
		*t = newEdgeType
		return nil
	}

	return errors.New(errUnmarshalError)
}

func (t SpectrumApplicationEdgeType) String() string {
	return string(t)
}

// SpectrumApplicationConnectivity specifies IP address type on the edge configuration
type SpectrumApplicationConnectivity string

const (
	// SpectrumConnectivityAll specifies IPv4/6 edge IP
	SpectrumConnectivityAll SpectrumApplicationConnectivity = "all"
	// SpectrumConnectivityIPv4 specifies IPv4 edge IP
	SpectrumConnectivityIPv4 SpectrumApplicationConnectivity = "ipv4"
	// SpectrumConnectivityIPv6 specifies IPv6 edge IP
	SpectrumConnectivityIPv6 SpectrumApplicationConnectivity = "ipv6"
	// SpectrumConnectivityStatic specifies static edge IP configuration
	SpectrumConnectivityStatic SpectrumApplicationConnectivity = "static"
)

func (c SpectrumApplicationConnectivity) String() string {
	return string(c)
}

// UnmarshalJSON function for SpectrumApplicationConnectivity enum
func (c *SpectrumApplicationConnectivity) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}

	newConnectivity := SpectrumApplicationConnectivity(strings.ToLower(s))
	if newConnectivity.Dynamic() {
		*c = newConnectivity
		return nil
	}

	return errors.New(errUnmarshalError)
}

// Dynamic checks if address family is specified as dynamic config
func (c SpectrumApplicationConnectivity) Dynamic() bool {
	switch c {
	case SpectrumConnectivityAll, SpectrumConnectivityIPv4, SpectrumConnectivityIPv6:
		return true
	}
	return false
}

// Static checks if address family is specified as static config
func (c SpectrumApplicationConnectivity) Static() bool {
	return c == SpectrumConnectivityStatic
}

// SpectrumApplications fetches all of the Spectrum applications for a zone.
//
// API reference: https://developers.cloudflare.com/spectrum/api-reference/#list-spectrum-applications
func (api *API) SpectrumApplications(ctx context.Context, zoneID string) ([]SpectrumApplication, error) {
	uri := fmt.Sprintf("/zones/%s/spectrum/apps", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []SpectrumApplication{}, err
	}

	var spectrumApplications SpectrumApplicationsDetailResponse
	err = json.Unmarshal(res, &spectrumApplications)
	if err != nil {
		return []SpectrumApplication{}, errors.Wrap(err, errUnmarshalError)
	}

	return spectrumApplications.Result, nil
}

// SpectrumApplication fetches a single Spectrum application based on the ID.
//
// API reference: https://developers.cloudflare.com/spectrum/api-reference/#list-spectrum-applications
func (api *API) SpectrumApplication(ctx context.Context, zoneID string, applicationID string) (SpectrumApplication, error) {
	uri := fmt.Sprintf(
		"/zones/%s/spectrum/apps/%s",
		zoneID,
		applicationID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return SpectrumApplication{}, err
	}

	var spectrumApplication SpectrumApplicationDetailResponse
	err = json.Unmarshal(res, &spectrumApplication)
	if err != nil {
		return SpectrumApplication{}, errors.Wrap(err, errUnmarshalError)
	}

	return spectrumApplication.Result, nil
}

// CreateSpectrumApplication creates a new Spectrum application.
//
// API reference: https://developers.cloudflare.com/spectrum/api-reference/#create-a-spectrum-application
func (api *API) CreateSpectrumApplication(ctx context.Context, zoneID string, appDetails SpectrumApplication) (SpectrumApplication, error) {
	uri := fmt.Sprintf("/zones/%s/spectrum/apps", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, appDetails)
	if err != nil {
		return SpectrumApplication{}, err
	}

	var spectrumApplication SpectrumApplicationDetailResponse
	err = json.Unmarshal(res, &spectrumApplication)
	if err != nil {
		return SpectrumApplication{}, errors.Wrap(err, errUnmarshalError)
	}

	return spectrumApplication.Result, nil
}

// UpdateSpectrumApplication updates an existing Spectrum application.
//
// API reference: https://developers.cloudflare.com/spectrum/api-reference/#update-a-spectrum-application
func (api *API) UpdateSpectrumApplication(ctx context.Context, zoneID, appID string, appDetails SpectrumApplication) (SpectrumApplication, error) {
	uri := fmt.Sprintf(
		"/zones/%s/spectrum/apps/%s",
		zoneID,
		appID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, appDetails)
	if err != nil {
		return SpectrumApplication{}, err
	}

	var spectrumApplication SpectrumApplicationDetailResponse
	err = json.Unmarshal(res, &spectrumApplication)
	if err != nil {
		return SpectrumApplication{}, errors.Wrap(err, errUnmarshalError)
	}

	return spectrumApplication.Result, nil
}

// DeleteSpectrumApplication removes a Spectrum application based on the ID.
//
// API reference: https://developers.cloudflare.com/spectrum/api-reference/#delete-a-spectrum-application
func (api *API) DeleteSpectrumApplication(ctx context.Context, zoneID string, applicationID string) error {
	uri := fmt.Sprintf(
		"/zones/%s/spectrum/apps/%s",
		zoneID,
		applicationID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
