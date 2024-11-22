// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[RegexMap] = (*RegexMapBuilder)(nil)

// Maps regular expressions to replacement text and a color.
// For example, if a value is www.example.com, you can configure a regex value mapping so that Grafana displays www and truncates the domain.
type RegexMapBuilder struct {
	internal *RegexMap
	errors   map[string]cog.BuildErrors
}

func NewRegexMapBuilder() *RegexMapBuilder {
	resource := &RegexMap{}
	builder := &RegexMapBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Type = "regex"

	return builder
}

func (builder *RegexMapBuilder) Build() (RegexMap, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("RegexMap", err)...)
	}

	if len(errs) != 0 {
		return RegexMap{}, errs
	}

	return *builder.internal, nil
}

// Regular expression to match against and the result to apply when the value matches the regex
func (builder *RegexMapBuilder) Options(options cog.Builder[DashboardRegexMapOptions]) *RegexMapBuilder {
	optionsResource, err := options.Build()
	if err != nil {
		builder.errors["options"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Options = optionsResource

	return builder
}

func (builder *RegexMapBuilder) applyDefaults() {
}
