# TDD Execution Plan: Move Parse Key Collection to Optimization Phase

## Overview
This plan refactors the logfmt parse key collection from the logical planner (where it determines WHAT keys to parse during planning) to the physical planner's optimization phase (where it determines HOW to optimize parsing based on downstream needs).

## Core Principle
**Single Unified Optimization Rule**: We will create one optimization rule that inspects the physical plan tree to determine which keys need to be parsed, without distinguishing between log and metric queries.

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

## Test-Driven Implementation Steps

### Step 1: Create ParseKeysPushdown Rule Structure

**Behavior Being Added**: A new optimization rule that can be registered with the optimizer and will be called for ParseNode instances.

**Test to Write**: `TestParseKeysPushdownRuleApplies` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Assert that:
  - The rule's `apply` method returns false when given a non-ParseNode
  - The rule's `apply` method is called when given a ParseNode
  - The rule can be added to the optimization pipeline

**Why Meaningful**: This test will fail because the `parseKeysPushdown` type doesn't exist yet, not due to syntax errors. It verifies the rule integrates with the existing optimization framework.

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

### Step 2: Detect and Update ParseNode Without Keys

**Behavior Being Added**: When a ParseNode has no RequestedKeys, the rule should leave it empty (no keys needed by downstream).

**Test to Write**: `TestParseKeysPushdownEmptyWhenNoDownstreamNeeds` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create a plan with: ParseNode -> DataObjScan
- Assert that after optimization, ParseNode.RequestedKeys remains empty

**Why Meaningful**: Fails because the rule doesn't yet examine downstream nodes or update RequestedKeys. Tests the base case where no parsing is actually needed.

**Minimal Implementation**:
```go
func (r *parseKeysPushdown) apply(node Node) bool {
    switch node := node.(type) {
    case *ParseNode:
        // Check if any downstream nodes need keys
        requiredKeys := r.collectRequiredKeys(node)
        if len(requiredKeys) == 0 {
            return false
        }
        node.RequestedKeys = requiredKeys
        return true
    }
    return false
}

func (r *parseKeysPushdown) collectRequiredKeys(node Node) []string {
    return []string{} // Base implementation
}
```

---

### Step 3: Collect Keys from Filter Predicates

**Behavior Being Added**: ParseNode should extract only the keys referenced in downstream Filter predicates.

**Test to Write**: `TestParseKeysPushdownCollectsFilterKeys` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create a plan with: ParseNode -> Filter(predicates on "level", "msg") -> DataObjScan
- Assert that after optimization, ParseNode.RequestedKeys contains ["level", "msg"] (sorted)

**Why Meaningful**: Fails because `collectRequiredKeys` doesn't walk descendants or examine Filter predicates yet. Tests the most common case of label filtering.

**Minimal Implementation**:
```go
func (r *parseKeysPushdown) collectRequiredKeys(parseNode Node) []string {
    keysMap := make(map[string]bool)
    
    r.walkDescendants(parseNode, func(n Node) {
        switch n := n.(type) {
        case *Filter:
            for _, col := range extractColumnsFromPredicates(n.Predicates) {
                if colExpr, ok := col.(*ColumnExpr); ok {
                    // Only include non-builtin columns
                    if colExpr.Ref.Type != types.ColumnTypeBuiltin {
                        keysMap[colExpr.Ref.Column] = true
                    }
                }
            }
        }
    })
    
    keys := make([]string, 0, len(keysMap))
    for k := range keysMap {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    return keys
}

func (r *parseKeysPushdown) walkDescendants(node Node, visit func(Node)) {
    for _, child := range r.plan.Children(node) {
        visit(child)
        r.walkDescendants(child, visit)
    }
}
```

---

### Step 4: Collect Keys from VectorAggregation GroupBy

**Behavior Being Added**: ParseNode should include keys used in VectorAggregation GroupBy clauses.

**Test to Write**: `TestParseKeysPushdownCollectsVectorAggregationKeys` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create a plan with: ParseNode -> VectorAggregation(GroupBy: ["status", "method"]) -> RangeAggregation -> DataObjScan
- Assert that after optimization, ParseNode.RequestedKeys contains ["method", "status"] (sorted)

**Why Meaningful**: Fails because `collectRequiredKeys` doesn't handle VectorAggregation nodes yet. Tests metric query grouping requirements.

**Minimal Implementation**:
Add to the switch statement in `collectRequiredKeys`:
```go
case *VectorAggregation:
    for _, col := range n.GroupBy {
        if colExpr, ok := col.(*ColumnExpr); ok {
            keysMap[colExpr.Ref.Column] = true
        }
    }
```

---

### Step 5: Collect Keys from RangeAggregation PartitionBy

**Behavior Being Added**: ParseNode should include keys used in RangeAggregation PartitionBy (pushed down from VectorAggregation).

**Test to Write**: `TestParseKeysPushdownCollectsRangeAggregationKeys` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create a plan with: ParseNode -> RangeAggregation(PartitionBy: ["duration", "bytes"]) -> DataObjScan
- Assert that after optimization, ParseNode.RequestedKeys contains ["bytes", "duration"] (sorted)

**Why Meaningful**: Fails because `collectRequiredKeys` doesn't handle RangeAggregation nodes yet. Tests unwrap field requirements.

**Minimal Implementation**:
Add to the switch statement in `collectRequiredKeys`:
```go
case *RangeAggregation:
    for _, col := range n.PartitionBy {
        if colExpr, ok := col.(*ColumnExpr); ok {
            keysMap[colExpr.Ref.Column] = true
        }
    }
```

---

### Step 6: Handle Combined Requirements (Filter + Aggregation)

**Behavior Being Added**: ParseNode should collect keys from all downstream operations, deduplicating them.

**Test to Write**: `TestParseKeysPushdownCombinesAllRequirements` (NEW)
- Location: `pkg/engine/planner/physical/optimizer_test.go`
- Create a complex plan with:
  - ParseNode -> Filter(predicates on "level") -> VectorAggregation(GroupBy: ["status"]) -> RangeAggregation(PartitionBy: ["duration"]) -> DataObjScan
- Assert that after optimization, ParseNode.RequestedKeys contains ["duration", "level", "status"] (sorted, deduplicated)

**Why Meaningful**: Fails only if deduplication or sorting logic is incorrect. Tests that the rule handles realistic query patterns.

**Minimal Implementation**: Already complete from previous steps (map ensures deduplication, sort ensures order).

---

### Step 7: Integrate Rule into Optimization Pipeline

**Behavior Being Added**: The parseKeysPushdown rule runs as part of the standard optimization pipeline.

**Test to Write**: `TestParseKeysPushdownIntegration` (UPDATE EXISTING)
- Location: `pkg/engine/planner/physical/planner_test.go`
- Find or create a test for a logfmt query
- Assert that the optimized plan's ParseNode has the correct RequestedKeys based on the query

**Why Meaningful**: Fails because the rule isn't registered in `Planner.Optimize()` yet. Tests end-to-end optimization.

**Minimal Implementation**:
In `Planner.Optimize()`:
```go
optimizations := []*optimization{
    newOptimization("PredicatePushdown", plan).withRules(
        &predicatePushdown{plan: plan},
        &removeNoopFilter{plan: plan},
    ),
    newOptimization("LimitPushdown", plan).withRules(
        &limitPushdown{plan: plan},
    ),
    newOptimization("GroupByPushdown", plan).withRules(
        &groupByPushdown{plan: plan},
    ),
    newOptimization("ParseKeysPushdown", plan).withRules(
        &parseKeysPushdown{plan: plan},
    ),
    newOptimization("ProjectionPushdown", plan).withRules(
        &projectionPushdown{plan: plan},
    ),
}
```

---

### Step 8: Simplify Logical Parse Node

**Behavior Being Changed**: Logical Parse node no longer specifies RequestedKeys or NumericHints at creation time.

**Test to Update**: `TestLogicalPlannerLogfmtParsing` (UPDATE EXISTING)
- Location: `pkg/engine/planner/logical/planner_test.go`
- Modify test to assert that Parse nodes are created without RequestedKeys
- Assert that the logical plan structure is correct but doesn't include optimization details

**Why Meaningful**: Fails because the logical planner still sets RequestedKeys. Tests separation of concerns.

**Minimal Implementation**:
1. Update `logical.Parse` struct - remove RequestedKeys and NumericHints
2. Update `Builder.Parse()` method - remove parameters
3. Update `buildPlanForLogQuery` - remove key collection logic
4. Update `buildPlanForSampleQuery` - remove key collection calls

---

### Step 9: Update Physical ParseNode for Dynamic Keys

**Behavior Being Added**: Physical ParseNode can have its RequestedKeys updated after creation.

**Test to Write**: `TestParseNodeUpdatesKeys` (NEW)
- Location: `pkg/engine/planner/physical/parse_test.go`
- Create a ParseNode with no keys
- Update its RequestedKeys
- Assert the keys are stored and accessible

**Why Meaningful**: Fails if ParseNode doesn't properly store/expose RequestedKeys. Tests that optimization can modify the node.

**Minimal Implementation**:
Ensure `ParseNode` struct has:
```go
type ParseNode struct {
    id            string
    Kind          logical.ParserKind
    RequestedKeys []string  // Mutable field
}
```

---

### Step 10: Remove Obsolete Key Collection Functions

**Behavior Being Changed**: The system no longer uses AST-walking key collection functions.

**Test to Update**: Remove obsolete tests
- Location: `pkg/engine/planner/logical/key_collector_test.go`
- Delete tests for `collectLogfmtFilterKeys` and `collectLogfmtMetricKeys`

**Why Meaningful**: These functions are no longer called. Tests cleanup and ensures no dead code.

**Minimal Implementation**:
1. Delete `collectLogfmtFilterKeys` function
2. Delete `collectLogfmtMetricKeys` function
3. Delete related helper functions if no longer used
4. Remove imports that are no longer needed

---

### Step 11: Verify End-to-End Behavior

**Behavior Being Verified**: Complete queries work correctly with the new optimization.

**Test to Write**: `TestEndToEndLogfmtOptimization` (NEW)
- Location: `integration/v2_query_engine_test.go`
- Run actual logfmt queries with various patterns:
  - Log query with label filters
  - Metric query with groupBy
  - Metric query with unwrap
  - Complex query with all features
- Assert correct results (not internal structure)

**Why Meaningful**: Fails if any part of the refactoring broke query execution. Tests the complete behavior.

**Minimal Implementation**: No new implementation needed - this validates all previous work.

---

## Execution Checklist

For each step above:
- [ ] Write/modify the test as specified
- [ ] Run the test - verify it fails for the right reason (missing behavior, not syntax)
- [ ] **PAUSE for review of the test**
- [ ] Implement the minimal code to make the test pass
- [ ] Run the test again - verify it passes
- [ ] **PAUSE for review of the implementation**
- [ ] Continue to next step

## Success Criteria

The refactoring is complete when:
1. All tests pass
2. The logical planner no longer contains key collection logic
3. The physical planner's optimization phase determines required keys
4. No distinction between log/metric queries exists in the optimization rule
5. Existing queries continue to work correctly

## Notes

- Numeric type hints have been deferred from this plan as they require additional design decisions
- The rule runs after GroupByPushdown but before ProjectionPushdown
- The implementation follows existing optimization patterns in the codebase
- This approach eliminates the need for AST walking in the optimization phase