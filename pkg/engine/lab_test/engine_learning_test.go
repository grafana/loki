package engine_lab

/*
================================================================================
LOKI QUERY ENGINE V2 - LEARNING LAB INDEX
================================================================================

This package contains a comprehensive learning lab for understanding the
Loki Query Engine V2 architecture. The tests are organized by stage, mirroring
the query execution pipeline.

================================================================================
QUERY EXECUTION PIPELINE OVERVIEW
================================================================================

  LogQL Query String
        │
        ▼ (syntax.ParseExpr)
  1. PARSING → syntax.Expr (AST)
        │
        ▼ (logical.BuildPlan)
  2. LOGICAL PLANNING → logical.Plan (SSA IR)
        │      See: stage1_logical_planning_test.go
        ▼ (physical.Planner.Build)
  3. PHYSICAL PLANNING → physical.Plan (DAG)
        │      See: stage2_physical_planning_test.go
        ▼ (physical.Planner.Optimize)
  3b. OPTIMIZATION → physical.Plan (Optimized DAG)
        │      See: stage2_physical_planning_test.go
        ▼ (workflow.New)
  4. WORKFLOW PLANNING → workflow.Workflow (Task Graph)
        │      See: stage3_workflow_planning_test.go
        ▼ (workflow.Run)
  5. EXECUTION → executor.Pipeline
        │      See: stage4_execution_test.go
        ▼ (Pipeline.Read)
  6. ARROW DATA → arrow.RecordBatch
        │      See: arrow_fundamentals_test.go
        ▼ (ResultBuilder)
  7. RESULT BUILDING → logqlmodel.Result
        │      See: stage4_execution_test.go
        ▼
  Final Result

================================================================================
LAB FILES OVERVIEW
================================================================================

STAGE 1: LOGICAL PLANNING (stage1_logical_planning_test.go)
  - SSA (Static Single Assignment) intermediate representation
  - Column type prefixes (label.*, metadata.*, parsed.*, builtin.*, ambiguous.*)
  - Instruction types (EQ, MATCH_STR, SELECT, PROJECT, TOPK, etc.)
  - Log queries vs metric queries
  - Pipeline stages (filters, parsers, aggregations)

STAGE 2: PHYSICAL PLANNING (stage2_physical_planning_test.go)
  - DAG (Directed Acyclic Graph) structure
  - Physical node types (DataObjScan, ScanSet, Filter, RangeAggregation, etc.)
  - Catalog system for resolving data sources
  - Optimization passes (predicate pushdown, limit pushdown, etc.)

STAGE 3: WORKFLOW PLANNING (stage3_workflow_planning_test.go)
  - Task partitioning and pipeline breakers
  - Stream connections between tasks
  - Manifest registration with scheduler
  - Admission control for resource management
  - Task lifecycle (Created → Pending → Running → Completed)

STAGE 4: EXECUTION (stage4_execution_test.go)
  - Pipeline interface and implementation
  - Arrow RecordBatch processing
  - Result builders (streams, vector, matrix)
  - Buffered and lazy pipelines

STAGE 5: DISTRIBUTED EXECUTION (stage5_distributed_execution_test.go)
  - Scheduler/Worker architecture
  - Task assignment and load balancing
  - Stream bindings and data flow
  - Wire protocol (local and remote communication)

ARROW FUNDAMENTALS (arrow_fundamentals_test.go)
  - Columnar data format
  - Schema definition
  - RecordBatch creation and manipulation
  - Memory management (Retain/Release)
  - Semantic column conventions

END-TO-END (end_to_end_test.go)
  - Complete query execution flow
  - Integration of all stages
  - Real-world query examples

UTILITIES (learning_test_utils.go)
  - Mock implementations (mockQuery, mockCatalog, mockPipeline)
  - Test runner for workflow testing
  - Helper functions for creating test data

================================================================================
HOW TO USE THIS LAB
================================================================================

1. START WITH THE ARCHITECTURE DOCUMENT
   Read ENGINE_V2_ARCHITECTURE.md for a high-level overview.

2. FOLLOW THE STAGES IN ORDER
   Stage 1 → Stage 2 → Stage 3 → Stage 4 → Stage 5

3. RUN THE TESTS WITH VERBOSE OUTPUT
   go test -v -run TestLogicalPlanning ./pkg/engine/lab_test/

4. EXAMINE THE SSA/DAG OUTPUT
   Each test logs the plan structure for inspection.

5. EXPERIMENT
   Modify queries and observe how plans change.

================================================================================
KEY CONCEPTS QUICK REFERENCE
================================================================================

SSA (Static Single Assignment):
  - Each variable assigned exactly once
  - Variables: %1, %2, %3, ...
  - Makes data flow explicit

DAG (Directed Acyclic Graph):
  - Nodes = operators (scan, filter, aggregate)
  - Edges = data flow
  - Multiple inputs for joins

PIPELINE BREAKERS:
  - TopK, RangeAggregation, VectorAggregation
  - Force task boundaries in workflow planning
  - Require all input data before producing output

ARROW RECORDBATCH:
  - Columnar data format
  - Reference counted memory
  - Schema + Column arrays

COLUMN TYPE PREFIXES:
  - label.*    : Stream labels (ingestion-time)
  - metadata.* : Structured metadata (per-line)
  - parsed.*   : Extracted during query (json, logfmt)
  - builtin.*  : Engine columns (timestamp, message, value)
  - ambiguous.*: Type unknown until physical planning

================================================================================
*/

// This file serves as the index/overview for the learning lab.
// All actual tests are in the stage-specific files.
