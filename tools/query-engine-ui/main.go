package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// PlanResponse represents the API response structure
type PlanResponse struct {
	LogicalPlan  string `json:"logicalPlan"`
	PhysicalPlan string `json:"physicalPlan"`
	Error        string `json:"error,omitempty"`
}

// QueryRequest represents the API request structure
type QueryRequest struct {
	LogQL string `json:"logql"`
}

// Convert a physical plan to a Mermaid diagram representation
func physicalPlanToMermaid(plan *physical.Plan) string {
	if plan == nil || plan.Len() == 0 {
		return "graph TD\n  Empty[Empty Plan]"
	}

	var sb strings.Builder
	physical.WriteMermaidFormat(&sb, plan)
	return sb.String()
}

// Convert a logical plan to a Mermaid diagram representation
func logicalPlanToMermaid(plan *logical.Plan) string {
	if plan == nil || len(plan.Instructions) == 0 {
		return "graph TD\n  node0[Empty Plan]"
	}

	var sb strings.Builder
	logical.WriteMermaidFormat(&sb, plan)
	return sb.String()

	// var sb strings.Builder
	// sb.WriteString("graph BT\n")

	// id := 0

	// for _, inst := range plan.Instructions {
	// 	switch inst := inst.(type) {
	// 	case *logical.Return:
	// 		nodeID := fmt.Sprintf("node%d", id)
	// 		fmt.Fprintf(&sb, "  %s[\"%s\"]\n", nodeID, inst.String())
	// 		child := processLogicalPlan(&sb, &id, inst.Value)
	// 		childID := fmt.Sprintf("node%d", child)
	// 		fmt.Fprintf(&sb, "  %s --- %s\n", nodeID, childID)
	// 	}
	// }

	// return sb.String()
}

func safe(s string) string {
	return strings.ReplaceAll(s, `"`, `&quot;`)
}

func processLogicalPlan(sb io.Writer, id *int, inst logical.Value) int {
	*id++

	currID := *id
	nodeID := fmt.Sprintf("node%d", currID)

	var children []int
	switch inst := inst.(type) {
	case *logical.MakeTable:
		fmt.Fprintf(sb, "  %s[\"%s = %s\"]\n", nodeID, safe(inst.Name()), safe(inst.String()))
		children = append(children, processLogicalPlan(sb, id, inst.Selector))
	case *logical.Select:
		fmt.Fprintf(sb, "  %s[\"%s = %s\"]\n", nodeID, safe(inst.Name()), safe(inst.String()))
		children = append(children, processLogicalPlan(sb, id, inst.Table))
		children = append(children, processLogicalPlan(sb, id, inst.Predicate))
	case *logical.Sort:
		fmt.Fprintf(sb, "  %s[\"%s = %s\"]\n", nodeID, safe(inst.Name()), safe(inst.String()))
		children = append(children, processLogicalPlan(sb, id, inst.Table))
	case *logical.Limit:
		fmt.Fprintf(sb, "  %s[\"%s = %s\"]\n", nodeID, safe(inst.Name()), safe(inst.String()))
		children = append(children, processLogicalPlan(sb, id, inst.Table))
	case *logical.BinOp:
		fmt.Fprintf(sb, "  %s[\"%s = %s\"]\n", nodeID, safe(inst.Name()), safe(inst.String()))
		children = append(children, processLogicalPlan(sb, id, inst.Left))
		children = append(children, processLogicalPlan(sb, id, inst.Right))
	case *logical.UnaryOp:
		fmt.Fprintf(sb, "  %s[\"%s = %s\"]\n", nodeID, safe(inst.Name()), safe(inst.String()))
		children = append(children, processLogicalPlan(sb, id, inst.Value))
	case *logical.Literal, *logical.ColumnRef:
		fmt.Fprintf(sb, "  %s[\"%T(%s)\"]\n", nodeID, inst, safe(inst.Name()))
	}

	for _, child := range children {
		childID := fmt.Sprintf("node%d", child)
		fmt.Fprintf(sb, "  %s --- %s\n", nodeID, childID)
	}
	return currID
}

// Generate a logical plan from LogQL using the actual Loki query engine
func generateLogicalPlan(logQL string) (*logical.Plan, error) {
	// Parse the LogQL expression
	expr, err := syntax.ParseExpr(logQL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LogQL: %w", err)
	}

	// Create a logql.Params object with the parsed expression and time range
	// Using a default time range for demonstration purposes
	now := time.Now()
	q := &query{
		expr:      expr,
		start:     now.Add(-1 * time.Hour),
		end:       now,
		step:      time.Minute,
		direction: logproto.FORWARD,
		limit:     1000,
	}

	// Convert to logical plan
	return logical.ConvertToLogicalPlan(q)
}

// Mock function to generate a physical plan from a logical plan
// In a real implementation, this would use the actual Loki query engine
func generatePhysicalPlan(logicalPlan *logical.Plan) (*physical.Plan, error) {
	catalog := &catalog{
		streamsByObject: map[string][]int64{
			"obj1": {1, 2},
			"obj2": {3, 4},
		},
	}
	planner := physical.NewPlanner(catalog)
	return planner.Build(logicalPlan)
}

func main() {
	port := flag.Int("port", 3111, "Port to run the server on")
	flag.Parse()

	// API endpoint for processing LogQL queries
	http.HandleFunc("/api/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		logQL := req.LogQL
		if logQL == "" {
			http.Error(w, "LogQL query is required", http.StatusBadRequest)
			return
		}

		// Generate logical plan
		logicalPlan, err := generateLogicalPlan(logQL)
		if err != nil {
			response := PlanResponse{Error: fmt.Sprintf("Error generating logical plan: %v", err)}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		// Generate physical plan
		physicalPlan, err := generatePhysicalPlan(logicalPlan)
		if err != nil {
			response := PlanResponse{Error: fmt.Sprintf("Error generating physical plan: %v", err)}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		// Convert plans to Mermaid diagrams
		logicalMermaid := logicalPlanToMermaid(logicalPlan)
		physicalMermaid := physicalPlanToMermaid(physicalPlan)

		// Send response
		response := PlanResponse{
			LogicalPlan:  logicalMermaid,
			PhysicalPlan: physicalMermaid,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Serve static files for the React UI
	fs := http.FileServer(http.Dir("./tools/query-engine-ui/build"))
	http.Handle("/", fs)

	// Create the build directory if it doesn't exist
	buildDir := "./tools/query-engine-ui/build"
	if _, err := os.Stat(buildDir); os.IsNotExist(err) {
		os.MkdirAll(buildDir, 0755)
	}

	// Start the server
	serverAddr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting server on http://localhost%s", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, nil))
}
