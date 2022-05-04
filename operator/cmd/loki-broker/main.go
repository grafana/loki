package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/go-logr/logr"
	"github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"sigs.k8s.io/yaml"
)

// Define the manifest options here as structured objects
type config struct {
	Name      string
	Namespace string
	Image     string

	featureFlags  manifests.FeatureFlags
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
	c.featureFlags = manifests.FeatureFlags{}
	f.BoolVar(&c.featureFlags.EnableCertificateSigningService, "with-cert-signing-service", false, "Enable usage of cert-signing service for scraping prometheus metrics via TLS.")
	f.BoolVar(&c.featureFlags.EnableServiceMonitors, "with-service-monitors", false, "Enable service monitors for all LokiStack components.")
	f.BoolVar(&c.featureFlags.EnableTLSServiceMonitorConfig, "with-tls-service-monitors", false, "Enable TLS endpoint for service monitors.")
	f.BoolVar(&c.featureFlags.EnablePrometheusAlerts, "with-prometheus-alerts", false, "Enables prometheus alerts")
	f.BoolVar(&c.featureFlags.EnableGateway, "with-lokistack-gateway", false, "Enables the manifest creation for the entire lokistack-gateway.")
	// Object storage options
	c.objectStorage = storage.Options{
		S3: &storage.S3StorageConfig{},
	}
	f.StringVar(&c.objectStorage.S3.Endpoint, "object-storage.s3.endpoint", "", "The S3 endpoint location.")
	f.StringVar(&c.objectStorage.S3.Buckets, "object-storage.s3.buckets", "", "A comma-separated list of S3 buckets.")
	f.StringVar(&c.objectStorage.S3.Region, "object-storage.s3.region", "", "An S3 region.")
	f.StringVar(&c.objectStorage.S3.AccessKeyID, "object-storage.s3.access-key-id", "", "The access key id for S3.")
	f.StringVar(&c.objectStorage.S3.AccessKeySecret, "object-storage.s3.access-key-secret", "", "The access key secret for S3.")
	// Input and output file/dir options
	f.StringVar(&c.crFilepath, "custom-resource.path", "", "Path to a custom resource YAML file.")
	f.StringVar(&c.writeToDir, "output.write-dir", "", "write each file to the specified directory.")
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
	if cfg.objectStorage.S3.AccessKeyID == "" {
		log.Info("-object-storage.s3.access.key.id flag is required")
		os.Exit(1)
	}
	if cfg.objectStorage.S3.AccessKeySecret == "" {
		log.Info("-object-storage.s3.access.key.secret flag is required")
		os.Exit(1)
	}
	// Validate feature flags
	if cfg.featureFlags.EnablePrometheusAlerts && !cfg.featureFlags.EnableServiceMonitors {
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

	b, err := ioutil.ReadFile(cfg.crFilepath)
	if err != nil {
		logger.Info("failed to read custom resource file", "path", cfg.crFilepath)
		os.Exit(1)
	}

	ls := &v1beta1.LokiStack{}
	if err = yaml.Unmarshal(b, ls); err != nil {
		logger.Error(err, "failed to unmarshal LokiStack CR", "path", cfg.crFilepath)
		os.Exit(1)
	}

	// Convert config to manifest.Options
	opts := manifests.Options{
		Name:          cfg.Name,
		Namespace:     cfg.Namespace,
		Image:         cfg.Image,
		Stack:         ls.Spec,
		Flags:         cfg.featureFlags,
		ObjectStorage: cfg.objectStorage,
	}

	if optErr := manifests.ApplyDefaultSettings(&opts); optErr != nil {
		logger.Error(optErr, "failed to conform options to build settings")
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
			if err := ioutil.WriteFile(fname, b, 0o644); err != nil {
				logger.Error(err, "failed to write file to directory", "path", fname)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stdout, "---\n%s", b)
		}
	}
}
