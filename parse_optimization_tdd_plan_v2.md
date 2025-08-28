# TDD Execution Plan v2: Move Parse Key Collection to Optimization Phase

## Overview
This plan refactors the logfmt parse key collection from the logical planner to the physical planner's optimization phase, while also simplifying column type resolution by eliminating the registry/resolver system.

## Key Insights
1. **Stream selector labels can be identified** at logical planning time
2. **Metadata vs parsed fields cannot be disambiguated** until runtime (we don't know what metadata exists)
3. **The registry/resolver system is unnecessary complexity** - we can handle ambiguity more simply
4. **Single optimization rule** works for all query types (no log/metric distinction needed)

## Architectural Changes

### Current State (Complex)
- Logical planner creates all non-builtin columns as `ColumnTypeAmbiguous`
- Physical planner has ColumnRegistry to track columns
- Physical planner has ExpressionResolver to disambiguate using registry
- Key collection happens in logical planner

### Target State (Simplified)
- Logical planner marks stream labels as `ColumnTypeLabel`
- Logical planner marks other columns as `ColumnTypeAmbiguous` (metadata or parsed)
- Physical planner has NO registry or resolver
- Parse optimization rule collects ambiguous columns that need parsing
- Executor handles metadata vs parsed at runtime (unchanged)

## TDD Execution Strategy

### Important TDD Reminders for Execution
- **Every test must focus on observable behavior, not structure or types**
- **Run each test BEFORE implementation - it must fail for the right reason**
- **A good failure = behavior is missing, NOT syntax/import/structural errors**
- **After implementation, re-run the test - it must pass**
- **Pause after each test fails correctly for review**
- **Implementation stage must NOT modify the test**
- **If test modification is needed during implementation, pause and explain why**
- **Pause after implementation is complete for review before next cycle**

---

## Part 1: Improve Logical Planner Column Type Resolution

### Step 1: Identify Stream Labels in Logical Planner

**Behavior Being Added**: The logical planner correctly identifies stream selector labels and marks them as `ColumnTypeLabel` instead of `ColumnTypeAmbiguous`.

**Test to Write**: `TestLogicalPlannerIdentifiesStreamLabels` (NEW)
- Location: `pkg/engine/planner/logical/planner_test.go`
- Create a query with stream selector: `{app="test", env="prod"} | level="error"`
- Assert that:
  - Stream selector columns (`app`, `env`) have type `ColumnTypeLabel`
  - Filter column (`level`) has type `ColumnTypeAmbiguous` (could be metadata or parsed)

**Why Meaningful**: Fails because the logical planner currently marks all these as `ColumnTypeAmbiguous`. Tests that we can distinguish what we know (labels) from what we don't (metadata/parsed).

**Minimal Implementation**:
```go
// In buildPlanForLogQuery, track stream labels
streamLabels := extractStreamLabels(selector)

// In convertLabelFilter
if contains(streamLabels, m.Name) {
    return &BinOp{
        Left:  NewColumnRef(m.Name, types.ColumnTypeLabel),
        ...
    }
} else {
    return &BinOp{
        Left:  NewColumnRef(m.Name, types.ColumnTypeAmbiguous),
        ...
    }
}
```

---

### Step 2: Track Stream Labels Through Query Building

**Behavior Being Added**: Stream labels are correctly identified even in complex queries with multiple operations.

**Test to Write**: `TestStreamLabelsInComplexQuery` (NEW)
- Location: `pkg/engine/planner/logical/planner_test.go`
- Create a metric query: `sum by(app,status) (count_over_time({app="test"}[5m] | logfmt))`
- Assert that:
  - `app` in groupBy is `ColumnTypeLabel`
  - `status` in groupBy is `ColumnTypeAmbiguous`

**Why Meaningful**: Fails because we need to propagate label information through the query building process. Tests that label tracking works across operations.

**Minimal Implementation**:
```go
// Pass streamLabels through buildPlanForSampleQuery
// Use it when creating groupBy column refs
```

---

## Part 2: Remove Registry/Resolver System

### Step 3: Remove Registry Usage from Physical Planner

**Behavior Being Changed**: The physical planner no longer uses a ColumnRegistry to resolve column types.

**Test to Update**: `TestPhysicalPlannerWithoutRegistry` (UPDATE EXISTING or NEW)
- Location: `pkg/engine/planner/physical/planner_test.go`
- Create a plan with ambiguous columns
- Assert that the physical plan preserves the column types from logical plan (no resolution happens)

**Why Meaningful**: Fails if the planner still tries to use the registry. Tests that we're truly removing this complexity.

**Minimal Implementation**:
1. Remove `registry` field from `Planner` struct
2. Remove `NewPlannerWithRegistry` function
3. Remove registry usage from `convertPredicate` method
4. Remove `registerColumns` method and its call in `Build`

---

### Step 4: Remove Expression Resolver

**Behavior Being Changed**: The physical planner directly converts logical expressions without trying to resolve ambiguous columns.

**Test to Update**: Remove resolver tests
- Location: `pkg/engine/planner/physical/expression_resolver_test.go`
- Delete this file entirely as the resolver no longer exists

**Why Meaningful**: These tests are for code that no longer exists. Cleanup ensures no dead code.

**Minimal Implementation**:
1. Delete `expression_resolver.go`
2. Delete `registry.go`
3. Simplify `convertPredicate` to not use resolver

---

## Part 3: Implement Parse Keys Optimization Rule

### Step 5: Create ParseKeysPushdown Rule Structure

**Behavior Being Added**: A new optimization rule that can be registered with the optimizer and will be called for ParseNode instances.

**Test to Write**: `TestParseKeysPushdownRuleApplies` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Assert that:
  - The rule's `apply` method returns false when given a non-ParseNode
  - The rule's `apply` method is called when given a ParseNode
  - The rule can be added to the optimization pipeline

**Why Meaningful**: This test will fail because the complete `parseKeysPushdown` implementation doesn't exist yet. It verifies the rule integrates with the existing optimization framework.

**Minimal Implementation**:
```go
type parseKeysPushdown struct {
    plan *Plan
}

func (r *parseKeysPushdown) apply(node Node) bool {
    switch node.(type) {
    case *ParseNode:
        return false // Will be expanded in next steps
    }
    return false
}
```

---

### Step 6: Collect Ambiguous Columns from Filter Predicates

**Behavior Being Added**: ParseNode collects ambiguous columns referenced in downstream Filter predicates as keys that might need parsing.

**Test to Write**: `TestParseKeysPushdownCollectsFilterKeys` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create a plan with: ParseNode -> Filter(predicates on ambiguous columns "level", "msg") -> DataObjScan
- Assert that after optimization, ParseNode.RequestedKeys contains ["level", "msg"] (sorted)

**Why Meaningful**: Fails because `collectRequiredKeys` doesn't examine Filter predicates for ambiguous columns yet. Tests the most common case.

**Minimal Implementation**:
```go
func (r *parseKeysPushdown) apply(node Node) bool {
    switch node := node.(type) {
    case *ParseNode:
        requiredKeys := r.collectRequiredKeys(node)
        if !reflect.DeepEqual(node.RequestedKeys, requiredKeys) {
            node.RequestedKeys = requiredKeys
            return true
        }
    }
    return false
}

func (r *parseKeysPushdown) collectRequiredKeys(parseNode Node) []string {
    keysMap := make(map[string]bool)
    
    r.walkDescendants(parseNode, func(n Node) {
        switch n := n.(type) {
        case *Filter:
            for _, col := range extractColumnsFromPredicates(n.Predicates) {
                if colExpr, ok := col.(*ColumnExpr); ok {
                    // Only collect ambiguous columns (might need parsing)
                    if colExpr.Ref.Type == types.ColumnTypeAmbiguous {
                        keysMap[colExpr.Ref.Column] = true
                    }
                }
            }
        }
    })
    
    // Convert to sorted slice
    keys := make([]string, 0, len(keysMap))
    for k := range keysMap {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    return keys
}
```

---

### Step 7: Skip Label Columns in Filter Predicates

**Behavior Being Added**: ParseNode does NOT collect columns marked as `ColumnTypeLabel` since they don't need parsing.

**Test to Write**: `TestParseKeysPushdownSkipsLabels` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create a plan with: ParseNode -> Filter(label column "app", ambiguous column "level") -> DataObjScan
- Assert that after optimization, ParseNode.RequestedKeys contains only ["level"], not "app"

**Why Meaningful**: Fails if we collect all columns regardless of type. Tests that we respect column types from logical planner.

**Minimal Implementation**: Already complete from Step 6 (checks for `ColumnTypeAmbiguous`).

---

### Step 8: Collect Ambiguous Columns from Aggregations

**Behavior Being Added**: ParseNode collects ambiguous columns used in aggregation GroupBy and PartitionBy.

**Test to Write**: `TestParseKeysPushdownCollectsAggregationKeys` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create plans with:
  - VectorAggregation(GroupBy: ambiguous "status", label "app")
  - RangeAggregation(PartitionBy: ambiguous "duration")
- Assert that ParseNode.RequestedKeys contains only ambiguous columns

**Why Meaningful**: Fails because `collectRequiredKeys` doesn't handle aggregation nodes yet. Tests metric query requirements.

**Minimal Implementation**:
Add to the switch in `walkDescendants`:
```go
case *VectorAggregation:
    for _, col := range n.GroupBy {
        if colExpr, ok := col.(*ColumnExpr); ok {
            if colExpr.Ref.Type == types.ColumnTypeAmbiguous {
                keysMap[colExpr.Ref.Column] = true
            }
        }
    }
case *RangeAggregation:
    for _, col := range n.PartitionBy {
        if colExpr, ok := col.(*ColumnExpr); ok {
            if colExpr.Ref.Type == types.ColumnTypeAmbiguous {
                keysMap[colExpr.Ref.Column] = true
            }
        }
    }
```

---

### Step 9: Integrate Rule into Optimization Pipeline

**Behavior Being Added**: The parseKeysPushdown rule runs as part of the standard optimization pipeline.

**Test to Write**: `TestParseKeysPushdownIntegration` (NEW)
- Location: `pkg/engine/planner/physical/planner_test.go`
- Create a full logical plan with logfmt parsing
- Run it through Build and Optimize
- Assert that the ParseNode has correct RequestedKeys based on downstream operations

**Why Meaningful**: Fails because the rule isn't registered in `Planner.Optimize()`. Tests end-to-end optimization.

**Minimal Implementation**:
In `Planner.Optimize()`:
```go
newOptimization("ParseKeysPushdown", plan).withRules(
    &parseKeysPushdown{plan: plan},
),
```

---

### Step 10: Update Logical Parse to Not Set Keys

**Behavior Being Changed**: Logical Parse node no longer specifies RequestedKeys at creation time (optimization determines them).

**Test to Update**: `TestLogicalParseHasNoKeys` (NEW)
- Location: `pkg/engine/planner/logical/planner_test.go`
- Assert that Parse nodes created by the logical planner have empty RequestedKeys

**Why Meaningful**: Fails because logical planner still sets keys. Tests separation of concerns.

**Minimal Implementation**:
1. Remove RequestedKeys parameter from `Builder.Parse()`
2. Remove key collection logic from `buildPlanForLogQuery`
3. Remove `collectLogfmtFilterKeys` and `collectLogfmtMetricKeys` functions

---

### Step 11: Verify End-to-End Behavior

**Behavior Being Verified**: Complete queries work correctly with the new optimization.

**Test to Write**: `TestEndToEndLogfmtOptimization` (NEW)
- Location: `integration/v2_query_engine_test.go`
- Run actual logfmt queries:
  - Log query with label and metadata filters
  - Metric query with groupBy on labels and parsed fields
- Assert correct results (not internal structure)

**Why Meaningful**: Fails if any part of the refactoring broke query execution. Tests complete behavior.

**Minimal Implementation**: No new implementation needed - validates all previous work.

---

## Execution Checklist

For each step:
- [ ] Write/modify the test as specified
- [ ] Run the test - verify it fails for the right reason (missing behavior, not syntax)
- [ ] **PAUSE for review of the test**
- [ ] Implement the minimal code to make the test pass
- [ ] Run the test again - verify it passes
- [ ] **PAUSE for review of the implementation**
- [ ] Continue to next step

## Success Criteria

1. Stream labels are correctly identified as `ColumnTypeLabel`
2. Other columns remain `ColumnTypeAmbiguous` (metadata vs parsed determined at runtime)
3. No registry or resolver in physical planner
4. Parse optimization rule collects only ambiguous columns
5. All existing queries continue to work correctly

## Notes

- We're NOT trying to distinguish metadata from parsed fields (impossible at planning time)
- We ARE distinguishing labels from ambiguous columns (possible from stream selector)
- The executor already handles ambiguous columns correctly (unchanged)
- This simplifies the system significantly by removing unnecessary state tracking