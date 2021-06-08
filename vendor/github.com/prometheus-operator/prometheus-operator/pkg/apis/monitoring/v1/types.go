// Copyright 2018 The prometheus-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	Version = "v1"

	PrometheusesKind  = "Prometheus"
	PrometheusName    = "prometheuses"
	PrometheusKindKey = "prometheus"

	AlertmanagersKind   = "Alertmanager"
	AlertmanagerName    = "alertmanagers"
	AlertManagerKindKey = "alertmanager"

	ServiceMonitorsKind   = "ServiceMonitor"
	ServiceMonitorName    = "servicemonitors"
	ServiceMonitorKindKey = "servicemonitor"

	PodMonitorsKind   = "PodMonitor"
	PodMonitorName    = "podmonitors"
	PodMonitorKindKey = "podmonitor"

	PrometheusRuleKind    = "PrometheusRule"
	PrometheusRuleName    = "prometheusrules"
	PrometheusRuleKindKey = "prometheusrule"

	ProbesKind   = "Probe"
	ProbeName    = "probes"
	ProbeKindKey = "probe"
)

// Prometheus defines a Prometheus deployment.
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:resource:categories="prometheus-operator"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of Prometheus"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description="The desired replicas number of Prometheuses"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Prometheus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of the desired behavior of the Prometheus cluster. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec PrometheusSpec `json:"spec"`
	// Most recent observed status of the Prometheus cluster. Read-only. Not
	// included when requesting from the apiserver, only from the Prometheus
	// Operator API itself. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Status *PrometheusStatus `json:"status,omitempty"`
}

// PrometheusList is a list of Prometheuses.
// +k8s:openapi-gen=true
type PrometheusList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of Prometheuses
	Items []*Prometheus `json:"items"`
}

// PrometheusSpec is a specification of the desired behavior of the Prometheus cluster. More info:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +k8s:openapi-gen=true
type PrometheusSpec struct {
	// PodMetadata configures Labels and Annotations which are propagated to the prometheus pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ServiceMonitors to be selected for target discovery. *Deprecated:* if
	// neither this nor podMonitorSelector are specified, configuration is
	// unmanaged.
	ServiceMonitorSelector *metav1.LabelSelector `json:"serviceMonitorSelector,omitempty"`
	// Namespace's labels to match for ServiceMonitor discovery. If nil, only
	// check own namespace.
	ServiceMonitorNamespaceSelector *metav1.LabelSelector `json:"serviceMonitorNamespaceSelector,omitempty"`
	// *Experimental* PodMonitors to be selected for target discovery.
	// *Deprecated:* if neither this nor serviceMonitorSelector are specified,
	// configuration is unmanaged.
	PodMonitorSelector *metav1.LabelSelector `json:"podMonitorSelector,omitempty"`
	// Namespace's labels to match for PodMonitor discovery. If nil, only
	// check own namespace.
	PodMonitorNamespaceSelector *metav1.LabelSelector `json:"podMonitorNamespaceSelector,omitempty"`
	// *Experimental* Probes to be selected for target discovery.
	ProbeSelector *metav1.LabelSelector `json:"probeSelector,omitempty"`
	// *Experimental* Namespaces to be selected for Probe discovery. If nil, only check own namespace.
	ProbeNamespaceSelector *metav1.LabelSelector `json:"probeNamespaceSelector,omitempty"`
	// Version of Prometheus to be deployed.
	Version string `json:"version,omitempty"`
	// Tag of Prometheus container image to be deployed. Defaults to the value of `version`.
	// Version is ignored if Tag is set.
	// Deprecated: use 'image' instead.  The image tag can be specified
	// as part of the image URL.
	Tag string `json:"tag,omitempty"`
	// SHA of Prometheus container image to be deployed. Defaults to the value of `version`.
	// Similar to a tag, but the SHA explicitly deploys an immutable container image.
	// Version and Tag are ignored if SHA is set.
	// Deprecated: use 'image' instead.  The image digest can be specified
	// as part of the image URL.
	SHA string `json:"sha,omitempty"`
	// When a Prometheus deployment is paused, no actions except for deletion
	// will be performed on the underlying objects.
	Paused bool `json:"paused,omitempty"`
	// Image if specified has precedence over baseImage, tag and sha
	// combinations. Specifying the version is still necessary to ensure the
	// Prometheus Operator knows what version of Prometheus is being
	// configured.
	Image *string `json:"image,omitempty"`
	// Base image to use for a Prometheus deployment.
	// Deprecated: use 'image' instead
	BaseImage string `json:"baseImage,omitempty"`
	// An optional list of references to secrets in the same namespace
	// to use for pulling prometheus and alertmanager images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Number of replicas of each shard to deploy for a Prometheus deployment.
	// Number of replicas multiplied by shards is the total number of Pods
	// created.
	Replicas *int32 `json:"replicas,omitempty"`
	// EXPERIMENTAL: Number of shards to distribute targets onto. Number of
	// replicas multiplied by shards is the total number of Pods created. Note
	// that scaling down shards will not reshard data onto remaining instances,
	// it must be manually moved. Increasing shards will not reshard data
	// either but it will continue to be available from the same instances. To
	// query globally use Thanos sidecar and Thanos querier or remote write
	// data to a central location. Sharding is done on the content of the
	// `__address__` target meta-label.
	Shards *int32 `json:"shards,omitempty"`
	// Name of Prometheus external label used to denote replica name.
	// Defaults to the value of `prometheus_replica`. External label will
	// _not_ be added when value is set to empty string (`""`).
	ReplicaExternalLabelName *string `json:"replicaExternalLabelName,omitempty"`
	// Name of Prometheus external label used to denote Prometheus instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	PrometheusExternalLabelName *string `json:"prometheusExternalLabelName,omitempty"`
	// Time duration Prometheus shall retain data for. Default is '24h',
	// and must match the regular expression `[0-9]+(ms|s|m|h|d|w|y)` (milliseconds seconds minutes hours days weeks years).
	Retention string `json:"retention,omitempty"`
	// Maximum amount of disk space used by blocks. Supported units: B, KB, MB, GB, TB, PB, EB. Ex: `512MB`.
	RetentionSize string `json:"retentionSize,omitempty"`
	// Disable prometheus compaction.
	DisableCompaction bool `json:"disableCompaction,omitempty"`
	// Enable compression of the write-ahead log using Snappy. This flag is
	// only available in versions of Prometheus >= 2.11.0.
	WALCompression *bool `json:"walCompression,omitempty"`
	// Log level for Prometheus to be configured with.
	LogLevel string `json:"logLevel,omitempty"`
	// Log format for Prometheus to be configured with.
	LogFormat string `json:"logFormat,omitempty"`
	// Interval between consecutive scrapes.
	ScrapeInterval string `json:"scrapeInterval,omitempty"`
	// Number of seconds to wait for target to respond before erroring.
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// Interval between consecutive evaluations.
	EvaluationInterval string `json:"evaluationInterval,omitempty"`
	// /--rules.*/ command-line arguments.
	Rules Rules `json:"rules,omitempty"`
	// The labels to add to any time series or alerts when communicating with
	// external systems (federation, remote storage, Alertmanager).
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`
	// Enable access to prometheus web admin API. Defaults to the value of `false`.
	// WARNING: Enabling the admin APIs enables mutating endpoints, to delete data,
	// shutdown Prometheus, and more. Enabling this should be done with care and the
	// user is advised to add additional authentication authorization via a proxy to
	// ensure only clients authorized to perform these actions can do so.
	// For more information see https://prometheus.io/docs/prometheus/latest/querying/api/#tsdb-admin-apis
	EnableAdminAPI bool `json:"enableAdminAPI,omitempty"`
	// Enable access to Prometheus disabled features. By default, no features are enabled.
	// Enabling disabled features is entirely outside the scope of what the maintainers will
	// support and by doing so, you accept that this behaviour may break at any
	// time without notice.
	// For more information see https://prometheus.io/docs/prometheus/latest/disabled_features/
	EnableFeatures []string `json:"enableFeatures,omitempty"`
	// The external URL the Prometheus instances will be available under. This is
	// necessary to generate correct URLs. This is necessary if Prometheus is not
	// served from root of a DNS name.
	ExternalURL string `json:"externalUrl,omitempty"`
	// The route prefix Prometheus registers HTTP handlers for. This is useful,
	// if using ExternalURL and a proxy is rewriting HTTP routes of a request,
	// and the actual ExternalURL is still true, but the server serves requests
	// under a different route prefix. For example for use with `kubectl proxy`.
	RoutePrefix string `json:"routePrefix,omitempty"`
	// QuerySpec defines the query command line flags when starting Prometheus.
	Query *QuerySpec `json:"query,omitempty"`
	// Storage spec to specify how storage shall be used.
	Storage *StorageSpec `json:"storage,omitempty"`
	// Volumes allows configuration of additional volumes on the output StatefulSet definition. Volumes specified will
	// be appended to other volumes that are generated as a result of StorageSpec objects.
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output StatefulSet definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the prometheus container,
	// that are generated as a result of StorageSpec objects.
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// WebSpec defines the web command line flags when starting Prometheus.
	Web *WebSpec `json:"web,omitempty"`
	// A selector to select which PrometheusRules to mount for loading alerting/recording
	// rules from. Until (excluding) Prometheus Operator v0.24.0 Prometheus
	// Operator will migrate any legacy rule ConfigMaps to PrometheusRule custom
	// resources selected by RuleSelector. Make sure it does not match any config
	// maps that you do not want to be migrated.
	RuleSelector *metav1.LabelSelector `json:"ruleSelector,omitempty"`
	// Namespaces to be selected for PrometheusRules discovery. If unspecified, only
	// the same namespace as the Prometheus object is in is used.
	RuleNamespaceSelector *metav1.LabelSelector `json:"ruleNamespaceSelector,omitempty"`
	// Define details regarding alerting.
	Alerting *AlertingSpec `json:"alerting,omitempty"`
	// Define resources requests and limits for single Pods.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// Define which Nodes the Pods are scheduled on.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// Prometheus Pods.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the Prometheus
	// object, which shall be mounted into the Prometheus Pods.
	// The Secrets are mounted into /etc/prometheus/secrets/<secret-name>.
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the Prometheus
	// object, which shall be mounted into the Prometheus Pods.
	// The ConfigMaps are mounted into /etc/prometheus/configmaps/<configmap-name>.
	ConfigMaps []string `json:"configMaps,omitempty"`
	// If specified, the pod's scheduling constraints.
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// If specified, the pod's tolerations.
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// If specified, the pod's topology spread constraints.
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// If specified, the remote_write spec. This is an experimental feature, it may change in any upcoming release in a breaking way.
	RemoteWrite []RemoteWriteSpec `json:"remoteWrite,omitempty"`
	// If specified, the remote_read spec. This is an experimental feature, it may change in any upcoming release in a breaking way.
	RemoteRead []RemoteReadSpec `json:"remoteRead,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// ListenLocal makes the Prometheus server listen on loopback, so that it
	// does not bind against the Pod IP.
	ListenLocal bool `json:"listenLocal,omitempty"`
	// Containers allows injecting additional containers or modifying operator
	// generated containers. This can be used to allow adding an authentication
	// proxy to a Prometheus pod or to change the behavior of an operator
	// generated container. Containers described here modify an operator
	// generated container if they share the same name and modifications are
	// done via a strategic merge patch. The current container names are:
	// `prometheus`, `config-reloader`, and `thanos-sidecar`. Overriding
	// containers is entirely outside the scope of what the maintainers will
	// support and by doing so, you accept that this behaviour may break at any
	// time without notice.
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the Prometheus configuration from external sources. Any errors
	// during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// AdditionalScrapeConfigs allows specifying a key of a Secret containing
	// additional Prometheus scrape configurations. Scrape configurations
	// specified are appended to the configurations generated by the Prometheus
	// Operator. Job configurations specified must have the form as specified
	// in the official Prometheus documentation:
	// https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config.
	// As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of Prometheus. It is advised to review Prometheus release
	// notes to ensure that no incompatible scrape configs are going to break
	// Prometheus after the upgrade.
	AdditionalScrapeConfigs *v1.SecretKeySelector `json:"additionalScrapeConfigs,omitempty"`
	// AdditionalAlertRelabelConfigs allows specifying a key of a Secret containing
	// additional Prometheus alert relabel configurations. Alert relabel configurations
	// specified are appended to the configurations generated by the Prometheus
	// Operator. Alert relabel configurations specified must have the form as specified
	// in the official Prometheus documentation:
	// https://prometheus.io/docs/prometheus/latest/configuration/configuration/#alert_relabel_configs.
	// As alert relabel configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of Prometheus. It is advised to review Prometheus release
	// notes to ensure that no incompatible alert relabel configs are going to break
	// Prometheus after the upgrade.
	AdditionalAlertRelabelConfigs *v1.SecretKeySelector `json:"additionalAlertRelabelConfigs,omitempty"`
	// AdditionalAlertManagerConfigs allows specifying a key of a Secret containing
	// additional Prometheus AlertManager configurations. AlertManager configurations
	// specified are appended to the configurations generated by the Prometheus
	// Operator. Job configurations specified must have the form as specified
	// in the official Prometheus documentation:
	// https://prometheus.io/docs/prometheus/latest/configuration/configuration/#alertmanager_config.
	// As AlertManager configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of Prometheus. It is advised to review Prometheus release
	// notes to ensure that no incompatible AlertManager configs are going to break
	// Prometheus after the upgrade.
	AdditionalAlertManagerConfigs *v1.SecretKeySelector `json:"additionalAlertManagerConfigs,omitempty"`
	// APIServerConfig allows specifying a host and auth methods to access apiserver.
	// If left empty, Prometheus is assumed to run inside of the cluster
	// and will discover API servers automatically and use the pod's CA certificate
	// and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/.
	APIServerConfig *APIServerConfig `json:"apiserverConfig,omitempty"`
	// Thanos configuration allows configuring various aspects of a Prometheus
	// server in a Thanos environment.
	//
	// This section is experimental, it may change significantly without
	// deprecation notice in any release.
	//
	// This is experimental and may change significantly without backward
	// compatibility in any release.
	Thanos *ThanosSpec `json:"thanos,omitempty"`
	// Priority class assigned to the Pods
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// Port name used for the pods and governing service.
	// This defaults to web
	PortName string `json:"portName,omitempty"`
	// ArbitraryFSAccessThroughSMs configures whether configuration
	// based on a service monitor can access arbitrary files on the file system
	// of the Prometheus container e.g. bearer token files.
	ArbitraryFSAccessThroughSMs ArbitraryFSAccessThroughSMsConfig `json:"arbitraryFSAccessThroughSMs,omitempty"`
	// OverrideHonorLabels if set to true overrides all user configured honor_labels.
	// If HonorLabels is set in ServiceMonitor or PodMonitor to true, this overrides honor_labels to false.
	OverrideHonorLabels bool `json:"overrideHonorLabels,omitempty"`
	// OverrideHonorTimestamps allows to globally enforce honoring timestamps in all scrape configs.
	OverrideHonorTimestamps bool `json:"overrideHonorTimestamps,omitempty"`
	// IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from
	// the podmonitor and servicemonitor configs, and they will only discover endpoints
	// within their current namespace.  Defaults to false.
	IgnoreNamespaceSelectors bool `json:"ignoreNamespaceSelectors,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// PrometheusRulesExcludedFromEnforce - list of prometheus rules to be excluded from enforcing
	// of adding namespace labels. Works only if enforcedNamespaceLabel set to true.
	// Make sure both ruleNamespace and ruleName are set for each pair
	PrometheusRulesExcludedFromEnforce []PrometheusRuleExcludeConfig `json:"prometheusRulesExcludedFromEnforce,omitempty"`
	// QueryLogFile specifies the file to which PromQL queries are logged.
	// Note that this location must be writable, and can be persisted using an attached volume.
	// Alternatively, the location can be set to a stdout location such as `/dev/stdout` to log
	// querie information to the default Prometheus log stream.
	// This is only available in versions of Prometheus >= 2.16.0.
	// For more details, see the Prometheus docs (https://prometheus.io/docs/guides/query-log/)
	QueryLogFile string `json:"queryLogFile,omitempty"`
	// EnforcedSampleLimit defines global limit on number of scraped samples
	// that will be accepted. This overrides any SampleLimit set per
	// ServiceMonitor or/and PodMonitor. It is meant to be used by admins to
	// enforce the SampleLimit to keep overall number of samples/series under
	// the desired limit.
	// Note that if SampleLimit is lower that value will be taken instead.
	EnforcedSampleLimit *uint64 `json:"enforcedSampleLimit,omitempty"`
	// AllowOverlappingBlocks enables vertical compaction and vertical query merge in Prometheus.
	// This is still experimental in Prometheus so it may change in any upcoming release.
	AllowOverlappingBlocks bool `json:"allowOverlappingBlocks,omitempty"`
	// EnforcedTargetLimit defines a global limit on the number of scraped targets.
	// This overrides any TargetLimit set per ServiceMonitor or/and PodMonitor.
	// It is meant to be used by admins to
	// enforce the TargetLimit to keep overall number of targets under
	// the desired limit.
	// Note that if TargetLimit is higher that value will be taken instead.
	EnforcedTargetLimit *uint64 `json:"enforcedTargetLimit,omitempty"`
}

// PrometheusRuleExcludeConfig enables users to configure excluded PrometheusRule names and their namespaces
// to be ignored while enforcing namespace label for alerts and metrics.
type PrometheusRuleExcludeConfig struct {
	// RuleNamespace - namespace of excluded rule
	RuleNamespace string `json:"ruleNamespace"`
	// RuleNamespace - name of excluded rule
	RuleName string `json:"ruleName"`
}

// ArbitraryFSAccessThroughSMsConfig enables users to configure, whether
// a service monitor selected by the Prometheus instance is allowed to use
// arbitrary files on the file system of the Prometheus container. This is the case
// when e.g. a service monitor specifies a BearerTokenFile in an endpoint. A
// malicious user could create a service monitor selecting arbitrary secret files
// in the Prometheus container. Those secrets would then be sent with a scrape
// request by Prometheus to a malicious target. Denying the above would prevent the
// attack, users can instead use the BearerTokenSecret field.
type ArbitraryFSAccessThroughSMsConfig struct {
	Deny bool `json:"deny,omitempty"`
}

// PrometheusStatus is the most recent observed status of the Prometheus cluster. Read-only. Not
// included when requesting from the apiserver, only from the Prometheus
// Operator API itself. More info:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +k8s:openapi-gen=true
type PrometheusStatus struct {
	// Represents whether any actions on the underlying managed objects are
	// being performed. Only delete actions will be performed.
	Paused bool `json:"paused"`
	// Total number of non-terminated pods targeted by this Prometheus deployment
	// (their labels match the selector).
	Replicas int32 `json:"replicas"`
	// Total number of non-terminated pods targeted by this Prometheus deployment
	// that have the desired version spec.
	UpdatedReplicas int32 `json:"updatedReplicas"`
	// Total number of available pods (ready for at least minReadySeconds)
	// targeted by this Prometheus deployment.
	AvailableReplicas int32 `json:"availableReplicas"`
	// Total number of unavailable pods targeted by this Prometheus deployment.
	UnavailableReplicas int32 `json:"unavailableReplicas"`
}

// AlertingSpec defines parameters for alerting configuration of Prometheus servers.
// +k8s:openapi-gen=true
type AlertingSpec struct {
	// AlertmanagerEndpoints Prometheus should fire alerts against.
	Alertmanagers []AlertmanagerEndpoints `json:"alertmanagers"`
}

// StorageSpec defines the configured storage for a group Prometheus servers.
// If neither `emptyDir` nor `volumeClaimTemplate` is specified, then by default an [EmptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) will be used.
// +k8s:openapi-gen=true
type StorageSpec struct {
	// Deprecated: subPath usage will be disabled by default in a future release, this option will become unnecessary.
	// DisableMountSubPath allows to remove any subPath usage in volume mounts.
	DisableMountSubPath bool `json:"disableMountSubPath,omitempty"`
	// EmptyDirVolumeSource to be used by the Prometheus StatefulSets. If specified, used in place of any volumeClaimTemplate. More
	// info: https://kubernetes.io/docs/concepts/storage/volumes/#emptydir
	EmptyDir *v1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	// A PVC spec to be used by the Prometheus StatefulSets.
	VolumeClaimTemplate EmbeddedPersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// EmbeddedPersistentVolumeClaim is an embedded version of k8s.io/api/core/v1.PersistentVolumeClaim.
// It contains TypeMeta and a reduced ObjectMeta.
type EmbeddedPersistentVolumeClaim struct {
	metav1.TypeMeta `json:",inline"`

	// EmbeddedMetadata contains metadata relevant to an EmbeddedResource.
	EmbeddedObjectMetadata `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the desired characteristics of a volume requested by a pod author.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Spec v1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information/status of a persistent volume claim.
	// Read-only.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Status v1.PersistentVolumeClaimStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// EmbeddedObjectMetadata contains a subset of the fields included in k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta
// Only fields which are relevant to embedded resources are included.
type EmbeddedObjectMetadata struct {
	// Name must be unique within a namespace. Is required when creating resources, although
	// some resources may allow a client to request the generation of an appropriate name
	// automatically. Name is primarily intended for creation idempotence and configuration
	// definition.
	// Cannot be updated.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: http://kubernetes.io/docs/user-guide/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: http://kubernetes.io/docs/user-guide/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

// QuerySpec defines the query command line flags when starting Prometheus.
// +k8s:openapi-gen=true
type QuerySpec struct {
	// The delta difference allowed for retrieving metrics during expression evaluations.
	LookbackDelta *string `json:"lookbackDelta,omitempty"`
	// Number of concurrent queries that can be run at once.
	MaxConcurrency *int32 `json:"maxConcurrency,omitempty"`
	// Maximum number of samples a single query can load into memory. Note that queries will fail if they would load more samples than this into memory, so this also limits the number of samples a query can return.
	MaxSamples *int32 `json:"maxSamples,omitempty"`
	// Maximum time a query may take before being aborted.
	Timeout *string `json:"timeout,omitempty"`
}

// WebSpec defines the query command line flags when starting Prometheus.
// +k8s:openapi-gen=true
type WebSpec struct {
	// The prometheus web page title
	PageTitle *string `json:"pageTitle,omitempty"`
}

// ThanosSpec defines parameters for a Prometheus server within a Thanos deployment.
// +k8s:openapi-gen=true
type ThanosSpec struct {
	// Image if specified has precedence over baseImage, tag and sha
	// combinations. Specifying the version is still necessary to ensure the
	// Prometheus Operator knows what version of Thanos is being
	// configured.
	Image *string `json:"image,omitempty"`
	// Version describes the version of Thanos to use.
	Version *string `json:"version,omitempty"`
	// Tag of Thanos sidecar container image to be deployed. Defaults to the value of `version`.
	// Version is ignored if Tag is set.
	// Deprecated: use 'image' instead.  The image tag can be specified
	// as part of the image URL.
	Tag *string `json:"tag,omitempty"`
	// SHA of Thanos container image to be deployed. Defaults to the value of `version`.
	// Similar to a tag, but the SHA explicitly deploys an immutable container image.
	// Version and Tag are ignored if SHA is set.
	// Deprecated: use 'image' instead.  The image digest can be specified
	// as part of the image URL.
	SHA *string `json:"sha,omitempty"`
	// Thanos base image if other than default.
	// Deprecated: use 'image' instead
	BaseImage *string `json:"baseImage,omitempty"`
	// Resources defines the resource requirements for the Thanos sidecar.
	// If not provided, no requests/limits will be set
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// ObjectStorageConfig configures object storage in Thanos.
	// Alternative to ObjectStorageConfigFile, and lower order priority.
	ObjectStorageConfig *v1.SecretKeySelector `json:"objectStorageConfig,omitempty"`
	// ObjectStorageConfigFile specifies the path of the object storage configuration file.
	// When used alongside with ObjectStorageConfig, ObjectStorageConfigFile takes precedence.
	ObjectStorageConfigFile *string `json:"objectStorageConfigFile,omitempty"`
	// ListenLocal makes the Thanos sidecar listen on loopback, so that it
	// does not bind against the Pod IP.
	ListenLocal bool `json:"listenLocal,omitempty"`
	// TracingConfig configures tracing in Thanos. This is an experimental feature, it may change in any upcoming release in a breaking way.
	TracingConfig *v1.SecretKeySelector `json:"tracingConfig,omitempty"`
	// TracingConfig specifies the path of the tracing configuration file.
	// When used alongside with TracingConfig, TracingConfigFile takes precedence.
	TracingConfigFile string `json:"tracingConfigFile,omitempty"`
	// GRPCServerTLSConfig configures the gRPC server from which Thanos Querier reads
	// recorded rule data.
	// Note: Currently only the CAFile, CertFile, and KeyFile fields are supported.
	// Maps to the '--grpc-server-tls-*' CLI args.
	GRPCServerTLSConfig *TLSConfig `json:"grpcServerTlsConfig,omitempty"`
	// LogLevel for Thanos sidecar to be configured with.
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for Thanos sidecar to be configured with.
	LogFormat string `json:"logFormat,omitempty"`
	// MinTime for Thanos sidecar to be configured with. Option can be a constant time in RFC3339 format or time duration relative to current time, such as -1d or 2h45m. Valid duration units are ms, s, m, h, d, w, y.
	MinTime string `json:"minTime,omitempty"`
}

// RemoteWriteSpec defines the remote_write configuration for prometheus.
// +k8s:openapi-gen=true
type RemoteWriteSpec struct {
	// The URL of the endpoint to send samples to.
	URL string `json:"url"`
	// The name of the remote write queue, must be unique if specified. The
	// name is used in metrics and logging in order to differentiate queues.
	// Only valid in Prometheus versions 2.15.0 and newer.
	Name string `json:"name,omitempty"`
	// Timeout for requests to the remote write endpoint.
	RemoteTimeout string `json:"remoteTimeout,omitempty"`
	// Custom HTTP headers to be sent along with each remote write request.
	// Be aware that headers that are set by Prometheus itself can't be overwritten.
	// Only valid in Prometheus versions 2.25.0 and newer.
	Headers map[string]string `json:"headers,omitempty"`
	// The list of remote write relabel configurations.
	WriteRelabelConfigs []RelabelConfig `json:"writeRelabelConfigs,omitempty"`
	//BasicAuth for the URL.
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Bearer token for remote write.
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for remote write.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLS Config to use for remote write.
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// Optional ProxyURL
	ProxyURL string `json:"proxyUrl,omitempty"`
	// QueueConfig allows tuning of the remote write queue parameters.
	QueueConfig *QueueConfig `json:"queueConfig,omitempty"`
	// MetadataConfig configures the sending of series metadata to remote storage.
	MetadataConfig *MetadataConfig `json:"metadataConfig,omitempty"`
}

// QueueConfig allows the tuning of remote_write queue_config parameters. This object
// is referenced in the RemoteWriteSpec object.
// +k8s:openapi-gen=true
type QueueConfig struct {
	// Capacity is the number of samples to buffer per shard before we start dropping them.
	Capacity int `json:"capacity,omitempty"`
	// MinShards is the minimum number of shards, i.e. amount of concurrency.
	MinShards int `json:"minShards,omitempty"`
	// MaxShards is the maximum number of shards, i.e. amount of concurrency.
	MaxShards int `json:"maxShards,omitempty"`
	// MaxSamplesPerSend is the maximum number of samples per send.
	MaxSamplesPerSend int `json:"maxSamplesPerSend,omitempty"`
	// BatchSendDeadline is the maximum time a sample will wait in buffer.
	BatchSendDeadline string `json:"batchSendDeadline,omitempty"`
	// MaxRetries is the maximum number of times to retry a batch on recoverable errors.
	MaxRetries int `json:"maxRetries,omitempty"`
	// MinBackoff is the initial retry delay. Gets doubled for every retry.
	MinBackoff string `json:"minBackoff,omitempty"`
	// MaxBackoff is the maximum retry delay.
	MaxBackoff string `json:"maxBackoff,omitempty"`
}

// RemoteReadSpec defines the remote_read configuration for prometheus.
// +k8s:openapi-gen=true
type RemoteReadSpec struct {
	// The URL of the endpoint to send samples to.
	URL string `json:"url"`
	// The name of the remote read queue, must be unique if specified. The name
	// is used in metrics and logging in order to differentiate read
	// configurations.  Only valid in Prometheus versions 2.15.0 and newer.
	Name string `json:"name,omitempty"`
	// An optional list of equality matchers which have to be present
	// in a selector to query the remote read endpoint.
	RequiredMatchers map[string]string `json:"requiredMatchers,omitempty"`
	// Timeout for requests to the remote read endpoint.
	RemoteTimeout string `json:"remoteTimeout,omitempty"`
	// Whether reads should be made for queries for time ranges that
	// the local storage should have complete data for.
	ReadRecent bool `json:"readRecent,omitempty"`
	// BasicAuth for the URL.
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Bearer token for remote read.
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for remote read.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLS Config to use for remote read.
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// Optional ProxyURL
	ProxyURL string `json:"proxyUrl,omitempty"`
}

// RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion.
// It defines `<metric_relabel_configs>`-section of Prometheus configuration.
// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs
// +k8s:openapi-gen=true
type RelabelConfig struct {
	//The source labels select values from existing labels. Their content is concatenated
	//using the configured separator and matched against the configured regular expression
	//for the replace, keep, and drop actions.
	SourceLabels []string `json:"sourceLabels,omitempty"`
	//Separator placed between concatenated source label values. default is ';'.
	Separator string `json:"separator,omitempty"`
	//Label to which the resulting value is written in a replace action.
	//It is mandatory for replace actions. Regex capture groups are available.
	TargetLabel string `json:"targetLabel,omitempty"`
	//Regular expression against which the extracted value is matched. Default is '(.*)'
	Regex string `json:"regex,omitempty"`
	// Modulus to take of the hash of the source label values.
	Modulus uint64 `json:"modulus,omitempty"`
	//Replacement value against which a regex replace is performed if the
	//regular expression matches. Regex capture groups are available. Default is '$1'
	Replacement string `json:"replacement,omitempty"`
	// Action to perform based on regex matching. Default is 'replace'
	Action string `json:"action,omitempty"`
}

// APIServerConfig defines a host and auth methods to access apiserver.
// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config
// +k8s:openapi-gen=true
type APIServerConfig struct {
	// Host of apiserver.
	// A valid string consisting of a hostname or IP followed by an optional port number
	Host string `json:"host"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Bearer token for accessing apiserver.
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for accessing apiserver.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLS Config to use for accessing apiserver.
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// AlertmanagerEndpoints defines a selection of a single Endpoints object
// containing alertmanager IPs to fire alerts against.
// +k8s:openapi-gen=true
type AlertmanagerEndpoints struct {
	// Namespace of Endpoints object.
	Namespace string `json:"namespace"`
	// Name of Endpoints object in Namespace.
	Name string `json:"name"`
	// Port the Alertmanager API is exposed on.
	Port intstr.IntOrString `json:"port"`
	// Scheme to use when firing alerts.
	Scheme string `json:"scheme,omitempty"`
	// Prefix for the HTTP path alerts are pushed to.
	PathPrefix string `json:"pathPrefix,omitempty"`
	// TLS Config to use for alertmanager connection.
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// BearerTokenFile to read from filesystem to use when authenticating to
	// Alertmanager.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Version of the Alertmanager API that Prometheus uses to send alerts. It
	// can be "v1" or "v2".
	APIVersion string `json:"apiVersion,omitempty"`
	// Timeout is a per-target Alertmanager timeout when pushing alerts.
	Timeout *string `json:"timeout,omitempty"`
}

// ServiceMonitor defines monitoring for a set of services.
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:resource:categories="prometheus-operator"
type ServiceMonitor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of desired Service selection for target discovery by
	// Prometheus.
	Spec ServiceMonitorSpec `json:"spec"`
}

// ServiceMonitorSpec contains specification parameters for a ServiceMonitor.
// +k8s:openapi-gen=true
type ServiceMonitorSpec struct {
	// The label to use to retrieve the job name from.
	JobLabel string `json:"jobLabel,omitempty"`
	// TargetLabels transfers labels on the Kubernetes Service onto the target.
	TargetLabels []string `json:"targetLabels,omitempty"`
	// PodTargetLabels transfers labels on the Kubernetes Pod onto the target.
	PodTargetLabels []string `json:"podTargetLabels,omitempty"`
	// A list of endpoints allowed as part of this ServiceMonitor.
	Endpoints []Endpoint `json:"endpoints"`
	// Selector to select Endpoints objects.
	Selector metav1.LabelSelector `json:"selector"`
	// Selector to select which namespaces the Endpoints objects are discovered from.
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
	// TargetLimit defines a limit on the number of scraped targets that will be accepted.
	TargetLimit uint64 `json:"targetLimit,omitempty"`
}

// Endpoint defines a scrapeable endpoint serving Prometheus metrics.
// +k8s:openapi-gen=true
type Endpoint struct {
	// Name of the service port this endpoint refers to. Mutually exclusive with targetPort.
	Port string `json:"port,omitempty"`
	// Name or number of the target port of the Pod behind the Service, the port must be specified with container port property. Mutually exclusive with port.
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`
	// HTTP path to scrape for metrics.
	Path string `json:"path,omitempty"`
	// HTTP scheme to use for scraping.
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	Params map[string][]string `json:"params,omitempty"`
	// Interval at which metrics should be scraped
	Interval string `json:"interval,omitempty"`
	// Timeout after which the scrape is ended
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// TLS configuration to use when scraping the endpoint
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// File to read bearer token for scraping targets.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the service monitor and accessible by
	// the Prometheus Operator.
	BearerTokenSecret v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether Prometheus respects the timestamps present in scraped data.
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// More info: https://prometheus.io/docs/operating/configuration/#endpoints
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// MetricRelabelConfigs to apply to samples before ingestion.
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelings,omitempty"`
	// RelabelConfigs to apply to samples before scraping.
	// Prometheus Operator automatically adds relabelings for a few standard Kubernetes fields
	// and replaces original scrape job name with __tmp_prometheus_job_name.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	RelabelConfigs []*RelabelConfig `json:"relabelings,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	ProxyURL *string `json:"proxyUrl,omitempty"`
}

// PodMonitor defines monitoring for a set of pods.
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:resource:categories="prometheus-operator"
type PodMonitor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of desired Pod selection for target discovery by Prometheus.
	Spec PodMonitorSpec `json:"spec"`
}

// PodMonitorSpec contains specification parameters for a PodMonitor.
// +k8s:openapi-gen=true
type PodMonitorSpec struct {
	// The label to use to retrieve the job name from.
	JobLabel string `json:"jobLabel,omitempty"`
	// PodTargetLabels transfers labels on the Kubernetes Pod onto the target.
	PodTargetLabels []string `json:"podTargetLabels,omitempty"`
	// A list of endpoints allowed as part of this PodMonitor.
	PodMetricsEndpoints []PodMetricsEndpoint `json:"podMetricsEndpoints"`
	// Selector to select Pod objects.
	Selector metav1.LabelSelector `json:"selector"`
	// Selector to select which namespaces the Endpoints objects are discovered from.
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
	// TargetLimit defines a limit on the number of scraped targets that will be accepted.
	TargetLimit uint64 `json:"targetLimit,omitempty"`
}

// PodMetricsEndpoint defines a scrapeable endpoint of a Kubernetes Pod serving Prometheus metrics.
// +k8s:openapi-gen=true
type PodMetricsEndpoint struct {
	// Name of the pod port this endpoint refers to. Mutually exclusive with targetPort.
	Port string `json:"port,omitempty"`
	// Deprecated: Use 'port' instead.
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`
	// HTTP path to scrape for metrics.
	Path string `json:"path,omitempty"`
	// HTTP scheme to use for scraping.
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	Params map[string][]string `json:"params,omitempty"`
	// Interval at which metrics should be scraped
	Interval string `json:"interval,omitempty"`
	// Timeout after which the scrape is ended
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// TLS configuration to use when scraping the endpoint.
	TLSConfig *PodMetricsEndpointTLSConfig `json:"tlsConfig,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the pod monitor and accessible by
	// the Prometheus Operator.
	BearerTokenSecret v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether Prometheus respects the timestamps present in scraped data.
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication.
	// More info: https://prometheus.io/docs/operating/configuration/#endpoint
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// MetricRelabelConfigs to apply to samples before ingestion.
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelings,omitempty"`
	// RelabelConfigs to apply to samples before scraping.
	// Prometheus Operator automatically adds relabelings for a few standard Kubernetes fields
	// and replaces original scrape job name with __tmp_prometheus_job_name.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	RelabelConfigs []*RelabelConfig `json:"relabelings,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	ProxyURL *string `json:"proxyUrl,omitempty"`
}

// PodMetricsEndpointTLSConfig specifies TLS configuration parameters.
// +k8s:openapi-gen=true
type PodMetricsEndpointTLSConfig struct {
	SafeTLSConfig `json:",inline"`
}

// Probe defines monitoring for a set of static targets or ingresses.
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:resource:categories="prometheus-operator"
type Probe struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of desired Ingress selection for target discovery by Prometheus.
	Spec ProbeSpec `json:"spec"`
}

// ProbeSpec contains specification parameters for a Probe.
// +k8s:openapi-gen=true
type ProbeSpec struct {
	// The job name assigned to scraped metrics by default.
	JobName string `json:"jobName,omitempty"`
	// Specification for the prober to use for probing targets.
	// The prober.URL parameter is required. Targets cannot be probed if left empty.
	ProberSpec ProberSpec `json:"prober,omitempty"`
	// The module to use for probing specifying how to probe the target.
	// Example module configuring in the blackbox exporter:
	// https://github.com/prometheus/blackbox_exporter/blob/master/example.yml
	Module string `json:"module,omitempty"`
	// Targets defines a set of static and/or dynamically discovered targets to be probed using the prober.
	Targets ProbeTargets `json:"targets,omitempty"`
	// Interval at which targets are probed using the configured prober.
	// If not specified Prometheus' global scrape interval is used.
	Interval string `json:"interval,omitempty"`
	// Timeout for scraping metrics from the Prometheus exporter.
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// TLS configuration to use when scraping the endpoint.
	TLSConfig *ProbeTLSConfig `json:"tlsConfig,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the probe and accessible by
	// the Prometheus Operator.
	BearerTokenSecret v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication.
	// More info: https://prometheus.io/docs/operating/configuration/#endpoint
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
}

// ProbeTargets defines a set of static and dynamically discovered targets for the prober.
// +k8s:openapi-gen=true
type ProbeTargets struct {
	// StaticConfig defines static targets which are considers for probing.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#static_config.
	StaticConfig *ProbeTargetStaticConfig `json:"staticConfig,omitempty"`
	// Ingress defines the set of dynamically discovered ingress objects which hosts are considered for probing.
	Ingress *ProbeTargetIngress `json:"ingress,omitempty"`
}

// ProbeTargetStaticConfig defines the set of static targets considered for probing.
// +k8s:openapi-gen=true
type ProbeTargetStaticConfig struct {
	// Targets is a list of URLs to probe using the configured prober.
	Targets []string `json:"static,omitempty"`
	// Labels assigned to all metrics scraped from the targets.
	Labels map[string]string `json:"labels,omitempty"`
	// RelabelConfigs to apply to samples before ingestion.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	RelabelConfigs []*RelabelConfig `json:"relabelingConfigs,omitempty"`
}

// ProbeTargetIngress defines the set of Ingress objects considered for probing.
// +k8s:openapi-gen=true
type ProbeTargetIngress struct {
	// Select Ingress objects by labels.
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// Select Ingress objects by namespace.
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// RelabelConfigs to apply to samples before ingestion.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	RelabelConfigs []*RelabelConfig `json:"relabelingConfigs,omitempty"`
}

// ProberSpec contains specification parameters for the Prober used for probing.
// +k8s:openapi-gen=true
type ProberSpec struct {
	// Mandatory URL of the prober.
	URL string `json:"url"`
	// HTTP scheme to use for scraping.
	// Defaults to `http`.
	Scheme string `json:"scheme,omitempty"`
	// Path to collect metrics from.
	// Defaults to `/probe`.
	Path string `json:"path,omitempty"`
}

// BasicAuth allow an endpoint to authenticate over basic authentication
// More info: https://prometheus.io/docs/operating/configuration/#endpoints
// +k8s:openapi-gen=true
type BasicAuth struct {
	// The secret in the service monitor namespace that contains the username
	// for authentication.
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// The secret in the service monitor namespace that contains the password
	// for authentication.
	Password v1.SecretKeySelector `json:"password,omitempty"`
}

// SecretOrConfigMap allows to specify data as a Secret or ConfigMap. Fields are mutually exclusive.
type SecretOrConfigMap struct {
	// Secret containing data to use for the targets.
	Secret *v1.SecretKeySelector `json:"secret,omitempty"`
	// ConfigMap containing data to use for the targets.
	ConfigMap *v1.ConfigMapKeySelector `json:"configMap,omitempty"`
}

// SecretOrConfigMapValidationError is returned by SecretOrConfigMap.Validate()
// on semantically invalid configurations.
// +k8s:openapi-gen=false
type SecretOrConfigMapValidationError struct {
	err string
}

func (e *SecretOrConfigMapValidationError) Error() string {
	return e.err
}

// Validate semantically validates the given TLSConfig.
func (c *SecretOrConfigMap) Validate() error {
	if c.Secret != nil && c.ConfigMap != nil {
		return &SecretOrConfigMapValidationError{"SecretOrConfigMap can not specify both Secret and ConfigMap"}
	}

	return nil
}

// SafeTLSConfig specifies safe TLS configuration parameters.
// +k8s:openapi-gen=true
type SafeTLSConfig struct {
	// Struct containing the CA cert to use for the targets.
	CA SecretOrConfigMap `json:"ca,omitempty"`
	// Struct containing the client cert file for the targets.
	Cert SecretOrConfigMap `json:"cert,omitempty"`
	// Secret containing the client key file for the targets.
	KeySecret *v1.SecretKeySelector `json:"keySecret,omitempty"`
	// Used to verify the hostname for the targets.
	ServerName string `json:"serverName,omitempty"`
	// Disable target certificate validation.
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// Validate semantically validates the given SafeTLSConfig.
func (c *SafeTLSConfig) Validate() error {
	if c.CA != (SecretOrConfigMap{}) {
		if err := c.CA.Validate(); err != nil {
			return err
		}
	}

	if c.Cert != (SecretOrConfigMap{}) {
		if err := c.Cert.Validate(); err != nil {
			return err
		}
	}

	if c.Cert != (SecretOrConfigMap{}) && c.KeySecret == nil {
		return &TLSConfigValidationError{"client cert specified without client key"}
	}

	if c.KeySecret != nil && c.Cert == (SecretOrConfigMap{}) {
		return &TLSConfigValidationError{"client key specified without client cert"}
	}

	return nil
}

// TLSConfig extends the safe TLS configuration with file parameters.
// +k8s:openapi-gen=true
type TLSConfig struct {
	SafeTLSConfig `json:",inline"`
	// Path to the CA cert in the Prometheus container to use for the targets.
	CAFile string `json:"caFile,omitempty"`
	// Path to the client cert file in the Prometheus container for the targets.
	CertFile string `json:"certFile,omitempty"`
	// Path to the client key file in the Prometheus container for the targets.
	KeyFile string `json:"keyFile,omitempty"`
}

// TLSConfigValidationError is returned by TLSConfig.Validate() on semantically
// invalid tls configurations.
// +k8s:openapi-gen=false
type TLSConfigValidationError struct {
	err string
}

func (e *TLSConfigValidationError) Error() string {
	return e.err
}

// Validate semantically validates the given TLSConfig.
func (c *TLSConfig) Validate() error {
	if c.CA != (SecretOrConfigMap{}) {
		if c.CAFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CAFile and CA"}
		}
		if err := c.CA.Validate(); err != nil {
			return &TLSConfigValidationError{"tls config CA is invalid"}
		}
	}

	if c.Cert != (SecretOrConfigMap{}) {
		if c.CertFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CertFile and Cert"}
		}
		if err := c.Cert.Validate(); err != nil {
			return &TLSConfigValidationError{"tls config Cert is invalid"}
		}
	}

	if c.KeyFile != "" && c.KeySecret != nil {
		return &TLSConfigValidationError{"tls config can not both specify KeyFile and KeySecret"}
	}

	hasCert := c.CertFile != "" || c.Cert != (SecretOrConfigMap{})
	hasKey := c.KeyFile != "" || c.KeySecret != nil

	if hasCert && !hasKey {
		return &TLSConfigValidationError{"tls config can not specify client cert without client key"}
	}

	if hasKey && !hasCert {
		return &TLSConfigValidationError{"tls config can not specify client key without client cert"}
	}

	return nil
}

// ServiceMonitorList is a list of ServiceMonitors.
// +k8s:openapi-gen=true
type ServiceMonitorList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of ServiceMonitors
	Items []*ServiceMonitor `json:"items"`
}

// PodMonitorList is a list of PodMonitors.
// +k8s:openapi-gen=true
type PodMonitorList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of PodMonitors
	Items []*PodMonitor `json:"items"`
}

// ProbeList is a list of Probes.
// +k8s:openapi-gen=true
type ProbeList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of Probes
	Items []*Probe `json:"items"`
}

// PrometheusRuleList is a list of PrometheusRules.
// +k8s:openapi-gen=true
type PrometheusRuleList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of Rules
	Items []*PrometheusRule `json:"items"`
}

// PrometheusRule defines recording and alerting rules for a Prometheus instance
// +genclient
// +k8s:openapi-gen=true
type PrometheusRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of desired alerting rule definitions for Prometheus.
	Spec PrometheusRuleSpec `json:"spec"`
}

// PrometheusRuleSpec contains specification parameters for a Rule.
// +k8s:openapi-gen=true
type PrometheusRuleSpec struct {
	// Content of Prometheus rule file
	Groups []RuleGroup `json:"groups,omitempty"`
}

// RuleGroup and Rule are copied instead of vendored because the
// upstream Prometheus struct definitions don't have json struct tags.

// RuleGroup is a list of sequentially evaluated recording and alerting rules.
// Note: PartialResponseStrategy is only used by ThanosRuler and will
// be ignored by Prometheus instances.  Valid values for this field are 'warn'
// or 'abort'.  More info: https://github.com/thanos-io/thanos/blob/master/docs/components/rule.md#partial-response
// +k8s:openapi-gen=true
type RuleGroup struct {
	Name                    string `json:"name"`
	Interval                string `json:"interval,omitempty"`
	Rules                   []Rule `json:"rules"`
	PartialResponseStrategy string `json:"partial_response_strategy,omitempty"`
}

// Rule describes an alerting or recording rule.
// +k8s:openapi-gen=true
type Rule struct {
	Record      string             `json:"record,omitempty"`
	Alert       string             `json:"alert,omitempty"`
	Expr        intstr.IntOrString `json:"expr"`
	For         string             `json:"for,omitempty"`
	Labels      map[string]string  `json:"labels,omitempty"`
	Annotations map[string]string  `json:"annotations,omitempty"`
}

// Alertmanager describes an Alertmanager cluster.
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:resource:categories="prometheus-operator"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of Alertmanager"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description="The desired replicas number of Alertmanagers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Alertmanager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of the desired behavior of the Alertmanager cluster. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec AlertmanagerSpec `json:"spec"`
	// Most recent observed status of the Alertmanager cluster. Read-only. Not
	// included when requesting from the apiserver, only from the Prometheus
	// Operator API itself. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Status *AlertmanagerStatus `json:"status,omitempty"`
}

// AlertmanagerSpec is a specification of the desired behavior of the Alertmanager cluster. More info:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +k8s:openapi-gen=true
type AlertmanagerSpec struct {
	// PodMetadata configures Labels and Annotations which are propagated to the alertmanager pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image if specified has precedence over baseImage, tag and sha
	// combinations. Specifying the version is still necessary to ensure the
	// Prometheus Operator knows what version of Alertmanager is being
	// configured.
	Image *string `json:"image,omitempty"`
	// Version the cluster should be on.
	Version string `json:"version,omitempty"`
	// Tag of Alertmanager container image to be deployed. Defaults to the value of `version`.
	// Version is ignored if Tag is set.
	// Deprecated: use 'image' instead.  The image tag can be specified
	// as part of the image URL.
	Tag string `json:"tag,omitempty"`
	// SHA of Alertmanager container image to be deployed. Defaults to the value of `version`.
	// Similar to a tag, but the SHA explicitly deploys an immutable container image.
	// Version and Tag are ignored if SHA is set.
	// Deprecated: use 'image' instead.  The image digest can be specified
	// as part of the image URL.
	SHA string `json:"sha,omitempty"`
	// Base image that is used to deploy pods, without tag.
	// Deprecated: use 'image' instead
	BaseImage string `json:"baseImage,omitempty"`
	// An optional list of references to secrets in the same namespace
	// to use for pulling prometheus and alertmanager images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the Alertmanager
	// object, which shall be mounted into the Alertmanager Pods.
	// The Secrets are mounted into /etc/alertmanager/secrets/<secret-name>.
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the Alertmanager
	// object, which shall be mounted into the Alertmanager Pods.
	// The ConfigMaps are mounted into /etc/alertmanager/configmaps/<configmap-name>.
	ConfigMaps []string `json:"configMaps,omitempty"`
	// ConfigSecret is the name of a Kubernetes Secret in the same namespace as the
	// Alertmanager object, which contains configuration for this Alertmanager
	// instance. Defaults to 'alertmanager-<alertmanager-name>'
	// The secret is mounted into /etc/alertmanager/config.
	ConfigSecret string `json:"configSecret,omitempty"`
	// Log level for Alertmanager to be configured with.
	LogLevel string `json:"logLevel,omitempty"`
	// Log format for Alertmanager to be configured with.
	LogFormat string `json:"logFormat,omitempty"`
	// Size is the expected size of the alertmanager cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	Replicas *int32 `json:"replicas,omitempty"`
	// Time duration Alertmanager shall retain data for. Default is '120h',
	// and must match the regular expression `[0-9]+(ms|s|m|h)` (milliseconds seconds minutes hours).
	Retention string `json:"retention,omitempty"`
	// Storage is the definition of how storage will be used by the Alertmanager
	// instances.
	Storage *StorageSpec `json:"storage,omitempty"`
	// Volumes allows configuration of additional volumes on the output StatefulSet definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output StatefulSet definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the alertmanager container,
	// that are generated as a result of StorageSpec objects.
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// The external URL the Alertmanager instances will be available under. This is
	// necessary to generate correct URLs. This is necessary if Alertmanager is not
	// served from root of a DNS name.
	ExternalURL string `json:"externalUrl,omitempty"`
	// The route prefix Alertmanager registers HTTP handlers for. This is useful,
	// if using ExternalURL and a proxy is rewriting HTTP routes of a request,
	// and the actual ExternalURL is still true, but the server serves requests
	// under a different route prefix. For example for use with `kubectl proxy`.
	RoutePrefix string `json:"routePrefix,omitempty"`
	// If set to true all actions on the underlying managed objects are not
	// goint to be performed, except for delete actions.
	Paused bool `json:"paused,omitempty"`
	// Define which Nodes the Pods are scheduled on.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Define resources requests and limits for single Pods.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// If specified, the pod's scheduling constraints.
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// If specified, the pod's tolerations.
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// If specified, the pod's topology spread constraints.
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// Prometheus Pods.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// ListenLocal makes the Alertmanager server listen on loopback, so that it
	// does not bind against the Pod IP. Note this is only for the Alertmanager
	// UI, not the gossip communication.
	ListenLocal bool `json:"listenLocal,omitempty"`
	// Containers allows injecting additional containers. This is meant to
	// allow adding an authentication proxy to an Alertmanager pod.
	// Containers described here modify an operator generated container if they
	// share the same name and modifications are done via a strategic merge
	// patch. The current container names are: `alertmanager` and
	// `config-reloader`. Overriding containers is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that
	// this behaviour may break at any time without notice.
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the Alertmanager configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// Priority class assigned to the Pods
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// AdditionalPeers allows injecting a set of additional Alertmanagers to peer with to form a highly available cluster.
	AdditionalPeers []string `json:"additionalPeers,omitempty"`
	// ClusterAdvertiseAddress is the explicit address to advertise in cluster.
	// Needs to be provided for non RFC1918 [1] (public) addresses.
	// [1] RFC1918: https://tools.ietf.org/html/rfc1918
	ClusterAdvertiseAddress string `json:"clusterAdvertiseAddress,omitempty"`
	// Interval between gossip attempts.
	ClusterGossipInterval string `json:"clusterGossipInterval,omitempty"`
	// Interval between pushpull attempts.
	ClusterPushpullInterval string `json:"clusterPushpullInterval,omitempty"`
	// Timeout for cluster peering.
	ClusterPeerTimeout string `json:"clusterPeerTimeout,omitempty"`
	// Port name used for the pods and governing service.
	// This defaults to web
	PortName string `json:"portName,omitempty"`
	// ForceEnableClusterMode ensures Alertmanager does not deactivate the cluster mode when running with a single replica.
	// Use case is e.g. spanning an Alertmanager cluster across Kubernetes clusters with a single replica in each.
	ForceEnableClusterMode bool `json:"forceEnableClusterMode,omitempty"`
	// AlertmanagerConfigs to be selected for to merge and configure Alertmanager with.
	AlertmanagerConfigSelector *metav1.LabelSelector `json:"alertmanagerConfigSelector,omitempty"`
	// Namespaces to be selected for AlertmanagerConfig discovery. If nil, only
	// check own namespace.
	AlertmanagerConfigNamespaceSelector *metav1.LabelSelector `json:"alertmanagerConfigNamespaceSelector,omitempty"`
}

// AlertmanagerList is a list of Alertmanagers.
// +k8s:openapi-gen=true
type AlertmanagerList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of Alertmanagers
	Items []Alertmanager `json:"items"`
}

// Configures the sending of series metadata to remote storage.
// +k8s:openapi-gen=true
type MetadataConfig struct {
	// Whether metric metadata is sent to remote storage or not.
	Send bool `json:"send,omitempty"`
	// How frequently metric metadata is sent to remote storage.
	SendInterval string `json:"sendInterval,omitempty"`
}

// AlertmanagerStatus is the most recent observed status of the Alertmanager cluster. Read-only. Not
// included when requesting from the apiserver, only from the Prometheus
// Operator API itself. More info:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +k8s:openapi-gen=true
type AlertmanagerStatus struct {
	// Represents whether any actions on the underlying managed objects are
	// being performed. Only delete actions will be performed.
	Paused bool `json:"paused"`
	// Total number of non-terminated pods targeted by this Alertmanager
	// cluster (their labels match the selector).
	Replicas int32 `json:"replicas"`
	// Total number of non-terminated pods targeted by this Alertmanager
	// cluster that have the desired version spec.
	UpdatedReplicas int32 `json:"updatedReplicas"`
	// Total number of available pods (ready for at least minReadySeconds)
	// targeted by this Alertmanager cluster.
	AvailableReplicas int32 `json:"availableReplicas"`
	// Total number of unavailable pods targeted by this Alertmanager cluster.
	UnavailableReplicas int32 `json:"unavailableReplicas"`
}

// NamespaceSelector is a selector for selecting either all namespaces or a
// list of namespaces.
// +k8s:openapi-gen=true
type NamespaceSelector struct {
	// Boolean describing whether all namespaces are selected in contrast to a
	// list restricting them.
	Any bool `json:"any,omitempty"`
	// List of namespace names.
	MatchNames []string `json:"matchNames,omitempty"`

	// TODO(fabxc): this should embed metav1.LabelSelector eventually.
	// Currently the selector is only used for namespaces which require more complex
	// implementation to support label selections.
}

// /--rules.*/ command-line arguments
// +k8s:openapi-gen=true
type Rules struct {
	Alert RulesAlert `json:"alert,omitempty"`
}

// /--rules.alert.*/ command-line arguments
// +k8s:openapi-gen=true
type RulesAlert struct {
	// Max time to tolerate prometheus outage for restoring 'for' state of alert.
	ForOutageTolerance string `json:"forOutageTolerance,omitempty"`
	// Minimum duration between alert and restored 'for' state.
	// This is maintained only for alerts with configured 'for' time greater than grace period.
	ForGracePeriod string `json:"forGracePeriod,omitempty"`
	// Minimum amount of time to wait before resending an alert to Alertmanager.
	ResendDelay string `json:"resendDelay,omitempty"`
}

// DeepCopyObject implements the runtime.Object interface.
func (l *Alertmanager) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *AlertmanagerList) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *Prometheus) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *PrometheusList) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *ServiceMonitor) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *ServiceMonitorList) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *PodMonitor) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *PodMonitorList) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *Probe) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *ProbeList) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (f *PrometheusRule) DeepCopyObject() runtime.Object {
	return f.DeepCopy()
}

// DeepCopyObject implements the runtime.Object interface.
func (l *PrometheusRuleList) DeepCopyObject() runtime.Object {
	return l.DeepCopy()
}

// ProbeTLSConfig specifies TLS configuration parameters.
// +k8s:openapi-gen=true
type ProbeTLSConfig struct {
	SafeTLSConfig `json:",inline"`
}
