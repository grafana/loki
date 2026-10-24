package godo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-querystring/query"
)

const (
	insightsNotificationChannelsPath = "/v2/insights/notification-channels"
	insightsAlertRulesPath           = "/v2/insights/alert-rules"
	insightsAlertInstancesPath       = "/v2/insights/alert-instances"
	insightsResourcesPath            = "/v2/insights/resources"
)

// Insights resource list sort fields.
const (
	InsightsResourceSortResourceURN = "resource_urn"
	InsightsResourceSortEffectiveAt = "effective_at"
)

// Insights resource list sort directions.
const (
	InsightsResourceSortAscending  = "asc"
	InsightsResourceSortDescending = "desc"
)

// Insights notification channel types. These are the protobuf enum values the
// API returns on read; create and update select the type by which config
// object is set.
const (
	InsightsChannelTypeEmail   = "CHANNEL_TYPE_EMAIL"
	InsightsChannelTypeSlack   = "CHANNEL_TYPE_SLACK"
	InsightsChannelTypeWebhook = "CHANNEL_TYPE_WEBHOOK"
)

// Insights alert rule statuses.
const (
	InsightsAlertRuleStatusActive = "ALERT_RULE_STATUS_ACTIVE"
	InsightsAlertRuleStatusPaused = "ALERT_RULE_STATUS_PAUSED"
)

// Insights alert evaluation windows.
const (
	InsightsEvaluationWindow1m  = "EVALUATION_WINDOW_1M"
	InsightsEvaluationWindow5m  = "EVALUATION_WINDOW_5M"
	InsightsEvaluationWindow10m = "EVALUATION_WINDOW_10M"
	InsightsEvaluationWindow15m = "EVALUATION_WINDOW_15M"
	InsightsEvaluationWindow30m = "EVALUATION_WINDOW_30M"
	InsightsEvaluationWindow1h  = "EVALUATION_WINDOW_1H"
)

// Insights threshold operators.
const (
	InsightsThresholdOperatorEqual              = "THRESHOLD_OPERATOR_EQUAL"
	InsightsThresholdOperatorLessThanOrEqual    = "THRESHOLD_OPERATOR_LESS_THAN_OR_EQUAL"
	InsightsThresholdOperatorLessThan           = "THRESHOLD_OPERATOR_LESS_THAN"
	InsightsThresholdOperatorGreaterThan        = "THRESHOLD_OPERATOR_GREATER_THAN"
	InsightsThresholdOperatorGreaterThanOrEqual = "THRESHOLD_OPERATOR_GREATER_THAN_OR_EQUAL"
	InsightsThresholdOperatorNotEqual           = "THRESHOLD_OPERATOR_NOT_EQUAL"
)

// Insights metrics-query filter operators.
const (
	InsightsFilterOperatorEqual              = "FILTER_OPERATOR_EQUAL"
	InsightsFilterOperatorNotEqual           = "FILTER_OPERATOR_NOT_EQUAL"
	InsightsFilterOperatorLessThan           = "FILTER_OPERATOR_LESS_THAN"
	InsightsFilterOperatorLessThanOrEqual    = "FILTER_OPERATOR_LESS_THAN_OR_EQUAL"
	InsightsFilterOperatorGreaterThan        = "FILTER_OPERATOR_GREATER_THAN"
	InsightsFilterOperatorGreaterThanOrEqual = "FILTER_OPERATOR_GREATER_THAN_OR_EQUAL"
)

// Insights re-alert durations.
const (
	InsightsReAlertDuration30m   = "RE_ALERT_DURATION_30M"
	InsightsReAlertDuration1h    = "RE_ALERT_DURATION_1H"
	InsightsReAlertDuration4h    = "RE_ALERT_DURATION_4H"
	InsightsReAlertDurationNever = "RE_ALERT_DURATION_NEVER"
)

// Insights severities used when binding a notification channel to an alert rule.
const (
	InsightsSeverityWarning  = "SEVERITY_WARNING"
	InsightsSeverityCritical = "SEVERITY_CRITICAL"
)

// Insights alert instance statuses. These are lowercase, unlike the protobuf
// enums used by alert rules and notification channels.
const (
	InsightsAlertInstanceStatusActive   = "active"
	InsightsAlertInstanceStatusResolved = "resolved"
)

// Insights alert instance severities.
const (
	InsightsAlertInstanceSeverityWarning  = "warning"
	InsightsAlertInstanceSeverityCritical = "critical"
)

// Insights logs filter operators.
const (
	InsightsLogsFilterOperatorEqual              = "FILTER_OPERATOR_EQ"
	InsightsLogsFilterOperatorNotEqual           = "FILTER_OPERATOR_NEQ"
	InsightsLogsFilterOperatorIn                 = "FILTER_OPERATOR_IN"
	InsightsLogsFilterOperatorExists             = "FILTER_OPERATOR_EXISTS"
	InsightsLogsFilterOperatorGreaterThanOrEqual = "FILTER_OPERATOR_GTE"
	InsightsLogsFilterOperatorLessThanOrEqual    = "FILTER_OPERATOR_LTE"
)

// Insights logs field scopes.
const (
	InsightsLogsFieldScopeResource   = "FIELD_SCOPE_RESOURCE"
	InsightsLogsFieldScopeAttributes = "FIELD_SCOPE_ATTRIBUTES"
)

// Insights logs sort directions.
const (
	InsightsLogsSortDirectionAscending  = "SORT_DIRECTION_ASC"
	InsightsLogsSortDirectionDescending = "SORT_DIRECTION_DESC"
)

// Insights spans filter node types.
// Deprecated: filter nodes are selected by setting Condition, And, Or, Not, or
// TextSearch on SpansFilterExpression.
const (
	InsightsSpansFilterTypeCondition  = "condition"
	InsightsSpansFilterTypeAnd        = "and"
	InsightsSpansFilterTypeOr         = "or"
	InsightsSpansFilterTypeNot        = "not"
	InsightsSpansFilterTypeTextSearch = "text_search"
)

// Insights spans filter condition operators.
const (
	InsightsSpansFilterOperatorEqual              = "FILTER_OPERATOR_EQ"
	InsightsSpansFilterOperatorNotEqual           = "FILTER_OPERATOR_NEQ"
	InsightsSpansFilterOperatorGreaterThanOrEqual = "FILTER_OPERATOR_GTE"
	InsightsSpansFilterOperatorLessThanOrEqual    = "FILTER_OPERATOR_LTE"
	InsightsSpansFilterOperatorIn                 = "FILTER_OPERATOR_IN"
	InsightsSpansFilterOperatorExists             = "FILTER_OPERATOR_EXISTS"
)

// InsightsSpansFilterOperatorGreaterThan is retained for source compatibility.
// Deprecated: the spans API does not support a strict greater-than operator.
const InsightsSpansFilterOperatorGreaterThan = "gt"

// InsightsSpansFilterOperatorLessThan is retained for source compatibility.
// Deprecated: the spans API does not support a strict less-than operator.
const InsightsSpansFilterOperatorLessThan = "lt"

// Insights spans field scopes. Omit the scope for well-known span fields or to
// search both attribute maps.
const (
	InsightsSpansFieldScopeResource   = "FIELD_SCOPE_RESOURCE"
	InsightsSpansFieldScopeAttributes = "FIELD_SCOPE_ATTRIBUTES"
)

// Insights spans sort directions.
const (
	InsightsSpansSortDirectionAscending  = "asc"
	InsightsSpansSortDirectionDescending = "desc"
)

// Insights spans filter value types.
// Deprecated: these identify the pre-COBS-1725 typed-wrapper representation
// accepted only by SpansFilterValue.UnmarshalJSON.
const (
	InsightsSpansValueTypeString      = "string"
	InsightsSpansValueTypeNumber      = "number"
	InsightsSpansValueTypeBool        = "bool"
	InsightsSpansValueTypeStringArray = "string_array"
	InsightsSpansValueTypeNumberArray = "number_array"
)

// Insights traces filter node types.
// Deprecated: filter nodes are selected by setting Condition, And, Or, Not, or
// TextSearch on TracesFilterExpression.
const (
	InsightsTracesFilterTypeCondition  = "condition"
	InsightsTracesFilterTypeAnd        = "and"
	InsightsTracesFilterTypeOr         = "or"
	InsightsTracesFilterTypeNot        = "not"
	InsightsTracesFilterTypeTextSearch = "text_search"
)

// Insights traces filter condition operators.
const (
	InsightsTracesFilterOperatorEqual              = "FILTER_OPERATOR_EQ"
	InsightsTracesFilterOperatorNotEqual           = "FILTER_OPERATOR_NEQ"
	InsightsTracesFilterOperatorGreaterThanOrEqual = "FILTER_OPERATOR_GTE"
	InsightsTracesFilterOperatorLessThanOrEqual    = "FILTER_OPERATOR_LTE"
	InsightsTracesFilterOperatorIn                 = "FILTER_OPERATOR_IN"
	InsightsTracesFilterOperatorExists             = "FILTER_OPERATOR_EXISTS"
)

// InsightsTracesFilterOperatorGreaterThan is retained for source compatibility.
// Deprecated: the traces API does not support a strict greater-than operator.
const InsightsTracesFilterOperatorGreaterThan = "gt"

// InsightsTracesFilterOperatorLessThan is retained for source compatibility.
// Deprecated: the traces API does not support a strict less-than operator.
const InsightsTracesFilterOperatorLessThan = "lt"

// Insights traces field scopes. Omit the scope for well-known span fields or to
// look up a span attribute with a resource attribute fallback.
const (
	InsightsTracesFieldScopeResource   = "FIELD_SCOPE_RESOURCE"
	InsightsTracesFieldScopeAttributes = "FIELD_SCOPE_ATTRIBUTES"
)

// Insights traces sort directions.
const (
	InsightsTracesSortDirectionAscending  = "asc"
	InsightsTracesSortDirectionDescending = "desc"
)

// Insights traces filter value types.
// Deprecated: these identify the pre-COBS-1725 typed-wrapper representation
// accepted only by TracesFilterValue.UnmarshalJSON.
const (
	InsightsTracesValueTypeString      = "string"
	InsightsTracesValueTypeNumber      = "number"
	InsightsTracesValueTypeBool        = "bool"
	InsightsTracesValueTypeStringArray = "string_array"
	InsightsTracesValueTypeNumberArray = "number_array"
)

// Insights trace statuses, rolled up from the status codes of a trace's spans.
const (
	InsightsTraceStatusOk    = "Ok"
	InsightsTraceStatusError = "Error"
)

// InsightsService manages Insights notification channels, alert rules, alert
// instances, PromQL queries, logs searches, spans searches, traces queries,
// and per-resource telemetry signal enablement.
type InsightsService interface {
	ListNotificationChannels(context.Context, *ListOptions) ([]NotificationChannel, *Response, error)
	GetNotificationChannel(context.Context, string) (*NotificationChannel, *Response, error)
	CreateNotificationChannel(context.Context, *NotificationChannelRequest) (*NotificationChannel, *Response, error)
	UpdateNotificationChannel(context.Context, string, *NotificationChannelRequest) (*NotificationChannel, *Response, error)
	DeleteNotificationChannel(context.Context, string) (*Response, error)

	ListAlertRules(context.Context, *AlertRuleListOptions) ([]AlertRule, *Response, error)
	GetAlertRule(context.Context, string) (*AlertRule, *Response, error)
	CreateAlertRule(context.Context, *AlertRuleRequest) (*AlertRule, *Response, error)
	UpdateAlertRule(context.Context, string, *AlertRuleRequest) (*AlertRule, *Response, error)
	DeleteAlertRule(context.Context, string) (*Response, error)

	ListAlertInstances(context.Context, *AlertInstanceListOptions) ([]AlertInstance, *Response, error)
	GetAlertInstance(context.Context, string) (*AlertInstance, *Response, error)

	// Query evaluates a PromQL expression at one timestamp (GET).
	Query(context.Context, string, *PromQueryOptions) (*PromQueryResponse, *Response, error)
	// PostQuery is the form-encoded POST form of Query, for expressions that do not fit in a URL.
	PostQuery(context.Context, string, *PromQueryOptions) (*PromQueryResponse, *Response, error)
	QueryRange(context.Context, string, *PromQueryRangeOptions) (*PromQueryRangeResponse, *Response, error)
	PostQueryRange(context.Context, string, *PromQueryRangeOptions) (*PromQueryRangeResponse, *Response, error)
	Series(context.Context, string, *PromSelectorOptions) (*PromSeriesResponse, *Response, error)
	PostSeries(context.Context, string, *PromSelectorOptions) (*PromSeriesResponse, *Response, error)
	Labels(context.Context, string, *PromSelectorOptions) (*PromLabelsResponse, *Response, error)
	PostLabels(context.Context, string, *PromSelectorOptions) (*PromLabelsResponse, *Response, error)
	LabelValues(context.Context, string, string, *PromSelectorOptions) (*PromLabelsResponse, *Response, error)

	SearchLogs(context.Context, string, *LogsSearchRequest) (*LogsSearchResponse, *Response, error)

	SearchSpans(context.Context, string, *SpansSearchRequest) (*SpansSearchResponse, *Response, error)

	SearchTraces(context.Context, string, *TracesSearchRequest) (*TracesSearchResponse, *Response, error)
	GetTrace(context.Context, string, string, *TraceGetOptions) (*Trace, *Response, error)

	ListResources(context.Context, *InsightsResourceListOptions) (*InsightsResourceList, *Response, error)
	GetResource(context.Context, string) (*InsightsResource, *Response, error)
	UpdateResourceSignals(context.Context, string, *InsightsResourceSignalsRequest) (*InsightsResource, *Response, error)
}

// InsightsServiceOp handles communication with Insights methods of the DigitalOcean API.
type InsightsServiceOp struct {
	client *Client
}

var _ InsightsService = &InsightsServiceOp{}

// NotificationChannel is a reusable Insights notification destination.
type NotificationChannel struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	ChannelType string                     `json:"channel_type"`
	Email       *EmailNotificationConfig   `json:"email,omitempty"`
	Slack       *SlackNotificationConfig   `json:"slack,omitempty"`
	Webhook     *WebhookNotificationConfig `json:"webhook,omitempty"`
	Usage       *NotificationChannelUsage  `json:"usage,omitempty"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
}

// NotificationChannelRequest is the body for creating or updating a notification channel.
// Set exactly one of Email, Slack, or Webhook. Secret fields are write-only:
// send the full value to set or rotate one, and leave it empty on update to keep the current secret.
type NotificationChannelRequest struct {
	Name    string                     `json:"name"`
	Email   *EmailNotificationConfig   `json:"email,omitempty"`
	Slack   *SlackNotificationConfig   `json:"slack,omitempty"`
	Webhook *WebhookNotificationConfig `json:"webhook,omitempty"`
}

// EmailNotificationConfig is an email notification channel.
// To is one or more verified team-member addresses separated by commas, semicolons, or spaces.
type EmailNotificationConfig struct {
	To string `json:"to"`
}

// SlackNotificationConfig is a Slack notification channel.
// WebhookURL is write-only: reads return a masked value, and an empty WebhookURL on update keeps the existing URL.
type SlackNotificationConfig struct {
	WebhookURL string `json:"webhook_url,omitempty"`
	Channel    string `json:"channel"`
}

// WebhookNotificationConfig is an HTTPS webhook notification channel.
// URL is returned in full on read. Credential fields are write-only and masked on read;
// leave a secret empty on update to keep the existing value. Set BasicAuth or BearerToken, not both.
type WebhookNotificationConfig struct {
	URL         string                  `json:"url"`
	BasicAuth   *WebhookBasicAuth       `json:"basic_auth,omitempty"`
	BearerToken *WebhookBearerToken     `json:"bearer_token,omitempty"`
	Headers     map[string]string       `json:"headers,omitempty"`
	Signature   *WebhookSignatureConfig `json:"signature,omitempty"`
}

// WebhookBasicAuth is HTTP basic auth for a webhook. Password is write-only.
type WebhookBasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

// WebhookBearerToken is bearer auth for a webhook. Token is write-only.
type WebhookBearerToken struct {
	Token string `json:"token,omitempty"`
}

// WebhookSignatureConfig is the HMAC signing secret for a webhook. Secret is write-only.
type WebhookSignatureConfig struct {
	Secret string `json:"secret,omitempty"`
}

// NotificationChannelUsage counts alert rules that reference a channel.
type NotificationChannelUsage struct {
	RuleCount int `json:"rule_count"`
}

type notificationChannelsRoot struct {
	NotificationChannels []NotificationChannel `json:"notification_channels"`
	Links                *Links                `json:"links"`
	Meta                 *Meta                 `json:"meta"`
}

type notificationChannelRoot struct {
	NotificationChannel *NotificationChannel `json:"notification_channel"`
}

// ListNotificationChannels lists notification channels.
func (s *InsightsServiceOp) ListNotificationChannels(ctx context.Context, opt *ListOptions) ([]NotificationChannel, *Response, error) {
	path, err := addOptions(insightsNotificationChannelsPath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(notificationChannelsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	setListPaging(resp, root.Links, root.Meta)
	return root.NotificationChannels, resp, nil
}

// GetNotificationChannel retrieves a notification channel. Secret fields are masked.
func (s *InsightsServiceOp) GetNotificationChannel(ctx context.Context, id string) (*NotificationChannel, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, insightsResourcePath(insightsNotificationChannelsPath, id), nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(notificationChannelRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.NotificationChannel, resp, nil
}

// CreateNotificationChannel creates a notification channel.
func (s *InsightsServiceOp) CreateNotificationChannel(ctx context.Context, create *NotificationChannelRequest) (*NotificationChannel, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, insightsNotificationChannelsPath, create)
	if err != nil {
		return nil, nil, err
	}

	root := new(notificationChannelRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.NotificationChannel, resp, nil
}

// UpdateNotificationChannel replaces a notification channel's name and config.
func (s *InsightsServiceOp) UpdateNotificationChannel(ctx context.Context, id string, update *NotificationChannelRequest) (*NotificationChannel, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPut, insightsResourcePath(insightsNotificationChannelsPath, id), update)
	if err != nil {
		return nil, nil, err
	}

	root := new(notificationChannelRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.NotificationChannel, resp, nil
}

// DeleteNotificationChannel deletes a notification channel.
// Deleting a channel that alert rules still reference returns 409 Conflict.
func (s *InsightsServiceOp) DeleteNotificationChannel(ctx context.Context, id string) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodDelete, insightsResourcePath(insightsNotificationChannelsPath, id), nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

// AlertRuleListOptions are filters for listing alert rules.
type AlertRuleListOptions struct {
	Page        int    `url:"page,omitempty"`
	PerPage     int    `url:"per_page,omitempty"`
	ResourceURN string `url:"resource_urn,omitempty"`
}

// AlertRule is an Insights alert rule.
type AlertRule struct {
	ID        string        `json:"id"`
	Spec      AlertRuleSpec `json:"spec"`
	Status    string        `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// AlertRuleRequest is the body for creating or updating an alert rule.
// Status is optional: create defaults to active, and update keeps the current status when empty.
type AlertRuleRequest struct {
	Spec   AlertRuleSpec `json:"spec"`
	Status string        `json:"status,omitempty"`
}

// AlertRuleSpec is the configuration of an alert rule.
type AlertRuleSpec struct {
	Name       string            `json:"name"`
	Query      AlertMetricsQuery `json:"query"`
	Condition  *AlertCondition   `json:"condition,omitempty"`
	Thresholds AlertThresholds   `json:"thresholds"`
	// NotificationChannels are notified when the rule fires.
	// On create, set a non-empty list. On update, leave nil to keep existing bindings.
	// A pointer to an empty slice is sent as [] and the API rejects it.
	NotificationChannels *[]NotificationChannelBinding `json:"notification_channels,omitempty"`
	ReAlertDuration      string                        `json:"re_alert_duration,omitempty"`
}

// AlertCondition is the evaluation window for an alert rule.
type AlertCondition struct {
	Window string `json:"window,omitempty"`
}

// AlertThresholds is the comparison applied to the aggregated metric.
// At least one of Warning or Critical must be set. Use a pointer so a threshold of 0 is sent.
type AlertThresholds struct {
	Warning  *float64 `json:"warning,omitempty"`
	Critical *float64 `json:"critical,omitempty"`
	Operator string   `json:"operator"`
}

// AlertMetricsQuery is the metric an alert rule evaluates.
// Metric must be a dotted OpenTelemetry name such as do.droplets.cpu_utilization.
type AlertMetricsQuery struct {
	Metric       string             `json:"metric"`
	Filters      []AlertQueryFilter `json:"filters,omitempty"`
	ResourceURNs []string           `json:"resource_urns,omitempty"`
	Tags         []string           `json:"tags,omitempty"`
}

// AlertQueryFilter is a label filter on an alert metrics query.
type AlertQueryFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// NotificationChannelBinding attaches a notification channel to an alert rule.
// An empty NotifyOn list notifies on every severity.
type NotificationChannelBinding struct {
	NotificationChannelID string   `json:"notification_channel_id"`
	NotifyOn              []string `json:"notify_on,omitempty"`
}

type alertRulesRoot struct {
	AlertRules []AlertRule `json:"alert_rules"`
	Links      *Links      `json:"links"`
	Meta       *Meta       `json:"meta"`
}

type alertRuleRoot struct {
	AlertRule *AlertRule `json:"alert_rule"`
}

// ListAlertRules lists alert rules.
func (s *InsightsServiceOp) ListAlertRules(ctx context.Context, opt *AlertRuleListOptions) ([]AlertRule, *Response, error) {
	path, err := addOptions(insightsAlertRulesPath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertRulesRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	setListPaging(resp, root.Links, root.Meta)
	return root.AlertRules, resp, nil
}

// GetAlertRule retrieves an alert rule.
func (s *InsightsServiceOp) GetAlertRule(ctx context.Context, id string) (*AlertRule, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, insightsResourcePath(insightsAlertRulesPath, id), nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertRuleRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.AlertRule, resp, nil
}

// CreateAlertRule creates an alert rule.
func (s *InsightsServiceOp) CreateAlertRule(ctx context.Context, create *AlertRuleRequest) (*AlertRule, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, insightsAlertRulesPath, create)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertRuleRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.AlertRule, resp, nil
}

// UpdateAlertRule updates an alert rule. Omitted status, re-alert duration, and
// notification channel bindings are left unchanged.
func (s *InsightsServiceOp) UpdateAlertRule(ctx context.Context, id string, update *AlertRuleRequest) (*AlertRule, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPut, insightsResourcePath(insightsAlertRulesPath, id), update)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertRuleRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.AlertRule, resp, nil
}

// DeleteAlertRule deletes an alert rule.
func (s *InsightsServiceOp) DeleteAlertRule(ctx context.Context, id string) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodDelete, insightsResourcePath(insightsAlertRulesPath, id), nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

// AlertInstanceListOptions are filters for listing alert instances.
type AlertInstanceListOptions struct {
	Page        int    `url:"page,omitempty"`
	PerPage     int    `url:"per_page,omitempty"`
	Status      string `url:"status,omitempty"`
	RuleID      string `url:"rule_id,omitempty"`
	ResourceURN string `url:"resource_urn,omitempty"`
}

// AlertInstance is a read-only firing of an alert rule against a resource.
type AlertInstance struct {
	ID              string     `json:"id"`
	RuleID          string     `json:"rule_id"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	ResourceURN     string     `json:"resource_urn,omitempty"`
	Value           float64    `json:"value"`
	TriggeredAt     time.Time  `json:"triggered_at"`
	LastTriggeredAt time.Time  `json:"last_triggered_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	LastNotifiedAt  *time.Time `json:"last_notified_at,omitempty"`
}

type alertInstancesRoot struct {
	AlertInstances []AlertInstance `json:"alert_instances"`
	Links          *Links          `json:"links"`
	Meta           *Meta           `json:"meta"`
}

type alertInstanceRoot struct {
	AlertInstance *AlertInstance `json:"alert_instance"`
}

// ListAlertInstances lists alert instances, newest trigger first.
func (s *InsightsServiceOp) ListAlertInstances(ctx context.Context, opt *AlertInstanceListOptions) ([]AlertInstance, *Response, error) {
	path, err := addOptions(insightsAlertInstancesPath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertInstancesRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	setListPaging(resp, root.Links, root.Meta)
	return root.AlertInstances, resp, nil
}

// GetAlertInstance retrieves an alert instance.
func (s *InsightsServiceOp) GetAlertInstance(ctx context.Context, id string) (*AlertInstance, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, insightsResourcePath(insightsAlertInstancesPath, id), nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertInstanceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.AlertInstance, resp, nil
}

// LogsSearchRequest describes an Insights logs search. TimeRange must not
// exceed seven days.
type LogsSearchRequest struct {
	TimeRange  LogsTimeRange          `json:"time_range"`
	Filter     *LogsFilterExpression  `json:"filter,omitempty"`
	OrderBy    []LogsOrderBy          `json:"order_by,omitempty"`
	Pagination *LogsPaginationRequest `json:"pagination,omitempty"`
}

// LogsTimeRange is an inclusive logs query time window.
type LogsTimeRange struct {
	From LogsTimeInstant `json:"from"`
	To   LogsTimeInstant `json:"to"`
}

// LogsTimeInstant identifies a point in time. Set exactly one of Absolute,
// Relative, or UnixNano.
type LogsTimeInstant struct {
	Absolute string `json:"absolute,omitempty"`
	Relative string `json:"relative,omitempty"`
	UnixNano string `json:"unix_nano,omitempty"`
}

// LogsFilterExpression is a boolean filter tree. Set exactly one expression
// node.
type LogsFilterExpression struct {
	Condition  *LogsFilterCondition  `json:"condition,omitempty"`
	And        *LogsFilterGroup      `json:"and,omitempty"`
	Or         *LogsFilterGroup      `json:"or,omitempty"`
	Not        *LogsFilterExpression `json:"not,omitempty"`
	TextSearch *LogsTextSearch       `json:"text_search,omitempty"`
}

// LogsFilterGroup combines nested logs filter expressions.
type LogsFilterGroup struct {
	Expressions []LogsFilterExpression `json:"expressions"`
}

// LogsTextSearch searches for a substring across the log body, service name,
// and resource URN.
type LogsTextSearch struct {
	Query string `json:"query"`
}

// LogsFilterCondition is a single field comparison.
type LogsFilterCondition struct {
	Field    LogsFieldRef     `json:"field"`
	Operator string           `json:"operator"`
	Value    *LogsFilterValue `json:"value,omitempty"`
}

// LogsFieldRef identifies an intrinsic field or an attribute.
type LogsFieldRef struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"`
}

// LogsFilterValue is a typed filter literal. Set exactly one value.
// Scalar fields use pointers so zero, false, and an empty string can be sent.
type LogsFilterValue struct {
	StringValue      *string               `json:"string_value,omitempty"`
	NumberValue      *float64              `json:"number_value,omitempty"`
	BoolValue        *bool                 `json:"bool_value,omitempty"`
	StringArrayValue *LogsStringArrayValue `json:"string_array_value,omitempty"`
	NumberArrayValue *LogsNumberArrayValue `json:"number_array_value,omitempty"`
}

// LogsStringArrayValue is a string-list filter value.
type LogsStringArrayValue struct {
	Values []string `json:"values"`
}

// LogsNumberArrayValue is a number-list filter value.
type LogsNumberArrayValue struct {
	Values []float64 `json:"values"`
}

// LogsOrderBy is a logs search sort clause. Cursor pagination is available
// only when ordering by timestamp alone.
type LogsOrderBy struct {
	Field     LogsFieldRef `json:"field"`
	Direction string       `json:"direction,omitempty"`
}

// LogsPaginationRequest configures cursor pagination for a logs search.
type LogsPaginationRequest struct {
	// Limit defaults to 100 and is clamped to 1000 by the API.
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// LogsSearchResponse contains matching log records and pagination state.
type LogsSearchResponse struct {
	Data       []InsightsLogRecord     `json:"data,omitempty"`
	Pagination *LogsPaginationResponse `json:"pagination,omitempty"`
}

// LogsPaginationResponse describes the next page of logs search results.
type LogsPaginationResponse struct {
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// InsightsLogRecord is a single log record returned by an Insights logs search.
type InsightsLogRecord struct {
	Timestamp      time.Time         `json:"timestamp"`
	SeverityNumber int32             `json:"severity_number,omitempty"`
	SeverityText   string            `json:"severity_text,omitempty"`
	Body           string            `json:"body,omitempty"`
	TraceID        string            `json:"trace_id,omitempty"`
	SpanID         string            `json:"span_id,omitempty"`
	ServiceName    string            `json:"service_name,omitempty"`
	Resource       map[string]string `json:"resource,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
}

// SearchLogs searches log records in a region. Cursor pagination requires
// ordering by timestamp alone; other sort orders return HasMore as false.
func (s *InsightsServiceOp) SearchLogs(ctx context.Context, region string, search *LogsSearchRequest) (*LogsSearchResponse, *Response, error) {
	path := "/v2/insights/query/" + url.PathEscape(region) + "/logs/search"
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, search)
	if err != nil {
		return nil, nil, err
	}

	result := new(LogsSearchResponse)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// PromQueryOptions are parameters for an instant PromQL query.
// Time and Timeout accept Prometheus timestamps and duration strings.
type PromQueryOptions struct {
	Query   string `url:"query"`
	Time    string `url:"time,omitempty"`
	Timeout string `url:"timeout,omitempty"`
}

// PromQueryRangeOptions are parameters for a range PromQL query.
type PromQueryRangeOptions struct {
	Query   string `url:"query"`
	Start   string `url:"start"`
	End     string `url:"end"`
	Step    string `url:"step"`
	Timeout string `url:"timeout,omitempty"`
}

// PromSelectorOptions are parameters for series, label, and label-value lookups.
// Match is repeated as match[] selectors.
type PromSelectorOptions struct {
	Match []string `url:"match,brackets,omitempty"`
	Start string   `url:"start,omitempty"`
	End   string   `url:"end,omitempty"`
}

// PromQueryResponse is a successful instant PromQL query.
// ResultType is vector, matrix, scalar, or string. Exactly one of Vector, Matrix, or Sample is set.
type PromQueryResponse struct {
	Status     string
	ResultType string
	Vector     []PromVectorSample
	Matrix     []PromMatrixSample
	Sample     *PromSample
}

// PromQueryRangeResponse is a successful range PromQL query. Data.ResultType is matrix.
type PromQueryRangeResponse struct {
	Status string           `json:"status"`
	Data   PromMatrixResult `json:"data"`
}

// PromMatrixResult is the data object of a range query.
type PromMatrixResult struct {
	ResultType string             `json:"resultType"`
	Result     []PromMatrixSample `json:"result"`
}

// PromVectorSample is one series from an instant vector result.
type PromVectorSample struct {
	Metric map[string]string `json:"metric"`
	Value  PromSample        `json:"value"`
}

// PromMatrixSample is one series from a range result.
type PromMatrixSample struct {
	Metric map[string]string `json:"metric"`
	Values []PromSample      `json:"values"`
}

// PromSample is a Prometheus sample pair [timestamp_seconds, value].
// Value is the raw string from the API, including non-numeric string results.
type PromSample struct {
	Timestamp float64
	Value     string
}

// PromSeriesResponse lists series matching one or more selectors.
type PromSeriesResponse struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
}

// PromLabelsResponse lists label names or the values of one label.
type PromLabelsResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

// UnmarshalJSON decodes a Prometheus instant-query envelope.
func (r *PromQueryResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Status = raw.Status
	r.ResultType = raw.Data.ResultType
	if len(raw.Data.Result) == 0 || string(raw.Data.Result) == "null" {
		return nil
	}
	switch raw.Data.ResultType {
	case "vector":
		return json.Unmarshal(raw.Data.Result, &r.Vector)
	case "matrix":
		return json.Unmarshal(raw.Data.Result, &r.Matrix)
	case "scalar", "string":
		r.Sample = &PromSample{}
		return json.Unmarshal(raw.Data.Result, r.Sample)
	default:
		return fmt.Errorf("godo: unknown PromQL result type %q", raw.Data.ResultType)
	}
}

// UnmarshalJSON decodes a Prometheus [timestamp, value] sample.
func (s *PromSample) UnmarshalJSON(data []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return fmt.Errorf("godo: prom sample has %d elements", len(pair))
	}
	if err := json.Unmarshal(pair[0], &s.Timestamp); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &s.Value)
}

// Query evaluates a PromQL expression at a single timestamp.
func (s *InsightsServiceOp) Query(ctx context.Context, region string, opt *PromQueryOptions) (*PromQueryResponse, *Response, error) {
	return s.promGet(ctx, insightsPromPath(region, "query"), opt)
}

// PostQuery evaluates a PromQL expression at a single timestamp using a form body.
func (s *InsightsServiceOp) PostQuery(ctx context.Context, region string, opt *PromQueryOptions) (*PromQueryResponse, *Response, error) {
	return s.promPost(ctx, insightsPromPath(region, "query"), opt)
}

// QueryRange evaluates a PromQL expression over a time range.
func (s *InsightsServiceOp) QueryRange(ctx context.Context, region string, opt *PromQueryRangeOptions) (*PromQueryRangeResponse, *Response, error) {
	out := new(PromQueryRangeResponse)
	resp, err := s.promDo(ctx, http.MethodGet, insightsPromPath(region, "query_range"), opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// PostQueryRange evaluates a PromQL expression over a time range using a form body.
func (s *InsightsServiceOp) PostQueryRange(ctx context.Context, region string, opt *PromQueryRangeOptions) (*PromQueryRangeResponse, *Response, error) {
	out := new(PromQueryRangeResponse)
	resp, err := s.promDo(ctx, http.MethodPost, insightsPromPath(region, "query_range"), opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Series finds series matching label selectors.
func (s *InsightsServiceOp) Series(ctx context.Context, region string, opt *PromSelectorOptions) (*PromSeriesResponse, *Response, error) {
	out := new(PromSeriesResponse)
	resp, err := s.promDo(ctx, http.MethodGet, insightsPromPath(region, "series"), opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// PostSeries finds series matching label selectors using a form body.
func (s *InsightsServiceOp) PostSeries(ctx context.Context, region string, opt *PromSelectorOptions) (*PromSeriesResponse, *Response, error) {
	out := new(PromSeriesResponse)
	resp, err := s.promDo(ctx, http.MethodPost, insightsPromPath(region, "series"), opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Labels lists label names.
func (s *InsightsServiceOp) Labels(ctx context.Context, region string, opt *PromSelectorOptions) (*PromLabelsResponse, *Response, error) {
	out := new(PromLabelsResponse)
	resp, err := s.promDo(ctx, http.MethodGet, insightsPromPath(region, "labels"), opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// PostLabels lists label names using a form body.
func (s *InsightsServiceOp) PostLabels(ctx context.Context, region string, opt *PromSelectorOptions) (*PromLabelsResponse, *Response, error) {
	out := new(PromLabelsResponse)
	resp, err := s.promDo(ctx, http.MethodPost, insightsPromPath(region, "labels"), opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// LabelValues lists values for a label name.
func (s *InsightsServiceOp) LabelValues(ctx context.Context, region, name string, opt *PromSelectorOptions) (*PromLabelsResponse, *Response, error) {
	out := new(PromLabelsResponse)
	path := fmt.Sprintf("/v2/insights/query/%s/prom/api/v1/label/%s/values", url.PathEscape(region), url.PathEscape(name))
	resp, err := s.promDo(ctx, http.MethodGet, path, opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *InsightsServiceOp) promGet(ctx context.Context, path string, opt interface{}) (*PromQueryResponse, *Response, error) {
	out := new(PromQueryResponse)
	resp, err := s.promDo(ctx, http.MethodGet, path, opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *InsightsServiceOp) promPost(ctx context.Context, path string, opt interface{}) (*PromQueryResponse, *Response, error) {
	out := new(PromQueryResponse)
	resp, err := s.promDo(ctx, http.MethodPost, path, opt, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *InsightsServiceOp) promDo(ctx context.Context, method, path string, opt, out interface{}) (*Response, error) {
	var (
		req *http.Request
		err error
	)
	switch method {
	case http.MethodGet:
		path, err = addOptions(path, opt)
		if err != nil {
			return nil, err
		}
		req, err = s.client.NewRequest(ctx, http.MethodGet, path, nil)
	case http.MethodPost:
		req, err = s.newFormRequest(ctx, path, opt)
	default:
		return nil, fmt.Errorf("godo: unsupported prom method %s", method)
	}
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, out)
}

func (s *InsightsServiceOp) newFormRequest(ctx context.Context, path string, opt interface{}) (*http.Request, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	encoded := ""
	if opt != nil {
		values, err := query.Values(opt)
		if err != nil {
			return nil, err
		}
		encoded = values.Encode()
	}
	req.Body = io.NopCloser(strings.NewReader(encoded))
	req.ContentLength = int64(len(encoded))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(encoded)), nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func insightsResourcePath(base, id string) string {
	return base + "/" + url.PathEscape(id)
}

func insightsPromPath(region, suffix string) string {
	return fmt.Sprintf("/v2/insights/query/%s/prom/api/v1/%s", url.PathEscape(region), suffix)
}

func setListPaging(resp *Response, links *Links, meta *Meta) {
	if resp == nil {
		return
	}
	if links != nil {
		resp.Links = links
	}
	if meta != nil {
		resp.Meta = meta
	}
}

// SpansSearchRequest describes an Insights spans search. TimeRange must not
// exceed seven days.
type SpansSearchRequest struct {
	TimeRange  SpansTimeRange          `json:"time_range"`
	Filter     *SpansFilterExpression  `json:"filter,omitempty"`
	OrderBy    []SpansOrderBy          `json:"order_by,omitempty"`
	Pagination *SpansPaginationRequest `json:"pagination,omitempty"`
}

// SpansTimeRange is an inclusive spans query time window.
type SpansTimeRange struct {
	From SpansTimeBound `json:"from"`
	To   SpansTimeBound `json:"to"`
}

// SpansTimeBound is a single time bound. Set exactly one of Relative or
// Absolute. Relative accepts "now", a bare lookback duration such as "1h" or
// "7d", or an offset such as "now-15m". Absolute accepts an RFC3339 string or a
// Unix timestamp encoded as a decimal string.
type SpansTimeBound struct {
	Relative string `json:"relative,omitempty"`
	Absolute string `json:"absolute,omitempty"`
}

// SpansFilterExpression is a boolean filter tree. Set exactly one expression
// node.
type SpansFilterExpression struct {
	Condition  *SpansFilterCondition  `json:"condition,omitempty"`
	And        *SpansFilterGroup      `json:"and,omitempty"`
	Or         *SpansFilterGroup      `json:"or,omitempty"`
	Not        *SpansFilterExpression `json:"not,omitempty"`
	TextSearch *SpansTextSearch       `json:"text_search,omitempty"`

	// Deprecated: use Condition, And, Or, Not, or TextSearch. These fields are
	// retained as source-compatible inputs and are never emitted directly.
	Type        string                  `json:"-"`
	Field       *SpansFilterField       `json:"-"`
	Operator    string                  `json:"-"`
	Value       *SpansFilterValue       `json:"-"`
	Expressions []SpansFilterExpression `json:"-"`
	Query       string                  `json:"-"`
}

// SpansFilterGroup combines nested spans filter expressions.
type SpansFilterGroup struct {
	Expressions []SpansFilterExpression `json:"expressions"`
}

// SpansTextSearch searches span text fields.
type SpansTextSearch struct {
	Query string `json:"query"`
}

// SpansFilterCondition is a single span field comparison.
type SpansFilterCondition struct {
	Field    SpansFilterField  `json:"field"`
	Operator string            `json:"operator"`
	Value    *SpansFilterValue `json:"value,omitempty"`
}

// SpansFilterField references a searchable span field. Name may be a well-known
// field such as "service_name" or "status_code", or an attribute key. Scope is
// optional: use an InsightsSpansFieldScope* value to target one attribute map,
// or leave it empty for well-known fields and to search both maps.
type SpansFilterField struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"`
}

// SpansFilterValue is a typed filter literal. Set exactly one value.
type SpansFilterValue struct {
	StringValue *string  `json:"string_value,omitempty"`
	NumberValue *float64 `json:"number_value,omitempty"`

	// NumberValueInt64 emits number_value without float64 precision loss. Use
	// it for Unix timestamps represented as integer seconds through nanoseconds.
	NumberValueInt64 *int64                 `json:"-"`
	StringArrayValue *SpansStringArrayValue `json:"string_array_value,omitempty"`
	NumberArrayValue *SpansNumberArrayValue `json:"number_array_value,omitempty"`

	// Deprecated: use the corresponding *Value field. Bool is retained only
	// for source compatibility; the spans API does not support boolean values.
	String      *string   `json:"-"`
	Number      *float64  `json:"-"`
	Bool        *bool     `json:"-"`
	StringArray []string  `json:"-"`
	NumberArray []float64 `json:"-"`
}

// SpansStringArrayValue is a string-list filter value.
type SpansStringArrayValue struct {
	Values []string `json:"values"`
}

// SpansNumberArrayValue is a number-list filter value.
type SpansNumberArrayValue struct {
	Values []float64 `json:"values"`
}

// SpansOrderBy is a spans search sort clause.
type SpansOrderBy struct {
	Field     SpansFilterField `json:"field"`
	Direction string           `json:"direction,omitempty"`
}

// SpansPaginationRequest limits the number of spans returned. Limit defaults to
// 100 and must be between 1 and 1000.
type SpansPaginationRequest struct {
	Limit int `json:"limit,omitempty"`
}

// SpansSearchResponse contains matching spans and pagination state.
type SpansSearchResponse struct {
	Data       []SpanRecord             `json:"data,omitempty"`
	Pagination *SpansPaginationResponse `json:"pagination,omitempty"`
}

// SpanRecord is a single span returned by a spans search.
type SpanRecord struct {
	TraceID       string            `json:"trace_id,omitempty"`
	SpanID        string            `json:"span_id,omitempty"`
	ParentSpanID  string            `json:"parent_span_id,omitempty"`
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	DurationNs    int64             `json:"duration_ns,omitempty"`
	Name          string            `json:"name,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	StatusCode    string            `json:"status_code,omitempty"`
	StatusMessage string            `json:"status_message,omitempty"`
	ServiceName   string            `json:"service_name,omitempty"`
	Resource      map[string]string `json:"resource,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
	Region        string            `json:"region,omitempty"`
}

// SpansPaginationResponse describes whether more spans matched than were
// returned. Cursor-based continuation is not yet supported.
type SpansPaginationResponse struct {
	HasMore bool `json:"has_more"`
}

// MarshalJSON emits the logs-style typed value and translates deprecated
// fields for source compatibility.
func (v SpansFilterValue) MarshalJSON() ([]byte, error) {
	type wire SpansFilterValue
	canonical := countSet(v.StringValue != nil, v.NumberValue != nil, v.NumberValueInt64 != nil, v.StringArrayValue != nil, v.NumberArrayValue != nil)
	legacy := countSet(v.String != nil, v.Number != nil, v.Bool != nil, v.StringArray != nil, v.NumberArray != nil)
	if canonical > 0 && legacy > 0 {
		return nil, fmt.Errorf("godo: SpansFilterValue mixes current and deprecated fields")
	}
	if canonical == 1 && v.NumberValueInt64 != nil {
		return json.Marshal(struct {
			NumberValue int64 `json:"number_value"`
		}{NumberValue: *v.NumberValueInt64})
	}
	if canonical == 1 {
		return json.Marshal(wire(v))
	}
	if canonical != 0 || legacy != 1 {
		return nil, fmt.Errorf("godo: SpansFilterValue must set exactly one field")
	}
	if v.Bool != nil {
		return nil, fmt.Errorf("godo: SpansFilterValue boolean values are not supported")
	}
	translated := SpansFilterValue{
		StringValue: v.String,
		NumberValue: v.Number,
	}
	if v.StringArray != nil {
		translated.StringArrayValue = &SpansStringArrayValue{Values: v.StringArray}
	}
	if v.NumberArray != nil {
		translated.NumberArrayValue = &SpansNumberArrayValue{Values: v.NumberArray}
	}
	return json.Marshal(wire(translated))
}

// UnmarshalJSON accepts the logs-style typed value.
func (v *SpansFilterValue) UnmarshalJSON(data []byte) error {
	*v = SpansFilterValue{}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err == nil {
		if keys["bool_value"] != nil {
			return fmt.Errorf("godo: SpansFilterValue boolean values are not supported")
		}
		current := countSet(keys["string_value"] != nil, keys["number_value"] != nil, keys["string_array_value"] != nil, keys["number_array_value"] != nil)
		deprecated := keys["type"] != nil || keys["value"] != nil
		if current > 0 && deprecated {
			return fmt.Errorf("godo: SpansFilterValue mixes current and deprecated wire shapes")
		}
		if deprecated {
			var old struct {
				Type  string          `json:"type"`
				Value json.RawMessage `json:"value"`
			}
			if err := json.Unmarshal(data, &old); err != nil {
				return err
			}
			return v.setDeprecated(old.Type, old.Value)
		}
		if current == 1 && keys["number_value"] != nil {
			var exact int64
			if err := json.Unmarshal(keys["number_value"], &exact); err == nil {
				v.NumberValueInt64 = &exact
				return nil
			}
		}
	}
	type wire SpansFilterValue
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return v.setDeprecatedBare(data)
	}
	*v = SpansFilterValue(decoded)
	if countSet(v.StringValue != nil, v.NumberValue != nil, v.StringArrayValue != nil, v.NumberArrayValue != nil) != 1 {
		return v.setDeprecatedBare(data)
	}
	return nil
}

func (v *SpansFilterValue) setDeprecated(typ string, raw json.RawMessage) error {
	switch typ {
	case InsightsSpansValueTypeString:
		return json.Unmarshal(raw, &v.String)
	case InsightsSpansValueTypeNumber:
		return json.Unmarshal(raw, &v.Number)
	case InsightsSpansValueTypeBool:
		return json.Unmarshal(raw, &v.Bool)
	case InsightsSpansValueTypeStringArray:
		return json.Unmarshal(raw, &v.StringArray)
	case InsightsSpansValueTypeNumberArray:
		return json.Unmarshal(raw, &v.NumberArray)
	default:
		return fmt.Errorf("godo: unknown deprecated SpansFilterValue type %q", typ)
	}
}

func (v *SpansFilterValue) setDeprecatedBare(data []byte) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch value := raw.(type) {
	case string:
		v.String = &value
	case float64:
		v.Number = &value
	case bool:
		v.Bool = &value
	case []interface{}:
		strings := make([]string, len(value))
		numbers := make([]float64, len(value))
		allStrings, allNumbers := true, true
		for i, element := range value {
			var ok bool
			strings[i], ok = element.(string)
			allStrings = allStrings && ok
			numbers[i], ok = element.(float64)
			allNumbers = allNumbers && ok
		}
		switch {
		case allStrings:
			v.StringArray = strings
		case allNumbers:
			v.NumberArray = numbers
		default:
			return fmt.Errorf("godo: deprecated SpansFilterValue array mixes element types")
		}
	default:
		return fmt.Errorf("godo: unsupported deprecated SpansFilterValue")
	}
	return nil
}

// MarshalJSON emits a logs-style expression and translates the deprecated
// flat AST for source compatibility.
func (e SpansFilterExpression) MarshalJSON() ([]byte, error) {
	type wire SpansFilterExpression
	canonical := countSet(e.Condition != nil, e.And != nil, e.Or != nil, e.Not != nil, e.TextSearch != nil)
	legacy := e.Type != "" || e.Field != nil || e.Operator != "" || e.Value != nil || e.Expressions != nil || e.Query != ""
	if canonical > 0 && legacy {
		return nil, fmt.Errorf("godo: SpansFilterExpression mixes current and deprecated fields")
	}
	if canonical == 1 {
		return json.Marshal(wire(e))
	}
	if canonical != 0 || !legacy {
		return nil, fmt.Errorf("godo: SpansFilterExpression must set exactly one node")
	}
	translated, err := e.fromDeprecated()
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire(translated))
}

// UnmarshalJSON accepts the current expression shape and the deprecated flat
// shape. Deprecated input is retained in the deprecated fields and marshals
// back using the current shape.
func (e *SpansFilterExpression) UnmarshalJSON(data []byte) error {
	*e = SpansFilterExpression{}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	hasCurrent := keys["condition"] != nil || keys["and"] != nil || keys["or"] != nil || keys["not"] != nil || keys["text_search"] != nil
	hasDeprecated := keys["type"] != nil || keys["field"] != nil || keys["operator"] != nil || keys["value"] != nil || keys["expressions"] != nil || keys["query"] != nil
	if hasCurrent && hasDeprecated {
		return fmt.Errorf("godo: SpansFilterExpression mixes current and deprecated wire shapes")
	}
	if hasCurrent {
		type wire SpansFilterExpression
		var decoded wire
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*e = SpansFilterExpression(decoded)
		if countSet(e.Condition != nil, e.And != nil, e.Or != nil, e.Not != nil, e.TextSearch != nil) != 1 {
			return fmt.Errorf("godo: SpansFilterExpression must contain exactly one node")
		}
		return nil
	}
	var deprecated struct {
		Type        string                  `json:"type"`
		Field       *SpansFilterField       `json:"field"`
		Operator    string                  `json:"operator"`
		Value       *SpansFilterValue       `json:"value"`
		Expressions []SpansFilterExpression `json:"expressions"`
		Query       string                  `json:"query"`
	}
	if err := json.Unmarshal(data, &deprecated); err != nil {
		return err
	}
	e.Type, e.Field, e.Operator, e.Value = deprecated.Type, deprecated.Field, deprecated.Operator, deprecated.Value
	e.Expressions, e.Query = deprecated.Expressions, deprecated.Query
	_, err := e.fromDeprecated()
	return err
}

func (e SpansFilterExpression) fromDeprecated() (SpansFilterExpression, error) {
	switch e.Type {
	case "", InsightsSpansFilterTypeCondition:
		if e.Field == nil {
			return SpansFilterExpression{}, fmt.Errorf("godo: deprecated spans condition requires Field")
		}
		return SpansFilterExpression{Condition: &SpansFilterCondition{Field: *e.Field, Operator: e.Operator, Value: e.Value}}, nil
	case InsightsSpansFilterTypeAnd:
		return SpansFilterExpression{And: &SpansFilterGroup{Expressions: e.Expressions}}, nil
	case InsightsSpansFilterTypeOr:
		return SpansFilterExpression{Or: &SpansFilterGroup{Expressions: e.Expressions}}, nil
	case InsightsSpansFilterTypeNot:
		if len(e.Expressions) != 1 {
			return SpansFilterExpression{}, fmt.Errorf("godo: deprecated spans not node requires one expression")
		}
		return SpansFilterExpression{Not: &e.Expressions[0]}, nil
	case InsightsSpansFilterTypeTextSearch:
		return SpansFilterExpression{TextSearch: &SpansTextSearch{Query: e.Query}}, nil
	default:
		return SpansFilterExpression{}, fmt.Errorf("godo: unknown deprecated spans filter type %q", e.Type)
	}
}

func countSet(values ...bool) int {
	n := 0
	for _, value := range values {
		if value {
			n++
		}
	}
	return n
}

// SearchSpans searches tracing spans in a region. The request time range must
// not exceed seven days. Results are ordered by start time descending unless
// OrderBy overrides it. Cursor pagination is not yet supported; the response
// Pagination.HasMore reports whether more spans matched than were returned.
func (s *InsightsServiceOp) SearchSpans(ctx context.Context, region string, search *SpansSearchRequest) (*SpansSearchResponse, *Response, error) {
	path := "/v2/insights/query/" + url.PathEscape(region) + "/spans/search"
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, search)
	if err != nil {
		return nil, nil, err
	}

	result := new(SpansSearchResponse)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// TracesSearchRequest describes an Insights traces search. TimeRange must not
// exceed seven days.
type TracesSearchRequest struct {
	TimeRange  TracesTimeRange          `json:"time_range"`
	Filter     *TracesFilterExpression  `json:"filter,omitempty"`
	OrderBy    []TracesOrderBy          `json:"order_by,omitempty"`
	Pagination *TracesPaginationRequest `json:"pagination,omitempty"`
}

// TracesTimeRange is an inclusive traces query time window.
type TracesTimeRange struct {
	From TracesTimeBound `json:"from"`
	To   TracesTimeBound `json:"to"`
}

// TracesTimeBound is a single time bound. Set Relative or Absolute; if both are
// set, Absolute is used. Relative accepts "now", a bare lookback duration such
// as "1h" or "7d", or an offset such as "now-15m". Absolute accepts an RFC3339
// string or a Unix timestamp encoded as a decimal string.
type TracesTimeBound struct {
	Relative string `json:"relative,omitempty"`
	Absolute string `json:"absolute,omitempty"`
}

// TracesFilterExpression is a boolean filter tree. Set exactly one expression
// node. A trace matches when at least one of its spans matches the filter.
type TracesFilterExpression struct {
	Condition  *TracesFilterCondition  `json:"condition,omitempty"`
	And        *TracesFilterGroup      `json:"and,omitempty"`
	Or         *TracesFilterGroup      `json:"or,omitempty"`
	Not        *TracesFilterExpression `json:"not,omitempty"`
	TextSearch *TracesTextSearch       `json:"text_search,omitempty"`

	// Deprecated: use Condition, And, Or, Not, or TextSearch. These fields are
	// retained as source-compatible inputs and are never emitted directly.
	Type        string                   `json:"-"`
	Field       *TracesFilterField       `json:"-"`
	Operator    string                   `json:"-"`
	Value       *TracesFilterValue       `json:"-"`
	Expressions []TracesFilterExpression `json:"-"`
	Query       string                   `json:"-"`
}

// TracesFilterGroup combines nested traces filter expressions.
type TracesFilterGroup struct {
	Expressions []TracesFilterExpression `json:"expressions"`
}

// TracesTextSearch searches span text fields within traces.
type TracesTextSearch struct {
	Query string `json:"query"`
}

// TracesFilterCondition is a single span field comparison.
type TracesFilterCondition struct {
	Field    TracesFilterField  `json:"field"`
	Operator string             `json:"operator"`
	Value    *TracesFilterValue `json:"value,omitempty"`
}

// TracesFilterField references a span field. In filters, Name may be a
// well-known span field such as "service_name" or "status_code", or an attribute
// key; Scope optionally uses an InsightsTracesFieldScope* value to restrict the
// lookup. In OrderBy, Name must be a trace-level field such as "started" or
// "duration_ns" and Scope is ignored.
type TracesFilterField struct {
	Name  string `json:"name"`
	Scope string `json:"scope,omitempty"`
}

// TracesFilterValue is a typed filter literal. Set exactly one value.
type TracesFilterValue struct {
	StringValue *string  `json:"string_value,omitempty"`
	NumberValue *float64 `json:"number_value,omitempty"`

	// NumberValueInt64 emits number_value without float64 precision loss. Use
	// it for Unix timestamps represented as integer seconds through nanoseconds.
	NumberValueInt64 *int64                  `json:"-"`
	StringArrayValue *TracesStringArrayValue `json:"string_array_value,omitempty"`
	NumberArrayValue *TracesNumberArrayValue `json:"number_array_value,omitempty"`

	// Deprecated: use the corresponding *Value field. Bool is retained only
	// for source compatibility; the traces API does not support boolean values.
	String      *string   `json:"-"`
	Number      *float64  `json:"-"`
	Bool        *bool     `json:"-"`
	StringArray []string  `json:"-"`
	NumberArray []float64 `json:"-"`
}

// TracesStringArrayValue is a string-list filter value.
type TracesStringArrayValue struct {
	Values []string `json:"values"`
}

// TracesNumberArrayValue is a number-list filter value.
type TracesNumberArrayValue struct {
	Values []float64 `json:"values"`
}

// TracesOrderBy is a traces search sort clause. Only sorting by "started" is
// exact; other fields sort the most recent matching traces up to the limit.
type TracesOrderBy struct {
	Field     TracesFilterField `json:"field"`
	Direction string            `json:"direction,omitempty"`
}

// TracesPaginationRequest limits the number of traces returned. Limit defaults
// to 100 and must not exceed 1000.
type TracesPaginationRequest struct {
	Limit int `json:"limit,omitempty"`
}

// TracesSearchResponse contains matching traces and pagination state.
type TracesSearchResponse struct {
	Data       []TraceSummary            `json:"data,omitempty"`
	Pagination *TracesPaginationResponse `json:"pagination,omitempty"`
}

// TraceSummary is a single trace returned by a traces search, aggregated from
// its spans.
type TraceSummary struct {
	TraceID     string    `json:"trace_id"`
	Started     time.Time `json:"started"`
	ServiceName string    `json:"service_name,omitempty"`
	DurationNs  int64     `json:"duration_ns,omitempty"`
	SpanCount   int       `json:"span_count,omitempty"`
	ErrorCount  int       `json:"error_count,omitempty"`
	Status      string    `json:"status,omitempty"`
	SpanNames   []string  `json:"span_names,omitempty"`
}

// TracesPaginationResponse describes whether more traces matched than were
// returned. There is no cursor; narrow the time range or filter to see more.
type TracesPaginationResponse struct {
	HasMore bool `json:"has_more"`
}

// TraceGetOptions narrows the lookup window for GetTrace. Both bounds accept
// the same relative and absolute formats as TracesTimeBound. The window
// defaults to the last seven days and must not exceed seven days.
type TraceGetOptions struct {
	From string `url:"from,omitempty"`
	To   string `url:"to,omitempty"`
}

// Trace is a single trace and its spans, ordered by start time.
type Trace struct {
	TraceID   string      `json:"trace_id"`
	Spans     []TraceSpan `json:"spans,omitempty"`
	Truncated bool        `json:"truncated,omitempty"`
}

// TraceSpan is a single span belonging to a Trace.
type TraceSpan struct {
	TraceID       string            `json:"trace_id"`
	SpanID        string            `json:"span_id"`
	ParentSpanID  string            `json:"parent_span_id,omitempty"`
	StartTime     time.Time         `json:"start_time"`
	EndTime       time.Time         `json:"end_time"`
	DurationNs    int64             `json:"duration_ns,omitempty"`
	Name          string            `json:"name,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	StatusCode    string            `json:"status_code,omitempty"`
	StatusMessage string            `json:"status_message,omitempty"`
	ServiceName   string            `json:"service_name,omitempty"`
	Resource      map[string]string `json:"resource,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

// MarshalJSON emits the logs-style typed value and translates deprecated
// fields for source compatibility.
func (v TracesFilterValue) MarshalJSON() ([]byte, error) {
	type wire TracesFilterValue
	canonical := countSet(v.StringValue != nil, v.NumberValue != nil, v.NumberValueInt64 != nil, v.StringArrayValue != nil, v.NumberArrayValue != nil)
	legacy := countSet(v.String != nil, v.Number != nil, v.Bool != nil, v.StringArray != nil, v.NumberArray != nil)
	if canonical > 0 && legacy > 0 {
		return nil, fmt.Errorf("godo: TracesFilterValue mixes current and deprecated fields")
	}
	if canonical == 1 && v.NumberValueInt64 != nil {
		return json.Marshal(struct {
			NumberValue int64 `json:"number_value"`
		}{NumberValue: *v.NumberValueInt64})
	}
	if canonical == 1 {
		return json.Marshal(wire(v))
	}
	if canonical != 0 || legacy != 1 {
		return nil, fmt.Errorf("godo: TracesFilterValue must set exactly one field")
	}
	if v.Bool != nil {
		return nil, fmt.Errorf("godo: TracesFilterValue boolean values are not supported")
	}
	translated := TracesFilterValue{
		StringValue: v.String,
		NumberValue: v.Number,
	}
	if v.StringArray != nil {
		translated.StringArrayValue = &TracesStringArrayValue{Values: v.StringArray}
	}
	if v.NumberArray != nil {
		translated.NumberArrayValue = &TracesNumberArrayValue{Values: v.NumberArray}
	}
	return json.Marshal(wire(translated))
}

// UnmarshalJSON accepts the logs-style typed value.
func (v *TracesFilterValue) UnmarshalJSON(data []byte) error {
	*v = TracesFilterValue{}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err == nil {
		if keys["bool_value"] != nil {
			return fmt.Errorf("godo: TracesFilterValue boolean values are not supported")
		}
		current := countSet(keys["string_value"] != nil, keys["number_value"] != nil, keys["string_array_value"] != nil, keys["number_array_value"] != nil)
		deprecated := keys["type"] != nil || keys["value"] != nil
		if current > 0 && deprecated {
			return fmt.Errorf("godo: TracesFilterValue mixes current and deprecated wire shapes")
		}
		if deprecated {
			var old struct {
				Type  string          `json:"type"`
				Value json.RawMessage `json:"value"`
			}
			if err := json.Unmarshal(data, &old); err != nil {
				return err
			}
			return v.setDeprecated(old.Type, old.Value)
		}
		if current == 1 && keys["number_value"] != nil {
			var exact int64
			if err := json.Unmarshal(keys["number_value"], &exact); err == nil {
				v.NumberValueInt64 = &exact
				return nil
			}
		}
	}
	type wire TracesFilterValue
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return v.setDeprecatedBare(data)
	}
	*v = TracesFilterValue(decoded)
	if countSet(v.StringValue != nil, v.NumberValue != nil, v.StringArrayValue != nil, v.NumberArrayValue != nil) != 1 {
		return v.setDeprecatedBare(data)
	}
	return nil
}

func (v *TracesFilterValue) setDeprecated(typ string, raw json.RawMessage) error {
	switch typ {
	case InsightsTracesValueTypeString:
		return json.Unmarshal(raw, &v.String)
	case InsightsTracesValueTypeNumber:
		return json.Unmarshal(raw, &v.Number)
	case InsightsTracesValueTypeBool:
		return json.Unmarshal(raw, &v.Bool)
	case InsightsTracesValueTypeStringArray:
		return json.Unmarshal(raw, &v.StringArray)
	case InsightsTracesValueTypeNumberArray:
		return json.Unmarshal(raw, &v.NumberArray)
	default:
		return fmt.Errorf("godo: unknown deprecated TracesFilterValue type %q", typ)
	}
}

func (v *TracesFilterValue) setDeprecatedBare(data []byte) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch value := raw.(type) {
	case string:
		v.String = &value
	case float64:
		v.Number = &value
	case bool:
		v.Bool = &value
	case []interface{}:
		strings := make([]string, len(value))
		numbers := make([]float64, len(value))
		allStrings, allNumbers := true, true
		for i, element := range value {
			var ok bool
			strings[i], ok = element.(string)
			allStrings = allStrings && ok
			numbers[i], ok = element.(float64)
			allNumbers = allNumbers && ok
		}
		switch {
		case allStrings:
			v.StringArray = strings
		case allNumbers:
			v.NumberArray = numbers
		default:
			return fmt.Errorf("godo: deprecated TracesFilterValue array mixes element types")
		}
	default:
		return fmt.Errorf("godo: unsupported deprecated TracesFilterValue")
	}
	return nil
}

// MarshalJSON emits a logs-style expression and translates the deprecated
// flat AST for source compatibility.
func (e TracesFilterExpression) MarshalJSON() ([]byte, error) {
	type wire TracesFilterExpression
	canonical := countSet(e.Condition != nil, e.And != nil, e.Or != nil, e.Not != nil, e.TextSearch != nil)
	legacy := e.Type != "" || e.Field != nil || e.Operator != "" || e.Value != nil || e.Expressions != nil || e.Query != ""
	if canonical > 0 && legacy {
		return nil, fmt.Errorf("godo: TracesFilterExpression mixes current and deprecated fields")
	}
	if canonical == 1 {
		return json.Marshal(wire(e))
	}
	if canonical != 0 || !legacy {
		return nil, fmt.Errorf("godo: TracesFilterExpression must set exactly one node")
	}
	translated, err := e.fromDeprecated()
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire(translated))
}

// UnmarshalJSON accepts the current expression shape and the deprecated flat
// shape.
func (e *TracesFilterExpression) UnmarshalJSON(data []byte) error {
	*e = TracesFilterExpression{}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	hasCurrent := keys["condition"] != nil || keys["and"] != nil || keys["or"] != nil || keys["not"] != nil || keys["text_search"] != nil
	hasDeprecated := keys["type"] != nil || keys["field"] != nil || keys["operator"] != nil || keys["value"] != nil || keys["expressions"] != nil || keys["query"] != nil
	if hasCurrent && hasDeprecated {
		return fmt.Errorf("godo: TracesFilterExpression mixes current and deprecated wire shapes")
	}
	if hasCurrent {
		type wire TracesFilterExpression
		var decoded wire
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*e = TracesFilterExpression(decoded)
		if countSet(e.Condition != nil, e.And != nil, e.Or != nil, e.Not != nil, e.TextSearch != nil) != 1 {
			return fmt.Errorf("godo: TracesFilterExpression must contain exactly one node")
		}
		return nil
	}
	var deprecated struct {
		Type        string                   `json:"type"`
		Field       *TracesFilterField       `json:"field"`
		Operator    string                   `json:"operator"`
		Value       *TracesFilterValue       `json:"value"`
		Expressions []TracesFilterExpression `json:"expressions"`
		Query       string                   `json:"query"`
	}
	if err := json.Unmarshal(data, &deprecated); err != nil {
		return err
	}
	e.Type, e.Field, e.Operator, e.Value = deprecated.Type, deprecated.Field, deprecated.Operator, deprecated.Value
	e.Expressions, e.Query = deprecated.Expressions, deprecated.Query
	_, err := e.fromDeprecated()
	return err
}

func (e TracesFilterExpression) fromDeprecated() (TracesFilterExpression, error) {
	switch e.Type {
	case "", InsightsTracesFilterTypeCondition:
		if e.Field == nil {
			return TracesFilterExpression{}, fmt.Errorf("godo: deprecated traces condition requires Field")
		}
		return TracesFilterExpression{Condition: &TracesFilterCondition{Field: *e.Field, Operator: e.Operator, Value: e.Value}}, nil
	case InsightsTracesFilterTypeAnd:
		return TracesFilterExpression{And: &TracesFilterGroup{Expressions: e.Expressions}}, nil
	case InsightsTracesFilterTypeOr:
		return TracesFilterExpression{Or: &TracesFilterGroup{Expressions: e.Expressions}}, nil
	case InsightsTracesFilterTypeNot:
		if len(e.Expressions) != 1 {
			return TracesFilterExpression{}, fmt.Errorf("godo: deprecated traces not node requires one expression")
		}
		return TracesFilterExpression{Not: &e.Expressions[0]}, nil
	case InsightsTracesFilterTypeTextSearch:
		return TracesFilterExpression{TextSearch: &TracesTextSearch{Query: e.Query}}, nil
	default:
		return TracesFilterExpression{}, fmt.Errorf("godo: unknown deprecated traces filter type %q", e.Type)
	}
}

// SearchTraces searches traces in a region. Each result aggregates one trace's
// spans; a trace is included when at least one of its spans matches the
// filter. The request time range must not exceed seven days. Results are
// ordered by start time descending unless OrderBy overrides it.
func (s *InsightsServiceOp) SearchTraces(ctx context.Context, region string, search *TracesSearchRequest) (*TracesSearchResponse, *Response, error) {
	path := "/v2/insights/query/" + url.PathEscape(region) + "/traces/search"
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, search)
	if err != nil {
		return nil, nil, err
	}

	result := new(TracesSearchResponse)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// GetTrace retrieves a trace and its spans by trace ID. A trace is only
// resolvable in the region that ingested it, and only spans that started within
// the lookup window are returned. Trace.Truncated reports whether the trace has
// more spans than were returned.
func (s *InsightsServiceOp) GetTrace(ctx context.Context, region, traceID string, opt *TraceGetOptions) (*Trace, *Response, error) {
	path := "/v2/insights/query/" + url.PathEscape(region) + "/traces/" + url.PathEscape(traceID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(Trace)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// InsightsResourceListOptions are filters and pagination for listing resource
// signal enablement. PerPage defaults to 20 and is capped at 200 by the API.
type InsightsResourceListOptions struct {
	ProductTypes []string `url:"filter.product_types,omitempty"`
	Regions      []string `url:"filter.regions,omitempty"`
	Page         int      `url:"pagination.page,omitempty"`
	PerPage      int      `url:"pagination.per_page,omitempty"`
	// Sort is one of the InsightsResourceSort* constants. Defaults to resource_urn.
	Sort string `url:"pagination.sort,omitempty"`
	// SortDirection is "asc" or "desc". Defaults to asc.
	SortDirection string `url:"pagination.sort_direction,omitempty"`
}

// InsightsResourceSignalState is the enablement state of one telemetry signal
// (metrics, logs, or traces) for a resource.
type InsightsResourceSignalState struct {
	Enabled     bool      `json:"enabled"`
	EffectiveAt time.Time `json:"effective_at"`
}

// InsightsResource is the current telemetry signal enablement for a resource.
// A signal is nil when no explicit enablement has ever been recorded for it.
type InsightsResource struct {
	ResourceURN string                       `json:"resource_urn"`
	ProductType string                       `json:"product_type"`
	Region      string                       `json:"region,omitempty"`
	Metrics     *InsightsResourceSignalState `json:"metrics,omitempty"`
	Logs        *InsightsResourceSignalState `json:"logs,omitempty"`
	Traces      *InsightsResourceSignalState `json:"traces,omitempty"`
}

// InsightsResourcesPagination is page-based pagination metadata for a
// resources list.
type InsightsResourcesPagination struct {
	Page    int `json:"page"`
	Pages   int `json:"pages"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// InsightsResourceList is a page of resources and their signal enablement.
type InsightsResourceList struct {
	Resources  []InsightsResource           `json:"resources,omitempty"`
	Pagination *InsightsResourcesPagination `json:"pagination,omitempty"`
}

// InsightsResourceSignalsRequest is the body for updating a resource's signal
// enablement. Set at least one of Metrics, Logs, or Traces (use godo.Bool);
// nil signals are left unchanged. Region is only used when the resource has
// no existing enablement and cannot be changed afterwards.
type InsightsResourceSignalsRequest struct {
	Region  string `json:"region,omitempty"`
	Metrics *bool  `json:"metrics,omitempty"`
	Logs    *bool  `json:"logs,omitempty"`
	Traces  *bool  `json:"traces,omitempty"`
}

type insightsResourceRoot struct {
	Resource *InsightsResource `json:"resource"`
}

// ListResources lists resources with recorded telemetry signal enablement.
func (s *InsightsServiceOp) ListResources(ctx context.Context, opt *InsightsResourceListOptions) (*InsightsResourceList, *Response, error) {
	path, err := addOptions(insightsResourcesPath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(InsightsResourceList)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}
	return result, resp, nil
}

// GetResource retrieves the signal enablement for a resource by URN. The API
// returns 404 when no enablement has been recorded for the resource.
func (s *InsightsServiceOp) GetResource(ctx context.Context, resourceURN string) (*InsightsResource, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, insightsResourcePath(insightsResourcesPath, resourceURN), nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(insightsResourceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Resource, resp, nil
}

// UpdateResourceSignals enables or disables telemetry signals for a resource.
// Signals left nil in the request are unchanged.
func (s *InsightsServiceOp) UpdateResourceSignals(ctx context.Context, resourceURN string, update *InsightsResourceSignalsRequest) (*InsightsResource, *Response, error) {
	path := insightsResourcePath(insightsResourcesPath, resourceURN) + "/signals"
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(insightsResourceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Resource, resp, nil
}
