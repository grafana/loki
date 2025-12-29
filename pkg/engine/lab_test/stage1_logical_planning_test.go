package engine_lab

/*
================================================================================
STAGE 1: LOGICAL PLANNING - SSA (Static Single Assignment) Intermediate Representation
================================================================================

This file covers the first major stage of the Loki Query Engine V2 pipeline:
converting a parsed LogQL AST into an SSA-based logical plan.

================================================================================
WHAT IS SSA (STATIC SINGLE ASSIGNMENT)?
================================================================================

SSA is an intermediate representation (IR) where every variable is assigned
exactly once. This property makes data flow explicit and enables powerful
optimizations.

Key Properties:
  - Each variable (%1, %2, %3, ...) is assigned exactly once
  - Values flow from earlier instructions to later ones
  - Data dependencies are explicit in the IR
  - No mutation - new values create new variables

Benefits of SSA:
  - Simplifies dataflow analysis
  - Enables constant propagation
  - Makes dead code elimination easier
  - Facilitates common subexpression elimination

================================================================================
COLUMN TYPE PREFIXES IN SSA
================================================================================

The SSA uses column type prefixes to identify the source of each column:

  label.*     : Stream labels (set at ingestion time, part of stream identity)
                Example: label.app, label.namespace, label.cluster
                Stream labels define the unique identity of a log stream.

  metadata.*  : Structured metadata (set at ingestion, NOT part of stream identity)
                Example: metadata.traceID, metadata.spanID, metadata.pod
                Metadata is per-line data that doesn't affect stream grouping.

  parsed.*    : Fields extracted during query execution
                Example: parsed.level, parsed.msg, parsed.duration
                Created by parsers like | json or | logfmt.

  builtin.*   : Special engine columns (always present)
                - builtin.timestamp: Log entry timestamp (nanoseconds)
                - builtin.message: The log line text
                - builtin.value: Numeric value (for metric queries)

  ambiguous.* : Column type unknown until physical planning resolves it
                Example: ambiguous.level (could be label, metadata, or parsed)
                Used when the planner doesn't know the column's origin yet.

================================================================================
SSA INSTRUCTION REFERENCE
================================================================================

The logical planner generates these instruction types:

PREDICATE INSTRUCTIONS (produce boolean values):
  - EQ:           Equality test (==)
  - NEQ:          Not equal (!=)
  - GT:           Greater than (>)
  - GTE:          Greater than or equal (>=)
  - LT:           Less than (<)
  - LTE:          Less than or equal (<=)
  - MATCH_STR:    Substring match (|= operator)
  - NOT_MATCH_STR: Negative substring match (!= operator)
  - MATCH_RE:     Regex match (|~ or =~ operator)
  - NOT_MATCH_RE: Negative regex match (!~ operator)
  - AND:          Logical AND of predicates
  - OR:           Logical OR of predicates

DATA SOURCE INSTRUCTIONS:
  - MAKETABLE:    Defines data source with selector and pushdown hints
                  Parameters:
                    - selector: Stream selector predicate
                    - predicates: Line filters for storage pushdown
                    - shard: Query sharding info (e.g., 0_of_1)

FILTERING INSTRUCTIONS:
  - SELECT:       Filter rows by predicate
                  Keeps rows where predicate evaluates to TRUE.

PROJECTION INSTRUCTIONS:
  - PROJECT:      Transform/add/remove columns
                  Modes:
                    - *E (Extend): Add new columns, keep existing
                    - *D (Drop): Remove specified columns
                  Expressions:
                    - PARSE_JSON: Parse log line as JSON
                    - PARSE_LOGFMT: Parse log line as logfmt (key=value)
                    - PARSE_REGEXP: Parse with regex capture groups
                    - PARSE_PATTERN: Parse with pattern template
                    - LINE_FMT: Format log line (not yet implemented)
                    - LABEL_FMT: Format labels (not yet implemented)

SORTING/LIMITING INSTRUCTIONS:
  - TOPK:         Sort and limit results
                  Parameters:
                    - sort_by: Column to sort by (typically builtin.timestamp)
                    - k: Maximum number of results
                    - asc: Sort direction (true=ascending, false=descending)
                    - nulls_first: NULL handling

AGGREGATION INSTRUCTIONS:
  - RANGE_AGGREGATION:  Aggregate over sliding time windows
                        Operations: count, sum, avg, min, max, first, last,
                                   bytes, absent, stdvar, stddev, quantile
                        Parameters: start_ts, end_ts, step, range

  - VECTOR_AGGREGATION: Aggregate across label groups
                        Operations: sum, avg, min, max, count, stdvar, stddev,
                                   bottomk, topk, sort, sort_desc
                        Parameters: group_by labels, without labels

ARITHMETIC INSTRUCTIONS:
  - ADD: Addition
  - SUB: Subtraction
  - MUL: Multiplication
  - DIV: Division (used for rate() = count/range_seconds)
  - MOD: Modulo
  - POW: Power

OUTPUT INSTRUCTIONS:
  - LOGQL_COMPAT: Compatibility wrapper for LogQL V1 output format
  - RETURN:       Designates final output value

================================================================================
QUERY EXECUTION FLOW
================================================================================

  1. User Query: {app="test"} |= "error" | json | level="error"

  2. Parsing (logql/syntax): Produces syntax.Expr (AST)

  3. Logical Planning (this stage): Produces logical.Plan (SSA IR)

     %1 = EQ label.app "test"              // Stream selector
     %2 = MATCH_STR builtin.message "error" // Line filter
     %3 = MAKETABLE [selector=%1, predicates=[%2]]
     %4 = GTE builtin.timestamp <start>
     %5 = SELECT %3 [predicate=%4]
     %6 = LT builtin.timestamp <end>
     %7 = SELECT %5 [predicate=%6]
     %8 = SELECT %7 [predicate=%2]         // Apply line filter
     %9 = PROJECT %8 [mode=*E, expr=PARSE_JSON(...)]
     %10 = EQ ambiguous.level "error"
     %11 = SELECT %9 [predicate=%10]
     %12 = TOPK %11 [sort_by=builtin.timestamp, k=100]
     %13 = LOGQL_COMPAT %12
     RETURN %13

  4. Physical Planning: Converts SSA to executable DAG
  5. Workflow Planning: Partitions into distributable tasks
  6. Execution: Produces Arrow RecordBatches
  7. Result Building: Converts to LogQL results

================================================================================
*/

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/logproto"
)

/*
TestLogicalPlanning_LogQuery demonstrates logical plan generation for log queries.

Each sub-test shows a specific LogQL pattern and explains the resulting SSA.
Tests are ordered from simple to complex to build understanding progressively.
*/
func TestLogicalPlanning_LogQuery(t *testing.T) {
	t.Run("simple_log_query_with_label_selector_and_line_filter", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Simple Log Query with Label Selector and Line Filter
		   ============================================================================

		   LogQL: {app="test"} |= "error"

		   This is the most basic log query pattern combining:
		   - A stream selector {app="test"} to filter which streams to read
		   - A line filter |= "error" to keep only lines containing "error"

		   ============================================================================
		   SSA OUTPUT (with explanations):
		   ============================================================================

		   %1 = EQ label.app "test"
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   Creates a PREDICATE that evaluates to TRUE when the stream label "app"
		   equals the string "test".

		   Key details:
		   - EQ is the equality comparison operator
		   - "label." prefix indicates this is a stream label
		   - The result is a boolean predicate, not a filter operation yet

		   %2 = MATCH_STR builtin.message "error"
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   Creates a PREDICATE for the line filter |= "error".

		   Key details:
		   - MATCH_STR performs substring matching (contains)
		   - "builtin.message" is the special column containing the log line text
		   - Returns TRUE if the log line contains "error" anywhere

		   Comparison with other line filter operators:
		   - |= "error"  → MATCH_STR     (contains)
		   - != "error"  → NOT_MATCH_STR (does not contain)
		   - |~ "err.*"  → MATCH_RE      (regex match)
		   - !~ "err.*"  → NOT_MATCH_RE  (negative regex match)

		   %3 = MAKETABLE [selector=%1, predicates=[%2], shard=0_of_1]
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   Defines the DATA SOURCE for this query.

		   Parameters:
		   - selector=%1: Use predicate %1 to filter streams (only {app="test"})
		   - predicates=[%2]: Pass line filter as HINT for storage pushdown
		   - shard=0_of_1: Query sharding info (0_of_1 means no sharding)

		   IMPORTANT: predicates are HINTS, not guarantees!
		   The storage layer MAY use them to skip data early, but the query
		   engine will still apply the filter explicitly later for correctness.

		   %4 = GTE builtin.timestamp 1970-01-01T00:16:40Z
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   Creates a PREDICATE for the query start time.

		   Key details:
		   - GTE = Greater Than or Equal
		   - Timestamps are in UTC
		   - The time corresponds to Unix timestamp 1000 (from q.start)

		   %5 = SELECT %3 [predicate=%4]
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   FILTERS the data source %3 by predicate %4.

		   SELECT is like SQL WHERE - it keeps rows where the predicate is TRUE.
		   After this, we only have data with timestamp >= start time.

		   %6 = LT builtin.timestamp 1970-01-01T00:33:20Z
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   Creates a PREDICATE for the query end time.

		   Key details:
		   - LT = Less Than (exclusive end time)
		   - This is a half-open interval: [start, end)

		   %7 = SELECT %5 [predicate=%6]
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   Applies the end time filter.
		   After this, we only have data in [start, end) time range.

		   %8 = SELECT %7 [predicate=%2]
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   Applies the line filter AGAIN.

		   WHY DUPLICATE? The line filter appears in BOTH:
		   1. MAKETABLE predicates (hint for storage optimization)
		   2. Explicit SELECT (guarantee correct results)

		   Storage might not support pushdown, so we always apply explicitly.

		   %9 = TOPK %8 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
		   SORTS and LIMITS the results.

		   Parameters:
		   - sort_by=builtin.timestamp: Sort by log entry time
		   - k=100: Return at most 100 results (from q.limit)
		   - asc=false: Descending order (newest first) for BACKWARD direction
		   - nulls_first=false: NULL timestamps sorted last

		   Direction mapping:
		   - BACKWARD → asc=false (newest first, descending)
		   - FORWARD  → asc=true  (oldest first, ascending)

		   %10 = LOGQL_COMPAT %9
		   ^^^^^^^^^^^^^^^^^^^^^^
		   Compatibility wrapper for LogQL V1 output format.

		   This ensures the output matches what LogQL V1 users expect:
		   - Proper column naming
		   - Correct data types
		   - Label formatting

		   RETURN %10
		   ^^^^^^^^^^^
		   Marks %10 as the final output of the query.

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{app="test"} |= "error"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "EQ label.app")
		require.Contains(t, planStr, "MATCH_STR builtin.message")
		require.Contains(t, planStr, "MAKETABLE")
		require.Contains(t, planStr, "SELECT")
		require.Contains(t, planStr, "TOPK")
		require.Contains(t, planStr, "LOGQL_COMPAT")
		require.Contains(t, planStr, "RETURN")
	})

	t.Run("multiple_label_matchers_with_AND", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Multiple Label Matchers with AND
		   ============================================================================

		   LogQL: {cluster="prod", namespace=~"loki-.*"}

		   This query demonstrates combining multiple label matchers.
		   ALL label matchers in {...} are combined with AND logic.

		   ============================================================================
		   NEW INSTRUCTION: AND
		   ============================================================================

		   AND combines two predicates with logical AND.

		   Syntax: %N = AND %A %B

		   Returns TRUE only if BOTH %A AND %B are TRUE.

		   Multiple label matchers are ALWAYS ANDed:
		   - {a="1", b="2"}     → AND(EQ a "1", EQ b "2")
		   - {a="1", b="2", c="3"} → AND(AND(EQ a "1", EQ b "2"), EQ c "3")

		   Note: LogQL does NOT support OR between label matchers in {...}.
		   For OR, use the | operator between separate selectors:
		     {a="1"} | {b="2"}  (but this is a different query structure)

		   ============================================================================
		   NEW INSTRUCTION: MATCH_RE
		   ============================================================================

		   MATCH_RE performs regular expression matching.

		   Syntax: %N = MATCH_RE <column> "<regex>"

		   Used for:
		   - Label regex matching: {namespace=~"loki-.*"}
		   - Line regex filtering: |~ "error|warn"

		   The regex syntax follows RE2 (Go's regex engine):
		   - No backreferences
		   - Linear time guarantee
		   - Full Unicode support

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.cluster "prod"
		        Equality match on cluster label.

		   %2 = MATCH_RE label.namespace "loki-.*"
		        Regex match on namespace label.
		        Matches: loki-read, loki-write, loki-backend, etc.

		   %3 = AND %1 %2
		        Combines both predicates: cluster="prod" AND namespace=~"loki-.*"
		        Only streams matching BOTH conditions are included.

		   %4 = MAKETABLE [selector=%3, predicates=[], shard=0_of_1]
		        Data source with combined selector.
		        predicates=[] because there are no line filters.

		   %5 = GTE builtin.timestamp <start>
		   %6 = SELECT %4 [predicate=%5]
		   %7 = LT builtin.timestamp <end>
		   %8 = SELECT %6 [predicate=%7]
		        Time range filtering (same pattern as before).

		   %9 = TOPK %8 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   %10 = LOGQL_COMPAT %9
		   RETURN %10
		        Standard sort, limit, and output.

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{cluster="prod", namespace=~"loki-.*"}`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "EQ label.cluster")
		require.Contains(t, planStr, "MATCH_RE label.namespace")
		require.Contains(t, planStr, "AND")
		require.Contains(t, planStr, "MAKETABLE")
	})

	t.Run("negative_line_filter", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Negative Line Filter
		   ============================================================================

		   LogQL: {app="test"} != "debug"

		   Demonstrates NEGATIVE line filtering - excluding lines that contain
		   the specified pattern.

		   ============================================================================
		   NEW INSTRUCTION: NOT_MATCH_STR
		   ============================================================================

		   NOT_MATCH_STR is the negation of MATCH_STR.

		   Syntax: %N = NOT_MATCH_STR <column> "<pattern>"

		   Returns TRUE if the column value does NOT contain the pattern.

		   Line filter operator mapping:
		   - |= "pattern"  → MATCH_STR      (line CONTAINS pattern)
		   - != "pattern"  → NOT_MATCH_STR  (line does NOT CONTAIN pattern)
		   - |~ "regex"    → MATCH_RE       (line MATCHES regex)
		   - !~ "regex"    → NOT_MATCH_RE   (line does NOT MATCH regex)

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		        Stream selector predicate.

		   %2 = NOT_MATCH_STR builtin.message "debug"
		        Negative substring match.
		        TRUE if log line does NOT contain "debug".

		   %3 = MAKETABLE [selector=%1, predicates=[%2], shard=0_of_1]
		        Data source with NOT_MATCH_STR as pushdown hint.

		   %4 = GTE builtin.timestamp <start>
		   %5 = SELECT %3 [predicate=%4]
		   %6 = LT builtin.timestamp <end>
		   %7 = SELECT %5 [predicate=%6]
		        Time range filtering.

		   %8 = SELECT %7 [predicate=%2]
		        Apply the negative line filter explicitly.
		        Same predicate %2 appears in MAKETABLE and SELECT.

		   %9 = TOPK %8 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   %10 = LOGQL_COMPAT %9
		   RETURN %10

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{app="test"} != "debug"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "NOT_MATCH_STR builtin.message")
	})

	t.Run("regex_line_filter", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Regex Line Filter
		   ============================================================================

		   LogQL: {app="test"} |~ "error|warn"

		   Demonstrates regex-based line filtering using |~ operator.

		   ============================================================================
		   MATCH_RE FOR LINE FILTERS
		   ============================================================================

		   MATCH_RE is used for both:
		   1. Label regex matching:  {namespace=~"loki-.*"}
		   2. Line regex filtering:  |~ "error|warn"

		   The column being matched determines the usage:
		   - label.<name>      → Label regex matching
		   - builtin.message   → Line regex filtering

		   Regex Best Practices:
		   - Keep patterns simple for better performance
		   - Avoid .* at start (forces scan of entire line)
		   - Use alternation (a|b) instead of lookahead
		   - Prefer literal prefixes when possible

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		        Stream selector.

		   %2 = MATCH_RE builtin.message "error|warn"
		        Regex match on log line.
		        Pattern "error|warn" matches lines containing either word.

		   %3 = MAKETABLE [selector=%1, predicates=[%2], shard=0_of_1]
		        Data source with regex filter as pushdown hint.

		   %4 = GTE builtin.timestamp <start>
		   %5 = SELECT %3 [predicate=%4]
		   %6 = LT builtin.timestamp <end>
		   %7 = SELECT %5 [predicate=%6]
		        Time range filtering.

		   %8 = SELECT %7 [predicate=%2]
		        Apply regex line filter.

		   %9 = TOPK %8 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   %10 = LOGQL_COMPAT %9
		   RETURN %10

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{app="test"} |~ "error|warn"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "MATCH_RE builtin.message")
	})

	t.Run("json_parsing_with_label_filter", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: JSON Parsing with Label Filter
		   ============================================================================

		   LogQL: {app="test"} | json | level="error"

		   This query demonstrates:
		   1. Parsing JSON from log lines
		   2. Filtering on parsed fields

		   ============================================================================
		   NEW INSTRUCTION: PROJECT
		   ============================================================================

		   PROJECT transforms the column set by adding, removing, or modifying columns.

		   Syntax: %N = PROJECT %input [mode=<mode>, expr=<expression>]

		   Modes:
		   - *E (Extend):  Add new columns while keeping all existing ones
		   - *D (Drop):    Remove specified columns, keep others
		   - *K (Keep):    Keep only specified columns (not yet implemented)
		   - *R (Replace): Replace column values (not yet implemented)

		   PROJECT is used for:
		   - Parsing: | json, | logfmt, | regexp, | pattern
		   - Dropping labels: | drop app
		   - Keeping labels: | keep app (not yet implemented)
		   - Formatting: | line_format, | label_format (not yet implemented)

		   ============================================================================
		   NEW EXPRESSION: PARSE_JSON
		   ============================================================================

		   PARSE_JSON extracts fields from JSON-formatted log lines.

		   Syntax: PARSE_JSON(builtin.message, [keys], strict, keep_strings)

		   Parameters:
		   - builtin.message: The log line to parse
		   - [keys]: Specific keys to extract ([] = all keys)
		   - strict: If true, fail on parse errors
		   - keep_strings: If true, keep values as strings

		   Output columns use "parsed." prefix:
		   - JSON: {"level": "error", "msg": "failed"}
		   - Columns: parsed.level, parsed.msg

		   ============================================================================
		   AMBIGUOUS COLUMN TYPE
		   ============================================================================

		   When filtering on a field like level="error", the planner uses
		   "ambiguous.level" because it doesn't know if "level" is:
		   - label.level (stream label)
		   - metadata.level (structured metadata)
		   - parsed.level (from json parser)

		   Physical planning resolves this based on:
		   1. Schema information from storage
		   2. Context from preceding operations (e.g., | json creates parsed.*)

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		        Stream selector.

		   %2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
		        Data source. predicates=[] because JSON parsing happens later.

		   %3 = GTE builtin.timestamp <start>
		   %4 = SELECT %2 [predicate=%3]
		   %5 = LT builtin.timestamp <end>
		   %6 = SELECT %4 [predicate=%5]
		        Time range filtering.

		   %7 = PROJECT %6 [mode=*E, expr=PARSE_JSON(builtin.message, [], false, false)]
		        Parse JSON from each log line:
		        - mode=*E: Extend - add parsed.* columns, keep existing columns
		        - expr=PARSE_JSON(...): The parsing expression
		        - []: Extract all JSON keys (not a subset)
		        - false, false: Not strict, don't force strings

		        After this, each row has new columns:
		        - parsed.level, parsed.msg, etc. (depends on JSON content)

		   %8 = EQ ambiguous.level "error"
		        Filter predicate on parsed field.
		        "ambiguous." because we don't know the exact column type yet.

		   %9 = SELECT %7 [predicate=%8]
		        Apply filter on parsed JSON field.

		   %10 = TOPK %9 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   %11 = LOGQL_COMPAT %10
		   RETURN %11

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{app="test"} | json | level="error"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "PROJECT")
		require.Contains(t, planStr, "PARSE_JSON")
		require.Contains(t, planStr, "ambiguous.level")
	})

	t.Run("logfmt_parsing", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Logfmt Parsing
		   ============================================================================

		   LogQL: {app="test"} | logfmt

		   Parses logfmt-formatted logs (key=value pairs separated by spaces).

		   ============================================================================
		   NEW EXPRESSION: PARSE_LOGFMT
		   ============================================================================

		   PARSE_LOGFMT extracts fields from logfmt-formatted log lines.

		   Syntax: PARSE_LOGFMT(builtin.message, [keys], strict, keep_strings)

		   Logfmt Format:
		     level=info msg="request completed" duration=42ms status=200

		   Results in columns:
		     parsed.level = "info"
		     parsed.msg = "request completed"
		     parsed.duration = "42ms"
		     parsed.status = "200"

		   Logfmt Parsing Rules:
		   - key=value pairs separated by whitespace
		   - Quoted values preserve spaces: msg="hello world"
		   - Unquoted values end at whitespace
		   - Keys cannot contain = or whitespace

		   ============================================================================
		   COMPARISON OPERATORS
		   ============================================================================

		   When filtering on parsed fields, these comparison operators are available:

		   Operator | SSA Instruction | Example
		   ---------|-----------------|------------------
		   ==       | EQ              | level="error"
		   !=       | NEQ             | level!="debug"
		   >        | GT              | duration>100ms
		   >=       | GTE             | duration>=100ms
		   <        | LT              | duration<1s
		   <=       | LTE             | duration<=1s
		   =~       | MATCH_RE        | level=~"err.*"
		   !~       | NOT_MATCH_RE    | level!~"debug"

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		        Stream selector.

		   %2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
		        Data source.

		   %3 = GTE builtin.timestamp <start>
		   %4 = SELECT %2 [predicate=%3]
		   %5 = LT builtin.timestamp <end>
		   %6 = SELECT %4 [predicate=%5]
		        Time range filtering.

		   %7 = PROJECT %6 [mode=*E, expr=PARSE_LOGFMT(builtin.message, [], false, false)]
		        Parse logfmt from each log line:
		        - mode=*E: Extend - add parsed.* columns
		        - expr=PARSE_LOGFMT(...): The parsing expression

		   %8 = TOPK %7 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   %9 = LOGQL_COMPAT %8
		   RETURN %9

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{app="test"} | logfmt`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "PROJECT")
		require.Contains(t, planStr, "PARSE_LOGFMT")
	})

	t.Run("line_format_unimplemented", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Line Format (line_format) - NOT YET IMPLEMENTED
		   ============================================================================

		   LogQL: {app="test"} | line_format "{{.message}}"

		   NOTE: This feature is NOT YET IMPLEMENTED in the V2 engine.

		   line_format transforms the log line using a Go template.

		   Expected behavior when implemented:
		   - Template syntax: {{.field}} accesses field values
		   - {{.message}} or {{__line__}} = original log line
		   - {{.level}} = value of level field
		   - Supports Go template functions (printf, etc.)

		   Example:
		     {app="test"} | json | line_format "{{.level}}: {{.msg}}"

		   Would transform:
		     {"level": "error", "msg": "failed"} → "error: failed"
		*/
		q := &mockQuery{
			statement: `{app="test"} | line_format "{{.app}}: {{__line__}}"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		_, err := logical.BuildPlan(q)
		require.Error(t, err, "line_format should not be implemented yet")
		require.Contains(t, err.Error(), "line_format")
		t.Logf("Expected error for unimplemented feature: %v", err)
	})

	t.Run("label_format_unimplemented", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Label Format (label_format) - NOT YET IMPLEMENTED
		   ============================================================================

		   LogQL: {app="test"} | label_format new_label="{{ .app }}_processed"

		   NOTE: This feature is NOT YET IMPLEMENTED in the V2 engine.

		   label_format creates or modifies labels using templates.

		   Difference from line_format:
		   - line_format: Modifies the log line content (builtin.message)
		   - label_format: Creates/modifies labels (affects stream grouping)
		*/
		q := &mockQuery{
			statement: `{app="test"} | label_format new_label="{{ .app }}_processed"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		_, err := logical.BuildPlan(q)
		require.Error(t, err, "label_format should not be implemented yet")
		require.Contains(t, err.Error(), "label_format")
		t.Logf("Expected error for unimplemented feature: %v", err)
	})

	t.Run("keep_labels_unimplemented", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Keep Labels (keep) - NOT YET IMPLEMENTED
		   ============================================================================

		   LogQL: {app="test"} | keep app

		   NOTE: This feature is NOT YET IMPLEMENTED in the V2 engine.

		   | keep specifies which labels to KEEP, dropping all others.

		   Useful for reducing stream cardinality by removing unnecessary labels.
		*/
		q := &mockQuery{
			statement: `{app="test"} | keep app`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		_, err := logical.BuildPlan(q)
		require.Error(t, err, "keep should not be implemented yet")
		require.Contains(t, err.Error(), "keep")
		t.Logf("Expected error for unimplemented feature: %v", err)
	})

	t.Run("drop_labels", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Drop Labels (drop)
		   ============================================================================

		   LogQL: {app="test"} | drop app

		   | drop removes specified labels while keeping all others.
		   This is the inverse of | keep.

		   ============================================================================
		   PROJECT WITH DROP MODE
		   ============================================================================

		   PROJECT mode=*D drops specified columns:

		   %N = PROJECT %input [mode=*D, expr=ambiguous.app]

		   - mode=*D: Drop mode
		   - expr=ambiguous.app: Column to drop

		   Uses "ambiguous." prefix because at logical planning time,
		   we don't know if "app" is a label, metadata, or parsed field.

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		        Stream selector.

		   %2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
		        Data source.

		   %3 = GTE builtin.timestamp <start>
		   %4 = SELECT %2 [predicate=%3]
		   %5 = LT builtin.timestamp <end>
		   %6 = SELECT %4 [predicate=%5]
		        Time range filtering.

		   %7 = PROJECT %6 [mode=*D, expr=ambiguous.app]
		        Drop the "app" label from results.
		        - mode=*D: Drop mode
		        - expr=ambiguous.app: The column to drop

		   %8 = TOPK %7 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   %9 = LOGQL_COMPAT %8
		   RETURN %9

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{app="test"} | drop app`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "PROJECT")
	})

	t.Run("multiple_pipeline_stages", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Multiple Pipeline Stages
		   ============================================================================

		   LogQL: {app="test"} |= "error" | json | level="error"

		   This query combines multiple pipeline stages:
		   1. Stream selector: {app="test"}
		   2. Line filter: |= "error"
		   3. JSON parser: | json
		   4. Label filter: | level="error"

		   ============================================================================
		   PIPELINE STAGE EXECUTION ORDER
		   ============================================================================

		   Pipeline stages execute in order, each transforming the data stream:

		   Raw logs → |= "error" → | json → | level="error" → Results
		              (filter)     (parse)   (filter)

		   Each stage can:
		   - Filter rows (SELECT)
		   - Add columns (PROJECT with PARSE_*)
		   - Remove columns (PROJECT mode=*D)
		   - Transform values (PROJECT with expressions)

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		        Stream selector.

		   %2 = MATCH_STR builtin.message "error"
		        Line filter predicate.

		   %3 = MAKETABLE [selector=%1, predicates=[%2], shard=0_of_1]
		        Data source with line filter as pushdown hint.

		   %4 = GTE builtin.timestamp <start>
		   %5 = SELECT %3 [predicate=%4]
		   %6 = LT builtin.timestamp <end>
		   %7 = SELECT %5 [predicate=%6]
		        Time range filtering.

		   %8 = SELECT %7 [predicate=%2]
		        Apply line filter |= "error".

		   %9 = PROJECT %8 [mode=*E, expr=PARSE_JSON(builtin.message, [], false, false)]
		        Parse JSON from log lines.

		   %10 = EQ ambiguous.level "error"
		         Predicate for label filter.

		   %11 = SELECT %9 [predicate=%10]
		         Apply label filter on parsed field.

		   %12 = TOPK %11 [sort_by=builtin.timestamp, k=100, asc=false, nulls_first=false]
		   %13 = LOGQL_COMPAT %12
		   RETURN %13

		   ============================================================================
		   STAGE-TO-SSA MAPPING
		   ============================================================================

		   LogQL Stage           | SSA Instructions
		   ----------------------|---------------------------
		   {app="test"}          | %1 EQ, %3 MAKETABLE
		   |= "error"            | %2 MATCH_STR, %8 SELECT
		   | json                | %9 PROJECT with PARSE_JSON
		   | level="error"       | %10 EQ, %11 SELECT
		   (implicit sort/limit) | %12 TOPK
		   (implicit compat)     | %13 LOGQL_COMPAT

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `{app="test"} |= "error" | json | level="error"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "MATCH_STR")
		require.Contains(t, planStr, "PARSE_JSON")
		require.Contains(t, planStr, "ambiguous.")
		require.Contains(t, planStr, "PROJECT")
		require.Contains(t, planStr, "TOPK")
	})

	t.Run("decolorize_unimplemented", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Decolorize Filter - NOT YET IMPLEMENTED
		   ============================================================================

		   LogQL: {app="test"} | decolorize

		   NOTE: This feature is NOT YET IMPLEMENTED in the V2 engine.

		   | decolorize removes ANSI color codes from log lines.
		   Useful when logs contain terminal color escape sequences.
		*/
		q := &mockQuery{
			statement: `{app="test"} | decolorize`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		_, err := logical.BuildPlan(q)
		require.Error(t, err, "decolorize should not be implemented yet")
		t.Logf("Expected error for unimplemented feature: %v", err)
	})

	t.Run("forward_direction_unimplemented", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Forward Direction - NOT YET IMPLEMENTED
		   ============================================================================

		   LogQL: {app="test"} (with FORWARD direction)

		   NOTE: Forward direction for log queries is NOT YET IMPLEMENTED.

		   Direction affects TOPK sort order:
		   - BACKWARD: asc=false (newest first, descending)
		   - FORWARD:  asc=true  (oldest first, ascending)
		*/
		q := &mockQuery{
			statement: `{app="test"}`,
			start:     1000,
			end:       2000,
			direction: logproto.FORWARD,
			limit:     100,
		}

		_, err := logical.BuildPlan(q)
		require.Error(t, err, "forward direction should not be implemented yet")
		require.Contains(t, err.Error(), "forward")
		t.Logf("Expected error for unimplemented feature: %v", err)
	})
}

/*
TestLogicalPlanning_MetricQuery demonstrates logical plan generation for metric queries.

Metric queries differ from log queries - they return numeric time series
instead of log lines. The SSA structure reflects these differences.
*/
func TestLogicalPlanning_MetricQuery(t *testing.T) {
	t.Run("count_over_time_with_sum_aggregation", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Metric Query with Range and Vector Aggregation
		   ============================================================================

		   LogQL: sum by (level) (count_over_time({app="test"}[5m]))

		   This is a typical metric query that:
		   1. Counts log lines over 5-minute windows
		   2. Groups and sums the counts by the "level" label

		   ============================================================================
		   NEW INSTRUCTION: RANGE_AGGREGATION
		   ============================================================================

		   RANGE_AGGREGATION aggregates data over sliding time windows.

		   Syntax: %N = RANGE_AGGREGATION %input [operation=<op>, start_ts=<t>,
		                                          end_ts=<t>, step=<d>, range=<d>]

		   Operations:
		   - count: Count log lines (count_over_time)
		   - sum: Sum values (sum_over_time)
		   - avg: Average (avg_over_time)
		   - min: Minimum (min_over_time)
		   - max: Maximum (max_over_time)
		   - first: First value (first_over_time)
		   - last: Last value (last_over_time)
		   - bytes: Sum of bytes (bytes_over_time)
		   - absent: 1 if no data (absent_over_time)
		   - stdvar: Variance (stdvar_over_time)
		   - stddev: Std deviation (stddev_over_time)
		   - quantile: Percentile (quantile_over_time)

		   Parameters:
		   - start_ts: Query start time (actual, not lookback-adjusted)
		   - end_ts: Query end time
		   - step: Evaluation interval (0 for instant queries)
		   - range: Window size (e.g., 5m from [5m])

		   ============================================================================
		   NEW INSTRUCTION: VECTOR_AGGREGATION
		   ============================================================================

		   VECTOR_AGGREGATION aggregates across label groups (series).

		   Syntax: %N = VECTOR_AGGREGATION %input [operation=<op>,
		                                           group_by=(<labels>)]

		   Operations:
		   - sum: Sum values
		   - avg: Average
		   - min: Minimum
		   - max: Maximum
		   - count: Count series
		   - stdvar: Variance
		   - stddev: Std deviation
		   - bottomk: K smallest values
		   - topk: K largest values
		   - sort: Sort ascending
		   - sort_desc: Sort descending

		   Grouping:
		   - by (label): Group by specified labels
		   - without (label): Group by all labels EXCEPT specified

		   ============================================================================
		   TIME LOOKBACK ADJUSTMENT
		   ============================================================================

		   For metric queries, the start time is adjusted BACKWARD by the range:

		   Query: count_over_time({app="test"}[5m])
		   Query time range: [01:00:00, 02:00:00)

		   Start time in SSA: 00:55:00 (5 minutes before query start)

		   WHY? The first evaluation at 01:00:00 needs data from
		   00:55:00 to 01:00:00 to compute the 5-minute count.

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		        Stream selector.

		   %2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
		        Data source. predicates=[] because count_over_time
		        counts ALL lines (no filtering).

		   %3 = GTE builtin.timestamp 1970-01-01T00:55:00Z
		        Start time with 5-minute LOOKBACK adjustment.
		        Query start: 01:00:00 (Unix 3600)
		        SSA start: 00:55:00 (Unix 3600 - 300)

		   %4 = SELECT %2 [predicate=%3]
		        Apply lookback-adjusted start time.

		   %5 = LT builtin.timestamp 1970-01-01T02:00:00Z
		        End time (no adjustment needed).

		   %6 = SELECT %4 [predicate=%5]
		        Apply end time filter.

		   %7 = RANGE_AGGREGATION %6 [operation=count, start_ts=..., end_ts=...,
		                              step=0s, range=5m0s]
		        Count log lines in each 5-minute window.
		        - operation=count: Count lines
		        - step=0s: Instant query (single evaluation)
		        - range=5m0s: Window size from [5m]

		   %8 = VECTOR_AGGREGATION %7 [operation=sum, group_by=(ambiguous.level)]
		        Sum counts by level label.
		        - operation=sum: Sum the counts
		        - group_by=(ambiguous.level): Group by "level"

		   %9 = LOGQL_COMPAT %8
		   RETURN %9

		   ============================================================================
		   KEY DIFFERENCES FROM LOG QUERIES
		   ============================================================================

		   1. NO TOPK: Metric queries return aggregated values, not sorted logs.

		   2. TIME LOOKBACK: Start time adjusted backward by range duration.

		   3. RANGE_AGGREGATION: Aggregates over time windows (produces metrics).

		   4. VECTOR_AGGREGATION: Aggregates across series (reduces cardinality).

		   5. EMPTY PREDICATES: count_over_time counts all lines.

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `sum by (level) (count_over_time({app="test"}[5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Metric Query Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "MAKETABLE")
		require.Contains(t, planStr, "RANGE_AGGREGATION")
		require.Contains(t, planStr, "VECTOR_AGGREGATION")
		require.Contains(t, planStr, "operation=count")
		require.Contains(t, planStr, "operation=sum")
		require.Contains(t, planStr, "group_by")
	})

	t.Run("rate_query_with_division", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Rate Query with Division
		   ============================================================================

		   LogQL: sum by (level) (rate({app="test"}[5m]))

		   rate() computes the per-second rate of log lines.

		   Internally: rate = count_over_time / range_seconds
		   For [5m]: rate = count / 300

		   ============================================================================
		   NEW INSTRUCTION: DIV
		   ============================================================================

		   DIV performs arithmetic division.

		   Syntax: %N = DIV %numerator <denominator>

		   Used for:
		   - rate() = count_over_time / range_seconds
		   - bytes_rate() = bytes_over_time / range_seconds
		   - Custom arithmetic operations

		   Other arithmetic operations:
		   - ADD: Addition
		   - SUB: Subtraction
		   - MUL: Multiplication
		   - DIV: Division
		   - MOD: Modulo
		   - POW: Power

		   ============================================================================
		   RATE vs COUNT_OVER_TIME
		   ============================================================================

		   count_over_time({app="test"}[5m]):
		     Returns: Total log lines in 5-minute window
		     Example: 600 lines

		   rate({app="test"}[5m]):
		     Returns: Lines per second (count / 300)
		     Example: 600/300 = 2.0 lines/second

		   Rate normalizes counts to per-second values, making it easier
		   to compare across different time ranges or set thresholds.

		   ============================================================================
		   SSA OUTPUT:
		   ============================================================================

		   %1 = EQ label.app "test"
		   %2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
		   %3 = GTE builtin.timestamp <lookback_start>
		   %4 = SELECT %2 [predicate=%3]
		   %5 = LT builtin.timestamp <end>
		   %6 = SELECT %4 [predicate=%5]

		   %7 = RANGE_AGGREGATION %6 [operation=count, ...]
		        Count log lines (numerator for rate).

		   %8 = DIV %7 300
		        Divide by range seconds (5m = 300s).
		        Converts count to per-second rate.

		   %9 = VECTOR_AGGREGATION %8 [operation=sum, group_by=(ambiguous.level)]
		        Sum rates by level.

		   %10 = LOGQL_COMPAT %9
		   RETURN %10

		   ============================================================================
		*/
		q := &mockQuery{
			statement: `sum by (level) (rate({app="test"}[5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		t.Logf("Rate Query Logical Plan:\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "DIV", "rate should have division")
	})
}
