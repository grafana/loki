package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/go-logr/logr"
	openshiftv1 "github.com/openshift/api/config/v1"
	"sigs.k8s.io/yaml"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// Define the manifest options here as structured objects
type config struct {
	Name      string
	Namespace string
	Image     string

	featureFlags  configv1.FeatureGates
	objectStorage storage.Options

	crFilepath string
	writeToDir string
}

func (c *config) registerFlags(f *flag.FlagSet) {
	// LokiStack metadata options
	f.StringVar(&c.Name, "name", "", "The name of the stack")
	f.StringVar(&c.Namespace, "namespace", "", "Namespace to deploy to")
	f.StringVar(&c.Image, "image", manifests.DefaultContainerImage, "The Loki image pull spec loation.")
	// Feature flags
	f.BoolVar(&c.featureFlags.ServiceMonitors, "with-service-monitors", false, "Enable service monitors for all LokiStack components.")
	f.BoolVar(&c.featureFlags.OpenShift.ServingCertsService, "with-serving-certs-service", false, "Enable usage of serving certs service on OpenShift.")
	f.BoolVar(&c.featureFlags.BuiltInCertManagement.Enabled, "with-builtin-cert-management", false, "Enable usage built-in cert generation and rotation.")
	f.StringVar(&c.featureFlags.BuiltInCertManagement.CACertValidity, "ca-cert-validity", "8760h", "CA Certificate validity duration.")
	f.StringVar(&c.featureFlags.BuiltInCertManagement.CACertRefresh, "ca-cert-refresh", "7008h", "CA Certificate refresh time.")
	f.StringVar(&c.featureFlags.BuiltInCertManagement.CertValidity, "target-cert-validity", "2160h", "Target Certificate validity duration.")
	f.StringVar(&c.featureFlags.BuiltInCertManagement.CertRefresh, "target-cert-refresh", "1728h", "Target Certificate refresh time.")
	f.BoolVar(&c.featureFlags.HTTPEncryption, "with-http-tls-services", false, "Enables TLS for all LokiStack GRPC services.")
	f.BoolVar(&c.featureFlags.GRPCEncryption, "with-grpc-tls-services", false, "Enables TLS for all LokiStack HTTP services.")
	f.BoolVar(&c.featureFlags.ServiceMonitorTLSEndpoints, "with-service-monitor-tls-endpoints", false, "Enable TLS endpoint for service monitors.")
	f.BoolVar(&c.featureFlags.LokiStackAlerts, "with-lokistack-alerts", false, "Enables prometheus alerts")
	f.BoolVar(&c.featureFlags.LokiStackGateway, "with-lokistack-gateway", false, "Enables the manifest creation for the entire lokistack-gateway.")
	f.BoolVar(&c.featureFlags.RestrictedPodSecurityStandard, "with-restricted-pod-security-standard", false, "Enable restricted security standard settings")
	// Object storage options
	c.objectStorage = storage.Options{
		S3: &storage.S3StorageConfig{},
	}
	f.StringVar(&c.objectStorage.S3.Endpoint, "object-storage.s3.endpoint", "", "The S3 endpoint location.")
	f.StringVar(&c.objectStorage.S3.Buckets, "object-storage.s3.buckets", "", "A comma-separated list of S3 buckets.")
	f.StringVar(&c.objectStorage.S3.Region, "object-storage.s3.region", "", "An S3 region.")
	// Input and output file/dir options
	f.StringVar(&c.crFilepath, "custom-resource.path", "", "Path to a custom resource YAML file.")
	f.StringVar(&c.writeToDir, "output.write-dir", "", "write each file to the specified directory.")
	// TLS profile option
	f.StringVar(&c.featureFlags.TLSProfile, "tls-profile", "", "The TLS security Profile configuration.")
}

func (c *config) validateFlags(log logr.Logger) {
	if cfg.crFilepath == "" {
		log.Info("-custom.resource.path flag is required")
		os.Exit(1)
	}
	if cfg.Name == "" {
		log.Info("-name flag is required")
		os.Exit(1)
	}
	if cfg.Namespace == "" {
		log.Info("-namespace flag is required")
		os.Exit(1)
	}
	// Validate manifests.objectStorage
	if cfg.objectStorage.S3.Endpoint == "" {
		log.Info("-object-storage.s3.endpoint flag is required")
		os.Exit(1)
	}
	if cfg.objectStorage.S3.Buckets == "" {
		log.Info("-object-storage.s3.buckets flag is required")
		os.Exit(1)
	}
	// Validate feature flags
	if cfg.featureFlags.LokiStackAlerts && !cfg.featureFlags.ServiceMonitors {
		log.Info("-with-prometheus-alerts flag requires -with-service-monitors")
		os.Exit(1)
	}
}

var cfg *config

func init() {
	cfg = &config{}
}

func main() {
	logger := log.NewLogger("loki-broker")

	f := flag.NewFlagSet("", flag.ExitOnError)
	cfg.registerFlags(f)
	if err := f.Parse(os.Args[1:]); err != nil {
		logger.Error(err, "failed to parse flags")
	}

	cfg.validateFlags(logger)

	b, err := os.ReadFile(cfg.crFilepath)
	if err != nil {
		logger.Info("failed to read custom resource file", "path", cfg.crFilepath)
		os.Exit(1)
	}

	ls := &lokiv1.LokiStack{}
	if err = yaml.Unmarshal(b, ls); err != nil {
		logger.Error(err, "failed to unmarshal LokiStack CR", "path", cfg.crFilepath)
		os.Exit(1)
	}

	if cfg.featureFlags.TLSProfile != "" &&
		cfg.featureFlags.TLSProfile != string(configv1.TLSProfileOldType) &&
		cfg.featureFlags.TLSProfile != string(configv1.TLSProfileIntermediateType) &&
		cfg.featureFlags.TLSProfile != string(configv1.TLSProfileModernType) {
		logger.Error(err, "failed to parse TLS profile. Allowed values: 'Old', 'Intermediate', 'Modern'", "value", cfg.featureFlags.TLSProfile)
		os.Exit(1)
	}

	// Convert config to manifest.Options
	opts := manifests.Options{
		Name:          cfg.Name,
		Namespace:     cfg.Namespace,
		Image:         cfg.Image,
		Stack:         ls.Spec,
		Gates:         cfg.featureFlags,
		ObjectStorage: cfg.objectStorage,
	}

	if optErr := manifests.ApplyDefaultSettings(&opts); optErr != nil {
		logger.Error(optErr, "failed to conform options to build settings")
		os.Exit(1)
	}

	var tlsSecurityProfile *openshiftv1.TLSSecurityProfile
	if cfg.featureFlags.TLSProfile != "" {
		tlsSecurityProfile = &openshiftv1.TLSSecurityProfile{
			Type: openshiftv1.TLSProfileType(cfg.featureFlags.TLSProfile),
		}
	}

	if optErr := manifests.ApplyTLSSettings(&opts, tlsSecurityProfile); optErr != nil {
		logger.Error(optErr, "failed to conform options to tls profile settings")
		os.Exit(1)
	}

	objects, err := manifests.BuildAll(opts)
	if err != nil {
		logger.Error(err, "failed to build manifests")
		os.Exit(1)
	}

	for _, o := range objects {
		b, err := yaml.Marshal(o)
		if err != nil {
			logger.Error(err, "failed to marshal manifest", "name", o.GetName(), "kind", o.GetObjectKind())
			continue
		}

		if cfg.writeToDir != "" {
			basename := fmt.Sprintf("%s-%s.yaml", o.GetObjectKind().GroupVersionKind().Kind, o.GetName())
			fname := strings.ToLower(path.Join(cfg.writeToDir, basename))
			if err := os.WriteFile(fname, b, 0o644); err != nil {
				logger.Error(err, "failed to write file to directory", "path", fname)
				os.Exit(1)
			}
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "---\n%s", b)
		}
	}
}
