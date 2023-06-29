---
title: "API"
description: "Generated API docs for the Loki Operator"
lead: ""
draft: false
images: []
menu:
  docs:
    parent: "operator"
weight: 1000
toc: true
---
This Document contains the types introduced by the Loki Operator to be consumed by users.
> This page is automatically generated with `gen-crd-api-reference-docs`.
# loki.grafana.com/v1 { #loki-grafana-com-v1 }
<div>
<p>Package v1 contains API Schema definitions for the loki v1 API group</p>
</div>
<b>Resource Types:</b>

## AlertManagerClientBasicAuth { #loki-grafana-com-v1-AlertManagerClientBasicAuth }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerClientConfig">AlertManagerClientConfig</a>)
</p>
<div>
<p>AlertManagerClientBasicAuth defines the basic authentication configuration for reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>username</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The subject&rsquo;s username for the basic authentication configuration.</p>
</td>
</tr>
<tr>
<td>
<code>password</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The subject&rsquo;s password for the basic authentication configuration.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerClientConfig { #loki-grafana-com-v1-AlertManagerClientConfig }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerSpec">AlertManagerSpec</a>)
</p>
<div>
<p>AlertManagerClientConfig defines the client configuration for reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tls</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerClientTLSConfig">
AlertManagerClientTLSConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>TLS configuration for reaching the alertmanager endpoints.</p>
</td>
</tr>
<tr>
<td>
<code>headerAuth</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerClientHeaderAuth">
AlertManagerClientHeaderAuth
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Header authentication configuration for reaching the alertmanager endpoints.</p>
</td>
</tr>
<tr>
<td>
<code>basicAuth</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerClientBasicAuth">
AlertManagerClientBasicAuth
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Basic authentication configuration for reaching the alertmanager endpoints.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerClientHeaderAuth { #loki-grafana-com-v1-AlertManagerClientHeaderAuth }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerClientConfig">AlertManagerClientConfig</a>)
</p>
<div>
<p>AlertManagerClientHeaderAuth defines the header configuration reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>type</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The authentication type for the header authentication configuration.</p>
</td>
</tr>
<tr>
<td>
<code>credentials</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The credentials for the header authentication configuration.</p>
</td>
</tr>
<tr>
<td>
<code>credentialsFile</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The credentials file for the Header authentication configuration. It is mutually exclusive with <code>credentials</code>.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerClientTLSConfig { #loki-grafana-com-v1-AlertManagerClientTLSConfig }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerClientConfig">AlertManagerClientConfig</a>)
</p>
<div>
<p>AlertManagerClientTLSConfig defines the TLS configuration for reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>caPath</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The CA certificate file path for the TLS configuration.</p>
</td>
</tr>
<tr>
<td>
<code>serverName</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The server name to validate in the alertmanager server certificates.</p>
</td>
</tr>
<tr>
<td>
<code>certPath</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The client-side certificate file path for the TLS configuration.</p>
</td>
</tr>
<tr>
<td>
<code>keyPath</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The client-side key file path for the TLS configuration.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerDiscoverySpec { #loki-grafana-com-v1-AlertManagerDiscoverySpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerSpec">AlertManagerSpec</a>)
</p>
<div>
<p>AlertManagerDiscoverySpec defines the configuration to use DNS resolution for AlertManager hosts.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>enableSRV</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Use DNS SRV records to discover Alertmanager hosts.</p>
</td>
</tr>
<tr>
<td>
<code>refreshInterval</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>How long to wait between refreshing DNS resolutions of Alertmanager hosts.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerNotificationQueueSpec { #loki-grafana-com-v1-AlertManagerNotificationQueueSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerSpec">AlertManagerSpec</a>)
</p>
<div>
<p>AlertManagerNotificationQueueSpec defines the configuration for AlertManager notification settings.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>capacity</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Capacity of the queue for notifications to be sent to the Alertmanager.</p>
</td>
</tr>
<tr>
<td>
<code>timeout</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>HTTP timeout duration when sending notifications to the Alertmanager.</p>
</td>
</tr>
<tr>
<td>
<code>forOutageTolerance</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Max time to tolerate outage for restoring &ldquo;for&rdquo; state of alert.</p>
</td>
</tr>
<tr>
<td>
<code>forGracePeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum duration between alert and restored &ldquo;for&rdquo; state. This is maintained
only for alerts with configured &ldquo;for&rdquo; time greater than the grace period.</p>
</td>
</tr>
<tr>
<td>
<code>resendDelay</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum amount of time to wait before resending an alert to Alertmanager.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerSpec { #loki-grafana-com-v1-AlertManagerSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RulerConfigSpec">RulerConfigSpec</a>, <a href="#loki-grafana-com-v1-RulerOverrides">RulerOverrides</a>)
</p>
<div>
<p>AlertManagerSpec defines the configuration for ruler&rsquo;s alertmanager connectivity.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>externalUrl</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>URL for alerts return path.</p>
</td>
</tr>
<tr>
<td>
<code>externalLabels</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Additional labels to add to all alerts.</p>
</td>
</tr>
<tr>
<td>
<code>enableV2</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>If enabled, then requests to Alertmanager use the v2 API.</p>
</td>
</tr>
<tr>
<td>
<code>endpoints</code><br/>
<em>
[]string
</em>
</td>
<td>
<p>List of AlertManager URLs to send notifications to. Each Alertmanager URL is treated as
a separate group in the configuration. Multiple Alertmanagers in HA per group can be
supported by using DNS resolution (See EnableDNSDiscovery).</p>
</td>
</tr>
<tr>
<td>
<code>discovery</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerDiscoverySpec">
AlertManagerDiscoverySpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for DNS-based discovery of AlertManager hosts.</p>
</td>
</tr>
<tr>
<td>
<code>notificationQueue</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerNotificationQueueSpec">
AlertManagerNotificationQueueSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for the notification queue to AlertManager hosts.</p>
</td>
</tr>
<tr>
<td>
<code>relabelConfigs</code><br/>
<em>
<a href="#loki-grafana-com-v1-RelabelConfig">
[]RelabelConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of alert relabel configurations.</p>
</td>
</tr>
<tr>
<td>
<code>client</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerClientConfig">
AlertManagerClientConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Client configuration for reaching the alertmanager endpoint.</p>
</td>
</tr>
</tbody>
</table>

## AlertingRule { #loki-grafana-com-v1-AlertingRule }
<div>
<p>AlertingRule is the Schema for the alertingrules API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertingRuleSpec">
AlertingRuleSpec
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertingRuleStatus">
AlertingRuleStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## AlertingRuleGroup { #loki-grafana-com-v1-AlertingRuleGroup }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertingRuleSpec">AlertingRuleSpec</a>)
</p>
<div>
<p>AlertingRuleGroup defines a group of Loki alerting rules.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of the alerting rule group. Must be unique within all alerting rules.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval defines the time interval between evaluation of the given
alerting rule.</p>
</td>
</tr>
<tr>
<td>
<code>limit</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Limit defines the number of alerts an alerting rule can produce. 0 is no limit.</p>
</td>
</tr>
<tr>
<td>
<code>rules</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertingRuleGroupSpec">
[]*AlertingRuleGroupSpec
</a>
</em>
</td>
<td>
<p>Rules defines a list of alerting rules</p>
</td>
</tr>
</tbody>
</table>

## AlertingRuleGroupSpec { #loki-grafana-com-v1-AlertingRuleGroupSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertingRuleGroup">AlertingRuleGroup</a>)
</p>
<div>
<p>AlertingRuleGroupSpec defines the spec for a Loki alerting rule.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>alert</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The name of the alert. Must be a valid label value.</p>
</td>
</tr>
<tr>
<td>
<code>expr</code><br/>
<em>
string
</em>
</td>
<td>
<p>The LogQL expression to evaluate. Every evaluation cycle this is
evaluated at the current time, and all resultant time series become
pending/firing alerts.</p>
</td>
</tr>
<tr>
<td>
<code>for</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Alerts are considered firing once they have been returned for this long.
Alerts which have not yet fired for long enough are considered pending.</p>
</td>
</tr>
<tr>
<td>
<code>annotations</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Annotations to add to each alert.</p>
</td>
</tr>
<tr>
<td>
<code>labels</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Labels to add to each alert.</p>
</td>
</tr>
</tbody>
</table>

## AlertingRuleSpec { #loki-grafana-com-v1-AlertingRuleSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertingRule">AlertingRule</a>)
</p>
<div>
<p>AlertingRuleSpec defines the desired state of AlertingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tenantID</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantID of tenant where the alerting rules are evaluated in.</p>
</td>
</tr>
<tr>
<td>
<code>groups</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertingRuleGroup">
[]*AlertingRuleGroup
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of groups for alerting rules.</p>
</td>
</tr>
</tbody>
</table>

## AlertingRuleStatus { #loki-grafana-com-v1-AlertingRuleStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertingRule">AlertingRule</a>)
</p>
<div>
<p>AlertingRuleStatus defines the observed state of AlertingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the AlertingRule generation health.</p>
</td>
</tr>
</tbody>
</table>

## AuthenticationSpec { #loki-grafana-com-v1-AuthenticationSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-TenantsSpec">TenantsSpec</a>)
</p>
<div>
<p>AuthenticationSpec defines the oidc configuration per tenant for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tenantName</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantName defines the name of the tenant.</p>
</td>
</tr>
<tr>
<td>
<code>tenantId</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantID defines the id of the tenant.</p>
</td>
</tr>
<tr>
<td>
<code>oidc</code><br/>
<em>
<a href="#loki-grafana-com-v1-OIDCSpec">
OIDCSpec
</a>
</em>
</td>
<td>
<p>OIDC defines the spec for the OIDC tenant&rsquo;s authentication.</p>
</td>
</tr>
</tbody>
</table>

## AuthorizationSpec { #loki-grafana-com-v1-AuthorizationSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-TenantsSpec">TenantsSpec</a>)
</p>
<div>
<p>AuthorizationSpec defines the opa, role bindings and roles
configuration per tenant for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>opa</code><br/>
<em>
<a href="#loki-grafana-com-v1-OPASpec">
OPASpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>OPA defines the spec for the third-party endpoint for tenant&rsquo;s authorization.</p>
</td>
</tr>
<tr>
<td>
<code>roles</code><br/>
<em>
<a href="#loki-grafana-com-v1-RoleSpec">
[]RoleSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Roles defines a set of permissions to interact with a tenant.</p>
</td>
</tr>
<tr>
<td>
<code>roleBindings</code><br/>
<em>
<a href="#loki-grafana-com-v1-RoleBindingsSpec">
[]RoleBindingsSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>RoleBindings defines configuration to bind a set of roles to a set of subjects.</p>
</td>
</tr>
</tbody>
</table>

## ClusterProxy { #loki-grafana-com-v1-ClusterProxy }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>ClusterProxy is the Proxy configuration when the cluster is behind a Proxy.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>httpProxy</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>HTTPProxy configures the HTTP_PROXY/http_proxy env variable.</p>
</td>
</tr>
<tr>
<td>
<code>httpsProxy</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>HTTPSProxy configures the HTTPS_PROXY/https_proxy env variable.</p>
</td>
</tr>
<tr>
<td>
<code>noProxy</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>NoProxy configures the NO_PROXY/no_proxy env variable.</p>
</td>
</tr>
</tbody>
</table>

## HashRingSpec { #loki-grafana-com-v1-HashRingSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>HashRingSpec defines the hash ring configuration</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>type</code><br/>
<em>
<a href="#loki-grafana-com-v1-HashRingType">
HashRingType
</a>
</em>
</td>
<td>
<p>Type of hash ring implementation that should be used</p>
</td>
</tr>
<tr>
<td>
<code>memberlist</code><br/>
<em>
<a href="#loki-grafana-com-v1-MemberListSpec">
MemberListSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>MemberList configuration spec</p>
</td>
</tr>
</tbody>
</table>

## HashRingType { #loki-grafana-com-v1-HashRingType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-HashRingSpec">HashRingSpec</a>)
</p>
<div>
<p>HashRingType defines the type of hash ring which can be used with the Loki cluster.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;memberlist&#34;</p></td>
<td><p>HashRingMemberList when using memberlist for the distributed hash ring.</p>
</td>
</tr></tbody>
</table>

## IngestionLimitSpec { #loki-grafana-com-v1-IngestionLimitSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LimitsTemplateSpec">LimitsTemplateSpec</a>)
</p>
<div>
<p>IngestionLimitSpec defines the limits applied at the ingestion path.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>ingestionRate</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>IngestionRate defines the sample size per second. Units MB.</p>
</td>
</tr>
<tr>
<td>
<code>ingestionBurstSize</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>IngestionBurstSize defines the local rate-limited sample size per
distributor replica. It should be set to the set at least to the
maximum logs size expected in a single push request.</p>
</td>
</tr>
<tr>
<td>
<code>maxLabelNameLength</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLabelNameLength defines the maximum number of characters allowed
for label keys in log streams.</p>
</td>
</tr>
<tr>
<td>
<code>maxLabelValueLength</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLabelValueLength defines the maximum number of characters allowed
for label values in log streams.</p>
</td>
</tr>
<tr>
<td>
<code>maxLabelNamesPerSeries</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLabelNamesPerSeries defines the maximum number of label names per series
in each log stream.</p>
</td>
</tr>
<tr>
<td>
<code>maxGlobalStreamsPerTenant</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxGlobalStreamsPerTenant defines the maximum number of active streams
per tenant, across the cluster.</p>
</td>
</tr>
<tr>
<td>
<code>maxLineSize</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLineSize defines the maximum line size on ingestion path. Units in Bytes.</p>
</td>
</tr>
<tr>
<td>
<code>perStreamRateLimit</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>PerStreamRateLimit defines the maximum byte rate per second per stream. Units MB.</p>
</td>
</tr>
<tr>
<td>
<code>perStreamRateLimitBurst</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>PerStreamRateLimitBurst defines the maximum burst bytes per stream. Units MB.</p>
</td>
</tr>
</tbody>
</table>

## InstanceAddrType { #loki-grafana-com-v1-InstanceAddrType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-MemberListSpec">MemberListSpec</a>)
</p>
<div>
<p>InstanceAddrType defines the type of pod network to use for advertising IPs to the ring.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;default&#34;</p></td>
<td><p>InstanceAddrDefault when using the first from any private network interfaces (RFC 1918 and RFC 6598).</p>
</td>
</tr><tr><td><p>&#34;podIP&#34;</p></td>
<td><p>InstanceAddrPodIP when using the public pod IP from the cluster&rsquo;s pod network.</p>
</td>
</tr></tbody>
</table>

## LimitsSpec { #loki-grafana-com-v1-LimitsSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>LimitsSpec defines the spec for limits applied at ingestion or query
path across the cluster or per tenant.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>global</code><br/>
<em>
<a href="#loki-grafana-com-v1-LimitsTemplateSpec">
LimitsTemplateSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Global defines the limits applied globally across the cluster.</p>
</td>
</tr>
<tr>
<td>
<code>tenants</code><br/>
<em>
<a href="#loki-grafana-com-v1-LimitsTemplateSpec">
map[string]github.com/grafana/loki/operator/apis/loki/v1.LimitsTemplateSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Tenants defines the limits applied per tenant.</p>
</td>
</tr>
</tbody>
</table>

## LimitsTemplateSpec { #loki-grafana-com-v1-LimitsTemplateSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LimitsSpec">LimitsSpec</a>)
</p>
<div>
<p>LimitsTemplateSpec defines the limits  applied at ingestion or query path.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>ingestion</code><br/>
<em>
<a href="#loki-grafana-com-v1-IngestionLimitSpec">
IngestionLimitSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>IngestionLimits defines the limits applied on ingested log streams.</p>
</td>
</tr>
<tr>
<td>
<code>queries</code><br/>
<em>
<a href="#loki-grafana-com-v1-QueryLimitSpec">
QueryLimitSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>QueryLimits defines the limit applied on querying log streams.</p>
</td>
</tr>
<tr>
<td>
<code>retention</code><br/>
<em>
<a href="#loki-grafana-com-v1-RetentionLimitSpec">
RetentionLimitSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Retention defines how long logs are kept in storage.</p>
</td>
</tr>
</tbody>
</table>

## LokiComponentSpec { #loki-grafana-com-v1-LokiComponentSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiTemplateSpec">LokiTemplateSpec</a>)
</p>
<div>
<p>LokiComponentSpec defines the requirements to configure scheduling
of each loki component individually.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>replicas</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Replicas defines the number of replica pods of the component.</p>
</td>
</tr>
<tr>
<td>
<code>nodeSelector</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>NodeSelector defines the labels required by a node to schedule
the component onto it.</p>
</td>
</tr>
<tr>
<td>
<code>tolerations</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#toleration-v1-core">
[]Kubernetes core/v1.Toleration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Tolerations defines the tolerations required by a node to schedule
the component onto it.</p>
</td>
</tr>
<tr>
<td>
<code>podAntiAffinity</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#podantiaffinity-v1-core">
Kubernetes core/v1.PodAntiAffinity
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>PodAntiAffinity defines the pod anti affinity scheduling rules to schedule pods
of a component.</p>
</td>
</tr>
</tbody>
</table>

## LokiStack { #loki-grafana-com-v1-LokiStack }
<div>
<p>LokiStack is the Schema for the lokistacks API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiStackSpec">
LokiStackSpec
</a>
</em>
</td>
<td>
<p>LokiStack CR spec field.</p>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiStackStatus">
LokiStackStatus
</a>
</em>
</td>
<td>
<p>LokiStack CR spec Status.</p>
</td>
</tr>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
</tbody>
</table>

## LokiStackComponentStatus { #loki-grafana-com-v1-LokiStackComponentStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackStatus">LokiStackStatus</a>)
</p>
<div>
<p>LokiStackComponentStatus defines the map of per pod status per LokiStack component.
Each component is represented by a separate map of v1.Phase to a list of pods.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>compactor</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Compactor is a map to the pod status of the compactor pod.</p>
</td>
</tr>
<tr>
<td>
<code>distributor</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Distributor is a map to the per pod status of the distributor deployment</p>
</td>
</tr>
<tr>
<td>
<code>indexGateway</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>IndexGateway is a map to the per pod status of the index gateway statefulset</p>
</td>
</tr>
<tr>
<td>
<code>ingester</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ingester is a map to the per pod status of the ingester statefulset</p>
</td>
</tr>
<tr>
<td>
<code>querier</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Querier is a map to the per pod status of the querier deployment</p>
</td>
</tr>
<tr>
<td>
<code>queryFrontend</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>QueryFrontend is a map to the per pod status of the query frontend deployment</p>
</td>
</tr>
<tr>
<td>
<code>gateway</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Gateway is a map to the per pod status of the lokistack gateway deployment.</p>
</td>
</tr>
<tr>
<td>
<code>ruler</code><br/>
<em>
<a href="#loki-grafana-com-v1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ruler is a map to the per pod status of the lokistack ruler statefulset.</p>
</td>
</tr>
</tbody>
</table>

## LokiStackConditionReason { #loki-grafana-com-v1-LokiStackConditionReason }
(<code>string</code> alias)
<div>
<p>LokiStackConditionReason defines the type for valid reasons of a Loki deployment conditions.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;FailedCertificateRotation&#34;</p></td>
<td><p>ReasonFailedCertificateRotation when the reconciler cannot rotate any of the required TLS certificates.</p>
</td>
</tr><tr><td><p>&#34;FailedComponents&#34;</p></td>
<td><p>ReasonFailedComponents when all/some LokiStack components fail to roll out.</p>
</td>
</tr><tr><td><p>&#34;InvalidGatewayTenantSecret&#34;</p></td>
<td><p>ReasonInvalidGatewayTenantSecret when the format of the secret is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidObjectStorageCAConfigMap&#34;</p></td>
<td><p>ReasonInvalidObjectStorageCAConfigMap when the format of the CA configmap is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidObjectStorageSchema&#34;</p></td>
<td><p>ReasonInvalidObjectStorageSchema when the spec contains an invalid schema(s).</p>
</td>
</tr><tr><td><p>&#34;InvalidObjectStorageSecret&#34;</p></td>
<td><p>ReasonInvalidObjectStorageSecret when the format of the secret is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidReplicationConfiguration&#34;</p></td>
<td><p>ReasonInvalidReplicationConfiguration when the configurated replication factor is not valid
with the select cluster size.</p>
</td>
</tr><tr><td><p>&#34;InvalidRulerSecret&#34;</p></td>
<td><p>ReasonInvalidRulerSecret when the format of the ruler remote write authorization secret is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidTenantsConfiguration&#34;</p></td>
<td><p>ReasonInvalidTenantsConfiguration when the tenant configuration provided is invalid.</p>
</td>
</tr><tr><td><p>&#34;MissingGatewayOpenShiftBaseDomain&#34;</p></td>
<td><p>ReasonMissingGatewayOpenShiftBaseDomain when the reconciler cannot lookup the OpenShift DNS base domain.</p>
</td>
</tr><tr><td><p>&#34;MissingGatewayTenantSecret&#34;</p></td>
<td><p>ReasonMissingGatewayTenantSecret when the required tenant secret
for authentication is missing.</p>
</td>
</tr><tr><td><p>&#34;MissingObjectStorageCAConfigMap&#34;</p></td>
<td><p>ReasonMissingObjectStorageCAConfigMap when the required configmap to verify object storage
certificates is missing.</p>
</td>
</tr><tr><td><p>&#34;MissingObjectStorageSecret&#34;</p></td>
<td><p>ReasonMissingObjectStorageSecret when the required secret to store logs to object
storage is missing.</p>
</td>
</tr><tr><td><p>&#34;MissingRulerSecret&#34;</p></td>
<td><p>ReasonMissingRulerSecret when the required secret to authorization remote write connections
for the ruler is missing.</p>
</td>
</tr><tr><td><p>&#34;PendingComponents&#34;</p></td>
<td><p>ReasonPendingComponents when all/some LokiStack components pending dependencies</p>
</td>
</tr><tr><td><p>&#34;ReasonQueryTimeoutInvalid&#34;</p></td>
<td><p>ReasonQueryTimeoutInvalid when the QueryTimeout can not be parsed.</p>
</td>
</tr><tr><td><p>&#34;ReadyComponents&#34;</p></td>
<td><p>ReasonReadyComponents when all LokiStack components are ready to serve traffic.</p>
</td>
</tr></tbody>
</table>

## LokiStackConditionType { #loki-grafana-com-v1-LokiStackConditionType }
(<code>string</code> alias)
<div>
<p>LokiStackConditionType deifnes the type of condition types of a Loki deployment.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;Degraded&#34;</p></td>
<td><p>ConditionDegraded defines the condition that some or all components in the Loki deployment
are degraded or the cluster cannot connect to object storage.</p>
</td>
</tr><tr><td><p>&#34;Failed&#34;</p></td>
<td><p>ConditionFailed defines the condition that components in the Loki deployment failed to roll out.</p>
</td>
</tr><tr><td><p>&#34;Pending&#34;</p></td>
<td><p>ConditionPending defines the condition that some or all components are in pending state.</p>
</td>
</tr><tr><td><p>&#34;Ready&#34;</p></td>
<td><p>ConditionReady defines the condition that all components in the Loki deployment are ready.</p>
</td>
</tr></tbody>
</table>

## LokiStackSizeType { #loki-grafana-com-v1-LokiStackSizeType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>LokiStackSizeType declares the type for loki cluster scale outs.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;1x.demo&#34;</p></td>
<td><p>SizeOneXDemo defines the size of a single Loki deployment
with tiny resource requirements and without HA support.
This size is intended to run in single-node clusters on laptops,
it is only useful for very light testing, demonstrations, or prototypes.
There are no ingestion/query performance guarantees.
DO NOT USE THIS IN PRODUCTION!</p>
</td>
</tr><tr><td><p>&#34;1x.extra-small&#34;</p></td>
<td><p>SizeOneXExtraSmall defines the size of a single Loki deployment
with extra small resources/limits requirements and without HA support.
This size is ultimately dedicated for development and demo purposes.
DO NOT USE THIS IN PRODUCTION!</p>
<p>FIXME: Add clear description of ingestion/query performance expectations.</p>
</td>
</tr><tr><td><p>&#34;1x.medium&#34;</p></td>
<td><p>SizeOneXMedium defines the size of a single Loki deployment
with small resources/limits requirements and HA support for all
Loki components. This size is dedicated for setup <strong>with</strong> the
requirement for single replication factor and auto-compaction.</p>
<p>FIXME: Add clear description of ingestion/query performance expectations.</p>
</td>
</tr><tr><td><p>&#34;1x.small&#34;</p></td>
<td><p>SizeOneXSmall defines the size of a single Loki deployment
with small resources/limits requirements and HA support for all
Loki components. This size is dedicated for setup <strong>without</strong> the
requirement for single replication factor and auto-compaction.</p>
<p>FIXME: Add clear description of ingestion/query performance expectations.</p>
</td>
</tr></tbody>
</table>

## LokiStackSpec { #loki-grafana-com-v1-LokiStackSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStack">LokiStack</a>)
</p>
<div>
<p>LokiStackSpec defines the desired state of LokiStack</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>managementState</code><br/>
<em>
<a href="#loki-grafana-com-v1-ManagementStateType">
ManagementStateType
</a>
</em>
</td>
<td>
<p>ManagementState defines if the CR should be managed by the operator or not.
Default is managed.</p>
</td>
</tr>
<tr>
<td>
<code>size</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiStackSizeType">
LokiStackSizeType
</a>
</em>
</td>
<td>
<p>Size defines one of the support Loki deployment scale out sizes.</p>
</td>
</tr>
<tr>
<td>
<code>hashRing</code><br/>
<em>
<a href="#loki-grafana-com-v1-HashRingSpec">
HashRingSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>HashRing defines the spec for the distributed hash ring configuration.</p>
</td>
</tr>
<tr>
<td>
<code>storage</code><br/>
<em>
<a href="#loki-grafana-com-v1-ObjectStorageSpec">
ObjectStorageSpec
</a>
</em>
</td>
<td>
<p>Storage defines the spec for the object storage endpoint to store logs.</p>
</td>
</tr>
<tr>
<td>
<code>storageClassName</code><br/>
<em>
string
</em>
</td>
<td>
<p>Storage class name defines the storage class for ingester/querier PVCs.</p>
</td>
</tr>
<tr>
<td>
<code>proxy</code><br/>
<em>
<a href="#loki-grafana-com-v1-ClusterProxy">
ClusterProxy
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Proxy defines the spec for the object proxy to configure cluster proxy information.</p>
</td>
</tr>
<tr>
<td>
<code>replicationFactor</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Deprecated: Please use replication.factor instead. This field will be removed in future versions of this CRD.
ReplicationFactor defines the policy for log stream replication.</p>
</td>
</tr>
<tr>
<td>
<code>replication</code><br/>
<em>
<a href="#loki-grafana-com-v1-ReplicationSpec">
ReplicationSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Replication defines the configuration for Loki data replication.</p>
</td>
</tr>
<tr>
<td>
<code>rules</code><br/>
<em>
<a href="#loki-grafana-com-v1-RulesSpec">
RulesSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Rules defines the spec for the ruler component.</p>
</td>
</tr>
<tr>
<td>
<code>limits</code><br/>
<em>
<a href="#loki-grafana-com-v1-LimitsSpec">
LimitsSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Limits defines the limits to be applied to log stream processing.</p>
</td>
</tr>
<tr>
<td>
<code>template</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiTemplateSpec">
LokiTemplateSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Template defines the resource/limits/tolerations/nodeselectors per component.</p>
</td>
</tr>
<tr>
<td>
<code>tenants</code><br/>
<em>
<a href="#loki-grafana-com-v1-TenantsSpec">
TenantsSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Tenants defines the per-tenant authentication and authorization spec for the lokistack-gateway component.</p>
</td>
</tr>
</tbody>
</table>

## LokiStackStatus { #loki-grafana-com-v1-LokiStackStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStack">LokiStack</a>)
</p>
<div>
<p>LokiStackStatus defines the observed state of LokiStack</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>components</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiStackComponentStatus">
LokiStackComponentStatus
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Components provides summary of all Loki pod status grouped
per component.</p>
</td>
</tr>
<tr>
<td>
<code>storage</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiStackStorageStatus">
LokiStackStorageStatus
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Storage provides summary of all changes that have occurred
to the storage configuration.</p>
</td>
</tr>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the Loki deployment health.</p>
</td>
</tr>
</tbody>
</table>

## LokiStackStorageStatus { #loki-grafana-com-v1-LokiStackStorageStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackStatus">LokiStackStatus</a>)
</p>
<div>
<p>LokiStackStorageStatus defines the observed state of
the Loki storage configuration.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>schemas</code><br/>
<em>
<a href="#loki-grafana-com-v1-ObjectStorageSchema">
[]ObjectStorageSchema
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Schemas is a list of schemas which have been applied
to the LokiStack.</p>
</td>
</tr>
</tbody>
</table>

## LokiTemplateSpec { #loki-grafana-com-v1-LokiTemplateSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>LokiTemplateSpec defines the template of all requirements to configure
scheduling of all Loki components to be deployed.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>compactor</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Compactor defines the compaction component spec.</p>
</td>
</tr>
<tr>
<td>
<code>distributor</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Distributor defines the distributor component spec.</p>
</td>
</tr>
<tr>
<td>
<code>ingester</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ingester defines the ingester component spec.</p>
</td>
</tr>
<tr>
<td>
<code>querier</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Querier defines the querier component spec.</p>
</td>
</tr>
<tr>
<td>
<code>queryFrontend</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>QueryFrontend defines the query frontend component spec.</p>
</td>
</tr>
<tr>
<td>
<code>gateway</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Gateway defines the lokistack gateway component spec.</p>
</td>
</tr>
<tr>
<td>
<code>indexGateway</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>IndexGateway defines the index gateway component spec.</p>
</td>
</tr>
<tr>
<td>
<code>ruler</code><br/>
<em>
<a href="#loki-grafana-com-v1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ruler defines the ruler component spec.</p>
</td>
</tr>
</tbody>
</table>

## ManagementStateType { #loki-grafana-com-v1-ManagementStateType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>ManagementStateType defines the type for CR management states.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;Managed&#34;</p></td>
<td><p>ManagementStateManaged when the LokiStack custom resource should be
reconciled by the operator.</p>
</td>
</tr><tr><td><p>&#34;Unmanaged&#34;</p></td>
<td><p>ManagementStateUnmanaged when the LokiStack custom resource should not be
reconciled by the operator.</p>
</td>
</tr></tbody>
</table>

## MemberListSpec { #loki-grafana-com-v1-MemberListSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-HashRingSpec">HashRingSpec</a>)
</p>
<div>
<p>MemberListSpec defines the configuration for the memberlist based hash ring.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>instanceAddrType</code><br/>
<em>
<a href="#loki-grafana-com-v1-InstanceAddrType">
InstanceAddrType
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>InstanceAddrType defines the type of address to use to advertise to the ring.
Defaults to the first address from any private network interfaces of the current pod.
Alternatively the public pod IP can be used in case private networks (RFC 1918 and RFC 6598)
are not available.</p>
</td>
</tr>
</tbody>
</table>

## ModeType { #loki-grafana-com-v1-ModeType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-TenantsSpec">TenantsSpec</a>)
</p>
<div>
<p>ModeType is the authentication/authorization mode in which LokiStack Gateway will be configured.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;dynamic&#34;</p></td>
<td><p>Dynamic mode delegates the authorization to a third-party OPA-compatible endpoint.</p>
</td>
</tr><tr><td><p>&#34;openshift-logging&#34;</p></td>
<td><p>OpenshiftLogging mode provides fully automatic OpenShift in-cluster authentication and authorization support for application, infrastructure and audit logs.</p>
</td>
</tr><tr><td><p>&#34;openshift-network&#34;</p></td>
<td><p>OpenshiftNetwork mode provides fully automatic OpenShift in-cluster authentication and authorization support for network logs only.</p>
</td>
</tr><tr><td><p>&#34;static&#34;</p></td>
<td><p>Static mode asserts the Authorization Spec&rsquo;s Roles and RoleBindings
using an in-process OpenPolicyAgent Rego authorizer.</p>
</td>
</tr></tbody>
</table>

## OIDCSpec { #loki-grafana-com-v1-OIDCSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AuthenticationSpec">AuthenticationSpec</a>)
</p>
<div>
<p>OIDCSpec defines the oidc configuration spec for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>secret</code><br/>
<em>
<a href="#loki-grafana-com-v1-TenantSecretSpec">
TenantSecretSpec
</a>
</em>
</td>
<td>
<p>Secret defines the spec for the clientID, clientSecret and issuerCAPath for tenant&rsquo;s authentication.</p>
</td>
</tr>
<tr>
<td>
<code>issuerURL</code><br/>
<em>
string
</em>
</td>
<td>
<p>IssuerURL defines the URL for issuer.</p>
</td>
</tr>
<tr>
<td>
<code>redirectURL</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>RedirectURL defines the URL for redirect.</p>
</td>
</tr>
<tr>
<td>
<code>groupClaim</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Group claim field from ID Token</p>
</td>
</tr>
<tr>
<td>
<code>usernameClaim</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>User claim field from ID Token</p>
</td>
</tr>
</tbody>
</table>

## OPASpec { #loki-grafana-com-v1-OPASpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AuthorizationSpec">AuthorizationSpec</a>)
</p>
<div>
<p>OPASpec defines the opa configuration spec for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>url</code><br/>
<em>
string
</em>
</td>
<td>
<p>URL defines the third-party endpoint for authorization.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageSchema { #loki-grafana-com-v1-ObjectStorageSchema }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackStorageStatus">LokiStackStorageStatus</a>, <a href="#loki-grafana-com-v1-ObjectStorageSpec">ObjectStorageSpec</a>)
</p>
<div>
<p>ObjectStorageSchema defines the requirements needed to configure a new
storage schema.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>version</code><br/>
<em>
<a href="#loki-grafana-com-v1-ObjectStorageSchemaVersion">
ObjectStorageSchemaVersion
</a>
</em>
</td>
<td>
<p>Version for writing and reading logs.</p>
</td>
</tr>
<tr>
<td>
<code>effectiveDate</code><br/>
<em>
<a href="#loki-grafana-com-v1-StorageSchemaEffectiveDate">
StorageSchemaEffectiveDate
</a>
</em>
</td>
<td>
<p>EffectiveDate is the date in UTC that the schema will be applied on.
To ensure readibility of logs, this date should be before the current
date in UTC.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageSchemaVersion { #loki-grafana-com-v1-ObjectStorageSchemaVersion }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-ObjectStorageSchema">ObjectStorageSchema</a>)
</p>
<div>
<p>ObjectStorageSchemaVersion defines the storage schema version which will be
used with the Loki cluster.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;v11&#34;</p></td>
<td><p>ObjectStorageSchemaV11 when using v11 for the storage schema</p>
</td>
</tr><tr><td><p>&#34;v12&#34;</p></td>
<td><p>ObjectStorageSchemaV12 when using v12 for the storage schema</p>
</td>
</tr></tbody>
</table>

## ObjectStorageSecretSpec { #loki-grafana-com-v1-ObjectStorageSecretSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-ObjectStorageSpec">ObjectStorageSpec</a>)
</p>
<div>
<p>ObjectStorageSecretSpec is a secret reference containing name only, no namespace.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>type</code><br/>
<em>
<a href="#loki-grafana-com-v1-ObjectStorageSecretType">
ObjectStorageSecretType
</a>
</em>
</td>
<td>
<p>Type of object storage that should be used</p>
</td>
</tr>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of a secret in the namespace configured for object storage secrets.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageSecretType { #loki-grafana-com-v1-ObjectStorageSecretType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-ObjectStorageSecretSpec">ObjectStorageSecretSpec</a>)
</p>
<div>
<p>ObjectStorageSecretType defines the type of storage which can be used with the Loki cluster.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;alibabacloud&#34;</p></td>
<td><p>ObjectStorageSecretAlibabaCloud when using AlibabaCloud OSS for Loki storage</p>
</td>
</tr><tr><td><p>&#34;azure&#34;</p></td>
<td><p>ObjectStorageSecretAzure when using Azure for Loki storage</p>
</td>
</tr><tr><td><p>&#34;gcs&#34;</p></td>
<td><p>ObjectStorageSecretGCS when using GCS for Loki storage</p>
</td>
</tr><tr><td><p>&#34;s3&#34;</p></td>
<td><p>ObjectStorageSecretS3 when using S3 for Loki storage</p>
</td>
</tr><tr><td><p>&#34;swift&#34;</p></td>
<td><p>ObjectStorageSecretSwift when using Swift for Loki storage</p>
</td>
</tr></tbody>
</table>

## ObjectStorageSpec { #loki-grafana-com-v1-ObjectStorageSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>ObjectStorageSpec defines the requirements to access the object
storage bucket to persist logs by the ingester component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>schemas</code><br/>
<em>
<a href="#loki-grafana-com-v1-ObjectStorageSchema">
[]ObjectStorageSchema
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Schemas for reading and writing logs.</p>
</td>
</tr>
<tr>
<td>
<code>secret</code><br/>
<em>
<a href="#loki-grafana-com-v1-ObjectStorageSecretSpec">
ObjectStorageSecretSpec
</a>
</em>
</td>
<td>
<p>Secret for object storage authentication.
Name of a secret in the same namespace as the LokiStack custom resource.</p>
</td>
</tr>
<tr>
<td>
<code>tls</code><br/>
<em>
<a href="#loki-grafana-com-v1-ObjectStorageTLSSpec">
ObjectStorageTLSSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>TLS configuration for reaching the object storage endpoint.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageTLSSpec { #loki-grafana-com-v1-ObjectStorageTLSSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-ObjectStorageSpec">ObjectStorageSpec</a>)
</p>
<div>
<p>ObjectStorageTLSSpec is the TLS configuration for reaching the object storage endpoint.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>caKey</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Key is the data key of a ConfigMap containing a CA certificate.
It needs to be in the same namespace as the LokiStack custom resource.
If empty, it defaults to &ldquo;service-ca.crt&rdquo;.</p>
</td>
</tr>
<tr>
<td>
<code>caName</code><br/>
<em>
string
</em>
</td>
<td>
<p>CA is the name of a ConfigMap containing a CA certificate.
It needs to be in the same namespace as the LokiStack custom resource.</p>
</td>
</tr>
</tbody>
</table>

## PermissionType { #loki-grafana-com-v1-PermissionType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RoleSpec">RoleSpec</a>)
</p>
<div>
<p>PermissionType is a LokiStack Gateway RBAC permission.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;read&#34;</p></td>
<td><p>Read gives access to read data from a tenant.</p>
</td>
</tr><tr><td><p>&#34;write&#34;</p></td>
<td><p>Write gives access to write data to a tenant.</p>
</td>
</tr></tbody>
</table>

## PodStatusMap { #loki-grafana-com-v1-PodStatusMap }
(<code>map[k8s.io/api/core/v1.PodPhase][]string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackComponentStatus">LokiStackComponentStatus</a>)
</p>
<div>
<p>PodStatusMap defines the type for mapping pod status to pod name.</p>
</div>

## PrometheusDuration { #loki-grafana-com-v1-PrometheusDuration }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerDiscoverySpec">AlertManagerDiscoverySpec</a>, <a href="#loki-grafana-com-v1-AlertManagerNotificationQueueSpec">AlertManagerNotificationQueueSpec</a>, <a href="#loki-grafana-com-v1-AlertingRuleGroup">AlertingRuleGroup</a>, <a href="#loki-grafana-com-v1-AlertingRuleGroupSpec">AlertingRuleGroupSpec</a>, <a href="#loki-grafana-com-v1-RecordingRuleGroup">RecordingRuleGroup</a>, <a href="#loki-grafana-com-v1-RemoteWriteClientQueueSpec">RemoteWriteClientQueueSpec</a>, <a href="#loki-grafana-com-v1-RemoteWriteClientSpec">RemoteWriteClientSpec</a>, <a href="#loki-grafana-com-v1-RemoteWriteSpec">RemoteWriteSpec</a>, <a href="#loki-grafana-com-v1-RulerConfigSpec">RulerConfigSpec</a>)
</p>
<div>
<p>PrometheusDuration defines the type for Prometheus durations.</p>
</div>

## QueryLimitSpec { #loki-grafana-com-v1-QueryLimitSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LimitsTemplateSpec">LimitsTemplateSpec</a>)
</p>
<div>
<p>QueryLimitSpec defines the limits applies at the query path.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>maxEntriesLimitPerQuery</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxEntriesLimitsPerQuery defines the maximum number of log entries
that will be returned for a query.</p>
</td>
</tr>
<tr>
<td>
<code>maxChunksPerQuery</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxChunksPerQuery defines the maximum number of chunks
that can be fetched by a single query.</p>
</td>
</tr>
<tr>
<td>
<code>maxQuerySeries</code><br/>
<em>
int32
</em>
</td>
<td>
<p>MaxQuerySeries defines the maximum of unique series
that is returned by a metric query.</p>
</td>
</tr>
<tr>
<td>
<code>queryTimeout</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Timeout when querying ingesters or storage during the execution of a query request.</p>
</td>
</tr>
</tbody>
</table>

## RecordingRule { #loki-grafana-com-v1-RecordingRule }
<div>
<p>RecordingRule is the Schema for the recordingrules API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1-RecordingRuleSpec">
RecordingRuleSpec
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1-RecordingRuleStatus">
RecordingRuleStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RecordingRuleGroup { #loki-grafana-com-v1-RecordingRuleGroup }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RecordingRuleSpec">RecordingRuleSpec</a>)
</p>
<div>
<p>RecordingRuleGroup defines a group of Loki  recording rules.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of the recording rule group. Must be unique within all recording rules.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval defines the time interval between evaluation of the given
recoding rule.</p>
</td>
</tr>
<tr>
<td>
<code>limit</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Limit defines the number of series a recording rule can produce. 0 is no limit.</p>
</td>
</tr>
<tr>
<td>
<code>rules</code><br/>
<em>
<a href="#loki-grafana-com-v1-RecordingRuleGroupSpec">
[]*RecordingRuleGroupSpec
</a>
</em>
</td>
<td>
<p>Rules defines a list of recording rules</p>
</td>
</tr>
</tbody>
</table>

## RecordingRuleGroupSpec { #loki-grafana-com-v1-RecordingRuleGroupSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RecordingRuleGroup">RecordingRuleGroup</a>)
</p>
<div>
<p>RecordingRuleGroupSpec defines the spec for a Loki recording rule.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>record</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The name of the time series to output to. Must be a valid metric name.</p>
</td>
</tr>
<tr>
<td>
<code>expr</code><br/>
<em>
string
</em>
</td>
<td>
<p>The LogQL expression to evaluate. Every evaluation cycle this is
evaluated at the current time, and all resultant time series become
pending/firing alerts.</p>
</td>
</tr>
</tbody>
</table>

## RecordingRuleSpec { #loki-grafana-com-v1-RecordingRuleSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RecordingRule">RecordingRule</a>)
</p>
<div>
<p>RecordingRuleSpec defines the desired state of RecordingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tenantID</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantID of tenant where the recording rules are evaluated in.</p>
</td>
</tr>
<tr>
<td>
<code>groups</code><br/>
<em>
<a href="#loki-grafana-com-v1-RecordingRuleGroup">
[]*RecordingRuleGroup
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of groups for recording rules.</p>
</td>
</tr>
</tbody>
</table>

## RecordingRuleStatus { #loki-grafana-com-v1-RecordingRuleStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RecordingRule">RecordingRule</a>)
</p>
<div>
<p>RecordingRuleStatus defines the observed state of RecordingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the RecordingRule generation health.</p>
</td>
</tr>
</tbody>
</table>

## RelabelActionType { #loki-grafana-com-v1-RelabelActionType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RelabelConfig">RelabelConfig</a>)
</p>
<div>
<p>RelabelActionType defines the enumeration type for RelabelConfig actions.</p>
</div>

## RelabelConfig { #loki-grafana-com-v1-RelabelConfig }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AlertManagerSpec">AlertManagerSpec</a>, <a href="#loki-grafana-com-v1-RemoteWriteClientSpec">RemoteWriteClientSpec</a>)
</p>
<div>
<p>RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion.
It defines <code>&lt;metric_relabel_configs&gt;</code> and <code>&lt;alert_relabel_configs&gt;</code> sections of Prometheus configuration.
More info: <a href="https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs">https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs</a></p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>sourceLabels</code><br/>
<em>
[]string
</em>
</td>
<td>
<p>The source labels select values from existing labels. Their content is concatenated
using the configured separator and matched against the configured regular expression
for the replace, keep, and drop actions.</p>
</td>
</tr>
<tr>
<td>
<code>separator</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Separator placed between concatenated source label values. default is &lsquo;;&rsquo;.</p>
</td>
</tr>
<tr>
<td>
<code>targetLabel</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Label to which the resulting value is written in a replace action.
It is mandatory for replace actions. Regex capture groups are available.</p>
</td>
</tr>
<tr>
<td>
<code>regex</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Regular expression against which the extracted value is matched. Default is &lsquo;(.*)&rsquo;</p>
</td>
</tr>
<tr>
<td>
<code>modulus</code><br/>
<em>
uint64
</em>
</td>
<td>
<em>(Optional)</em>
<p>Modulus to take of the hash of the source label values.</p>
</td>
</tr>
<tr>
<td>
<code>replacement</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Replacement value against which a regex replace is performed if the
regular expression matches. Regex capture groups are available. Default is &lsquo;$1&rsquo;</p>
</td>
</tr>
<tr>
<td>
<code>action</code><br/>
<em>
<a href="#loki-grafana-com-v1-RelabelActionType">
RelabelActionType
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Action to perform based on regex matching. Default is &lsquo;replace&rsquo;</p>
</td>
</tr>
</tbody>
</table>

## RemoteWriteAuthType { #loki-grafana-com-v1-RemoteWriteAuthType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RemoteWriteClientSpec">RemoteWriteClientSpec</a>)
</p>
<div>
<p>RemoteWriteAuthType defines the type of authorization to use to access the remote write endpoint.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;basic&#34;</p></td>
<td><p>BasicAuthorization defines the remote write client to use HTTP basic authorization.</p>
</td>
</tr><tr><td><p>&#34;bearer&#34;</p></td>
<td><p>BearerAuthorization defines the remote write client to use HTTP bearer authorization.</p>
</td>
</tr></tbody>
</table>

## RemoteWriteClientQueueSpec { #loki-grafana-com-v1-RemoteWriteClientQueueSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RemoteWriteSpec">RemoteWriteSpec</a>)
</p>
<div>
<p>RemoteWriteClientQueueSpec defines the configuration of the remote write client queue.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>capacity</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Number of samples to buffer per shard before we block reading of more</p>
</td>
</tr>
<tr>
<td>
<code>maxShards</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum number of shards, i.e. amount of concurrency.</p>
</td>
</tr>
<tr>
<td>
<code>minShards</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum number of shards, i.e. amount of concurrency.</p>
</td>
</tr>
<tr>
<td>
<code>maxSamplesPerSend</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum number of samples per send.</p>
</td>
</tr>
<tr>
<td>
<code>batchSendDeadline</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum time a sample will wait in buffer.</p>
</td>
</tr>
<tr>
<td>
<code>minBackOffPeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Initial retry delay. Gets doubled for every retry.</p>
</td>
</tr>
<tr>
<td>
<code>maxBackOffPeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum retry delay.</p>
</td>
</tr>
</tbody>
</table>

## RemoteWriteClientSpec { #loki-grafana-com-v1-RemoteWriteClientSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RemoteWriteSpec">RemoteWriteSpec</a>)
</p>
<div>
<p>RemoteWriteClientSpec defines the configuration of the remote write client.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of the remote write config, which if specified must be unique among remote write configs.</p>
</td>
</tr>
<tr>
<td>
<code>url</code><br/>
<em>
string
</em>
</td>
<td>
<p>The URL of the endpoint to send samples to.</p>
</td>
</tr>
<tr>
<td>
<code>timeout</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Timeout for requests to the remote write endpoint.</p>
</td>
</tr>
<tr>
<td>
<code>authorization</code><br/>
<em>
<a href="#loki-grafana-com-v1-RemoteWriteAuthType">
RemoteWriteAuthType
</a>
</em>
</td>
<td>
<p>Type of authorzation to use to access the remote write endpoint</p>
</td>
</tr>
<tr>
<td>
<code>authorizationSecretName</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of a secret in the namespace configured for authorization secrets.</p>
</td>
</tr>
<tr>
<td>
<code>additionalHeaders</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Additional HTTP headers to be sent along with each remote write request.</p>
</td>
</tr>
<tr>
<td>
<code>relabelConfigs</code><br/>
<em>
<a href="#loki-grafana-com-v1-RelabelConfig">
[]RelabelConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of remote write relabel configurations.</p>
</td>
</tr>
<tr>
<td>
<code>proxyUrl</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Optional proxy URL.</p>
</td>
</tr>
<tr>
<td>
<code>followRedirects</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Configure whether HTTP requests follow HTTP 3xx redirects.</p>
</td>
</tr>
</tbody>
</table>

## RemoteWriteSpec { #loki-grafana-com-v1-RemoteWriteSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RulerConfigSpec">RulerConfigSpec</a>)
</p>
<div>
<p>RemoteWriteSpec defines the configuration for ruler&rsquo;s remote_write connectivity.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>enabled</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Enable remote-write functionality.</p>
</td>
</tr>
<tr>
<td>
<code>refreshPeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum period to wait between refreshing remote-write reconfigurations.</p>
</td>
</tr>
<tr>
<td>
<code>client</code><br/>
<em>
<a href="#loki-grafana-com-v1-RemoteWriteClientSpec">
RemoteWriteClientSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for remote write client.</p>
</td>
</tr>
<tr>
<td>
<code>queue</code><br/>
<em>
<a href="#loki-grafana-com-v1-RemoteWriteClientQueueSpec">
RemoteWriteClientQueueSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for remote write client queue.</p>
</td>
</tr>
</tbody>
</table>

## ReplicationSpec { #loki-grafana-com-v1-ReplicationSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>factor</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Factor defines the policy for log stream replication.</p>
</td>
</tr>
<tr>
<td>
<code>zones</code><br/>
<em>
<a href="#loki-grafana-com-v1-ZoneSpec">
[]ZoneSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Zones defines an array of ZoneSpec that the scheduler will try to satisfy.
IMPORTANT: Make sure that the replication factor defined is less than or equal to the number of available zones.</p>
</td>
</tr>
</tbody>
</table>

## RetentionLimitSpec { #loki-grafana-com-v1-RetentionLimitSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LimitsTemplateSpec">LimitsTemplateSpec</a>)
</p>
<div>
<p>RetentionLimitSpec controls how long logs will be kept in storage.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>days</code><br/>
<em>
uint
</em>
</td>
<td>
<p>Days contains the number of days logs are kept.</p>
</td>
</tr>
<tr>
<td>
<code>streams</code><br/>
<em>
<a href="#loki-grafana-com-v1-RetentionStreamSpec">
[]*RetentionStreamSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Stream defines the log stream.</p>
</td>
</tr>
</tbody>
</table>

## RetentionStreamSpec { #loki-grafana-com-v1-RetentionStreamSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RetentionLimitSpec">RetentionLimitSpec</a>)
</p>
<div>
<p>RetentionStreamSpec defines a log stream with separate retention time.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>days</code><br/>
<em>
uint
</em>
</td>
<td>
<p>Days contains the number of days logs are kept.</p>
</td>
</tr>
<tr>
<td>
<code>priority</code><br/>
<em>
uint32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Priority defines the priority of this selector compared to other retention rules.</p>
</td>
</tr>
<tr>
<td>
<code>selector</code><br/>
<em>
string
</em>
</td>
<td>
<p>Selector contains the LogQL query used to define the log stream.</p>
</td>
</tr>
</tbody>
</table>

## RoleBindingsSpec { #loki-grafana-com-v1-RoleBindingsSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AuthorizationSpec">AuthorizationSpec</a>)
</p>
<div>
<p>RoleBindingsSpec binds a set of roles to a set of subjects.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>subjects</code><br/>
<em>
<a href="#loki-grafana-com-v1-Subject">
[]Subject
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>roles</code><br/>
<em>
[]string
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RoleSpec { #loki-grafana-com-v1-RoleSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-AuthorizationSpec">AuthorizationSpec</a>)
</p>
<div>
<p>RoleSpec describes a set of permissions to interact with a tenant.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>resources</code><br/>
<em>
[]string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>tenants</code><br/>
<em>
[]string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>permissions</code><br/>
<em>
<a href="#loki-grafana-com-v1-PermissionType">
[]PermissionType
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RulerConfig { #loki-grafana-com-v1-RulerConfig }
<div>
<p>RulerConfig is the Schema for the rulerconfigs API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1-RulerConfigSpec">
RulerConfigSpec
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1-RulerConfigStatus">
RulerConfigStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RulerConfigSpec { #loki-grafana-com-v1-RulerConfigSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RulerConfig">RulerConfig</a>)
</p>
<div>
<p>RulerConfigSpec defines the desired state of Ruler</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>evaluationInterval</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval on how frequently to evaluate rules.</p>
</td>
</tr>
<tr>
<td>
<code>pollInterval</code><br/>
<em>
<a href="#loki-grafana-com-v1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval on how frequently to poll for new rule definitions.</p>
</td>
</tr>
<tr>
<td>
<code>alertmanager</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerSpec">
AlertManagerSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines alert manager configuration to notify on firing alerts.</p>
</td>
</tr>
<tr>
<td>
<code>remoteWrite</code><br/>
<em>
<a href="#loki-grafana-com-v1-RemoteWriteSpec">
RemoteWriteSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines a remote write endpoint to write recording rule metrics.</p>
</td>
</tr>
<tr>
<td>
<code>overrides</code><br/>
<em>
<a href="#loki-grafana-com-v1-RulerOverrides">
map[string]github.com/grafana/loki/operator/apis/loki/v1.RulerOverrides
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Overrides defines the config overrides to be applied per-tenant.</p>
</td>
</tr>
</tbody>
</table>

## RulerConfigStatus { #loki-grafana-com-v1-RulerConfigStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RulerConfig">RulerConfig</a>)
</p>
<div>
<p>RulerConfigStatus defines the observed state of RulerConfig</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the RulerConfig health.</p>
</td>
</tr>
</tbody>
</table>

## RulerOverrides { #loki-grafana-com-v1-RulerOverrides }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RulerConfigSpec">RulerConfigSpec</a>)
</p>
<div>
<p>RulerOverrides defines the overrides applied per-tenant.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>alertmanager</code><br/>
<em>
<a href="#loki-grafana-com-v1-AlertManagerSpec">
AlertManagerSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>AlertManagerOverrides defines the overrides to apply to the alertmanager config.</p>
</td>
</tr>
</tbody>
</table>

## RulesSpec { #loki-grafana-com-v1-RulesSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>RulesSpec defines the spec for the ruler component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>enabled</code><br/>
<em>
bool
</em>
</td>
<td>
<p>Enabled defines a flag to enable/disable the ruler component</p>
</td>
</tr>
<tr>
<td>
<code>selector</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#labelselector-v1-meta">
Kubernetes meta/v1.LabelSelector
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>A selector to select which LokiRules to mount for loading alerting/recording
rules from.</p>
</td>
</tr>
<tr>
<td>
<code>namespaceSelector</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#labelselector-v1-meta">
Kubernetes meta/v1.LabelSelector
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Namespaces to be selected for PrometheusRules discovery. If unspecified, only
the same namespace as the LokiStack object is in is used.</p>
</td>
</tr>
</tbody>
</table>

## StorageSchemaEffectiveDate { #loki-grafana-com-v1-StorageSchemaEffectiveDate }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-ObjectStorageSchema">ObjectStorageSchema</a>)
</p>
<div>
<p>StorageSchemaEffectiveDate defines the type for the Storage Schema Effect Date</p>
</div>

## Subject { #loki-grafana-com-v1-Subject }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-RoleBindingsSpec">RoleBindingsSpec</a>)
</p>
<div>
<p>Subject represents a subject that has been bound to a role.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>kind</code><br/>
<em>
<a href="#loki-grafana-com-v1-SubjectKind">
SubjectKind
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## SubjectKind { #loki-grafana-com-v1-SubjectKind }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-Subject">Subject</a>)
</p>
<div>
<p>SubjectKind is a kind of LokiStack Gateway RBAC subject.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;group&#34;</p></td>
<td><p>Group represents a subject that is a group.</p>
</td>
</tr><tr><td><p>&#34;user&#34;</p></td>
<td><p>User represents a subject that is a user.</p>
</td>
</tr></tbody>
</table>

## TenantSecretSpec { #loki-grafana-com-v1-TenantSecretSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-OIDCSpec">OIDCSpec</a>)
</p>
<div>
<p>TenantSecretSpec is a secret reference containing name only
for a secret living in the same namespace as the LokiStack custom resource.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of a secret in the namespace configured for tenant secrets.</p>
</td>
</tr>
</tbody>
</table>

## TenantsSpec { #loki-grafana-com-v1-TenantsSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>TenantsSpec defines the mode, authentication and authorization
configuration of the lokiStack gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>mode</code><br/>
<em>
<a href="#loki-grafana-com-v1-ModeType">
ModeType
</a>
</em>
</td>
<td>
<p>Mode defines the mode in which lokistack-gateway component will be configured.</p>
</td>
</tr>
<tr>
<td>
<code>authentication</code><br/>
<em>
<a href="#loki-grafana-com-v1-AuthenticationSpec">
[]AuthenticationSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Authentication defines the lokistack-gateway component authentication configuration spec per tenant.</p>
</td>
</tr>
<tr>
<td>
<code>authorization</code><br/>
<em>
<a href="#loki-grafana-com-v1-AuthorizationSpec">
AuthorizationSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Authorization defines the lokistack-gateway component authorization configuration spec per tenant.</p>
</td>
</tr>
</tbody>
</table>

## ZoneSpec { #loki-grafana-com-v1-ZoneSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1-ReplicationSpec">ReplicationSpec</a>)
</p>
<div>
<p>ZoneSpec defines the spec to support zone-aware component deployments.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>maxSkew</code><br/>
<em>
int
</em>
</td>
<td>
<p>MaxSkew describes the maximum degree to which Pods can be unevenly distributed.</p>
</td>
</tr>
<tr>
<td>
<code>topologyKey</code><br/>
<em>
string
</em>
</td>
<td>
<p>TopologyKey is the key that defines a topology in the Nodes&rsquo; labels.</p>
</td>
</tr>
</tbody>
</table>
<hr/>


# loki.grafana.com/v1beta1 { #loki-grafana-com-v1beta1 }
<div>
<p>Package v1beta1 contains API Schema definitions for the loki v1beta1 API group</p>
</div>
<b>Resource Types:</b>

## AlertManagerClientBasicAuth { #loki-grafana-com-v1beta1-AlertManagerClientBasicAuth }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerClientConfig">AlertManagerClientConfig</a>)
</p>
<div>
<p>AlertManagerClientBasicAuth defines the basic authentication configuration for reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>username</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The subject&rsquo;s username for the basic authentication configuration.</p>
</td>
</tr>
<tr>
<td>
<code>password</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The subject&rsquo;s password for the basic authentication configuration.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerClientConfig { #loki-grafana-com-v1beta1-AlertManagerClientConfig }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerSpec">AlertManagerSpec</a>)
</p>
<div>
<p>AlertManagerClientConfig defines the client configuration for reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tls</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerClientTLSConfig">
AlertManagerClientTLSConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>TLS configuration for reaching the alertmanager endpoints.</p>
</td>
</tr>
<tr>
<td>
<code>headerAuth</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerClientHeaderAuth">
AlertManagerClientHeaderAuth
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Header authentication configuration for reaching the alertmanager endpoints.</p>
</td>
</tr>
<tr>
<td>
<code>basicAuth</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerClientBasicAuth">
AlertManagerClientBasicAuth
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Basic authentication configuration for reaching the alertmanager endpoints.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerClientHeaderAuth { #loki-grafana-com-v1beta1-AlertManagerClientHeaderAuth }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerClientConfig">AlertManagerClientConfig</a>)
</p>
<div>
<p>AlertManagerClientHeaderAuth defines the header configuration reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>type</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The authentication type for the header authentication configuration.</p>
</td>
</tr>
<tr>
<td>
<code>credentials</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The credentials for the header authentication configuration.</p>
</td>
</tr>
<tr>
<td>
<code>credentialsFile</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The credentials file for the Header authentication configuration. It is mutually exclusive with <code>credentials</code>.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerClientTLSConfig { #loki-grafana-com-v1beta1-AlertManagerClientTLSConfig }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerClientConfig">AlertManagerClientConfig</a>)
</p>
<div>
<p>AlertManagerClientTLSConfig defines the TLS configuration for reaching alertmanager endpoints.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>caPath</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The CA certificate file path for the TLS configuration.</p>
</td>
</tr>
<tr>
<td>
<code>serverName</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The server name to validate in the alertmanager server certificates.</p>
</td>
</tr>
<tr>
<td>
<code>certPath</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The client-side certificate file path for the TLS configuration.</p>
</td>
</tr>
<tr>
<td>
<code>keyPath</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The client-side key file path for the TLS configuration.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerDiscoverySpec { #loki-grafana-com-v1beta1-AlertManagerDiscoverySpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerSpec">AlertManagerSpec</a>)
</p>
<div>
<p>AlertManagerDiscoverySpec defines the configuration to use DNS resolution for AlertManager hosts.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>enableSRV</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Use DNS SRV records to discover Alertmanager hosts.</p>
</td>
</tr>
<tr>
<td>
<code>refreshInterval</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>How long to wait between refreshing DNS resolutions of Alertmanager hosts.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerNotificationQueueSpec { #loki-grafana-com-v1beta1-AlertManagerNotificationQueueSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerSpec">AlertManagerSpec</a>)
</p>
<div>
<p>AlertManagerNotificationQueueSpec defines the configuration for AlertManager notification settings.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>capacity</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Capacity of the queue for notifications to be sent to the Alertmanager.</p>
</td>
</tr>
<tr>
<td>
<code>timeout</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>HTTP timeout duration when sending notifications to the Alertmanager.</p>
</td>
</tr>
<tr>
<td>
<code>forOutageTolerance</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Max time to tolerate outage for restoring &ldquo;for&rdquo; state of alert.</p>
</td>
</tr>
<tr>
<td>
<code>forGracePeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum duration between alert and restored &ldquo;for&rdquo; state. This is maintained
only for alerts with configured &ldquo;for&rdquo; time greater than the grace period.</p>
</td>
</tr>
<tr>
<td>
<code>resendDelay</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum amount of time to wait before resending an alert to Alertmanager.</p>
</td>
</tr>
</tbody>
</table>

## AlertManagerSpec { #loki-grafana-com-v1beta1-AlertManagerSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RulerConfigSpec">RulerConfigSpec</a>, <a href="#loki-grafana-com-v1beta1-RulerOverrides">RulerOverrides</a>)
</p>
<div>
<p>AlertManagerSpec defines the configuration for ruler&rsquo;s alertmanager connectivity.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>externalUrl</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>URL for alerts return path.</p>
</td>
</tr>
<tr>
<td>
<code>externalLabels</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Additional labels to add to all alerts.</p>
</td>
</tr>
<tr>
<td>
<code>enableV2</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>If enabled, then requests to Alertmanager use the v2 API.</p>
</td>
</tr>
<tr>
<td>
<code>endpoints</code><br/>
<em>
[]string
</em>
</td>
<td>
<p>List of AlertManager URLs to send notifications to. Each Alertmanager URL is treated as
a separate group in the configuration. Multiple Alertmanagers in HA per group can be
supported by using DNS resolution (See EnableDNSDiscovery).</p>
</td>
</tr>
<tr>
<td>
<code>discovery</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerDiscoverySpec">
AlertManagerDiscoverySpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for DNS-based discovery of AlertManager hosts.</p>
</td>
</tr>
<tr>
<td>
<code>notificationQueue</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerNotificationQueueSpec">
AlertManagerNotificationQueueSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for the notification queue to AlertManager hosts.</p>
</td>
</tr>
<tr>
<td>
<code>relabelConfigs</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RelabelConfig">
[]RelabelConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of alert relabel configurations.</p>
</td>
</tr>
<tr>
<td>
<code>client</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerClientConfig">
AlertManagerClientConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Client configuration for reaching the alertmanager endpoint.</p>
</td>
</tr>
</tbody>
</table>

## AlertingRule { #loki-grafana-com-v1beta1-AlertingRule }
<div>
<p>AlertingRule is the Schema for the alertingrules API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertingRuleSpec">
AlertingRuleSpec
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertingRuleStatus">
AlertingRuleStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## AlertingRuleGroup { #loki-grafana-com-v1beta1-AlertingRuleGroup }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertingRuleSpec">AlertingRuleSpec</a>)
</p>
<div>
<p>AlertingRuleGroup defines a group of Loki alerting rules.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of the alerting rule group. Must be unique within all alerting rules.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval defines the time interval between evaluation of the given
alerting rule.</p>
</td>
</tr>
<tr>
<td>
<code>limit</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Limit defines the number of alerts an alerting rule can produce. 0 is no limit.</p>
</td>
</tr>
<tr>
<td>
<code>rules</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertingRuleGroupSpec">
[]*AlertingRuleGroupSpec
</a>
</em>
</td>
<td>
<p>Rules defines a list of alerting rules</p>
</td>
</tr>
</tbody>
</table>

## AlertingRuleGroupSpec { #loki-grafana-com-v1beta1-AlertingRuleGroupSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertingRuleGroup">AlertingRuleGroup</a>)
</p>
<div>
<p>AlertingRuleGroupSpec defines the spec for a Loki alerting rule.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>alert</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The name of the alert. Must be a valid label value.</p>
</td>
</tr>
<tr>
<td>
<code>expr</code><br/>
<em>
string
</em>
</td>
<td>
<p>The LogQL expression to evaluate. Every evaluation cycle this is
evaluated at the current time, and all resultant time series become
pending/firing alerts.</p>
</td>
</tr>
<tr>
<td>
<code>for</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Alerts are considered firing once they have been returned for this long.
Alerts which have not yet fired for long enough are considered pending.</p>
</td>
</tr>
<tr>
<td>
<code>annotations</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Annotations to add to each alert.</p>
</td>
</tr>
<tr>
<td>
<code>labels</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Labels to add to each alert.</p>
</td>
</tr>
</tbody>
</table>

## AlertingRuleSpec { #loki-grafana-com-v1beta1-AlertingRuleSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertingRule">AlertingRule</a>)
</p>
<div>
<p>AlertingRuleSpec defines the desired state of AlertingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tenantID</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantID of tenant where the alerting rules are evaluated in.</p>
</td>
</tr>
<tr>
<td>
<code>groups</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertingRuleGroup">
[]*AlertingRuleGroup
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of groups for alerting rules.</p>
</td>
</tr>
</tbody>
</table>

## AlertingRuleStatus { #loki-grafana-com-v1beta1-AlertingRuleStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertingRule">AlertingRule</a>)
</p>
<div>
<p>AlertingRuleStatus defines the observed state of AlertingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the AlertingRule generation health.</p>
</td>
</tr>
</tbody>
</table>

## AuthenticationSpec { #loki-grafana-com-v1beta1-AuthenticationSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-TenantsSpec">TenantsSpec</a>)
</p>
<div>
<p>AuthenticationSpec defines the oidc configuration per tenant for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tenantName</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantName defines the name of the tenant.</p>
</td>
</tr>
<tr>
<td>
<code>tenantId</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantID defines the id of the tenant.</p>
</td>
</tr>
<tr>
<td>
<code>oidc</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-OIDCSpec">
OIDCSpec
</a>
</em>
</td>
<td>
<p>OIDC defines the spec for the OIDC tenant&rsquo;s authentication.</p>
</td>
</tr>
</tbody>
</table>

## AuthorizationSpec { #loki-grafana-com-v1beta1-AuthorizationSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-TenantsSpec">TenantsSpec</a>)
</p>
<div>
<p>AuthorizationSpec defines the opa, role bindings and roles
configuration per tenant for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>opa</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-OPASpec">
OPASpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>OPA defines the spec for the third-party endpoint for tenant&rsquo;s authorization.</p>
</td>
</tr>
<tr>
<td>
<code>roles</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RoleSpec">
[]RoleSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Roles defines a set of permissions to interact with a tenant.</p>
</td>
</tr>
<tr>
<td>
<code>roleBindings</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RoleBindingsSpec">
[]RoleBindingsSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>RoleBindings defines configuration to bind a set of roles to a set of subjects.</p>
</td>
</tr>
</tbody>
</table>

## IngestionLimitSpec { #loki-grafana-com-v1beta1-IngestionLimitSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LimitsTemplateSpec">LimitsTemplateSpec</a>)
</p>
<div>
<p>IngestionLimitSpec defines the limits applied at the ingestion path.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>ingestionRate</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>IngestionRate defines the sample size per second. Units MB.</p>
</td>
</tr>
<tr>
<td>
<code>ingestionBurstSize</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>IngestionBurstSize defines the local rate-limited sample size per
distributor replica. It should be set to the set at least to the
maximum logs size expected in a single push request.</p>
</td>
</tr>
<tr>
<td>
<code>maxLabelNameLength</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLabelNameLength defines the maximum number of characters allowed
for label keys in log streams.</p>
</td>
</tr>
<tr>
<td>
<code>maxLabelValueLength</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLabelValueLength defines the maximum number of characters allowed
for label values in log streams.</p>
</td>
</tr>
<tr>
<td>
<code>maxLabelNamesPerSeries</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLabelNamesPerSeries defines the maximum number of label names per series
in each log stream.</p>
</td>
</tr>
<tr>
<td>
<code>maxGlobalStreamsPerTenant</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxGlobalStreamsPerTenant defines the maximum number of active streams
per tenant, across the cluster.</p>
</td>
</tr>
<tr>
<td>
<code>maxLineSize</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxLineSize defines the maximum line size on ingestion path. Units in Bytes.</p>
</td>
</tr>
</tbody>
</table>

## LimitsSpec { #loki-grafana-com-v1beta1-LimitsSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>LimitsSpec defines the spec for limits applied at ingestion or query
path across the cluster or per tenant.
It also defines the per-tenant configuration overrides.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>global</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LimitsTemplateSpec">
LimitsTemplateSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Global defines the limits applied globally across the cluster.</p>
</td>
</tr>
<tr>
<td>
<code>tenants</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LimitsTemplateSpec">
map[string]github.com/grafana/loki/operator/apis/loki/v1beta1.LimitsTemplateSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Tenants defines the limits and overrides applied per tenant.</p>
</td>
</tr>
</tbody>
</table>

## LimitsTemplateSpec { #loki-grafana-com-v1beta1-LimitsTemplateSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LimitsSpec">LimitsSpec</a>)
</p>
<div>
<p>LimitsTemplateSpec defines the limits and overrides applied per-tenant.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>ingestion</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-IngestionLimitSpec">
IngestionLimitSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>IngestionLimits defines the limits applied on ingested log streams.</p>
</td>
</tr>
<tr>
<td>
<code>queries</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-QueryLimitSpec">
QueryLimitSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>QueryLimits defines the limit applied on querying log streams.</p>
</td>
</tr>
</tbody>
</table>

## LokiComponentSpec { #loki-grafana-com-v1beta1-LokiComponentSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiTemplateSpec">LokiTemplateSpec</a>)
</p>
<div>
<p>LokiComponentSpec defines the requirements to configure scheduling
of each loki component individually.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>replicas</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Replicas defines the number of replica pods of the component.</p>
</td>
</tr>
<tr>
<td>
<code>nodeSelector</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>NodeSelector defines the labels required by a node to schedule
the component onto it.</p>
</td>
</tr>
<tr>
<td>
<code>tolerations</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#toleration-v1-core">
[]Kubernetes core/v1.Toleration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Tolerations defines the tolerations required by a node to schedule
the component onto it.</p>
</td>
</tr>
</tbody>
</table>

## LokiStack { #loki-grafana-com-v1beta1-LokiStack }
<div>
<p>LokiStack is the Schema for the lokistacks API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiStackSpec">
LokiStackSpec
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiStackStatus">
LokiStackStatus
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
</tbody>
</table>

## LokiStackComponentStatus { #loki-grafana-com-v1beta1-LokiStackComponentStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackStatus">LokiStackStatus</a>)
</p>
<div>
<p>LokiStackComponentStatus defines the map of per pod status per LokiStack component.
Each component is represented by a separate map of v1.Phase to a list of pods.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>compactor</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Compactor is a map to the pod status of the compactor pod.</p>
</td>
</tr>
<tr>
<td>
<code>distributor</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Distributor is a map to the per pod status of the distributor deployment</p>
</td>
</tr>
<tr>
<td>
<code>indexGateway</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>IndexGateway is a map to the per pod status of the index gateway statefulset</p>
</td>
</tr>
<tr>
<td>
<code>ingester</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ingester is a map to the per pod status of the ingester statefulset</p>
</td>
</tr>
<tr>
<td>
<code>querier</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Querier is a map to the per pod status of the querier deployment</p>
</td>
</tr>
<tr>
<td>
<code>queryFrontend</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>QueryFrontend is a map to the per pod status of the query frontend deployment</p>
</td>
</tr>
<tr>
<td>
<code>gateway</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Gateway is a map to the per pod status of the lokistack gateway deployment.</p>
</td>
</tr>
<tr>
<td>
<code>ruler</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PodStatusMap">
PodStatusMap
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ruler is a map to the per pod status of the lokistack ruler statefulset.</p>
</td>
</tr>
</tbody>
</table>

## LokiStackConditionReason { #loki-grafana-com-v1beta1-LokiStackConditionReason }
(<code>string</code> alias)
<div>
<p>LokiStackConditionReason defines the type for valid reasons of a Loki deployment conditions.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;FailedComponents&#34;</p></td>
<td><p>ReasonFailedComponents when all/some LokiStack components fail to roll out.</p>
</td>
</tr><tr><td><p>&#34;InvalidGatewayTenantSecret&#34;</p></td>
<td><p>ReasonInvalidGatewayTenantSecret when the format of the secret is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidObjectStorageCAConfigMap&#34;</p></td>
<td><p>ReasonInvalidObjectStorageCAConfigMap when the format of the CA configmap is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidObjectStorageSchema&#34;</p></td>
<td><p>ReasonInvalidObjectStorageSchema when the spec contains an invalid schema(s).</p>
</td>
</tr><tr><td><p>&#34;InvalidObjectStorageSecret&#34;</p></td>
<td><p>ReasonInvalidObjectStorageSecret when the format of the secret is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidReplicationConfiguration&#34;</p></td>
<td><p>ReasonInvalidReplicationConfiguration when the configurated replication factor is not valid
with the select cluster size.</p>
</td>
</tr><tr><td><p>&#34;InvalidRulerSecret&#34;</p></td>
<td><p>ReasonInvalidRulerSecret when the format of the ruler remote write authorization secret is invalid.</p>
</td>
</tr><tr><td><p>&#34;InvalidTenantsConfiguration&#34;</p></td>
<td><p>ReasonInvalidTenantsConfiguration when the tenant configuration provided is invalid.</p>
</td>
</tr><tr><td><p>&#34;MissingGatewayOpenShiftBaseDomain&#34;</p></td>
<td><p>ReasonMissingGatewayOpenShiftBaseDomain when the reconciler cannot lookup the OpenShift DNS base domain.</p>
</td>
</tr><tr><td><p>&#34;MissingGatewayTenantSecret&#34;</p></td>
<td><p>ReasonMissingGatewayTenantSecret when the required tenant secret
for authentication is missing.</p>
</td>
</tr><tr><td><p>&#34;MissingObjectStorageCAConfigMap&#34;</p></td>
<td><p>ReasonMissingObjectStorageCAConfigMap when the required configmap to verify object storage
certificates is missing.</p>
</td>
</tr><tr><td><p>&#34;MissingObjectStorageSecret&#34;</p></td>
<td><p>ReasonMissingObjectStorageSecret when the required secret to store logs to object
storage is missing.</p>
</td>
</tr><tr><td><p>&#34;MissingRulerSecret&#34;</p></td>
<td><p>ReasonMissingRulerSecret when the required secret to authorization remote write connections
for the ruler is missing.</p>
</td>
</tr><tr><td><p>&#34;PendingComponents&#34;</p></td>
<td><p>ReasonPendingComponents when all/some LokiStack components pending dependencies</p>
</td>
</tr><tr><td><p>&#34;ReadyComponents&#34;</p></td>
<td><p>ReasonReadyComponents when all LokiStack components are ready to serve traffic.</p>
</td>
</tr></tbody>
</table>

## LokiStackConditionType { #loki-grafana-com-v1beta1-LokiStackConditionType }
(<code>string</code> alias)
<div>
<p>LokiStackConditionType deifnes the type of condition types of a Loki deployment.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;Degraded&#34;</p></td>
<td><p>ConditionDegraded defines the condition that some or all components in the Loki deployment
are degraded or the cluster cannot connect to object storage.</p>
</td>
</tr><tr><td><p>&#34;Failed&#34;</p></td>
<td><p>ConditionFailed defines the condition that components in the Loki deployment failed to roll out.</p>
</td>
</tr><tr><td><p>&#34;Pending&#34;</p></td>
<td><p>ConditionPending defines the conditioin that some or all components are in pending state.</p>
</td>
</tr><tr><td><p>&#34;Ready&#34;</p></td>
<td><p>ConditionReady defines the condition that all components in the Loki deployment are ready.</p>
</td>
</tr></tbody>
</table>

## LokiStackSizeType { #loki-grafana-com-v1beta1-LokiStackSizeType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>LokiStackSizeType declares the type for loki cluster scale outs.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;1x.extra-small&#34;</p></td>
<td><p>SizeOneXExtraSmall defines the size of a single Loki deployment
with extra small resources/limits requirements and without HA support.
This size is ultimately dedicated for development and demo purposes.
DO NOT USE THIS IN PRODUCTION!</p>
<p>FIXME: Add clear description of ingestion/query performance expectations.</p>
</td>
</tr><tr><td><p>&#34;1x.medium&#34;</p></td>
<td><p>SizeOneXMedium defines the size of a single Loki deployment
with small resources/limits requirements and HA support for all
Loki components. This size is dedicated for setup <strong>with</strong> the
requirement for single replication factor and auto-compaction.</p>
<p>FIXME: Add clear description of ingestion/query performance expectations.</p>
</td>
</tr><tr><td><p>&#34;1x.small&#34;</p></td>
<td><p>SizeOneXSmall defines the size of a single Loki deployment
with small resources/limits requirements and HA support for all
Loki components. This size is dedicated for setup <strong>without</strong> the
requirement for single replication factor and auto-compaction.</p>
<p>FIXME: Add clear description of ingestion/query performance expectations.</p>
</td>
</tr></tbody>
</table>

## LokiStackSpec { #loki-grafana-com-v1beta1-LokiStackSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStack">LokiStack</a>)
</p>
<div>
<p>LokiStackSpec defines the desired state of LokiStack</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>managementState</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ManagementStateType">
ManagementStateType
</a>
</em>
</td>
<td>
<p>ManagementState defines if the CR should be managed by the operator or not.
Default is managed.</p>
</td>
</tr>
<tr>
<td>
<code>size</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiStackSizeType">
LokiStackSizeType
</a>
</em>
</td>
<td>
<p>Size defines one of the support Loki deployment scale out sizes.</p>
</td>
</tr>
<tr>
<td>
<code>storage</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ObjectStorageSpec">
ObjectStorageSpec
</a>
</em>
</td>
<td>
<p>Storage defines the spec for the object storage endpoint to store logs.</p>
</td>
</tr>
<tr>
<td>
<code>storageClassName</code><br/>
<em>
string
</em>
</td>
<td>
<p>Storage class name defines the storage class for ingester/querier PVCs.</p>
</td>
</tr>
<tr>
<td>
<code>replicationFactor</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>ReplicationFactor defines the policy for log stream replication.</p>
</td>
</tr>
<tr>
<td>
<code>rules</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RulesSpec">
RulesSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Rules defines the spec for the ruler component</p>
</td>
</tr>
<tr>
<td>
<code>limits</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LimitsSpec">
LimitsSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Limits defines the per-tenant limits to be applied to log stream processing and the per-tenant the config overrides.</p>
</td>
</tr>
<tr>
<td>
<code>template</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiTemplateSpec">
LokiTemplateSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Template defines the resource/limits/tolerations/nodeselectors per component</p>
</td>
</tr>
<tr>
<td>
<code>tenants</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-TenantsSpec">
TenantsSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Tenants defines the per-tenant authentication and authorization spec for the lokistack-gateway component.</p>
</td>
</tr>
</tbody>
</table>

## LokiStackStatus { #loki-grafana-com-v1beta1-LokiStackStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStack">LokiStack</a>)
</p>
<div>
<p>LokiStackStatus defines the observed state of LokiStack</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>components</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiStackComponentStatus">
LokiStackComponentStatus
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Components provides summary of all Loki pod status grouped
per component.</p>
</td>
</tr>
<tr>
<td>
<code>storage</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiStackStorageStatus">
LokiStackStorageStatus
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Storage provides summary of all changes that have occurred
to the storage configuration.</p>
</td>
</tr>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the Loki deployment health.</p>
</td>
</tr>
</tbody>
</table>

## LokiStackStorageStatus { #loki-grafana-com-v1beta1-LokiStackStorageStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackStatus">LokiStackStatus</a>)
</p>
<div>
<p>LokiStackStorageStatus defines the observed state of
the Loki storage configuration.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>schemas</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ObjectStorageSchema">
[]ObjectStorageSchema
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Schemas is a list of schemas which have been applied
to the LokiStack.</p>
</td>
</tr>
</tbody>
</table>

## LokiTemplateSpec { #loki-grafana-com-v1beta1-LokiTemplateSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>LokiTemplateSpec defines the template of all requirements to configure
scheduling of all Loki components to be deployed.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>compactor</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Compactor defines the compaction component spec.</p>
</td>
</tr>
<tr>
<td>
<code>distributor</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Distributor defines the distributor component spec.</p>
</td>
</tr>
<tr>
<td>
<code>ingester</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ingester defines the ingester component spec.</p>
</td>
</tr>
<tr>
<td>
<code>querier</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Querier defines the querier component spec.</p>
</td>
</tr>
<tr>
<td>
<code>queryFrontend</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>QueryFrontend defines the query frontend component spec.</p>
</td>
</tr>
<tr>
<td>
<code>gateway</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Gateway defines the lokistack gateway component spec.</p>
</td>
</tr>
<tr>
<td>
<code>indexGateway</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>IndexGateway defines the index gateway component spec.</p>
</td>
</tr>
<tr>
<td>
<code>ruler</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-LokiComponentSpec">
LokiComponentSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Ruler defines the ruler component spec.</p>
</td>
</tr>
</tbody>
</table>

## ManagementStateType { #loki-grafana-com-v1beta1-ManagementStateType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>ManagementStateType defines the type for CR management states.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;Managed&#34;</p></td>
<td><p>ManagementStateManaged when the LokiStack custom resource should be
reconciled by the operator.</p>
</td>
</tr><tr><td><p>&#34;Unmanaged&#34;</p></td>
<td><p>ManagementStateUnmanaged when the LokiStack custom resource should not be
reconciled by the operator.</p>
</td>
</tr></tbody>
</table>

## ModeType { #loki-grafana-com-v1beta1-ModeType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-TenantsSpec">TenantsSpec</a>)
</p>
<div>
<p>ModeType is the authentication/authorization mode in which LokiStack Gateway will be configured.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;dynamic&#34;</p></td>
<td><p>Dynamic mode delegates the authorization to a third-party OPA-compatible endpoint.</p>
</td>
</tr><tr><td><p>&#34;openshift-logging&#34;</p></td>
<td><p>OpenshiftLogging mode provides fully automatic OpenShift in-cluster authentication and authorization support.</p>
</td>
</tr><tr><td><p>&#34;static&#34;</p></td>
<td><p>Static mode asserts the Authorization Spec&rsquo;s Roles and RoleBindings
using an in-process OpenPolicyAgent Rego authorizer.</p>
</td>
</tr></tbody>
</table>

## OIDCSpec { #loki-grafana-com-v1beta1-OIDCSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AuthenticationSpec">AuthenticationSpec</a>)
</p>
<div>
<p>OIDCSpec defines the oidc configuration spec for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>secret</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-TenantSecretSpec">
TenantSecretSpec
</a>
</em>
</td>
<td>
<p>Secret defines the spec for the clientID, clientSecret and issuerCAPath for tenant&rsquo;s authentication.</p>
</td>
</tr>
<tr>
<td>
<code>issuerURL</code><br/>
<em>
string
</em>
</td>
<td>
<p>IssuerURL defines the URL for issuer.</p>
</td>
</tr>
<tr>
<td>
<code>redirectURL</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>RedirectURL defines the URL for redirect.</p>
</td>
</tr>
<tr>
<td>
<code>groupClaim</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Group claim field from ID Token</p>
</td>
</tr>
<tr>
<td>
<code>usernameClaim</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>User claim field from ID Token</p>
</td>
</tr>
</tbody>
</table>

## OPASpec { #loki-grafana-com-v1beta1-OPASpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AuthorizationSpec">AuthorizationSpec</a>)
</p>
<div>
<p>OPASpec defines the opa configuration spec for lokiStack Gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>url</code><br/>
<em>
string
</em>
</td>
<td>
<p>URL defines the third-party endpoint for authorization.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageSchema { #loki-grafana-com-v1beta1-ObjectStorageSchema }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackStorageStatus">LokiStackStorageStatus</a>, <a href="#loki-grafana-com-v1beta1-ObjectStorageSpec">ObjectStorageSpec</a>)
</p>
<div>
<p>ObjectStorageSchema defines the requirements needed to configure a new
storage schema.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>version</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ObjectStorageSchemaVersion">
ObjectStorageSchemaVersion
</a>
</em>
</td>
<td>
<p>Version for writing and reading logs.</p>
</td>
</tr>
<tr>
<td>
<code>effectiveDate</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-StorageSchemaEffectiveDate">
StorageSchemaEffectiveDate
</a>
</em>
</td>
<td>
<p>EffectiveDate is the date in UTC that the schema will be applied on.
To ensure readibility of logs, this date should be before the current
date in UTC.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageSchemaVersion { #loki-grafana-com-v1beta1-ObjectStorageSchemaVersion }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-ObjectStorageSchema">ObjectStorageSchema</a>)
</p>
<div>
<p>ObjectStorageSchemaVersion defines the storage schema version which will be
used with the Loki cluster.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;v11&#34;</p></td>
<td><p>ObjectStorageSchemaV11 when using v11 for the storage schema</p>
</td>
</tr><tr><td><p>&#34;v12&#34;</p></td>
<td><p>ObjectStorageSchemaV12 when using v12 for the storage schema</p>
</td>
</tr></tbody>
</table>

## ObjectStorageSecretSpec { #loki-grafana-com-v1beta1-ObjectStorageSecretSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-ObjectStorageSpec">ObjectStorageSpec</a>)
</p>
<div>
<p>ObjectStorageSecretSpec is a secret reference containing name only, no namespace.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>type</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ObjectStorageSecretType">
ObjectStorageSecretType
</a>
</em>
</td>
<td>
<p>Type of object storage that should be used</p>
</td>
</tr>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of a secret in the namespace configured for object storage secrets.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageSecretType { #loki-grafana-com-v1beta1-ObjectStorageSecretType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-ObjectStorageSecretSpec">ObjectStorageSecretSpec</a>)
</p>
<div>
<p>ObjectStorageSecretType defines the type of storage which can be used with the Loki cluster.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;azure&#34;</p></td>
<td><p>ObjectStorageSecretAzure when using Azure for Loki storage</p>
</td>
</tr><tr><td><p>&#34;gcs&#34;</p></td>
<td><p>ObjectStorageSecretGCS when using GCS for Loki storage</p>
</td>
</tr><tr><td><p>&#34;s3&#34;</p></td>
<td><p>ObjectStorageSecretS3 when using S3 for Loki storage</p>
</td>
</tr><tr><td><p>&#34;swift&#34;</p></td>
<td><p>ObjectStorageSecretSwift when using Swift for Loki storage</p>
</td>
</tr></tbody>
</table>

## ObjectStorageSpec { #loki-grafana-com-v1beta1-ObjectStorageSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>ObjectStorageSpec defines the requirements to access the object
storage bucket to persist logs by the ingester component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>schemas</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ObjectStorageSchema">
[]ObjectStorageSchema
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Schemas for reading and writing logs.</p>
</td>
</tr>
<tr>
<td>
<code>secret</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ObjectStorageSecretSpec">
ObjectStorageSecretSpec
</a>
</em>
</td>
<td>
<p>Secret for object storage authentication.
Name of a secret in the same namespace as the LokiStack custom resource.</p>
</td>
</tr>
<tr>
<td>
<code>tls</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ObjectStorageTLSSpec">
ObjectStorageTLSSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>TLS configuration for reaching the object storage endpoint.</p>
</td>
</tr>
</tbody>
</table>

## ObjectStorageTLSSpec { #loki-grafana-com-v1beta1-ObjectStorageTLSSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-ObjectStorageSpec">ObjectStorageSpec</a>)
</p>
<div>
<p>ObjectStorageTLSSpec is the TLS configuration for reaching the object storage endpoint.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>caName</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>CA is the name of a ConfigMap containing a CA certificate.
It needs to be in the same namespace as the LokiStack custom resource.</p>
</td>
</tr>
</tbody>
</table>

## PermissionType { #loki-grafana-com-v1beta1-PermissionType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RoleSpec">RoleSpec</a>)
</p>
<div>
<p>PermissionType is a LokiStack Gateway RBAC permission.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;read&#34;</p></td>
<td><p>Read gives access to read data from a tenant.</p>
</td>
</tr><tr><td><p>&#34;write&#34;</p></td>
<td><p>Write gives access to write data to a tenant.</p>
</td>
</tr></tbody>
</table>

## PodStatusMap { #loki-grafana-com-v1beta1-PodStatusMap }
(<code>map[k8s.io/api/core/v1.PodPhase][]string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackComponentStatus">LokiStackComponentStatus</a>)
</p>
<div>
<p>PodStatusMap defines the type for mapping pod status to pod name.</p>
</div>

## PrometheusDuration { #loki-grafana-com-v1beta1-PrometheusDuration }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerDiscoverySpec">AlertManagerDiscoverySpec</a>, <a href="#loki-grafana-com-v1beta1-AlertManagerNotificationQueueSpec">AlertManagerNotificationQueueSpec</a>, <a href="#loki-grafana-com-v1beta1-AlertingRuleGroup">AlertingRuleGroup</a>, <a href="#loki-grafana-com-v1beta1-AlertingRuleGroupSpec">AlertingRuleGroupSpec</a>, <a href="#loki-grafana-com-v1beta1-RecordingRuleGroup">RecordingRuleGroup</a>, <a href="#loki-grafana-com-v1beta1-RemoteWriteClientQueueSpec">RemoteWriteClientQueueSpec</a>, <a href="#loki-grafana-com-v1beta1-RemoteWriteClientSpec">RemoteWriteClientSpec</a>, <a href="#loki-grafana-com-v1beta1-RemoteWriteSpec">RemoteWriteSpec</a>, <a href="#loki-grafana-com-v1beta1-RulerConfigSpec">RulerConfigSpec</a>)
</p>
<div>
<p>PrometheusDuration defines the type for Prometheus durations.</p>
</div>

## QueryLimitSpec { #loki-grafana-com-v1beta1-QueryLimitSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LimitsTemplateSpec">LimitsTemplateSpec</a>)
</p>
<div>
<p>QueryLimitSpec defines the limits applies at the query path.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>maxEntriesLimitPerQuery</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxEntriesLimitsPerQuery defines the maximum number of log entries
that will be returned for a query.</p>
</td>
</tr>
<tr>
<td>
<code>maxChunksPerQuery</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>MaxChunksPerQuery defines the maximum number of chunks
that can be fetched by a single query.</p>
</td>
</tr>
<tr>
<td>
<code>maxQuerySeries</code><br/>
<em>
int32
</em>
</td>
<td>
<p>MaxQuerySeries defines the maximum of unique series
that is returned by a metric query.</p>
</td>
</tr>
</tbody>
</table>

## RecordingRule { #loki-grafana-com-v1beta1-RecordingRule }
<div>
<p>RecordingRule is the Schema for the recordingrules API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RecordingRuleSpec">
RecordingRuleSpec
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RecordingRuleStatus">
RecordingRuleStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RecordingRuleGroup { #loki-grafana-com-v1beta1-RecordingRuleGroup }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RecordingRuleSpec">RecordingRuleSpec</a>)
</p>
<div>
<p>RecordingRuleGroup defines a group of Loki  recording rules.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of the recording rule group. Must be unique within all recording rules.</p>
</td>
</tr>
<tr>
<td>
<code>interval</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval defines the time interval between evaluation of the given
recoding rule.</p>
</td>
</tr>
<tr>
<td>
<code>limit</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Limit defines the number of series a recording rule can produce. 0 is no limit.</p>
</td>
</tr>
<tr>
<td>
<code>rules</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RecordingRuleGroupSpec">
[]*RecordingRuleGroupSpec
</a>
</em>
</td>
<td>
<p>Rules defines a list of recording rules</p>
</td>
</tr>
</tbody>
</table>

## RecordingRuleGroupSpec { #loki-grafana-com-v1beta1-RecordingRuleGroupSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RecordingRuleGroup">RecordingRuleGroup</a>)
</p>
<div>
<p>RecordingRuleGroupSpec defines the spec for a Loki recording rule.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>record</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>The name of the time series to output to. Must be a valid metric name.</p>
</td>
</tr>
<tr>
<td>
<code>expr</code><br/>
<em>
string
</em>
</td>
<td>
<p>The LogQL expression to evaluate. Every evaluation cycle this is
evaluated at the current time, and all resultant time series become
pending/firing alerts.</p>
</td>
</tr>
</tbody>
</table>

## RecordingRuleSpec { #loki-grafana-com-v1beta1-RecordingRuleSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RecordingRule">RecordingRule</a>)
</p>
<div>
<p>RecordingRuleSpec defines the desired state of RecordingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>tenantID</code><br/>
<em>
string
</em>
</td>
<td>
<p>TenantID of tenant where the recording rules are evaluated in.</p>
</td>
</tr>
<tr>
<td>
<code>groups</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RecordingRuleGroup">
[]*RecordingRuleGroup
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of groups for recording rules.</p>
</td>
</tr>
</tbody>
</table>

## RecordingRuleStatus { #loki-grafana-com-v1beta1-RecordingRuleStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RecordingRule">RecordingRule</a>)
</p>
<div>
<p>RecordingRuleStatus defines the observed state of RecordingRule</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the RecordingRule generation health.</p>
</td>
</tr>
</tbody>
</table>

## RelabelActionType { #loki-grafana-com-v1beta1-RelabelActionType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RelabelConfig">RelabelConfig</a>)
</p>
<div>
<p>RelabelActionType defines the enumeration type for RelabelConfig actions.</p>
</div>

## RelabelConfig { #loki-grafana-com-v1beta1-RelabelConfig }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AlertManagerSpec">AlertManagerSpec</a>, <a href="#loki-grafana-com-v1beta1-RemoteWriteClientSpec">RemoteWriteClientSpec</a>)
</p>
<div>
<p>RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion.
It defines <code>&lt;metric_relabel_configs&gt;</code> and <code>&lt;alert_relabel_configs&gt;</code> sections of Prometheus configuration.
More info: <a href="https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs">https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs</a></p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>sourceLabels</code><br/>
<em>
[]string
</em>
</td>
<td>
<p>The source labels select values from existing labels. Their content is concatenated
using the configured separator and matched against the configured regular expression
for the replace, keep, and drop actions.</p>
</td>
</tr>
<tr>
<td>
<code>separator</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Separator placed between concatenated source label values. default is &lsquo;;&rsquo;.</p>
</td>
</tr>
<tr>
<td>
<code>targetLabel</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Label to which the resulting value is written in a replace action.
It is mandatory for replace actions. Regex capture groups are available.</p>
</td>
</tr>
<tr>
<td>
<code>regex</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Regular expression against which the extracted value is matched. Default is &lsquo;(.*)&rsquo;</p>
</td>
</tr>
<tr>
<td>
<code>modulus</code><br/>
<em>
uint64
</em>
</td>
<td>
<em>(Optional)</em>
<p>Modulus to take of the hash of the source label values.</p>
</td>
</tr>
<tr>
<td>
<code>replacement</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Replacement value against which a regex replace is performed if the
regular expression matches. Regex capture groups are available. Default is &lsquo;$1&rsquo;</p>
</td>
</tr>
<tr>
<td>
<code>action</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RelabelActionType">
RelabelActionType
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Action to perform based on regex matching. Default is &lsquo;replace&rsquo;</p>
</td>
</tr>
</tbody>
</table>

## RemoteWriteAuthType { #loki-grafana-com-v1beta1-RemoteWriteAuthType }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RemoteWriteClientSpec">RemoteWriteClientSpec</a>)
</p>
<div>
<p>RemoteWriteAuthType defines the type of authorization to use to access the remote write endpoint.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;basic&#34;</p></td>
<td><p>BasicAuthorization defines the remote write client to use HTTP basic authorization.</p>
</td>
</tr><tr><td><p>&#34;bearer&#34;</p></td>
<td><p>BearerAuthorization defines the remote write client to use HTTP bearer authorization.</p>
</td>
</tr></tbody>
</table>

## RemoteWriteClientQueueSpec { #loki-grafana-com-v1beta1-RemoteWriteClientQueueSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RemoteWriteSpec">RemoteWriteSpec</a>)
</p>
<div>
<p>RemoteWriteClientQueueSpec defines the configuration of the remote write client queue.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>capacity</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Number of samples to buffer per shard before we block reading of more</p>
</td>
</tr>
<tr>
<td>
<code>maxShards</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum number of shards, i.e. amount of concurrency.</p>
</td>
</tr>
<tr>
<td>
<code>minShards</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum number of shards, i.e. amount of concurrency.</p>
</td>
</tr>
<tr>
<td>
<code>maxSamplesPerSend</code><br/>
<em>
int32
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum number of samples per send.</p>
</td>
</tr>
<tr>
<td>
<code>batchSendDeadline</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum time a sample will wait in buffer.</p>
</td>
</tr>
<tr>
<td>
<code>minBackOffPeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Initial retry delay. Gets doubled for every retry.</p>
</td>
</tr>
<tr>
<td>
<code>maxBackOffPeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Maximum retry delay.</p>
</td>
</tr>
</tbody>
</table>

## RemoteWriteClientSpec { #loki-grafana-com-v1beta1-RemoteWriteClientSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RemoteWriteSpec">RemoteWriteSpec</a>)
</p>
<div>
<p>RemoteWriteClientSpec defines the configuration of the remote write client.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of the remote write config, which if specified must be unique among remote write configs.</p>
</td>
</tr>
<tr>
<td>
<code>url</code><br/>
<em>
string
</em>
</td>
<td>
<p>The URL of the endpoint to send samples to.</p>
</td>
</tr>
<tr>
<td>
<code>timeout</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Timeout for requests to the remote write endpoint.</p>
</td>
</tr>
<tr>
<td>
<code>authorization</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RemoteWriteAuthType">
RemoteWriteAuthType
</a>
</em>
</td>
<td>
<p>Type of authorzation to use to access the remote write endpoint</p>
</td>
</tr>
<tr>
<td>
<code>authorizationSecretName</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of a secret in the namespace configured for authorization secrets.</p>
</td>
</tr>
<tr>
<td>
<code>additionalHeaders</code><br/>
<em>
map[string]string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Additional HTTP headers to be sent along with each remote write request.</p>
</td>
</tr>
<tr>
<td>
<code>relabelConfigs</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RelabelConfig">
[]RelabelConfig
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>List of remote write relabel configurations.</p>
</td>
</tr>
<tr>
<td>
<code>proxyUrl</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Optional proxy URL.</p>
</td>
</tr>
<tr>
<td>
<code>followRedirects</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Configure whether HTTP requests follow HTTP 3xx redirects.</p>
</td>
</tr>
</tbody>
</table>

## RemoteWriteSpec { #loki-grafana-com-v1beta1-RemoteWriteSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RulerConfigSpec">RulerConfigSpec</a>)
</p>
<div>
<p>RemoteWriteSpec defines the configuration for ruler&rsquo;s remote_write connectivity.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>enabled</code><br/>
<em>
bool
</em>
</td>
<td>
<em>(Optional)</em>
<p>Enable remote-write functionality.</p>
</td>
</tr>
<tr>
<td>
<code>refreshPeriod</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Minimum period to wait between refreshing remote-write reconfigurations.</p>
</td>
</tr>
<tr>
<td>
<code>client</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RemoteWriteClientSpec">
RemoteWriteClientSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for remote write client.</p>
</td>
</tr>
<tr>
<td>
<code>queue</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RemoteWriteClientQueueSpec">
RemoteWriteClientQueueSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines the configuration for remote write client queue.</p>
</td>
</tr>
</tbody>
</table>

## RoleBindingsSpec { #loki-grafana-com-v1beta1-RoleBindingsSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AuthorizationSpec">AuthorizationSpec</a>)
</p>
<div>
<p>RoleBindingsSpec binds a set of roles to a set of subjects.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>subjects</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-Subject">
[]Subject
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>roles</code><br/>
<em>
[]string
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RoleSpec { #loki-grafana-com-v1beta1-RoleSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-AuthorizationSpec">AuthorizationSpec</a>)
</p>
<div>
<p>RoleSpec describes a set of permissions to interact with a tenant.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>resources</code><br/>
<em>
[]string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>tenants</code><br/>
<em>
[]string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>permissions</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PermissionType">
[]PermissionType
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RulerConfig { #loki-grafana-com-v1beta1-RulerConfig }
<div>
<p>RulerConfig is the Schema for the rulerconfigs API</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>metadata</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#objectmeta-v1-meta">
Kubernetes meta/v1.ObjectMeta
</a>
</em>
</td>
<td>
Refer to the Kubernetes API documentation for the fields of the
<code>metadata</code> field.
</td>
</tr>
<tr>
<td>
<code>spec</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RulerConfigSpec">
RulerConfigSpec
</a>
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>status</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RulerConfigStatus">
RulerConfigStatus
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## RulerConfigSpec { #loki-grafana-com-v1beta1-RulerConfigSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RulerConfig">RulerConfig</a>)
</p>
<div>
<p>RulerConfigSpec defines the desired state of Ruler</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>evaluationInterval</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval on how frequently to evaluate rules.</p>
</td>
</tr>
<tr>
<td>
<code>pollInterval</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-PrometheusDuration">
PrometheusDuration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Interval on how frequently to poll for new rule definitions.</p>
</td>
</tr>
<tr>
<td>
<code>alertmanager</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerSpec">
AlertManagerSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines alert manager configuration to notify on firing alerts.</p>
</td>
</tr>
<tr>
<td>
<code>remoteWrite</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RemoteWriteSpec">
RemoteWriteSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Defines a remote write endpoint to write recording rule metrics.</p>
</td>
</tr>
<tr>
<td>
<code>overrides</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-RulerOverrides">
map[string]github.com/grafana/loki/operator/apis/loki/v1beta1.RulerOverrides
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Overrides defines the config overrides to be applied per-tenant.</p>
</td>
</tr>
</tbody>
</table>

## RulerConfigStatus { #loki-grafana-com-v1beta1-RulerConfigStatus }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RulerConfig">RulerConfig</a>)
</p>
<div>
<p>RulerConfigStatus defines the observed state of RulerConfig</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>conditions</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#condition-v1-meta">
[]Kubernetes meta/v1.Condition
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Conditions of the RulerConfig health.</p>
</td>
</tr>
</tbody>
</table>

## RulerOverrides { #loki-grafana-com-v1beta1-RulerOverrides }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RulerConfigSpec">RulerConfigSpec</a>)
</p>
<div>
<p>RulerOverrides defines the overrides applied per-tenant.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>alertmanager</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AlertManagerSpec">
AlertManagerSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>AlertManagerOverrides defines the overrides to apply to the alertmanager config.</p>
</td>
</tr>
</tbody>
</table>

## RulesSpec { #loki-grafana-com-v1beta1-RulesSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>RulesSpec deifnes the spec for the ruler component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>enabled</code><br/>
<em>
bool
</em>
</td>
<td>
<p>Enabled defines a flag to enable/disable the ruler component</p>
</td>
</tr>
<tr>
<td>
<code>selector</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#labelselector-v1-meta">
Kubernetes meta/v1.LabelSelector
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>A selector to select which LokiRules to mount for loading alerting/recording
rules from.</p>
</td>
</tr>
<tr>
<td>
<code>namespaceSelector</code><br/>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.24/#labelselector-v1-meta">
Kubernetes meta/v1.LabelSelector
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Namespaces to be selected for PrometheusRules discovery. If unspecified, only
the same namespace as the LokiStack object is in is used.</p>
</td>
</tr>
</tbody>
</table>

## StorageSchemaEffectiveDate { #loki-grafana-com-v1beta1-StorageSchemaEffectiveDate }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-ObjectStorageSchema">ObjectStorageSchema</a>)
</p>
<div>
<p>StorageSchemaEffectiveDate defines the type for the Storage Schema Effect Date</p>
</div>

## Subject { #loki-grafana-com-v1beta1-Subject }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-RoleBindingsSpec">RoleBindingsSpec</a>)
</p>
<div>
<p>Subject represents a subject that has been bound to a role.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
</td>
</tr>
<tr>
<td>
<code>kind</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-SubjectKind">
SubjectKind
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## SubjectKind { #loki-grafana-com-v1beta1-SubjectKind }
(<code>string</code> alias)
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-Subject">Subject</a>)
</p>
<div>
<p>SubjectKind is a kind of LokiStack Gateway RBAC subject.</p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;group&#34;</p></td>
<td><p>Group represents a subject that is a group.</p>
</td>
</tr><tr><td><p>&#34;user&#34;</p></td>
<td><p>User represents a subject that is a user.</p>
</td>
</tr></tbody>
</table>

## TenantSecretSpec { #loki-grafana-com-v1beta1-TenantSecretSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-OIDCSpec">OIDCSpec</a>)
</p>
<div>
<p>TenantSecretSpec is a secret reference containing name only
for a secret living in the same namespace as the LokiStack custom resource.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>name</code><br/>
<em>
string
</em>
</td>
<td>
<p>Name of a secret in the namespace configured for tenant secrets.</p>
</td>
</tr>
</tbody>
</table>

## TenantsSpec { #loki-grafana-com-v1beta1-TenantsSpec }
<p>
(<em>Appears on:</em><a href="#loki-grafana-com-v1beta1-LokiStackSpec">LokiStackSpec</a>)
</p>
<div>
<p>TenantsSpec defines the mode, authentication and authorization
configuration of the lokiStack gateway component.</p>
</div>
<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>
<tr>
<td>
<code>mode</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-ModeType">
ModeType
</a>
</em>
</td>
<td>
<p>Mode defines the mode in which lokistack-gateway component will be configured.</p>
</td>
</tr>
<tr>
<td>
<code>authentication</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AuthenticationSpec">
[]AuthenticationSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Authentication defines the lokistack-gateway component authentication configuration spec per tenant.</p>
</td>
</tr>
<tr>
<td>
<code>authorization</code><br/>
<em>
<a href="#loki-grafana-com-v1beta1-AuthorizationSpec">
AuthorizationSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Authorization defines the lokistack-gateway component authorization configuration spec per tenant.</p>
</td>
</tr>
</tbody>
</table>
<hr/>


