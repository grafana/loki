package loki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	"github.com/grafana/dskit/tenant"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/util/build"
	"github.com/grafana/loki/v3/pkg/validation"
)

func yamlMarshalUnmarshal(in interface{}) (map[interface{}]interface{}, error) {
	yamlBytes, err := yaml.Marshal(in)
	if err != nil {
		return nil, err
	}

	object := make(map[interface{}]interface{})
	if err := yaml.Unmarshal(yamlBytes, object); err != nil {
		return nil, err
	}

	return object, nil
}

func diffConfig(defaultConfig, actualConfig map[interface{}]interface{}) (map[interface{}]interface{}, error) {
	output := make(map[interface{}]interface{})

	for key, value := range actualConfig {

		defaultValue, ok := defaultConfig[key]
		if !ok {
			output[key] = value
			continue
		}

		switch v := value.(type) {
		case int:
			defaultV, ok := defaultValue.(int)
			if !ok || defaultV != v {
				output[key] = v
			}
		case string:
			defaultV, ok := defaultValue.(string)
			if !ok || defaultV != v {
				output[key] = v
			}
		case bool:
			defaultV, ok := defaultValue.(bool)
			if !ok || defaultV != v {
				output[key] = v
			}
		case []interface{}:
			defaultV, ok := defaultValue.([]interface{})
			if !ok || !reflect.DeepEqual(defaultV, v) {
				output[key] = v
			}
		case float64:
			defaultV, ok := defaultValue.(float64)
			if !ok || !reflect.DeepEqual(defaultV, v) {
				output[key] = v
			}
		case map[interface{}]interface{}:
			defaultV, ok := defaultValue.(map[interface{}]interface{})
			if !ok {
				output[key] = value
			}
			diff, err := diffConfig(defaultV, v)
			if err != nil {
				return nil, err
			}
			if len(diff) > 0 {
				output[key] = diff
			}
		default:
			return nil, fmt.Errorf("unsupported type %T", v)
		}
	}

	return output, nil
}

func configHandler(actualCfg any, defaultCfg any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var output any
		switch r.URL.Query().Get("mode") {
		case "diff":
			defaultCfgObj, err := yamlMarshalUnmarshal(defaultCfg)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			actualCfgObj, err := yamlMarshalUnmarshal(actualCfg)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			diff, err := diffConfig(defaultCfgObj, actualCfgObj)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			output = diff

		case "defaults":
			output = defaultCfg
		default:
			output = actualCfg
		}

		writeYAMLResponse(w, output)
	}
}

func filterLimitFields(limits any, allowlist []string) (map[string]any, error) {
	// Convert limits to map via JSON marshaling to get proper field names
	// This avoids YAML conversion and gives us the JSON field names directly
	jsonBytes, err := json.Marshal(limits)
	if err != nil {
		return nil, err
	}

	var limitsMap map[string]any
	if err := json.Unmarshal(jsonBytes, &limitsMap); err != nil {
		return nil, err
	}

	// If no allowlist, return all fields
	if len(allowlist) == 0 {
		return limitsMap, nil
	}

	// Create allowlist set for O(1) lookup
	allowSet := make(map[string]bool)
	for _, field := range allowlist {
		allowSet[field] = true
	}

	// Filter to only allowed fields
	filtered := make(map[string]any)
	for key, value := range limitsMap {
		if allowSet[key] {
			filtered[key] = value
		}
	}

	return filtered, nil
}

func (t *Loki) tenantLimitsHandler(forDrilldown bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _, err := tenant.ExtractTenantIDFromHTTPRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Get tenant limits or defaults
		var limit *validation.Limits
		if t.TenantLimits != nil {
			limit = t.TenantLimits.TenantLimits(user)
		}
		if limit == nil && t.Overrides != nil {
			// There is no limit for this tenant, so we default to the default limits.
			limit = t.Overrides.DefaultLimits()
		}
		if limit == nil {
			// This should not happen, but we handle it gracefully.
			http.Error(w, "No default limits configured", http.StatusNotFound)
			return
		}

		// Apply allowlist filtering if configured
		allowlist := t.Cfg.TenantLimitsAllowPublish
		filteredLimits, err := filterLimitFields(limit, allowlist)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if !forDrilldown {
			writeYAMLResponse(w, filteredLimits)
			return
		}

		// Build response
		version := build.GetVersion().Version
		if version == "" {
			version = "unknown"
		}
		response := DrilldownConfigResponse{
			Limits:                 filteredLimits,
			PatternIngesterEnabled: t.Cfg.Pattern.Enabled,
			Version:                version,
		}

		// Return JSON response
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// writeYAMLResponse writes some YAML as a HTTP response.
func writeYAMLResponse(w http.ResponseWriter, v any) {
	// There is not standardised content-type for YAML, text/plain ensures the
	// YAML is displayed in the browser instead of offered as a download
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	data, err := yaml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// We ignore errors here, because we cannot do anything about them.
	// Write will trigger sending Status code, so we cannot send a different status code afterwards.
	// Also this isn't internal error, but error communicating with client.
	_, _ = w.Write(data)
}
