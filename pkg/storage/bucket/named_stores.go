package bucket

import (
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/storage/bucket/azure"
	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
	"github.com/grafana/loki/v3/pkg/storage/bucket/swift"

	"github.com/grafana/dskit/flagext"
)

// NamedStores helps configure additional object stores from a given storage provider
type NamedStores struct {
	Azure      map[string]NamedAzureStorageConfig      `yaml:"azure"`
	Filesystem map[string]NamedFilesystemStorageConfig `yaml:"filesystem"`
	GCS        map[string]NamedGCSStorageConfig        `yaml:"gcs"`
	S3         map[string]NamedS3StorageConfig         `yaml:"s3"`
	Swift      map[string]NamedSwiftStorageConfig      `yaml:"swift"`

	// contains mapping from named store reference name to store type
	storeType map[string]string `yaml:"-"`
}

func (ns *NamedStores) Validate() error {
	for name, s3Cfg := range ns.S3 {
		if err := s3Cfg.Validate(); err != nil {
			return fmt.Errorf("invalid S3 Storage config with name %s: %w", name, err)
		}
	}

	return ns.populateStoreType()
}

func (ns *NamedStores) populateStoreType() error {
	ns.storeType = make(map[string]string)

	checkForDuplicates := func(name string) error {
		if slices.Contains(SupportedBackends, name) {
			return fmt.Errorf("named store %q should not match with the name of a predefined storage type", name)
		}

		if st, ok := ns.storeType[name]; ok {
			return fmt.Errorf("named store %q is already defined under %s", name, st)
		}

		return nil
	}

	for name := range ns.S3 {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = S3
	}

	for name := range ns.Azure {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = Azure
	}

	for name := range ns.Filesystem {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = Filesystem
	}

	for name := range ns.GCS {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = GCS
	}

	for name := range ns.Swift {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = Swift
	}

	return nil
}

func (ns *NamedStores) LookupStoreType(name string) (string, bool) {
	st, ok := ns.storeType[name]
	return st, ok
}

func (ns *NamedStores) Exists(name string) bool {
	_, ok := ns.storeType[name]
	return ok
}

// OverrideConfig overrides the store config with the named store config
func (ns *NamedStores) OverrideConfig(storeCfg *Config, namedStore string) error {
	storeType, ok := ns.LookupStoreType(namedStore)
	if !ok {
		return fmt.Errorf("Unrecognized named storage config %s", namedStore)
	}

	switch storeType {
	case GCS:
		nsCfg, ok := ns.GCS[namedStore]
		if !ok {
			return fmt.Errorf("Unrecognized named gcs storage config %s", namedStore)
		}

		storeCfg.GCS = (gcs.Config)(nsCfg)
	case S3:
		nsCfg, ok := ns.S3[namedStore]
		if !ok {
			return fmt.Errorf("Unrecognized named s3 storage config %s", namedStore)
		}

		storeCfg.S3 = (s3.Config)(nsCfg)
	case Filesystem:
		nsCfg, ok := ns.Filesystem[namedStore]
		if !ok {
			return fmt.Errorf("Unrecognized named filesystem storage config %s", namedStore)
		}

		storeCfg.Filesystem = (filesystem.Config)(nsCfg)
	case Azure:
		nsCfg, ok := ns.Azure[namedStore]
		if !ok {
			return fmt.Errorf("Unrecognized named azure storage config %s", namedStore)
		}

		storeCfg.Azure = (azure.Config)(nsCfg)
	case Swift:
		nsCfg, ok := ns.Swift[namedStore]
		if !ok {
			return fmt.Errorf("Unrecognized named swift storage config %s", namedStore)
		}

		storeCfg.Swift = (swift.Config)(nsCfg)
	default:
		return fmt.Errorf("Unrecognized named storage type: %s", storeType)
	}

	return nil
}

// Storage configs defined as Named stores don't get any defaults as they do not
// register flags. To get around this we implement Unmarshaler interface that
// assigns the defaults before calling unmarshal.

// We cannot implement Unmarshaler directly on s3.Config or other stores
// as it would end up overriding values set as part of ApplyDynamicConfig().
// Note: we unmarshal a second time after applying dynamic configs
//
// Implementing the Unmarshaler for Named*StorageConfig types is fine as
// we do not apply any dynamic config on them.

type NamedS3StorageConfig s3.Config

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedS3StorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*s3.Config)(cfg))
	return unmarshal((*s3.Config)(cfg))
}

func (cfg *NamedS3StorageConfig) Validate() error {
	return (*s3.Config)(cfg).Validate()
}

type NamedGCSStorageConfig gcs.Config

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedGCSStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*gcs.Config)(cfg))
	return unmarshal((*gcs.Config)(cfg))
}

type NamedAzureStorageConfig azure.Config

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedAzureStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*azure.Config)(cfg))
	return unmarshal((*azure.Config)(cfg))
}

type NamedSwiftStorageConfig swift.Config

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedSwiftStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*swift.Config)(cfg))
	return unmarshal((*swift.Config)(cfg))
}

type NamedFilesystemStorageConfig filesystem.Config

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedFilesystemStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*filesystem.Config)(cfg))
	return unmarshal((*filesystem.Config)(cfg))
}
