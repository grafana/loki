---
title: "Feature Gates"
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
# config.loki.grafana.com/v1 { #config-loki-grafana-com-v1 }
<div>
<p>Package v1 contains API Schema definitions for the config v1 API group</p>
</div>
<b>Resource Types:</b>

## BuiltInCertManagement { #config-loki-grafana-com-v1-BuiltInCertManagement }
<p>
(<em>Appears on:</em><a href="#config-loki-grafana-com-v1-FeatureGates">FeatureGates</a>)
</p>
<div>
<p>BuiltInCertManagement is the configuration for the built-in facility to generate and rotate
TLS client and serving certificates for all LokiStack services and internal clients except
for the lokistack-gateway.</p>
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
<p>Enabled defines to flag to enable/disable built-in certificate management feature gate.</p>
</td>
</tr>
<tr>
<td>
<code>caValidity</code><br/>
<em>
string
</em>
</td>
<td>
<p>CACertValidity defines the total duration of the CA certificate validity.</p>
</td>
</tr>
<tr>
<td>
<code>caRefresh</code><br/>
<em>
string
</em>
</td>
<td>
<p>CACertRefresh defines the duration of the CA certificate validity until a rotation
should happen. It can be set up to 80% of CA certificate validity or equal to the
CA certificate validity. Latter should be used only for rotating only when expired.</p>
</td>
</tr>
<tr>
<td>
<code>certValidity</code><br/>
<em>
string
</em>
</td>
<td>
<p>CertValidity defines the total duration of the validity for all LokiStack certificates.</p>
</td>
</tr>
<tr>
<td>
<code>certRefresh</code><br/>
<em>
string
</em>
</td>
<td>
<p>CertRefresh defines the duration of the certificate validity until a rotation
should happen. It can be set up to 80% of certificate validity or equal to the
certificate validity. Latter should be used only for rotating only when expired.
The refresh is applied to all LokiStack certificates at once.</p>
</td>
</tr>
</tbody>
</table>

## FeatureGates { #config-loki-grafana-com-v1-FeatureGates }
<p>
(<em>Appears on:</em><a href="#config-loki-grafana-com-v1-ProjectConfig">ProjectConfig</a>)
</p>
<div>
<p>FeatureGates is the supported set of all operator feature gates.</p>
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
<code>serviceMonitors</code><br/>
<em>
bool
</em>
</td>
<td>
<p>ServiceMonitors enables creating a Prometheus-Operator managed ServiceMonitor
resource per LokiStack component.</p>
</td>
</tr>
<tr>
<td>
<code>serviceMonitorTlsEndpoints</code><br/>
<em>
bool
</em>
</td>
<td>
<p>ServiceMonitorTLSEndpoints enables TLS for the ServiceMonitor endpoints.</p>
</td>
</tr>
<tr>
<td>
<code>lokiStackAlerts</code><br/>
<em>
bool
</em>
</td>
<td>
<p>LokiStackAlerts enables creating Prometheus-Operator managed PrometheusRules
for common Loki alerts.</p>
</td>
</tr>
<tr>
<td>
<code>httpEncryption</code><br/>
<em>
bool
</em>
</td>
<td>
<p>HTTPEncryption enables TLS encryption for all HTTP LokiStack services.
Each HTTP service requires a secret named as the service with the following data:
- <code>tls.crt</code>: The TLS server side certificate.
- <code>tls.key</code>: The TLS key for server-side encryption.
In addition each service requires a configmap named as the LokiStack CR with the
suffix <code>-ca-bundle</code>, e.g. <code>lokistack-dev-ca-bundle</code> and the following data:
- <code>service-ca.crt</code>: The CA signing the service certificate in <code>tls.crt</code>.</p>
</td>
</tr>
<tr>
<td>
<code>grpcEncryption</code><br/>
<em>
bool
</em>
</td>
<td>
<p>GRPCEncryption enables TLS encryption for all GRPC LokiStack services.
Each GRPC service requires a secret named as the service with the following data:
- <code>tls.crt</code>: The TLS server side certificate.
- <code>tls.key</code>: The TLS key for server-side encryption.
In addition each service requires a configmap named as the LokiStack CR with the
suffix <code>-ca-bundle</code>, e.g. <code>lokistack-dev-ca-bundle</code> and the following data:
- <code>service-ca.crt</code>: The CA signing the service certificate in <code>tls.crt</code>.</p>
</td>
</tr>
<tr>
<td>
<code>builtInCertManagement</code><br/>
<em>
<a href="#config-loki-grafana-com-v1-BuiltInCertManagement">
BuiltInCertManagement
</a>
</em>
</td>
<td>
<p>BuiltInCertManagement enables the built-in facility for generating and rotating
TLS client and serving certificates for all LokiStack services and internal clients except
for the lokistack-gateway, In detail all internal Loki HTTP and GRPC communication is lifted
to require mTLS. For the lokistack-gateay you need to provide a secret with or use the <code>ServingCertsService</code>
on OpenShift:
- <code>tls.crt</code>: The TLS server side certificate.
- <code>tls.key</code>: The TLS key for server-side encryption.
In addition each service requires a configmap named as the LokiStack CR with the
suffix <code>-ca-bundle</code>, e.g. <code>lokistack-dev-ca-bundle</code> and the following data:
- <code>service-ca.crt</code>: The CA signing the service certificate in <code>tls.crt</code>.</p>
</td>
</tr>
<tr>
<td>
<code>lokiStackGateway</code><br/>
<em>
bool
</em>
</td>
<td>
<p>LokiStackGateway enables reconciling the reverse-proxy lokistack-gateway
component for multi-tenant authentication/authorization traffic control
to Loki.</p>
</td>
</tr>
<tr>
<td>
<code>grafanaLabsUsageReport</code><br/>
<em>
bool
</em>
</td>
<td>
<p>GrafanaLabsUsageReport enables the Grafana Labs usage report for Loki.
More details: <a href="https://grafana.com/docs/loki/latest/release-notes/v2-5/#usage-reporting">https://grafana.com/docs/loki/latest/release-notes/v2-5/#usage-reporting</a></p>
</td>
</tr>
<tr>
<td>
<code>restrictedPodSecurityStandard</code><br/>
<em>
bool
</em>
</td>
<td>
<p>RestrictedPodSecurityStandard enables compliance with the restrictive pod security standard.
More details: <a href="https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted">https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted</a></p>
</td>
</tr>
<tr>
<td>
<code>lokiStackWebhook</code><br/>
<em>
bool
</em>
</td>
<td>
<p>LokiStackWebhook enables the LokiStack CR validation and conversion webhooks.</p>
</td>
</tr>
<tr>
<td>
<code>alertingRuleWebhook</code><br/>
<em>
bool
</em>
</td>
<td>
<p>AlertingRuleWebhook enables the AlertingRule CR validation webhook.</p>
</td>
</tr>
<tr>
<td>
<code>recordingRuleWebhook</code><br/>
<em>
bool
</em>
</td>
<td>
<p>RecordingRuleWebhook enables the RecordingRule CR validation webhook.</p>
</td>
</tr>
<tr>
<td>
<code>rulerConfigWebhook</code><br/>
<em>
bool
</em>
</td>
<td>
<p>RulerConfigWebhook enables the RulerConfig CR validation webhook.</p>
</td>
</tr>
<tr>
<td>
<code>defaultNodeAffinity</code><br/>
<em>
bool
</em>
</td>
<td>
<p>When DefaultNodeAffinity is enabled the operator will set a default node affinity on all pods.
This will limit scheduling of the pods to Nodes with Linux.</p>
</td>
</tr>
<tr>
<td>
<code>openshift</code><br/>
<em>
<a href="#config-loki-grafana-com-v1-OpenShiftFeatureGates">
OpenShiftFeatureGates
</a>
</em>
</td>
<td>
<p>OpenShift contains a set of feature gates supported only on OpenShift.</p>
</td>
</tr>
<tr>
<td>
<code>tlsProfile</code><br/>
<em>
string
</em>
</td>
<td>
<p>TLSProfile allows to chose a TLS security profile. Enforced
when using HTTPEncryption or GRPCEncryption.</p>
</td>
</tr>
</tbody>
</table>

## OpenShiftFeatureGates { #config-loki-grafana-com-v1-OpenShiftFeatureGates }
<p>
(<em>Appears on:</em><a href="#config-loki-grafana-com-v1-FeatureGates">FeatureGates</a>)
</p>
<div>
<p>OpenShiftFeatureGates is the supported set of all operator features gates on OpenShift.</p>
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
<p>Enabled defines the flag to enable that these feature gates are used against OpenShift Container Platform releases.</p>
</td>
</tr>
<tr>
<td>
<code>servingCertsService</code><br/>
<em>
bool
</em>
</td>
<td>
<p>ServingCertsService enables OpenShift service-ca annotations on the lokistack-gateway service only
to use the in-platform CA and generate a TLS cert/key pair per service for
in-cluster data-in-transit encryption.
More details: <a href="https://docs.openshift.com/container-platform/latest/security/certificate_types_descriptions/service-ca-certificates.html">https://docs.openshift.com/container-platform/latest/security/certificate_types_descriptions/service-ca-certificates.html</a></p>
</td>
</tr>
<tr>
<td>
<code>ruleExtendedValidation</code><br/>
<em>
bool
</em>
</td>
<td>
<p>ExtendedRuleValidation enables extended validation of AlertingRule and RecordingRule
to enforce tenancy in an OpenShift context.</p>
</td>
</tr>
<tr>
<td>
<code>clusterTLSPolicy</code><br/>
<em>
bool
</em>
</td>
<td>
<p>ClusterTLSPolicy enables usage of TLS policies set in the API Server.
More details: <a href="https://docs.openshift.com/container-platform/4.11/security/tls-security-profiles.html">https://docs.openshift.com/container-platform/4.11/security/tls-security-profiles.html</a></p>
</td>
</tr>
<tr>
<td>
<code>clusterProxy</code><br/>
<em>
bool
</em>
</td>
<td>
<p>ClusterProxy enables usage of the proxy variables set in the proxy resource.
More details: <a href="https://docs.openshift.com/container-platform/4.11/networking/enable-cluster-wide-proxy.html#enable-cluster-wide-proxy">https://docs.openshift.com/container-platform/4.11/networking/enable-cluster-wide-proxy.html#enable-cluster-wide-proxy</a></p>
</td>
</tr>
<tr>
<td>
<code>dashboards</code><br/>
<em>
bool
</em>
</td>
<td>
<p>Dashboards enables the loki-mixin dashboards into the OpenShift Console</p>
</td>
</tr>
</tbody>
</table>

## ProjectConfig { #config-loki-grafana-com-v1-ProjectConfig }
<div>
<p>ProjectConfig is the Schema for the projectconfigs API</p>
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
<code>syncPeriod</code><br/>
<em>
<a href="https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>SyncPeriod determines the minimum frequency at which watched resources are
reconciled. A lower period will correct entropy more quickly, but reduce
responsiveness to change if there are many watched resources. Change this
value only if you know what you are doing. Defaults to 10 hours if unset.
there will a 10 percent jitter between the SyncPeriod of all controllers
so that all controllers will not send list requests simultaneously.</p>
</td>
</tr>
<tr>
<td>
<code>leaderElection</code><br/>
<em>
<a href="https://pkg.go.dev/k8s.io/component-base/config#LeaderElectionConfiguration">
Kubernetes v1alpha1.LeaderElectionConfiguration
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>LeaderElection is the LeaderElection config to be used when configuring
the manager.Manager leader election</p>
</td>
</tr>
<tr>
<td>
<code>cacheNamespace</code><br/>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>CacheNamespace if specified restricts the manager&rsquo;s cache to watch objects in
the desired namespace Defaults to all namespaces</p>
<p>Note: If a namespace is specified, controllers can still Watch for a
cluster-scoped resource (e.g Node).  For namespaced resources the cache
will only hold objects from the desired namespace.</p>
</td>
</tr>
<tr>
<td>
<code>gracefulShutDown</code><br/>
<em>
<a href="https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Duration">
Kubernetes meta/v1.Duration
</a>
</em>
</td>
<td>
<p>GracefulShutdownTimeout is the duration given to runnable to stop before the manager actually returns on stop.
To disable graceful shutdown, set to time.Duration(0)
To use graceful shutdown without timeout, set to a negative duration, e.G. time.Duration(-1)
The graceful shutdown is skipped for safety reasons in case the leader election lease is lost.</p>
</td>
</tr>
<tr>
<td>
<code>controller</code><br/>
<em>
<a href="https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/config/v1alpha1#ControllerConfigurationSpec">
K8S Controller-runtime v1alpha1.ControllerConfigurationSpec
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Controller contains global configuration options for controllers
registered within this manager.</p>
</td>
</tr>
<tr>
<td>
<code>metrics</code><br/>
<em>
<a href="https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/config/v1alpha1#ControllerMetrics">
K8S Controller-runtime v1alpha1.ControllerMetrics
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Metrics contains thw controller metrics configuration</p>
</td>
</tr>
<tr>
<td>
<code>health</code><br/>
<em>
<a href="https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/config/v1alpha1#ControllerHealth">
K8S Controller-runtime v1alpha1.ControllerHealth
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Health contains the controller health configuration</p>
</td>
</tr>
<tr>
<td>
<code>webhook</code><br/>
<em>
<a href="https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/config/v1alpha1#ControllerWebhook">
K8S Controller-runtime v1alpha1.ControllerWebhook
</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Webhook contains the controllers webhook configuration</p>
</td>
</tr>
<tr>
<td>
<code>featureGates</code><br/>
<em>
<a href="#config-loki-grafana-com-v1-FeatureGates">
FeatureGates
</a>
</em>
</td>
<td>
</td>
</tr>
</tbody>
</table>

## TLSProfileType { #config-loki-grafana-com-v1-TLSProfileType }
(<code>string</code> alias)
<div>
<p>TLSProfileType is a TLS security profile based on the Mozilla definitions:
<a href="https://wiki.mozilla.org/Security/Server_Side_TLS">https://wiki.mozilla.org/Security/Server_Side_TLS</a></p>
</div>
<table>
<thead>
<tr>
<th>Value</th>
<th>Description</th>
</tr>
</thead>
<tbody><tr><td><p>&#34;Intermediate&#34;</p></td>
<td><p>TLSProfileIntermediateType is a TLS security profile based on:
<a href="https://wiki.mozilla.org/Security/Server_Side_TLS#Intermediate_compatibility_.28default.29">https://wiki.mozilla.org/Security/Server_Side_TLS#Intermediate_compatibility_.28default.29</a></p>
</td>
</tr><tr><td><p>&#34;Modern&#34;</p></td>
<td><p>TLSProfileModernType is a TLS security profile based on:
<a href="https://wiki.mozilla.org/Security/Server_Side_TLS#Modern_compatibility">https://wiki.mozilla.org/Security/Server_Side_TLS#Modern_compatibility</a></p>
</td>
</tr><tr><td><p>&#34;Old&#34;</p></td>
<td><p>TLSProfileOldType is a TLS security profile based on:
<a href="https://wiki.mozilla.org/Security/Server_Side_TLS#Old_backward_compatibility">https://wiki.mozilla.org/Security/Server_Side_TLS#Old_backward_compatibility</a></p>
</td>
</tr></tbody>
</table>
<hr/>


