package kingpin

import (
	"os"
	"regexp"
)

var (
	envVarValuesSeparator = "\r?\n"
	envVarValuesTrimmer   = regexp.MustCompile(envVarValuesSeparator + "$")
	envVarValuesSplitter  = regexp.MustCompile(envVarValuesSeparator)
)

type envarMixin struct {
	envar   string
	noEnvar bool
}

func (e *envarMixin) HasEnvarValue() bool {
	return e.GetEnvarValue() != ""
}

func (e *envarMixin) GetEnvarValue() string {
	if e.noEnvar || e.envar == "" {
		return ""
	}
	return os.Getenv(e.envar)
}

func (e *envarMixin) GetSplitEnvarValue() []string {
	envarValue := e.GetEnvarValue()
	if envarValue == "" {
		return []string{}
	}

	// Split by new line to extract multiple values, if any.
	trimmed := envVarValuesTrimmer.ReplaceAllString(envarValue, "")

	return envVarValuesSplitter.Split(trimmed, -1)
}
