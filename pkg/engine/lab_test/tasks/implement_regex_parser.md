# Task: Implement Regex Parser (OpParserTypeRegexp) in V2 Engine

## Overview

The regex parser (`| regexp "..."`) allows extracting fields from log lines using regular expressions with named capture groups. This is a commonly used parser that needs to be implemented in the V2 query engine.

### Example Query

```logql
{app="foo"} | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)"
```

Applied to a log line like:
```
POST /api/prom/api/v1/query_range (200) 1.5s
```

Would extract:
```
method   => "POST"
path     => "/api/prom/api/v1/query_range"
status   => "200"
duration => "1.5s"
```

---

## Reusable Code Analysis

### Grafana's Optimized Regex Package

The old engine uses `github.com/grafana/regexp` which is an optimized fork of Go's standard regexp package. This package is already a dependency and should be reused.

**Import:**
```go
import "github.com/grafana/regexp"
```

### Old Engine's RegexpParser Implementation

Location: [`pkg/logql/log/parser.go:285-359`](pkg/logql/log/parser.go:285-359)

The old engine's `RegexpParser` implementation can be used as reference:

```go
type RegexpParser struct {
    regex     *regexp.Regexp
    nameIndex map[int]string  // Maps capture group index to label name
    keys      internedStringSet
}

// NewRegexpParser creates a new log stage that can extract labels from a log line using a regex expression.
// The regex expression must contain at least one named match. If the regex doesn't match the line is not filtered out.
func NewRegexpParser(re string) (*RegexpParser, error) {
    regex, err := regexp.Compile(re)
    if err != nil {
        return nil, err
    }
    if regex.NumSubexp() == 0 {
        return nil, errMissingCapture
    }
    nameIndex := map[int]string{}
    uniqueNames := map[string]struct{}{}
    for i, n := range regex.SubexpNames() {
        if n != "" {
            if !model.UTF8Validation.IsValidLabelName(n) {
                return nil, fmt.Errorf("invalid extracted label name '%s'", n)
            }
            if _, ok := uniqueNames[n]; ok {
                return nil, fmt.Errorf("duplicate extracted label name '%s'", n)
            }
            nameIndex[i] = n
            uniqueNames[n] = struct{}{}
        }
    }
    if len(nameIndex) == 0 {
        return nil, errMissingCapture
    }
    return &RegexpParser{
        regex:     regex,
        nameIndex: nameIndex,
        keys:      internedStringSet{},
    }, nil
}

func (r *RegexpParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
    parserHints := lbs.ParserLabelHints()
    for i, value := range r.regex.FindSubmatch(line) {
        if name, ok := r.nameIndex[i]; ok {
            // ... sanitization and label setting logic
            lbs.Set(ParsedLabel, key, string(value))
        }
    }
    return line, true
}
```

**Key Points:**
1. Uses `regexp.Compile()` for compilation
2. `regex.SubexpNames()` returns named capture group names
3. `regex.FindSubmatch()` extracts all matches
4. Named groups are identified by non-empty names in `SubexpNames()`
5. Validates that at least one named capture group exists
6. Validates that capture group names are valid label names
7. Validates no duplicate capture group names

---

## Implementation Steps

### Step 1: Add VariadicOp Type

**File:** [`pkg/engine/internal/types/operators.go`](pkg/engine/internal/types/operators.go)

Add new operator constant:

```go
const (
    // ... existing operators ...
    
    VariadicOpParseLogfmt // Parse logfmt line to set of columns operation (logfmt).
    VariadicOpParseJSON   // Parse JSON line to set of columns operation (json).
    VariadicOpParseRegexp // Parse line with regex capture groups operation (regexp).
)

func (t VariadicOp) String() string {
    switch t {
    // ... existing cases ...
    case VariadicOpParseRegexp:
        return "PARSE_REGEXP"
    // ...
    }
}
```

### Step 2: Update Proto Definitions

**File:** [`pkg/engine/internal/proto/expressionpb/expressionpb.proto`](pkg/engine/internal/proto/expressionpb/expressionpb.proto)

Add new enum value:

```protobuf
enum VariadicOp {
  VARIADIC_OP_INVALID = 0;
  VARIADIC_OP_PARSE_LOGFMT = 1;
  VARIADIC_OP_PARSE_JSON = 2;
  VARIADIC_OP_PARSE_REGEXP = 3;  // NEW
}
```

Then regenerate proto files.

**File:** [`pkg/engine/internal/proto/expressionpb/marshal_types.go`](pkg/engine/internal/proto/expressionpb/marshal_types.go)

```go
var variadicOpFromProto = map[VariadicOp]types.VariadicOp{
    // ... existing mappings ...
    VARIADIC_OP_PARSE_REGEXP: types.VariadicOpParseRegexp,
}
```

**File:** [`pkg/engine/internal/proto/expressionpb/unmarshal_types.go`](pkg/engine/internal/proto/expressionpb/unmarshal_types.go)

```go
var variadicOpToProto = map[types.VariadicOp]VariadicOp{
    // ... existing mappings ...
    types.VariadicOpParseRegexp: VARIADIC_OP_PARSE_REGEXP,
}
```

### Step 3: Update Logical Planner

**File:** [`pkg/engine/internal/planner/logical/planner.go`](pkg/engine/internal/planner/logical/planner.go)

Update the `buildPlanForLogQuery` function to handle regex parser:

```go
var (
    // ... existing vars ...
    hasLogfmtParser     bool
    logfmtStrict        bool
    logfmtKeepEmpty     bool
    hasJSONParser       bool
    hasRegexpParser     bool   // NEW
    regexpPattern       string // NEW - store the regex pattern
)

// In the Walk function:
case *syntax.LineParserExpr:
    switch e.Op {
    case syntax.OpParserTypeJSON:
        hasJSONParser = true
        return true
    case syntax.OpParserTypeRegexp:  // NEW CASE
        hasRegexpParser = true
        regexpPattern = e.Param  // The regex pattern is in e.Param
        return true
    case syntax.OpParserTypeUnpack, syntax.OpParserTypePattern:
        err = errUnimplemented
        return false
    default:
        err = errUnimplemented
        return false
    }

// After the walk, add to builder:
if hasLogfmtParser {
    builder = builder.Parse(types.VariadicOpParseLogfmt, logfmtStrict, logfmtKeepEmpty)
}
if hasJSONParser {
    builder = builder.Parse(types.VariadicOpParseJSON, false, false)
}
if hasRegexpParser {  // NEW
    builder = builder.ParseRegexp(regexpPattern)  // Need new builder method
}
```

### Step 4: Update Builder

**File:** [`pkg/engine/internal/planner/logical/builder.go`](pkg/engine/internal/planner/logical/builder.go)

Add new builder method for regex parsing:

```go
// ParseRegexp applies a regex [Parse] operation to the Builder.
// The pattern must contain at least one named capture group like (?P<name>...).
func (b *Builder) ParseRegexp(pattern string) *Builder {
    val := &FunctionOp{
        Op: types.VariadicOpParseRegexp,
        Values: []Value{
            // source column
            &ColumnRef{
                Ref: semconv.ColumnIdentMessage.ColumnRef(),
            },
            // regex pattern
            NewLiteral(pattern),
            // requested keys (to be filled in by projection pushdown optimizer)
            NewLiteral([]string{}),
        },
    }
    return b.ProjectExpand(val)
}
```

### Step 5: Implement Executor Function

**File:** [`pkg/engine/internal/executor/parse.go`](pkg/engine/internal/executor/parse.go)

Add the regex parsing implementation:

```go
import (
    "github.com/grafana/regexp"
)

func parseFn(op types.VariadicOp) VariadicFunction {
    return VariadicFunctionFunc(func(args ...arrow.Array) (arrow.Array, error) {
        var headers []string
        var parsedColumns []arrow.Array
        
        switch op {
        case types.VariadicOpParseLogfmt:
            sourceCol, requestedKeys, strict, keepEmpty, err := extractParseFnParameters(args)
            if err != nil {
                panic(err)
            }
            headers, parsedColumns = buildLogfmtColumns(sourceCol, requestedKeys, strict, keepEmpty)
        case types.VariadicOpParseJSON:
            sourceCol, requestedKeys, _, _, err := extractParseFnParameters(args)
            if err != nil {
                panic(err)
            }
            headers, parsedColumns = buildJSONColumns(sourceCol, requestedKeys)
        case types.VariadicOpParseRegexp:  // NEW
            sourceCol, pattern, requestedKeys, err := extractRegexpParseFnParameters(args)
            if err != nil {
                panic(err)
            }
            headers, parsedColumns = buildRegexpColumns(sourceCol, pattern, requestedKeys)
        default:
            return nil, fmt.Errorf("unsupported parser kind: %v", op)
        }
        
        // ... rest of function unchanged ...
    })
}

// NEW: Extract parameters specific to regexp parser
func extractRegexpParseFnParameters(args []arrow.Array) (*array.String, string, []string, error) {
    // Valid signature: parseRegexp(sourceColVec, pattern, requestedKeys)
    if len(args) != 3 {
        return nil, "", nil, fmt.Errorf("regexp parse function expected 3 arguments, got %d", len(args))
    }

    sourceColArr := args[0]
    patternArr := args[1]
    requestedKeysArr := args[2]

    if sourceColArr == nil {
        return nil, "", nil, fmt.Errorf("regexp parse function arguments did not include a source ColumnVector")
    }

    sourceCol, ok := sourceColArr.(*array.String)
    if !ok {
        return nil, "", nil, fmt.Errorf("regexp parse can only operate on string column types, got %T", sourceColArr)
    }

    // Extract pattern (scalar string)
    var pattern string
    if patternArr != nil && patternArr.Len() > 0 {
        strArr, ok := patternArr.(*array.String)
        if !ok {
            return nil, "", nil, fmt.Errorf("pattern must be a string, got %T", patternArr)
        }
        pattern = strArr.Value(0)
    }

    // Extract requested keys
    var requestedKeys []string
    if requestedKeysArr != nil && !requestedKeysArr.IsNull(0) {
        reqKeysList, ok := requestedKeysArr.(*array.List)
        if ok {
            firstRow, ok := util.ArrayListValue(reqKeysList, 0).([]string)
            if ok {
                requestedKeys = append(requestedKeys, firstRow...)
            }
        }
    }

    return sourceCol, pattern, requestedKeys, nil
}

// NEW: Build columns from regex pattern
func buildRegexpColumns(input *array.String, pattern string, requestedKeys []string) ([]string, []arrow.Array) {
    // Compile the regex
    regex, err := regexp.Compile(pattern)
    if err != nil {
        // Return error column
        return buildErrorColumns(input, "RegexpParseErr", err.Error())
    }

    // Build name index for capture groups
    nameIndex := make(map[int]string)
    for i, name := range regex.SubexpNames() {
        if name != "" {
            nameIndex[i] = name
        }
    }

    if len(nameIndex) == 0 {
        return buildErrorColumns(input, "RegexpParseErr", "at least one named capture must be supplied")
    }

    // Use the generic buildColumns function with a regexp-specific parser
    parseFunc := func(line string) (map[string]string, error) {
        result := make(map[string]string)
        matches := regex.FindStringSubmatch(line)
        
        if matches == nil {
            // No match - return empty result (not an error, line just doesn't match)
            return result, nil
        }
        
        for i, value := range matches {
            if name, ok := nameIndex[i]; ok {
                result[name] = value
            }
        }
        return result, nil
    }

    return buildColumns(input, requestedKeys, parseFunc, "RegexpParseErr")
}

// Helper for returning error columns
func buildErrorColumns(input *array.String, errType, errDetails string) ([]string, []arrow.Array) {
    errBuilder := array.NewStringBuilder(memory.DefaultAllocator)
    errDetailsBuilder := array.NewStringBuilder(memory.DefaultAllocator)
    
    for i := 0; i < input.Len(); i++ {
        errBuilder.Append(errType)
        errDetailsBuilder.Append(errDetails)
    }
    
    return []string{
            semconv.ColumnIdentError.ShortName(),
            semconv.ColumnIdentErrorDetails.ShortName(),
        }, []arrow.Array{
            errBuilder.NewArray(),
            errDetailsBuilder.NewArray(),
        }
}
```

### Step 6: Register the Function

**File:** [`pkg/engine/internal/executor/functions.go`](pkg/engine/internal/executor/functions.go)

Add registration:

```go
func init() {
    // ... existing registrations ...
    
    // Parse functions
    variadicFunctions.register(types.VariadicOpParseLogfmt, parseFn(types.VariadicOpParseLogfmt))
    variadicFunctions.register(types.VariadicOpParseJSON, parseFn(types.VariadicOpParseJSON))
    variadicFunctions.register(types.VariadicOpParseRegexp, parseFn(types.VariadicOpParseRegexp))  // NEW
}
```

### Step 7: Update Physical Planner (if needed)

**File:** [`pkg/engine/internal/planner/physical/planner.go`](pkg/engine/internal/planner/physical/planner.go)

Check if any special handling is needed for the new parser type (likely just add to existing checks):

```go
if funcExpr.Op == types.VariadicOpParseJSON || funcExpr.Op == types.VariadicOpParseLogfmt || funcExpr.Op == types.VariadicOpParseRegexp {
    needsCompat = true
}
```

### Step 8: Update Optimizer (if needed)

**File:** [`pkg/engine/internal/planner/physical/optimizer.go`](pkg/engine/internal/planner/physical/optimizer.go)

Add regex parser to the projection pushdown logic:

```go
case *VariadicExpr:
    if e.Op == types.VariadicOpParseJSON || e.Op == types.VariadicOpParseLogfmt || e.Op == types.VariadicOpParseRegexp {
        projectionNodeChanged, projsToPropagate := r.handleParse(e, projections)
        // ...
    }
```

---

## Test Cases

### Unit Tests

1. **Regex compilation error** - Invalid regex pattern should produce error column
2. **No named capture groups** - Should produce error
3. **Valid extraction** - Named groups should be extracted correctly
4. **No match** - When line doesn't match, columns should be null (not error)
5. **Partial match** - Only matching groups should have values
6. **Multiple lines** - Different lines with different matches

### Integration Tests

Add test in [`pkg/engine/lab_test/`](pkg/engine/lab_test/):

```go
func TestRegexpParser(t *testing.T) {
    query := `{app="test"} | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+)\\)"`
    
    // Test with sample data
    // Verify extracted fields
}
```

---

## Files to Modify Summary

| File | Change |
|------|--------|
| `pkg/engine/internal/types/operators.go` | Add `VariadicOpParseRegexp` constant |
| `pkg/engine/internal/proto/expressionpb/expressionpb.proto` | Add `VARIADIC_OP_PARSE_REGEXP` enum |
| `pkg/engine/internal/proto/expressionpb/marshal_types.go` | Add mapping |
| `pkg/engine/internal/proto/expressionpb/unmarshal_types.go` | Add mapping |
| `pkg/engine/internal/planner/logical/planner.go` | Handle `OpParserTypeRegexp` |
| `pkg/engine/internal/planner/logical/builder.go` | Add `ParseRegexp()` method |
| `pkg/engine/internal/executor/parse.go` | Implement `buildRegexpColumns()` |
| `pkg/engine/internal/executor/functions.go` | Register new function |
| `pkg/engine/internal/planner/physical/planner.go` | Add to parser checks |
| `pkg/engine/internal/planner/physical/optimizer.go` | Add to projection pushdown |

---

## Dependencies

- `github.com/grafana/regexp` - Already available, used by old engine
- No new dependencies required

---

## Open Questions

1. **Should we validate capture group names against Prometheus label naming rules?**
   - The old engine does: `model.UTF8Validation.IsValidLabelName(n)`
   
2. **Should we support case-insensitive regex flags?**
   - Go's regexp supports `(?i)` inline flag

3. **Performance considerations for compiled regex caching?**
   - The regex pattern is constant per query, so compilation happens once per query execution