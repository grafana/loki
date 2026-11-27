// Copyright 2026 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// AliasDataAPI is the anchor policy plugin API name.
	AliasDataAPI = "alias-data"
	// ErrorFormatAPI is the error-format plugin API name.
	ErrorFormatAPI = "error-format"
	// LoaderLimitsAPI is the loader-limits plugin API name.
	LoaderLimitsAPI = "loader-limits"
	// MergeKeyAPI is the Merge-Key plugin API name.
	MergeKeyAPI = "merge-key"
	// YAMLParserAPI is the YAML-parser plugin API name.
	YAMLParserAPI = "yaml-parser"
	// legacyLimitAPI exists for release-candidate compatibility.
	legacyLimitAPI = "limit"
)

// AliasDataDisabled recognizes the three explicit alias rejection forms.
func AliasDataDisabled(value any) bool {
	switch setting := value.(type) {
	case bool:
		return !setting
	case string:
		return setting == "disable"
	case map[string]any:
		disabled, _ := setting["disable"].(bool)
		return disabled
	}
	return false
}

// Selection identifies one plugin API, implementation, and optional version.
type Selection struct {
	API, Name, Version string
}

// CanonicalAPI returns the canonical name for a plugin API.
func CanonicalAPI(api string) string {
	if api == legacyLimitAPI {
		return LoaderLimitsAPI
	}
	return api
}

// CanonicalName returns the canonical implementation name for an API.
func CanonicalName(api, name string) string {
	if CanonicalAPI(api) == LoaderLimitsAPI && name == legacyLimitAPI {
		return LoaderLimitsAPI
	}
	return name
}

// NormalizeConfig replaces deprecated plugin API keys with canonical keys.
func NormalizeConfig(plugins map[string]any) error {
	value, legacy := plugins[legacyLimitAPI]
	if !legacy {
		return nil
	}
	if _, canonical := plugins[LoaderLimitsAPI]; canonical {
		return fmt.Errorf(
			"plugin APIs %q and %q cannot both be configured",
			legacyLimitAPI, LoaderLimitsAPI)
	}
	delete(plugins, legacyLimitAPI)
	plugins[LoaderLimitsAPI] = value
	return nil
}

// ParseSelectors parses comma-separated plugin selector syntax.
func ParseSelectors(text string) ([]Selection, error) {
	var selections []Selection
	for _, raw := range strings.Split(text, ",") {
		selection, err := parseSelector(raw)
		if err != nil {
			return nil, err
		}
		selections = append(selections, selection)
	}
	return selections, nil
}

func parseSelector(text string) (Selection, error) {
	spec := strings.TrimSpace(text)
	if spec == "" {
		return Selection{}, errors.New("plugin selector must not be empty")
	}
	if strings.Count(spec, "=") > 1 || strings.Count(spec, "@") > 1 {
		return Selection{}, fmt.Errorf("invalid plugin selector %q", spec)
	}
	api, implementation, found := strings.Cut(spec, "=")
	if !found {
		implementation = ""
	}
	name, version, versioned := strings.Cut(implementation, "@")
	if !found {
		api, version, versioned = strings.Cut(api, "@")
		name = ""
	}
	if api == "" || (found && name == "") ||
		(versioned && version == "") {
		return Selection{}, fmt.Errorf("invalid plugin selector %q", spec)
	}
	if versioned {
		if _, ok := normalizeVersion(version); !ok {
			return Selection{}, fmt.Errorf(
				"plugin selector %q version must be a release version",
				spec)
		}
	}
	api = CanonicalAPI(api)
	name = CanonicalName(api, name)
	return Selection{API: api, Name: name, Version: version}, nil
}

// ConfigValue converts an implementation short form into registry config.
func ConfigValue(api, value string) (map[string]any, error) {
	selection, err := parseSelector(api + "=" + value)
	if err != nil {
		return nil, err
	}
	config := map[string]any{"name": selection.Name}
	if selection.Version != "" {
		config["version"] = selection.Version
	}
	return config, nil
}
