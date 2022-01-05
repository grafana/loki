package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// LoadBalancerPool represents a load balancer pool's properties.
type LoadBalancerPool struct {
	ID                string                      `json:"id,omitempty"`
	CreatedOn         *time.Time                  `json:"created_on,omitempty"`
	ModifiedOn        *time.Time                  `json:"modified_on,omitempty"`
	Description       string                      `json:"description"`
	Name              string                      `json:"name"`
	Enabled           bool                        `json:"enabled"`
	MinimumOrigins    int                         `json:"minimum_origins,omitempty"`
	Monitor           string                      `json:"monitor,omitempty"`
	Origins           []LoadBalancerOrigin        `json:"origins"`
	NotificationEmail string                      `json:"notification_email,omitempty"`
	Latitude          *float32                    `json:"latitude,omitempty"`
	Longitude         *float32                    `json:"longitude,omitempty"`
	LoadShedding      *LoadBalancerLoadShedding   `json:"load_shedding,omitempty"`
	OriginSteering    *LoadBalancerOriginSteering `json:"origin_steering,omitempty"`

	// CheckRegions defines the geographic region(s) from where to run health-checks from - e.g. "WNAM", "WEU", "SAF", "SAM".
	// Providing a null/empty value means "all regions", which may not be available to all plan types.
	CheckRegions []string `json:"check_regions"`
}

// LoadBalancerOrigin represents a Load Balancer origin's properties.
type LoadBalancerOrigin struct {
	Name    string              `json:"name"`
	Address string              `json:"address"`
	Enabled bool                `json:"enabled"`
	Weight  float64             `json:"weight"`
	Header  map[string][]string `json:"header"`
}

// LoadBalancerOriginSteering controls origin selection for new sessions and traffic without session affinity.
type LoadBalancerOriginSteering struct {
	// Policy defaults to "random" (weighted) when empty or unspecified.
	Policy string `json:"policy,omitempty"`
}

// LoadBalancerMonitor represents a load balancer monitor's properties.
type LoadBalancerMonitor struct {
	ID              string              `json:"id,omitempty"`
	CreatedOn       *time.Time          `json:"created_on,omitempty"`
	ModifiedOn      *time.Time          `json:"modified_on,omitempty"`
	Type            string              `json:"type"`
	Description     string              `json:"description"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	Header          map[string][]string `json:"header"`
	Timeout         int                 `json:"timeout"`
	Retries         int                 `json:"retries"`
	Interval        int                 `json:"interval"`
	Port            uint16              `json:"port,omitempty"`
	ExpectedBody    string              `json:"expected_body"`
	ExpectedCodes   string              `json:"expected_codes"`
	FollowRedirects bool                `json:"follow_redirects"`
	AllowInsecure   bool                `json:"allow_insecure"`
	ProbeZone       string              `json:"probe_zone"`
}

// LoadBalancer represents a load balancer's properties.
type LoadBalancer struct {
	ID                        string                     `json:"id,omitempty"`
	CreatedOn                 *time.Time                 `json:"created_on,omitempty"`
	ModifiedOn                *time.Time                 `json:"modified_on,omitempty"`
	Description               string                     `json:"description"`
	Name                      string                     `json:"name"`
	TTL                       int                        `json:"ttl,omitempty"`
	FallbackPool              string                     `json:"fallback_pool"`
	DefaultPools              []string                   `json:"default_pools"`
	RegionPools               map[string][]string        `json:"region_pools"`
	PopPools                  map[string][]string        `json:"pop_pools"`
	Proxied                   bool                       `json:"proxied"`
	Enabled                   *bool                      `json:"enabled,omitempty"`
	Persistence               string                     `json:"session_affinity,omitempty"`
	PersistenceTTL            int                        `json:"session_affinity_ttl,omitempty"`
	SessionAffinityAttributes *SessionAffinityAttributes `json:"session_affinity_attributes,omitempty"`
	Rules                     []*LoadBalancerRule        `json:"rules,omitempty"`

	// SteeringPolicy controls pool selection logic.
	// "off" select pools in DefaultPools order
	// "geo" select pools based on RegionPools/PopPools
	// "dynamic_latency" select pools based on RTT (requires health checks)
	// "random" selects pools in a random order
	// "proximity" select pools based on 'distance' from request
	// "" maps to "geo" if RegionPools or PopPools have entries otherwise "off"
	SteeringPolicy string `json:"steering_policy,omitempty"`
}

// LoadBalancerLoadShedding contains the settings for controlling load shedding
type LoadBalancerLoadShedding struct {
	DefaultPercent float32 `json:"default_percent,omitempty"`
	DefaultPolicy  string  `json:"default_policy,omitempty"`
	SessionPercent float32 `json:"session_percent,omitempty"`
	SessionPolicy  string  `json:"session_policy,omitempty"`
}

// LoadBalancerRule represents a single rule entry for a Load Balancer. Each rules
// is run one after the other in priority order. Disabled rules are skipped.
type LoadBalancerRule struct {
	// Name is required but is only used for human readability
	Name string `json:"name"`
	// Priority controls the order of rule execution the lowest value will be invoked first
	Priority int  `json:"priority"`
	Disabled bool `json:"disabled"`

	Condition string                    `json:"condition"`
	Overrides LoadBalancerRuleOverrides `json:"overrides"`

	// Terminates flag this rule as 'terminating'. No further rules will
	// be executed after this one.
	Terminates bool `json:"terminates,omitempty"`

	// FixedResponse if set and the condition is true we will not run
	// routing logic but rather directly respond with the provided fields.
	// FixedResponse implies terminates.
	FixedResponse *LoadBalancerFixedResponseData `json:"fixed_response,omitempty"`
}

// LoadBalancerFixedResponseData contains all the data needed to generate
// a fixed response from a Load Balancer. This behavior can be enabled via Rules.
type LoadBalancerFixedResponseData struct {
	// MessageBody data to write into the http body
	MessageBody string `json:"message_body,omitempty"`
	// StatusCode the http status code to response with
	StatusCode int `json:"status_code,omitempty"`
	// ContentType value of the http 'content-type' header
	ContentType string `json:"content_type,omitempty"`
	// Location value of the http 'location' header
	Location string `json:"location,omitempty"`
}

// LoadBalancerRuleOverrides are the set of field overridable by the rules system.
type LoadBalancerRuleOverrides struct {
	// session affinity
	Persistence    string `json:"session_affinity,omitempty"`
	PersistenceTTL *uint  `json:"session_affinity_ttl,omitempty"`

	SessionAffinityAttrs *LoadBalancerRuleOverridesSessionAffinityAttrs `json:"session_affinity_attributes,omitempty"`

	TTL uint `json:"ttl,omitempty"`

	SteeringPolicy string `json:"steering_policy,omitempty"`
	FallbackPool   string `json:"fallback_pool,omitempty"`

	DefaultPools []string            `json:"default_pools,omitempty"`
	PoPPools     map[string][]string `json:"pop_pools,omitempty"`
	RegionPools  map[string][]string `json:"region_pools,omitempty"`
}

// LoadBalancerRuleOverridesSessionAffinityAttrs mimics SessionAffinityAttributes without the
// DrainDuration field as that field can not be overwritten via rules.
type LoadBalancerRuleOverridesSessionAffinityAttrs struct {
	SameSite string `json:"samesite,omitempty"`
	Secure   string `json:"secure,omitempty"`
}

// SessionAffinityAttributes represents the fields used to set attributes in a load balancer session affinity cookie.
type SessionAffinityAttributes struct {
	SameSite      string `json:"samesite,omitempty"`
	Secure        string `json:"secure,omitempty"`
	DrainDuration int    `json:"drain_duration,omitempty"`
}

// LoadBalancerOriginHealth represents the health of the origin.
type LoadBalancerOriginHealth struct {
	Healthy       bool     `json:"healthy,omitempty"`
	RTT           Duration `json:"rtt,omitempty"`
	FailureReason string   `json:"failure_reason,omitempty"`
	ResponseCode  int      `json:"response_code,omitempty"`
}

// LoadBalancerPoolPopHealth represents the health of the pool for given PoP.
type LoadBalancerPoolPopHealth struct {
	Healthy bool                                  `json:"healthy,omitempty"`
	Origins []map[string]LoadBalancerOriginHealth `json:"origins,omitempty"`
}

// LoadBalancerPoolHealth represents the healthchecks from different PoPs for a pool.
type LoadBalancerPoolHealth struct {
	ID        string                               `json:"pool_id,omitempty"`
	PopHealth map[string]LoadBalancerPoolPopHealth `json:"pop_health,omitempty"`
}

// loadBalancerPoolResponse represents the response from the load balancer pool endpoints.
type loadBalancerPoolResponse struct {
	Response
	Result LoadBalancerPool `json:"result"`
}

// loadBalancerPoolListResponse represents the response from the List Pools endpoint.
type loadBalancerPoolListResponse struct {
	Response
	Result     []LoadBalancerPool `json:"result"`
	ResultInfo ResultInfo         `json:"result_info"`
}

// loadBalancerMonitorResponse represents the response from the load balancer monitor endpoints.
type loadBalancerMonitorResponse struct {
	Response
	Result LoadBalancerMonitor `json:"result"`
}

// loadBalancerMonitorListResponse represents the response from the List Monitors endpoint.
type loadBalancerMonitorListResponse struct {
	Response
	Result     []LoadBalancerMonitor `json:"result"`
	ResultInfo ResultInfo            `json:"result_info"`
}

// loadBalancerResponse represents the response from the load balancer endpoints.
type loadBalancerResponse struct {
	Response
	Result LoadBalancer `json:"result"`
}

// loadBalancerListResponse represents the response from the List Load Balancers endpoint.
type loadBalancerListResponse struct {
	Response
	Result     []LoadBalancer `json:"result"`
	ResultInfo ResultInfo     `json:"result_info"`
}

// loadBalancerPoolHealthResponse represents the response from the Pool Health Details endpoint.
type loadBalancerPoolHealthResponse struct {
	Response
	Result LoadBalancerPoolHealth `json:"result"`
}

// CreateLoadBalancerPool creates a new load balancer pool.
//
// API reference: https://api.cloudflare.com/#load-balancer-pools-create-pool
func (api *API) CreateLoadBalancerPool(ctx context.Context, pool LoadBalancerPool) (LoadBalancerPool, error) {
	uri := fmt.Sprintf("%s/load_balancers/pools", api.userBaseURL("/user"))
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, pool)
	if err != nil {
		return LoadBalancerPool{}, err
	}
	var r loadBalancerPoolResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancerPool{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListLoadBalancerPools lists load balancer pools connected to an account.
//
// API reference: https://api.cloudflare.com/#load-balancer-pools-list-pools
func (api *API) ListLoadBalancerPools(ctx context.Context) ([]LoadBalancerPool, error) {
	uri := fmt.Sprintf("%s/load_balancers/pools", api.userBaseURL("/user"))
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r loadBalancerPoolListResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// LoadBalancerPoolDetails returns the details for a load balancer pool.
//
// API reference: https://api.cloudflare.com/#load-balancer-pools-pool-details
func (api *API) LoadBalancerPoolDetails(ctx context.Context, poolID string) (LoadBalancerPool, error) {
	uri := fmt.Sprintf("%s/load_balancers/pools/%s", api.userBaseURL("/user"), poolID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return LoadBalancerPool{}, err
	}
	var r loadBalancerPoolResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancerPool{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// DeleteLoadBalancerPool disables and deletes a load balancer pool.
//
// API reference: https://api.cloudflare.com/#load-balancer-pools-delete-pool
func (api *API) DeleteLoadBalancerPool(ctx context.Context, poolID string) error {
	uri := fmt.Sprintf("%s/load_balancers/pools/%s", api.userBaseURL("/user"), poolID)
	if _, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil); err != nil {
		return err
	}
	return nil
}

// ModifyLoadBalancerPool modifies a configured load balancer pool.
//
// API reference: https://api.cloudflare.com/#load-balancer-pools-update-pool
func (api *API) ModifyLoadBalancerPool(ctx context.Context, pool LoadBalancerPool) (LoadBalancerPool, error) {
	uri := fmt.Sprintf("%s/load_balancers/pools/%s", api.userBaseURL("/user"), pool.ID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, pool)
	if err != nil {
		return LoadBalancerPool{}, err
	}
	var r loadBalancerPoolResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancerPool{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// CreateLoadBalancerMonitor creates a new load balancer monitor.
//
// API reference: https://api.cloudflare.com/#load-balancer-monitors-create-monitor
func (api *API) CreateLoadBalancerMonitor(ctx context.Context, monitor LoadBalancerMonitor) (LoadBalancerMonitor, error) {
	uri := fmt.Sprintf("%s/load_balancers/monitors", api.userBaseURL("/user"))
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, monitor)
	if err != nil {
		return LoadBalancerMonitor{}, err
	}
	var r loadBalancerMonitorResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancerMonitor{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListLoadBalancerMonitors lists load balancer monitors connected to an account.
//
// API reference: https://api.cloudflare.com/#load-balancer-monitors-list-monitors
func (api *API) ListLoadBalancerMonitors(ctx context.Context) ([]LoadBalancerMonitor, error) {
	uri := fmt.Sprintf("%s/load_balancers/monitors", api.userBaseURL("/user"))
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r loadBalancerMonitorListResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// LoadBalancerMonitorDetails returns the details for a load balancer monitor.
//
// API reference: https://api.cloudflare.com/#load-balancer-monitors-monitor-details
func (api *API) LoadBalancerMonitorDetails(ctx context.Context, monitorID string) (LoadBalancerMonitor, error) {
	uri := fmt.Sprintf("%s/load_balancers/monitors/%s", api.userBaseURL("/user"), monitorID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return LoadBalancerMonitor{}, err
	}
	var r loadBalancerMonitorResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancerMonitor{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// DeleteLoadBalancerMonitor disables and deletes a load balancer monitor.
//
// API reference: https://api.cloudflare.com/#load-balancer-monitors-delete-monitor
func (api *API) DeleteLoadBalancerMonitor(ctx context.Context, monitorID string) error {
	uri := fmt.Sprintf("%s/load_balancers/monitors/%s", api.userBaseURL("/user"), monitorID)
	if _, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil); err != nil {
		return err
	}
	return nil
}

// ModifyLoadBalancerMonitor modifies a configured load balancer monitor.
//
// API reference: https://api.cloudflare.com/#load-balancer-monitors-update-monitor
func (api *API) ModifyLoadBalancerMonitor(ctx context.Context, monitor LoadBalancerMonitor) (LoadBalancerMonitor, error) {
	uri := fmt.Sprintf("%s/load_balancers/monitors/%s", api.userBaseURL("/user"), monitor.ID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, monitor)
	if err != nil {
		return LoadBalancerMonitor{}, err
	}
	var r loadBalancerMonitorResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancerMonitor{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// CreateLoadBalancer creates a new load balancer.
//
// API reference: https://api.cloudflare.com/#load-balancers-create-load-balancer
func (api *API) CreateLoadBalancer(ctx context.Context, zoneID string, lb LoadBalancer) (LoadBalancer, error) {
	uri := fmt.Sprintf("/zones/%s/load_balancers", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, lb)
	if err != nil {
		return LoadBalancer{}, err
	}
	var r loadBalancerResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancer{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListLoadBalancers lists load balancers configured on a zone.
//
// API reference: https://api.cloudflare.com/#load-balancers-list-load-balancers
func (api *API) ListLoadBalancers(ctx context.Context, zoneID string) ([]LoadBalancer, error) {
	uri := fmt.Sprintf("/zones/%s/load_balancers", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r loadBalancerListResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// LoadBalancerDetails returns the details for a load balancer.
//
// API reference: https://api.cloudflare.com/#load-balancers-load-balancer-details
func (api *API) LoadBalancerDetails(ctx context.Context, zoneID, lbID string) (LoadBalancer, error) {
	uri := fmt.Sprintf("/zones/%s/load_balancers/%s", zoneID, lbID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return LoadBalancer{}, err
	}
	var r loadBalancerResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancer{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// DeleteLoadBalancer disables and deletes a load balancer.
//
// API reference: https://api.cloudflare.com/#load-balancers-delete-load-balancer
func (api *API) DeleteLoadBalancer(ctx context.Context, zoneID, lbID string) error {
	uri := fmt.Sprintf("/zones/%s/load_balancers/%s", zoneID, lbID)
	if _, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil); err != nil {
		return err
	}
	return nil
}

// ModifyLoadBalancer modifies a configured load balancer.
//
// API reference: https://api.cloudflare.com/#load-balancers-update-load-balancer
func (api *API) ModifyLoadBalancer(ctx context.Context, zoneID string, lb LoadBalancer) (LoadBalancer, error) {
	uri := fmt.Sprintf("/zones/%s/load_balancers/%s", zoneID, lb.ID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, lb)
	if err != nil {
		return LoadBalancer{}, err
	}
	var r loadBalancerResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancer{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// PoolHealthDetails fetches the latest healtcheck details for a single pool.
//
// API reference: https://api.cloudflare.com/#load-balancer-pools-pool-health-details
func (api *API) PoolHealthDetails(ctx context.Context, poolID string) (LoadBalancerPoolHealth, error) {
	uri := fmt.Sprintf("%s/load_balancers/pools/%s/health", api.userBaseURL("/user"), poolID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return LoadBalancerPoolHealth{}, err
	}
	var r loadBalancerPoolHealthResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return LoadBalancerPoolHealth{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}
