package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/idna"
)

// Owner describes the resource owner.
type Owner struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	OwnerType string `json:"type"`
}

// Zone describes a Cloudflare zone.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// DevMode contains the time in seconds until development expires (if
	// positive) or since it expired (if negative). It will be 0 if never used.
	DevMode           int       `json:"development_mode"`
	OriginalNS        []string  `json:"original_name_servers"`
	OriginalRegistrar string    `json:"original_registrar"`
	OriginalDNSHost   string    `json:"original_dnshost"`
	CreatedOn         time.Time `json:"created_on"`
	ModifiedOn        time.Time `json:"modified_on"`
	NameServers       []string  `json:"name_servers"`
	Owner             Owner     `json:"owner"`
	Permissions       []string  `json:"permissions"`
	Plan              ZonePlan  `json:"plan"`
	PlanPending       ZonePlan  `json:"plan_pending,omitempty"`
	Status            string    `json:"status"`
	Paused            bool      `json:"paused"`
	Type              string    `json:"type"`
	Host              struct {
		Name    string
		Website string
	} `json:"host"`
	VanityNS        []string `json:"vanity_name_servers"`
	Betas           []string `json:"betas"`
	DeactReason     string   `json:"deactivation_reason"`
	Meta            ZoneMeta `json:"meta"`
	Account         Account  `json:"account"`
	VerificationKey string   `json:"verification_key"`
}

// ZoneMeta describes metadata about a zone.
type ZoneMeta struct {
	// custom_certificate_quota is broken - sometimes it's a string, sometimes a number!
	// CustCertQuota     int    `json:"custom_certificate_quota"`
	PageRuleQuota     int  `json:"page_rule_quota"`
	WildcardProxiable bool `json:"wildcard_proxiable"`
	PhishingDetected  bool `json:"phishing_detected"`
}

// ZonePlan contains the plan information for a zone.
type ZonePlan struct {
	ZonePlanCommon
	IsSubscribed      bool   `json:"is_subscribed"`
	CanSubscribe      bool   `json:"can_subscribe"`
	LegacyID          string `json:"legacy_id"`
	LegacyDiscount    bool   `json:"legacy_discount"`
	ExternallyManaged bool   `json:"externally_managed"`
}

// ZoneRatePlan contains the plan information for a zone.
type ZoneRatePlan struct {
	ZonePlanCommon
	Components []zoneRatePlanComponents `json:"components,omitempty"`
}

// ZonePlanCommon contains fields used by various Plan endpoints
type ZonePlanCommon struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Price     int    `json:"price,omitempty"`
	Currency  string `json:"currency,omitempty"`
	Frequency string `json:"frequency,omitempty"`
}

type zoneRatePlanComponents struct {
	Name      string `json:"name"`
	Default   int    `json:"Default"`
	UnitPrice int    `json:"unit_price"`
}

// ZoneID contains only the zone ID.
type ZoneID struct {
	ID string `json:"id"`
}

// ZoneResponse represents the response from the Zone endpoint containing a single zone.
type ZoneResponse struct {
	Response
	Result Zone `json:"result"`
}

// ZonesResponse represents the response from the Zone endpoint containing an array of zones.
type ZonesResponse struct {
	Response
	Result     []Zone `json:"result"`
	ResultInfo `json:"result_info"`
}

// ZoneIDResponse represents the response from the Zone endpoint, containing only a zone ID.
type ZoneIDResponse struct {
	Response
	Result ZoneID `json:"result"`
}

// AvailableZoneRatePlansResponse represents the response from the Available Rate Plans endpoint.
type AvailableZoneRatePlansResponse struct {
	Response
	Result     []ZoneRatePlan `json:"result"`
	ResultInfo `json:"result_info"`
}

// AvailableZonePlansResponse represents the response from the Available Plans endpoint.
type AvailableZonePlansResponse struct {
	Response
	Result []ZonePlan `json:"result"`
	ResultInfo
}

// ZoneRatePlanResponse represents the response from the Plan Details endpoint.
type ZoneRatePlanResponse struct {
	Response
	Result ZoneRatePlan `json:"result"`
}

// ZoneSetting contains settings for a zone.
type ZoneSetting struct {
	ID            string      `json:"id"`
	Editable      bool        `json:"editable"`
	ModifiedOn    string      `json:"modified_on,omitempty"`
	Value         interface{} `json:"value"`
	TimeRemaining int         `json:"time_remaining"`
}

// ZoneSettingResponse represents the response from the Zone Setting endpoint.
type ZoneSettingResponse struct {
	Response
	Result []ZoneSetting `json:"result"`
}

// ZoneSettingSingleResponse represents the response from the Zone Setting endpoint for the specified setting.
type ZoneSettingSingleResponse struct {
	Response
	Result ZoneSetting `json:"result"`
}

// ZoneSSLSetting contains ssl setting for a zone.
type ZoneSSLSetting struct {
	ID                string `json:"id"`
	Editable          bool   `json:"editable"`
	ModifiedOn        string `json:"modified_on"`
	Value             string `json:"value"`
	CertificateStatus string `json:"certificate_status"`
}

// ZoneSSLSettingResponse represents the response from the Zone SSL Setting
// endpoint.
type ZoneSSLSettingResponse struct {
	Response
	Result ZoneSSLSetting `json:"result"`
}

// ZoneAnalyticsData contains totals and timeseries analytics data for a zone.
type ZoneAnalyticsData struct {
	Totals     ZoneAnalytics   `json:"totals"`
	Timeseries []ZoneAnalytics `json:"timeseries"`
}

// zoneAnalyticsDataResponse represents the response from the Zone Analytics Dashboard endpoint.
type zoneAnalyticsDataResponse struct {
	Response
	Result ZoneAnalyticsData `json:"result"`
}

// ZoneAnalyticsColocation contains analytics data by datacenter.
type ZoneAnalyticsColocation struct {
	ColocationID string          `json:"colo_id"`
	Timeseries   []ZoneAnalytics `json:"timeseries"`
}

// zoneAnalyticsColocationResponse represents the response from the Zone Analytics By Co-location endpoint.
type zoneAnalyticsColocationResponse struct {
	Response
	Result []ZoneAnalyticsColocation `json:"result"`
}

// ZoneAnalytics contains analytics data for a zone.
type ZoneAnalytics struct {
	Since    time.Time `json:"since"`
	Until    time.Time `json:"until"`
	Requests struct {
		All         int            `json:"all"`
		Cached      int            `json:"cached"`
		Uncached    int            `json:"uncached"`
		ContentType map[string]int `json:"content_type"`
		Country     map[string]int `json:"country"`
		SSL         struct {
			Encrypted   int `json:"encrypted"`
			Unencrypted int `json:"unencrypted"`
		} `json:"ssl"`
		HTTPStatus map[string]int `json:"http_status"`
	} `json:"requests"`
	Bandwidth struct {
		All         int            `json:"all"`
		Cached      int            `json:"cached"`
		Uncached    int            `json:"uncached"`
		ContentType map[string]int `json:"content_type"`
		Country     map[string]int `json:"country"`
		SSL         struct {
			Encrypted   int `json:"encrypted"`
			Unencrypted int `json:"unencrypted"`
		} `json:"ssl"`
	} `json:"bandwidth"`
	Threats struct {
		All     int            `json:"all"`
		Country map[string]int `json:"country"`
		Type    map[string]int `json:"type"`
	} `json:"threats"`
	Pageviews struct {
		All           int            `json:"all"`
		SearchEngines map[string]int `json:"search_engines"`
	} `json:"pageviews"`
	Uniques struct {
		All int `json:"all"`
	}
}

// ZoneAnalyticsOptions represents the optional parameters in Zone Analytics
// endpoint requests.
type ZoneAnalyticsOptions struct {
	Since      *time.Time
	Until      *time.Time
	Continuous *bool
}

// PurgeCacheRequest represents the request format made to the purge endpoint.
type PurgeCacheRequest struct {
	Everything bool `json:"purge_everything,omitempty"`
	// Purge by filepath (exact match). Limit of 30
	Files []string `json:"files,omitempty"`
	// Purge by Tag (Enterprise only):
	// https://support.cloudflare.com/hc/en-us/articles/206596608-How-to-Purge-Cache-Using-Cache-Tags-Enterprise-only-
	Tags []string `json:"tags,omitempty"`
	// Purge by hostname - e.g. "assets.example.com"
	Hosts []string `json:"hosts,omitempty"`
	// Purge by prefix - e.g. "example.com/css"
	Prefixes []string `json:"prefixes,omitempty"`
}

// PurgeCacheResponse represents the response from the purge endpoint.
type PurgeCacheResponse struct {
	Response
	Result struct {
		ID string `json:"id"`
	} `json:"result"`
}

// newZone describes a new zone.
type newZone struct {
	Name      string `json:"name"`
	JumpStart bool   `json:"jump_start"`
	Type      string `json:"type"`
	// We use a pointer to get a nil type when the field is empty.
	// This allows us to completely omit this with json.Marshal().
	Account *Account `json:"organization,omitempty"`
}

// FallbackOrigin describes a fallback origin
type FallbackOrigin struct {
	Value string `json:"value"`
	ID    string `json:"id,omitempty"`
}

// FallbackOriginResponse represents the response from the fallback_origin endpoint
type FallbackOriginResponse struct {
	Response
	Result FallbackOrigin `json:"result"`
}

// zoneSubscriptionRatePlanPayload is used to build the JSON payload for
// setting a particular rate plan on an existing zone.
type zoneSubscriptionRatePlanPayload struct {
	RatePlan struct {
		ID string `json:"id"`
	} `json:"rate_plan"`
}

// CreateZone creates a zone on an account.
//
// Setting jumpstart to true will attempt to automatically scan for existing
// DNS records. Setting this to false will create the zone with no DNS records.
//
// If account is non-empty, it must have at least the ID field populated.
// This will add the new zone to the specified multi-user account.
//
// API reference: https://api.cloudflare.com/#zone-create-a-zone
func (api *API) CreateZone(ctx context.Context, name string, jumpstart bool, account Account, zoneType string) (Zone, error) {
	var newzone newZone
	newzone.Name = name
	newzone.JumpStart = jumpstart
	if account.ID != "" {
		newzone.Account = &account
	}

	if zoneType == "partial" {
		newzone.Type = "partial"
	} else {
		newzone.Type = "full"
	}

	res, err := api.makeRequestContext(ctx, http.MethodPost, "/zones", newzone)
	if err != nil {
		return Zone{}, err
	}

	var r ZoneResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return Zone{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ZoneActivationCheck initiates another zone activation check for newly-created zones.
//
// API reference: https://api.cloudflare.com/#zone-initiate-another-zone-activation-check
func (api *API) ZoneActivationCheck(ctx context.Context, zoneID string) (Response, error) {
	res, err := api.makeRequestContext(ctx, http.MethodPut, "/zones/"+zoneID+"/activation_check", nil)
	if err != nil {
		return Response{}, err
	}
	var r Response
	err = json.Unmarshal(res, &r)
	if err != nil {
		return Response{}, errors.Wrap(err, errUnmarshalError)
	}
	return r, nil
}

// ListZones lists zones on an account. Optionally takes a list of zone names
// to filter against.
//
// API reference: https://api.cloudflare.com/#zone-list-zones
func (api *API) ListZones(ctx context.Context, z ...string) ([]Zone, error) {
	var zones []Zone
	if len(z) > 0 {
		var (
			v = url.Values{}
			r ZonesResponse
		)
		for _, zone := range z {
			v.Set("name", normalizeZoneName(zone))
			res, err := api.makeRequestContext(ctx, http.MethodGet, "/zones?"+v.Encode(), nil)
			if err != nil {
				return []Zone{}, err
			}
			err = json.Unmarshal(res, &r)
			if err != nil {
				return []Zone{}, errors.Wrap(err, errUnmarshalError)
			}
			if !r.Success {
				// TODO: Provide an actual error message instead of always returning nil
				return []Zone{}, err
			}
			zones = append(zones, r.Result...)
		}
	} else {
		res, err := api.ListZonesContext(ctx)
		if err != nil {
			return nil, err
		}

		zones = res.Result
	}

	return zones, nil
}

const listZonesPerPage = 50

// listZonesFetch fetches one page of zones.
// This is placed as a separate function to prevent any possibility of unintended capturing.
func (api *API) listZonesFetch(ctx context.Context, wg *sync.WaitGroup, errc chan error,
	path string, pageSize int, buf []Zone) {
	defer wg.Done()

	// recordError sends the error to errc in a non-blocking manner
	recordError := func(err error) {
		select {
		case errc <- err:
		default:
		}
	}

	res, err := api.makeRequestContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		recordError(err)
		return
	}

	var r ZonesResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		recordError(err)
		return
	}

	if len(r.Result) != pageSize {
		recordError(errors.New(errResultInfo))
		return
	}

	copy(buf, r.Result)
}

// ListZonesContext lists all zones on an account automatically handling the
// pagination. Optionally takes a list of ReqOptions.
func (api *API) ListZonesContext(ctx context.Context, opts ...ReqOption) (r ZonesResponse, err error) {
	opt := reqOption{
		params: url.Values{},
	}
	for _, of := range opts {
		of(&opt)
	}

	if opt.params.Get("page") != "" || opt.params.Get("per_page") != "" {
		return ZonesResponse{}, errors.New(errManualPagination)
	}

	opt.params.Add("per_page", strconv.Itoa(listZonesPerPage))

	res, err := api.makeRequestContext(ctx, http.MethodGet, "/zones?"+opt.params.Encode(), nil)
	if err != nil {
		return ZonesResponse{}, err
	}
	err = json.Unmarshal(res, &r)
	if err != nil {
		return ZonesResponse{}, errors.Wrap(err, errUnmarshalError)
	}

	// avoid overhead in most common cases where the total #zones <= 50
	if r.TotalPages < 2 {
		return r, nil
	}

	// parameters of pagination
	var (
		totalPageCount = r.TotalPages
		totalCount     = r.Total

		// zones is a large slice to prevent resizing during concurrent writes.
		zones = make([]Zone, totalCount)
	)

	// Copy the first page into zones.
	copy(zones, r.Result)

	var wg sync.WaitGroup
	wg.Add(totalPageCount - 1)  // all pages except the first one.
	errc := make(chan error, 1) // getting the first error

	// Creating all the workers.
	for pageNum := 2; pageNum <= totalPageCount; pageNum++ {
		// Note: URL.Values is just a map[string], so this would override the existing 'page'
		opt.params.Set("page", strconv.Itoa(pageNum))

		// start is the first index in the zone buffer
		start := listZonesPerPage * (pageNum - 1)

		pageSize := listZonesPerPage
		if pageNum == totalPageCount {
			// The size of the last page (which would be <= 50).
			pageSize = totalCount - start
		}

		go api.listZonesFetch(ctx, &wg, errc, "/zones?"+opt.params.Encode(), pageSize, zones[start:])
	}

	wg.Wait()

	select {
	case err := <-errc: // if there were any errors
		return ZonesResponse{}, err
	default: // if there were no errors, the receive statement should block
		r.Result = zones
		return r, nil
	}
}

// ZoneDetails fetches information about a zone.
//
// API reference: https://api.cloudflare.com/#zone-zone-details
func (api *API) ZoneDetails(ctx context.Context, zoneID string) (Zone, error) {
	res, err := api.makeRequestContext(ctx, http.MethodGet, "/zones/"+zoneID, nil)
	if err != nil {
		return Zone{}, err
	}
	var r ZoneResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return Zone{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ZoneOptions is a subset of Zone, for editable options.
type ZoneOptions struct {
	Paused   *bool     `json:"paused,omitempty"`
	VanityNS []string  `json:"vanity_name_servers,omitempty"`
	Plan     *ZonePlan `json:"plan,omitempty"`
	Type     string    `json:"type,omitempty"`
}

// ZoneSetPaused pauses Cloudflare service for the entire zone, sending all
// traffic direct to the origin.
func (api *API) ZoneSetPaused(ctx context.Context, zoneID string, paused bool) (Zone, error) {
	zoneopts := ZoneOptions{Paused: &paused}
	zone, err := api.EditZone(ctx, zoneID, zoneopts)
	if err != nil {
		return Zone{}, err
	}

	return zone, nil
}

// ZoneSetType toggles the type for an existing zone.
//
// Valid values for `type` are "full" and "partial"
//
// API reference: https://api.cloudflare.com/#zone-edit-zone
func (api *API) ZoneSetType(ctx context.Context, zoneID string, zoneType string) (Zone, error) {
	zoneopts := ZoneOptions{Type: zoneType}
	zone, err := api.EditZone(ctx, zoneID, zoneopts)
	if err != nil {
		return Zone{}, err
	}

	return zone, nil
}

// ZoneSetVanityNS sets custom nameservers for the zone.
// These names must be within the same zone.
func (api *API) ZoneSetVanityNS(ctx context.Context, zoneID string, ns []string) (Zone, error) {
	zoneopts := ZoneOptions{VanityNS: ns}
	zone, err := api.EditZone(ctx, zoneID, zoneopts)
	if err != nil {
		return Zone{}, err
	}

	return zone, nil
}

// ZoneSetPlan sets the rate plan of an existing zone.
//
// Valid values for `planType` are "CF_FREE", "CF_PRO", "CF_BIZ" and
// "CF_ENT".
//
// API reference: https://api.cloudflare.com/#zone-subscription-create-zone-subscription
func (api *API) ZoneSetPlan(ctx context.Context, zoneID string, planType string) error {
	zonePayload := zoneSubscriptionRatePlanPayload{}
	zonePayload.RatePlan.ID = planType

	uri := fmt.Sprintf("/zones/%s/subscription", zoneID)

	_, err := api.makeRequestContext(ctx, http.MethodPost, uri, zonePayload)
	if err != nil {
		return err
	}

	return nil
}

// ZoneUpdatePlan updates the rate plan of an existing zone.
//
// Valid values for `planType` are "CF_FREE", "CF_PRO", "CF_BIZ" and
// "CF_ENT".
//
// API reference: https://api.cloudflare.com/#zone-subscription-update-zone-subscription
func (api *API) ZoneUpdatePlan(ctx context.Context, zoneID string, planType string) error {
	zonePayload := zoneSubscriptionRatePlanPayload{}
	zonePayload.RatePlan.ID = planType

	uri := fmt.Sprintf("/zones/%s/subscription", zoneID)

	_, err := api.makeRequestContext(ctx, http.MethodPut, uri, zonePayload)
	if err != nil {
		return err
	}

	return nil
}

// EditZone edits the given zone.
//
// This is usually called by ZoneSetPaused, ZoneSetType, or ZoneSetVanityNS.
//
// API reference: https://api.cloudflare.com/#zone-edit-zone-properties
func (api *API) EditZone(ctx context.Context, zoneID string, zoneOpts ZoneOptions) (Zone, error) {
	res, err := api.makeRequestContext(ctx, http.MethodPatch, "/zones/"+zoneID, zoneOpts)
	if err != nil {
		return Zone{}, err
	}
	var r ZoneResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return Zone{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}

// PurgeEverything purges the cache for the given zone.
//
// Note: this will substantially increase load on the origin server for that
// zone if there is a high cached vs. uncached request ratio.
//
// API reference: https://api.cloudflare.com/#zone-purge-all-files
func (api *API) PurgeEverything(ctx context.Context, zoneID string) (PurgeCacheResponse, error) {
	uri := fmt.Sprintf("/zones/%s/purge_cache", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, PurgeCacheRequest{true, nil, nil, nil, nil})
	if err != nil {
		return PurgeCacheResponse{}, err
	}
	var r PurgeCacheResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return PurgeCacheResponse{}, errors.Wrap(err, errUnmarshalError)
	}
	return r, nil
}

// PurgeCache purges the cache using the given PurgeCacheRequest (zone/url/tag).
//
// API reference: https://api.cloudflare.com/#zone-purge-individual-files-by-url-and-cache-tags
func (api *API) PurgeCache(ctx context.Context, zoneID string, pcr PurgeCacheRequest) (PurgeCacheResponse, error) {
	return api.PurgeCacheContext(ctx, zoneID, pcr)
}

// PurgeCacheContext purges the cache using the given PurgeCacheRequest (zone/url/tag).
//
// API reference: https://api.cloudflare.com/#zone-purge-individual-files-by-url-and-cache-tags
func (api *API) PurgeCacheContext(ctx context.Context, zoneID string, pcr PurgeCacheRequest) (PurgeCacheResponse, error) {
	uri := fmt.Sprintf("/zones/%s/purge_cache", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, pcr)
	if err != nil {
		return PurgeCacheResponse{}, err
	}
	var r PurgeCacheResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return PurgeCacheResponse{}, errors.Wrap(err, errUnmarshalError)
	}
	return r, nil
}

// DeleteZone deletes the given zone.
//
// API reference: https://api.cloudflare.com/#zone-delete-a-zone
func (api *API) DeleteZone(ctx context.Context, zoneID string) (ZoneID, error) {
	res, err := api.makeRequestContext(ctx, http.MethodDelete, "/zones/"+zoneID, nil)
	if err != nil {
		return ZoneID{}, err
	}
	var r ZoneIDResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return ZoneID{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// AvailableZoneRatePlans returns information about all plans available to the specified zone.
//
// API reference: https://api.cloudflare.com/#zone-plan-available-plans
func (api *API) AvailableZoneRatePlans(ctx context.Context, zoneID string) ([]ZoneRatePlan, error) {
	uri := fmt.Sprintf("/zones/%s/available_rate_plans", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []ZoneRatePlan{}, err
	}
	var r AvailableZoneRatePlansResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []ZoneRatePlan{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// AvailableZonePlans returns information about all plans available to the specified zone.
//
// API reference: https://api.cloudflare.com/#zone-rate-plan-list-available-plans
func (api *API) AvailableZonePlans(ctx context.Context, zoneID string) ([]ZonePlan, error) {
	uri := fmt.Sprintf("/zones/%s/available_plans", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []ZonePlan{}, err
	}
	var r AvailableZonePlansResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []ZonePlan{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// encode encodes non-nil fields into URL encoded form.
func (o ZoneAnalyticsOptions) encode() string {
	v := url.Values{}
	if o.Since != nil {
		v.Set("since", (*o.Since).Format(time.RFC3339))
	}
	if o.Until != nil {
		v.Set("until", (*o.Until).Format(time.RFC3339))
	}
	if o.Continuous != nil {
		v.Set("continuous", fmt.Sprintf("%t", *o.Continuous))
	}
	return v.Encode()
}

// ZoneAnalyticsDashboard returns zone analytics information.
//
// API reference: https://api.cloudflare.com/#zone-analytics-dashboard
func (api *API) ZoneAnalyticsDashboard(ctx context.Context, zoneID string, options ZoneAnalyticsOptions) (ZoneAnalyticsData, error) {
	uri := fmt.Sprintf("/zones/%s/analytics/dashboard?%s", zoneID, options.encode())
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return ZoneAnalyticsData{}, err
	}
	var r zoneAnalyticsDataResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return ZoneAnalyticsData{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ZoneAnalyticsByColocation returns zone analytics information by datacenter.
//
// API reference: https://api.cloudflare.com/#zone-analytics-analytics-by-co-locations
func (api *API) ZoneAnalyticsByColocation(ctx context.Context, zoneID string, options ZoneAnalyticsOptions) ([]ZoneAnalyticsColocation, error) {
	uri := fmt.Sprintf("/zones/%s/analytics/colos?%s", zoneID, options.encode())
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r zoneAnalyticsColocationResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ZoneSettings returns all of the settings for a given zone.
//
// API reference: https://api.cloudflare.com/#zone-settings-get-all-zone-settings
func (api *API) ZoneSettings(ctx context.Context, zoneID string) (*ZoneSettingResponse, error) {
	uri := fmt.Sprintf("/zones/%s/settings", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	response := &ZoneSettingResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// UpdateZoneSettings updates the settings for a given zone.
//
// API reference: https://api.cloudflare.com/#zone-settings-edit-zone-settings-info
func (api *API) UpdateZoneSettings(ctx context.Context, zoneID string, settings []ZoneSetting) (*ZoneSettingResponse, error) {
	uri := fmt.Sprintf("/zones/%s/settings", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, struct {
		Items []ZoneSetting `json:"items"`
	}{settings})
	if err != nil {
		return nil, err
	}

	response := &ZoneSettingResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// ZoneSSLSettings returns information about SSL setting to the specified zone.
//
// API reference: https://api.cloudflare.com/#zone-settings-get-ssl-setting
func (api *API) ZoneSSLSettings(ctx context.Context, zoneID string) (ZoneSSLSetting, error) {
	uri := fmt.Sprintf("/zones/%s/settings/ssl", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return ZoneSSLSetting{}, err
	}
	var r ZoneSSLSettingResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return ZoneSSLSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdateZoneSSLSettings update information about SSL setting to the specified zone.
//
// API reference: https://api.cloudflare.com/#zone-settings-change-ssl-setting
func (api *API) UpdateZoneSSLSettings(ctx context.Context, zoneID string, sslValue string) (ZoneSSLSetting, error) {
	uri := fmt.Sprintf("/zones/%s/settings/ssl", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, ZoneSSLSetting{Value: sslValue})
	if err != nil {
		return ZoneSSLSetting{}, err
	}
	var r ZoneSSLSettingResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return ZoneSSLSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// FallbackOrigin returns information about the fallback origin for the specified zone.
//
// API reference: https://developers.cloudflare.com/ssl/ssl-for-saas/api-calls/#fallback-origin-configuration
func (api *API) FallbackOrigin(ctx context.Context, zoneID string) (FallbackOrigin, error) {
	uri := fmt.Sprintf("/zones/%s/fallback_origin", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return FallbackOrigin{}, err
	}

	var r FallbackOriginResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return FallbackOrigin{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}

// UpdateFallbackOrigin updates the fallback origin for a given zone.
//
// API reference: https://developers.cloudflare.com/ssl/ssl-for-saas/api-calls/#4-example-patch-to-change-fallback-origin
func (api *API) UpdateFallbackOrigin(ctx context.Context, zoneID string, fbo FallbackOrigin) (*FallbackOriginResponse, error) {
	uri := fmt.Sprintf("/zones/%s/fallback_origin", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, fbo)
	if err != nil {
		return nil, err
	}

	response := &FallbackOriginResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// normalizeZoneName tries to convert IDNs (international domain names)
// from Punycode to Unicode form. If the given zone name is not represented
// as Punycode, or converting fails (for invalid representations), it
// is returned unchanged.
//
// Because all the zone name comparison is currently done using the API service
// (except for comparison with the empty string), theoretically, we could
// remove this function from the Go library. However, there should be no harm
// calling this function other than gelable performance penalty.
//
// Note: conversion errors are silently discarded.
func normalizeZoneName(name string) string {
	if n, err := idna.ToUnicode(name); err == nil {
		return n
	}
	return name
}

// ZoneSingleSetting returns information about specified setting to the specified zone.
//
// API reference: https://api.cloudflare.com/#zone-settings-get-all-zone-settings
func (api *API) ZoneSingleSetting(ctx context.Context, zoneID, settingName string) (ZoneSetting, error) {
	uri := fmt.Sprintf("/zones/%s/settings/%s", zoneID, settingName)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return ZoneSetting{}, err
	}
	var r ZoneSettingSingleResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return ZoneSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdateZoneSingleSetting updates the specified setting for a given zone.
//
// API reference: https://api.cloudflare.com/#zone-settings-edit-zone-settings-info
func (api *API) UpdateZoneSingleSetting(ctx context.Context, zoneID, settingName string, setting ZoneSetting) (*ZoneSettingSingleResponse, error) {
	uri := fmt.Sprintf("/zones/%s/settings/%s", zoneID, settingName)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, setting)
	if err != nil {
		return nil, err
	}

	response := &ZoneSettingSingleResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// ZoneExport returns the text BIND config for the given zone
//
// API reference: https://api.cloudflare.com/#dns-records-for-a-zone-export-dns-records
func (api *API) ZoneExport(ctx context.Context, zoneID string) (string, error) {
	res, err := api.makeRequestContext(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records/export", nil)
	if err != nil {
		return "", err
	}
	return string(res), nil
}

// ZoneDNSSECResponse represents the response from the Zone DNSSEC Setting
type ZoneDNSSECResponse struct {
	Response
	Result ZoneDNSSEC `json:"result"`
}

// ZoneDNSSEC represents the response from the Zone DNSSEC Setting result
type ZoneDNSSEC struct {
	Status          string    `json:"status"`
	Flags           int       `json:"flags"`
	Algorithm       string    `json:"algorithm"`
	KeyType         string    `json:"key_type"`
	DigestType      string    `json:"digest_type"`
	DigestAlgorithm string    `json:"digest_algorithm"`
	Digest          string    `json:"digest"`
	DS              string    `json:"ds"`
	KeyTag          int       `json:"key_tag"`
	PublicKey       string    `json:"public_key"`
	ModifiedOn      time.Time `json:"modified_on"`
}

// ZoneDNSSECSetting returns the DNSSEC details of a zone
//
// API reference: https://api.cloudflare.com/#dnssec-dnssec-details
func (api *API) ZoneDNSSECSetting(ctx context.Context, zoneID string) (ZoneDNSSEC, error) {
	res, err := api.makeRequestContext(ctx, http.MethodGet, "/zones/"+zoneID+"/dnssec", nil)
	if err != nil {
		return ZoneDNSSEC{}, err
	}
	response := ZoneDNSSECResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return ZoneDNSSEC{}, errors.Wrap(err, errUnmarshalError)
	}

	return response.Result, nil
}

// ZoneDNSSECDeleteResponse represents the response from the Zone DNSSEC Delete request
type ZoneDNSSECDeleteResponse struct {
	Response
	Result string `json:"result"`
}

// DeleteZoneDNSSEC deletes DNSSEC for zone
//
// API reference: https://api.cloudflare.com/#dnssec-delete-dnssec-records
func (api *API) DeleteZoneDNSSEC(ctx context.Context, zoneID string) (string, error) {
	res, err := api.makeRequestContext(ctx, http.MethodDelete, "/zones/"+zoneID+"/dnssec", nil)
	if err != nil {
		return "", err
	}
	response := ZoneDNSSECDeleteResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return "", errors.Wrap(err, errUnmarshalError)
	}
	return response.Result, nil
}

// ZoneDNSSECUpdateOptions represents the options for DNSSEC update
type ZoneDNSSECUpdateOptions struct {
	Status string `json:"status"`
}

// UpdateZoneDNSSEC updates DNSSEC for a zone
//
// API reference: https://api.cloudflare.com/#dnssec-edit-dnssec-status
func (api *API) UpdateZoneDNSSEC(ctx context.Context, zoneID string, options ZoneDNSSECUpdateOptions) (ZoneDNSSEC, error) {
	res, err := api.makeRequestContext(ctx, http.MethodPatch, "/zones/"+zoneID+"/dnssec", options)
	if err != nil {
		return ZoneDNSSEC{}, err
	}
	response := ZoneDNSSECResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return ZoneDNSSEC{}, errors.Wrap(err, errUnmarshalError)
	}
	return response.Result, nil
}
