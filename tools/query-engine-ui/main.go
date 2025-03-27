package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
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
}

func safe(s string) string {
	return html.EscapeString(s)
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
	return logical.BuildPlan(q)
}

// Mock function to generate a physical plan from a logical plan
// In a real implementation, this would use the actual Loki query engine
func generatePhysicalPlan(logicalPlan *logical.Plan) (*physical.Plan, error) {
	catalog := &catalog{
		streamsByObject: map[string][]int64{
			"obj1": {1, 2},
			"obj2": {3, 4},
			"obj3": {5, 6},
		},
	}
	planner := physical.NewPlanner(catalog)
	plan, err := planner.Build(logicalPlan)
	if err != nil {
		return nil, err
	}
	return planner.Optimize(plan)
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

		fmt.Println("LOGICAL PLAN")
		fmt.Println(logicalPlan.String())
		fmt.Println("")

		// Generate physical plan
		physicalPlan, err := generatePhysicalPlan(logicalPlan)
		if err != nil {
			response := PlanResponse{Error: fmt.Sprintf("Error generating physical plan: %v", err)}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		fmt.Println("PHYSICAL PLAN")
		fmt.Println(physical.PrintAsTree(physicalPlan))
		fmt.Println("")

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
	fs := http.FileServer(http.Dir("./tools/query-engine-ui/static"))
	http.Handle("/", fs)

	// Start the server
	serverAddr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting server on http://localhost%s", serverAddr)
	log.Fatal(http.ListenAndServe(serverAddr, nil))
}
