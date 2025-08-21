# TDD Execution Plan: Logfmt for Metric Queries in Loki v2 Engine

## Overview
This document provides a test-driven development plan for implementing logfmt parsing in the v2 engine. Each section represents a logical chunk of functionality that can be implemented independently. Within each section, behaviors are broken down into small, testable units.

## TDD Workflow Reminders
- **Every test should focus on observable behavior, not structure or types**
- **Every test should be run before implementation and should fail for the correct reason**
- **A good failure occurs because the behavior is missing—not because of superficial errors**
- **After implementation, the test should be re-run and pass**
- **PAUSE after each test fails correctly for review before implementation**
- **PAUSE after each implementation for review before next cycle**
- **Tests should NOT be modified during implementation unless absolutely necessary**

---

## Section 1: Key Collection from AST ✅ COMPLETE

### 1.1 Collect Keys from Simple Filter Expression

**Behavior**: When a logfmt parser is followed by a label filter like `level="error"`, the system should identify that "level" needs to be extracted.

**Test** (NEW):
- File: `pkg/engine/planner/logical/key_collector_test.go`
- Create AST with: `{app="test"} | logfmt | level="error"`
- Assert that collected keys = ["level"]

**Expected Failure**: No key collector exists yet, function returns empty slice

**Implementation**:
- Create `key_collector.go` with `CollectRequestedKeys(expr syntax.Expr) []string`
- Implement visitor pattern to walk AST from logfmt node
- When encountering LabelFilterExpr, extract the label name

### 1.2 Collect Keys from Multiple Filters

**Behavior**: Multiple label filters should collect all referenced keys.

**Test** (NEW):
- Same file as 1.1
- Create AST: `{app="test"} | logfmt | level="error" | status=200`
- Assert collected keys = ["level", "status"] (sorted)

**Expected Failure**: Visitor stops after first filter

**Implementation**:
- Continue walking AST through all filter expressions
- Maintain a set to avoid duplicates, return sorted slice

### 1.3 Collect Keys from Unwrap Expression

*Behavior**: When unwrap references a parsed field, that field should be collected.

**Test** (NEW):
- Create AST: `{app="test"} | logfmt | unwrap bytes | sum_over_time([5m])`
- Assert collected keys = ["bytes"]
- Assert numeric hints contains "bytes" → Int64 (or appropriate type)

**Expected Failure**: Unwrap expressions not handled

**Implementation**:
- Add case for UnwrapExpr in visitor
- Extract unwrap target field
- Mark field in NumericHints map

### 1.4 Collect Keys from Vector Aggregation GroupBy

**Behavior**: GroupBy clauses in vector aggregations should collect referenced parsed fields.

**Test** (NEW):
- Create AST: `sum by (app, region) ({} | logfmt | count_over_time([5m]))`
- Assume "region" comes from logfmt, "app" from labels
- Assert collected keys contains "region"

**Expected Failure**: Vector aggregation grouping not traversed

**Implementation**:
- Visit VectorAggregationExpr
- Check groupBy labels against known stream labels
- Include only those that would come from parsing

### 1.5 Skip Collection for Non-Metric Queries

**Behavior**: For log queries (non-metric), key collection should identify this and return a special marker.

**Test** (NEW):
- Create AST: `{app="test"} | logfmt | level="error"` (no metric aggregation)
- Assert that collector returns nil or special "not-a-metric-query" indicator

**Expected Failure**: No differentiation between log and metric queries

**Implementation**:
- Detect presence of range/vector aggregation in AST
- Return nil or empty result for non-metric queries

---

## Section 2: Logical Plan Parse Node ✅ COMPLETE

### 2.1 Create Basic Parse Instruction ✅

**Behavior**: A Parse instruction should be creatable with parser type and requested keys.

**Test** (NEW):
- File: `pkg/engine/planner/logical/node_parse_test.go`
- Create Parse{Kind: ParserLogfmt, RequestedKeys: ["level", "status"]}
- Assert instruction can be added to logical plan
- Assert plan.Instructions contains the Parse node

**Expected Failure**: Parse type doesn't exist

**Implementation**:
- Create `node_parse.go` with Parse struct
- Add ParserKind enum (ParserLogfmt, ParserJSON)
- Implement Value interface methods

### 2.2 Builder Adds Parse After MakeTable ✅

**Behavior**: Builder should insert Parse instruction in correct position after MakeTable.

**Test** (NEW):
- File: `pkg/engine/planner/logical/builder_test.go`
- Use builder to create: MakeTable → Parse → Select
- Assert instruction order is correct

**Expected Failure**: No Parse method on Builder

**Implementation**:
- Add Parse method to Builder
- Method adds Parse instruction and returns new Builder

### 2.3 Planner Creates Parse from LogfmtParserExpr ✅

**Behavior**: When planner encounters LogfmtParserExpr in AST, it should create Parse instruction with collected keys.

**Test** (MODIFY EXISTING):
- File: `pkg/engine/planner/logical/planner_test.go`
- Add test case with logfmt in metric query
- Assert Parse instruction exists with correct keys

**Expected Failure**: LogfmtParserExpr case not handled

**Implementation**:
- Add case in buildPlanForLogQuery for LogfmtParserExpr
- Call key collector
- Insert Parse instruction if keys found

---

## Section 3: Physical Plan Conversion ✅ COMPLETE

### 3.1 Convert Logical Parse to Physical ParseNode

**Behavior**: Logical Parse instruction should convert to physical ParseNode.

**Test** (NEW):
- File: `pkg/engine/planner/physical/parse_test.go`
- Create logical plan with Parse instruction
- Convert to physical plan
- Assert physical plan contains ParseNode with same keys

**Expected Failure**: ParseNode type doesn't exist

**Implementation**:
- Create `parse.go` with ParseNode struct
- Add NodeTypeParse to enum (fix typo in RangeAggregation too)
- Implement Node interface methods

### 3.2 Physical Planner Processes Parse Instruction

**Behavior**: Physical planner should handle Parse instruction during conversion.

**Test** (MODIFY EXISTING):
- File: `pkg/engine/planner/physical/planner_test.go`
- Add test with Parse in logical plan
- Assert physical plan has ParseNode in correct position

**Expected Failure**: No case for Parse in planner.process()

**Implementation**:
- Add case for logical.Parse in process() method
- Create ParseNode with keys from instruction
- Connect to parent/child nodes correctly

### 3.3 Visitor Can Visit ParseNode

**Behavior**: ParseNode should be visitable in physical plan traversal.

**Test** (NEW):
- Create visitor that counts ParseNodes
- Visit plan with ParseNode
- Assert count = 1

**Expected Failure**: No VisitParse method in Visitor interface

**Implementation**:
- Add VisitParse to Visitor interface
- Implement Accept method on ParseNode
- Update printer/optimizer visitors

---

## Section 4: Basic Logfmt Tokenization

### 4.1 Tokenize Simple Key-Value Pair

**Behavior**: Tokenizer should extract key and value from "key=value".

**Test** (NEW):
- File: `pkg/engine/executor/logfmt_tokenizer_test.go`
- Input: "level=error"
- Assert: yields ("level", "error")

**Expected Failure**: Tokenizer doesn't exist

**Implementation**:
- Create `logfmt_tokenizer.go`
- Implement basic tokenization without quotes/escaping
- Use existing logfmt decoder as reference

### 4.2 Tokenize Multiple Pairs

**Behavior**: Tokenizer should extract all key-value pairs from line.

**Test** (NEW):
- Input: "level=error status=500 msg=failed"
- Assert: yields [("level","error"), ("status","500"), ("msg","failed")]

**Expected Failure**: Tokenizer stops after first pair

**Implementation**:
- Continue parsing until end of line
- Handle whitespace between pairs

### 4.3 Handle Quoted Values

**Behavior**: Tokenizer should handle quoted values with spaces.

**Test** (NEW):
- Input: `msg="hello world" level=error`
- Assert: yields [("msg","hello world"), ("level","error")]

**Expected Failure**: Quotes not handled

**Implementation**:
- Detect quote after '='
- Read until closing quote
- Handle escape sequences

### 4.4 Handle Missing Values

**Behavior**: Keys without values should be skipped or marked as empty.

**Test** (NEW):
- Input: "level= status=200"
- Assert: level has empty value or is skipped
- Assert: status="200" is extracted

**Expected Failure**: Empty values not handled

**Implementation**:
- Check for empty value after '='
- Decide on skip vs empty string based on keepEmpty flag

### 4.5 Last-Wins for Duplicate Keys

**Behavior**: When same key appears multiple times, last value should win.

**Test** (NEW):
- Input: "level=debug level=error level=info"
- Assert: level="info" (last value)

**Expected Failure**: First or all values returned

**Implementation**:
- Overwrite previous value when key seen again
- Track in map during tokenization

---

## Section 5: Arrow Column Building ✅ COMPLETE

### 5.1 Build Single String Column

**Behavior**: Should build Arrow utf8 column from extracted values.

**Test** (NEW):
- File: `pkg/engine/executor/parse_logfmt_test.go`
- Input: ["level=error", "level=info", "level=debug"]
- Requested keys: ["level"]
- Assert: Arrow column contains ["error", "info", "debug"]

**Expected Failure**: No Arrow builder logic

**Implementation**:
- Use array.StringBuilder
- Append values in order
- Build and return column

### 5.2 Handle Missing Keys with NULL

**Behavior**: Missing keys should produce NULL values in Arrow column.

**Test** (NEW):
- Input: ["level=error", "status=200", "level=info"]
- Requested keys: ["level"]
- Assert: Column = ["error", NULL, "info"] with proper null bitmap

**Expected Failure**: Missing keys not handled

**Implementation**:
- Check if key found during tokenization
- Call AppendNull() when key missing
- Ensure null bitmap is correct

### 5.3 Build Multiple Columns

**Behavior**: Should build multiple columns for multiple requested keys.

**Test** (NEW):
- Input: ["level=error status=500", "level=info"]
- Requested keys: ["level", "status"]
- Assert: Two columns with correct values and NULLs

**Expected Failure**: Single column logic

**Implementation**:
- Create StringBuilder for each requested key
- Fill all builders during single pass
- Return columns in deterministic order

### 5.4 Early Stop When All Keys Found

**Behavior**: Tokenization should stop early when all requested keys are found.

**Test** (NEW):
- Input: "a=1 b=2 c=3 d=4 e=5 f=6"
- Requested keys: ["a", "b"]
- Assert: Tokenizer doesn't process past "b=2"
- (Test by adding counter in tokenizer)

**Expected Failure**: Processes entire line

**Implementation**:
- Track found count
- Break when foundCount == len(requestedKeys)
- Maintain found set to avoid duplicates

---

## Section 6: Parse Executor Integration ✅ COMPLETE

### 6.1 Execute ParseNode in Pipeline ✅

**Behavior**: Executor should create parse stage when encountering ParseNode.

**Test** (NEW):
- File: `pkg/engine/executor/executor_test.go`
- Create physical plan with ParseNode
- Execute plan
- Assert pipeline contains parse stage

**Expected Failure**: No case for ParseNode

**Implementation**:
- Add case in execute() for ParseNode
- Create LogfmtParseExecutor
- Return as pipeline stage

### 6.2 Parse Stage Transforms Records ✅

**Behavior**: Parse stage should add columns to Arrow records.

**Test** (NEW):
- Input record with "message" column: ["level=error", "level=info"]
- ParseNode requests ["level"]
- Assert output has "message" and "level" columns

**Expected Failure**: No record transformation

**Implementation**:
- Implement Pipeline interface
- Read message column
- Parse and append new columns
- Return expanded record

### 6.3 Preserve Existing Columns ✅

**Behavior**: Parse stage should preserve all existing columns.

**Test** (NEW):
- Input: record with "timestamp", "message", "app" columns
- After parse: should have all original + parsed columns

**Expected Failure**: Original columns lost

**Implementation**:
- Copy all input columns to output
- Append only new parsed columns
- Maintain column order

---

## Section 7: Column Resolution & Binding ✅

### 7.1 Register Parsed Columns in Catalog ✅

**Behavior**: Parsed columns should be registered for expression resolution.

**Test** (NEW): ✅
- File: `pkg/engine/planner/physical/catalog_test.go`
- Add parsed column "level" to catalog
- Resolve identifier "level"
- Assert resolves to ColumnTypeParsed

**Expected Failure**: Parsed columns not in catalog ✅

**Implementation**: ✅
- Update catalog to track parsed columns
- Register during planning phase
- Set correct ColumnType

### 7.2 Resolve Parsed Columns in Filter Expressions ✅

**Behavior**: Filter expressions should resolve parsed column references.

**Test** (NEW): ✅
- Create filter: level="error"
- With "level" as parsed column
- Assert expression has ColumnExpr with ColumnTypeParsed

**Expected Failure**: Identifier not resolved ✅

**Implementation**: ✅
- Update expression builder
- Check parsed columns during resolution
- Create appropriate ColumnExpr

### 7.3 Ambiguous Column Resolution ✅

**Behavior**: When column exists as both label and parsed, parsed should win.

**Test** (NEW): ✅
- Register "app" as both label and parsed column
- Resolve "app"
- Assert resolves to parsed (higher precedence)

**Expected Failure**: Wrong precedence ✅

**Implementation**: ✅
- Use ColumnTypePrecedence
- Parsed has higher precedence than Label
- Return highest precedence match

### 7.4 Parse Node Registers Columns During Planning ✅

**Behavior**: When the physical planner creates a ParseNode, it should register the parsed columns with the column registry so downstream operations can resolve them.

**Test** (NEW): ✅
- Create logical plan with Parse instruction followed by Select with filter
- During physical planning, Parse should register its columns
- Filter expressions in Select should resolve parsed columns correctly
- Assert end-to-end: Parse → registered columns → resolved filter

**Expected Failure**: ParseNode doesn't interact with registry ✅

**Implementation**: ✅
- Physical planner maintains a ColumnRegistry during planning
- Pre-pass traversal to register all columns before conversion
- When processing Parse instruction, register requested keys
- Pass registry to expression resolver for downstream nodes
- Ensure registry is threaded through planning context

---

## Section 8: Numeric Unwrap Support

### 8.1 Cast String Column to Int64

**Behavior**: Unwrap should cast string column to numeric when needed.

**Test** (NEW):
- File: `pkg/engine/executor/cast_test.go`
- Input: string column ["100", "200", "abc"]
- Cast to int64
- Assert: [100, 200, NULL]

**Expected Failure**: No cast operation

**Implementation**:
- Create CastNode/executor
- Use strconv.ParseInt
- Return NULL for parse errors

### 8.2 Insert Cast Node After Parse

**Behavior**: When unwrap targets parsed field, planner should insert cast.

**Test** (NEW):
- Logical plan: Parse → Unwrap(bytes)
- Physical plan should have: ParseNode → CastNode → RangeAggregation
- With bytes marked for casting

**Expected Failure**: No cast inserted

**Implementation**:
- Detect numeric hints in Parse
- Insert CastNode for numeric fields
- Connect in plan correctly

### 8.3 Cast String Column to Float64

**Behavior**: Float unwrap should cast to float64.

**Test** (NEW):
- Input: string column ["1.5", "2.7", "NaN"]
- Cast to float64
- Assert: [1.5, 2.7, NULL or NaN]

**Expected Failure**: Only int64 supported

**Implementation**:
- Add float64 case
- Use strconv.ParseFloat
- Handle special float values

---

## Section 9: Aggregation Integration

### 9.1 Range Aggregation Over Parsed Numeric

**Behavior**: sum_over_time should work on parsed numeric field.

**Test** (INTEGRATION):
- File: `pkg/engine/engine_test.go`
- Query: `{} | logfmt | unwrap bytes | sum_over_time([5m])`
- Input logs with bytes=100, bytes=200
- Assert sum = 300

**Expected Failure**: Aggregation can't find numeric column

**Implementation**:
- Ensure cast column has correct name
- Range aggregation finds column
- Performs sum correctly

### 9.2 Vector Aggregation Groups By Parsed Label

**Behavior**: Vector aggregation should group by parsed string field.

**Test** (INTEGRATION):
- Query: `sum by (region) ({} | logfmt | unwrap bytes | sum_over_time([5m]))`
- Logs: region=us bytes=100, region=eu bytes=200, region=us bytes=50
- Assert: us=150, eu=200

**Expected Failure**: Grouping column not found

**Implementation**:
- Ensure parsed labels available for grouping
- Vector aggregation recognizes parsed columns
- Groups correctly

### 9.3 Count Over Time with Parsed Filter

**Behavior**: count_over_time should count only matching parsed values.

**Test** (INTEGRATION):
- Query: `{} | logfmt | level="error" | count_over_time([5m])`
- Mix of level=error and level=info logs
- Assert: counts only error logs

**Expected Failure**: Filter not applied

**Implementation**:
- Ensure filter executes after parse
- Filter evaluates parsed column
- Count reflects filtered results

---

## Section 10: Error Handling & v1 Compatibility ✅ COMPLETE

### 10.1 Preserve v1 Error Column Names

**Behavior**: The v2 parser should preserve the `__error__` and `__error_details__` column naming convention from v1 for compatibility with existing queries and dashboards.

**Integration with v2 Engine**:
- When parsing errors occur, they should be collected in a special column
- The column should be named `__error__` to match v1 behavior
- Error details should use the same format as v1 for consistency

**v1 Error Name Mapping**:
The v2 tokenizer needs to match v1 error names:
- "malformed key-value pair" → v1 uses specific error text
- "invalid UTF-8" → v1 error format
- "unclosed quote" → v1 error format
- "quote in key" → v1 may have different handling

**Test Requirements**:
- Compare error messages between v1 and v2 for same input
- Ensure `__error__` column is created when errors occur
- Verify `__error_details__` contains position information

**Implementation Notes**:
- The tokenizer currently collects errors with position information
- These errors need to be formatted to match v1's error strings
- Selective error reporting (only for requested keys) may differ from v1
- Structural errors (unclosed quotes) are always reported

**✅ Implemented**:
- Updated `logfmt_tokenizer.go` to directly produce v1-compatible error messages:
  - Double equals: "logfmt syntax error at pos X : unexpected '='"
  - Quote in key: "logfmt syntax error at pos X : unexpected '\"'" or "unexpected '''" 
  - Invalid UTF-8 in key: "logfmt syntax error at pos X : invalid key"
  - Unclosed quote: "logfmt syntax error at pos X : unterminated quoted value"
  - Invalid UTF-8 in value: "logfmt syntax error : invalid UTF-8 in value for key 'X'"
- Error label will need to be set to "LogfmtParserErr" when integrating with v2 engine
- Updated all tests in `logfmt_tokenizer_test.go` to expect v1 error formats

### 10.2 Parse Error Column in Columnar Format

**Behavior**: In the v2 engine's columnar format, parse errors should populate the `__error__` column while still returning partial results in other columns.

**Column Behavior**:
```
Input: "level=info bad==value status=200"
Columns:
  level:     ["info"]
  bad:       [""]        // Empty value due to error
  status:    ["200"]
  __error__: ["malformed input: key 'bad' has unexpected '==' at position 11"]
```

**Selective Error Reporting**:
- When specific keys are requested, only report errors for those keys
- When no keys specified (get all), report all errors
- Structural errors (unclosed quotes) always reported regardless

---

## Section 11: Fallback & Feature Gating

### 11.1 Fallback for Line Format

**Behavior**: Queries with line_format after logfmt should fallback to v1.

**Test** (NEW):
- File: `pkg/engine/planner/logical/fallback_test.go`
- Query: `{} | logfmt | line_format "{{.level}}"`
- Assert: planner returns fallback indicator

**Expected Failure**: No fallback detection

**Implementation**:
- Detect line_format in AST after logfmt
- Return special fallback error/flag
- Engine routes to v1

### 11.2 Fallback for Unknown Keys

**Behavior**: When key collection returns empty/unknown, should fallback.

**Test** (NEW):
- Query with dynamic key references
- Assert: fallback triggered

**Expected Failure**: Attempts to parse with no keys

**Implementation**:
- Check if RequestedKeys empty
- Trigger fallback
- Clear error message

### 11.3 Feature Gate Disables v2 Logfmt

**Behavior**: When feature flag off, all logfmt queries use v1.

**Test** (NEW):
- Set EnableV2LogfmtMetrics=false
- Any logfmt metric query
- Assert: uses v1 engine

**Expected Failure**: No feature flag

**Implementation**:
- Add config flag
- Check early in planner
- Skip v2 logic when disabled

### 11.4 Non-Metric Queries Always Use v1

**Behavior**: Log queries (non-metric) should use v1 even with flag on.

**Test** (NEW):
- Query: `{} | logfmt | level="error"` (no aggregation)
- With flag enabled
- Assert: uses v1

**Expected Failure**: Tries v2 for log query

**Implementation**:
- Detect metric vs log query
- Force v1 for log queries
- Independent of feature flag

---

## Execution Notes

1. **Work through sections sequentially** - Each section builds on the previous
2. **One test at a time** - Write test, see it fail correctly, implement, see it pass
3. **Pause points** - After each test failure and after each implementation
4. **Commit frequently** - After each passing test or logical group
5. **Run existing tests** - Ensure no regressions with each change
6. **Integration last** - Sections 1-8 are mostly unit tests, 9-10 are integration

## Success Criteria

- All tests pass
- v2 logfmt matches v1 behavior for supported queries
- Clean fallback for unsupported queries  
- Performance improvement measurable
- No regressions in existing functionality
