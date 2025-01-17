package v1beta1

import (
	v1 "github.com/grafana/loki/operator/api/loki/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
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

// SubjectKind is a kind of LokiStack Gateway RBAC subject.
//
// +kubebuilder:validation:Enum=user;group
type SubjectKind string

const (
	// User represents a subject that is a user.
	User SubjectKind = "user"
	// Group represents a subject that is a group.
	Group SubjectKind = "group"
)

// Subject represents a subject that has been bound to a role.
type Subject struct {
	Name string      `json:"name"`
	Kind SubjectKind `json:"kind"`
}

// RoleBindingsSpec binds a set of roles to a set of subjects.
type RoleBindingsSpec struct {
	Name     string    `json:"name"`
	Subjects []Subject `json:"subjects"`
	Roles    []string  `json:"roles"`
}

// PermissionType is a LokiStack Gateway RBAC permission.
//
// +kubebuilder:validation:Enum=read;write
type PermissionType string

const (
	// Write gives access to write data to a tenant.
	Write PermissionType = "write"
	// Read gives access to read data from a tenant.
	Read PermissionType = "read"
)

// RoleSpec describes a set of permissions to interact with a tenant.
type RoleSpec struct {
	Name        string           `json:"name"`
	Resources   []string         `json:"resources"`
	Tenants     []string         `json:"tenants"`
	Permissions []PermissionType `json:"permissions"`
}

// OPASpec defines the opa configuration spec for lokiStack Gateway component.
type OPASpec struct {
	// URL defines the third-party endpoint for authorization.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OpenPolicyAgent URL"
	URL string `json:"url"`
}

// AuthorizationSpec defines the opa, role bindings and roles
// configuration per tenant for lokiStack Gateway component.
type AuthorizationSpec struct {
	// OPA defines the spec for the third-party endpoint for tenant's authorization.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OPA Configuration"
	OPA *OPASpec `json:"opa"`
	// Roles defines a set of permissions to interact with a tenant.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Static Roles"
	Roles []RoleSpec `json:"roles"`
	// RoleBindings defines configuration to bind a set of roles to a set of subjects.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Static Role Bindings"
	RoleBindings []RoleBindingsSpec `json:"roleBindings"`
}

// TenantSecretSpec is a secret reference containing name only
// for a secret living in the same namespace as the LokiStack custom resource.
type TenantSecretSpec struct {
	// Name of a secret in the namespace configured for tenant secrets.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret",displayName="Tenant Secret Name"
	Name string `json:"name"`
}

// OIDCSpec defines the oidc configuration spec for lokiStack Gateway component.
type OIDCSpec struct {
	// Secret defines the spec for the clientID, clientSecret and issuerCAPath for tenant's authentication.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant Secret"
	Secret *TenantSecretSpec `json:"secret"`
	// IssuerURL defines the URL for issuer.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Issuer URL"
	IssuerURL string `json:"issuerURL"`
	// RedirectURL defines the URL for redirect.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Redirect URL"
	RedirectURL string `json:"redirectURL,omitempty"`
	// Group claim field from ID Token
	//
	// +optional
	// +kubebuilder:validation:Optional
	GroupClaim string `json:"groupClaim,omitempty"`
	// User claim field from ID Token
	//
	// +optional
	// +kubebuilder:validation:Optional
	UsernameClaim string `json:"usernameClaim,omitempty"`
}

// AuthenticationSpec defines the oidc configuration per tenant for lokiStack Gateway component.
type AuthenticationSpec struct {
	// TenantName defines the name of the tenant.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant Name"
	TenantName string `json:"tenantName"`
	// TenantID defines the id of the tenant.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant ID"
	TenantID string `json:"tenantId"`
	// OIDC defines the spec for the OIDC tenant's authentication.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OIDC Configuration"
	OIDC *OIDCSpec `json:"oidc"`
}

// ModeType is the authentication/authorization mode in which LokiStack Gateway will be configured.
//
// +kubebuilder:validation:Enum=static;dynamic;openshift-logging
type ModeType string

const (
	// Static mode asserts the Authorization Spec's Roles and RoleBindings
	// using an in-process OpenPolicyAgent Rego authorizer.
	Static ModeType = "static"
	// Dynamic mode delegates the authorization to a third-party OPA-compatible endpoint.
	Dynamic ModeType = "dynamic"
	// OpenshiftLogging mode provides fully automatic OpenShift in-cluster authentication and authorization support.
	OpenshiftLogging ModeType = "openshift-logging"
)

// TenantsSpec defines the mode, authentication and authorization
// configuration of the lokiStack gateway component.
type TenantsSpec struct {
	// Mode defines the mode in which lokistack-gateway component will be configured.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:default:=openshift-logging
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:static","urn:alm:descriptor:com.tectonic.ui:select:dynamic","urn:alm:descriptor:com.tectonic.ui:select:openshift-logging"},displayName="Mode"
	Mode ModeType `json:"mode"`
	// Authentication defines the lokistack-gateway component authentication configuration spec per tenant.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Authentication"
	Authentication []AuthenticationSpec `json:"authentication,omitempty"`
	// Authorization defines the lokistack-gateway component authorization configuration spec per tenant.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Authorization"
	Authorization *AuthorizationSpec `json:"authorization,omitempty"`
}

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

	// Gateway defines the lokistack gateway component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Gateway pods"
	Gateway *LokiComponentSpec `json:"gateway,omitempty"`

	// IndexGateway defines the index gateway component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Index Gateway pods"
	IndexGateway *LokiComponentSpec `json:"indexGateway,omitempty"`

	// Ruler defines the ruler component spec.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ruler pods"
	Ruler *LokiComponentSpec `json:"ruler,omitempty"`
}

// ObjectStorageTLSSpec is the TLS configuration for reaching the object storage endpoint.
type ObjectStorageTLSSpec struct {
	// CA is the name of a ConfigMap containing a CA certificate.
	// It needs to be in the same namespace as the LokiStack custom resource.
	//
	// +optional
	// +kubebuilder:validation:optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:ConfigMap",displayName="CA ConfigMap Name"
	CA string `json:"caName,omitempty"`
}

// ObjectStorageSecretType defines the type of storage which can be used with the Loki cluster.
//
// +kubebuilder:validation:Enum=azure;gcs;s3;swift
type ObjectStorageSecretType string

const (
	// ObjectStorageSecretAzure when using Azure for Loki storage
	ObjectStorageSecretAzure ObjectStorageSecretType = "azure"

	// ObjectStorageSecretGCS when using GCS for Loki storage
	ObjectStorageSecretGCS ObjectStorageSecretType = "gcs"

	// ObjectStorageSecretS3 when using S3 for Loki storage
	ObjectStorageSecretS3 ObjectStorageSecretType = "s3"

	// ObjectStorageSecretSwift when using Swift for Loki storage
	ObjectStorageSecretSwift ObjectStorageSecretType = "swift"
)

// ObjectStorageSecretSpec is a secret reference containing name only, no namespace.
type ObjectStorageSecretSpec struct {
	// Type of object storage that should be used
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:azure","urn:alm:descriptor:com.tectonic.ui:select:gcs","urn:alm:descriptor:com.tectonic.ui:select:s3","urn:alm:descriptor:com.tectonic.ui:select:swift"},displayName="Object Storage Secret Type"
	Type ObjectStorageSecretType `json:"type"`

	// Name of a secret in the namespace configured for object storage secrets.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret",displayName="Object Storage Secret Name"
	Name string `json:"name"`
}

// ObjectStorageSchemaVersion defines the storage schema version which will be
// used with the Loki cluster.
//
// +kubebuilder:validation:Enum=v11;v12
type ObjectStorageSchemaVersion string

const (
	// ObjectStorageSchemaV11 when using v11 for the storage schema
	ObjectStorageSchemaV11 ObjectStorageSchemaVersion = "v11"

	// ObjectStorageSchemaV12 when using v12 for the storage schema
	ObjectStorageSchemaV12 ObjectStorageSchemaVersion = "v12"
)

// ObjectStorageSchema defines the requirements needed to configure a new
// storage schema.
type ObjectStorageSchema struct {
	// Version for writing and reading logs.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:v11","urn:alm:descriptor:com.tectonic.ui:select:v12"},displayName="Version"
	Version ObjectStorageSchemaVersion `json:"version"`

	// EffectiveDate is the date in UTC that the schema will be applied on.
	// To ensure readibility of logs, this date should be before the current
	// date in UTC.
	//
	// +required
	// +kubebuilder:validation:Required
	EffectiveDate StorageSchemaEffectiveDate `json:"effectiveDate"`
}

// ObjectStorageSpec defines the requirements to access the object
// storage bucket to persist logs by the ingester component.
type ObjectStorageSpec struct {
	// Schemas for reading and writing logs.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MinItems:=1
	// +kubebuilder:default:={{version:v11,effectiveDate:"2020-10-11"}}
	Schemas []ObjectStorageSchema `json:"schemas"`

	// Secret for object storage authentication.
	// Name of a secret in the same namespace as the LokiStack custom resource.
	//
	// +required
	// +kubebuilder:validation:Required
	Secret ObjectStorageSecretSpec `json:"secret"`

	// TLS configuration for reaching the object storage endpoint.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TLS Config"
	TLS *ObjectStorageTLSSpec `json:"tls,omitempty"`
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

	// MaxQuerySeries defines the maximum of unique series
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

	// MaxLineSize defines the maximum line size on ingestion path. Units in Bytes.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Line Size"
	MaxLineSize int32 `json:"maxLineSize,omitempty"`
}

// LimitsTemplateSpec defines the limits and overrides applied per-tenant.
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
// It also defines the per-tenant configuration overrides.
type LimitsSpec struct {
	// Global defines the limits applied globally across the cluster.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Global Limits"
	Global *LimitsTemplateSpec `json:"global,omitempty"`

	// Tenants defines the limits and overrides applied per tenant.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Limits per Tenant"
	Tenants map[string]LimitsTemplateSpec `json:"tenants,omitempty"`
}

// RulesSpec deifnes the spec for the ruler component.
type RulesSpec struct {
	// Enabled defines a flag to enable/disable the ruler component
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enable"
	Enabled bool `json:"enabled"`

	// A selector to select which LokiRules to mount for loading alerting/recording
	// rules from.
	//
	// +optional
	// +kubebuilder:validation:optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Selector"
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Namespaces to be selected for PrometheusRules discovery. If unspecified, only
	// the same namespace as the LokiStack object is in is used.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Namespace Selector"
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
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
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:default:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Replication Factor"
	ReplicationFactor int32 `json:"replicationFactor"`

	// Rules defines the spec for the ruler component
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Rules"
	Rules *RulesSpec `json:"rules,omitempty"`

	// Limits defines the per-tenant limits to be applied to log stream processing and the per-tenant the config overrides.
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

	// Tenants defines the per-tenant authentication and authorization spec for the lokistack-gateway component.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenants Configuration"
	Tenants *TenantsSpec `json:"tenants,omitempty"`
}

// LokiStackConditionType deifnes the type of condition types of a Loki deployment.
type LokiStackConditionType string

const (
	// ConditionReady defines the condition that all components in the Loki deployment are ready.
	ConditionReady LokiStackConditionType = "Ready"

	// ConditionPending defines the condition that some or all components are in pending state.
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
	// ReasonInvalidObjectStorageSchema when the spec contains an invalid schema(s).
	ReasonInvalidObjectStorageSchema LokiStackConditionReason = "InvalidObjectStorageSchema"
	// ReasonMissingObjectStorageCAConfigMap when the required configmap to verify object storage
	// certificates is missing.
	ReasonMissingObjectStorageCAConfigMap LokiStackConditionReason = "MissingObjectStorageCAConfigMap"
	// ReasonInvalidObjectStorageCAConfigMap when the format of the CA configmap is invalid.
	ReasonInvalidObjectStorageCAConfigMap LokiStackConditionReason = "InvalidObjectStorageCAConfigMap"
	// ReasonMissingRulerSecret when the required secret to authorization remote write connections
	// for the ruler is missing.
	ReasonMissingRulerSecret LokiStackConditionReason = "MissingRulerSecret"
	// ReasonInvalidRulerSecret when the format of the ruler remote write authorization secret is invalid.
	ReasonInvalidRulerSecret LokiStackConditionReason = "InvalidRulerSecret"
	// ReasonInvalidReplicationConfiguration when the configurated replication factor is not valid
	// with the select cluster size.
	ReasonInvalidReplicationConfiguration LokiStackConditionReason = "InvalidReplicationConfiguration"
	// ReasonMissingGatewayTenantSecret when the required tenant secret
	// for authentication is missing.
	ReasonMissingGatewayTenantSecret LokiStackConditionReason = "MissingGatewayTenantSecret"
	// ReasonInvalidGatewayTenantSecret when the format of the secret is invalid.
	ReasonInvalidGatewayTenantSecret LokiStackConditionReason = "InvalidGatewayTenantSecret"
	// ReasonInvalidTenantsConfiguration when the tenant configuration provided is invalid.
	ReasonInvalidTenantsConfiguration LokiStackConditionReason = "InvalidTenantsConfiguration"
	// ReasonMissingGatewayOpenShiftBaseDomain when the reconciler cannot lookup the OpenShift DNS base domain.
	ReasonMissingGatewayOpenShiftBaseDomain LokiStackConditionReason = "MissingGatewayOpenShiftBaseDomain"
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

	// IndexGateway is a map to the per pod status of the index gateway statefulset
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="IndexGateway",order=6
	IndexGateway PodStatusMap `json:"indexGateway,omitempty"`

	// Ingester is a map to the per pod status of the ingester statefulset
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Ingester",order=2
	Ingester PodStatusMap `json:"ingester,omitempty"`

	// Querier is a map to the per pod status of the querier deployment
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Querier",order=3
	Querier PodStatusMap `json:"querier,omitempty"`

	// QueryFrontend is a map to the per pod status of the query frontend deployment
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Query Frontend",order=4
	QueryFrontend PodStatusMap `json:"queryFrontend,omitempty"`

	// Gateway is a map to the per pod status of the lokistack gateway deployment.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Gateway",order=5
	Gateway PodStatusMap `json:"gateway,omitempty"`

	// Ruler is a map to the per pod status of the lokistack ruler statefulset.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses",displayName="Ruler",order=6
	Ruler PodStatusMap `json:"ruler,omitempty"`
}

// LokiStackStorageStatus defines the observed state of
// the Loki storage configuration.
type LokiStackStorageStatus struct {
	// Schemas is a list of schemas which have been applied
	// to the LokiStack.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Schemas []ObjectStorageSchema `json:"schemas,omitempty"`
}

// LokiStackStatus defines the observed state of LokiStack
type LokiStackStatus struct {
	// Components provides summary of all Loki pod status grouped
	// per component.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Components LokiStackComponentStatus `json:"components,omitempty"`

	// Storage provides summary of all changes that have occurred
	// to the storage configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Storage LokiStackStorageStatus `json:"storage,omitempty"`

	// Conditions of the Loki deployment health.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:unservedversion
// +kubebuilder:resource:categories=logging

// LokiStack is the Schema for the lokistacks API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="LokiStack",resources={{Deployment,v1},{StatefulSet,v1},{ConfigMap,v1},{Ingress,v1},{Service,v1},{ServiceAccount,v1},{PersistentVolumeClaims,v1},{Route,v1},{ServiceMonitor,v1}}
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

func convertStatusV1(src PodStatusMap) v1.PodStatusMap {
	if src == nil {
		return nil
	}

	dst := v1.PodStatusMap{}
	for k, v := range src {
		dst[v1.PodStatus(k)] = v
	}
	return dst
}

func convertStatusBeta(src v1.PodStatusMap) PodStatusMap {
	if src == nil {
		return nil
	}

	dst := PodStatusMap{}
	for k, v := range src {
		dst[corev1.PodPhase(k)] = v
	}
	return dst
}

// ConvertTo converts this LokiStack (v1beta1) to the Hub version (v1).
func (src *LokiStack) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1.LokiStack)

	dst.ObjectMeta = src.ObjectMeta
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.Components = v1.LokiStackComponentStatus{
		Compactor:     convertStatusV1(src.Status.Components.Compactor),
		Distributor:   convertStatusV1(src.Status.Components.Distributor),
		Ingester:      convertStatusV1(src.Status.Components.Ingester),
		Querier:       convertStatusV1(src.Status.Components.Querier),
		QueryFrontend: convertStatusV1(src.Status.Components.QueryFrontend),
		IndexGateway:  convertStatusV1(src.Status.Components.IndexGateway),
		Ruler:         convertStatusV1(src.Status.Components.Ruler),
		Gateway:       convertStatusV1(src.Status.Components.Gateway),
	}

	var statusSchemas []v1.ObjectStorageSchema
	for _, s := range src.Status.Storage.Schemas {
		statusSchemas = append(statusSchemas, v1.ObjectStorageSchema{
			Version:       v1.ObjectStorageSchemaVersion(s.Version),
			EffectiveDate: v1.StorageSchemaEffectiveDate(s.EffectiveDate),
		})
	}
	dst.Status.Storage = v1.LokiStackStorageStatus{Schemas: statusSchemas}

	if src.Spec.ManagementState != "" {
		dst.Spec.ManagementState = v1.ManagementStateType(src.Spec.ManagementState)
	}

	if src.Spec.Size != "" {
		dst.Spec.Size = v1.LokiStackSizeType(src.Spec.Size)
	}

	var storageTLS *v1.ObjectStorageTLSSpec
	if src.Spec.Storage.TLS != nil {
		storageTLS = &v1.ObjectStorageTLSSpec{
			CASpec: v1.CASpec{
				CA: src.Spec.Storage.TLS.CA,
			},
		}
	}

	var schemas []v1.ObjectStorageSchema
	for _, s := range src.Spec.Storage.Schemas {
		schemas = append(schemas, v1.ObjectStorageSchema{
			EffectiveDate: v1.StorageSchemaEffectiveDate(s.EffectiveDate),
			Version:       v1.ObjectStorageSchemaVersion(s.Version),
		})
	}

	dst.Spec.Storage = v1.ObjectStorageSpec{
		Schemas: schemas,
		Secret: v1.ObjectStorageSecretSpec{
			Type: v1.ObjectStorageSecretType(src.Spec.Storage.Secret.Type),
			Name: src.Spec.Storage.Secret.Name,
		},
		TLS: storageTLS,
	}

	if src.Spec.StorageClassName != "" {
		dst.Spec.StorageClassName = src.Spec.StorageClassName
	}

	if src.Spec.ReplicationFactor != 0 {
		dst.Spec.ReplicationFactor = src.Spec.ReplicationFactor
	}

	if src.Spec.Rules != nil {
		dst.Spec.Rules = &v1.RulesSpec{
			Enabled:           src.Spec.Rules.Enabled,
			Selector:          src.Spec.Rules.Selector,
			NamespaceSelector: src.Spec.Rules.NamespaceSelector,
		}
	}

	if src.Spec.Limits != nil {
		dst.Spec.Limits = &v1.LimitsSpec{}

		if src.Spec.Limits.Global != nil {
			dst.Spec.Limits.Global = &v1.LimitsTemplateSpec{}

			if src.Spec.Limits.Global.IngestionLimits != nil {
				dst.Spec.Limits.Global.IngestionLimits = &v1.IngestionLimitSpec{
					IngestionRate:             src.Spec.Limits.Global.IngestionLimits.IngestionRate,
					IngestionBurstSize:        src.Spec.Limits.Global.IngestionLimits.IngestionBurstSize,
					MaxLabelNameLength:        src.Spec.Limits.Global.IngestionLimits.MaxLabelNameLength,
					MaxLabelValueLength:       src.Spec.Limits.Global.IngestionLimits.MaxLabelValueLength,
					MaxLabelNamesPerSeries:    src.Spec.Limits.Global.IngestionLimits.MaxLabelNamesPerSeries,
					MaxGlobalStreamsPerTenant: src.Spec.Limits.Global.IngestionLimits.MaxGlobalStreamsPerTenant,
					MaxLineSize:               src.Spec.Limits.Global.IngestionLimits.MaxLineSize,
				}
			}

			if src.Spec.Limits.Global.QueryLimits != nil {
				dst.Spec.Limits.Global.QueryLimits = &v1.QueryLimitSpec{
					MaxEntriesLimitPerQuery: src.Spec.Limits.Global.QueryLimits.MaxEntriesLimitPerQuery,
					MaxChunksPerQuery:       src.Spec.Limits.Global.QueryLimits.MaxChunksPerQuery,
					MaxQuerySeries:          src.Spec.Limits.Global.QueryLimits.MaxQuerySeries,
				}
			}
		}

		if len(src.Spec.Limits.Tenants) > 0 {
			dst.Spec.Limits.Tenants = make(map[string]v1.PerTenantLimitsTemplateSpec)
		}

		for tenant, srcSpec := range src.Spec.Limits.Tenants {
			dstSpec := v1.PerTenantLimitsTemplateSpec{}

			if srcSpec.IngestionLimits != nil {
				dstSpec.IngestionLimits = &v1.IngestionLimitSpec{
					IngestionRate:             srcSpec.IngestionLimits.IngestionRate,
					IngestionBurstSize:        srcSpec.IngestionLimits.IngestionBurstSize,
					MaxLabelNameLength:        srcSpec.IngestionLimits.MaxLabelNameLength,
					MaxLabelValueLength:       srcSpec.IngestionLimits.MaxLabelValueLength,
					MaxLabelNamesPerSeries:    srcSpec.IngestionLimits.MaxLabelNamesPerSeries,
					MaxGlobalStreamsPerTenant: srcSpec.IngestionLimits.MaxGlobalStreamsPerTenant,
					MaxLineSize:               srcSpec.IngestionLimits.MaxLineSize,
				}
			}

			if srcSpec.QueryLimits != nil {
				dstSpec.QueryLimits = &v1.PerTenantQueryLimitSpec{
					QueryLimitSpec: v1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: srcSpec.QueryLimits.MaxEntriesLimitPerQuery,
						MaxChunksPerQuery:       srcSpec.QueryLimits.MaxChunksPerQuery,
						MaxQuerySeries:          srcSpec.QueryLimits.MaxQuerySeries,
					},
				}
			}

			dst.Spec.Limits.Tenants[tenant] = dstSpec
		}
	}

	if src.Spec.Template != nil {
		dst.Spec.Template = &v1.LokiTemplateSpec{}
		if src.Spec.Template.Compactor != nil {
			dst.Spec.Template.Compactor = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.Compactor.Replicas,
				NodeSelector: src.Spec.Template.Compactor.NodeSelector,
				Tolerations:  src.Spec.Template.Compactor.Tolerations,
			}
		}
		if src.Spec.Template.Distributor != nil {
			dst.Spec.Template.Distributor = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.Distributor.Replicas,
				NodeSelector: src.Spec.Template.Distributor.NodeSelector,
				Tolerations:  src.Spec.Template.Distributor.Tolerations,
			}
		}
		if src.Spec.Template.Ingester != nil {
			dst.Spec.Template.Ingester = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.Ingester.Replicas,
				NodeSelector: src.Spec.Template.Ingester.NodeSelector,
				Tolerations:  src.Spec.Template.Ingester.Tolerations,
			}
		}
		if src.Spec.Template.Querier != nil {
			dst.Spec.Template.Querier = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.Querier.Replicas,
				NodeSelector: src.Spec.Template.Querier.NodeSelector,
				Tolerations:  src.Spec.Template.Querier.Tolerations,
			}
		}
		if src.Spec.Template.QueryFrontend != nil {
			dst.Spec.Template.QueryFrontend = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.QueryFrontend.Replicas,
				NodeSelector: src.Spec.Template.QueryFrontend.NodeSelector,
				Tolerations:  src.Spec.Template.QueryFrontend.Tolerations,
			}
		}
		if src.Spec.Template.Gateway != nil {
			dst.Spec.Template.Gateway = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.Gateway.Replicas,
				NodeSelector: src.Spec.Template.Gateway.NodeSelector,
				Tolerations:  src.Spec.Template.Gateway.Tolerations,
			}
		}
		if src.Spec.Template.IndexGateway != nil {
			dst.Spec.Template.IndexGateway = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.IndexGateway.Replicas,
				NodeSelector: src.Spec.Template.IndexGateway.NodeSelector,
				Tolerations:  src.Spec.Template.IndexGateway.Tolerations,
			}
		}
		if src.Spec.Template.Ruler != nil {
			dst.Spec.Template.Ruler = &v1.LokiComponentSpec{
				Replicas:     src.Spec.Template.Ruler.Replicas,
				NodeSelector: src.Spec.Template.Ruler.NodeSelector,
				Tolerations:  src.Spec.Template.Ruler.Tolerations,
			}
		}
	}

	if src.Spec.Tenants != nil {
		dst.Spec.Tenants = &v1.TenantsSpec{
			Mode: v1.ModeType(src.Spec.Tenants.Mode),
		}

		for _, srcAuth := range src.Spec.Tenants.Authentication {
			dstAuth := v1.AuthenticationSpec{
				TenantName: srcAuth.TenantName,
				TenantID:   srcAuth.TenantID,
			}

			if srcAuth.OIDC != nil {
				dstAuth.OIDC = &v1.OIDCSpec{
					Secret: &v1.TenantSecretSpec{
						Name: srcAuth.OIDC.Secret.Name,
					},
					IssuerURL:     srcAuth.OIDC.IssuerURL,
					RedirectURL:   srcAuth.OIDC.RedirectURL,
					GroupClaim:    srcAuth.OIDC.GroupClaim,
					UsernameClaim: srcAuth.OIDC.UsernameClaim,
				}
			}

			dst.Spec.Tenants.Authentication = append(dst.Spec.Tenants.Authentication, dstAuth)
		}

		if src.Spec.Tenants.Authorization != nil {
			dstAuthz := &v1.AuthorizationSpec{}

			if src.Spec.Tenants.Authorization.OPA != nil {
				dstAuthz.OPA = &v1.OPASpec{
					URL: src.Spec.Tenants.Authorization.OPA.URL,
				}
			}

			for _, srcRole := range src.Spec.Tenants.Authorization.Roles {
				dstRole := v1.RoleSpec{
					Name:        srcRole.Name,
					Resources:   srcRole.Resources,
					Tenants:     srcRole.Tenants,
					Permissions: []v1.PermissionType{},
				}

				for _, perm := range srcRole.Permissions {
					dstRole.Permissions = append(dstRole.Permissions, v1.PermissionType(perm))
				}

				dstAuthz.Roles = append(dstAuthz.Roles, dstRole)
			}

			for _, srcBinding := range src.Spec.Tenants.Authorization.RoleBindings {
				dstBinding := v1.RoleBindingsSpec{
					Name:  srcBinding.Name,
					Roles: srcBinding.Roles,
				}

				for _, srcSubject := range srcBinding.Subjects {
					dstBinding.Subjects = append(dstBinding.Subjects, v1.Subject{
						Name: srcSubject.Name,
						Kind: v1.SubjectKind(srcSubject.Kind),
					})
				}

				dstAuthz.RoleBindings = append(dstAuthz.RoleBindings, dstBinding)
			}

			dst.Spec.Tenants.Authorization = dstAuthz
		}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version (v1beta1).
// nolint:golint
func (dst *LokiStack) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.LokiStack)

	dst.ObjectMeta = src.ObjectMeta
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.Components = LokiStackComponentStatus{
		Compactor:     convertStatusBeta(src.Status.Components.Compactor),
		Distributor:   convertStatusBeta(src.Status.Components.Distributor),
		Ingester:      convertStatusBeta(src.Status.Components.Ingester),
		Querier:       convertStatusBeta(src.Status.Components.Querier),
		QueryFrontend: convertStatusBeta(src.Status.Components.QueryFrontend),
		IndexGateway:  convertStatusBeta(src.Status.Components.IndexGateway),
		Ruler:         convertStatusBeta(src.Status.Components.Ruler),
		Gateway:       convertStatusBeta(src.Status.Components.Gateway),
	}

	var statusSchemas []ObjectStorageSchema
	for _, s := range src.Status.Storage.Schemas {
		statusSchemas = append(statusSchemas, ObjectStorageSchema{
			Version:       ObjectStorageSchemaVersion(s.Version),
			EffectiveDate: StorageSchemaEffectiveDate(s.EffectiveDate),
		})
	}
	dst.Status.Storage = LokiStackStorageStatus{Schemas: statusSchemas}

	if src.Spec.ManagementState != "" {
		dst.Spec.ManagementState = ManagementStateType(src.Spec.ManagementState)
	}

	if src.Spec.Size != "" {
		dst.Spec.Size = LokiStackSizeType(src.Spec.Size)
	}

	var storageTLS *ObjectStorageTLSSpec
	if src.Spec.Storage.TLS != nil {
		storageTLS = &ObjectStorageTLSSpec{
			CA: src.Spec.Storage.TLS.CA,
		}
	}

	var schemas []ObjectStorageSchema
	for _, s := range src.Spec.Storage.Schemas {
		schemas = append(schemas, ObjectStorageSchema{
			EffectiveDate: StorageSchemaEffectiveDate(s.EffectiveDate),
			Version:       ObjectStorageSchemaVersion(s.Version),
		})
	}

	dst.Spec.Storage = ObjectStorageSpec{
		Schemas: schemas,
		Secret: ObjectStorageSecretSpec{
			Type: ObjectStorageSecretType(src.Spec.Storage.Secret.Type),
			Name: src.Spec.Storage.Secret.Name,
		},
		TLS: storageTLS,
	}

	if src.Spec.StorageClassName != "" {
		dst.Spec.StorageClassName = src.Spec.StorageClassName
	}

	if src.Spec.ReplicationFactor != 0 {
		dst.Spec.ReplicationFactor = src.Spec.ReplicationFactor
	}

	if src.Spec.Rules != nil {
		dst.Spec.Rules = &RulesSpec{
			Enabled:           src.Spec.Rules.Enabled,
			Selector:          src.Spec.Rules.Selector,
			NamespaceSelector: src.Spec.Rules.NamespaceSelector,
		}
	}

	if src.Spec.Limits != nil {
		dst.Spec.Limits = &LimitsSpec{}

		if src.Spec.Limits.Global != nil {
			dst.Spec.Limits.Global = &LimitsTemplateSpec{}

			if src.Spec.Limits.Global.IngestionLimits != nil {
				dst.Spec.Limits.Global.IngestionLimits = &IngestionLimitSpec{
					IngestionRate:             src.Spec.Limits.Global.IngestionLimits.IngestionRate,
					IngestionBurstSize:        src.Spec.Limits.Global.IngestionLimits.IngestionBurstSize,
					MaxLabelNameLength:        src.Spec.Limits.Global.IngestionLimits.MaxLabelNameLength,
					MaxLabelValueLength:       src.Spec.Limits.Global.IngestionLimits.MaxLabelValueLength,
					MaxLabelNamesPerSeries:    src.Spec.Limits.Global.IngestionLimits.MaxLabelNamesPerSeries,
					MaxGlobalStreamsPerTenant: src.Spec.Limits.Global.IngestionLimits.MaxGlobalStreamsPerTenant,
					MaxLineSize:               src.Spec.Limits.Global.IngestionLimits.MaxLineSize,
				}
			}

			if src.Spec.Limits.Global.QueryLimits != nil {
				dst.Spec.Limits.Global.QueryLimits = &QueryLimitSpec{
					MaxEntriesLimitPerQuery: src.Spec.Limits.Global.QueryLimits.MaxEntriesLimitPerQuery,
					MaxChunksPerQuery:       src.Spec.Limits.Global.QueryLimits.MaxChunksPerQuery,
					MaxQuerySeries:          src.Spec.Limits.Global.QueryLimits.MaxQuerySeries,
				}
			}
		}

		if len(src.Spec.Limits.Tenants) > 0 {
			dst.Spec.Limits.Tenants = make(map[string]LimitsTemplateSpec)
		}

		for tenant, srcSpec := range src.Spec.Limits.Tenants {
			dstSpec := LimitsTemplateSpec{}

			if srcSpec.IngestionLimits != nil {
				dstSpec.IngestionLimits = &IngestionLimitSpec{
					IngestionRate:             srcSpec.IngestionLimits.IngestionRate,
					IngestionBurstSize:        srcSpec.IngestionLimits.IngestionBurstSize,
					MaxLabelNameLength:        srcSpec.IngestionLimits.MaxLabelNameLength,
					MaxLabelValueLength:       srcSpec.IngestionLimits.MaxLabelValueLength,
					MaxLabelNamesPerSeries:    srcSpec.IngestionLimits.MaxLabelNamesPerSeries,
					MaxGlobalStreamsPerTenant: srcSpec.IngestionLimits.MaxGlobalStreamsPerTenant,
					MaxLineSize:               srcSpec.IngestionLimits.MaxLineSize,
				}
			}

			if srcSpec.QueryLimits != nil {
				dstSpec.QueryLimits = &QueryLimitSpec{
					MaxEntriesLimitPerQuery: srcSpec.QueryLimits.MaxEntriesLimitPerQuery,
					MaxChunksPerQuery:       srcSpec.QueryLimits.MaxChunksPerQuery,
					MaxQuerySeries:          srcSpec.QueryLimits.MaxQuerySeries,
				}
			}

			dst.Spec.Limits.Tenants[tenant] = dstSpec
		}
	}

	if src.Spec.Template != nil {
		dst.Spec.Template = &LokiTemplateSpec{}
		if src.Spec.Template.Compactor != nil {
			dst.Spec.Template.Compactor = &LokiComponentSpec{
				Replicas:     src.Spec.Template.Compactor.Replicas,
				NodeSelector: src.Spec.Template.Compactor.NodeSelector,
				Tolerations:  src.Spec.Template.Compactor.Tolerations,
			}
		}
		if src.Spec.Template.Distributor != nil {
			dst.Spec.Template.Distributor = &LokiComponentSpec{
				Replicas:     src.Spec.Template.Distributor.Replicas,
				NodeSelector: src.Spec.Template.Distributor.NodeSelector,
				Tolerations:  src.Spec.Template.Distributor.Tolerations,
			}
		}
		if src.Spec.Template.Ingester != nil {
			dst.Spec.Template.Ingester = &LokiComponentSpec{
				Replicas:     src.Spec.Template.Ingester.Replicas,
				NodeSelector: src.Spec.Template.Ingester.NodeSelector,
				Tolerations:  src.Spec.Template.Ingester.Tolerations,
			}
		}
		if src.Spec.Template.Querier != nil {
			dst.Spec.Template.Querier = &LokiComponentSpec{
				Replicas:     src.Spec.Template.Querier.Replicas,
				NodeSelector: src.Spec.Template.Querier.NodeSelector,
				Tolerations:  src.Spec.Template.Querier.Tolerations,
			}
		}
		if src.Spec.Template.QueryFrontend != nil {
			dst.Spec.Template.QueryFrontend = &LokiComponentSpec{
				Replicas:     src.Spec.Template.QueryFrontend.Replicas,
				NodeSelector: src.Spec.Template.QueryFrontend.NodeSelector,
				Tolerations:  src.Spec.Template.QueryFrontend.Tolerations,
			}
		}
		if src.Spec.Template.Gateway != nil {
			dst.Spec.Template.Gateway = &LokiComponentSpec{
				Replicas:     src.Spec.Template.Gateway.Replicas,
				NodeSelector: src.Spec.Template.Gateway.NodeSelector,
				Tolerations:  src.Spec.Template.Gateway.Tolerations,
			}
		}
		if src.Spec.Template.IndexGateway != nil {
			dst.Spec.Template.IndexGateway = &LokiComponentSpec{
				Replicas:     src.Spec.Template.IndexGateway.Replicas,
				NodeSelector: src.Spec.Template.IndexGateway.NodeSelector,
				Tolerations:  src.Spec.Template.IndexGateway.Tolerations,
			}
		}
		if src.Spec.Template.Ruler != nil {
			dst.Spec.Template.Ruler = &LokiComponentSpec{
				Replicas:     src.Spec.Template.Ruler.Replicas,
				NodeSelector: src.Spec.Template.Ruler.NodeSelector,
				Tolerations:  src.Spec.Template.Ruler.Tolerations,
			}
		}
	}

	if src.Spec.Tenants != nil {
		dst.Spec.Tenants = &TenantsSpec{
			Mode: ModeType(src.Spec.Tenants.Mode),
		}

		for _, srcAuth := range src.Spec.Tenants.Authentication {
			dstAuth := AuthenticationSpec{
				TenantName: srcAuth.TenantName,
				TenantID:   srcAuth.TenantID,
			}

			if srcAuth.OIDC != nil {
				dstAuth.OIDC = &OIDCSpec{
					Secret: &TenantSecretSpec{
						Name: srcAuth.OIDC.Secret.Name,
					},
					IssuerURL:     srcAuth.OIDC.IssuerURL,
					RedirectURL:   srcAuth.OIDC.RedirectURL,
					GroupClaim:    srcAuth.OIDC.GroupClaim,
					UsernameClaim: srcAuth.OIDC.UsernameClaim,
				}
			}

			dst.Spec.Tenants.Authentication = append(dst.Spec.Tenants.Authentication, dstAuth)
		}

		if src.Spec.Tenants.Authorization != nil {
			dstAuthz := &AuthorizationSpec{}

			if src.Spec.Tenants.Authorization.OPA != nil {
				dstAuthz.OPA = &OPASpec{
					URL: src.Spec.Tenants.Authorization.OPA.URL,
				}
			}

			for _, srcRole := range src.Spec.Tenants.Authorization.Roles {
				dstRole := RoleSpec{
					Name:        srcRole.Name,
					Resources:   srcRole.Resources,
					Tenants:     srcRole.Tenants,
					Permissions: []PermissionType{},
				}

				for _, perm := range srcRole.Permissions {
					dstRole.Permissions = append(dstRole.Permissions, PermissionType(perm))
				}

				dstAuthz.Roles = append(dstAuthz.Roles, dstRole)
			}

			for _, srcBinding := range src.Spec.Tenants.Authorization.RoleBindings {
				dstBinding := RoleBindingsSpec{
					Name:  srcBinding.Name,
					Roles: srcBinding.Roles,
				}

				for _, srcSubject := range srcBinding.Subjects {
					dstBinding.Subjects = append(dstBinding.Subjects, Subject{
						Name: srcSubject.Name,
						Kind: SubjectKind(srcSubject.Kind),
					})
				}

				dstAuthz.RoleBindings = append(dstAuthz.RoleBindings, dstBinding)
			}

			dst.Spec.Tenants.Authorization = dstAuthz
		}
	}

	return nil
}
