// Package collisiongen generates testdata/label_collisions_parsers.logqltest. The script runs the
// label collision cases for every parser, for every source of a name, and for both key orders. A
// model of the rules computes the expected labels (see model.go), so the engine is tested against
// something that does not come from the engine.
//
// To change the script, change the model or the cases here, and run:
//
//	go test ./pkg/logql/internal/logqltest/collisiongen -update
package collisiongen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// headerLines is the comment at the top of the generated script.
var headerLines = []string{
	"# This file is generated. Do not edit it by hand. Change the model or the cases in the",
	"# `collisiongen` package, and run `go test ./pkg/logql/internal/logqltest/collisiongen -update`.",
	"#",
	"# The label collision rules for every parser. `label_collisions.logqltest` has the cases one by one,",
	"# mostly for json. This file runs them for json, logfmt, regexp, pattern, unpack, and the expression",
	"# forms `json foo, bar` (named jsonx here) and `logfmt foo, bar` (named logfmtx). The model in",
	"# `collisiongen/model.go` computes the expected values, not the engine. It follows the rules at",
	"# the top of `label_collisions.logqltest`.",
	"#",
	"# There is one section for each parser. Its streams are named `<parser>_<s><m><o>`:",
	"#",
	"# - `s` is the set of stream labels: 0 none, 1 `foo`, 2 `foo_extracted`, 3 both.",
	"# - `m` is the set of structured metadata keys: 0 none, 1 `foo`, 2 `foo_extracted`, 3 both.",
	"# - `o` is the key order of the line, which has the keys `foo`, `foo_extracted` and `bar`: `a` has",
	"#   the renamed key `foo` first, and `b` has the literal key `foo_extracted` first.",
	"#",
	"# The same streams go through a label filter, grouped metric queries, `drop foo` and",
	"# `drop foo_extracted` before the parser, and `line_format` after it. The single cases",
	"# (`<parser>_c_<case>`) have a key that the line lacks, an empty value, a null, or a duplicate key,",
	"# for the parsers that can write them.",
	"#",
	"# The sections at the end run two parsers in a row, for every pair, with `line_format` between them.",
	"#",
	"# Every parser gives the same labels for the same sources of a name. The key order does not change",
	"# the result either.",
	"#",
}

var (
	// gridIDs are the names that an expression form asks for in the grid and the pairs.
	gridIDs   = []string{"foo", "foo_extracted", "bar"}
	singleIDs = []string{"foo", "foo_extracted", "bar", "other"}
	pairIDs   = []string{"foo", "bar"}

	// lineOrders are the keys of a line of the grid. In `a` the renamed key foo comes first, and in
	// `b` the literal key foo_extracted comes first.
	lineOrders = map[string][]item{
		"a": {{key: "foo", val: "p"}, {key: "foo_extracted", val: "lit"}, {key: "bar", val: "b"}},
		"b": {{key: "foo_extracted", val: "lit"}, {key: "foo", val: "p"}, {key: "bar", val: "b"}},
	}
	streamOpts = []labelSet{{}, {"foo": "l"}, {"foo_extracted": "le"}, {"foo": "l", "foo_extracted": "le"}}
	metaOpts   = []labelSet{{}, {"foo": "m"}, {"foo_extracted": "me"}, {"foo": "m", "foo_extracted": "me"}}
)

// single is a stream of the single cases of a section.
type single struct {
	tag    string
	stream labelSet
	meta   labelSet
	items  []item
	cap    string // what the parser must be able to write, or empty
}

var singles = []single{
	{"b3", labelSet{"foo": "l"}, labelSet{}, []item{{key: "other", val: "x"}}, ""},
	{"c2", labelSet{}, labelSet{"foo": "m"}, []item{{key: "other", val: "x"}}, ""},
	{"d2", labelSet{"foo": "l"}, labelSet{"foo": "m"}, []item{{key: "other", val: "x"}}, ""},
	{"d3", labelSet{"foo": "l"}, labelSet{"foo_extracted": "me"}, []item{{key: "foo", val: "p"}}, ""},
	{"f2", labelSet{}, labelSet{}, []item{{key: "foo_extracted", val: "lit"}}, ""},
	{"f4", labelSet{"foo_extracted": "le"}, labelSet{}, []item{{key: "foo_extracted", val: "p"}}, ""},
	{"f5", labelSet{}, labelSet{"foo_extracted": "me"}, []item{{key: "foo_extracted", val: "p"}}, ""},
	{"g1", labelSet{"foo": "l"}, labelSet{}, []item{{key: "foo", val: ""}}, "empty"},
	{"g2", labelSet{}, labelSet{}, []item{{key: "foo", val: ""}}, "empty"},
	{"g3", labelSet{"foo": "l"}, labelSet{}, []item{{key: "foo", null: true}}, "null"},
	{"g3b", labelSet{}, labelSet{}, []item{{key: "foo", null: true}}, "null"},
	{"h1", labelSet{}, labelSet{}, []item{{key: "foo", val: "a"}, {key: "foo", val: "b"}}, "dup"},
	{"h3", labelSet{"foo": "l"}, labelSet{}, []item{{key: "foo", val: "a"}, {key: "foo", val: "b"}}, "dup"},
	{"h4", labelSet{}, labelSet{"foo": "m"}, []item{{key: "foo", val: "a"}, {key: "foo", val: "b"}}, "dup"},
}

// pairCase is two parsers in a row: the stream labels, and the keys of each stage.
type pairCase struct {
	tag    string
	stream labelSet
	first  []item
	second []item
}

var pairCases = []pairCase{
	{"e1", labelSet{"foo": "l"}, []item{{key: "foo", val: "j1"}}, []item{{key: "foo", val: "f2"}}},
	{"e3", labelSet{}, []item{{key: "foo", val: "j1"}}, []item{{key: "foo", val: "f2"}}},
	{"e4", labelSet{"foo": "l"}, []item{{key: "bar", val: "b1"}}, []item{{key: "foo", val: "f2"}}},
}

// ---------------------------------------------------------------- rendering helpers

func lbl(d labelSet) string {
	keys := []string{"app"}
	var rest []string
	for k := range d {
		if k != "app" {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, d[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func clause(name string, d labelSet) string {
	if len(d) == 0 {
		return ""
	}
	keys := sortedKeys(d)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, d[k])
	}
	return " [" + name + " " + strings.Join(parts, " ") + "]"
}

// quote writes a log line as a script string. A line with a double quote uses backticks.
func quote(line string) string {
	if strings.Contains(line, `"`) {
		return "`" + line + "`"
	}
	return `"` + line + `"`
}

func jsonString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonValue(it item) string {
	if it.null {
		return "null"
	}
	return jsonString(it.val)
}

// ---------------------------------------------------------------- parsers

// parser renders the line and the query of one parser, and applies the model of it.
type parser interface {
	// entry is the line that the stage sees, for the keys.
	entry(items []item) string
	// query is the text after `| `.
	query(items []item) string
	apply(p *pipe, items []item)
	// canWrite reports whether the parser can write an empty value, a null or a duplicate key.
	canWrite(capability string) bool
	// perCaseQuery is true when the query holds the keys of the line.
	perCaseQuery() bool
}

func jsonLine(items []item, entry bool) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = jsonString(it.key) + ":" + jsonValue(it)
	}
	if entry {
		parts = append(parts, `"_entry":"x"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func nonNull(items []item) []item {
	var out []item
	for _, it := range items {
		if !it.null {
			out = append(out, it)
		}
	}
	return out
}

func values(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.val
	}
	return out
}

type jsonParser struct{}

func (jsonParser) entry(items []item) string   { return jsonLine(items, false) }
func (jsonParser) query([]item) string         { return "json" }
func (jsonParser) apply(p *pipe, items []item) { plainApply(p, nonNull(items)) }
func (jsonParser) canWrite(c string) bool      { return c == "empty" || c == "null" || c == "dup" }
func (jsonParser) perCaseQuery() bool          { return false }

type logfmtParser struct{ keepEmpty bool }

func (logfmtParser) entry(items []item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = it.key + "=" + it.val
	}
	return strings.Join(parts, " ")
}

func (l logfmtParser) query([]item) string {
	if l.keepEmpty {
		return "logfmt --keep-empty"
	}
	return "logfmt"
}

func (l logfmtParser) apply(p *pipe, items []item) {
	var kept []item
	for _, it := range items {
		if l.keepEmpty || it.val != "" {
			kept = append(kept, it)
		}
	}
	plainApply(p, kept)
}

func (logfmtParser) canWrite(c string) bool { return c == "empty" || c == "dup" }
func (logfmtParser) perCaseQuery() bool     { return false }

type unpackParser struct{}

func (unpackParser) entry(items []item) string { return jsonLine(items, true) }
func (unpackParser) query([]item) string       { return "unpack" }
func (unpackParser) apply(p *pipe, items []item) {
	plainApplyUpfront(p, nonNull(items))
	p.line = "x"
}
func (unpackParser) canWrite(c string) bool { return c == "empty" || c == "null" || c == "dup" }
func (unpackParser) perCaseQuery() bool     { return false }

type regexpParser struct{}

func (regexpParser) entry(items []item) string { return strings.Join(values(items), "|") }
func (regexpParser) query(items []item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = `(?P<` + it.key + `>\w+)`
	}
	return "regexp `" + strings.Join(parts, `\|`) + "`"
}
func (regexpParser) apply(p *pipe, items []item) { plainApply(p, items) }
func (regexpParser) canWrite(string) bool        { return false }
func (regexpParser) perCaseQuery() bool          { return true }

type patternParser struct{}

func (patternParser) entry(items []item) string { return strings.Join(values(items), " ") }
func (patternParser) query(items []item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = "<" + it.key + ">"
	}
	return "pattern `" + strings.Join(parts, " ") + "`"
}
func (patternParser) apply(p *pipe, items []item) { plainApply(p, items) }
func (patternParser) canWrite(string) bool        { return false }
func (patternParser) perCaseQuery() bool          { return true }

// jsonExprParser is `json a, b`.
type jsonExprParser struct {
	jsonParser
	ids []string
}

func (j jsonExprParser) query([]item) string         { return "json " + strings.Join(j.ids, ", ") }
func (j jsonExprParser) apply(p *pipe, items []item) { jsonExprApply(p, j.ids, items) }

// logfmtExprParser is `logfmt a, b`.
type logfmtExprParser struct {
	logfmtParser
	ids []string
}

func (l logfmtExprParser) query([]item) string         { return "logfmt " + strings.Join(l.ids, ", ") }
func (l logfmtExprParser) apply(p *pipe, items []item) { logfmtExprApply(p, l.ids, items) }

type namedParser struct {
	name string
	grid parser // the parser of the grid
	one  parser // the parser of the single cases
}

func parserSets() []namedParser {
	return []namedParser{
		{"json", jsonParser{}, jsonParser{}},
		{"logfmt", logfmtParser{}, logfmtParser{}},
		{"regexp", regexpParser{}, regexpParser{}},
		{"pattern", patternParser{}, patternParser{}},
		{"unpack", unpackParser{}, unpackParser{}},
		{"jsonx", jsonExprParser{ids: gridIDs}, jsonExprParser{ids: singleIDs}},
		{"logfmtx", logfmtExprParser{ids: gridIDs}, logfmtExprParser{ids: singleIDs}},
	}
}

func pairParsers() []namedParser {
	return []namedParser{
		{name: "json", grid: jsonParser{}},
		{name: "logfmt", grid: logfmtParser{}},
		{name: "regexp", grid: regexpParser{}},
		{name: "pattern", grid: patternParser{}},
		{name: "unpack", grid: unpackParser{}},
		{name: "jsonx", grid: jsonExprParser{ids: pairIDs}},
		{name: "logfmtx", grid: logfmtExprParser{ids: pairIDs}},
	}
}

// ---------------------------------------------------------------- streams and steps

type stream struct {
	app    string
	stream labelSet
	meta   labelSet
	items  []item
	tag    string
}

// load renders the stream as a line of the `load` block.
func (s *stream) load(p parser) string {
	lab := labelSet{"app": s.app}
	for k, v := range s.stream {
		lab[k] = v
	}
	tag := ""
	if s.tag != "" {
		tag = " # " + s.tag
	}
	return fmt.Sprintf("  %s %s @ 10s%s%s", lbl(lab), quote(p.entry(s.items)), clause("metadata", s.meta), tag)
}

type step func(p *pipe)

func run(s *stream, par parser, steps []step) *pipe {
	p := newPipe(s.stream, s.meta)
	p.line = par.entry(s.items)
	for _, st := range steps {
		st(p)
	}
	return p
}

func stepParse(par parser, items []item) step {
	return func(p *pipe) { par.apply(p, items) }
}

func stepDrop(name string) step { return func(p *pipe) { p.drop(name) } }

func stepFilter(name, value string) step {
	return func(p *pipe) {
		if p.merged()[name] != value {
			p.alive = false
		}
	}
}

func stepLineFormatConst(text string) step { return func(p *pipe) { p.line = text } }

func stepLineFormatTemplate() step {
	return func(p *pipe) {
		m := p.merged()
		p.line = m["foo"] + "|" + m["foo_extracted"]
	}
}

// ---------------------------------------------------------------- eval emitters

type parserOf func(s *stream) parser
type stepsOf func(s *stream) []step

func fixedParser(p parser) parserOf { return func(*stream) parser { return p } }

func parseSteps(par parser) stepsOf {
	return func(s *stream) []step { return []step{stepParse(par, s.items)} }
}

func selectEval(selector, query string, streams []*stream, par parserOf, steps stepsOf) []string {
	out := []string{fmt.Sprintf("eval select from 0 to 30s forward {%s} | %s", selector, query)}
	n := 0
	for _, s := range streams {
		p := run(s, par(s), steps(s))
		if !p.alive {
			continue
		}
		base, meta, parsed := p.final()
		lab := labelSet{"app": s.app}
		for k, v := range base {
			lab[k] = v
		}
		out = append(out, fmt.Sprintf("  %s %s @ 10s%s%s", lbl(lab), quote(p.line), clause("metadata", meta), clause("parsed", parsed)))
		n++
	}
	if n == 0 {
		out = append(out, "  expect empty")
	}
	return out
}

func groupedEvals(selector, query string, streams []*stream, par parserOf, steps stepsOf, by []string) []string {
	var order []string
	counts := map[string]int{}
	for _, s := range streams {
		p := run(s, par(s), steps(s))
		if !p.alive {
			continue
		}
		m := p.merged()
		var parts []string
		for _, n := range by {
			if v, ok := m[n]; ok {
				parts = append(parts, fmt.Sprintf("%s=%q", n, v))
			}
		}
		key := "{" + strings.Join(parts, ", ") + "}"
		if _, ok := counts[key]; !ok {
			order = append(order, key)
		}
		counts[key]++
	}
	var bodyInstant, bodyRange []string
	for _, key := range order {
		bodyInstant = append(bodyInstant, fmt.Sprintf("  %s %d", key, counts[key]))
		bodyRange = append(bodyRange, fmt.Sprintf("  %s %d %d", key, counts[key], counts[key]))
	}
	if len(order) == 0 {
		bodyInstant, bodyRange = []string{"  expect empty"}, []string{"  expect empty"}
	}
	expr := fmt.Sprintf("sum by (%s) (count_over_time({%s} | %s [1m]))", strings.Join(by, ", "), selector, query)
	out := []string{"eval instant at 60s " + expr}
	out = append(out, bodyInstant...)
	out = append(out, "eval range from 40s to 60s step 20s "+expr+" # overlapping")
	out = append(out, bodyRange...)
	return out
}

// ---------------------------------------------------------------- the section of a parser

// sameLabels reports whether the two pipes ended with the same labels.
func sameLabels(a, b *pipe) bool {
	as, am, ap := a.final()
	bs, bm, bp := b.final()
	return equal(as, bs) && equal(am, bm) && equal(ap, bp)
}

func equal(a, b labelSet) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func streamsFor(name string, one parser) (grid, singleStreams []*stream, err error) {
	for si, s := range streamOpts {
		for mi, m := range metaOpts {
			for _, o := range []string{"a", "b"} {
				// Testing the keys one by one, or all before the first write, gives the same labels.
				a, b := newPipe(s, m), newPipe(s, m)
				plainApply(a, lineOrders[o])
				plainApplyUpfront(b, lineOrders[o])
				if !sameLabels(a, b) {
					return nil, nil, fmt.Errorf("%s: the key order or the stage decides the labels for stream %d, metadata %d, order %s", name, si, mi, o)
				}
				grid = append(grid, &stream{app: fmt.Sprintf("%s_%d%d%s", name, si, mi, o), stream: s, meta: m, items: lineOrders[o]})
			}
		}
	}
	for _, c := range singles {
		if c.cap != "" && !one.canWrite(c.cap) {
			continue
		}
		singleStreams = append(singleStreams, &stream{app: name + "_c_" + c.tag, stream: c.stream, meta: c.meta, items: c.items, tag: strings.ToUpper(c.tag)})
	}
	return grid, singleStreams, nil
}

func distinctValues(streams []*stream, par parserOf, name string) []string {
	var vals []string
	for _, s := range streams {
		m := run(s, par(s), []step{stepParse(par(s), s.items)}).merged()
		v := m[name]
		found := false
		for _, w := range vals {
			if w == v {
				found = true
			}
		}
		if !found {
			vals = append(vals, v)
		}
	}
	sort.Strings(vals)
	return vals
}

func parserSection(np namedParser) ([]string, error) {
	name, gp, sp := np.name, np.grid, np.one
	grid, singleStreams, err := streamsFor(name, sp)
	if err != nil {
		return nil, err
	}
	out := []string{"# " + name, "clear", "load"}
	for _, s := range grid {
		out = append(out, s.load(gp))
	}
	for _, s := range singleStreams {
		out = append(out, s.load(sp))
	}
	out = append(out, "")

	type group struct {
		selector string
		streams  []*stream
		order    string
	}
	var groups []group
	if gp.perCaseQuery() {
		for _, o := range []string{"a", "b"} {
			var sel []*stream
			for _, s := range grid {
				if strings.HasSuffix(s.app, o) {
					sel = append(sel, s)
				}
			}
			groups = append(groups, group{fmt.Sprintf(`app=~"%s_..%s"`, name, o), sel, o})
		}
	} else {
		groups = append(groups, group{fmt.Sprintf(`app=~"%s_..[ab]"`, name), grid, ""})
	}

	par := fixedParser(gp)
	steps := parseSteps(gp)
	for _, g := range groups {
		query := gp.query(nil)
		which := ""
		if g.order != "" {
			query = gp.query(lineOrders[g.order])
			which = " Key order " + g.order + "."
		}
		out = append(out, fmt.Sprintf("# %s: every set of stream labels x every set of metadata x both key orders.%s", name, which))
		out = append(out, selectEval(g.selector, query, g.streams, par, steps)...)

		out = append(out, fmt.Sprintf("# %s: a label filter after the parser, for every final value of `foo_extracted` and `foo`.%s", name, which))
		for _, lname := range []string{"foo_extracted", "foo"} {
			for _, v := range distinctValues(g.streams, par, lname) {
				st := func(s *stream) []step { return []step{stepParse(gp, s.items), stepFilter(lname, v)} }
				out = append(out, selectEval(g.selector, fmt.Sprintf(`%s | %s="%s"`, query, lname, v), g.streams, par, st)...)
			}
		}

		out = append(out, fmt.Sprintf("# %s: grouped metric queries.%s", name, which))
		for _, by := range [][]string{{"foo", "foo_extracted"}, {"foo_extracted"}} {
			out = append(out, groupedEvals(g.selector, query, g.streams, par, steps, by)...)
		}

		out = append(out, fmt.Sprintf("# %s: `drop foo` and `drop foo_extracted` before the parser, and a formatter after it.%s", name, which))
		for _, d := range []string{"foo", "foo_extracted"} {
			st := func(s *stream) []step { return []step{stepDrop(d), stepParse(gp, s.items)} }
			out = append(out, selectEval(g.selector, fmt.Sprintf("drop %s | %s", d, query), g.streams, par, st)...)
		}
		st := func(s *stream) []step { return []step{stepParse(gp, s.items), stepLineFormatTemplate()} }
		out = append(out, selectEval(g.selector, query+` | line_format "{{.foo}}|{{.foo_extracted}}"`, g.streams, par, st)...)
		out = append(out, "")
	}

	if len(singleStreams) > 0 {
		out = append(out, fmt.Sprintf("# %s: single cases. A key that the line lacks, an empty value, a null, a duplicate key.", name))
		one := fixedParser(sp)
		if sp.perCaseQuery() {
			for _, s := range singleStreams {
				out = append(out, selectEval(fmt.Sprintf(`app="%s"`, s.app), sp.query(s.items), []*stream{s}, one, parseSteps(sp))...)
			}
		} else {
			selector := fmt.Sprintf(`app=~"%s_c_.*"`, name)
			out = append(out, selectEval(selector, sp.query(nil), singleStreams, one, parseSteps(sp))...)
			if _, ok := sp.(logfmtParser); ok {
				kp := logfmtParser{keepEmpty: true}
				out = append(out, fmt.Sprintf("# %s: the same single cases with `--keep-empty`.", name))
				out = append(out, selectEval(selector, kp.query(nil), singleStreams, fixedParser(kp), parseSteps(kp))...)
			}
		}
		out = append(out, "")
	}
	return out, nil
}

// ---------------------------------------------------------------- the parser pairs

func pairSection(first namedParser) []string {
	p1 := first.grid
	out := []string{
		fmt.Sprintf("# Two parsers in a row: %s first, then every parser. `line_format` swaps the line between the stages.", first.name),
		"# E1: the stream has `foo`, and both stages write `foo`. E3: no stream label. E4: the first stage lacks `foo`.",
		"clear",
		"load",
	}
	streams := map[string]*stream{}
	for _, c := range pairCases {
		s := &stream{app: fmt.Sprintf("pair_%s_%s", first.name, c.tag), stream: c.stream, meta: labelSet{}, items: c.first, tag: strings.ToUpper(c.tag)}
		streams[c.tag] = s
		out = append(out, s.load(p1))
	}
	out = append(out, "")
	for _, c := range pairCases {
		s := streams[c.tag]
		for _, second := range pairParsers() {
			p2 := second.grid
			text := p2.entry(c.second)
			escaped := strings.ReplaceAll(text, `"`, `\"`)
			query := fmt.Sprintf(`%s | line_format "%s" | %s`, p1.query(c.first), escaped, p2.query(c.second))
			steps := []step{stepParse(p1, c.first), stepLineFormatConst(text), stepParse(p2, c.second)}
			out = append(out, selectEval(fmt.Sprintf(`app="%s"`, s.app), query, []*stream{s}, fixedParser(p1), func(*stream) []step { return steps })...)
		}
	}
	out = append(out, "")
	return out
}

// Generate returns the text of the script. It fails when the model gives different labels for the
// same shape, depending on the order of the keys or on the parser stage.
func Generate() (string, error) {
	body := append([]string{}, headerLines...)
	body = append(body, "")
	for _, np := range parserSets() {
		section, err := parserSection(np)
		if err != nil {
			return "", err
		}
		body = append(body, section...)
	}
	for _, np := range pairParsers() {
		body = append(body, pairSection(np)...)
	}
	return strings.TrimRight(strings.Join(body, "\n"), "\n") + "\n", nil
}
