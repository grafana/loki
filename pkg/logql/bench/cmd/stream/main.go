package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/bench"
)

type logEntry struct {
	Timestamp string            `json:"ts"`
	Labels    map[string]string `json:"labels"`
	Line      string            `json:"line"`
	Resource  map[string]string `json:"resource,omitempty"` // OTEL resource attributes
	Trace     *traceContext     `json:"trace,omitempty"`    // OTEL trace context
}

type traceContext struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

func main() {
	var (
		format     = flag.String("format", "json", "Output format: json or raw")
		startTime  = flag.String("start", "2024-01-01T00:00:00Z", "Start time in RFC3339 format")
		timeSpread = flag.Duration("spread", 24*time.Hour, "Time spread for the logs")
		dense      = flag.Bool("dense", false, "Include dense intervals")
		apps       = flag.String("apps", "", "Comma-separated list of applications to include (web-server,nginx,mysql,kafka). Empty means all.")
		showHelp   = flag.Bool("help", false, "Show detailed help")
	)
	flag.Parse()

	if *showHelp {
		fmt.Fprintf(os.Stderr, `Log Generator Help:
Available Applications:
  - web-server: Modern web application with HTTP request logs
  - nginx: Nginx web server access and error logs
  - mysql: MySQL database query and error logs
  - kafka: Kafka broker events and errors

Output Formats:
  - json: Structured JSON output with OTEL attributes
  - raw: Simple text format

Example Commands:
  # Generate all application logs:
  %s

  # Generate only nginx and mysql logs:
  %s -apps nginx,mysql

  # Generate dense traffic for web-server:
  %s -apps web-server -dense

  # Generate logs for a specific time range:
  %s -start 2024-03-14T00:00:00Z -spread 1h

OpenTelemetry Attributes:
  Logs include standard OTEL resource attributes and trace context.
  Resource attributes are prefixed with "resource_" in labels.
  About 30%% of logs include trace context for distributed tracing.
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0])
		os.Exit(0)
	}

	// Parse start time
	start, err := time.Parse(time.RFC3339, *startTime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid start time: %v\n", err)
		os.Exit(1)
	}

	// Parse applications filter
	var appFilter map[string]bool
	if *apps != "" {
		appFilter = make(map[string]bool)
		for _, app := range strings.Split(*apps, ",") {
			appFilter[strings.TrimSpace(app)] = true
		}
	}

	currentTime := start
	// Stream logs infinitely
	for {
		// Generate a batch of logs
		opt := bench.DefaultOpt().
			WithStartTime(currentTime).
			WithTimeSpread(*timeSpread)

		// Add dense intervals if enabled
		if *dense {
			opt = opt.
				WithDenseInterval(currentTime.Add(10*time.Hour), time.Hour).
				WithDenseInterval(currentTime.Add(15*time.Hour), 30*time.Minute)
		}

		g := bench.NewGenerator(opt)
		for batch := range g.Batches() {
			for _, stream := range batch.Streams {
				// Parse labels into a map
				labels := parseLabels(stream.Labels)

				// Filter by application if specified
				if appFilter != nil {
					component := labels["service_name"]
					var matchesFilter bool
					switch component {
					case "web-server":
						matchesFilter = appFilter["web-server"]
					case "mysql":
						matchesFilter = appFilter["mysql"]
					case "redis":
						matchesFilter = appFilter["redis"]
					case "auth-service":
						matchesFilter = appFilter["auth-service"]
					}
					if !matchesFilter {
						continue
					}
				}

				for _, entry := range stream.Entries {
					if *format == "json" {
						// Extract OTEL resource attributes and trace context
						resource := make(map[string]string)
						var trace *traceContext

						for _, meta := range entry.StructuredMetadata {
							if strings.HasPrefix(meta.Name, "resource_") {
								key := strings.TrimPrefix(meta.Name, "resource_")
								resource[key] = meta.Value
							} else if meta.Name == "trace_id" {
								if trace == nil {
									trace = &traceContext{}
								}
								trace.TraceID = meta.Value
							} else if meta.Name == "span_id" {
								if trace == nil {
									trace = &traceContext{}
								}
								trace.SpanID = meta.Value
							}
						}

						// Output in JSON format
						log := logEntry{
							Timestamp: entry.Timestamp.Format(time.RFC3339),
							Labels:    labels,
							Line:      entry.Line,
							Resource:  resource,
							Trace:     trace,
						}
						jsonBytes, err := json.Marshal(log)
						if err != nil {
							continue
						}
						fmt.Println(string(jsonBytes))
					} else {
						// Output in raw format with OTEL attributes
						fmt.Printf("%s %s %s", entry.Timestamp.Format(time.RFC3339), stream.Labels, entry.Line)
						// Add OTEL attributes in a readable format
						for _, meta := range entry.StructuredMetadata {
							if strings.HasPrefix(meta.Name, "resource_") || meta.Name == "trace_id" || meta.Name == "span_id" {
								fmt.Printf(" %s=%s", meta.Name, meta.Value)
							}
						}
						fmt.Println()
					}
				}
			}
		}

		// Update start time for the next iteration
		currentTime = currentTime.Add(*timeSpread)
	}
}

// parseLabels converts a Prometheus-style label string into a map
func parseLabels(labels string) map[string]string {
	// Remove the enclosing braces
	labels = labels[1 : len(labels)-1]

	result := make(map[string]string)
	// Split by comma and space
	pairs := splitLabels(labels)
	for _, pair := range pairs {
		// Find the equals sign
		for i := 0; i < len(pair); i++ {
			if pair[i] == '=' {
				key := pair[:i]
				// Remove the quotes from the value
				value := pair[i+2 : len(pair)-1]
				result[key] = value
				break
			}
		}
	}
	return result
}

// splitLabels splits Prometheus-style labels into individual key=value pairs
func splitLabels(labels string) []string {
	var result []string
	var current []byte
	inQuotes := false

	for i := 0; i < len(labels); i++ {
		c := labels[i]
		if c == '"' {
			inQuotes = !inQuotes
		}
		if c == ',' && !inQuotes {
			// Skip the space after the comma
			i++
			if len(current) > 0 {
				result = append(result, string(current))
				current = current[:0]
			}
			continue
		}
		current = append(current, c)
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}
