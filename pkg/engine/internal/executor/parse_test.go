package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestNewParsePipeline_logfmt(t *testing.T) {
	var (
		colTs  = "timestamp_ns.builtin.timestamp"
		colMsg = "utf8.builtin.message"
	)

	for _, tt := range []struct {
		name           string
		schema         *arrow.Schema
		input          arrowtest.Rows
		requestedKeys  []string
		expectedFields int
		expectedOutput arrowtest.Rows
	}{
		{
			name: "parse stage transforms records, adding columns parsed from message",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=error status=500"},
				{colMsg: "level=info status=200"},
				{colMsg: "level=debug status=201"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 3, // 3 columns: message, level, status
			expectedOutput: arrowtest.Rows{
				{
					colMsg:               "level=error status=500",
					"utf8.parsed.level":  "error",
					"utf8.parsed.status": "500",
				},
				{
					colMsg:               "level=info status=200",
					"utf8.parsed.level":  "info",
					"utf8.parsed.status": "200",
				},
				{
					colMsg:               "level=debug status=201",
					"utf8.parsed.level":  "debug",
					"utf8.parsed.status": "201",
				},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", true),
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.app", true),
			}, nil),
			input: arrowtest.Rows{
				{colTs: time.Unix(1, 0).UTC(), colMsg: "level=error status=500", "utf8.label.app": "frontend"},
				{colTs: time.Unix(2, 0).UTC(), colMsg: "level=info status=200", "utf8.label.app": "backend"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedOutput: arrowtest.Rows{
				{
					colTs:                time.Unix(1, 0).UTC(),
					colMsg:               "level=error status=500",
					"utf8.label.app":     "frontend",
					"utf8.parsed.level":  "error",
					"utf8.parsed.status": "500",
				},
				{
					colTs:                time.Unix(2, 0).UTC(),
					colMsg:               "level=info status=200",
					"utf8.label.app":     "backend",
					"utf8.parsed.level":  "info",
					"utf8.parsed.status": "200",
				},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=error"},
				{colMsg: "status=200"},
				{colMsg: "level=info"},
			},
			requestedKeys:  []string{"level"},
			expectedFields: 2, // 2 columns: message, level
			expectedOutput: arrowtest.Rows{
				{
					colMsg:              "level=error",
					"utf8.parsed.level": "error",
				},
				{
					colMsg:              "status=200",
					"utf8.parsed.level": nil,
				},
				{
					colMsg:              "level=info",
					"utf8.parsed.level": "info",
				},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200"},       // No errors
				{colMsg: "status==value level=error"},   // Double equals error on requested key
				{colMsg: "level=\"unclosed status=500"}, // Unclosed quote error
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: message, level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                                "level=info status=200",
					"utf8.parsed.level":                   "info",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                "status==value level=error",
					"utf8.parsed.level":                   nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 8 : unexpected '='",
				},
				{
					colMsg:                                "level=\"unclosed status=500",
					"utf8.parsed.level":                   nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 27 : unterminated quoted value",
				},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 method=GET"},
				{colMsg: "level=warn code=304"},
				{colMsg: "level=error status=500 method=POST duration=123ms"},
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, code, duration, level, method, status
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                 "level=info status=200 method=GET",
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "info",
					"utf8.parsed.method":   "GET",
					"utf8.parsed.status":   "200",
				},
				{
					colMsg:                 "level=warn code=304",
					"utf8.parsed.code":     "304",
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "warn",
					"utf8.parsed.method":   nil,
					"utf8.parsed.status":   nil,
				},
				{
					colMsg:                 "level=error status=500 method=POST duration=123ms",
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": "123ms",
					"utf8.parsed.level":    "error",
					"utf8.parsed.method":   "POST",
					"utf8.parsed.status":   "500",
				},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 method=GET"},       // Valid line
				{colMsg: "level==error code=500"},                  // Double equals error
				{colMsg: "msg=\"unclosed duration=100ms code=400"}, // Unclosed quote error
				{colMsg: "level=debug method=POST"},                // Valid line
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, level, method, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                                "level=info status=200 method=GET",
					"utf8.parsed.level":                   "info",
					"utf8.parsed.method":                  "GET",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                "level==error code=500",
					"utf8.parsed.level":                   nil,
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 7 : unexpected '='",
				},
				{
					colMsg:                                "msg=\"unclosed duration=100ms code=400",
					"utf8.parsed.level":                   nil,
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 38 : unterminated quoted value",
				},
				{
					colMsg:                                "level=debug method=POST",
					"utf8.parsed.level":                   "debug",
					"utf8.parsed.method":                  "POST",
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer alloc.AssertSize(t, 0) // Assert empty on test exit

			// Create input data with message column containing logfmt
			input := NewArrowtestPipeline(
				alloc,
				tt.schema,
				tt.input,
			)

			// Create ParseNode requesting "level" field
			parseNode := &physical.ParseNode{
				Kind:          physical.ParserLogfmt,
				RequestedKeys: tt.requestedKeys,
			}

			pipeline := NewParsePipeline(parseNode, input, alloc)

			// Read first record
			ctx := t.Context()
			record, err := pipeline.Read(ctx)
			require.NoError(t, err)
			defer record.Release()

			// Verify the output has the expected number of fields
			outputSchema := record.Schema()
			require.Equal(t, tt.expectedFields, outputSchema.NumFields())

			// Convert record to rows for comparison
			actual, err := arrowtest.RecordRows(record)
			require.NoError(t, err)
			require.Equal(t, tt.expectedOutput, actual)
		})
	}
}

func TestNewParsePipeline_JSON(t *testing.T) {
	var (
		colTs  = "timestamp_ns.builtin.timestamp"
		colMsg = "utf8.builtin.message"
	)

	for _, tt := range []struct {
		name           string
		schema         *arrow.Schema
		input          arrowtest.Rows
		requestedKeys  []string
		expectedFields int
		expectedOutput arrowtest.Rows
	}{
		{
			name: "parse stage transforms records, adding columns parsed from message",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "error", "status": "500"}`},
				{colMsg: `{"level": "info", "status": "200"}`},
				{colMsg: `{"level": "debug", "status": "201"}`},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 3, // 3 columns: message, level, status
			expectedOutput: arrowtest.Rows{
				{colMsg: `{"level": "error", "status": "500"}`, "utf8.parsed.level": "error", "utf8.parsed.status": "500"},
				{colMsg: `{"level": "info", "status": "200"}`, "utf8.parsed.level": "info", "utf8.parsed.status": "200"},
				{colMsg: `{"level": "debug", "status": "201"}`, "utf8.parsed.level": "debug", "utf8.parsed.status": "201"},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", true),
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.app", true),
			}, nil),
			input: arrowtest.Rows{
				{colTs: time.Unix(1, 0).UTC(), colMsg: `{"level": "error", "status": "500"}`, "utf8.label.app": "frontend"},
				{colTs: time.Unix(2, 0).UTC(), colMsg: `{"level": "info", "status": "200"}`, "utf8.label.app": "backend"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedOutput: arrowtest.Rows{
				{colTs: time.Unix(1, 0).UTC(), colMsg: `{"level": "error", "status": "500"}`, "utf8.label.app": "frontend", "utf8.parsed.level": "error", "utf8.parsed.status": "500"},
				{colTs: time.Unix(2, 0).UTC(), colMsg: `{"level": "info", "status": "200"}`, "utf8.label.app": "backend", "utf8.parsed.level": "info", "utf8.parsed.status": "200"},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "error"}`},
				{colMsg: `{"status": "200"}`},
				{colMsg: `{"level": "info"}`},
			},
			requestedKeys:  []string{"level"},
			expectedFields: 2, // 2 columns: message, level
			expectedOutput: arrowtest.Rows{
				{colMsg: `{"level": "error"}`, "utf8.parsed.level": "error"},
				{colMsg: `{"status": "200"}`, "utf8.parsed.level": nil},
				{colMsg: `{"level": "info"}`, "utf8.parsed.level": "info"},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "info", "status": "200"}`}, // No errors
				{colMsg: `{"level": "error", "status":`},       // Missing closing brace and value
				{colMsg: `{"level": "info", "status": 200}`},   // Number should be converted to string
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: message, level, status, __error__, __error_details__ (due to malformed JSON)
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                                `{"level": "info", "status": "200"}`,
					"utf8.parsed.level":                   "info",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                `{"level": "error", "status":`,
					"utf8.parsed.level":                   nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        "JSONParserErr",
					semconv.ColumnIdentErrorDetails.FQN(): "Malformed JSON error",
				},
				{
					colMsg:                                `{"level": "info", "status": 200}`,
					"utf8.parsed.level":                   "info",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "info", "status": "200", "method": "GET"}`},
				{colMsg: `{"level": "warn", "code": "304"}`},
				{colMsg: `{"level": "error", "status": "500", "method": "POST", "duration": "123ms"}`},
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, code, duration, level, method, status
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                 `{"level": "info", "status": "200", "method": "GET"}`,
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "info",
					"utf8.parsed.method":   "GET",
					"utf8.parsed.status":   "200",
				},
				{
					colMsg:                 `{"level": "warn", "code": "304"}`,
					"utf8.parsed.code":     "304",
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "warn",
					"utf8.parsed.method":   nil,
					"utf8.parsed.status":   nil,
				},
				{
					colMsg:                 `{"level": "error", "status": "500", "method": "POST", "duration": "123ms"}`,
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": "123ms",
					"utf8.parsed.level":    "error",
					"utf8.parsed.method":   "POST",
					"utf8.parsed.status":   "500",
				},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "info", "status": "200", "method": "GET"}`}, // Valid line
				{colMsg: `{"level": "error", "code": 500}`},                     // Also valid, adds code column
				{colMsg: `{"msg": "unclosed}`},                                  // Unclosed quote
				{colMsg: `{"level": "debug", "method": "POST"}`},                // Valid line
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 7,   // 7 columns: message, level, method, status, code, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                                `{"level": "info", "status": "200", "method": "GET"}`,
					"utf8.parsed.level":                   "info",
					"utf8.parsed.method":                  "GET",
					"utf8.parsed.status":                  "200",
					"utf8.parsed.code":                    nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                `{"level": "error", "code": 500}`,
					"utf8.parsed.level":                   "error",
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					"utf8.parsed.code":                    "500",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                `{"msg": "unclosed}`,
					"utf8.parsed.level":                   nil,
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					"utf8.parsed.code":                    nil,
					semconv.ColumnIdentError.FQN():        "JSONParserErr",
					semconv.ColumnIdentErrorDetails.FQN(): "Value is string, but can't find closing '\"' symbol",
				},
				{
					colMsg:                                `{"level": "debug", "method": "POST"}`,
					"utf8.parsed.level":                   "debug",
					"utf8.parsed.method":                  "POST",
					"utf8.parsed.status":                  nil,
					"utf8.parsed.code":                    nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
		{
			name: "handle nested JSON objects with underscore flattening",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"user": {"name": "john", "details": {"age": "30", "city": "NYC"}}, "status": "active"}`},
				{colMsg: `{"app": {"version": "1.0", "config": {"debug": "true"}}, "level": "info"}`},
				{colMsg: `{"nested": {"deep": {"very": {"deep": "value"}}}}`},
			},
			requestedKeys:  nil, // Extract all keys including nested ones
			expectedFields: 9,   // message, app_config_debug, app_version, level, nested_deep_very_deep, status, user_details_age, user_details_city, user_name
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                              `{"user": {"name": "john", "details": {"age": "30", "city": "NYC"}}, "status": "active"}`,
					"utf8.parsed.app_config_debug":      nil,
					"utf8.parsed.app_version":           nil,
					"utf8.parsed.level":                 nil,
					"utf8.parsed.nested_deep_very_deep": nil,
					"utf8.parsed.status":                "active",
					"utf8.parsed.user_details_age":      "30",
					"utf8.parsed.user_details_city":     "NYC",
					"utf8.parsed.user_name":             "john",
				},
				{
					colMsg:                              `{"app": {"version": "1.0", "config": {"debug": "true"}}, "level": "info"}`,
					"utf8.parsed.app_config_debug":      "true",
					"utf8.parsed.app_version":           "1.0",
					"utf8.parsed.level":                 "info",
					"utf8.parsed.nested_deep_very_deep": nil,
					"utf8.parsed.status":                nil,
					"utf8.parsed.user_details_age":      nil,
					"utf8.parsed.user_details_city":     nil,
					"utf8.parsed.user_name":             nil,
				},
				{
					colMsg:                              `{"nested": {"deep": {"very": {"deep": "value"}}}}`,
					"utf8.parsed.app_config_debug":      nil,
					"utf8.parsed.app_version":           nil,
					"utf8.parsed.level":                 nil,
					"utf8.parsed.nested_deep_very_deep": "value",
					"utf8.parsed.status":                nil,
					"utf8.parsed.user_details_age":      nil,
					"utf8.parsed.user_details_city":     nil,
					"utf8.parsed.user_name":             nil,
				},
			},
		},
		{
			name: "handle nested JSON with specific requested keys",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"user": {"name": "alice", "profile": {"email": "alice@example.com"}}, "level": "debug"}`},
				{colMsg: `{"user": {"name": "bob"}, "level": "info"}`},
				{colMsg: `{"level": "error", "error": {"code": "500", "message": "internal"}}`},
			},
			requestedKeys:  []string{"user_name", "user_profile_email", "level"},
			expectedFields: 4, // message, level, user_name, user_profile_email
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                           `{"user": {"name": "alice", "profile": {"email": "alice@example.com"}}, "level": "debug"}`,
					"utf8.parsed.level":              "debug",
					"utf8.parsed.user_name":          "alice",
					"utf8.parsed.user_profile_email": "alice@example.com",
				},
				{
					colMsg:                           `{"user": {"name": "bob"}, "level": "info"}`,
					"utf8.parsed.level":              "info",
					"utf8.parsed.user_name":          "bob",
					"utf8.parsed.user_profile_email": nil,
				},
				{
					colMsg:                           `{"level": "error", "error": {"code": "500", "message": "internal"}}`,
					"utf8.parsed.level":              "error",
					"utf8.parsed.user_name":          nil,
					"utf8.parsed.user_profile_email": nil,
				},
			},
		},
		{
			name: "accept JSON numbers as strings (v1 compatibility)",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"status": 200, "port": 8080, "timeout": 30.5, "retries": 0}`},
				{colMsg: `{"level": "info", "pid": 12345, "memory": 256.8}`},
				{colMsg: `{"score": -1, "version": 2.1, "enabled": true}`},
			},
			requestedKeys:  []string{"status", "port", "timeout", "pid", "memory", "score", "version"},
			expectedFields: 8, // message, memory, pid, port, score, status, timeout, version
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                `{"status": 200, "port": 8080, "timeout": 30.5, "retries": 0}`,
					"utf8.parsed.memory":  nil,
					"utf8.parsed.pid":     nil,
					"utf8.parsed.port":    "8080",
					"utf8.parsed.score":   nil,
					"utf8.parsed.status":  "200",
					"utf8.parsed.timeout": "30.5",
					"utf8.parsed.version": nil,
				},
				{
					colMsg:                `{"level": "info", "pid": 12345, "memory": 256.8}`,
					"utf8.parsed.memory":  "256.8",
					"utf8.parsed.pid":     "12345",
					"utf8.parsed.port":    nil,
					"utf8.parsed.score":   nil,
					"utf8.parsed.status":  nil,
					"utf8.parsed.timeout": nil,
					"utf8.parsed.version": nil,
				},
				{
					colMsg:                `{"score": -1, "version": 2.1, "enabled": true}`,
					"utf8.parsed.memory":  nil,
					"utf8.parsed.pid":     nil,
					"utf8.parsed.port":    nil,
					"utf8.parsed.score":   "-1",
					"utf8.parsed.status":  nil,
					"utf8.parsed.timeout": nil,
					"utf8.parsed.version": "2.1",
				},
			},
		},
		{
			name: "mixed nested objects and numbers",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"request": {"url": "/api/users", "method": "GET"}, "response": {"status": 200, "time": 45.2}}`},
				{colMsg: `{"user": {"id": 123, "profile": {"age": 25}}, "active": true}`},
			},
			requestedKeys:  nil, // Extract all keys
			expectedFields: 8,   // message, active, request_method, request_url, response_status, response_time, user_id, user_profile_age
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                         `{"request": {"url": "/api/users", "method": "GET"}, "response": {"status": 200, "time": 45.2}}`,
					"utf8.parsed.active":           nil,
					"utf8.parsed.request_method":   "GET",
					"utf8.parsed.request_url":      "/api/users",
					"utf8.parsed.response_status":  "200",
					"utf8.parsed.response_time":    "45.2",
					"utf8.parsed.user_id":          nil,
					"utf8.parsed.user_profile_age": nil,
				},
				{
					colMsg:                         `{"user": {"id": 123, "profile": {"age": 25}}, "active": true}`,
					"utf8.parsed.active":           "true",
					"utf8.parsed.request_method":   nil,
					"utf8.parsed.request_url":      nil,
					"utf8.parsed.response_status":  nil,
					"utf8.parsed.response_time":    nil,
					"utf8.parsed.user_id":          "123",
					"utf8.parsed.user_profile_age": "25",
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer alloc.AssertSize(t, 0) // Assert empty on test exit

			// Create input data with message column containing JSON
			input := NewArrowtestPipeline(
				alloc,
				tt.schema,
				tt.input,
			)

			// Create ParseNode for JSON parsing
			parseNode := &physical.ParseNode{
				Kind:          physical.ParserJSON,
				RequestedKeys: tt.requestedKeys,
			}

			pipeline := NewParsePipeline(parseNode, input, alloc)

			// Read first record
			ctx := t.Context()
			record, err := pipeline.Read(ctx)
			require.NoError(t, err)
			defer record.Release()

			// Verify the output has the expected number of fields
			outputSchema := record.Schema()
			require.Equal(t, tt.expectedFields, outputSchema.NumFields())

			// Convert record to rows for comparison
			actual, err := arrowtest.RecordRows(record)
			require.NoError(t, err)
			require.Equal(t, tt.expectedOutput, actual)
		})
	}
}
