package executor

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// Conformance suite for label/metadata/parsed name-collision handling
// (`_extracted` renaming). Every case is executed against both engines:
//
//   - TestCompatConformanceV1 runs the v1 pipeline (pkg/logql/log) and asserts
//     the expected outcome. v1 is the reference; these must always pass.
//   - TestCompatConformanceV2 runs the corresponding v2 pipeline (scan compat →
//     [parse projection → parse compat]...) and asserts the same outcome.
//
// Fixtures are per-row: labels/metadata describe the stream, lines[i] is the
// log line content when parser stage i runs (simulating `line_format` between
// stages), and want is the effective label set (v1 LabelsResult).

type compatConformanceRow struct {
	name     string
	labels   map[string]string
	metadata map[string]string
	lines    []string // one entry per parser stage (at least one, even with no stages)
	want     map[string]string
	// v2Want overrides want for the v2 engine on rows where v1's outcome
	// depends on information the columnar model does not have (see the row's
	// comment). Nil means v2 must match want.
	v2Want map[string]string
}

type compatConformanceCase struct {
	name   string
	stages []types.VariadicOp // parser stages, run in order
	rows   []compatConformanceRow
}

var compatConformanceCases = []compatConformanceCase{
	{
		// A: metadata × label (scan compat only, no parser).
		name: "A_metadata_vs_label",
		rows: []compatConformanceRow{
			{name: "A1_both", labels: map[string]string{"foo": "l"}, metadata: map[string]string{"foo": "m"}, lines: []string{""},
				want: map[string]string{"foo": "l", "foo_extracted": "m"}},
			{name: "A2_metadata_only", metadata: map[string]string{"foo": "m"}, lines: []string{""},
				want: map[string]string{"foo": "m"}},
			{name: "A3_label_only", labels: map[string]string{"foo": "l"}, lines: []string{""},
				want: map[string]string{"foo": "l"}},
			{name: "A4_neither", lines: []string{""},
				want: map[string]string{}},
		},
	},
	{
		// B: parsed × label.
		name:   "B_parsed_vs_label",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "B1_both", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":"p"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "p"}},
			{name: "B2_parsed_only", lines: []string{`{"foo":"p"}`},
				want: map[string]string{"foo": "p"}},
			{name: "B3_label_only", labels: map[string]string{"foo": "l"}, lines: []string{`{"other":"x"}`},
				want: map[string]string{"foo": "l", "other": "x"}},
			{name: "B4_neither", lines: []string{`{}`},
				want: map[string]string{}},
		},
	},
	{
		// C: parsed × metadata (no label with the same name on the row), with a
		// mixed-stream row to exercise per-row collision resolution in one batch.
		name:   "C_parsed_vs_metadata",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "C1_metadata_and_parsed", metadata: map[string]string{"foo": "m"}, lines: []string{`{"foo":"p"}`},
				want: map[string]string{"foo": "m", "foo_extracted": "p"}},
			{name: "C2_metadata_only", metadata: map[string]string{"foo": "m"}, lines: []string{`{"other":"x"}`},
				want: map[string]string{"foo": "m", "other": "x"}},
			{name: "C3_label_row_in_same_batch", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":"p"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "p"}},
		},
	},
	{
		// D: triple collision label + metadata + parsed. The metadata value is
		// renamed by the scan compat and then clobbered (or not) per row by the
		// parse compat, depending on whether the parser extracted the key.
		name:   "D_triple_collision",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "D1_parsed_present", labels: map[string]string{"foo": "l"}, metadata: map[string]string{"foo": "m"}, lines: []string{`{"foo":"p","other":"x"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "p", "other": "x"}},
			{name: "D2_parsed_absent", labels: map[string]string{"foo": "l"}, metadata: map[string]string{"foo": "m"}, lines: []string{`{"other":"x"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "m", "other": "x"}},
		},
	},
	{
		// D3: metadata literally named foo_extracted, clobbered by the renamed
		// parsed foo (v1: Set(ParsedLabel, "foo_extracted") deletes the SM entry).
		name:   "D3_metadata_named_foo_extracted",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "D3", labels: map[string]string{"foo": "l"}, metadata: map[string]string{"foo_extracted": "me"}, lines: []string{`{"foo":"p"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "p"}},
		},
	},
	{
		// E: layered parsers (json, then logfmt on a rewritten line). v1 is
		// first-writer-wins per key name: every Set(ParsedLabel, ...) records
		// the key in the parser hints and later parser stages skip keys that
		// were already extracted.
		name:   "E_layered_parsers",
		stages: []types.VariadicOp{types.VariadicOpParseJSON, types.VariadicOpParseLogfmt},
		rows: []compatConformanceRow{
			{name: "E1_both_stages_extract", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":"j1"}`, `foo=f2`},
				want: map[string]string{"foo": "l", "foo_extracted": "j1"}},
			{name: "E2_stage2_missing_key", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":"j1"}`, `bar=b2`},
				want: map[string]string{"foo": "l", "foo_extracted": "j1", "bar": "b2"}},
			{name: "E3_no_collision", lines: []string{`{"foo":"j1"}`, `foo=f2`},
				want: map[string]string{"foo": "j1"}},
			{name: "E4_stage1_missing_key", labels: map[string]string{"foo": "l"}, lines: []string{`{"bar":"j1"}`, `foo=f2`},
				want: map[string]string{"foo": "l", "foo_extracted": "f2", "bar": "j1"}},
			{name: "E5_stage1_empty_string", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":""}`, `foo=f2`},
				want: map[string]string{"foo": "l", "foo_extracted": ""}},
			{name: "E6_stage1_empty_string_stage2_missing", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":""}`, `bar=b2`},
				want: map[string]string{"foo": "l", "foo_extracted": "", "bar": "b2"}},
		},
	},
	{
		// F1: line contains both foo and a literal foo_extracted while label foo
		// collides. Both parsed keys end up targeting the name foo_extracted; in
		// v1 the winner is whichever key comes first in the line (first-writer-
		// wins per final name). Key order within a line is not representable in
		// the columnar model, so v2 deterministically keeps the value that
		// already occupies the foo_extracted column (the literal key), matching
		// v1 only when the literal key comes first — hence the v2Want override
		// on F1a.
		name:   "F1_literal_extracted_with_collision",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "F1a_foo_first", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":"p","foo_extracted":"lit"}`},
				want:   map[string]string{"foo": "l", "foo_extracted": "p"},
				v2Want: map[string]string{"foo": "l", "foo_extracted": "lit"}},
			{name: "F1b_literal_first", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo_extracted":"lit","foo":"p"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "lit"}},
		},
	},
	{
		// F2: literal foo_extracted with no collision anywhere — passthrough.
		name:   "F2_literal_extracted_no_collision",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "F2_literal_only", lines: []string{`{"foo_extracted":"lit"}`},
				want: map[string]string{"foo_extracted": "lit"}},
			{name: "F2b_foo_and_literal", lines: []string{`{"foo":"p","foo_extracted":"lit"}`},
				want: map[string]string{"foo": "p", "foo_extracted": "lit"}},
		},
	},
	{
		// F6: same as F2b, but the batch also contains a row whose stream has
		// label foo — so the collision column exists but is null on the F6 row.
		name:   "F6_mixed_stream_literal_extracted",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "F6_other_stream", labels: map[string]string{"foo": "l"}, lines: []string{`{"other":"x"}`},
				want: map[string]string{"foo": "l", "other": "x"}},
			{name: "F6_no_label_row", lines: []string{`{"foo":"p","foo_extracted":"lit"}`},
				want: map[string]string{"foo": "p", "foo_extracted": "lit"}},
		},
	},
	{
		// F3: stream label literally named foo_extracted is clobbered by the
		// renamed parsed foo (v1 R5).
		name:   "F3_label_named_foo_extracted",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "F3", labels: map[string]string{"foo": "l", "foo_extracted": "le"}, lines: []string{`{"foo":"p"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "p"}},
		},
	},
	{
		// F4: label foo_extracted + parsed literal foo_extracted → double suffix.
		name:   "F4_double_suffix_label",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "F4", labels: map[string]string{"foo_extracted": "le"}, lines: []string{`{"foo_extracted":"p"}`},
				want: map[string]string{"foo_extracted": "le", "foo_extracted_extracted": "p"}},
		},
	},
	{
		// F5: metadata foo_extracted + parsed literal foo_extracted → double suffix.
		name:   "F5_double_suffix_metadata",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "F5", metadata: map[string]string{"foo_extracted": "me"}, lines: []string{`{"foo_extracted":"p"}`},
				want: map[string]string{"foo_extracted": "me", "foo_extracted_extracted": "p"}},
		},
	},
	{
		// F7: labels foo AND foo_extracted, line contains both foo and a literal
		// foo_extracted. v1: foo→foo_extracted (clobbers le), literal
		// foo_extracted→foo_extracted_extracted.
		name:   "F7_chained_rename_conflict",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			// parsed.foo_extracted is simultaneously a collision source (vs
			// label.foo_extracted) and the destination name of parsed.foo.
			{name: "F7", labels: map[string]string{"foo": "l", "foo_extracted": "le"}, lines: []string{`{"foo":"p","foo_extracted":"q"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "p", "foo_extracted_extracted": "q"}},
		},
	},
	{
		// F8: both label.foo_extracted and metadata.foo_extracted exist
		// alongside a colliding parsed foo. v1 renames the SM key at Add
		// (foo_extracted → foo_extracted_extracted, BaseHas) and the parsed foo
		// clobbers the stream foo_extracted.
		name:   "F8_double_lower_priority_extracted",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "F8", labels: map[string]string{"foo": "l", "foo_extracted": "le"}, metadata: map[string]string{"foo_extracted": "me"}, lines: []string{`{"foo":"p"}`},
				want: map[string]string{"foo": "l", "foo_extracted": "p", "foo_extracted_extracted": "me"}},
		},
	},
	{
		// G/H (json): value-shape edge cases and within-line duplicate keys.
		name:   "G_json_value_shapes",
		stages: []types.VariadicOp{types.VariadicOpParseJSON},
		rows: []compatConformanceRow{
			{name: "G1_empty_string_with_collision", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":""}`},
				want: map[string]string{"foo": "l", "foo_extracted": ""}},
			{name: "G2_empty_string_no_collision", lines: []string{`{"foo":""}`},
				want: map[string]string{"foo": ""}},
			{name: "G3_json_null_skipped", labels: map[string]string{"foo": "l"}, lines: []string{`{"foo":null}`},
				want: map[string]string{"foo": "l"}},
			{name: "H1_duplicate_key_first_wins", lines: []string{`{"foo":"a","foo":"b"}`},
				want: map[string]string{"foo": "a"}},
		},
	},
	{
		// G/H (logfmt): empty values skipped without --keep-empty; duplicate keys.
		name:   "H_logfmt_value_shapes",
		stages: []types.VariadicOp{types.VariadicOpParseLogfmt},
		rows: []compatConformanceRow{
			{name: "H2_duplicate_key_first_wins", lines: []string{`foo=a foo=b`},
				want: map[string]string{"foo": "a"}},
			{name: "G4_empty_value_skipped", labels: map[string]string{"foo": "l"}, lines: []string{`foo=`},
				want: map[string]string{"foo": "l"}},
		},
	},
}

// --- v1 (reference) runner ---

func newV1ConformanceStage(t *testing.T, op types.VariadicOp) log.Stage {
	switch op {
	case types.VariadicOpParseJSON:
		return log.NewJSONParser(false)
	case types.VariadicOpParseLogfmt:
		return log.NewLogfmtParser(false, false)
	default:
		t.Fatalf("unsupported v1 conformance stage: %v", op)
		return nil
	}
}

func sortedKVLabels(m map[string]string) labels.Labels {
	if len(m) == 0 {
		return labels.EmptyLabels()
	}
	kv := make([]string, 0, len(m)*2)
	for k, v := range m {
		kv = append(kv, k, v)
	}
	return labels.FromStrings(kv...)
}

func runV1ConformanceRow(t *testing.T, stages []types.VariadicOp, row compatConformanceRow) map[string]string {
	base := sortedKVLabels(row.labels)
	b := log.NewBaseLabelsBuilderWithGrouping(nil, log.NoParserHints(), false, false).
		ForLabels(base, labels.StableHash(base))
	b.Reset()
	if len(row.metadata) > 0 {
		// Structured metadata enters through Add, which applies the BaseHas rename.
		b.Add(log.StructuredMetadataLabel, sortedKVLabels(row.metadata))
	}
	for i, op := range stages {
		_, _ = newV1ConformanceStage(t, op).Process(0, []byte(row.lines[i]), b)
	}
	got := map[string]string{}
	b.LabelsResult().Labels().Range(func(l labels.Label) {
		got[l.Name] = l.Value
	})
	return got
}

func TestCompatConformanceV1(t *testing.T) {
	for _, tc := range compatConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, row := range tc.rows {
				t.Run(row.name, func(t *testing.T) {
					require.Equal(t, row.want, runV1ConformanceRow(t, tc.stages, row))
				})
			}
		})
	}
}

// --- v2 runner ---

// conformanceBatch builds the scan output batch: message + label.* + metadata.*
// columns, null where a row's stream doesn't have the key.
func conformanceBatch(tc compatConformanceCase) (*arrow.Schema, arrowtest.Rows) {
	var labelKeys, mdKeys []string
	for _, r := range tc.rows {
		for k := range r.labels {
			if !slices.Contains(labelKeys, k) {
				labelKeys = append(labelKeys, k)
			}
		}
		for k := range r.metadata {
			if !slices.Contains(mdKeys, k) {
				mdKeys = append(mdKeys, k)
			}
		}
	}
	slices.Sort(labelKeys)
	slices.Sort(mdKeys)

	fields := []arrow.Field{semconv.FieldFromIdent(semconv.ColumnIdentMessage, false)}
	fqn := func(k string, ct types.ColumnType) string {
		return semconv.NewIdentifier(k, ct, types.Loki.String).FQN()
	}
	for _, k := range labelKeys {
		fields = append(fields, semconv.FieldFromFQN(fqn(k, types.ColumnTypeLabel), true))
	}
	for _, k := range mdKeys {
		fields = append(fields, semconv.FieldFromFQN(fqn(k, types.ColumnTypeMetadata), true))
	}

	rows := make(arrowtest.Rows, len(tc.rows))
	for i, r := range tc.rows {
		m := map[string]any{semconv.ColumnIdentMessage.FQN(): r.lines[0]}
		for _, k := range labelKeys {
			if v, ok := r.labels[k]; ok {
				m[fqn(k, types.ColumnTypeLabel)] = v
			} else {
				m[fqn(k, types.ColumnTypeLabel)] = nil
			}
		}
		for _, k := range mdKeys {
			if v, ok := r.metadata[k]; ok {
				m[fqn(k, types.ColumnTypeMetadata)] = v
			} else {
				m[fqn(k, types.ColumnTypeMetadata)] = nil
			}
		}
		rows[i] = m
	}
	return arrow.NewSchema(fields, nil), rows
}

// swapMessagePipeline replaces the builtin message column values with lines,
// simulating `| line_format` between parser stages.
func swapMessagePipeline(input Pipeline, lines []string) Pipeline {
	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
		batch, err := inputs[0].Read(ctx)
		if err != nil {
			return nil, err
		}
		msgIdxs := batch.Schema().FieldIndices(semconv.ColumnIdentMessage.FQN())
		if len(msgIdxs) != 1 {
			return nil, fmt.Errorf("expected exactly one message column, got %d", len(msgIdxs))
		}
		builder := array.NewStringBuilder(memory.DefaultAllocator)
		defer builder.Release()
		for _, l := range lines {
			builder.Append(l)
		}
		cols := make([]arrow.Array, batch.NumCols())
		for i := range cols {
			cols[i] = batch.Column(i)
		}
		cols[msgIdxs[0]] = builder.NewArray()
		return array.NewRecordBatch(batch.Schema(), cols, batch.NumRows()), nil
	}, input)
}

func parseProjection(op types.VariadicOp) *physical.Projection {
	return &physical.Projection{
		Expressions: []physical.Expression{&physical.VariadicExpr{
			Op: op,
			Expressions: []physical.Expression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				physical.NewLiteral([]string{}),
				physical.NewLiteral(false), // strict
				physical.NewLiteral(false), // keepEmpty
			},
		}},
		All:    true,
		Expand: true,
	}
}

// runV2ConformanceCase assembles the v2 pipeline exactly as the physical
// planner does for `{...} | parser | parser ...` in v1-compatible mode:
// scan → ColumnCompat(metadata vs label) → per parser: projection(expand) →
// ColumnCompat(parsed vs label+metadata). Returns the effective (non-null,
// non-builtin, non-generated) label set per row.
func runV2ConformanceCase(t *testing.T, tc compatConformanceCase) (got []map[string]string, panicked any) {
	schema, rows := conformanceBatch(tc)
	var pipe Pipeline = NewArrowtestPipeline(schema, rows)

	pipe = newColumnCompatibilityPipeline(&physical.ColumnCompat{
		Source:      types.ColumnTypeMetadata,
		Destination: types.ColumnTypeMetadata,
		Collisions:  []types.ColumnType{types.ColumnTypeLabel},
	}, pipe)

	evaluator := newExpressionEvaluator()
	for si, op := range tc.stages {
		if si > 0 {
			lines := make([]string, len(tc.rows))
			for i, r := range tc.rows {
				lines[i] = r.lines[si]
			}
			pipe = swapMessagePipeline(pipe, lines)
		}
		var err error
		pipe, err = NewProjectPipeline(pipe, parseProjection(op), evaluator)
		require.NoError(t, err)
		pipe = newColumnCompatibilityPipeline(&physical.ColumnCompat{
			Source:      types.ColumnTypeParsed,
			Destination: types.ColumnTypeParsed,
			Collisions:  []types.ColumnType{types.ColumnTypeLabel, types.ColumnTypeMetadata},
		}, pipe)
	}

	// All pipeline stages are lazy; panics (e.g. duplicate-label assertions)
	// surface during Read. Recover so a broken case fails instead of crashing
	// the suite.
	defer func() { panicked = recover() }()

	batch, err := pipe.Read(t.Context())
	require.NoError(t, err)
	defer pipe.Close()

	got = make([]map[string]string, batch.NumRows())
	for r := range got {
		got[r] = map[string]string{}
	}
	for i, f := range batch.Schema().Fields() {
		ident, err := semconv.ParseFQN(f.Name)
		if err != nil {
			continue
		}
		ct := ident.ColumnType()
		if ct == types.ColumnTypeBuiltin || ct == types.ColumnTypeGenerated {
			continue
		}
		col := batch.Column(i)
		for r := range int(batch.NumRows()) {
			if col.IsNull(r) || !col.IsValid(r) {
				continue
			}
			name := ident.ShortName()
			if prev, ok := got[r][name]; ok {
				t.Errorf("row %d: duplicate output label %q: %q and %q", r, name, prev, col.ValueStr(r))
			}
			got[r][name] = col.ValueStr(r)
		}
	}
	return got, nil
}

func TestCompatConformanceV2(t *testing.T) {
	for _, tc := range compatConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			got, panicked := runV2ConformanceCase(t, tc)
			if panicked != nil {
				t.Fatalf("v2 pipeline panicked: %v", panicked)
			}
			require.Len(t, got, len(tc.rows))
			for i, row := range tc.rows {
				t.Run(row.name, func(t *testing.T) {
					want := row.want
					if row.v2Want != nil {
						want = row.v2Want
					}
					require.Equal(t, want, got[i])
				})
			}
		})
	}
}
