// Copyright 2023-2024 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package model

type DocumentChangesFlat struct {
	*PropertyChanges
	InfoChanges                []*Change `json:"info,omitempty" yaml:"info,omitempty"`
	PathsChanges               []*Change `json:"paths,omitempty" yaml:"paths,omitempty"`
	TagChanges                 []*Change `json:"tags,omitempty" yaml:"tags,omitempty"`
	ExternalDocChanges         []*Change `json:"externalDoc,omitempty" yaml:"externalDoc,omitempty"`
	WebhookChanges             []*Change `json:"webhooks,omitempty" yaml:"webhooks,omitempty"`
	ServerChanges              []*Change `json:"servers,omitempty" yaml:"servers,omitempty"`
	SecurityRequirementChanges []*Change `json:"securityRequirements,omitempty" yaml:"securityRequirements,omitempty"`
	ComponentsChanges          []*Change `json:"components,omitempty" yaml:"components,omitempty"`
	ExtensionChanges           []*Change `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}
