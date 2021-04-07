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

// LokiStackSizeType declares the type for loki cluster scale outs.
//
// +kubebuilder:validation:Enum=OneXExtraSmallSize;OneXSmall;OneXMedium
type LokiStackSizeType string

const (
	// OneXExtraSmallSize defines the size of a single Loki deployment
	// with extra small resources/limits requirements and without HA support.
	// This size is ultimately dedicated for development and demo purposes.
	// DO NOT USE THIS IN PRODUCTION!
	//
	// FIXME: Add clear description of ingestion/query performance expectations.
	OneXExtraSmallSize LokiStackSizeType = "1x.extra-small"

	// OneXSmall defines the size of a single Loki deployment
	// with small resources/limits requirements and HA support for all
	// Loki components. This size is dedicated for setup **without** the
	// requirement for single replication factor and auto-compaction.
	//
	// FIXME: Add clear description of ingestion/query performance expectations.
	OneXSmall LokiStackSizeType = "1x.small"

	// OneXMedium defines the size of a single Loki deployment
	// with small resources/limits requirements and HA support for all
	// Loki components. This size is dedicated for setup **with** the
	// requirement for single replication factor and auto-compaction.
	//
	// FIXME: Add clear description of ingestion/query performance expectations.
	OneXMedium LokiStackSizeType = "1x.medium"
)

// LokiComponentSpec defines the requirements to configure scheduling
// of each loki component individually.
type LokiComponentSpec struct {
	// Replicas defines the number of replica pods of the component.
	//
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// NodeSelector defines the labels required by a node to schedule
	// the component onto it.
	//
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations defines the tolerations required by a node to schedule
	// the component onto it.
	//
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// LokiTemplateSpec defines the template of all requirements to configure
// scheduling of all Loki components to be deployed.
type LokiTemplateSpec struct {

	// Compactor defines the compaction component spec.
	//
	// +optional
	Compactor *LokiComponentSpec `json:"compactor,omitempty"`

	// Distributor defines the distributor component spec.
	//
	// +optional
	Distributor *LokiComponentSpec `json:"distributor,omitempty"`

	// Ingester defines the ingester component spec.
	//
	// +optional
	Ingester *LokiComponentSpec `json:"ingester,omitempty"`

	// Querier defines the querier component spec.
	//
	// +optional
	Querier *LokiComponentSpec `json:"querier,omitempty"`

	// QueryFrontend defines the query frontend component spec.
	//
	// +optional
	QueryFrontend *LokiComponentSpec `json:"queryFrontend,omitempty"`
}

// ObjectStorageSecretSpec is a secret reference containing name only, no namespace.
type ObjectStorageSecretSpec struct {
	// Name of a secret in the namespace configured for object storage secrets.
	//
	// +required
	Name string `json:"name"`
}

// ObjectStorageSpec defines the requirements to access the object
// storage bucket to persist logs by the ingester component.
type ObjectStorageSpec struct {

	// URL of the object storage to store logs.
	URL string `json:"url,omitempty"`

	// Secret for object storage authentication.
	// Name of a secret in the same namespace as the cluster logging operator.
	//
	// +required
	Secret *ObjectStorageSecretSpec `json:"secret,omitempty"`
}

// QueryLimitSpec defines the limits applies at the query path.
type QueryLimitSpec struct {

	// MaxEntriesPerQuery defines the aximum number of log entries
	// that will be returned for a query.
	//
	// +optional
	MaxEntriesPerQuery int32 `json:"maxEntriesPerQuery,omitempty"`

	// MaxChunksPerQuery defines the maximum number of chunks
	// that can be fetched by a single query.
	//
	// +optional
	MaxChunksPerQuery int32 `json:"maxChunksPerQuery,omitempty"`

	// MaxQuerySeries defines the the maximum of unique series
	// that is returned by a metric query.
	//
	// + optional
	MaxQuerySeries int32 `json:"maxQuerySeries,omitempty"`
}

// IngestionLimitSpec defines the limits applied at the ingestion path.
type IngestionLimitSpec struct {

	// IngestionRate defines the sample size per second. Units MB.
	//
	// +optional
	IngestionRate int32 `json:"ingestionRate,omitempty"`

	// IngestionBurstSize defines the local rate-limited sample size per
	// distributor replica. It should be set to the set at least to the
	// maximum logs size expected in a single push request.
	//
	// +optional
	IngestionBurstSize int32 `json:"ingestionBurstSize,omitempty"`

	// MaxLabelLength defines the maximum number of characters allowed
	// for label keys in log streams.
	//
	// +optional
	MaxLabelLength int32 `json:"maxLabelLength,omitempty"`

	// MaxLabelValueLength defines the maximum number of characters allowed
	// for label values in log streams.
	//
	// +optional
	MaxLabelValueLength int32 `json:"maxLabelValueLength,omitempty"`

	// MaxLabelsPerSeries defines the maximum number of labels per series
	// in each log stream.
	//
	// +optional
	MaxLabelsPerSeries int32 `json:"maxLabelsPerSeries,omitempty"`

	// MaxStreamsPerUser defines the maximum number of active streams
	// per user, per ingester.
	//
	// +optional
	MaxStreamsPerUser int32 `json:"maxStreamsPerUser,omitempty"`

	// MaxGlobalStreamsPerUser defines the maximum number of active streams
	// per user, across the cluster.
	//
	// +optional
	MaxGlobalStreamsPerUser int32 `json:"maxGlobalStreamsPerUser,omitempty"`

	// MaxLineSize defines the aximum line size on ingestion path. Units in Bytes.
	//
	// +optional
	MaxLineSize int32 `json:"maxLineSize,omitempty"`
}

// LimitsTemplateSpec defines the limits  applied at ingestion or query path.
type LimitsTemplateSpec struct {
	// IngestionLimits defines the limits applied on ingested log streams.
	//
	// +optional
	IngestionLimits *IngestionLimitSpec `json:"ingestion,omitempty"`

	// QueryLimits defines the limit applied on querying log streams.
	//
	// +optional
	QueryLimits *QueryLimitSpec `json:"queries,omitempty"`
}

// LimitsSpec defines the spec for limits applied at ingestion or query
// path across the cluster or per tenant.
type LimitsSpec struct {

	// Global defines the limits applied globally across the cluster.
	//
	// +optional
	Global *LimitsTemplateSpec `json:"global,omitempty"`

	// Tenants defines the limits applied per tenant.
	//
	// +optional
	Tenants map[string]LimitsTemplateSpec `json:"tenants,omitempty"`
}

// LokiStackSpec defines the desired state of LokiStack
type LokiStackSpec struct {

	// Size defines one of the support Loki deployment scale out sizes.
	//
	// +required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Loki Stack Size"
	Size LokiStackSizeType `json:"size,omitempty"`

	// Storage defines the spec for the object storage endpoint to store logs.
	//
	// +required
	Storage ObjectStorageSpec `json:"storage,omitempty"`

	// Storage class name defines the storage class for ingester/querier PVCs.
	//
	// +required
	StorageClassName string `json:"storageClassName,omitempty"`

	// ReplicationFactor defines the policy for log stream replication.
	//
	// +required
	ReplicationFactor int32 `json:"replicationFactor,omitempty"`

	// Limits defines the limits to be applied to log stream processing.
	//
	// +optional
	Limits *LimitsSpec `json:"limits,omitempty"`

	// Template defines the resource/limits/tolerations/nodeselectors per component
	//
	// +optional
	Template *LokiTemplateSpec `json:"template,omitempty"`
}

// LokiStackConditionType deifnes the type of condition types of a Loki deployment.
type LokiStackConditionType string

const (
	// ConditionReady defines the condition that all components in the Loki deployment are ready.
	ConditionReady LokiStackConditionType = "Ready"

	// ConditionDegraded defines the condition that some or all components in the Loki deployment
	// are degraded or the cluster cannot connect to object storage.
	ConditionDegraded LokiStackConditionType = "Degraded"
)

// LokiStackConditionReason defines the type for valid reasons of a Loki deployment conditions.
type LokiStackConditionReason string

const (
	// ReasonMissingObjectStorageSecret when the required secret to store logs to object
	// storage is missing.
	ReasonMissingObjectStorageSecret LokiStackConditionReason = "MissingObjectStorageSecret"

	// ReasonInvalidObjectStorageSecret when the format of the secret is invalid.
	ReasonInvalidObjectStorageSecret LokiStackConditionReason = "InvalidObjectStorageSecret"

	// ReasonInvalidReplicationConfiguration when the configurated replication factor is not valid
	// with the select cluster size.
	ReasonInvalidReplicationConfiguration LokiStackConditionReason = "InvalidReplicationConfiguration"
)

// LokiStackStatus defines the observed state of LokiStack
type LokiStackStatus struct {
	// Conditions of the Loki deployment health.
	//
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=logging

// LokiStack is the Schema for the lokistacks API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="LokiStack",resources={{Deployment,v1},{StatefulSet,v1},{ConfigMap,v1},{Service,v1},{PersistentVolumeClaims,v1}}
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
