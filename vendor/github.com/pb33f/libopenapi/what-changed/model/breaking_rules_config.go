// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"go.yaml.in/yaml/v4"
)

// BreakingRulesConfig holds all breaking change rules organized by OpenAPI component.
// Structure mirrors the OpenAPI 3.x specification.
type BreakingRulesConfig struct {
	OpenAPI             *BreakingChangeRule       `json:"openapi,omitempty" yaml:"openapi,omitempty"`
	JSONSchemaDialect   *BreakingChangeRule       `json:"jsonSchemaDialect,omitempty" yaml:"jsonSchemaDialect,omitempty"`
	Self                *BreakingChangeRule       `json:"$self,omitempty" yaml:"$self,omitempty"`
	Components          *BreakingChangeRule       `json:"components,omitempty" yaml:"components,omitempty"`
	Info                *InfoRules                `json:"info,omitempty" yaml:"info,omitempty"`
	Contact             *ContactRules             `json:"contact,omitempty" yaml:"contact,omitempty"`
	License             *LicenseRules             `json:"license,omitempty" yaml:"license,omitempty"`
	Paths               *PathsRules               `json:"paths,omitempty" yaml:"paths,omitempty"`
	PathItem            *PathItemRules            `json:"pathItem,omitempty" yaml:"pathItem,omitempty"`
	Operation           *OperationRules           `json:"operation,omitempty" yaml:"operation,omitempty"`
	Parameter           *ParameterRules           `json:"parameter,omitempty" yaml:"parameter,omitempty"`
	RequestBody         *RequestBodyRules         `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses           *ResponsesRules           `json:"responses,omitempty" yaml:"responses,omitempty"`
	Response            *ResponseRules            `json:"response,omitempty" yaml:"response,omitempty"`
	MediaType           *MediaTypeRules           `json:"mediaType,omitempty" yaml:"mediaType,omitempty"`
	Encoding            *EncodingRules            `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	Header              *HeaderRules              `json:"header,omitempty" yaml:"header,omitempty"`
	Schema              *SchemaRules              `json:"schema,omitempty" yaml:"schema,omitempty"`
	Schemas             *BreakingChangeRule       `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	Servers             *BreakingChangeRule       `json:"servers,omitempty" yaml:"servers,omitempty"`
	Discriminator       *DiscriminatorRules       `json:"discriminator,omitempty" yaml:"discriminator,omitempty"`
	XML                 *XMLRules                 `json:"xml,omitempty" yaml:"xml,omitempty"`
	Server              *ServerRules              `json:"server,omitempty" yaml:"server,omitempty"`
	ServerVariable      *ServerVariableRules      `json:"serverVariable,omitempty" yaml:"serverVariable,omitempty"`
	Tags                *BreakingChangeRule       `json:"tags,omitempty" yaml:"tags,omitempty"`
	Tag                 *TagRules                 `json:"tag,omitempty" yaml:"tag,omitempty"`
	Security            *BreakingChangeRule       `json:"security,omitempty" yaml:"security,omitempty"`
	ExternalDocs        *ExternalDocsRules        `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	SecurityScheme      *SecuritySchemeRules      `json:"securityScheme,omitempty" yaml:"securityScheme,omitempty"`
	SecurityRequirement *SecurityRequirementRules `json:"securityRequirement,omitempty" yaml:"securityRequirement,omitempty"`
	OAuthFlows          *OAuthFlowsRules          `json:"oauthFlows,omitempty" yaml:"oauthFlows,omitempty"`
	OAuthFlow           *OAuthFlowRules           `json:"oauthFlow,omitempty" yaml:"oauthFlow,omitempty"`
	Callback            *CallbackRules            `json:"callback,omitempty" yaml:"callback,omitempty"`
	Link                *LinkRules                `json:"link,omitempty" yaml:"link,omitempty"`
	Example             *ExampleRules             `json:"example,omitempty" yaml:"example,omitempty"`

	ruleCache map[string]*BreakingChangeRule
	cacheOnce sync.Once
}

// Merge applies user overrides to the configuration. Only non-nil values from
// the override config replace the current values. Uses reflection to reduce boilerplate.
func (c *BreakingRulesConfig) Merge(override *BreakingRulesConfig) {
	if override == nil {
		return
	}

	cVal := reflect.ValueOf(c).Elem()
	oVal := reflect.ValueOf(override).Elem()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		cField := cVal.Field(i)
		oField := oVal.Field(i)

		if !cField.CanSet() {
			continue
		}

		if field.Type == ruleType {
			cField.Set(reflect.ValueOf(mergeRule(
				cField.Interface().(*BreakingChangeRule),
				oField.Interface().(*BreakingChangeRule),
			)))
			continue
		}

		if field.Type.Kind() == reflect.Ptr && field.Type.Elem().Kind() == reflect.Struct {
			if oField.IsNil() {
				continue
			}
			if cField.IsNil() {
				cField.Set(reflect.New(field.Type.Elem()))
			}
			mergeRulesStruct(cField.Elem(), oField.Elem())
		}
	}

	c.invalidateCache()
}

// IsBreaking looks up whether a change is breaking based on the component, property, and change type.
// Returns the configured breaking status, or false if the rule is not found.
func (c *BreakingRulesConfig) IsBreaking(component, property, changeType string) bool {
	rule := c.GetRule(component, property)
	if rule == nil {
		return false
	}

	switch changeType {
	case ChangeTypeAdded:
		if rule.Added != nil {
			return *rule.Added
		}
	case ChangeTypeModified:
		if rule.Modified != nil {
			return *rule.Modified
		}
	case ChangeTypeRemoved:
		if rule.Removed != nil {
			return *rule.Removed
		}
	}
	return false
}

// GetRule returns the BreakingChangeRule for a given component and property.
// Returns nil if no rule is defined. Uses internal cache for O(1) lookups.
func (c *BreakingRulesConfig) GetRule(component, property string) *BreakingChangeRule {
	c.cacheOnce.Do(func() {
		c.ruleCache = c.buildRuleCache()
	})
	if property == "" {
		return c.ruleCache[component]
	}
	return c.ruleCache[component+"."+property]
}

// buildRuleCache creates a flat map of all rules for O(1) lookups using reflection.
func (c *BreakingRulesConfig) buildRuleCache() map[string]*BreakingChangeRule {
	cache := make(map[string]*BreakingChangeRule, 200)
	cVal := reflect.ValueOf(c).Elem()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		fVal := cVal.Field(i)

		compName := jsonTagName(field)
		if compName == "" || !fVal.CanInterface() {
			continue
		}

		if field.Type == ruleType {
			cache[compName] = fVal.Interface().(*BreakingChangeRule)
			continue
		}

		if field.Type.Kind() == reflect.Ptr && field.Type.Elem().Kind() == reflect.Struct {
			if fVal.IsNil() {
				continue
			}
			addRulesToCache(cache, compName, fVal.Elem())
		}
	}
	return cache
}

// invalidateCache resets the cache so it will be rebuilt on next access.
func (c *BreakingRulesConfig) invalidateCache() {
	c.cacheOnce = sync.Once{}
	c.ruleCache = nil
}

// --- Config Validation ---

// ConfigValidationError represents a single validation issue in a breaking rules config.
type ConfigValidationError struct {
	// Message is a human-readable description of the issue.
	Message string

	// Path is the YAML path where the issue was found (e.g., "schema.discriminator").
	Path string

	// Line is the 1-based line number in the YAML source (0 if unknown).
	Line int

	// Column is the 1-based column number in the YAML source (0 if unknown).
	Column int

	// FoundKey is the misplaced key that was detected.
	FoundKey string

	// SuggestedPath is where the key should be placed instead.
	SuggestedPath string
}

// Error implements the error interface.
func (e *ConfigValidationError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s (line %d, column %d)", e.Message, e.Line, e.Column)
	}
	return e.Message
}

// ConfigValidationResult holds the results of validating a breaking rules config.
type ConfigValidationResult struct {
	// Errors contains all validation issues found.
	Errors []*ConfigValidationError
}

// HasErrors returns true if any validation errors were found.
func (r *ConfigValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// Error implements the error interface, joining all errors.
func (r *ConfigValidationResult) Error() string {
	if !r.HasErrors() {
		return ""
	}
	msgs := make([]string, len(r.Errors))
	for i, e := range r.Errors {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// validTopLevelComponents is the set of valid top-level keys in a breaking rules config.
// Built from BreakingRulesConfig struct field tags at init time.
var validTopLevelComponents = buildValidComponentSet()

// buildValidComponentSet creates a set of valid top-level component names
// by reflecting on the BreakingRulesConfig struct tags.
func buildValidComponentSet() map[string]bool {
	result := make(map[string]bool)
	t := reflect.TypeOf(BreakingRulesConfig{})
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := jsonTagName(field)
		if name != "" && name != "-" {
			result[name] = true
		}
	}
	return result
}

// ValidateBreakingRulesConfigYAML validates raw YAML bytes for a breaking rules config.
// It detects misplaced nested configurations (e.g., "schema.discriminator" should be
// just "discriminator" at the top level) and returns all validation errors found.
// Returns nil if the configuration is valid.
func ValidateBreakingRulesConfigYAML(yamlBytes []byte) *ConfigValidationResult {
	var rootNode yaml.Node
	if err := yaml.Unmarshal(yamlBytes, &rootNode); err != nil {
		return &ConfigValidationResult{
			Errors: []*ConfigValidationError{{
				Message: fmt.Sprintf("invalid YAML: %v", err),
			}},
		}
	}

	result := &ConfigValidationResult{}
	validateConfigNode(&rootNode, "", result)

	if result.HasErrors() {
		return result
	}
	return nil
}

// breakingRuleFields are the valid fields for BreakingChangeRule
var breakingRuleFields = map[string]bool{
	"added":    true,
	"modified": true,
	"removed":  true,
}

// simpleRuleComponents are components that are directly BreakingChangeRule (not nested)
// For these, added/modified/removed at depth 1 is correct (e.g., "openapi.modified: false")
var simpleRuleComponents = map[string]bool{
	"openapi":           true,
	"jsonSchemaDialect": true,
	"$self":             true,
	"components":        true,
	"schemas":           true,
	"servers":           true,
	"tags":              true,
	"security":          true,
}

// componentProperties maps each component to its valid property names
// Built from reflection on BreakingRulesConfig struct
var componentProperties = buildComponentPropertiesMap()

func buildComponentPropertiesMap() map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	t := reflect.TypeOf(BreakingRulesConfig{})
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		componentName := jsonTagName(field)
		if componentName == "" || componentName == "-" {
			continue
		}
		// Get the field type (pointer to rules struct)
		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}
		// Build property set for this component
		props := make(map[string]bool)
		for j := 0; j < fieldType.NumField(); j++ {
			propField := fieldType.Field(j)
			propName := jsonTagName(propField)
			if propName != "" && propName != "-" {
				props[propName] = true
			}
		}
		result[componentName] = props
	}
	return result
}

// isValidPropertyForComponent checks if a property is valid for the given component
func isValidPropertyForComponent(component, property string) bool {
	if props, ok := componentProperties[component]; ok {
		return props[property]
	}
	return false
}

// validateConfigNode recursively walks the YAML tree looking for misplaced configurations.
func validateConfigNode(node *yaml.Node, path string, result *ConfigValidationResult) {
	validateConfigNodeWithDepth(node, path, 0, "", result)
}

// validateConfigNodeWithDepth recursively walks the YAML tree with depth tracking.
// depth 0 = root, depth 1 = under a component (e.g., "paths"), depth 2 = under a property (e.g., "paths.path")
// parentComponent tracks the root-level component we're under (e.g., "paths", "schema", "openapi")
// parentProperty tracks the property we're under within that component (e.g., "path" under "paths")
func validateConfigNodeWithDepth(node *yaml.Node, path string, depth int, parentComponent string, result *ConfigValidationResult) {
	validateConfigNodeWithDepthAndProperty(node, path, depth, parentComponent, "", result)
}

// validateConfigNodeWithDepthAndProperty is the internal recursive validator.
func validateConfigNodeWithDepthAndProperty(node *yaml.Node, path string, depth int, parentComponent, parentProperty string, result *ConfigValidationResult) {
	// Document nodes contain a single content node
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		validateConfigNodeWithDepthAndProperty(node.Content[0], path, depth, parentComponent, parentProperty, result)
		return
	}

	// Only process mapping nodes (objects)
	if node.Kind != yaml.MappingNode {
		return
	}

	// Process key-value pairs in the mapping
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		if keyNode.Kind != yaml.ScalarNode {
			continue
		}

		key := keyNode.Value
		currentPath := buildConfigPath(path, key)

		// Track the parent component when we enter a top-level component
		currentParent := parentComponent
		currentProperty := parentProperty
		if depth == 0 && validTopLevelComponents[key] {
			currentParent = key
			currentProperty = ""
		} else if depth == 1 && parentComponent != "" {
			// At depth 1, the key is a property name under a component
			currentProperty = key
		}

		// If we're already nested under a component and find another top-level component name,
		// this is a misplacement error UNLESS the key is a valid property of the parent component.
		// For example, "parameter.example" is valid because ParameterRules has an Example property,
		// even though "example" is also a top-level component.
		if path != "" && validTopLevelComponents[key] && !isValidPropertyForComponent(parentComponent, key) {
			result.Errors = append(result.Errors, &ConfigValidationError{
				Message:       fmt.Sprintf("found '%s' nested under '%s'; '%s' should be a top-level key", key, path, key),
				Path:          currentPath,
				Line:          keyNode.Line,
				Column:        keyNode.Column,
				FoundKey:      key,
				SuggestedPath: key,
			})
		}

		// Check for breaking rule fields (added/modified/removed) at wrong depth
		// depth 1 = directly under a component (e.g., "paths.added" is wrong)
		// These should only appear at depth 2 (e.g., "paths.path.added" is correct)
		// Exception: "simple" components like openapi, schemas, servers are directly BreakingChangeRule
		// so "openapi.modified: false" is correct
		if depth == 1 && breakingRuleFields[key] && !simpleRuleComponents[parentComponent] {
			// Extract the component name from the path
			componentName := path
			result.Errors = append(result.Errors, &ConfigValidationError{
				Message:       fmt.Sprintf("'%s' found directly under '%s'; breaking rules must be nested under a property name (e.g., '%s.path.%s' not '%s.%s')", key, componentName, componentName, key, componentName, key),
				Path:          currentPath,
				Line:          keyNode.Line,
				Column:        keyNode.Column,
				FoundKey:      key,
				SuggestedPath: fmt.Sprintf("%s.<property>.%s", componentName, key),
			})
		}

		// At depth 2+, check if we're trying to add invalid keys under a BreakingChangeRule.
		// A BreakingChangeRule (like schema.discriminator) can only have added/modified/removed.
		// If we find anything else (like "propertyName"), it's someone trying to configure
		// a component's sub-rules under the wrong parent.
		//
		// Only check this if:
		// 1. The parentProperty is also a valid top-level component name (like "discriminator")
		// 2. The key is a property of that top-level component (like "propertyName" is a property of DiscriminatorRules)
		// 3. The key is NOT a breaking rule field (added/modified/removed)
		//
		// This catches cases like schema.discriminator.propertyName where:
		// - schema.discriminator is valid (SchemaRules has Discriminator property)
		// - BUT propertyName under it is wrong (should be discriminator.propertyName at top level)
		if depth >= 2 && parentProperty != "" && !breakingRuleFields[key] && validTopLevelComponents[parentProperty] {
			// Check if this key is a property of the top-level component with the same name as parentProperty
			if props, ok := componentProperties[parentProperty]; ok && props[key] {
				result.Errors = append(result.Errors, &ConfigValidationError{
					Message:       fmt.Sprintf("'%s' is incorrectly nested under '%s'; move your '%s:' block to the top level of your config (current: %s.%s.%s, should be: %s.%s)", parentProperty, parentComponent, parentProperty, parentComponent, parentProperty, key, parentProperty, key),
					Path:          currentPath,
					Line:          keyNode.Line,
					Column:        keyNode.Column,
					FoundKey:      parentProperty,
					SuggestedPath: parentProperty,
				})
			}
		}

		// Recurse into nested mappings
		if valueNode.Kind == yaml.MappingNode {
			validateConfigNodeWithDepthAndProperty(valueNode, currentPath, depth+1, currentParent, currentProperty, result)
		}
	}
}

// buildConfigPath constructs a dotted path from parent and child components.
func buildConfigPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
