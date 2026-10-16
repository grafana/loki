package runtimeconfig

import (
	"fmt"
	"slices"
	"strings"
)

// sourceParameter is a per-source parameter parsed from a LoadPath entry.
type sourceParameter string

const (
	parameterOptionalOnStartup              sourceParameter = "optional-on-startup"
	parameterOptionalKeepLastValueOnFailure sourceParameter = "optional-keep-last-value-on-failure"
)

var knownParameters = []sourceParameter{
	parameterOptionalOnStartup,
	parameterOptionalKeepLastValueOnFailure,
}

// sourceParameters are the parameters parsed from one LoadPath entry, in the order
// they were written. An empty list is the default: a failed read aborts the load.
type sourceParameters []sourceParameter

// toleratesFailure reports whether a failed read can be ignored for this parameter.
// initial is true for the load that the Manager performs while it starts.
func (p sourceParameter) toleratesFailure(initial bool) bool {
	switch p {
	case parameterOptionalOnStartup:
		return initial
	case parameterOptionalKeepLastValueOnFailure:
		return true
	default:
		return false
	}
}

// keepsLastValueOnFailure reports whether a tolerated failure keeps the bytes the source last supplied.
func (p sourceParameter) keepsLastValueOnFailure() bool {
	return p == parameterOptionalKeepLastValueOnFailure
}

// toleratesFailure reports whether a failed read can be ignored given these parameters.
func (ps sourceParameters) toleratesFailure(initial bool) bool {
	for _, p := range ps {
		if p.toleratesFailure(initial) {
			return true
		}
	}
	return false
}

// keepsLastValueOnFailure reports whether a tolerated failure keeps the bytes the source last supplied.
func (ps sourceParameters) keepsLastValueOnFailure() bool {
	return slices.ContainsFunc(ps, sourceParameter.keepsLastValueOnFailure)
}

// String returns the parameters in the ";"-separated form a LoadPath entry writes them in.
func (ps sourceParameters) String() string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = string(p)
	}
	return strings.Join(parts, ";")
}

// parseConfigSource splits one Config.LoadPath entry into a configSource's path
// and parameters. An entry can end with semicolon-separated parameters, for example:
//
//	/etc/overrides.yaml
//	http://config-server/overrides;optional-on-startup
//	http://config-server/overrides;optional-keep-last-value-on-failure
//
// Known parameters are peeled from the right, so several can be appended as
// ;parameter1;parameter2. Only these exact suffixes are parameters. Anything else after
// a ";" belongs to the path, so a URL parameter such as ;jsessionid=ABC or ;v2
// is left alone. The two parameters above contradict each other, so naming both
// is an error.
func parseConfigSource(entry string) (configSource, error) {
	path := entry
	var parameters sourceParameters
	for {
		rest, parameter, ok := cutParameter(path)
		if !ok {
			break
		}
		if rest == "" {
			return configSource{}, fmt.Errorf("runtime config source %q has no path", entry)
		}
		parameters = append(parameters, parameter)
		path = rest
	}
	// Collected from the right, so restore the order they were written.
	slices.Reverse(parameters)
	if err := checkParameters(entry, parameters); err != nil {
		return configSource{}, err
	}
	return configSource{path: path, parameters: parameters}, nil
}

// assignSourceIDs sets how every source identifies itself in metrics. A file is
// identified by its path, and a URL by the same without the userinfo, query, and
// fragment that can carry credentials.
//
// Two entries can come out of that identical, whether because they are the same file
// twice or because only a dropped part of a URL told them apart, and sources sharing a
// series overwrite each other's status. So an ID that repeats gets the index of its
// LoadPath entry appended, which is why this needs every source rather than one.
func assignSourceIDs(sources []configSource) error {
	occurrences := make(map[string]int, len(sources))
	for i := range sources {
		id := sources[i].path
		if isURL(id) {
			sanitized, err := sanitizeURLForMetrics(id)
			if err != nil {
				return err
			}
			id = sanitized
		}
		sources[i].sourceID = id
		occurrences[id]++
	}

	// A sanitized URL cannot contain "#", because String escapes it in a path and the
	// fragment is gone, so an appended suffix always tells two URLs apart. A file path
	// can contain one, so in theory it can already hold the suffix another entry is
	// about to be given. It takes a config as unlikely as
	// "/etc/o.yaml,/etc/o.yaml,/etc/o.yaml#0", where the first two are given
	// "/etc/o.yaml#0" and "/etc/o.yaml#1" while the third repeats nothing and so keeps
	// its path, which is what the first was just given. Refuse it rather than let the
	// two share a series, which is what the suffix is here to prevent.
	taken := make(map[string]int, len(sources))
	for i := range sources {
		if occurrences[sources[i].sourceID] > 1 {
			sources[i].sourceID = fmt.Sprintf("%s#%d", sources[i].sourceID, i)
		}
		if j, duplicate := taken[sources[i].sourceID]; duplicate {
			return fmt.Errorf(
				"runtime config sources %q and %q both report as %q in metrics, rename one of them",
				sources[j].path, sources[i].path, sources[i].sourceID,
			)
		}
		taken[sources[i].sourceID] = i
	}
	return nil
}

// checkParameters reports parameters that cannot be combined on one source.
func checkParameters(entry string, parameters sourceParameters) error {
	if len(parameters) > 1 {
		return fmt.Errorf(
			"runtime config source %q has more than one parameter, specify only one of %q and %q",
			entry, parameterOptionalOnStartup, parameterOptionalKeepLastValueOnFailure,
		)
	}
	return nil
}

// cutParameter removes a trailing parameter from entry and reports which it names.
func cutParameter(entry string) (path string, parameter sourceParameter, ok bool) {
	for _, p := range knownParameters {
		s := ";" + string(p)
		if strings.HasSuffix(entry, s) {
			return entry[:len(entry)-len(s)], p, true
		}
	}
	return entry, "", false
}
