package log

import (
	"flag"
	"fmt"
	"sync/atomic"

	"github.com/cloudflare/ahocorasick"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// Supported values of MultiLiteralFilterConfig.CaseSensitiveMatcher.
const (
	CaseSensitiveMatcherAhoCorasick = "aho-corasick"
	CaseSensitiveMatcherDFA         = "dfa"
)

// MultiLiteralFilterConfig controls whether an OR of literal line filters is tested in one pass
// over each line.
type MultiLiteralFilterConfig struct {
	Enabled              bool   `yaml:"enabled" category:"experimental"`
	CaseSensitiveMatcher string `yaml:"case_sensitive_matcher" category:"experimental"`
}

// RegisterFlagsWithPrefix registers the flags of cfg, with prefix before each name.
func (cfg *MultiLiteralFilterConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, `Test an OR of many literal line filters, such as |~ "a|b|c" or |= "a" or "b", in one pass over each line instead of one pass for each literal. It applies to the line filters that this process builds.`)
	f.StringVar(&cfg.CaseSensitiveMatcher, prefix+"case-sensitive-matcher", CaseSensitiveMatcherAhoCorasick, fmt.Sprintf("Matcher for case-sensitive literals. Supported values: %s, %s. The %s matcher is faster to scan but uses about 4 KB of memory for each byte of the literals. Case-insensitive literals always use %s.", CaseSensitiveMatcherAhoCorasick, CaseSensitiveMatcherDFA, CaseSensitiveMatcherAhoCorasick, CaseSensitiveMatcherDFA))
}

// Validate returns an error when cfg names an unknown matcher.
func (cfg *MultiLiteralFilterConfig) Validate() error {
	switch cfg.CaseSensitiveMatcher {
	case CaseSensitiveMatcherAhoCorasick, CaseSensitiveMatcherDFA:
		return nil
	default:
		return fmt.Errorf("unknown case-sensitive matcher %q: supported values are %s and %s", cfg.CaseSensitiveMatcher, CaseSensitiveMatcherAhoCorasick, CaseSensitiveMatcherDFA)
	}
}

// activeMerger holds the containsAnyMerger that mergeContainsAny uses. It is nil when the
// multi-literal filter is disabled.
var activeMerger atomic.Pointer[containsAnyMerger]

// SetMultiLiteralFilter sets how the line filters that this process builds after the call test an
// OR of literals. Filters built before the call keep their behavior.
//
// The setting applies to the whole process, because line filters are built without a config.
// SetMultiLiteralFilter is safe for concurrent use.
func SetMultiLiteralFilter(cfg MultiLiteralFilterConfig) error {
	if !cfg.Enabled {
		activeMerger.Store(nil)
		return nil
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	var caseSensitive caseSensitiveMatcher = ahoCorasickMatcher{}
	if cfg.CaseSensitiveMatcher == CaseSensitiveMatcherDFA {
		caseSensitive = dfaMatcher{}
	}
	activeMerger.Store(&containsAnyMerger{caseSensitive: caseSensitive})
	return nil
}

var multiLiteralFiltersBuilt = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: constants.Loki,
	Name:      "logql_multi_literal_filters_built_total",
	Help:      "Number of line filters that test an OR of literals in one pass, by matcher and case sensitivity.",
}, []string{"matcher", "case_sensitive"})

// caseSensitiveMatcher builds containsAnyFilters for case-sensitive literals with one kind of
// matcher.
type caseSensitiveMatcher interface {
	// fit reports whether filters are enough to merge and within the limits of the matcher.
	fit(filters []*containsFilter) bool

	// newFilter returns a containsAnyFilter for literals. Each literal must be non-empty.
	newFilter(literals [][]byte) containsAnyFilter

	name() string
}

// ahoCorasickMatcher uses the Aho-Corasick trie of github.com/cloudflare/ahocorasick.
type ahoCorasickMatcher struct{}

func (ahoCorasickMatcher) fit(filters []*containsFilter) bool {
	if len(filters) < minContainsAnyLiterals {
		return false
	}
	total := 0
	for _, f := range filters {
		if len(f.match) > maxContainsAnyLiteralLen {
			return false
		}
		total += len(f.match)
	}
	return total <= maxContainsAnyBytes
}

func (ahoCorasickMatcher) newFilter(literals [][]byte) containsAnyFilter {
	return containsAnyFilter{
		literals: literals,
		matcher:  ahocorasick.NewMatcher(literals),
	}
}

func (ahoCorasickMatcher) name() string { return CaseSensitiveMatcherAhoCorasick }

// dfaMatcher uses literalDFA.
type dfaMatcher struct{}

func (dfaMatcher) fit(filters []*containsFilter) bool {
	if len(filters) < minContainsAnyLiterals {
		return false
	}
	total := 0
	for _, f := range filters {
		total += len(f.match)
	}
	return total <= maxLiteralDFABytes
}

func (dfaMatcher) newFilter(literals [][]byte) containsAnyFilter {
	return containsAnyFilter{
		literals: literals,
		matcher:  newLiteralDFA(literals, false),
	}
}

func (dfaMatcher) name() string { return CaseSensitiveMatcherDFA }
