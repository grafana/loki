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
// LokiStackSpec defines the desired state of LokiStack
type LokiStackSpec struct {
...
    // EnableRuler defines a flag to enable/disable the ruler component
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enable Ruler"
    EnableRuler bool `json:"enableRuler"`
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

#### LokiRule definition

The `LokiRule` CRD comprises a set of specifications and status definitions to declare groups of alerting and/or recording rules for a single `LokiStack` instance. The syntax for the rule groups resembles the official [Prometheus Rule syntax](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/#recording-rules). In addition the status definition provides support for rule validation conditions:

1. If a `LokiRule` includes an alert name and a record name it is ambiguous if it is an alerting or a recording rule.
2. If a `LokiRule` includes an invalid `for` period it is an invalid alerting rule.
3. If a `LokiRule` includes an invalid `record` metric name it is an invalid recording rule.
4. If a `LokiRule` includes an invalid LogQL `expr` it is an invalid rule.
5. If a `LokiRule` includes two groups with the same name it is an invalid rule.
6. If none of above applies a `LokiRule` is considered a valid rule.

```go
// EvaluationDuration defines the type for Prometheus durations.
//
// +kubebuilder:validation:Pattern:="((([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?|0)"
type EvaluationDuration string

// LokiRuleSpec defines the desired state of LokiRule
type LokiRuleSpec struct {
    // Name of the LokiStack to reconcile the rules for
    //
    // +required
    // +kubebuilder:validation:Required
    StackName string `json:"stackName"`

    // List of groups for alerting and/or recording rules.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Groups []*LokiRuleGroup `json:"groups"`
}

// LokiRuleGroup defines a group of Loki alerting and/or recording rules.
type LokiRuleGroup struct {
    // Name defines a name of the present recoding/alerting rule. Must be unique
    // within all loki rules.
    //
    // +required
    // +kubebuilder:validation:Required
    Name string `json:"name"`

    // Interval defines the time interval between evaluation of the given
    // recoding rule.
    //
    // +required
    // +kubebuilder:validation:Required
    Interval EvaluationDuration `json:"interval"`

    // Limit defines the number of alerts an alerting rule and series a recording
    // rule can produce. 0 is no limit.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Limit of firing alerts "
    Limit int32 `json:"limit,omitempty"`

    // Rules defines a list of alerting and/or recording rules
    //
    // +required
    // +kubebuilder:validation:Required
    Rules []*LokiRuleGroupSpec `json:"rules"`
}

// LokiRuleGroupSpec defines the spec for a Loki alerting or recording rule.
type LokiRuleGroupSpec struct {
    // The name of the alert. Must be a valid label value.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Alert string `json:"alert,omitempty"`

    // The name of the time series to output to. Must be a valid metric name.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Record string `json:"record,omitempty"`

    // The LogQL expression to evaluate. Every evaluation cycle this is
    // evaluated at the current time, and all resultant time series become
    // pending/firing alerts.
    //
    // +required
    // +kubebuilder:validation:Required
    Expr string `json:"expr"`

    // Alerts are considered firing once they have been returned for this long.
    // Alerts which have not yet fired for long enough are considered pending.
    //
    // +optional
    // +kubebuilder:validation:Optional
    For EvaluationDuration `json:"for,omitempty"`

    // Annotations to add to each alert.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Annotations map[string]string `json:"annotations,omitempty"`

    // Labels to add to each alert.
    //
    // +optional
    // +kubebuilder:validation:Optional
    Labels map[string]string `json:"labels,omitempty"`
}

// LokiRuleConditionType defines the type for LokiRule conditions.
type LokiRuleConditionType string

const (
    // ConditionValid defines the condition when all given LokiRule groups expressions are valid.
    ConditionValid LokiRuleConditionType = "Valid"
    // ConditionInvalid defines the condition when at least one LokiRule group definition is invalid.
    ConditionInvalid LokiRuleConditionType = "Invalid"
)

// LokiRuleConditionReason defines the type for valid reasons of a LokiRule condition.
type LokiRuleConditionReason string

const (
    // ReasonAllRulesValid when no rule validation occurred.
    ReasonAllRulesValid LokiRuleConditionReason = "AllRulesValid"
    // ReasonAmbiguousRuleConfig when a loki rule includes alerting and recording rule fields.
    ReasonAmbiguousRuleConfig LokiRuleConditionReason = "AmbiguousRuleConfig"
    // ReasonInvalidAlertingRuleConfig when a loki alerting rule has an invalid period for firing alerts.
    ReasonInvalidAlertingRuleConfig LokiRuleConditionReason = "InvalidAlertingRuleConfig"
    // ReasonInvalidRecordingRuleConfig when a loki recording rules has an invalid record label name.
    ReasonInvalidRecordingRuleConfig LokiRuleConditionReason = "InvalidRecordingRuleConfig"
    // ReasonInvalidRuleExpression when a loki rule expression cannot be parsed by the LogQL parser.
    ReasonInvalidRuleExpression LokiRuleConditionReason = "InvalidRuleExpression"
    // ReasonNotUniqueRuleGroupName when a loki rule group name is not unique.
    ReasonNotUniqueRuleGroupName LokiRuleConditionReason = "NotUniqueRuleGroupName"
)

// LokiRuleStatus defines the observed state of LokiRule
type LokiRuleStatus struct {
    // Conditions of the LokiRule generation health.
    //
    // +optional
    // +kubebuilder:validation:Optional
    // +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// LokiRule is the Schema for the lokirules API
type LokiRule struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   LokiRuleSpec   `json:"spec,omitempty"`
    Status LokiRuleStatus `json:"status,omitempty"`
}
```

#### LokiRulerConfig definition

The following CRD defines the ruler configuration to access AlertManager hosts and to access a global Remote-Write-Endpoint (e.g. Prometheus, Thanos, Cortex). The types are split in three groups:
1. `LokiRulerSpec`: This spec includes general settings like evalution and poll interval.
2. `AlertManagerSpec`: This spec includes all settings for pushing alert notifications to a list of AlertManager hosts.
3. `RemoteWriteSpec`: This spec includes all settings to configure a single global remote write endpoint to send recorded metrics.

**Note**: Sensitive authorization information are provided by a Kubernetes Secret resource, i.e. basic auth user/password, header authorization (See `AuthorizationSecretName`). The Secret is required to live in the same namespace as the `LokiRulerConfig` resource.

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

// LokiRulerSpec defines the desired state of LokiRuler
type LokiRulerSpec struct {
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

// LokiRulerStatus defines the observed state of LokiRuler
type LokiRulerStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// LokiRuler is the Schema for the lokirulers API
type LokiRuler struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   LokiRulerSpec   `json:"spec,omitempty"`
    Status LokiRulerStatus `json:"status,omitempty"`
}
```

### Implementation Details/Notes/Constraints

#### LokiRule reconciliation

`LokiRule` custom resources are transformed into a single ruler configuration file for a given `LokiStack` by `StackName`. The following two-step approach is taken:
1. A dedicated `LokiRuleController` reads all `LokiRule` resources per namespace and creates for each an entry in a single Kubernetes ConfigMap for the given `StackName`, e.g.
```yaml
apiVersion: loki.grafana.com/v1beta1
kind: LokiRule
metadata:
  name: loki-rule-a
  namespace: ns-a
spec:
  stackName: lokistack-dev
---
apiVersion: loki.grafana.com/v1beta1
kind: LokiRule
metadata:
  name: loki-rule-b
  namespace: ns-b
spec:
  stackName: lokistack-dev
```

results in:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: lokistack-dev-rules
  namespace: lokistack-ns
data:
  "ns-a-loki-rule-a.yaml": |
    ...
  "ns-b-loki-rule-b.yaml": |
   ...
```

2. The `LokiStackController` listens for `LokiRule` create/update/delete events and for a given `StackName` it configures the Loki ruler pod to mount the appropriate ConfigMap. The convention here is to use the `StackName` as a prefix for the ConfigMap name, e.g. for a stack name `lokistack-dev` the ConfigMap name is expected to be `lokistack-dev-rules`.
3. The `LokiStackController` compiles on each create/update/delete event a SHA1 of the ConfigMap data entries and appends a dedicated annotation `loki.grafana.com/rules-config-hash` to the ruler pod spec. If this hash changes, the change applies to the annotation value and the ruler pods get restarted.

In summary this approach allows to group all `LokiRule` custom resources in the **same** namespace as the `LokiStack` in a single ConfigMap, i.e. where the stack runs is the place where the rules live.

#### LokiRulerConfig reconciliation

The `LokiRulerConfig` custom resource is not transformed into a specific ruler configuration file per se. It is considered an optional companion to the `LokiStack` custom resource. Thus the existing `LokiStackController` listens for `LokiRuleConfig` create/update/delete events on the same namespace as for a `LokiStack` custom resource and reconciles the final ruler configuration.

In detail the separation of concerns between both CRDs looks like:
1. `LokiStack`: Defines all required aspects to spin up a Loki ruler component, i.e. size, node placement. In addition it controls the common config settings for the Work-Ahead-Log and Ring configuration.
2. `LokiRulerConfig`: Defines only the global runtime settings for the ruler, i.e. evaluation/poll intervals, AlertManager configuration, remote write client configuration.

#### General constraints

1. The above `LokiRulerConfig` is limited to a single global remote write endpoint for exporting metrics from recording rules. This leaves solving multi-tenancy issues on the remote write server side and provide means (e.g. headers) for spliting/amending metrics ingestion per tenant. On the other hand it simplifies ruler operations as it does not require to spin concurrent remote write clients per tenant.
2. Additionally the `LokiRulerConfig** requires a user-provided Kubernetes Secret for sensitive information. This adds an extra dependency that requires validation inside the controller-loop.

### Risks and Mitigations

#### Loki Ruler Configuration versioning

**Risk**: The `LokiRulerConfig` CRD exposes an almost identical [ruler config](https://grafana.com/docs/loki/latest/configuration/#ruler) to support AlertManager and Remote-Write. Both sub-specifications are subject of change to the API version of the server endpoint (i.e. AlertManager, Remote-Write). Although AlertManager is using a versioned approach e.g. `EnableV2` switch, we miss a similar approach accross remote write endpoints.

**Mitigation**: We require a support matrix for `LokiRulerConfig` versions to AlertManager API versions and Remote Write implementations. For later we could use a list of releases (e.g. Prometheus 2.x, Thanos 0.20.z).

#### Single CRD for Alerting and Recording Rules

**Risk**: `LokiRule` is an amalgam of types for alerting and recording rules. This requires custom validation to inform the user of bad inputs and in turn extra maintaince over time. In addition alerting rules might deviate with new features in future (See [Feature Request: alert relabel configs](https://github.com/grafana/loki/issues/5886)). Thus a CRD combining both rules types might become confusing and error-prone from a user experience perspective.

**Mitigation**: Either split the two types in an `AlertingRule` and `RecordingRule` type now or in a future v2 version. Later would probably be harder to support seamless migrations from the Kubernetes API server side.

#### Single ConfigMap for all LokiRule instances

**Risk**: The proposed reconciliation approach manifests that all `LokiRule` instances are transformed to individual entries in a single ConfigMap. Alerting/Recording rules can be become quite big (e.g. large LogQL expresssions). As per ConfigMaps represent Kubernetes resource stored in etcd, this might hit some store limits (See [1MiB limit](https://kubernetes.io/docs/concepts/configuration/configmap/#motivation)). Therefore storing all rules in a single ConfigMap might become impossible over time or on large clusters.

**Mitigation**: Introduce some sort of sharding of LokiRules into multiple ConfigMap resources.

## Design Details

### Open Questions [optional]

1. Do we need to split the `LokiRule` into two types `RecordingRule` and `AlertingRule`?
2. Do we need to add support for Remote Write Configuration per tenant? (See `ruler_remote_write_*` parameters in [limits_config](https://grafana.com/docs/loki/latest/configuration/#limits_config))
3. Do we need to add support for the `ruler_evaluation_delay_duration`, `ruler_max_rules_per_rule_group` and `ruler_max_rule_groups_per_tenant` limits?

## Implementation History

* 2022-04-21: Initial draft proposal
* 2022-04-21: Spike implementation for `LokiRule` reconciliation and `LokiRulerConfig` types. (See [PR](https://github.com/grafana/loki/pull/5986))

## Drawbacks

The above proposed design and implementation for LokiStack Ruler support adds a significant amount of new APIs as well as complexity into exposing a declarative approach **only** for the ruler component. Regardless the hard effort to minimize the amount of configuration settings in the proposed CRDs it remains still a huge addition to the operator code base. In contrast to the existing `LokiStack` CRD that is a very slim set of Loki settings (Note: without considering the gateway tenant configuration), the `LokiRulerConfig` is almost one-to-one identical to the [ruler config](https://grafana.com/docs/loki/latest/configuration/#ruler).

## Alternatives

Alternatives providing support for Loki alerting and recording rules:
1. [opsgy/loki-rule-operator](https://github.com/opsgy/loki-rule-operator)
2. Add here anything missed
