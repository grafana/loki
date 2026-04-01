// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

// BreakingChangeRule holds the breaking status for a property's change types.
// nil values mean "use default" - only set values override the defaults.
type BreakingChangeRule struct {
	Added    *bool `json:"added,omitempty" yaml:"added,omitempty"`
	Modified *bool `json:"modified,omitempty" yaml:"modified,omitempty"`
	Removed  *bool `json:"removed,omitempty" yaml:"removed,omitempty"`
}

// InfoRules defines breaking rules for the Info object properties.
type InfoRules struct {
	Title          *BreakingChangeRule `json:"title,omitempty" yaml:"title,omitempty"`
	Summary        *BreakingChangeRule `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description    *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	TermsOfService *BreakingChangeRule `json:"termsOfService,omitempty" yaml:"termsOfService,omitempty"`
	Version        *BreakingChangeRule `json:"version,omitempty" yaml:"version,omitempty"`
	Contact        *BreakingChangeRule `json:"contact,omitempty" yaml:"contact,omitempty"`
	License        *BreakingChangeRule `json:"license,omitempty" yaml:"license,omitempty"`
}

// ContactRules defines breaking rules for the Contact object properties.
type ContactRules struct {
	URL   *BreakingChangeRule `json:"url,omitempty" yaml:"url,omitempty"`
	Name  *BreakingChangeRule `json:"name,omitempty" yaml:"name,omitempty"`
	Email *BreakingChangeRule `json:"email,omitempty" yaml:"email,omitempty"`
}

// LicenseRules defines breaking rules for the License object properties.
type LicenseRules struct {
	URL        *BreakingChangeRule `json:"url,omitempty" yaml:"url,omitempty"`
	Name       *BreakingChangeRule `json:"name,omitempty" yaml:"name,omitempty"`
	Identifier *BreakingChangeRule `json:"identifier,omitempty" yaml:"identifier,omitempty"`
}

// PathsRules defines breaking rules for the Paths object properties.
type PathsRules struct {
	Path *BreakingChangeRule `json:"path,omitempty" yaml:"path,omitempty"`
}

// PathItemRules defines breaking rules for the Path Item object properties.
type PathItemRules struct {
	Description          *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Summary              *BreakingChangeRule `json:"summary,omitempty" yaml:"summary,omitempty"`
	Get                  *BreakingChangeRule `json:"get,omitempty" yaml:"get,omitempty"`
	Put                  *BreakingChangeRule `json:"put,omitempty" yaml:"put,omitempty"`
	Post                 *BreakingChangeRule `json:"post,omitempty" yaml:"post,omitempty"`
	Delete               *BreakingChangeRule `json:"delete,omitempty" yaml:"delete,omitempty"`
	Options              *BreakingChangeRule `json:"options,omitempty" yaml:"options,omitempty"`
	Head                 *BreakingChangeRule `json:"head,omitempty" yaml:"head,omitempty"`
	Patch                *BreakingChangeRule `json:"patch,omitempty" yaml:"patch,omitempty"`
	Trace                *BreakingChangeRule `json:"trace,omitempty" yaml:"trace,omitempty"`
	Query                *BreakingChangeRule `json:"query,omitempty" yaml:"query,omitempty"`
	AdditionalOperations *BreakingChangeRule `json:"additionalOperations,omitempty" yaml:"additionalOperations,omitempty"`
	Servers              *BreakingChangeRule `json:"servers,omitempty" yaml:"servers,omitempty"`
	Parameters           *BreakingChangeRule `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// OperationRules defines breaking rules for the Operation object properties.
type OperationRules struct {
	Tags         *BreakingChangeRule `json:"tags,omitempty" yaml:"tags,omitempty"`
	Summary      *BreakingChangeRule `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description  *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Deprecated   *BreakingChangeRule `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	OperationID  *BreakingChangeRule `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	ExternalDocs *BreakingChangeRule `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Responses    *BreakingChangeRule `json:"responses,omitempty" yaml:"responses,omitempty"`
	Parameters   *BreakingChangeRule `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Security     *BreakingChangeRule `json:"security,omitempty" yaml:"security,omitempty"`
	RequestBody  *BreakingChangeRule `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Callbacks    *BreakingChangeRule `json:"callbacks,omitempty" yaml:"callbacks,omitempty"`
	Servers      *BreakingChangeRule `json:"servers,omitempty" yaml:"servers,omitempty"`
}

// ParameterRules defines breaking rules for the Parameter object properties.
type ParameterRules struct {
	Name            *BreakingChangeRule `json:"name,omitempty" yaml:"name,omitempty"`
	In              *BreakingChangeRule `json:"in,omitempty" yaml:"in,omitempty"`
	Description     *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Required        *BreakingChangeRule `json:"required,omitempty" yaml:"required,omitempty"`
	AllowEmptyValue *BreakingChangeRule `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Style           *BreakingChangeRule `json:"style,omitempty" yaml:"style,omitempty"`
	AllowReserved   *BreakingChangeRule `json:"allowReserved,omitempty" yaml:"allowReserved,omitempty"`
	Explode         *BreakingChangeRule `json:"explode,omitempty" yaml:"explode,omitempty"`
	Deprecated      *BreakingChangeRule `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Example         *BreakingChangeRule `json:"example,omitempty" yaml:"example,omitempty"`
	Schema          *BreakingChangeRule `json:"schema,omitempty" yaml:"schema,omitempty"`
	Items           *BreakingChangeRule `json:"items,omitempty" yaml:"items,omitempty"`
}

// RequestBodyRules defines breaking rules for the Request Body object properties.
type RequestBodyRules struct {
	Description *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Required    *BreakingChangeRule `json:"required,omitempty" yaml:"required,omitempty"`
}

// ResponsesRules defines breaking rules for the Responses object properties.
type ResponsesRules struct {
	Default *BreakingChangeRule `json:"default,omitempty" yaml:"default,omitempty"`
	Codes   *BreakingChangeRule `json:"codes,omitempty" yaml:"codes,omitempty"`
}

// ResponseRules defines breaking rules for the Response object properties.
type ResponseRules struct {
	Description *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Summary     *BreakingChangeRule `json:"summary,omitempty" yaml:"summary,omitempty"`
	Schema      *BreakingChangeRule `json:"schema,omitempty" yaml:"schema,omitempty"`
	Examples    *BreakingChangeRule `json:"examples,omitempty" yaml:"examples,omitempty"`
}

// MediaTypeRules defines breaking rules for the Media Type object properties.
type MediaTypeRules struct {
	Example      *BreakingChangeRule `json:"example,omitempty" yaml:"example,omitempty"`
	Schema       *BreakingChangeRule `json:"schema,omitempty" yaml:"schema,omitempty"`
	ItemSchema   *BreakingChangeRule `json:"itemSchema,omitempty" yaml:"itemSchema,omitempty"`
	ItemEncoding *BreakingChangeRule `json:"itemEncoding,omitempty" yaml:"itemEncoding,omitempty"`
}

// EncodingRules defines breaking rules for the Encoding object properties.
type EncodingRules struct {
	ContentType   *BreakingChangeRule `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	Style         *BreakingChangeRule `json:"style,omitempty" yaml:"style,omitempty"`
	Explode       *BreakingChangeRule `json:"explode,omitempty" yaml:"explode,omitempty"`
	AllowReserved *BreakingChangeRule `json:"allowReserved,omitempty" yaml:"allowReserved,omitempty"`
}

// HeaderRules defines breaking rules for the Header object properties.
type HeaderRules struct {
	Description     *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Style           *BreakingChangeRule `json:"style,omitempty" yaml:"style,omitempty"`
	AllowReserved   *BreakingChangeRule `json:"allowReserved,omitempty" yaml:"allowReserved,omitempty"`
	AllowEmptyValue *BreakingChangeRule `json:"allowEmptyValue,omitempty" yaml:"allowEmptyValue,omitempty"`
	Explode         *BreakingChangeRule `json:"explode,omitempty" yaml:"explode,omitempty"`
	Example         *BreakingChangeRule `json:"example,omitempty" yaml:"example,omitempty"`
	Deprecated      *BreakingChangeRule `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Required        *BreakingChangeRule `json:"required,omitempty" yaml:"required,omitempty"`
	Schema          *BreakingChangeRule `json:"schema,omitempty" yaml:"schema,omitempty"`
	Items           *BreakingChangeRule `json:"items,omitempty" yaml:"items,omitempty"`
}

// SchemaRules defines breaking rules for the Schema object properties.
type SchemaRules struct {
	Ref                   *BreakingChangeRule `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Type                  *BreakingChangeRule `json:"type,omitempty" yaml:"type,omitempty"`
	Title                 *BreakingChangeRule `json:"title,omitempty" yaml:"title,omitempty"`
	Description           *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Format                *BreakingChangeRule `json:"format,omitempty" yaml:"format,omitempty"`
	Maximum               *BreakingChangeRule `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	Minimum               *BreakingChangeRule `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	ExclusiveMaximum      *BreakingChangeRule `json:"exclusiveMaximum,omitempty" yaml:"exclusiveMaximum,omitempty"`
	ExclusiveMinimum      *BreakingChangeRule `json:"exclusiveMinimum,omitempty" yaml:"exclusiveMinimum,omitempty"`
	MaxLength             *BreakingChangeRule `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`
	MinLength             *BreakingChangeRule `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	Pattern               *BreakingChangeRule `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	MaxItems              *BreakingChangeRule `json:"maxItems,omitempty" yaml:"maxItems,omitempty"`
	MinItems              *BreakingChangeRule `json:"minItems,omitempty" yaml:"minItems,omitempty"`
	MaxProperties         *BreakingChangeRule `json:"maxProperties,omitempty" yaml:"maxProperties,omitempty"`
	MinProperties         *BreakingChangeRule `json:"minProperties,omitempty" yaml:"minProperties,omitempty"`
	UniqueItems           *BreakingChangeRule `json:"uniqueItems,omitempty" yaml:"uniqueItems,omitempty"`
	MultipleOf            *BreakingChangeRule `json:"multipleOf,omitempty" yaml:"multipleOf,omitempty"`
	ContentEncoding       *BreakingChangeRule `json:"contentEncoding,omitempty" yaml:"contentEncoding,omitempty"`
	ContentMediaType      *BreakingChangeRule `json:"contentMediaType,omitempty" yaml:"contentMediaType,omitempty"`
	Default               *BreakingChangeRule `json:"default,omitempty" yaml:"default,omitempty"`
	Const                 *BreakingChangeRule `json:"const,omitempty" yaml:"const,omitempty"`
	Nullable              *BreakingChangeRule `json:"nullable,omitempty" yaml:"nullable,omitempty"`
	ReadOnly              *BreakingChangeRule `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	WriteOnly             *BreakingChangeRule `json:"writeOnly,omitempty" yaml:"writeOnly,omitempty"`
	Deprecated            *BreakingChangeRule `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Example               *BreakingChangeRule `json:"example,omitempty" yaml:"example,omitempty"`
	Examples              *BreakingChangeRule `json:"examples,omitempty" yaml:"examples,omitempty"`
	Required              *BreakingChangeRule `json:"required,omitempty" yaml:"required,omitempty"`
	Enum                  *BreakingChangeRule `json:"enum,omitempty" yaml:"enum,omitempty"`
	Properties            *BreakingChangeRule `json:"properties,omitempty" yaml:"properties,omitempty"`
	AdditionalProperties  *BreakingChangeRule `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
	AllOf                 *BreakingChangeRule `json:"allOf,omitempty" yaml:"allOf,omitempty"`
	AnyOf                 *BreakingChangeRule `json:"anyOf,omitempty" yaml:"anyOf,omitempty"`
	OneOf                 *BreakingChangeRule `json:"oneOf,omitempty" yaml:"oneOf,omitempty"`
	PrefixItems           *BreakingChangeRule `json:"prefixItems,omitempty" yaml:"prefixItems,omitempty"`
	Items                 *BreakingChangeRule `json:"items,omitempty" yaml:"items,omitempty"`
	Discriminator         *BreakingChangeRule `json:"discriminator,omitempty" yaml:"discriminator,omitempty"`
	ExternalDocs          *BreakingChangeRule `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Not                   *BreakingChangeRule `json:"not,omitempty" yaml:"not,omitempty"`
	If                    *BreakingChangeRule `json:"if,omitempty" yaml:"if,omitempty"`
	Then                  *BreakingChangeRule `json:"then,omitempty" yaml:"then,omitempty"`
	Else                  *BreakingChangeRule `json:"else,omitempty" yaml:"else,omitempty"`
	PropertyNames         *BreakingChangeRule `json:"propertyNames,omitempty" yaml:"propertyNames,omitempty"`
	Contains              *BreakingChangeRule `json:"contains,omitempty" yaml:"contains,omitempty"`
	UnevaluatedItems      *BreakingChangeRule `json:"unevaluatedItems,omitempty" yaml:"unevaluatedItems,omitempty"`
	UnevaluatedProperties *BreakingChangeRule `json:"unevaluatedProperties,omitempty" yaml:"unevaluatedProperties,omitempty"`
	DynamicAnchor         *BreakingChangeRule `json:"$dynamicAnchor,omitempty" yaml:"$dynamicAnchor,omitempty"`
	DynamicRef            *BreakingChangeRule `json:"$dynamicRef,omitempty" yaml:"$dynamicRef,omitempty"`
	Id                    *BreakingChangeRule `json:"$id,omitempty" yaml:"$id,omitempty"`
	Comment               *BreakingChangeRule `json:"$comment,omitempty" yaml:"$comment,omitempty"`
	ContentSchema         *BreakingChangeRule `json:"contentSchema,omitempty" yaml:"contentSchema,omitempty"`
	Vocabulary            *BreakingChangeRule `json:"$vocabulary,omitempty" yaml:"$vocabulary,omitempty"`
	DependentRequired     *BreakingChangeRule `json:"dependentRequired,omitempty" yaml:"dependentRequired,omitempty"`
	XML                   *BreakingChangeRule `json:"xml,omitempty" yaml:"xml,omitempty"`
	SchemaDialect         *BreakingChangeRule `json:"schemaDialect,omitempty" yaml:"schemaDialect,omitempty"`
}

// DiscriminatorRules defines breaking rules for the Discriminator object properties.
type DiscriminatorRules struct {
	PropertyName   *BreakingChangeRule `json:"propertyName,omitempty" yaml:"propertyName,omitempty"`
	DefaultMapping *BreakingChangeRule `json:"defaultMapping,omitempty" yaml:"defaultMapping,omitempty"`
	Mapping        *BreakingChangeRule `json:"mapping,omitempty" yaml:"mapping,omitempty"`
}

// XMLRules defines breaking rules for the XML object properties.
type XMLRules struct {
	Name      *BreakingChangeRule `json:"name,omitempty" yaml:"name,omitempty"`
	Namespace *BreakingChangeRule `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Prefix    *BreakingChangeRule `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Attribute *BreakingChangeRule `json:"attribute,omitempty" yaml:"attribute,omitempty"`
	NodeType  *BreakingChangeRule `json:"nodeType,omitempty" yaml:"nodeType,omitempty"`
	Wrapped   *BreakingChangeRule `json:"wrapped,omitempty" yaml:"wrapped,omitempty"`
}

// ServerRules defines breaking rules for the Server object properties.
type ServerRules struct {
	Name        *BreakingChangeRule `json:"name,omitempty" yaml:"name,omitempty"`
	URL         *BreakingChangeRule `json:"url,omitempty" yaml:"url,omitempty"`
	Description *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
}

// ServerVariableRules defines breaking rules for the Server Variable object properties.
type ServerVariableRules struct {
	Enum        *BreakingChangeRule `json:"enum,omitempty" yaml:"enum,omitempty"`
	Default     *BreakingChangeRule `json:"default,omitempty" yaml:"default,omitempty"`
	Description *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
}

// TagRules defines breaking rules for the Tag object properties.
type TagRules struct {
	Name         *BreakingChangeRule `json:"name,omitempty" yaml:"name,omitempty"`
	Summary      *BreakingChangeRule `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description  *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Parent       *BreakingChangeRule `json:"parent,omitempty" yaml:"parent,omitempty"`
	Kind         *BreakingChangeRule `json:"kind,omitempty" yaml:"kind,omitempty"`
	ExternalDocs *BreakingChangeRule `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
}

// ExternalDocsRules defines breaking rules for the External Documentation object properties.
type ExternalDocsRules struct {
	URL         *BreakingChangeRule `json:"url,omitempty" yaml:"url,omitempty"`
	Description *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
}

// SecuritySchemeRules defines breaking rules for the Security Scheme object properties.
type SecuritySchemeRules struct {
	Type              *BreakingChangeRule `json:"type,omitempty" yaml:"type,omitempty"`
	Description       *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Name              *BreakingChangeRule `json:"name,omitempty" yaml:"name,omitempty"`
	In                *BreakingChangeRule `json:"in,omitempty" yaml:"in,omitempty"`
	Scheme            *BreakingChangeRule `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	BearerFormat      *BreakingChangeRule `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty"`
	OpenIDConnectURL  *BreakingChangeRule `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`
	OAuth2MetadataUrl *BreakingChangeRule `json:"oauth2MetadataUrl,omitempty" yaml:"oauth2MetadataUrl,omitempty"`
	Flows             *BreakingChangeRule `json:"flows,omitempty" yaml:"flows,omitempty"`
	Scopes            *BreakingChangeRule `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Flow              *BreakingChangeRule `json:"flow,omitempty" yaml:"flow,omitempty"`                         // Swagger 2.0
	AuthorizationURL  *BreakingChangeRule `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"` // Swagger 2.0
	TokenURL          *BreakingChangeRule `json:"tokenUrl,omitempty" yaml:"tokenUrl,omitempty"`                 // Swagger 2.0
	Deprecated        *BreakingChangeRule `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
}

// SecurityRequirementRules defines breaking rules for the Security Requirement object properties.
type SecurityRequirementRules struct {
	Schemes *BreakingChangeRule `json:"schemes,omitempty" yaml:"schemes,omitempty"`
	Scopes  *BreakingChangeRule `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// OAuthFlowsRules defines breaking rules for the OAuth Flows object properties.
type OAuthFlowsRules struct {
	Implicit          *BreakingChangeRule `json:"implicit,omitempty" yaml:"implicit,omitempty"`
	Password          *BreakingChangeRule `json:"password,omitempty" yaml:"password,omitempty"`
	ClientCredentials *BreakingChangeRule `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *BreakingChangeRule `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
	Device            *BreakingChangeRule `json:"device,omitempty" yaml:"device,omitempty"`
}

// OAuthFlowRules defines breaking rules for the OAuth Flow object properties.
type OAuthFlowRules struct {
	AuthorizationURL *BreakingChangeRule `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenURL         *BreakingChangeRule `json:"tokenUrl,omitempty" yaml:"tokenUrl,omitempty"`
	RefreshURL       *BreakingChangeRule `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty"`
	Scopes           *BreakingChangeRule `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// CallbackRules defines breaking rules for the Callback object properties.
type CallbackRules struct {
	Expressions *BreakingChangeRule `json:"expressions,omitempty" yaml:"expressions,omitempty"`
}

// LinkRules defines breaking rules for the Link object properties.
type LinkRules struct {
	OperationRef *BreakingChangeRule `json:"operationRef,omitempty" yaml:"operationRef,omitempty"`
	OperationID  *BreakingChangeRule `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	RequestBody  *BreakingChangeRule `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Description  *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Server       *BreakingChangeRule `json:"server,omitempty" yaml:"server,omitempty"`
	Parameters   *BreakingChangeRule `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// ExampleRules defines breaking rules for the Example object properties.
type ExampleRules struct {
	Summary         *BreakingChangeRule `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description     *BreakingChangeRule `json:"description,omitempty" yaml:"description,omitempty"`
	Value           *BreakingChangeRule `json:"value,omitempty" yaml:"value,omitempty"`
	ExternalValue   *BreakingChangeRule `json:"externalValue,omitempty" yaml:"externalValue,omitempty"`
	DataValue       *BreakingChangeRule `json:"dataValue,omitempty" yaml:"dataValue,omitempty"`
	SerializedValue *BreakingChangeRule `json:"serializedValue,omitempty" yaml:"serializedValue,omitempty"`
}
