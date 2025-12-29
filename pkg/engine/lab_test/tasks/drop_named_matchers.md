# LogQL Drop Stage with Named Matchers - Implementation Plan

## Overview

The `drop` stage with named matchers allows conditional removal of labels based on their values, providing fine-grained control over label management in LogQL queries.

## Feature Specification

### Syntax

```logql
| drop label1="value1", label2=~"regex", label3!="value3"
```

### Supported Matchers

1. **`=`** (equality): Drop label if value exactly matches
2. **`!=`** (inequality): Drop label if value doesn't match
3. **`=~`** (regex): Drop label if value matches regex pattern
4. **`!~`** (negative regex): Drop label if value doesn't match regex pattern

### Expected Behavior

**Drop specific values only:**
```logql
{job="app"} | drop status="200"
```
Result: Removes `status` label only where it equals "200". Labels like `status="404"` remain.

**Drop using regex:**
```logql
{job="app"} | drop pod=~"test-.*"
```
Result: Removes `pod` label only from entries where pod name starts with "test-".

**Drop everything except specific values:**
```logql
{job="app"} | drop level!="error"
```
Result: Removes `level` label from all entries EXCEPT where `level="error"`.

### Key Differences from Plain Drop

- **Without matchers:** `| drop status` removes ALL status labels unconditionally
- **With matchers:** `| drop status="200"` removes ONLY status labels with value "200"

## Implementation Plan for Engine V2

Based on the Engine V2 architecture, the implementation spans multiple stages:

### 1. Logical Planning Stage

**Location:** `pkg/engine/internal/planner/logical/`

**Changes Required:**

#### A. Add New SSA Instructions

In the logical plan, we need to represent conditional drop operations:

```go
// New instruction type for conditional drop
type DropInstruction struct {
    Input      Value              // Input table/stream
    Conditions []DropCondition    // List of drop conditions
}

type DropCondition struct {
    Column   ColumnRef          // Column to potentially drop (e.g., "label.status")
    Matcher  MatcherType        // =, !=, =~, !~
    Value    string             // Value or regex pattern to match
}

type MatcherType int

const (
    MatcherEqual MatcherType = iota
    MatcherNotEqual
    MatcherRegex
    MatcherNotRegex
)
```

#### B. SSA Representation Example

**LogQL Query:**
```logql
{app="test"} | drop status="200", pod=~"test-.*"
```

**SSA Representation:**
```
%1 = EQ label.app "test"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = GTE builtin.timestamp 1970-01-01T01:00:00Z
%4 = SELECT %2 [predicate=%3]
%5 = LT builtin.timestamp 1970-01-01T02:00:00Z
%6 = SELECT %4 [predicate=%5]
%7 = EQ label.status "200"                              # Drop condition 1
%8 = MATCH_RE label.pod "test-.*"                       # Drop condition 2
%9 = DROP %6 [conditions=[{col=label.status, matcher=%7}, {col=label.pod, matcher=%8}]]
%10 = TOPK %9 [sort_by=builtin.timestamp, k=1000, asc=false]
%11 = LOGQL_COMPAT %10
RETURN %11
```

**Key Points:**
- Drop conditions are predicates created before the DROP instruction
- DROP instruction references both the input and the conditions
- Each condition specifies which column to drop and under what predicate

#### C. Parser Integration

Update the logical planner to recognize drop stages with matchers:

```go
// In planner.go, handle syntax.LabelFilterExpr with Drop operation
func (b *builder) visitLabelFilter(expr *syntax.LabelFilterExpr) Value {
    if expr.Type == syntax.LabelFilterDrop {
        // Build drop conditions from matchers
        var conditions []DropCondition
        for _, matcher := range expr.Matchers {
            condition := DropCondition{
                Column:  b.resolveColumn(matcher.Name),
                Matcher: convertMatcherType(matcher.Type),
                Value:   matcher.Value,
            }
            conditions = append(conditions, condition)
        }
        
        input := b.visit(expr.Left)
        return b.addInstruction(&DropInstruction{
            Input:      input,
            Conditions: conditions,
        })
    }
    // ... handle other label filter types
}
```

### 2. Physical Planning Stage

**Location:** `pkg/engine/internal/planner/physical/`

**Changes Required:**

#### A. Add Physical Drop Node

```go
// Physical plan node for conditional drop
type DropNode struct {
    baseNode
    Input      Node
    Conditions []DropCondition
}

func (n *DropNode) Children() []Node {
    return []Node{n.Input}
}

func (n *DropNode) String() string {
    return fmt.Sprintf("Drop[conditions=%v]", n.Conditions)
}
```

#### B. Logical to Physical Conversion

```go
// In planner.go, convert logical DROP to physical DropNode
func (p *Planner) buildDrop(instr *logical.DropInstruction) (Node, error) {
    input, err := p.build(instr.Input)
    if err != nil {
        return nil, err
    }
    
    return &DropNode{
        Input:      input,
        Conditions: instr.Conditions,
    }, nil
}
```

#### C. Schema Propagation

The Drop node needs to update the schema to reflect potentially removed columns:

```go
func (n *DropNode) Schema() *arrow.Schema {
    inputSchema := n.Input.Schema()
    
    // For conditional drops, we can't know at planning time which columns
    // will actually be dropped (depends on runtime values), so we keep
    // all columns in the schema but mark them as nullable
    
    fields := make([]arrow.Field, 0, len(inputSchema.Fields()))
    for _, field := range inputSchema.Fields() {
        // Check if this field might be dropped
        mightBeDropped := false
        for _, cond := range n.Conditions {
            if cond.Column.Name == field.Name {
                mightBeDropped = true
                break
            }
        }
        
        if mightBeDropped {
            // Make field nullable since it might be dropped
            field.Nullable = true
        }
        fields = append(fields, field)
    }
    
    return arrow.NewSchema(fields, nil)
}
```

### 3. Execution Stage

**Location:** `pkg/engine/internal/executor/`

**Changes Required:**

#### A. Drop Pipeline Implementation

```go
// dropPipeline implements conditional label dropping
type dropPipeline struct {
    input      Pipeline
    conditions []dropCondition
    pool       memory.Allocator
}

type dropCondition struct {
    columnIdx int           // Index of column in schema
    matcher   MatcherType
    value     string
    regex     *regexp.Regexp // Compiled regex for =~ and !~
}

func newDropPipeline(input Pipeline, conditions []physical.DropCondition, pool memory.Allocator) (*dropPipeline, error) {
    schema := input.Schema()
    
    // Resolve column indices and compile regexes
    execConditions := make([]dropCondition, len(conditions))
    for i, cond := range conditions {
        // Find column index in schema
        idx := -1
        for j, field := range schema.Fields() {
            if field.Name == cond.Column.Name {
                idx = j
                break
            }
        }
        if idx == -1 {
            return nil, fmt.Errorf("column %s not found in schema", cond.Column.Name)
        }
        
        execConditions[i] = dropCondition{
            columnIdx: idx,
            matcher:   cond.Matcher,
            value:     cond.Value,
        }
        
        // Compile regex if needed
        if cond.Matcher == MatcherRegex || cond.Matcher == MatcherNotRegex {
            regex, err := regexp.Compile(cond.Value)
            if err != nil {
                return nil, fmt.Errorf("invalid regex %q: %w", cond.Value, err)
            }
            execConditions[i].regex = regex
        }
    }
    
    return &dropPipeline{
        input:      input,
        conditions: execConditions,
        pool:       pool,
    }, nil
}

func (p *dropPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
    batch, err := p.input.Read(ctx)
    if err != nil {
        return nil, err
    }
    if batch == nil {
        return nil, nil
    }
    defer batch.Release()
    
    // Process each condition
    columns := make([]arrow.Array, batch.NumCols())
    for i := 0; i < batch.NumCols(); i++ {
        columns[i] = batch.Column(i)
        columns[i].Retain() // Keep reference
    }
    
    // Apply drop conditions
    for _, cond := range p.conditions {
        col := batch.Column(cond.columnIdx)
        
        // Create a null mask based on the condition
        nullMask := p.evaluateCondition(col, cond)
        
        // Apply null mask to column
        if nullMask != nil {
            columns[cond.columnIdx].Release()
            columns[cond.columnIdx] = p.applyNullMask(col, nullMask)
        }
    }
    
    // Create new batch with modified columns
    result := array.NewRecord(batch.Schema(), columns, batch.NumRows())
    
    // Release column references
    for _, col := range columns {
        col.Release()
    }
    
    return result, nil
}

func (p *dropPipeline) evaluateCondition(col arrow.Array, cond dropCondition) []bool {
    // Returns a boolean mask: true = drop this value (set to null)
    
    numRows := col.Len()
    mask := make([]bool, numRows)
    
    // Handle different column types
    switch arr := col.(type) {
    case *array.String:
        for i := 0; i < numRows; i++ {
            if arr.IsNull(i) {
                continue // Already null, skip
            }
            
            value := arr.Value(i)
            shouldDrop := false
            
            switch cond.matcher {
            case MatcherEqual:
                shouldDrop = value == cond.value
            case MatcherNotEqual:
                shouldDrop = value != cond.value
            case MatcherRegex:
                shouldDrop = cond.regex.MatchString(value)
            case MatcherNotRegex:
                shouldDrop = !cond.regex.MatchString(value)
            }
            
            mask[i] = shouldDrop
        }
    
    case *array.Dictionary:
        // Handle dictionary-encoded strings (common for labels)
        dict := arr.Dictionary().(*array.String)
        for i := 0; i < numRows; i++ {
            if arr.IsNull(i) {
                continue
            }
            
            dictIdx := arr.GetValueIndex(i)
            value := dict.Value(dictIdx)
            shouldDrop := false
            
            switch cond.matcher {
            case MatcherEqual:
                shouldDrop = value == cond.value
            case MatcherNotEqual:
                shouldDrop = value != cond.value
            case MatcherRegex:
                shouldDrop = cond.regex.MatchString(value)
            case MatcherNotRegex:
                shouldDrop = !cond.regex.MatchString(value)
            }
            
            mask[i] = shouldDrop
        }
    
    default:
        // Unsupported type, don't drop
        return nil
    }
    
    return mask
}

func (p *dropPipeline) applyNullMask(col arrow.Array, mask []bool) arrow.Array {
    // Create a new array with nulls where mask is true
    
    switch arr := col.(type) {
    case *array.String:
        builder := array.NewStringBuilder(p.pool)
        defer builder.Release()
        
        for i := 0; i < arr.Len(); i++ {
            if mask[i] || arr.IsNull(i) {
                builder.AppendNull()
            } else {
                builder.Append(arr.Value(i))
            }
        }
        
        return builder.NewArray()
    
    case *array.Dictionary:
        // For dictionary arrays, we need to preserve the dictionary
        // but mark values as null
        builder := array.NewDictionaryBuilder(p.pool, arr.DataType())
        defer builder.Release()
        
        dict := arr.Dictionary()
        if err := builder.InsertDictValues(dict); err != nil {
            // Fallback: return original
            arr.Retain()
            return arr
        }
        
        for i := 0; i < arr.Len(); i++ {
            if mask[i] || arr.IsNull(i) {
                builder.AppendNull()
            } else {
                builder.Append(arr.GetValueIndex(i))
            }
        }
        
        return builder.NewArray()
    
    default:
        // Unsupported type, return original
        arr.Retain()
        return arr
    }
}

func (p *dropPipeline) Close() {
    p.input.Close()
}
```

#### B. Executor Integration

```go
// In executor.go, add case for DropNode
func (e *Executor) buildPipeline(node physical.Node) (Pipeline, error) {
    switch n := node.(type) {
    case *physical.DropNode:
        input, err := e.buildPipeline(n.Input)
        if err != nil {
            return nil, err
        }
        return newDropPipeline(input, n.Conditions, e.pool)
    
    // ... other node types
    }
}
```

### 4. Testing Strategy

**Location:** `pkg/engine/lab_test/`

Create comprehensive tests covering:

#### A. Unit Tests

```go
// Test file: drop_named_matchers_test.go

func TestDropWithNamedMatchers_Equality(t *testing.T) {
    // Test: | drop status="200"
    // Verify only status="200" is dropped, others remain
}

func TestDropWithNamedMatchers_Inequality(t *testing.T) {
    // Test: | drop level!="error"
    // Verify all levels except "error" are dropped
}

func TestDropWithNamedMatchers_Regex(t *testing.T) {
    // Test: | drop pod=~"test-.*"
    // Verify pods matching regex are dropped
}

func TestDropWithNamedMatchers_NegativeRegex(t *testing.T) {
    // Test: | drop namespace!~"kube-.*"
    // Verify namespaces not matching regex are dropped
}

func TestDropWithNamedMatchers_Multiple(t *testing.T) {
    // Test: | drop status="200", level="debug"
    // Verify multiple conditions work together
}

func TestDropWithNamedMatchers_DictionaryEncoded(t *testing.T) {
    // Test drop on dictionary-encoded columns (common for labels)
}
```

#### B. Integration Tests

```go
func TestDropWithNamedMatchers_EndToEnd(t *testing.T) {
    // Full query execution test
    query := `{app="test"} | drop status="200"`
    
    // Create test data with various status values
    // Execute query
    // Verify results
}
```

#### C. Performance Tests

```go
func BenchmarkDropWithNamedMatchers(b *testing.B) {
    // Benchmark drop performance with:
    // - Different matcher types
    // - Different data sizes
    // - Dictionary vs non-dictionary encoding
}
```

### 5. Implementation Phases

#### Phase 1: Core Infrastructure (Week 1)
- [ ] Add SSA instructions for conditional drop
- [ ] Implement logical planner changes
- [ ] Add physical plan node
- [ ] Write unit tests for planning stages

#### Phase 2: Execution Engine (Week 2)
- [ ] Implement drop pipeline
- [ ] Add matcher evaluation logic
- [ ] Handle different Arrow array types
- [ ] Optimize for dictionary-encoded columns

#### Phase 3: Integration & Testing (Week 3)
- [ ] Parser integration for syntax support
- [ ] End-to-end tests
- [ ] Performance benchmarks
- [ ] Documentation

#### Phase 4: Optimization (Week 4)
- [ ] Vectorized evaluation for better performance
- [ ] Memory pool optimization
- [ ] Pushdown opportunities (if applicable)
- [ ] Edge case handling

## Use Cases

### 1. Reduce Cardinality
```logql
{app="frontend"} | drop request_id=~".*"
```
Remove high-cardinality request IDs before aggregation.

### 2. Privacy/Security
```logql
{app="api"} | drop user_email=~".*", ip_address=~".*"
```
Remove sensitive labels before storing or displaying.

### 3. Conditional Cleanup
```logql
{app="backend"} | drop trace_id="", span_id=""
```
Remove empty trace/span IDs.

### 4. Performance Optimization
```logql
{job="logs"} | drop pod=~".*" | count_over_time([5m])
```
Drop unnecessary labels before expensive operations.

## Technical Considerations

### 1. Column Type Handling
- String columns: Direct comparison
- Dictionary-encoded: Efficient lookup in dictionary
- Nullable columns: Preserve null semantics

### 2. Memory Management
- Use Arrow memory pools for allocations
- Release intermediate arrays promptly
- Consider copy-on-write for unchanged columns

### 3. Performance Optimizations
- Compile regexes once at pipeline creation
- Use vectorized operations where possible
- Consider SIMD for equality checks
- Cache dictionary lookups

### 4. Edge Cases
- Empty matchers list (no-op)
- Non-existent columns (error or ignore?)
- Already-null values (preserve)
- Type mismatches (error handling)

## References

- Engine V2 Architecture: `pkg/engine/lab_test/ENGINE_V2_ARCHITECTURE.md`
- Logical Planning: `pkg/engine/internal/planner/logical/`
- Physical Planning: `pkg/engine/internal/planner/physical/`
- Execution: `pkg/engine/internal/executor/`
- Arrow Documentation: https://arrow.apache.org/docs/