package lexers

import (
	. "github.com/alecthomas/chroma/v2" // nolint
)

// YAML+Jinja is YAML with Jinja templating embedded. Used by Ansible playbooks
// and Salt SLS files.
var YAMLJinja = Register(DelegatingLexer(
	MustNewXMLLexer(embedded, "embedded/yaml.xml"),
	MustNewXMLLexer(embedded, "embedded/django_jinja.xml").SetConfig(
		&Config{
			Name:      "YAML+Jinja",
			Aliases:   []string{"yaml+jinja", "salt", "sls", "ansible"},
			Filenames: []string{"*.sls"},
			MimeTypes: []string{"text/x-yaml+jinja", "text/x-sls"},
			DotAll:    true,
		},
	),
))
