package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/ViaQ/logerr/log"
	"github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests"
	"sigs.k8s.io/yaml"
)

// Define the manifest options here as structured objects
type config struct {
	Name      string
	Namespace string
	Image     string

	featureFlags  manifests.FeatureFlags
	objectStorage manifests.ObjectStorage

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
	// Object storage options
	c.objectStorage = manifests.ObjectStorage{}
	f.StringVar(&c.objectStorage.Endpoint, "object-storage.endpoint", "", "The S3 endpoint location.")
	f.StringVar(&c.objectStorage.Buckets, "object-storage.buckets", "", "A comma-separated list of S3 buckets.")
	f.StringVar(&c.objectStorage.Region, "object-storage.region", "", "An S3 region.")
	f.StringVar(&c.objectStorage.AccessKeyID, "object-storage.access-key-id", "", "The access key id for S3.")
	f.StringVar(&c.objectStorage.AccessKeySecret, "object-storage.access-key-secret", "", "The access key secret for S3.")
	// Input and output file/dir options
	f.StringVar(&c.crFilepath, "custom-resource.path", "", "Path to a custom resource YAML file.")
	f.StringVar(&c.writeToDir, "output.write-dir", "", "write each file to the specified directory.")
}

func (c *config) validateFlags() {
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
	if cfg.objectStorage.Endpoint == "" {
		log.Info("-object.storage.endpoint flag is required")
		os.Exit(1)
	}
	if cfg.objectStorage.Buckets == "" {
		log.Info("-object.storage.buckets flag is required")
		os.Exit(1)
	}
	if cfg.objectStorage.AccessKeyID == "" {
		log.Info("-object.storage.access.key.id flag is required")
		os.Exit(1)
	}
	if cfg.objectStorage.AccessKeySecret == "" {
		log.Info("-object.storage.access.key.secret flag is required")
		os.Exit(1)
	}
}

var cfg *config

func init() {
	log.Init("loki-broker")
	cfg = &config{}
}

func main() {
	f := flag.NewFlagSet("", flag.ExitOnError)
	cfg.registerFlags(f)
	if err := f.Parse(os.Args[1:]); err != nil {
		log.Error(err, "failed to parse flags")
	}

	cfg.validateFlags()

	b, err := ioutil.ReadFile(cfg.crFilepath)
	if err != nil {
		log.Info("failed to read custom resource file", "path", cfg.crFilepath)
		os.Exit(1)
	}

	ls := &v1beta1.LokiStack{}
	if err = yaml.Unmarshal(b, ls); err != nil {
		log.Error(err, "failed to unmarshal LokiStack CR", "path", cfg.crFilepath)
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
		log.Error(optErr, "failed to conform options to build settings")
		os.Exit(1)
	}

	objects, err := manifests.BuildAll(opts)
	if err != nil {
		log.Error(err, "failed to build manifests")
		os.Exit(1)
	}

	for _, o := range objects {
		b, err := yaml.Marshal(o)
		if err != nil {
			log.Error(err, "failed to marshal manifest", "name", o.GetName(), "kind", o.GetObjectKind())
			continue
		}

		if cfg.writeToDir != "" {
			basename := fmt.Sprintf("%s-%s.yaml", o.GetObjectKind().GroupVersionKind().Kind, o.GetName())
			fname := strings.ToLower(path.Join(cfg.writeToDir, basename))
			if err := ioutil.WriteFile(fname, b, 0o644); err != nil {
				log.Error(err, "failed to write file to directory", "path", fname)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stdout, "---\n%s", b)
		}
	}
}
