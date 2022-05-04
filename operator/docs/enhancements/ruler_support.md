---
title: lokistack ruler support
authors:
  - @periklis
reviewers:
  - @xperimental
  - @cyriltovena
creation-date: 2022-04-21
last-updated: 2022-04-21
tracking-link:
  - [5211](https://github.com/grafana/loki/issues/5211)
  - [5843](https://github.com/grafana/loki/issues/5843)
see-also:
  - [PR 5986](https://github.com/grafana/loki/pull/5986)
---

# LokiStack Ruler Support

## Summary

The ruler has been an integral part of Loki since release v2.2.0 and complete since v2.3.0 that added support for recording rules. This component enables users to give a group of LogQL expressions that trigger either alerts (e.g. high amount of error logs in production service) and/or post-evaluation recorded metrics (e.g. rate of incoming error logs on production service per minute). The following sections describe a set of APIs in form of custom resource definitions (CRD) that enable users of `LokiStack` resources to support:
- Operating the ruler optimized for a `LokiStack` size, i.e. `1x.extra-small`, `1x.small` and `1x.medium`.
- Define Loki alerting and recording rules using a Kubernetes custom resource (e.g. similar to `PrometheusRule` in [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator/)).
- Define a Kubernetes custom resource for the ruler to connect to a [remote write](https://grafana.com/docs/loki/latest/configuration/#ruler) endpoint and store log-based metrics.
- Define a Kubernetes custom resource for the ruler to connect to an [Alertmanager](https://grafana.com/docs/loki/latest/configuration/#ruler) endpoint and notify on firing alerts.

## Motivation

The Loki Operator manages `LokiStack` resources that consists of a set of Loki components for ingestion/quering and optionally a gateway microservice that ensures authenticated and authorized access to logs stored by Loki. The addition of the Loki ruler component initiates support for a new component type that acts as an automated user requesting logs from Loki. The ruler is supposed to work autonomously with direct access to object storage, i.e. acts as its own querier ([See more details](https://grafana.com/docs/loki/latest/operations/recording-rules/)) but respecting query [limits](https://grafana.com/docs/loki/latest/configuration/#limits_config). Thus the motivation of the following proposal is to enable operating the ruler as an integral but optional part of a `LokiStack` instance as well as supporting Kubernetes native configuration for declaring rules and external dependencies (e.g. remote_write, alertmanager).

### Goals

* The user can enable the ruler component via the `LokiStack` custom resource.
* The user can declare per-namespace custom resource for set of Loki alerting and recording rules.
* The user can optionally declare an endpoint for the `LokiStack` ruler to write recording-rule metrics.
* The user can optionally declare an endpoint for the `LokiStack` ruler to notify on firing alerts.
* The Loki Operator manages a ruler optimized for the selected `LokiStack` T-Shirt size.

### Non-Goals

* Reconciling stateless instances of the ruler component.
* Access rules and ruler configuration via the `LokiStack` gateway microservice.
* Support for viewing/editing rules via a UI tool of choice.
* Full-automated reconciliation of remote-write and AlertManager configuration on OpenShift.

## Proposal

The following enhancement proposal describes the required API additions and changes in the Loki Operator to add support for reconciling the Loki Ruler component as an integral part of `LokiStack` resources. In addition to the component itself it provides additional CRDs that enable users to declare alerting and recording rules. Furthermore it describes a CRD that allows to configure the ruler's `remote_write` and `alertmanager` connectivity capabilities.

In summary the `LokiStack` ruler component will support by default minimum the following capabilities:
- Use the Work-Ahead-Log to enable higher availability and mitigate data loss for recording rules (See [here](https://grafana.com/docs/loki/latest/operations/recording-rules/#write-ahead-log-wal)).
- Use a ring to allow horizontal scaling of the ruler per group (See [here](https://grafana.com/docs/loki/latest/operations/recording-rules/#scaling)).
- Use sharding per default (See [enable_sharding](https://grafana.com/docs/loki/latest/configuration/#ruler)).
- Re-use the same store configuration of an existing `LokiStack` instance.
- Re-use the same ring configuration of an existing `LokiStack` instance (per default only memberlist supported).

### API Extensions

#### LokiStack Changes: Support for the ruler component

The following section describes a set of changes that enable reconciling the Loki ruler component as an optional StatefulSet. The Loki ruler component spec inherits the same node placement capabilities as the rest of the `LokiStack` components, i.e. currently node-selectors and tolerations (defined in `LokiComponentSpec`).

```go
// RulesSpec defines the spec for the ruler component.
type RulesSpec struct {
    // Enabled defines a flag to enable/disable the ruler component
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enable"
    Enabled bool `json:"enabled"`

    // A selector to select which Loki rules to mount for loading alerting/recording
    // rules from.
    //
    // +optional
    // +kubebuilder:validation:optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Selector"
    Selector *metav1.LabelSelector `json:"selector,omitempty"`

    // Namespaces to be selected for AlertingRules/RecordingRules discovery. If unspecified, only
    // the same namespace as the LokiStack object is in is used.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Namespace Selector"
    NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}

// LokiStackSpec defines the desired state of LokiStack
type LokiStackSpec struct {
...
    // Rules defines the spec for the ruler component
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Rules"
    Rules *RulesSpec `json:"rules,omitempty"`
...
}

// LokiTemplateSpec defines the template of all requirements to configure
// scheduling of all Loki components to be deployed.
type LokiTemplateSpec struct {
...
    // Ruler defines the ruler component spec.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ruler pods"
    Ruler *LokiComponentSpec `json:"ruler,omitempty"`
}

// LokiStackComponentStatus defines the map of per pod status per LokiStack component.
// Each component is represented by a separate map of v1.Phase to a list of pods.
type LokiStackComponentStatus struct {
...
    // Ruler is a map to the per pod status of the lokistack ruler statefulset.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Ruler",order=6
    Ruler PodStatusMap `json:"ruler,omitempty"`
}
```

#### AlertingRule definition

The `AlertingRules` CRD comprises a set of specifications and webhook validation definitions to declare groups of alerting rules for a single `LokiStack` instance. The syntax for the rule groups resembles the official [Prometheus Rule syntax](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/#recording-rules). In addition the webhook validation definition provides support for rule validation conditions:

1. If a `AlertingRule` includes an invalid `interval` period it is an invalid alerting rule
2. If a `AlertingRule` includes an invalid `for` period it is an invalid alerting rule.
3. If a `AlertingRule` includes an invalid LogQL `expr` it is an invalid alerting rule.
4. If a `AlertingRule` includes two groups with the same name it is an invalid alerting rule.
5. If none of above applies a `AlertingRule` is considered a valid alerting rule.

```go
// AlertingRuleSpec defines the desired state of AlertingRule
type AlertingRuleSpec struct {
    // Tenant to associate the alerting rule groups.
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant ID"
    TenantID string `json:"tenantID"`

    // List of groups for alerting rules.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Groups"
    Groups []*AlertingRuleGroup `json:"groups"`
}

// AlertingRuleGroup defines a group of Loki alerting rules.
type AlertingRuleGroup struct {
    // Name defines a name of the present recoding/alerting rule. Must be unique
    // within all loki rules.
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
    Name string `json:"name"`

    // Interval defines the time interval between evaluation of the given
    // alerting rule.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="1m"
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Evaluation Interval"
    Interval PrometheusDuration `json:"interval"`

    // Limit defines the number of alerts an alerting rule can produce. 0 is no limit.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Limit of firing alerts"
    Limit int32 `json:"limit,omitempty"`

    // Rules defines a list of alerting rules
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Rules"
    Rules []*AlertingRuleGroupSpec `json:"rules"`
}

// AlertingRuleGroupSpec defines the spec for a Loki alerting rule.
type AlertingRuleGroupSpec struct {
    // The name of the alert. Must be a valid label value.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
    Alert string `json:"alert,omitempty"`

    // The LogQL expression to evaluate. Every evaluation cycle this is
    // evaluated at the current time, and all resultant time series become
    // pending/firing alerts.
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="LogQL Expression"
    Expr string `json:"expr"`

    // Alerts are considered firing once they have been returned for this long.
    // Alerts which have not yet fired for long enough are considered pending.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Firing Threshold"
    For PrometheusDuration `json:"for,omitempty"`

    // Annotations to add to each alert.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Annotations"
    Annotations map[string]string `json:"annotations,omitempty"`

    // Labels to add to each alert.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Labels"
    Labels map[string]string `json:"labels,omitempty"`
}

// AlertingRuleStatus defines the observed state of AlertingRule
type AlertingRuleStatus struct {
    // Conditions of the AlertingRule generation health.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AlertingRule is the Schema for the alertingrules API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="AlertingRule",resources={{LokiStack,v1beta1}}
type AlertingRule struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   AlertingRuleSpec   `json:"spec,omitempty"`
    Status AlertingRuleStatus `json:"status,omitempty"`
}
```

#### RecordingRule definition

The `RecordingRule` CRD comprises a set of specifications and webhook validation definitions to declare groups of recording rules for a single `LokiStack` instance. The syntax for the rule groups resembles the official [Prometheus Rule syntax](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/#recording-rules). In addition the webhook validation definition provides support for rule validation conditions:

1. If a `RecordingRule` includes an invalid `interval` period it is an invalid recording rule
2. If a `RecordingRule` includes an invalid metric name for `record` it is an invalid recording rule.
3. If a `RecordingRule` includes an invalid LogQL `expr` it is an invalid recording rule.
4. If a `RecordingRule` includes two groups with the same name it is an invalid recording rule.
4. If none of above applies a `RecordingRule` is considered a valid recording rule.

```go
// RecordingRuleSpec defines the desired state of RecordingRule
type RecordingRuleSpec struct {
    // Tenant to associate the recording rule groups.
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant ID"
    TenantID string `json:"tenantID"`

    // List of groups for recording rules.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Groups"
    Groups []*RecordingRuleGroup `json:"groups"`
}

// RecordingRuleGroup defines a group of Loki  recording rules.
type RecordingRuleGroup struct {
    // Name defines a name of the present recoding rule. Must be unique
    // within all loki rules.
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
    Name string `json:"name"`

    // Interval defines the time interval between evaluation of the given
    // recoding rule.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="1m"
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Evaluation Interval"
    Interval PrometheusDuration `json:"interval"`

    // Limit defines the number of series a recording rule can produce. 0 is no limit.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Limit of produced series"
    Limit int32 `json:"limit,omitempty"`

    // Rules defines a list of recording rules
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Rules"
    Rules []*RecordingRuleGroupSpec `json:"rules"`
}

// RecordingRuleGroupSpec defines the spec for a Loki recording rule.
type RecordingRuleGroupSpec struct {
    // The name of the time series to output to. Must be a valid metric name.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Metric Name"
    Record string `json:"record,omitempty"`

    // The LogQL expression to evaluate. Every evaluation cycle this is
    // evaluated at the current time, and all resultant time series become
    // pending/firing alerts.
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="LogQL Expression"
    Expr string `json:"expr"`
}

// RecordingRuleStatus defines the observed state of RecordingRule
type RecordingRuleStatus struct {
    // Conditions of the RecordingRule generation health.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RecordingRule is the Schema for the recordingrules API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="RecordingRule",resources={{LokiStack,v1beta1}}
type RecordingRule struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   RecordingRuleSpec   `json:"spec,omitempty"`
    Status RecordingRuleStatus `json:"status,omitempty"`
}
```

#### RulerConfig definition

The following CRD defines the ruler configuration to access AlertManager hosts and to access a global Remote-Write-Endpoint (e.g. Prometheus, Thanos, Cortex). The types are split in three groups:
1. `RulerSpec`: This spec includes general settings like evalution and poll interval.
2. `AlertManagerSpec`: This spec includes all settings for pushing alert notifications to a list of AlertManager hosts.
3. `RemoteWriteSpec`: This spec includes all settings to configure a single global remote write endpoint to send recorded metrics.

**Note**: Sensitive authorization information are provided by a Kubernetes Secret resource, i.e. basic auth user/password, header authorization (See `AuthorizationSecretName`). The Secret is required to live in the same namespace as the `RulerConfig` resource.

```go
package v1beta1

import (
    monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LokiDuration defines the type for Prometheus durations.
//
// +kubebuilder:validation:Pattern:="((([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?|0)"
type LokiDuration string

// AlertManagerDiscoverySpec defines the configuration to use DNS resolution for AlertManager hosts.
type AlertManagerDiscoverySpec struct {
    // Use DNS SRV records to discover Alertmanager hosts.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Enabled bool `json:"enabled"`

    // How long to wait between refreshing DNS resolutions of Alertmanager hosts.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="1m"
    RefreshInterval LokiDuration `json:"refreshInterval"`
}

// AlertManagerNotificationSpec defines the configuration for AlertManager notification settings.
type AlertManagerNotificationSpec struct {
    // Capacity of the queue for notifications to be sent to the Alertmanager.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:=10000
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Notification Queue Capacity"
    QueueCapacity int32 `json:"queueCapacity"`

    // HTTP timeout duration when sending notifications to the Alertmanager.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="10s"
    Timeout LokiDuration `json:"timeout"`

    // Max time to tolerate outage for restoring "for" state of alert.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="1h"
    ForOutageTolerance LokiDuration `json:"forOutageTolerance"`

    // Minimum duration between alert and restored "for" state. This is maintained
    // only for alerts with configured "for" time greater than the grace period.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="10m"
    ForGracePeriod LokiDuration `json:"forGracePeriod"`

    // Minimum amount of time to wait before resending an alert to Alertmanager.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="1m"
    ResendDelay LokiDuration `json:"resendDelay"`
}

// AlertManagerSpec defines the configuration for ruler's alertmanager connectivity.
type AlertManagerSpec struct {
    // URL for alerts return path.
    //
    // +optional
    // +kubebuilder:validation:Optional
    ExternalURL string `json:"external_url,omitempty"`

    // Additional labels to add to all alerts.
    //
    // +optional
    // +kubebuilder:validation:Optional
    ExternalLabels map[string]string `json:"external_labels,omitempty"`

    // If enabled, then requests to Alertmanager use the v2 API.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enable AlertManager V2 API"
    EnableV2 bool `json:"enableV2"`

    // List of AlertManager URLs to send notifications to. Each Alertmanager URL is treated as
    // a separate group in the configuration. Multiple Alertmanagers in HA per group can be
    // supported by using DNS resolution (See EnableDNSDiscovery).
    //
    // +required
    // +kubebuilder:validation:Required
    Endpoints []string `json:"endpoints"`

    // Defines the configuration for DNS-based discovery of AlertManager hosts.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="DNS Discovery"
    DiscoverySpec *AlertManagerDiscoverySpec `json:"discoverySpec"`

    // Defines the configuration for the notification queue to AlertManager hosts.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Notification Queue"
    NotificationSpec *AlertManagerNotificationSpec `json:"notificationSpec"`
}

// RemoteWriteAuthType defines the type of authorization to use to access the remote write endpoint.
//
// +kubebuilder:validation:Enum=basic;header
type RemoteWriteAuthType string

const (
    // BasicAuthorization defines the remote write client to use HTTP basic authorization.
    BasicAuthorization RemoteWriteAuthType = "basic"
    // BasicAuthorization defines the remote write client to use HTTP header authorization.
    HeaderAuthorization RemoteWriteAuthType = "header"
)

// RemoteWriteClientSpec defines the configuration of the remote write client.
type RemoteWriteClientSpec struct {
    // Name of the remote write config, which if specified must be unique among remote write configs.
    //
    // +required
    // +kubebuilder:validation:Required
    Name string `json:"name"`

    // The URL of the endpoint to send samples to.
    //
    // +required
    // +kubebuilder:validation:Required
    URL string `json:"url"`

    // Timeout for requests to the remote write endpoint.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="30s"
    Timeout LokiDuration `json:"timeout"`

    // Type of Authorzation to use to access the remote write endpoint
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:basic","urn:alm:descriptor:com.tectonic.ui:select:header"},displayName="Authorization Type"
    AuthorizationType RemoteWriteAuthType `json:"authorization"`

    // Name of a secret in the namespace configured for authorization secrets.
    //
    // +required
    // +kubebuilder:validation:Required
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret",displayName="Authorization Secret Name"
    AuthorizationSecretName string `json:"authorizationSecretName"`

    // Additional HTTP headers to be sent along with each remote write request.
    //
    // +optional
    // +kubebuilder:validation:Optional
    AdditionalHeaders map[string]string `json:"additionalHeaders,omitempty"`

    // List of remote write relabel configurations.
    //
    // +optional
    // +kubebuilder:validation:Optional
    RelabelConfigs []monitoringv1.RelabelConfig `json:"relabelConfigs,omitempty"`

    // Optional proxy URL.
    //
    // +optional
    // +kubebuilder:validation:Optional
    ProxyURL string `json:"proxyUrl"`

    // Configure whether HTTP requests follow HTTP 3xx redirects.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:=true
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Follow HTTP Redirects"
    FollowRedirects bool `json:"followRedirects"`
}

// RemoteWriteClientQueueSpec defines the configuration of the remote write client queue.
type RemoteWriteClientQueueSpec struct {
    // Number of samples to buffer per shard before we block reading of more
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:=2500
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Queue Capacity"
    Capacity int32 `json:"capacity"`

    // Maximum number of shards, i.e. amount of concurrency.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:=200
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Maximum Shards"
    MaxShards int32 `json:"maxShards"`

    // Minimum number of shards, i.e. amount of concurrency.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:=200
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Minimum Shards"
    MinShards int32 `json:"minShards"`

    // Maximum number of samples per send.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:=500
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Maximum Shards per Send"
    MaxSamplesPerSend int32 `json:"maxSamplesPerSend"`

    // Maximum time a sample will wait in buffer.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="5s"
    BatchSendDeadline int32 `json:"batchSendDeadline"`

    // Initial retry delay. Gets doubled for every retry.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="30ms"
    MinBackOffPeriod int32 `json:"minBackOffPeriod"`

    // Maximum retry delay.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="100ms"
    MaxBackOffPeriod int32 `json:"maxBackOffPeriod"`
}

// RemoteWriteSpec defines the configuration for ruler's remote_write connectivity.
type RemoteWriteSpec struct {
    // Enable remote-write functionality.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enabled"
    Enabled bool `json:"enabled"`

    // Minimum period to wait between refreshing remote-write reconfigurations.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="10s"
    RefreshPeriod LokiDuration `json:"refreshPeriod"`

    // Defines the configuration for remote write client.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Client"
    ClientSpec *RemoteWriteClientSpec `json:"clientSpec"`

    // Defines the configuration for remote write client queue.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Client Queue"
    QueueSpec *RemoteWriteClientQueueSpec `json:"queueSpec"`
}

// RulerSpec defines the desired state of Ruler
type RulerSpec struct {
    // Interval on how frequently to evaluate rules.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="1m"
    EvalutionInterval LokiDuration `json:"evaluationInterval"`

    // Interval on how frequently to poll for new rule definitions.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +kubebuilder:default:="1m"
    PollInterval LokiDuration `json:"pollInterval"`

    // Defines alert manager configuration to notify on firing alerts.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Alert Manager Configuration"
    AlertManagerSpec *AlertManagerSpec `json:"alertmanager,omitempty"`

    // Defines a remote write endpoint to write recording rule metrics.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Remote Write Configuration"
    RemoteWriteSpec *RemoteWriteSpec `json:"remoteWrite,omitempty"`
}

// RulerStatus defines the observed state of Ruler
type RulerStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Ruler is the Schema for the lokirulers API
type Ruler struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   RulerSpec   `json:"spec,omitempty"`
    Status RulerStatus `json:"status,omitempty"`
}
```

### Implementation Details/Notes/Constraints

#### AlertingRule and RecordingRule reconciliation

`AlertringRule` and `RecordingRule` custom resources are transformed into a single ruler configuration file for a given `LokiStack` per type (i.e one for alerting and one for recording rules). The following approach is taken:
1. The `LokiStackController` filters all available cluster namespaces by `RulesSpec.NamespaceSelector`.
2. The `LokiStackController` filters first all available `AlertingRule` and `RecordingRule` custom resources by `RulesSpec.Selector` and further by the filtered list of namespaces from the previous step.
3. For `AlertingRule` and `RecordingRule` it transforms the final list into a ConfigMap:
```yaml
apiVersion: loki.grafana.com/v1beta1
kind: AlertingRule
metadata:
  name: alerting-rule-a
  namespace: ns-a
  UID: kube-uid-a
spec:
  tenantID: application
  groups: ...
---
apiVersion: loki.grafana.com/v1beta1
kind: AlertingRule
metadata:
  name: alerting-rule-b
  namespace: ns-b
  UID: kube-uid-b
spec:
  tenantID: infrastructure
  groups: ...
---
apiVersion: loki.grafana.com/v1beta1
kind: RecordingRule
metadata:
  name: recording-rule-a
  namespace: ns-a
  UID: kube-uid-c
spec:
  tenantID: application
  groups: ...
---
apiVersion: loki.grafana.com/v1beta1
kind: RecordingRule
metadata:
  name: recording-rule-b
  namespace: ns-b
  UID: kube-uid-c
spec:
  tenantID: infrastructure
  groups: ...
```

results in:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: lokistack-dev-alerting-rules
  namespace: lokistack-ns
data:
  "ns-a-alerting-rule-a-kube-uid-a.yaml": |
    ...
  "ns-b-alerting-rule-b-kube-uid-b.yaml": |
   ...
  "ns-a-recording-rule-a-kube-uid-c.yaml": |
    ...
  "ns-b-recording-rule-b-kube-uid-c.yaml": |
   ...
```

In addition the ruler's volumes specs are split in `v1.KeyToPath` items based on `AlertingRuleSpec.TenantID` and `RecordingRuleSpec.TenantID`, i.e:
```yaml
spec:
  template:
    spec:
      containers:
      - name: "ruler"
        volumeMounts:
        - name: "rules"
          volume: "rules"
          path: "/tmp/rules"
      volumes:
      - name: "rules"
        items:
        - key: "ns-a-alerting-rule-a-kube-uid-a.yaml"
          path: "application/ns-a-alerting-rule-a-kube-uid-a.yaml"
        - key: "ns-b-alerting-rule-b-kube-uid-b.yaml"
          path: "infrastructure/ns-b-alerting-rule-b-kube-uid-b.yaml"
        - key: "ns-a-recording-rule-a-kube-uid-c.yaml"
          path: "application/ns-a-recording-rule-a-kube-uid-c.yaml"
        - key: "ns-b-recording-rule-b-kube-uid-d.yaml"
          path: "infrastructure/ns-b-recording-rule-b-kube-uid-d.yaml"
```

In turn the rules directory is outlined as such:

```
/tmp/rules/application/ns-a-alerting-rule-a-kube-uid-a.yaml
          /application/ns-a-recording-rule-a-kube-uid-b.yaml
          /infrastructure/ns-b-alerting-rule-b-kube-uid-c.yaml
          /infrastructure/ns-b-recording-rule-b-kube-uid-d.yaml
```

5. The `AlertingRuleController` listens for `AlertingRule` create/update/delete events and it applies the `loki.grafana.com/rulesDiscoveredAt: time.Now().Format(time.RFC3339)` on each `LokiStack` instance on the cluster. This ensures that the `LokiStack` reconciliation loop starts anew.
6. The `RecordingRuleController` listens for `RecordingRule` create/update/delete events and it applies the `loki.grafana.com/rulesDiscoveredAt: time.Now().Format(time.RFC3339)` on each `LokiStack` instance on the cluster. This ensures that the `LokiStack` reconciliation loop starts anew.

In summary this approach allows to group all `AlertingRule` and `RecordingRule` custom resources in the **same** namespace as the `LokiStack` in a two dedicated ConfigMap, i.e. where the stack runs is the place where the rules live.

#### RulerConfig reconciliation

The `RulerConfig` custom resource is not transformed into a specific ruler configuration file per se. It is considered an optional companion to the `LokiStack` custom resource. Thus the existing `LokiStackController` listens for `RulerConfig` create/update/delete events on the same namespace as for a `LokiStack` custom resource and reconciles the final ruler configuration.

In detail the separation of concerns between both CRDs looks like:
1. `LokiStack`: Defines all required aspects to spin up a Loki ruler component, i.e. size, node placement. In addition it controls the common config settings for the Work-Ahead-Log and Ring configuration.
2. `RulerConfig`: Defines only the global runtime settings for the ruler, i.e. evaluation/poll intervals, AlertManager configuration, remote write client configuration.

#### General constraints

1. The above `RulerConfig` is limited to a single global remote write endpoint for exporting metrics from recording rules. This leaves solving multi-tenancy issues on the remote write server side and provide means (e.g. headers) for spliting/amending metrics ingestion per tenant. On the other hand it simplifies ruler operations as it does not require to spin concurrent remote write clients per tenant.
2. Additionally the `RulerConfig` requires a user-provided Kubernetes Secret for sensitive information. This adds an extra dependency that requires validation inside the controller-loop.

### Risks and Mitigations

#### Loki Ruler Configuration versioning

**Risk**: The `RulerConfig` CRD exposes an almost identical [ruler config](https://grafana.com/docs/loki/latest/configuration/#ruler) to support AlertManager and Remote-Write. Both sub-specifications are subject of change to the API version of the server endpoint (i.e. AlertManager, Remote-Write). Although AlertManager is using a versioned approach e.g. `EnableV2` switch, we miss a similar approach accross remote write endpoints.

**Mitigation**: We require a support matrix for `RulerConfig` versions to AlertManager API versions and Remote Write implementations. For later we could use a list of releases (e.g. Prometheus 2.x, Thanos 0.20.z).

#### Two ConfigMaps for all RecordingRule and AlertingRule custom resources

**Risk**: The proposed reconciliation approach manifests that all `AlertingRule` instances are transformed to individual entries in a single ConfigMap. Alerting rules can be become quite big (e.g. large LogQL expresssions). As per ConfigMaps represent Kubernetes resource stored in etcd, this might hit some store limits (See [1MiB limit](https://kubernetes.io/docs/concepts/configuration/configmap/#motivation)). Therefore storing all rules in a single ConfigMap might become impossible over time or on large clusters.

**Mitigation**: Introduce some sort of sharding of `AlertingRule** custom resources into multiple ConfigMap resources.

**Note**: The same applies to `RecordingRule`

## Design Details

### Open Questions [optional]

1.0000 Do we need to add support for Remote Write Configuration per tenant? (See `ruler_remote_write_*` parameters in [limits_config](https://grafana.com/docs/loki/latest/configuration/#limits_config))
2. Do we need to add support for the `ruler_evaluation_delay_duration`, `ruler_max_rules_per_rule_group` and `ruler_max_rule_groups_per_tenant` limits?

## Implementation History

* 2022-04-21: Initial draft proposal
* 2022-04-21: Spike implementation for `LokiRule` reconciliation and `RulerConfig` types. (See [PR](https://github.com/grafana/loki/pull/5986))
* 2022-04-26: Update draft to use `LokiRule` types to use webhook-based validation and `LokiRuleSpec.Selector`, `LokiRuleSpec.NamespaceSelector`. (See [PR](https://github.com/grafana/loki/pull/5986))
* 2022-04-28: Update draft to split `LokiRule` into two distinct types `AlertingRule` and `RecordingRule`. (See [PR](https://github.com/grafana/loki/pull/5986))
* 2022-04-28: Renamed `LokiRulerConfig` types to `RulerConfig` as per `Loki` prefix is part of the API group `loki.grafana.com`, i.e. fully qualified type is `ruler.loki.grafana.com`
* 2022-05-02: Update rule configmap generation based on per tenant subdirectory structure.
* 2022-05-03: Update rule configmap entry naming to use Kubernetes `Metadata.UID` as permanent suffix.

## Drawbacks

The above proposed design and implementation for LokiStack Ruler support adds a significant amount of new APIs as well as complexity into exposing a declarative approach **only** for the ruler component. Regardless the hard effort to minimize the amount of configuration settings in the proposed CRDs it remains still a huge addition to the operator code base. In contrast to the existing `LokiStack` CRD that is a very slim set of Loki settings (Note: without considering the gateway tenant configuration), the `RulerConfig` is almost one-to-one identical to the [ruler config](https://grafana.com/docs/loki/latest/configuration/#ruler).

## Alternatives

Alternatives providing support for Loki alerting and recording rules:
1. [opsgy/loki-rule-operator](https://github.com/opsgy/loki-rule-operator)
2. Add here anything missed
