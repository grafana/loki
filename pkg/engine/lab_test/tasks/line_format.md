# LogQL Line Format Stage - Implementation Plan

## Overview

The `line_format` stage in LogQL allows you to rewrite the log line content using a Go text/template expression. It can reference labels, extracted fields, and built-in values to construct a new log line.

## Feature Specification

### Syntax

```logql
| line_format "{{ template expression }}"
```

### Template Syntax

Line format uses Go's `text/template` syntax with access to:

1. **Labels**: `{{ .label_name }}` - Access stream labels
2. **Extracted Fields**: `{{ .parsed_field }}` - Access fields from json/logfmt/pattern/regexp
3. **Built-in Values**:
   - `{{ __line__ }}` - The original log line
   - `{{ __timestamp__ }}` - The log entry timestamp
4. **Template Functions**: Various helper functions for string manipulation

### Examples

#### Basic Label Substitution
```logql
{app="test"} | line_format "app={{ .app }} msg={{ __line__ }}"
```
**Input:** `error occurred`
**Output:** `app=test msg=error occurred`

#### With JSON Parsing
```logql
{app="test"} | json | line_format "level={{ .level }} message={{ .msg }}"
```
**Input:** `{"level":"error","msg":"connection failed","code":500}`
**Output:** `level=error message=connection failed`

#### Using Template Functions
```logql
{app="test"} | line_format "{{ .app | ToUpper }}: {{ __line__ }}"
```
**Input:** `something happened`
**Output:** `TEST: something happened`

#### Complex Formatting
```logql
{app="test"} 
| json 
| line_format `[{{ .level | ToUpper }}] {{ __timestamp__ | date "2006-01-02" }} - {{ .message }}`
```
**Input:** `{"level":"info","message":"user logged in"}`
**Output:** `[INFO] 2024-01-15 - user logged in`

### Available Template Functions

| Function | Description | Example |
|----------|-------------|---------|
| `ToLower` | Convert to lowercase | `{{ .level \| ToLower }}` |
| `ToUpper` | Convert to uppercase | `{{ .level \| ToUpper }}` |
| `Replace` | Replace substring | `{{ Replace .msg "old" "new" -1 }}` |
| `Trim` | Trim whitespace | `{{ Trim .msg }}` |
| `TrimLeft` | Trim left whitespace | `{{ TrimLeft .msg }}` |
| `TrimRight` | Trim right whitespace | `{{ TrimRight .msg }}` |
| `TrimPrefix` | Remove prefix | `{{ TrimPrefix .path "/" }}` |
| `TrimSuffix` | Remove suffix | `{{ TrimSuffix .file ".log" }}` |
| `regexReplaceAll` | Regex replace | `{{ regexReplaceAll "\\d+" .msg "X" }}` |
| `regexReplaceAllLiteral` | Literal regex replace | `{{ regexReplaceAllLiteral "\\d+" .msg "X" }}` |
| `date` | Format timestamp | `{{ __timestamp__ \| date "2006-01-02" }}` |
| `unixEpoch` | Unix timestamp | `{{ __timestamp__ \| unixEpoch }}` |
| `default` | Default value | `{{ .level \| default "unknown" }}` |
| `count` | Count occurrences | `{{ count "error" __line__ }}` |
| `urlencode` | URL encode | `{{ .path \| urlencode }}` |
| `urldecode` | URL decode | `{{ .encoded \| urldecode }}` |
| `b64enc` | Base64 encode | `{{ .data \| b64enc }}` |
| `b64dec` | Base64 decode | `{{ .encoded \| b64dec }}` |
| `substr` | Substring | `{{ substr 0 10 .msg }}` |
| `contains` | Check contains | `{{ if contains "error" .msg }}...{{ end }}` |
| `hasPrefix` | Check prefix | `{{ if hasPrefix "/" .path }}...{{ end }}` |
| `hasSuffix` | Check suffix | `{{ if hasSuffix ".log" .file }}...{{ end }}` |
| `indent` | Add indentation | `{{ .json \| indent 2 }}` |
| `nindent` | Newline + indent | `{{ .json \| nindent 2 }}` |
| `repeat` | Repeat string | `{{ repeat 3 "-" }}` |
| `printf` | Formatted print | `{{ printf "%s: %d" .name .count }}` |

## Implementation Plan for Engine V2

Based on the Engine V2 architecture, the implementation spans multiple stages:

### 1. Logical Planning Stage

**Location:** `pkg/engine/internal/planner/logical/`

**Changes Required:**

#### A. Add New SSA Instruction

```go
// LineFormatInstruction represents a line_format stage
type LineFormatInstruction struct {
    Input    Value    // Input table/stream
    Template string   // The template string (raw, unparsed)
}
```

#### B. SSA Representation Example

**LogQL Query:**
```logql
{app="test"} | json | line_format "level={{ .level }} msg={{ .msg }}"
```

**SSA Representation:**
```
%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = PROJECT %6 [mode=*E, expr=PARSE_JSON(builtin.message, [], false, false)]
%8 = LINE_FORMAT %7 [template="level={{ .level }} msg={{ .msg }}"]
%9 = TOPK %8 [sort_by=builtin.timestamp, k=1000, asc=false]
%10 = LOGQL_COMPAT %9
RETURN %10
```

**Key Points:**
- LINE_FORMAT comes after any parsing stages (json, logfmt, etc.)
- It modifies `builtin.message` column
- Template string is stored as-is for later compilation

#### C. Planner Integration

```go
// In planner.go, handle syntax.LineFmtExpr
func (b *builder) visitLineFmt(expr *syntax.LineFmtExpr) Value {
    input := b.visit(expr.Left)
    
    return b.addInstruction(&LineFormatInstruction{
        Input:    input,
        Template: expr.Value,
    })
}
```

### 2. Physical Planning Stage

**Location:** `pkg/engine/internal/planner/physical/`

**Changes Required:**

#### A. Add Physical LineFormat Node

```go
// LineFormatNode represents a line_format operation in the physical plan
type LineFormatNode struct {
    baseNode
    Input    Node
    Template string
}

func (n *LineFormatNode) Children() []Node {
    return []Node{n.Input}
}

func (n *LineFormatNode) String() string {
    return fmt.Sprintf("LineFormat[template=%q]", n.Template)
}
```

#### B. Logical to Physical Conversion

```go
// In planner.go, convert logical LINE_FORMAT to physical LineFormatNode
func (p *Planner) buildLineFormat(instr *logical.LineFormatInstruction) (Node, error) {
    input, err := p.build(instr.Input)
    if err != nil {
        return nil, err
    }
    
    return &LineFormatNode{
        Input:    input,
        Template: instr.Template,
    }, nil
}
```

#### C. Schema Propagation

The LineFormat node doesn't change the schema - it only modifies the content of `builtin.message`:

```go
func (n *LineFormatNode) Schema() *arrow.Schema {
    // Schema remains the same - we're just modifying message content
    return n.Input.Schema()
}
```

### 3. Execution Stage

**Location:** `pkg/engine/internal/executor/`

**Changes Required:**

#### A. Template Context Structure

```go
// templateContext provides data to the template during execution
type templateContext struct {
    // Labels from the stream
    labels map[string]string
    
    // Parsed fields (from json, logfmt, etc.)
    parsed map[string]interface{}
    
    // Built-in values
    line      string
    timestamp time.Time
}

// Get implements template data access
func (c *templateContext) Get(key string) interface{} {
    // Check for built-in values first
    switch key {
    case "__line__":
        return c.line
    case "__timestamp__":
        return c.timestamp
    }
    
    // Check labels
    if v, ok := c.labels[key]; ok {
        return v
    }
    
    // Check parsed fields
    if v, ok := c.parsed[key]; ok {
        return v
    }
    
    return ""
}
```

#### B. LineFormat Pipeline Implementation

```go
// lineFormatPipeline implements the line_format stage
type lineFormatPipeline struct {
    input    Pipeline
    template *template.Template
    pool     memory.Allocator
    
    // Column indices
    messageIdx   int
    timestampIdx int
    labelCols    map[string]int  // label name -> column index
    parsedCols   map[string]int  // parsed field name -> column index
}

func newLineFormatPipeline(input Pipeline, templateStr string, pool memory.Allocator) (*lineFormatPipeline, error) {
    // Parse and compile the template
    tmpl, err := template.New("line_format").
        Funcs(templateFuncs()).
        Parse(templateStr)
    if err != nil {
        return nil, fmt.Errorf("invalid line_format template: %w", err)
    }
    
    schema := input.Schema()
    
    // Find column indices
    messageIdx := -1
    timestampIdx := -1
    labelCols := make(map[string]int)
    parsedCols := make(map[string]int)
    
    for i, field := range schema.Fields() {
        switch {
        case field.Name == "builtin.message":
            messageIdx = i
        case field.Name == "builtin.timestamp":
            timestampIdx = i
        case strings.HasPrefix(field.Name, "label."):
            labelName := strings.TrimPrefix(field.Name, "label.")
            labelCols[labelName] = i
        case strings.HasPrefix(field.Name, "parsed."):
            parsedName := strings.TrimPrefix(field.Name, "parsed.")
            parsedCols[parsedName] = i
        case strings.HasPrefix(field.Name, "metadata."):
            metaName := strings.TrimPrefix(field.Name, "metadata.")
            parsedCols[metaName] = i  // Treat metadata like parsed fields
        }
    }
    
    if messageIdx == -1 {
        return nil, fmt.Errorf("builtin.message column not found")
    }
    
    return &lineFormatPipeline{
        input:        input,
        template:     tmpl,
        pool:         pool,
        messageIdx:   messageIdx,
        timestampIdx: timestampIdx,
        labelCols:    labelCols,
        parsedCols:   parsedCols,
    }, nil
}

func (p *lineFormatPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
    batch, err := p.input.Read(ctx)
    if err != nil {
        return nil, err
    }
    if batch == nil {
        return nil, nil
    }
    defer batch.Release()
    
    numRows := int(batch.NumRows())
    
    // Build new message column
    messageBuilder := array.NewStringBuilder(p.pool)
    defer messageBuilder.Release()
    
    var buf bytes.Buffer
    
    for i := 0; i < numRows; i++ {
        // Build template context for this row
        ctx := p.buildContext(batch, i)
        
        // Execute template
        buf.Reset()
        if err := p.template.Execute(&buf, ctx); err != nil {
            // On template error, keep original line
            if msgCol, ok := batch.Column(p.messageIdx).(*array.String); ok {
                messageBuilder.Append(msgCol.Value(i))
            } else {
                messageBuilder.AppendNull()
            }
            continue
        }
        
        messageBuilder.Append(buf.String())
    }
    
    // Create new batch with modified message column
    columns := make([]arrow.Array, batch.NumCols())
    for i := 0; i < batch.NumCols(); i++ {
        if i == p.messageIdx {
            columns[i] = messageBuilder.NewArray()
        } else {
            columns[i] = batch.Column(i)
            columns[i].Retain()
        }
    }
    
    result := array.NewRecord(batch.Schema(), columns, int64(numRows))
    
    // Release column references
    for _, col := range columns {
        col.Release()
    }
    
    return result, nil
}

func (p *lineFormatPipeline) buildContext(batch arrow.RecordBatch, row int) map[string]interface{} {
    ctx := make(map[string]interface{})
    
    // Add original line
    if msgCol, ok := batch.Column(p.messageIdx).(*array.String); ok && !msgCol.IsNull(row) {
        ctx["__line__"] = msgCol.Value(row)
    } else {
        ctx["__line__"] = ""
    }
    
    // Add timestamp
    if p.timestampIdx >= 0 {
        if tsCol, ok := batch.Column(p.timestampIdx).(*array.Timestamp); ok && !tsCol.IsNull(row) {
            ctx["__timestamp__"] = tsCol.Value(row).ToTime(arrow.Nanosecond)
        }
    }
    
    // Add labels
    for name, idx := range p.labelCols {
        ctx[name] = p.getStringValue(batch.Column(idx), row)
    }
    
    // Add parsed fields
    for name, idx := range p.parsedCols {
        ctx[name] = p.getValue(batch.Column(idx), row)
    }
    
    return ctx
}

func (p *lineFormatPipeline) getStringValue(col arrow.Array, row int) string {
    if col.IsNull(row) {
        return ""
    }
    
    switch arr := col.(type) {
    case *array.String:
        return arr.Value(row)
    case *array.Dictionary:
        dict := arr.Dictionary().(*array.String)
        return dict.Value(arr.GetValueIndex(row))
    default:
        return fmt.Sprintf("%v", arr)
    }
}

func (p *lineFormatPipeline) getValue(col arrow.Array, row int) interface{} {
    if col.IsNull(row) {
        return nil
    }
    
    switch arr := col.(type) {
    case *array.String:
        return arr.Value(row)
    case *array.Int64:
        return arr.Value(row)
    case *array.Float64:
        return arr.Value(row)
    case *array.Boolean:
        return arr.Value(row)
    case *array.Dictionary:
        dict := arr.Dictionary()
        idx := arr.GetValueIndex(row)
        if strDict, ok := dict.(*array.String); ok {
            return strDict.Value(idx)
        }
        return nil
    default:
        return nil
    }
}

func (p *lineFormatPipeline) Close() {
    p.input.Close()
}
```

#### C. Template Functions

```go
// templateFuncs returns the function map for line_format templates
func templateFuncs() template.FuncMap {
    return template.FuncMap{
        // String manipulation
        "ToLower":    strings.ToLower,
        "ToUpper":    strings.ToUpper,
        "Replace":    strings.Replace,
        "Trim":       strings.TrimSpace,
        "TrimLeft":   strings.TrimLeft,
        "TrimRight":  strings.TrimRight,
        "TrimPrefix": strings.TrimPrefix,
        "TrimSuffix": strings.TrimSuffix,
        "contains":   strings.Contains,
        "hasPrefix":  strings.HasPrefix,
        "hasSuffix":  strings.HasSuffix,
        "repeat":     strings.Repeat,
        "substr":     substr,
        "indent":     indent,
        "nindent":    nindent,
        "printf":     fmt.Sprintf,
        
        // Regex
        "regexReplaceAll":        regexReplaceAll,
        "regexReplaceAllLiteral": regexReplaceAllLiteral,
        
        // Encoding
        "urlencode": url.QueryEscape,
        "urldecode": url.QueryUnescape,
        "b64enc":    base64Encode,
        "b64dec":    base64Decode,
        
        // Date/Time
        "date":      formatDate,
        "unixEpoch": unixEpoch,
        
        // Utility
        "default": defaultValue,
        "count":   countOccurrences,
    }
}

func substr(start, end int, s string) string {
    if start < 0 {
        start = 0
    }
    if end > len(s) {
        end = len(s)
    }
    if start > end {
        return ""
    }
    return s[start:end]
}

func indent(spaces int, s string) string {
    pad := strings.Repeat(" ", spaces)
    return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

func nindent(spaces int, s string) string {
    return "\n" + indent(spaces, s)
}

func regexReplaceAll(pattern, s, repl string) string {
    re, err := regexp.Compile(pattern)
    if err != nil {
        return s
    }
    return re.ReplaceAllString(s, repl)
}

func regexReplaceAllLiteral(pattern, s, repl string) string {
    re, err := regexp.Compile(pattern)
    if err != nil {
        return s
    }
    return re.ReplaceAllLiteralString(s, repl)
}

func base64Encode(s string) string {
    return base64.StdEncoding.EncodeToString([]byte(s))
}

func base64Decode(s string) string {
    data, err := base64.StdEncoding.DecodeString(s)
    if err != nil {
        return s
    }
    return string(data)
}

func formatDate(layout string, t time.Time) string {
    return t.Format(layout)
}

func unixEpoch(t time.Time) int64 {
    return t.Unix()
}

func defaultValue(def, val interface{}) interface{} {
    if val == nil || val == "" {
        return def
    }
    return val
}

func countOccurrences(substr, s string) int {
    return strings.Count(s, substr)
}
```

#### D. Executor Integration

```go
// In executor.go, add case for LineFormatNode
func (e *Executor) buildPipeline(node physical.Node) (Pipeline, error) {
    switch n := node.(type) {
    case *physical.LineFormatNode:
        input, err := e.buildPipeline(n.Input)
        if err != nil {
            return nil, err
        }
        return newLineFormatPipeline(input, n.Template, e.pool)
    
    // ... other node types
    }
}
```

### 4. Testing Strategy

**Location:** `pkg/engine/lab_test/`

#### A. Unit Tests

```go
// Test file: line_format_test.go

func TestLineFormat_BasicLabelSubstitution(t *testing.T) {
    // Test: | line_format "app={{ .app }}"
    // Verify label values are substituted
}

func TestLineFormat_OriginalLine(t *testing.T) {
    // Test: | line_format "prefix: {{ __line__ }}"
    // Verify __line__ contains original message
}

func TestLineFormat_Timestamp(t *testing.T) {
    // Test: | line_format "{{ __timestamp__ | date \"2006-01-02\" }}"
    // Verify timestamp formatting
}

func TestLineFormat_ParsedFields(t *testing.T) {
    // Test: | json | line_format "{{ .level }}: {{ .msg }}"
    // Verify parsed JSON fields are accessible
}

func TestLineFormat_TemplateFunctions(t *testing.T) {
    // Test various template functions
    tests := []struct {
        template string
        input    string
        expected string
    }{
        {`{{ .msg | ToUpper }}`, "hello", "HELLO"},
        {`{{ .msg | ToLower }}`, "HELLO", "hello"},
        {`{{ substr 0 5 .msg }}`, "hello world", "hello"},
        {`{{ .val | default "N/A" }}`, "", "N/A"},
    }
    // ...
}

func TestLineFormat_ComplexTemplate(t *testing.T) {
    // Test: | json | line_format `[{{ .level | ToUpper }}] {{ .msg }}`
    // Verify complex template with multiple operations
}

func TestLineFormat_MissingField(t *testing.T) {
    // Test behavior when referenced field doesn't exist
    // Should return empty string, not error
}

func TestLineFormat_InvalidTemplate(t *testing.T) {
    // Test: | line_format "{{ invalid syntax"
    // Should return error during pipeline creation
}
```

#### B. Integration Tests

```go
func TestLineFormat_EndToEnd(t *testing.T) {
    // Full query execution test
    query := `{app="test"} | json | line_format "{{ .level }}: {{ .message }}"`
    
    // Create test data
    // Execute query
    // Verify results
}

func TestLineFormat_WithOtherStages(t *testing.T) {
    // Test line_format combined with other stages
    query := `{app="test"} | json | line_format "{{ .level }}" | level="error"`
    // Verify pipeline works correctly
}
```

#### C. Performance Tests

```go
func BenchmarkLineFormat_Simple(b *testing.B) {
    // Benchmark simple template
}

func BenchmarkLineFormat_Complex(b *testing.B) {
    // Benchmark complex template with multiple functions
}

func BenchmarkLineFormat_ManyFields(b *testing.B) {
    // Benchmark template accessing many fields
}
```

### 5. Implementation Phases

#### Phase 1: Core Infrastructure (Week 1)
- [ ] Add SSA instruction for LINE_FORMAT
- [ ] Implement logical planner changes
- [ ] Add physical plan node
- [ ] Write unit tests for planning stages

#### Phase 2: Template Engine (Week 2)
- [ ] Implement template context structure
- [ ] Add all template functions
- [ ] Handle different Arrow column types
- [ ] Unit tests for template functions

#### Phase 3: Execution Pipeline (Week 3)
- [ ] Implement lineFormatPipeline
- [ ] Integrate with executor
- [ ] End-to-end tests
- [ ] Error handling

#### Phase 4: Optimization & Polish (Week 4)
- [ ] Template caching/precompilation
- [ ] Performance benchmarks
- [ ] Memory optimization
- [ ] Documentation

## Technical Considerations

### 1. Template Compilation
- Templates should be compiled once during pipeline creation
- Cache compiled templates for reuse
- Validate template syntax early to fail fast

### 2. Column Type Handling
- String columns: Direct access
- Dictionary-encoded: Decode from dictionary
- Numeric columns: Convert to string for template
- Null handling: Return empty string or nil

### 3. Memory Management
- Use Arrow memory pools for string building
- Reuse buffers across rows
- Release intermediate arrays promptly

### 4. Performance Optimizations
- Pre-compile templates at pipeline creation
- Cache column index lookups
- Consider template simplification for common patterns
- Batch string building operations

### 5. Error Handling
- Invalid template syntax: Error at pipeline creation
- Template execution errors: Keep original line, log warning
- Missing fields: Return empty string (not error)
- Type mismatches: Best-effort conversion

### 6. Edge Cases
- Empty template: Return empty string
- Template with only static text: No field access needed
- Recursive templates: Prevent infinite loops
- Very long output: Consider truncation limits

## Comparison with V1 Implementation

The V1 engine uses `log/line_format.go` which processes logs one at a time. Key differences in V2:

| Aspect | V1 | V2 |
|--------|----|----|
| Processing | Row-by-row | Batch (RecordBatch) |
| Template | Compiled per query | Compiled once, reused |
| Data Access | Direct struct fields | Arrow column access |
| Memory | Per-row allocation | Pooled allocation |
| Parallelism | Limited | Vectorized potential |

## References

- Engine V2 Architecture: `pkg/engine/lab_test/ENGINE_V2_ARCHITECTURE.md`
- V1 Line Format: `pkg/logql/log/line_format.go`
- Go text/template: https://pkg.go.dev/text/template
- Arrow Documentation: https://arrow.apache.org/docs/