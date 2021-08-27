/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ManagementStateType defines the type for CR management states.
//
// +kubebuilder:validation:Enum=Managed;Unmanaged
type ManagementStateType string

const (
	// ManagementStateManaged when the LokiStack custom resource should be
	// reconciled by the operator.
	ManagementStateManaged ManagementStateType = "Managed"

	// ManagementStateUnmanaged when the LokiStack custom resource should not be
	// reconciled by the operator.
	ManagementStateUnmanaged ManagementStateType = "Unmanaged"
)

// LokiStackSizeType declares the type for loki cluster scale outs.
//
// +kubebuilder:validation:Enum="1x.extra-small";"1x.small";"1x.medium"
type LokiStackSizeType string

const (
	// SizeOneXExtraSmall defines the size of a single Loki deployment
	// with extra small resources/limits requirements and without HA support.
	// This size is ultimately dedicated for development and demo purposes.
	// DO NOT USE THIS IN PRODUCTION!
	//
	// FIXME: Add clear description of ingestion/query performance expectations.
	SizeOneXExtraSmall LokiStackSizeType = "1x.extra-small"

	// SizeOneXSmall defines the size of a single Loki deployment
	// with small resources/limits requirements and HA support for all
	// Loki components. This size is dedicated for setup **without** the
	// requirement for single replication factor and auto-compaction.
	//
	// FIXME: Add clear description of ingestion/query performance expectations.
	SizeOneXSmall LokiStackSizeType = "1x.small"

	// SizeOneXMedium defines the size of a single Loki deployment
	// with small resources/limits requirements and HA support for all
	// Loki components. This size is dedicated for setup **with** the
	// requirement for single replication factor and auto-compaction.
	//
	// FIXME: Add clear description of ingestion/query performance expectations.
	SizeOneXMedium LokiStackSizeType = "1x.medium"
)

// LokiComponentSpec defines the requirements to configure scheduling
// of each loki component individually.
type LokiComponentSpec struct {
	// Replicas defines the number of replica pods of the component.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	Replicas int32 `json:"replicas,omitempty"`

	// NodeSelector defines the labels required by a node to schedule
	// the component onto it.
	//
	// +optional
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations defines the tolerations required by a node to schedule
	// the component onto it.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// LokiTemplateSpec defines the template of all requirements to configure
// scheduling of all Loki components to be deployed.
type LokiTemplateSpec struct {

	// Compactor defines the compaction component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Compactor pods"
	Compactor *LokiComponentSpec `json:"compactor,omitempty"`

	// Distributor defines the distributor component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Distributor pods"
	Distributor *LokiComponentSpec `json:"distributor,omitempty"`

	// Ingester defines the ingester component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ingester pods"
	Ingester *LokiComponentSpec `json:"ingester,omitempty"`

	// Querier defines the querier component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Querier pods"
	Querier *LokiComponentSpec `json:"querier,omitempty"`

	// QueryFrontend defines the query frontend component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Query Frontend pods"
	QueryFrontend *LokiComponentSpec `json:"queryFrontend,omitempty"`

	// Gateway defines the lokistack-gateway component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Gateway pods"
	Gateway *LokiComponentSpec `json:"gateway,omitempty"`
}

// ObjectStorageSecretSpec is a secret reference containing name only, no namespace.
type ObjectStorageSecretSpec struct {
	// Name of a secret in the namespace configured for object storage secrets.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret",displayName="Object Storage Secret"
	Name string `json:"name"`
}

// ObjectStorageSpec defines the requirements to access the object
// storage bucket to persist logs by the ingester component.
type ObjectStorageSpec struct {
	// Secret for object storage authentication.
	// Name of a secret in the same namespace as the cluster logging operator.
	//
	// +required
	// +kubebuilder:validation:Required
	Secret ObjectStorageSecretSpec `json:"secret"`
}

// QueryLimitSpec defines the limits applies at the query path.
type QueryLimitSpec struct {

	// MaxEntriesLimitsPerQuery defines the maximum number of log entries
	// that will be returned for a query.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Entries Limit per Query"
	MaxEntriesLimitPerQuery int32 `json:"maxEntriesLimitPerQuery,omitempty"`

	// MaxChunksPerQuery defines the maximum number of chunks
	// that can be fetched by a single query.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Chunk per Query"
	MaxChunksPerQuery int32 `json:"maxChunksPerQuery,omitempty"`

	// MaxQuerySeries defines the the maximum of unique series
	// that is returned by a metric query.
	//
	// + optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Query Series"
	MaxQuerySeries int32 `json:"maxQuerySeries,omitempty"`
}

// IngestionLimitSpec defines the limits applied at the ingestion path.
type IngestionLimitSpec struct {

	// IngestionRate defines the sample size per second. Units MB.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Ingestion Rate (in MB)"
	IngestionRate int32 `json:"ingestionRate,omitempty"`

	// IngestionBurstSize defines the local rate-limited sample size per
	// distributor replica. It should be set to the set at least to the
	// maximum logs size expected in a single push request.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Ingestion Burst Size (in MB)"
	IngestionBurstSize int32 `json:"ingestionBurstSize,omitempty"`

	// MaxLabelNameLength defines the maximum number of characters allowed
	// for label keys in log streams.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Label Name Length"
	MaxLabelNameLength int32 `json:"maxLabelNameLength,omitempty"`

	// MaxLabelValueLength defines the maximum number of characters allowed
	// for label values in log streams.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Label Value Length"
	MaxLabelValueLength int32 `json:"maxLabelValueLength,omitempty"`

	// MaxLabelNamesPerSeries defines the maximum number of label names per series
	// in each log stream.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Labels Names per Series"
	MaxLabelNamesPerSeries int32 `json:"maxLabelNamesPerSeries,omitempty"`

	// MaxGlobalStreamsPerTenant defines the maximum number of active streams
	// per tenant, across the cluster.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Global Streams per  Tenant"
	MaxGlobalStreamsPerTenant int32 `json:"maxGlobalStreamsPerTenant,omitempty"`

	// MaxLineSize defines the aximum line size on ingestion path. Units in Bytes.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Line Size"
	MaxLineSize int32 `json:"maxLineSize,omitempty"`
}

// LimitsTemplateSpec defines the limits  applied at ingestion or query path.
type LimitsTemplateSpec struct {
	// IngestionLimits defines the limits applied on ingested log streams.
	//
	// +optional
	// +kubebuilder:validation:Optional
	IngestionLimits *IngestionLimitSpec `json:"ingestion,omitempty"`

	// QueryLimits defines the limit applied on querying log streams.
	//
	// +optional
	// +kubebuilder:validation:Optional
	QueryLimits *QueryLimitSpec `json:"queries,omitempty"`
}

// LimitsSpec defines the spec for limits applied at ingestion or query
// path across the cluster or per tenant.
type LimitsSpec struct {

	// Global defines the limits applied globally across the cluster.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Global Limits"
	Global *LimitsTemplateSpec `json:"global,omitempty"`

	// Tenants defines the limits applied per tenant.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Limits per Tenant"
	Tenants map[string]LimitsTemplateSpec `json:"tenants,omitempty"`
}

// LokiStackSpec defines the desired state of LokiStack
type LokiStackSpec struct {

	// ManagementState defines if the CR should be managed by the operator or not.
	// Default is managed.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:default:=Managed
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Managed","urn:alm:descriptor:com.tectonic.ui:select:Unmanaged"},displayName="Management State"
	ManagementState ManagementStateType `json:"managementState,omitempty"`

	// Size defines one of the support Loki deployment scale out sizes.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:1x.extra-small","urn:alm:descriptor:com.tectonic.ui:select:1x.small","urn:alm:descriptor:com.tectonic.ui:select:1x.medium"},displayName="LokiStack Size"
	Size LokiStackSizeType `json:"size"`

	// Storage defines the spec for the object storage endpoint to store logs.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Object Storage"
	Storage ObjectStorageSpec `json:"storage"`

	// Storage class name defines the storage class for ingester/querier PVCs.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:StorageClass",displayName="Storage Class Name"
	StorageClassName string `json:"storageClassName"`

	// ReplicationFactor defines the policy for log stream replication.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Replication Factor"
	ReplicationFactor int32 `json:"replicationFactor"`

	// Limits defines the limits to be applied to log stream processing.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Rate Limiting"
	Limits *LimitsSpec `json:"limits,omitempty"`

	// Template defines the resource/limits/tolerations/nodeselectors per component
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Node Placement"
	Template *LokiTemplateSpec `json:"template,omitempty"`
}

// LokiStackConditionType deifnes the type of condition types of a Loki deployment.
type LokiStackConditionType string

const (
	// ConditionReady defines the condition that all components in the Loki deployment are ready.
	ConditionReady LokiStackConditionType = "Ready"

	// ConditionPending defines the conditioin that some or all components are in pending state.
	ConditionPending LokiStackConditionType = "Pending"

	// ConditionFailed defines the condition that components in the Loki deployment failed to roll out.
	ConditionFailed LokiStackConditionType = "Failed"

	// ConditionDegraded defines the condition that some or all components in the Loki deployment
	// are degraded or the cluster cannot connect to object storage.
	ConditionDegraded LokiStackConditionType = "Degraded"
)

// LokiStackConditionReason defines the type for valid reasons of a Loki deployment conditions.
type LokiStackConditionReason string

const (
	// ReasonFailedComponents when all/some LokiStack components fail to roll out.
	ReasonFailedComponents LokiStackConditionReason = "FailedComponents"
	// ReasonPendingComponents when all/some LokiStack components pending dependencies
	ReasonPendingComponents LokiStackConditionReason = "PendingComponents"
	// ReasonReadyComponents when all LokiStack components are ready to serve traffic.
	ReasonReadyComponents LokiStackConditionReason = "ReadyComponents"
	// ReasonMissingObjectStorageSecret when the required secret to store logs to object
	// storage is missing.
	ReasonMissingObjectStorageSecret LokiStackConditionReason = "MissingObjectStorageSecret"
	// ReasonInvalidObjectStorageSecret when the format of the secret is invalid.
	ReasonInvalidObjectStorageSecret LokiStackConditionReason = "InvalidObjectStorageSecret"
	// ReasonInvalidReplicationConfiguration when the configurated replication factor is not valid
	// with the select cluster size.
	ReasonInvalidReplicationConfiguration LokiStackConditionReason = "InvalidReplicationConfiguration"
)

// PodStatusMap defines the type for mapping pod status to pod name.
type PodStatusMap map[corev1.PodPhase][]string

// LokiStackComponentStatus defines the map of per pod status per LokiStack component.
// Each component is represented by a separate map of v1.Phase to a list of pods.
type LokiStackComponentStatus struct {
	// Compactor is a map to the pod status of the compactor pod.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Compactor",order=5
	Compactor PodStatusMap `json:"compactor,omitempty"`

	// Distributor is a map to the per pod status of the distributor deployment
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Distributor",order=1
	Distributor PodStatusMap `json:"distributor,omitempty"`

	// Ingester is a map to the per pod status of the ingester statefulset
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Ingester",order=2
	Ingester PodStatusMap `json:"ingester,omitempty"`

	// Querier is a map to the per pod status of the querier statefulset
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Querier",order=3
	Querier PodStatusMap `json:"querier,omitempty"`

	// QueryFrontend is a mpa to the per pod status of the query frontend deployment.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Query Frontend",order=4
	QueryFrontend PodStatusMap `json:"queryFrontend,omitempty"`
}

// LokiStackStatus defines the observed state of LokiStack
type LokiStackStatus struct {
	// Components provides summary of all Loki pod status grouped
	// per component.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Components LokiStackComponentStatus `json:"components,omitempty"`

	// Conditions of the Loki deployment health.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=logging

// LokiStack is the Schema for the lokistacks API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="LokiStack",resources={{Deployment,v1},{StatefulSet,v1},{ConfigMap,v1},{Service,v1},{PersistentVolumeClaims,v1},{ServiceMonitor,v1}}
type LokiStack struct {
	Spec              LokiStackSpec   `json:"spec,omitempty"`
	Status            LokiStackStatus `json:"status,omitempty"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	metav1.TypeMeta   `json:",inline"`
}

// +kubebuilder:object:root=true

// LokiStackList contains a list of LokiStack
type LokiStackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LokiStack `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LokiStack{}, &LokiStackList{})
}
