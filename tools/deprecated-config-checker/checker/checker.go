package checker

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	deprecatedValuesField = "_deprecated"
	messageField          = "_msg"

	defaultDeprecatesFilePath = "tools/deprecated-config-checker/deprecated-config.yaml"
	defaultDeletesFilePath    = "tools/deprecated-config-checker/deleted-config.yaml"

	configRequiredErrorMsg = "config.file or runtime-config.file are required"
)

type Config struct {
	DeprecatesFile    string
	DeletesFile       string
	ConfigFile        string
	RuntimeConfigFile string
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.DeprecatesFile, "deprecates-file", defaultDeprecatesFilePath, "YAML file with deprecated configs")
	f.StringVar(&c.DeletesFile, "deletes-file", defaultDeletesFilePath, "YAML file with deleted configs")
	f.StringVar(&c.ConfigFile, "config.file", "", "User-defined config file to validate")
	f.StringVar(&c.RuntimeConfigFile, "runtime-config.file", "", "User-defined runtime config file to validate")
}

func (c *Config) Validate() error {
	if c.ConfigFile == "" && c.RuntimeConfigFile == "" {
		return fmt.Errorf("%s", configRequiredErrorMsg)
	}
	return nil
}

type RawYaml map[string]interface{}

type Checker struct {
	config        RawYaml
	runtimeConfig RawYaml

	deprecates RawYaml
	deletes    RawYaml
}

func loadYAMLFile(path string) (RawYaml, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var out RawYaml
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func NewChecker(cfg Config) (*Checker, error) {
	deprecates, err := loadYAMLFile(cfg.DeprecatesFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read deprecates YAML: %w", err)
	}

	deletes, err := loadYAMLFile(cfg.DeletesFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read deletes YAML: %w", err)
	}

	var config RawYaml
	if cfg.ConfigFile != "" {
		config, err = loadYAMLFile(cfg.ConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config YAML: %w", err)
		}
	}

	var runtimeConfig RawYaml
	if cfg.RuntimeConfigFile != "" {
		runtimeConfig, err = loadYAMLFile(cfg.RuntimeConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read runtime config YAML: %w", err)
		}
	}

	return &Checker{
		config:        config,
		runtimeConfig: runtimeConfig,
		deprecates:    deprecates,
		deletes:       deletes,
	}, nil
}

func (c *Checker) CheckConfigDeprecated() []DeprecationNotes {
	return checkConfigDeprecated(c.deprecates, c.config)
}

func (c *Checker) CheckConfigDeleted() []DeprecationNotes {
	return checkConfigDeprecated(c.deletes, c.config)
}

func (c *Checker) CheckRuntimeConfigDeprecated() []DeprecationNotes {
	return checkRuntimeConfigDeprecated(c.deprecates, c.runtimeConfig)
}

func (c *Checker) CheckRuntimeConfigDeleted() []DeprecationNotes {
	return checkRuntimeConfigDeprecated(c.deletes, c.runtimeConfig)
}

type deprecationAnnotation struct {
	DeprecatedValues []string
	Msg              string
}

func getDeprecationAnnotation(value interface{}) (deprecationAnnotation, bool) {
	// If the value is a string, return it as the message
	if msg, is := value.(string); is {
		return deprecationAnnotation{
			Msg: msg,
		}, true
	}

	// If the value is a map, check if it has a _msg field
	if inner, is := value.(RawYaml); is {
		msg, exists := inner[messageField]
		if !exists {
			return deprecationAnnotation{}, false
		}

		var deprecatedValues []string
		if v, exists := inner[deprecatedValuesField]; exists {
			asIfcSlice := v.([]interface{})
			deprecatedValues = make([]string, len(asIfcSlice))
			for i, v := range asIfcSlice {
				deprecatedValues[i] = v.(string)
			}
		}

		return deprecationAnnotation{
			Msg:              msg.(string),
			DeprecatedValues: deprecatedValues,
		}, true
	}

	return deprecationAnnotation{}, false
}

type DeprecationNotes struct {
	deprecationAnnotation
	ItemPath   string
	ItemValues []string
}

func (d DeprecationNotes) String() string {
	var sb strings.Builder

	sb.WriteString(d.ItemPath)
	if len(d.ItemValues) > 0 {
		sb.WriteString(" = ")
		if len(d.ItemValues) == 1 {
			sb.WriteString(d.ItemValues[0])
		} else {
			sb.WriteString("[")
			sb.WriteString(strings.Join(d.ItemValues, ", "))
			sb.WriteString("]")
		}
	}
	sb.WriteString(": " + d.Msg)
	if len(d.DeprecatedValues) > 0 {
		sb.WriteString("\n\t|- " + "Deprecated values: ")
		sb.WriteString(strings.Join(d.DeprecatedValues, ", "))
	}

	return sb.String()
}

func appenToPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func getLimitsConfig(in RawYaml) RawYaml {
	limits, exists := in["limits_config"]
	if !exists {
		return nil
	}
	return limits.(RawYaml)
}

func getOverrides(runtimeConf RawYaml) RawYaml {
	overrides, exists := runtimeConf["overrides"]
	if !exists {
		return nil
	}
	return overrides.(RawYaml)
}

func checkRuntimeConfigDeprecated(deprecates, runtimeConfig RawYaml) []DeprecationNotes {
	deprecatedLimits := getLimitsConfig(deprecates)
	if deprecatedLimits == nil {
		return nil
	}

	overrides := getOverrides(runtimeConfig)
	if overrides == nil {
		return nil
	}

	// We check the deprecated fields for each tenant
	var deprecations []DeprecationNotes
	for tenant, tenantOverrides := range overrides {
		tenantDeprecations := checkConfigDeprecated(deprecatedLimits, tenantOverrides.(RawYaml))
		for i := range tenantDeprecations {
			tenantPath := appenToPath("overrides", tenant)
			tenantDeprecations[i].ItemPath = appenToPath(tenantPath, tenantDeprecations[i].ItemPath)
		}
		deprecations = append(deprecations, tenantDeprecations...)
	}

	return deprecations
}

func checkConfigDeprecated(deprecates, config RawYaml) []DeprecationNotes {
	return enumerateDeprecatesFields(deprecates, config, "", []DeprecationNotes{})
}

func enumerateDeprecatesFields(deprecates, input RawYaml, rootPath string, deprecations []DeprecationNotes) []DeprecationNotes {
	for key, deprecate := range deprecates {
		inputValue, exists := input[key]
		if !exists {
			// If this item is not set in the config, we can skip it.
			continue
		}

		path := appenToPath(rootPath, key)

		note, isDeprecatedNote := getDeprecationAnnotation(deprecate)
		if isDeprecatedNote {
			var inputValueStrSlice []string
			switch v := inputValue.(type) {
			case []interface{}:
				for _, val := range v {
					inputValueStrSlice = append(inputValueStrSlice, val.(string))
				}
			case string:
				inputValueStrSlice = []string{v}
			case int, int32, int64, uint, uint32, uint64, float32, float64:
				inputValueStrSlice = []string{fmt.Sprintf("%d", v)}
			case bool:
				inputValueStrSlice = []string{fmt.Sprintf("%t", v)}
			}

			// If there are no specific values deprecated, the whole config is deprecated.
			// Otherwise, look for the config value in the list of deprecated values.
			var inputDeprecated bool
			if len(note.DeprecatedValues) == 0 {
				inputDeprecated = true
			} else {
				// If the config is a list, check each item.
			FindDeprecatedValues:
				for _, v := range note.DeprecatedValues {
					for _, itemValueStr := range inputValueStrSlice {
						if v == itemValueStr {
							inputDeprecated = true
							break FindDeprecatedValues
						}
					}
				}
			}

			if inputDeprecated {
				deprecations = append(deprecations, DeprecationNotes{
					deprecationAnnotation: note,
					ItemPath:              path,
					ItemValues:            inputValueStrSlice,
				})
				continue
			}
		}

		// To this point, the deprecate item is not a leaf, so we need to recurse into it.
		if deprecateYaml, is := deprecate.(RawYaml); is {
			switch v := inputValue.(type) {
			case RawYaml:
				deprecations = enumerateDeprecatesFields(deprecateYaml, v, path, deprecations)
			case []interface{}:
				// If the config is a list, recurse into each item.
				for i, item := range v {
					itemYaml := item.(RawYaml)
					deprecations = enumerateDeprecatesFields(deprecateYaml, itemYaml, appenToPath(path, fmt.Sprintf("[%d]", i)), deprecations)
				}
			}
		}
	}

	return deprecations
}
