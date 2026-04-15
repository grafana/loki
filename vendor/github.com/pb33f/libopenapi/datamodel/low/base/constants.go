// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

// Constants for labels used to look up values within OpenAPI specifications.
const (
	VersionLabel               = "version"
	TermsOfServiceLabel        = "termsOfService"
	DescriptionLabel           = "description"
	TitleLabel                 = "title"
	EmailLabel                 = "email"
	NameLabel                  = "name"
	SummaryLabel               = "summary"
	URLLabel                   = "url"
	ServersLabel               = "servers"
	ServerLabel                = "server"
	TagsLabel                  = "tags"
	ParentLabel                = "parent"
	KindLabel                  = "kind"
	ExternalDocsLabel          = "externalDocs"
	ExamplesLabel              = "examples"
	ExampleLabel               = "example"
	ValueLabel                 = "value"
	DataValueLabel             = "dataValue"       // OpenAPI 3.2+ dataValue field
	SerializedValueLabel       = "serializedValue" // OpenAPI 3.2+ serializedValue field
	InfoLabel                  = "info"
	ContactLabel               = "contact"
	LicenseLabel               = "license"
	PropertiesLabel            = "properties"
	DependentSchemasLabel      = "dependentSchemas"
	DependentRequiredLabel     = "dependentRequired"
	PatternPropertiesLabel     = "patternProperties"
	IfLabel                    = "if"
	ElseLabel                  = "else"
	ThenLabel                  = "then"
	PropertyNamesLabel         = "propertyNames"
	UnevaluatedItemsLabel      = "unevaluatedItems"
	UnevaluatedPropertiesLabel = "unevaluatedProperties"
	AdditionalPropertiesLabel  = "additionalProperties"
	XMLLabel                   = "xml"
	NodeTypeLabel              = "nodeType"
	ItemsLabel                 = "items"
	PrefixItemsLabel           = "prefixItems"
	ContainsLabel              = "contains"
	AllOfLabel                 = "allOf"
	AnyOfLabel                 = "anyOf"
	OneOfLabel                 = "oneOf"
	NotLabel                   = "not"
	TypeLabel                  = "type"
	DiscriminatorLabel         = "discriminator"
	DefaultMappingLabel        = "defaultMapping"
	MappingLabel               = "mapping"
	PropertyNameLabel          = "propertyName"
	ExclusiveMinimumLabel      = "exclusiveMinimum"
	ExclusiveMaximumLabel      = "exclusiveMaximum"
	SchemaLabel                = "schema"
	SchemaTypeLabel            = "$schema"
	IdLabel                    = "$id"
	AnchorLabel                = "$anchor"
	DynamicAnchorLabel         = "$dynamicAnchor"
	DynamicRefLabel            = "$dynamicRef"
	CommentLabel               = "$comment"
	ContentSchemaLabel         = "contentSchema"
	VocabularyLabel            = "$vocabulary"
)

/*
PropertyNames         low.NodeReference[*SchemaProxy]
			UnevaluatedItems      low.NodeReference[*SchemaProxy]
			UnevaluatedProperties low.NodeReference[*SchemaProxy]
*/
