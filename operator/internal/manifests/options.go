package manifests

import (
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/internal"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// Options is a set of configuration values to use when building manifests such as resource sizes, etc.
// Most of this should be provided - either directly or indirectly - by the user.
type Options struct {
	Name              string
	Namespace         string
	Image             string
	GatewayImage      string
	GatewayBaseDomain string
	ConfigSHA1        string

	Flags FeatureFlags

	Stack                lokiv1beta1.LokiStackSpec
	ResourceRequirements internal.ComponentResources

	ObjectStorage ObjectStorage

	OpenShiftOptions openshift.Options
	TenantSecrets    []*TenantSecrets
	TenantConfigMap  map[string]openshift.TenantData
}

// ObjectStorage for storage config.
type ObjectStorage struct {
	Azure *AzureConfig
	GCS   *GCSConfig
	S3    *S3Config
	Swift *SwiftConfig
}

// AzureConfig for Azure storage config
type AzureConfig struct {
	Env         string
	Container   string
	AccountName string
	AccountKey  string
}

// GCSConfig for GCS storage config
type GCSConfig struct {
	Bucket string
}

// S3Config for S3 storage config
type S3Config struct {
	Endpoint        string
	Region          string
	Buckets         string
	AccessKeyID     string
	AccessKeySecret string
}

// SwiftConfig for Swift storage config
type SwiftConfig struct {
	AuthURL           string
	Username          string
	UserDomainName    string
	UserDomainID      string
	UserID            string
	Password          string
	DomainID          string
	DomainName        string
	ProjectID         string
	ProjectName       string
	ProjectDomainID   string
	ProjectDomainName string
	Region            string
	Container         string
}

// FeatureFlags contains flags that activate various features
type FeatureFlags struct {
	EnableCertificateSigningService bool
	EnableServiceMonitors           bool
	EnableTLSServiceMonitorConfig   bool
	EnableGateway                   bool
	EnableGatewayRoute              bool
}

// TenantSecrets for clientID, clientSecret and issuerCAPath for tenant's authentication.
type TenantSecrets struct {
	TenantName   string
	ClientID     string
	ClientSecret string
	IssuerCAPath string
}
