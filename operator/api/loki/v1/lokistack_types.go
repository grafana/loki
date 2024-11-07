package v1

import (
	"strings"

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
// +kubebuilder:validation:Enum="1x.demo";"1x.pico";"1x.extra-small";"1x.small";"1x.medium"
type LokiStackSizeType string

const (
	// SizeOneXDemo defines the size of a single Loki deployment
	// with tiny resource requirements and without HA support.
	// This size is intended to run in single-node clusters on laptops,
	// it is only useful for very light testing, demonstrations, or prototypes.
	// There are no ingestion/query performance guarantees.
	// DO NOT USE THIS IN PRODUCTION!
	SizeOneXDemo LokiStackSizeType = "1x.demo"

	// SizeOneXPico defines the size of a single Loki deployment
	// with extra small resources/limits requirements and HA support for all
	// Loki components. This size is dedicated for setup **without** the
	// requirement for single replication factor and auto-compaction.
	//
	// FIXME: Add clear description of ingestion/query performance expectations.
	SizeOneXPico LokiStackSizeType = "1x.pico"

	// SizeOneXExtraSmall defines the size of a single Loki deployment
	// with extra small resources/limits requirements and HA support for all
	// Loki components. This size is dedicated for setup **without** the
	// requirement for single replication factor and auto-compaction.
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
	// Secret defines the spec for the clientID and clientSecret for tenant's authentication.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant Secret"
	Secret *TenantSecretSpec `json:"secret"`
	// IssuerCA defines the spec for the issuer CA for tenant's authentication.
	//
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="IssuerCA ConfigMap"
	IssuerCA *CASpec `json:"issuerCA"`
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

// MTLSSpec specifies mTLS configuration parameters.
type MTLSSpec struct {
	// CA defines the spec for the custom CA for tenant's authentication.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CA ConfigMap"
	CA *CASpec `json:"ca"`
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
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OIDC Configuration"
	OIDC *OIDCSpec `json:"oidc,omitempty"`

	// TLSConfig defines the spec for the mTLS tenant's authentication.
	//
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="mTLS Configuration"
	MTLS *MTLSSpec `json:"mTLS,omitempty"`
}

// ModeType is the authentication/authorization mode in which LokiStack Gateway will be configured.
//
// +kubebuilder:validation:Enum=static;dynamic;openshift-logging;openshift-network
type ModeType string

const (
	// Static mode asserts the Authorization Spec's Roles and RoleBindings
	// using an in-process OpenPolicyAgent Rego authorizer.
	Static ModeType = "static"
	// Dynamic mode delegates the authorization to a third-party OPA-compatible endpoint.
	Dynamic ModeType = "dynamic"
	// OpenshiftLogging mode provides fully automatic OpenShift in-cluster authentication and authorization support for application, infrastructure and audit logs.
	OpenshiftLogging ModeType = "openshift-logging"
	// OpenshiftNetwork mode provides fully automatic OpenShift in-cluster authentication and authorization support for network logs only.
	OpenshiftNetwork ModeType = "openshift-network"
)

// TenantsSpec defines the mode, authentication and authorization
// configuration of the lokiStack gateway component.
type TenantsSpec struct {
	// Mode defines the mode in which lokistack-gateway component will be configured.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:default:=openshift-logging
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:static","urn:alm:descriptor:com.tectonic.ui:select:dynamic","urn:alm:descriptor:com.tectonic.ui:select:openshift-logging","urn:alm:descriptor:com.tectonic.ui:select:openshift-network"},displayName="Mode"
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

	// Openshift defines the configuration specific to Openshift modes.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Openshift"
	Openshift *OpenshiftTenantSpec `json:"openshift,omitempty"`
}

// OpenshiftTenantSpec defines the configuration specific to Openshift modes.
type OpenshiftTenantSpec struct {
	// AdminGroups defines a list of groups, whose members are considered to have admin-privileges by the Loki Operator.
	// Setting this to an empty array disables admin groups.
	//
	// By default the following groups are considered admin-groups:
	//  - system:cluster-admins
	//  - cluster-admin
	//  - dedicated-admin
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Admin Groups"
	AdminGroups []string `json:"adminGroups"`

	// OTLP contains settings for ingesting data using OTLP in the OpenShift tenancy mode.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="OpenTelemetry Protocol"
	OTLP *OpenshiftOTLPConfig `json:"otlp,omitempty"`
}

// OpenshiftOTLPConfig defines configuration specific to users using OTLP together with an OpenShift tenancy mode.
type OpenshiftOTLPConfig struct {
	// DisableRecommendedAttributes can be used to reduce the number of attributes used for stream labels and structured
	// metadata.
	//
	// Enabling this setting removes the "recommended attributes" from the generated Loki configuration. This will cause
	// meta information to not be available as stream labels or structured metadata, potentially making queries more
	// expensive and less performant.
	//
	// Note that there is a set of "required attributes", needed for OpenShift Logging to work properly. Those will be
	// added to the configuration, even if this field is set to true.
	//
	// This option is supposed to be combined with a custom label configuration customizing the labels for the specific
	// usecase.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Disable recommended OTLP attributes"
	DisableRecommendedAttributes bool `json:"disableRecommendedAttributes,omitempty"`
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

	// PodAntiAffinity defines the pod anti affinity scheduling rules to schedule pods
	// of a component.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podAntiAffinity",displayName="PodAntiAffinity"
	PodAntiAffinity *corev1.PodAntiAffinity `json:"podAntiAffinity,omitempty"`
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

// ClusterProxy is the Proxy configuration when the cluster is behind a Proxy.
type ClusterProxy struct {
	// HTTPProxy configures the HTTP_PROXY/http_proxy env variable.
	//
	// +optional
	// +kubebuilder:validation:optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="HTTPProxy"
	HTTPProxy string `json:"httpProxy,omitempty"`
	// HTTPSProxy configures the HTTPS_PROXY/https_proxy env variable.
	//
	// +optional
	// +kubebuilder:validation:optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="HTTPSProxy"
	HTTPSProxy string `json:"httpsProxy,omitempty"`
	// NoProxy configures the NO_PROXY/no_proxy env variable.
	//
	// +optional
	// +kubebuilder:validation:optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NoProxy"
	NoProxy string `json:"noProxy,omitempty"`
}

// HashRingType defines the type of hash ring which can be used with the Loki cluster.
//
// +kubebuilder:validation:Enum=memberlist
type HashRingType string

const (
	// HashRingMemberList when using memberlist for the distributed hash ring.
	HashRingMemberList HashRingType = "memberlist"
)

// InstanceAddrType defines the type of pod network to use for advertising IPs to the ring.
//
// +kubebuilder:validation:Enum=default;podIP
type InstanceAddrType string

const (
	// InstanceAddrDefault when using the first from any private network interfaces (RFC 1918 and RFC 6598).
	InstanceAddrDefault InstanceAddrType = "default"
	// InstanceAddrPodIP when using the public pod IP from the cluster's pod network.
	InstanceAddrPodIP InstanceAddrType = "podIP"
)

// MemberListSpec defines the configuration for the memberlist based hash ring.
type MemberListSpec struct {
	// InstanceAddrType defines the type of address to use to advertise to the ring.
	// Defaults to the first address from any private network interfaces of the current pod.
	// Alternatively the public pod IP can be used in case private networks (RFC 1918 and RFC 6598)
	// are not available.
	//
	// +optional
	// +kubebuilder:validation:optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:default","urn:alm:descriptor:com.tectonic.ui:select:podIP"},displayName="Instance Address"
	InstanceAddrType InstanceAddrType `json:"instanceAddrType,omitempty"`

	// EnableIPv6 enables IPv6 support for the memberlist based hash ring.
	//
	// Currently this also forces the instanceAddrType to podIP to avoid local address lookup
	// for the memberlist.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enable IPv6"
	EnableIPv6 bool `json:"enableIPv6,omitempty"`
}

// HashRingSpec defines the hash ring configuration
type HashRingSpec struct {
	// Type of hash ring implementation that should be used
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:memberlist"},displayName="Type"
	// +kubebuilder:default:=memberlist
	Type HashRingType `json:"type"`

	// MemberList configuration spec
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Memberlist Config"
	MemberList *MemberListSpec `json:"memberlist,omitempty"`
}

type CASpec struct {
	// Key is the data key of a ConfigMap containing a CA certificate.
	// It needs to be in the same namespace as the LokiStack custom resource.
	// If empty, it defaults to "service-ca.crt".
	//
	// +optional
	// +kubebuilder:validation:optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CA ConfigMap Key"
	CAKey string `json:"caKey,omitempty"`
	// CA is the name of a ConfigMap containing a CA certificate.
	// It needs to be in the same namespace as the LokiStack custom resource.
	//
	// +required
	// +kubebuilder:validation:required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:ConfigMap",displayName="CA ConfigMap Name"
	CA string `json:"caName"`
}

// ObjectStorageTLSSpec is the TLS configuration for reaching the object storage endpoint.
type ObjectStorageTLSSpec struct {
	CASpec `json:",inline"`
}

// ObjectStorageSecretType defines the type of storage which can be used with the Loki cluster.
//
// +kubebuilder:validation:Enum=azure;gcs;s3;swift;alibabacloud;
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

	// ObjectStorageSecretAlibabaCloud when using AlibabaCloud OSS for Loki storage
	ObjectStorageSecretAlibabaCloud ObjectStorageSecretType = "alibabacloud"
)

// ObjectStorageSecretSpec is a secret reference containing name only, no namespace.
type ObjectStorageSecretSpec struct {
	// Type of object storage that should be used
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:azure","urn:alm:descriptor:com.tectonic.ui:select:gcs","urn:alm:descriptor:com.tectonic.ui:select:s3","urn:alm:descriptor:com.tectonic.ui:select:swift","urn:alm:descriptor:com.tectonic.ui:select:alibabacloud"},displayName="Object Storage Secret Type"
	Type ObjectStorageSecretType `json:"type"`

	// Name of a secret in the namespace configured for object storage secrets.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret",displayName="Object Storage Secret Name"
	Name string `json:"name"`

	// CredentialMode can be used to set the desired credential mode for authenticating with the object storage.
	// If this is not set, then the operator tries to infer the credential mode from the provided secret and its
	// own configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	CredentialMode CredentialMode `json:"credentialMode,omitempty"`
}

// ObjectStorageSchemaVersion defines the storage schema version which will be
// used with the Loki cluster.
//
// +kubebuilder:validation:Enum=v11;v12;v13
type ObjectStorageSchemaVersion string

const (
	// ObjectStorageSchemaV11 when using v11 for the storage schema
	ObjectStorageSchemaV11 ObjectStorageSchemaVersion = "v11"

	// ObjectStorageSchemaV12 when using v12 for the storage schema
	ObjectStorageSchemaV12 ObjectStorageSchemaVersion = "v12"

	// ObjectStorageSchemaV13 when using v13 for the storage schema
	ObjectStorageSchemaV13 ObjectStorageSchemaVersion = "v13"
)

// ObjectStorageSchema defines a schema version and the date when it will become effective.
type ObjectStorageSchema struct {
	// Version for writing and reading logs.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:v11","urn:alm:descriptor:com.tectonic.ui:select:v12","urn:alm:descriptor:com.tectonic.ui:select:v13"},displayName="Version"
	Version ObjectStorageSchemaVersion `json:"version"`

	// EffectiveDate contains a date in YYYY-MM-DD format which is interpreted in the UTC time zone.
	//
	// The configuration always needs at least one schema that is currently valid. This means that when creating a new
	// LokiStack it is recommended to add a schema with the latest available version and an effective date of "yesterday".
	// New schema versions added to the configuration always needs to be placed "in the future", so that Loki can start
	// using it once the day rolls over.
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

	// Timeout when querying ingesters or storage during the execution of a query request.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="3m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Query Timeout"
	QueryTimeout string `json:"queryTimeout,omitempty"`

	// CardinalityLimit defines the cardinality limit for index queries.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Cardinality Limit"
	CardinalityLimit int32 `json:"cardinalityLimit,omitempty"`

	// MaxVolumeSeries defines the maximum number of aggregated series in a log-volume response
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Volume Series"
	MaxVolumeSeries int32 `json:"maxVolumeSeries,omitempty"`
}

// BlockedQueryType defines which type of query a blocked query should apply to.
//
// +kubebuilder:validation:Enum=filter;limited;metric
type BlockedQueryType string

const (
	// BlockedQueryFilter is used, when the blocked query should apply to queries using a log filter.
	BlockedQueryFilter BlockedQueryType = "filter"
	// BlockedQueryLimited is used, when the blocked query should apply to queries without a filter or a metric aggregation.
	BlockedQueryLimited BlockedQueryType = "limited"
	// BlockedQueryMetric is used, when the blocked query should apply to queries with an aggregation.
	BlockedQueryMetric BlockedQueryType = "metric"
)

// BlockedQueryTypes defines a slice of BlockedQueryType values to be used for a blocked query.
type BlockedQueryTypes []BlockedQueryType

// BlockedQuerySpec defines the rule spec for queries to be blocked.
//
// +kubebuilder:validation:MinProperties:=1
type BlockedQuerySpec struct {
	// Hash is a 32-bit FNV-1 hash of the query string.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Query Hash"
	Hash int32 `json:"hash,omitempty"`
	// Pattern defines the pattern matching the queries to be blocked.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Query Pattern"
	Pattern string `json:"pattern,omitempty"`
	// Regex defines if the pattern is a regular expression. If false the pattern will be used only for exact matches.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Regex"
	Regex bool `json:"regex,omitempty"`
	// Types defines the list of query types that should be considered for blocking.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Query Types"
	Types BlockedQueryTypes `json:"types,omitempty"`
}

// PerTenantQueryLimitSpec defines the limits applied to per tenant query path.
type PerTenantQueryLimitSpec struct {
	QueryLimitSpec `json:",omitempty"`

	// Blocked defines the list of rules to block matching queries.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Blocked"
	Blocked []BlockedQuerySpec `json:"blocked,omitempty"`
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

	// PerStreamDesiredRate defines the desired ingestion rate per second that LokiStack should
	// target applying automatic stream sharding. Units MB.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Per Stream Desired Rate (in MB)"
	PerStreamDesiredRate int32 `json:"perStreamDesiredRate,omitempty"`

	// PerStreamRateLimit defines the maximum byte rate per second per stream. Units MB.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Maximum byte rate per second per stream (in MB)"
	PerStreamRateLimit int32 `json:"perStreamRateLimit,omitempty"`

	// PerStreamRateLimitBurst defines the maximum burst bytes per stream. Units MB.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Maximum burst bytes per stream (in MB)"
	PerStreamRateLimitBurst int32 `json:"perStreamRateLimitBurst,omitempty"`
}

// OTLPSpec defines which resource, scope and log attributes should be used as stream labels or
// stored as structured metadata.
type OTLPSpec struct {
	// StreamLabels configures which resource attributes are converted to Loki stream labels.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Stream Labels"
	StreamLabels *OTLPStreamLabelSpec `json:"streamLabels,omitempty"`

	// StructuredMetadata configures which attributes are saved in structured metadata.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Structured Metadata"
	StructuredMetadata *OTLPMetadataSpec `json:"structuredMetadata,omitempty"`
}

type OTLPStreamLabelSpec struct {
	// ResourceAttributes lists the names of the resource attributes that should be converted into Loki stream labels.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Attributes"
	ResourceAttributes []OTLPAttributeReference `json:"resourceAttributes,omitempty"`
}

type OTLPMetadataSpec struct {
	// ResourceAttributes lists the names of resource attributes that should be included in structured metadata.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Attributes"
	ResourceAttributes []OTLPAttributeReference `json:"resourceAttributes,omitempty"`

	// ScopeAttributes lists the names of scope attributes that should be included in structured metadata.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Scope Attributes"
	ScopeAttributes []OTLPAttributeReference `json:"scopeAttributes,omitempty"`

	// LogAttributes lists the names of log attributes that should be included in structured metadata.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Log Attributes"
	LogAttributes []OTLPAttributeReference `json:"logAttributes,omitempty"`
}

type OTLPAttributeReference struct {
	// Name contains either a verbatim name of an attribute or a regular expression matching many attributes.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
	Name string `json:"name"`

	// If Regex is true, then Name is treated as a regular expression instead of as a verbatim attribute name.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Treat name as regular expression"
	Regex bool `json:"regex,omitempty"`
}

// RetentionStreamSpec defines a log stream with separate retention time.
type RetentionStreamSpec struct {
	// Days contains the number of days logs are kept.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum:=1
	Days uint `json:"days"`

	// Priority defines the priority of this selector compared to other retention rules.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=1
	Priority uint32 `json:"priority,omitempty"`

	// Selector contains the LogQL query used to define the log stream.
	//
	// +required
	// +kubebuilder:validation:Required
	Selector string `json:"selector"`
}

// RetentionLimitSpec controls how long logs will be kept in storage.
type RetentionLimitSpec struct {
	// Days contains the number of days logs are kept.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum:=1
	Days uint `json:"days"`

	// Stream defines the log stream.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Streams []*RetentionStreamSpec `json:"streams,omitempty"`
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

	// OTLP to configure which resource, scope and log attributes are stored as stream labels or structured metadata.
	//
	// Tenancy modes can provide a default OTLP configuration, when no custom OTLP configuration is set or even
	// enforce the use of some required attributes.
	//
	// +optional
	// +kubebuilder:validation:Optional
	OTLP *OTLPSpec `json:"otlp,omitempty"`

	// Retention defines how long logs are kept in storage.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Retention *RetentionLimitSpec `json:"retention,omitempty"`
}

// PerTenantLimitsTemplateSpec defines the limits  applied at ingestion or query path.
type PerTenantLimitsTemplateSpec struct {
	// IngestionLimits defines the limits applied on ingested log streams.
	//
	// +optional
	// +kubebuilder:validation:Optional
	IngestionLimits *IngestionLimitSpec `json:"ingestion,omitempty"`

	// QueryLimits defines the limit applied on querying log streams.
	//
	// +optional
	// +kubebuilder:validation:Optional
	QueryLimits *PerTenantQueryLimitSpec `json:"queries,omitempty"`

	// OTLP to configure which resource, scope and log attributes are stored as stream labels or structured metadata.
	//
	// Tenancy modes can provide a default OTLP configuration, when no custom OTLP configuration is set or even
	// enforce the use of some required attributes.
	//
	// The per-tenant configuration for OTLP attributes will be merged with the global configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	OTLP *OTLPSpec `json:"otlp,omitempty"`

	// Retention defines how long logs are kept in storage.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Retention *RetentionLimitSpec `json:"retention,omitempty"`
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
	Tenants map[string]PerTenantLimitsTemplateSpec `json:"tenants,omitempty"`
}

// RulesSpec defines the spec for the ruler component.
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
	// +optional
	// +kubebuilder:validation:optional
	// +kubebuilder:default:=Managed
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Managed","urn:alm:descriptor:com.tectonic.ui:select:Unmanaged"},displayName="Management State"
	ManagementState ManagementStateType `json:"managementState,omitempty"`

	// Size defines one of the support Loki deployment scale out sizes.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:1x.pico","urn:alm:descriptor:com.tectonic.ui:select:1x.extra-small","urn:alm:descriptor:com.tectonic.ui:select:1x.small","urn:alm:descriptor:com.tectonic.ui:select:1x.medium"},displayName="LokiStack Size"
	Size LokiStackSizeType `json:"size"`

	// HashRing defines the spec for the distributed hash ring configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Hash Ring"
	HashRing *HashRingSpec `json:"hashRing,omitempty"`

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

	// Proxy defines the spec for the object proxy to configure cluster proxy information.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Proxy"
	Proxy *ClusterProxy `json:"proxy,omitempty"`

	// Deprecated: Please use replication.factor instead. This field will be removed in future versions of this CRD.
	// ReplicationFactor defines the policy for log stream replication.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Replication Factor"
	ReplicationFactor int32 `json:"replicationFactor,omitempty"`

	// Replication defines the configuration for Loki data replication.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Replication Spec"
	Replication *ReplicationSpec `json:"replication,omitempty"`

	// Rules defines the spec for the ruler component.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Rules"
	Rules *RulesSpec `json:"rules,omitempty"`

	// Limits defines the limits to be applied to log stream processing.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Rate Limiting"
	Limits *LimitsSpec `json:"limits,omitempty"`

	// Template defines the resource/limits/tolerations/nodeselectors per component.
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

type ReplicationSpec struct {
	// Factor defines the policy for log stream replication.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Replication Factor"
	Factor int32 `json:"factor,omitempty"`

	// Zones defines an array of ZoneSpec that the scheduler will try to satisfy.
	// IMPORTANT: Make sure that the replication factor defined is less than or equal to the number of available zones.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Zones Spec"
	Zones []ZoneSpec `json:"zones,omitempty"`
}

// ZoneSpec defines the spec to support zone-aware component deployments.
type ZoneSpec struct {
	// MaxSkew describes the maximum degree to which Pods can be unevenly distributed.
	//
	// +required
	// +kubebuilder:default:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Max Skew"
	MaxSkew int `json:"maxSkew"`

	// TopologyKey is the key that defines a topology in the Nodes' labels.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Topology Key"
	TopologyKey string `json:"topologyKey"`
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

	// ConditionWarning is used for configurations that are not recommended, but don't currently cause
	// issues. There can be multiple warning conditions active at a time.
	ConditionWarning LokiStackConditionType = "Warning"
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
	// ReasonMissingTokenCCOAuthSecret when the secret generated by CCO for token authentication is missing.
	// This is usually a transient error because the secret is not immediately available after creating the
	// CredentialsRequest, but it can persist if the CCO or its configuration are incorrect.
	ReasonMissingTokenCCOAuthSecret LokiStackConditionReason = "MissingTokenCCOAuthenticationSecret"
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
	// ReasonMissingGatewayTenantConfigMap when the required tenant configmap
	// for authentication is missing.
	ReasonMissingGatewayTenantConfigMap LokiStackConditionReason = "MissingGatewayTenantConfigMap"
	// ReasonInvalidGatewayTenantSecret when the format of the secret is invalid.
	ReasonInvalidGatewayTenantSecret LokiStackConditionReason = "InvalidGatewayTenantSecret"
	// ReasonInvalidGatewayTenantConfigMap when the format of the configmap is invalid.
	ReasonInvalidGatewayTenantConfigMap LokiStackConditionReason = "InvalidGatewayTenantConfigMap"
	// ReasonMissingGatewayAuthenticationConfig when the config for when a tenant is missing authentication config
	ReasonMissingGatewayAuthenticationConfig LokiStackConditionReason = "MissingGatewayTenantAuthenticationConfig"
	// ReasonInvalidTenantsConfiguration when the tenant configuration provided is invalid.
	ReasonInvalidTenantsConfiguration LokiStackConditionReason = "InvalidTenantsConfiguration"
	// ReasonMissingGatewayOpenShiftBaseDomain when the reconciler cannot lookup the OpenShift DNS base domain.
	ReasonMissingGatewayOpenShiftBaseDomain LokiStackConditionReason = "MissingGatewayOpenShiftBaseDomain"
	// ReasonFailedCertificateRotation when the reconciler cannot rotate any of the required TLS certificates.
	ReasonFailedCertificateRotation LokiStackConditionReason = "FailedCertificateRotation"
	// ReasonQueryTimeoutInvalid when the QueryTimeout can not be parsed.
	ReasonQueryTimeoutInvalid LokiStackConditionReason = "ReasonQueryTimeoutInvalid"
	// ReasonZoneAwareNodesMissing when the cluster does not contain any nodes with the labels needed for zone-awareness.
	ReasonZoneAwareNodesMissing LokiStackConditionReason = "ReasonZoneAwareNodesMissing"
	// ReasonZoneAwareEmptyLabel when the node-label used for zone-awareness has an empty value.
	ReasonZoneAwareEmptyLabel LokiStackConditionReason = "ReasonZoneAwareEmptyLabel"
	// ReasonStorageNeedsSchemaUpdate when the object storage schema version is older than V13
	ReasonStorageNeedsSchemaUpdate LokiStackConditionReason = "StorageNeedsSchemaUpdate"
)

// PodStatus is a short description of the status a Pod can be in.
type PodStatus string

const (
	// PodPending means the pod has been accepted by the system, but one or more of the containers
	// has not been started. This includes time before being bound to a node, as well as time spent
	// pulling images onto the host.
	PodPending PodStatus = "Pending"
	// PodRunning means the pod has been bound to a node and all of the containers have been started.
	// At least one container is still running or is in the process of being restarted.
	PodRunning PodStatus = "Running"
	// PodReady means the pod has been started and the readiness probe reports a successful status.
	PodReady PodStatus = "Ready"
	// PodFailed means that all containers in the pod have terminated, and at least one container has
	// terminated in a failure (exited with a non-zero exit code or was stopped by the system).
	PodFailed PodStatus = "Failed"
	// PodStatusUnknown is used when none of the other statuses apply or the information is not ready yet.
	PodStatusUnknown PodStatus = "Unknown"
)

// PodStatusMap defines the type for mapping pod status to pod name.
type PodStatusMap map[PodStatus][]string

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

// CredentialMode represents the type of authentication used for accessing the object storage.
//
// +kubebuilder:validation:Enum=static;token;token-cco
type CredentialMode string

const (
	// CredentialModeStatic represents the usage of static, long-lived credentials stored in a Secret.
	// This is the default authentication mode and available for all supported object storage types.
	CredentialModeStatic CredentialMode = "static"
	// CredentialModeToken represents the usage of short-lived tokens retrieved from a credential source.
	// In this mode the static configuration does not contain credentials needed for the object storage.
	// Instead, they are generated during runtime using a service, which allows for shorter-lived credentials and
	// much more granular control. This authentication mode is not supported for all object storage types.
	CredentialModeToken CredentialMode = "token"
	// CredentialModeTokenCCO represents the usage of short-lived tokens retrieved from a credential source.
	// This mode is similar to CredentialModeToken, but instead of having a user-configured credential source,
	// it is configured by the environment and the operator relies on the Cloud Credential Operator to provide
	// a secret. This mode is only supported for certain object storage types in certain runtime environments.
	CredentialModeTokenCCO CredentialMode = "token-cco"
)

// LokiStackStorageStatus defines the observed state of
// the Loki storage configuration.
type LokiStackStorageStatus struct {
	// Schemas is a list of schemas which have been applied
	// to the LokiStack.
	//
	// +optional
	// +kubebuilder:validation:Optional
	Schemas []ObjectStorageSchema `json:"schemas,omitempty"`

	// CredentialMode contains the authentication mode used for accessing the object storage.
	//
	// +optional
	// +kubebuilder:validation:Optional
	CredentialMode CredentialMode `json:"credentialMode,omitempty"`
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
// +kubebuilder:storageversion
// +kubebuilder:resource:categories=logging
// +kubebuilder:webhook:path=/validate-loki-grafana-com-v1-lokistack,mutating=false,failurePolicy=fail,sideEffects=None,groups=loki.grafana.com,resources=lokistacks,verbs=create;update,versions=v1,name=vlokistack.loki.grafana.com,admissionReviewVersions=v1

// LokiStack is the Schema for the lokistacks API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="LokiStack",resources={{Deployment,v1},{StatefulSet,v1},{ConfigMap,v1},{Ingress,v1},{Service,v1},{ServiceAccount,v1},{PersistentVolumeClaims,v1},{Route,v1},{ServiceMonitor,v1}}
type LokiStack struct {
	// LokiStack CR spec field.
	Spec LokiStackSpec `json:"spec,omitempty"`
	// LokiStack CR spec Status.
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

// Hub declares the v1.LokiStack as the hub CRD version.
func (*LokiStack) Hub() {}

func (t BlockedQueryTypes) String() string {
	res := make([]string, 0, len(t))
	for _, t := range t {
		res = append(res, string(t))
	}

	return strings.Join(res, ",")
}
