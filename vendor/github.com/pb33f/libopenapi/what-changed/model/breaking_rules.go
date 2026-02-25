// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"
	"strings"
	"sync"
)

// ResetDefaultBreakingRules resets the cached default rules. This is primarily
// intended for testing scenarios where the cache needs to be cleared.
func ResetDefaultBreakingRules() {
	defaultRulesOnce = sync.Once{}
	defaultRulesCache = nil
}

// SetActiveBreakingRulesConfig sets the active breaking rules configuration used
// by comparison functions. Pass nil to reset to defaults.
func SetActiveBreakingRulesConfig(config *BreakingRulesConfig) {
	activeConfigMu.Lock()
	defer activeConfigMu.Unlock()
	activeConfig = config
}

// GetActiveBreakingRulesConfig returns the currently active breaking rules config.
// If no custom config has been set, returns the default rules.
func GetActiveBreakingRulesConfig() *BreakingRulesConfig {
	activeConfigMu.RLock()
	defer activeConfigMu.RUnlock()
	if activeConfig != nil {
		return activeConfig
	}
	return GenerateDefaultBreakingRules()
}

// ResetActiveBreakingRulesConfig clears any custom config and reverts to defaults.
func ResetActiveBreakingRulesConfig() {
	activeConfigMu.Lock()
	defer activeConfigMu.Unlock()
	activeConfig = nil
}

// GenerateDefaultBreakingRules returns the default breaking change rules for OpenAPI 3.x.
// These rules match the currently hardcoded behavior in the comparison functions.
// The returned config is cached and reused for performance.
func GenerateDefaultBreakingRules() *BreakingRulesConfig {
	defaultRulesOnce.Do(func() {
		defaultRulesCache = buildDefaultRules()
	})
	return defaultRulesCache
}

// IsBreakingChange is a package-level helper that looks up whether a change is breaking
// using the currently active configuration.
func IsBreakingChange(component, property, changeType string) bool {
	return GetActiveBreakingRulesConfig().IsBreaking(component, property, changeType)
}

// BreakingAdded returns whether adding the specified property is a breaking change.
func BreakingAdded(component, property string) bool {
	return IsBreakingChange(component, property, ChangeTypeAdded)
}

// BreakingModified returns whether modifying the specified property is a breaking change.
func BreakingModified(component, property string) bool {
	return IsBreakingChange(component, property, ChangeTypeModified)
}

// BreakingRemoved returns whether removing the specified property is a breaking change.
func BreakingRemoved(component, property string) bool {
	return IsBreakingChange(component, property, ChangeTypeRemoved)
}

func boolPtr(b bool) *bool {
	return &b
}

func rule(added, modified, removed bool) *BreakingChangeRule {
	return &BreakingChangeRule{
		Added:    boolPtr(added),
		Modified: boolPtr(modified),
		Removed:  boolPtr(removed),
	}
}

// jsonTagName extracts the field name from a JSON struct tag.
func jsonTagName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" || tag == "-" {
		return ""
	}
	if idx := strings.Index(tag, ","); idx != -1 {
		return tag[:idx]
	}
	return tag
}

// mergeRulesStruct merges all *BreakingChangeRule fields from override into base.
func mergeRulesStruct(base, override reflect.Value) {
	rulesType := base.Type()
	for i := 0; i < rulesType.NumField(); i++ {
		field := rulesType.Field(i)
		if field.Type != ruleType {
			continue
		}
		bField := base.Field(i)
		oField := override.Field(i)
		if bField.CanSet() {
			bField.Set(reflect.ValueOf(mergeRule(
				bField.Interface().(*BreakingChangeRule),
				oField.Interface().(*BreakingChangeRule),
			)))
		}
	}
}

// addRulesToCache adds all *BreakingChangeRule fields from a rule struct to the cache.
func addRulesToCache(cache map[string]*BreakingChangeRule, compName string, rulesVal reflect.Value) {
	rulesType := rulesVal.Type()
	for i := 0; i < rulesType.NumField(); i++ {
		field := rulesType.Field(i)
		fVal := rulesVal.Field(i)

		propName := jsonTagName(field)
		if propName == "" || field.Type != ruleType {
			continue
		}
		cache[compName+"."+propName] = fVal.Interface().(*BreakingChangeRule)
	}
}

// mergeRule merges an override rule into a base rule.
// nil values in override are ignored, non-nil values replace the base.
func mergeRule(base, override *BreakingChangeRule) *BreakingChangeRule {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}
	result := &BreakingChangeRule{
		Added:    base.Added,
		Modified: base.Modified,
		Removed:  base.Removed,
	}
	if override.Added != nil {
		result.Added = override.Added
	}
	if override.Modified != nil {
		result.Modified = override.Modified
	}
	if override.Removed != nil {
		result.Removed = override.Removed
	}
	return result
}

// buildDefaultRules creates the actual default rules configuration.
func buildDefaultRules() *BreakingRulesConfig {
	return &BreakingRulesConfig{
		OpenAPI:           rule(true, true, true),
		JSONSchemaDialect: rule(true, true, true),
		Self:              rule(true, true, true),
		Components:        rule(false, false, true),

		Info: &InfoRules{
			Title:          rule(false, false, false),
			Summary:        rule(false, false, false),
			Description:    rule(false, false, false),
			TermsOfService: rule(false, false, false),
			Version:        rule(false, false, false),
			Contact:        rule(false, false, false),
			License:        rule(false, false, false),
		},

		Contact: &ContactRules{
			URL:   rule(false, false, false),
			Name:  rule(false, false, false),
			Email: rule(false, false, false),
		},

		License: &LicenseRules{
			URL:        rule(false, false, false),
			Name:       rule(false, false, false),
			Identifier: rule(false, false, false),
		},

		Paths: &PathsRules{
			Path: rule(false, false, true),
		},

		PathItem: &PathItemRules{
			Description:          rule(false, false, false),
			Summary:              rule(false, false, false),
			Get:                  rule(false, false, true),
			Put:                  rule(false, false, true),
			Post:                 rule(false, false, true),
			Delete:               rule(false, false, true),
			Options:              rule(false, false, true),
			Head:                 rule(false, false, true),
			Patch:                rule(false, false, true),
			Trace:                rule(false, false, true),
			Query:                rule(false, false, true),
			AdditionalOperations: rule(false, false, true),
			Servers:              rule(false, false, true),
			Parameters:           rule(false, false, true),
		},

		Operation: &OperationRules{
			Tags:         rule(false, false, true),
			Summary:      rule(false, false, false),
			Description:  rule(false, false, false),
			Deprecated:   rule(false, false, false),
			OperationID:  rule(true, true, true),
			ExternalDocs: rule(false, false, false),
			Responses:    rule(false, false, true),
			Parameters:   rule(false, false, true),
			Security:     rule(false, false, true),
			RequestBody:  rule(true, false, true),
			Callbacks:    rule(false, false, true),
			Servers:      rule(false, false, true),
		},

		Parameter: &ParameterRules{
			Name:            rule(true, true, true),
			In:              rule(true, true, true),
			Description:     rule(false, false, false),
			Required:        rule(false, true, false),
			AllowEmptyValue: rule(true, true, true),
			Style:           rule(false, false, false),
			AllowReserved:   rule(true, true, true),
			Explode:         rule(false, false, false),
			Deprecated:      rule(false, false, false),
			Example:         rule(false, false, false),
			Schema:          rule(true, false, true),
			Items:           rule(true, false, true),
		},

		RequestBody: &RequestBodyRules{
			Description: rule(false, false, false),
			Required:    rule(true, true, true),
		},

		Responses: &ResponsesRules{
			Default: rule(false, false, true),
			Codes:   rule(false, false, true),
		},

		Response: &ResponseRules{
			Description: rule(false, false, false),
			Summary:     rule(false, false, false),
			Schema:      rule(true, false, true),
			Examples:    rule(false, false, false),
		},

		MediaType: &MediaTypeRules{
			Example:      rule(false, false, false),
			Schema:       rule(true, false, true),
			ItemSchema:   rule(true, false, true),
			ItemEncoding: rule(false, false, true),
		},

		Encoding: &EncodingRules{
			ContentType:   rule(true, true, true),
			Style:         rule(true, true, true),
			Explode:       rule(true, true, true),
			AllowReserved: rule(false, false, false),
		},

		Header: &HeaderRules{
			Description:     rule(false, false, false),
			Style:           rule(false, false, false),
			AllowReserved:   rule(false, false, false),
			AllowEmptyValue: rule(true, true, true),
			Explode:         rule(false, false, false),
			Example:         rule(false, false, false),
			Deprecated:      rule(false, false, false),
			Required:        rule(true, true, true),
			Schema:          rule(true, false, true),
			Items:           rule(true, false, true),
		},

		Schemas: rule(true, false, true),
		Servers: rule(false, false, true),

		Schema: &SchemaRules{
			Ref:                   rule(false, false, false),
			Type:                  rule(false, true, false),
			Title:                 rule(false, false, false),
			Description:           rule(false, false, false),
			Format:                rule(false, true, false),
			Maximum:               rule(false, true, false),
			Minimum:               rule(false, true, false),
			ExclusiveMaximum:      rule(false, true, false),
			ExclusiveMinimum:      rule(false, true, false),
			MaxLength:             rule(false, true, false),
			MinLength:             rule(false, true, false),
			Pattern:               rule(false, true, false),
			MaxItems:              rule(false, true, false),
			MinItems:              rule(false, true, false),
			MaxProperties:         rule(false, true, false),
			MinProperties:         rule(false, true, false),
			UniqueItems:           rule(false, true, false),
			MultipleOf:            rule(false, true, false),
			ContentEncoding:       rule(false, true, false),
			ContentMediaType:      rule(false, true, false),
			Default:               rule(false, true, false),
			Const:                 rule(false, true, false),
			Nullable:              rule(false, true, false),
			ReadOnly:              rule(false, true, false),
			WriteOnly:             rule(false, true, false),
			Deprecated:            rule(false, false, false),
			Example:               rule(false, false, false),
			Examples:              rule(false, false, false),
			Required:              rule(true, false, true),
			Enum:                  rule(false, false, true),
			Properties:            rule(false, false, true),
			AdditionalProperties:  rule(true, true, true),
			AllOf:                 rule(false, false, true),
			AnyOf:                 rule(false, false, true),
			OneOf:                 rule(false, false, true),
			PrefixItems:           rule(false, false, true),
			Items:                 rule(true, true, true),
			Discriminator:         rule(true, false, true),
			ExternalDocs:          rule(false, false, false),
			Not:                   rule(true, false, true),
			If:                    rule(true, false, true),
			Then:                  rule(true, false, true),
			Else:                  rule(true, false, true),
			PropertyNames:         rule(true, false, true),
			Contains:              rule(true, false, true),
			UnevaluatedItems:      rule(true, false, true),
			UnevaluatedProperties: rule(true, true, true),
			DynamicAnchor:         rule(false, true, true),   // $dynamicAnchor: modification/removal is breaking
			DynamicRef:            rule(false, true, true),   // $dynamicRef: modification/removal is breaking
			Id:                    rule(true, true, true),    // $id: all changes are breaking (affects reference resolution)
			Comment:               rule(false, false, false), // $comment: does not affect API contracts
			ContentSchema:         rule(true, true, true),    // contentSchema: affects content validation
			Vocabulary:            rule(true, true, true),    // $vocabulary: affects schema interpretation
			DependentRequired:     rule(false, true, true),
			XML:                   rule(false, false, true),
			SchemaDialect:         rule(true, true, true),
		},

		Discriminator: &DiscriminatorRules{
			PropertyName:   rule(true, true, true),
			DefaultMapping: rule(true, true, true),
			Mapping:        rule(false, true, true),
		},

		XML: &XMLRules{
			Name:      rule(true, true, true),
			Namespace: rule(true, true, true),
			Prefix:    rule(true, true, true),
			Attribute: rule(true, true, true),
			NodeType:  rule(true, true, true),
			Wrapped:   rule(true, true, true),
		},

		Server: &ServerRules{
			Name:        rule(true, true, true),
			URL:         rule(true, true, true),
			Description: rule(false, false, false),
		},

		ServerVariable: &ServerVariableRules{
			Enum:        rule(false, false, true),
			Default:     rule(true, true, true),
			Description: rule(false, false, false),
		},

		Tags: rule(false, false, true),

		Security: rule(false, false, true),

		Tag: &TagRules{
			Name:         rule(false, true, true),
			Summary:      rule(false, false, false),
			Description:  rule(false, false, false),
			Parent:       rule(true, true, true),
			Kind:         rule(false, false, false),
			ExternalDocs: rule(false, false, false),
		},

		ExternalDocs: &ExternalDocsRules{
			URL:         rule(false, false, false),
			Description: rule(false, false, false),
		},

		SecurityScheme: &SecuritySchemeRules{
			Type:              rule(true, true, true),
			Description:       rule(false, false, false),
			Name:              rule(true, true, true),
			In:                rule(true, true, true),
			Scheme:            rule(true, true, true),
			BearerFormat:      rule(false, false, false),
			OpenIDConnectURL:  rule(false, false, false),
			OAuth2MetadataUrl: rule(false, false, false),
			Flows:             rule(false, false, true),
			Scopes:            rule(false, false, true),
			Flow:              rule(true, true, true), // Swagger 2.0
			AuthorizationURL:  rule(true, true, true), // Swagger 2.0
			TokenURL:          rule(true, true, true), // Swagger 2.0
			Deprecated:        rule(false, false, false),
		},

		SecurityRequirement: &SecurityRequirementRules{
			Schemes: rule(false, false, true),
			Scopes:  rule(false, false, true),
		},

		OAuthFlows: &OAuthFlowsRules{
			Implicit:          rule(false, false, true),
			Password:          rule(false, false, true),
			ClientCredentials: rule(false, false, true),
			AuthorizationCode: rule(false, false, true),
			Device:            rule(false, false, true),
		},

		OAuthFlow: &OAuthFlowRules{
			AuthorizationURL: rule(true, true, true),
			TokenURL:         rule(true, true, true),
			RefreshURL:       rule(true, true, true),
			Scopes:           rule(false, true, true),
		},

		Callback: &CallbackRules{
			Expressions: rule(false, false, true),
		},

		Link: &LinkRules{
			OperationRef: rule(true, true, true),
			OperationID:  rule(true, true, true),
			RequestBody:  rule(true, true, true),
			Description:  rule(false, false, false),
			Server:       rule(true, false, true),
			Parameters:   rule(true, true, true),
		},

		Example: &ExampleRules{
			Summary:         rule(false, false, false),
			Description:     rule(false, false, false),
			Value:           rule(false, false, false),
			ExternalValue:   rule(false, false, false),
			DataValue:       rule(false, false, false),
			SerializedValue: rule(false, false, false),
		},
	}
}
