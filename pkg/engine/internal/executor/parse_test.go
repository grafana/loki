package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestNewParsePipeline_logfmt(t *testing.T) {
	var (
		colTs  = "timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp"
		colMsg = "utf8.COLUMN_TYPE_BUILTIN.message"
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
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					colMsg:                           "level=error status=500",
					"utf8.COLUMN_TYPE_PARSED.level":  "error",
					"utf8.COLUMN_TYPE_PARSED.status": "500",
				},
				{
					colMsg:                           "level=info status=200",
					"utf8.COLUMN_TYPE_PARSED.level":  "info",
					"utf8.COLUMN_TYPE_PARSED.status": "200",
				},
				{
					colMsg:                           "level=debug status=201",
					"utf8.COLUMN_TYPE_PARSED.level":  "debug",
					"utf8.COLUMN_TYPE_PARSED.status": "201",
				},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.app", true),
			}, nil),
			input: arrowtest.Rows{
				{colTs: time.Unix(1, 0).UTC(), colMsg: "level=error status=500", "utf8.COLUMN_TYPE_LABEL.app": "frontend"},
				{colTs: time.Unix(2, 0).UTC(), colMsg: "level=info status=200", "utf8.COLUMN_TYPE_LABEL.app": "backend"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedOutput: arrowtest.Rows{
				{
					colTs:                            time.Unix(1, 0).UTC(),
					colMsg:                           "level=error status=500",
					"utf8.COLUMN_TYPE_LABEL.app":     "frontend",
					"utf8.COLUMN_TYPE_PARSED.level":  "error",
					"utf8.COLUMN_TYPE_PARSED.status": "500",
				},
				{
					colTs:                            time.Unix(2, 0).UTC(),
					colMsg:                           "level=info status=200",
					"utf8.COLUMN_TYPE_LABEL.app":     "backend",
					"utf8.COLUMN_TYPE_PARSED.level":  "info",
					"utf8.COLUMN_TYPE_PARSED.status": "200",
				},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					colMsg:                          "level=error",
					"utf8.COLUMN_TYPE_PARSED.level": "error",
				},
				{
					colMsg:                          "status=200",
					"utf8.COLUMN_TYPE_PARSED.level": nil,
				},
				{
					colMsg:                          "level=info",
					"utf8.COLUMN_TYPE_PARSED.level": "info",
				},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					"utf8.COLUMN_TYPE_PARSED.level":       "info",
					"utf8.COLUMN_TYPE_PARSED.status":      "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                "status==value level=error",
					"utf8.COLUMN_TYPE_PARSED.level":       nil,
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 8 : unexpected '='",
				},
				{
					colMsg:                                "level=\"unclosed status=500",
					"utf8.COLUMN_TYPE_PARSED.level":       nil,
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 27 : unterminated quoted value",
				},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					colMsg:                             "level=info status=200 method=GET",
					"utf8.COLUMN_TYPE_PARSED.code":     nil,
					"utf8.COLUMN_TYPE_PARSED.duration": nil,
					"utf8.COLUMN_TYPE_PARSED.level":    "info",
					"utf8.COLUMN_TYPE_PARSED.method":   "GET",
					"utf8.COLUMN_TYPE_PARSED.status":   "200",
				},
				{
					colMsg:                             "level=warn code=304",
					"utf8.COLUMN_TYPE_PARSED.code":     "304",
					"utf8.COLUMN_TYPE_PARSED.duration": nil,
					"utf8.COLUMN_TYPE_PARSED.level":    "warn",
					"utf8.COLUMN_TYPE_PARSED.method":   nil,
					"utf8.COLUMN_TYPE_PARSED.status":   nil,
				},
				{
					colMsg:                             "level=error status=500 method=POST duration=123ms",
					"utf8.COLUMN_TYPE_PARSED.code":     nil,
					"utf8.COLUMN_TYPE_PARSED.duration": "123ms",
					"utf8.COLUMN_TYPE_PARSED.level":    "error",
					"utf8.COLUMN_TYPE_PARSED.method":   "POST",
					"utf8.COLUMN_TYPE_PARSED.status":   "500",
				},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					"utf8.COLUMN_TYPE_PARSED.level":       "info",
					"utf8.COLUMN_TYPE_PARSED.method":      "GET",
					"utf8.COLUMN_TYPE_PARSED.status":      "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                "level==error code=500",
					"utf8.COLUMN_TYPE_PARSED.level":       nil,
					"utf8.COLUMN_TYPE_PARSED.method":      nil,
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 7 : unexpected '='",
				},
				{
					colMsg:                                "msg=\"unclosed duration=100ms code=400",
					"utf8.COLUMN_TYPE_PARSED.level":       nil,
					"utf8.COLUMN_TYPE_PARSED.method":      nil,
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 38 : unterminated quoted value",
				},
				{
					colMsg:                                "level=debug method=POST",
					"utf8.COLUMN_TYPE_PARSED.level":       "debug",
					"utf8.COLUMN_TYPE_PARSED.method":      "POST",
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
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
			parseNode := &physicalpb.Parse{
				Operation:     physicalpb.PARSE_OP_LOGFMT,
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
		colTs  = "timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp"
		colMsg = "utf8.COLUMN_TYPE_BUILTIN.message"
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
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "error", "status": "500"}`},
				{colMsg: `{"level": "info", "status": "200"}`},
				{colMsg: `{"level": "debug", "status": "201"}`},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 3, // 3 columns: message, level, status
			expectedOutput: arrowtest.Rows{
				{colMsg: `{"level": "error", "status": "500"}`, "utf8.COLUMN_TYPE_PARSED.level": "error", "utf8.COLUMN_TYPE_PARSED.status": "500"},
				{colMsg: `{"level": "info", "status": "200"}`, "utf8.COLUMN_TYPE_PARSED.level": "info", "utf8.COLUMN_TYPE_PARSED.status": "200"},
				{colMsg: `{"level": "debug", "status": "201"}`, "utf8.COLUMN_TYPE_PARSED.level": "debug", "utf8.COLUMN_TYPE_PARSED.status": "201"},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.app", true),
			}, nil),
			input: arrowtest.Rows{
				{colTs: time.Unix(1, 0).UTC(), colMsg: `{"level": "error", "status": "500"}`, "utf8.COLUMN_TYPE_LABEL.app": "frontend"},
				{colTs: time.Unix(2, 0).UTC(), colMsg: `{"level": "info", "status": "200"}`, "utf8.COLUMN_TYPE_LABEL.app": "backend"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedOutput: arrowtest.Rows{
				{colTs: time.Unix(1, 0).UTC(), colMsg: `{"level": "error", "status": "500"}`, "utf8.COLUMN_TYPE_LABEL.app": "frontend", "utf8.COLUMN_TYPE_PARSED.level": "error", "utf8.COLUMN_TYPE_PARSED.status": "500"},
				{colTs: time.Unix(2, 0).UTC(), colMsg: `{"level": "info", "status": "200"}`, "utf8.COLUMN_TYPE_LABEL.app": "backend", "utf8.COLUMN_TYPE_PARSED.level": "info", "utf8.COLUMN_TYPE_PARSED.status": "200"},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "error"}`},
				{colMsg: `{"status": "200"}`},
				{colMsg: `{"level": "info"}`},
			},
			requestedKeys:  []string{"level"},
			expectedFields: 2, // 2 columns: message, level
			expectedOutput: arrowtest.Rows{
				{colMsg: `{"level": "error"}`, "utf8.COLUMN_TYPE_PARSED.level": "error"},
				{colMsg: `{"status": "200"}`, "utf8.COLUMN_TYPE_PARSED.level": nil},
				{colMsg: `{"level": "info"}`, "utf8.COLUMN_TYPE_PARSED.level": "info"},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					"utf8.COLUMN_TYPE_PARSED.level":       "info",
					"utf8.COLUMN_TYPE_PARSED.status":      "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                `{"level": "error", "status":`,
					"utf8.COLUMN_TYPE_PARSED.level":       nil,
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					semconv.ColumnIdentError.FQN():        "JSONParserErr",
					semconv.ColumnIdentErrorDetails.FQN(): "Malformed JSON error",
				},
				{
					colMsg:                                `{"level": "info", "status": 200}`,
					"utf8.COLUMN_TYPE_PARSED.level":       "info",
					"utf8.COLUMN_TYPE_PARSED.status":      "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					colMsg:                             `{"level": "info", "status": "200", "method": "GET"}`,
					"utf8.COLUMN_TYPE_PARSED.code":     nil,
					"utf8.COLUMN_TYPE_PARSED.duration": nil,
					"utf8.COLUMN_TYPE_PARSED.level":    "info",
					"utf8.COLUMN_TYPE_PARSED.method":   "GET",
					"utf8.COLUMN_TYPE_PARSED.status":   "200",
				},
				{
					colMsg:                             `{"level": "warn", "code": "304"}`,
					"utf8.COLUMN_TYPE_PARSED.code":     "304",
					"utf8.COLUMN_TYPE_PARSED.duration": nil,
					"utf8.COLUMN_TYPE_PARSED.level":    "warn",
					"utf8.COLUMN_TYPE_PARSED.method":   nil,
					"utf8.COLUMN_TYPE_PARSED.status":   nil,
				},
				{
					colMsg:                             `{"level": "error", "status": "500", "method": "POST", "duration": "123ms"}`,
					"utf8.COLUMN_TYPE_PARSED.code":     nil,
					"utf8.COLUMN_TYPE_PARSED.duration": "123ms",
					"utf8.COLUMN_TYPE_PARSED.level":    "error",
					"utf8.COLUMN_TYPE_PARSED.method":   "POST",
					"utf8.COLUMN_TYPE_PARSED.status":   "500",
				},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					"utf8.COLUMN_TYPE_PARSED.level":       "info",
					"utf8.COLUMN_TYPE_PARSED.method":      "GET",
					"utf8.COLUMN_TYPE_PARSED.status":      "200",
					"utf8.COLUMN_TYPE_PARSED.code":        nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                `{"level": "error", "code": 500}`,
					"utf8.COLUMN_TYPE_PARSED.level":       "error",
					"utf8.COLUMN_TYPE_PARSED.method":      nil,
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					"utf8.COLUMN_TYPE_PARSED.code":        "500",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					colMsg:                                `{"msg": "unclosed}`,
					"utf8.COLUMN_TYPE_PARSED.level":       nil,
					"utf8.COLUMN_TYPE_PARSED.method":      nil,
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					"utf8.COLUMN_TYPE_PARSED.code":        nil,
					semconv.ColumnIdentError.FQN():        "JSONParserErr",
					semconv.ColumnIdentErrorDetails.FQN(): "Value is string, but can't find closing '\"' symbol",
				},
				{
					colMsg:                                `{"level": "debug", "method": "POST"}`,
					"utf8.COLUMN_TYPE_PARSED.level":       "debug",
					"utf8.COLUMN_TYPE_PARSED.method":      "POST",
					"utf8.COLUMN_TYPE_PARSED.status":      nil,
					"utf8.COLUMN_TYPE_PARSED.code":        nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
		{
			name: "handle nested JSON objects with underscore flattening",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					colMsg: `{"user": {"name": "john", "details": {"age": "30", "city": "NYC"}}, "status": "active"}`,
					"utf8.COLUMN_TYPE_PARSED.app_config_debug":      nil,
					"utf8.COLUMN_TYPE_PARSED.app_version":           nil,
					"utf8.COLUMN_TYPE_PARSED.level":                 nil,
					"utf8.COLUMN_TYPE_PARSED.nested_deep_very_deep": nil,
					"utf8.COLUMN_TYPE_PARSED.status":                "active",
					"utf8.COLUMN_TYPE_PARSED.user_details_age":      "30",
					"utf8.COLUMN_TYPE_PARSED.user_details_city":     "NYC",
					"utf8.COLUMN_TYPE_PARSED.user_name":             "john",
				},
				{
					colMsg: `{"app": {"version": "1.0", "config": {"debug": "true"}}, "level": "info"}`,
					"utf8.COLUMN_TYPE_PARSED.app_config_debug":      "true",
					"utf8.COLUMN_TYPE_PARSED.app_version":           "1.0",
					"utf8.COLUMN_TYPE_PARSED.level":                 "info",
					"utf8.COLUMN_TYPE_PARSED.nested_deep_very_deep": nil,
					"utf8.COLUMN_TYPE_PARSED.status":                nil,
					"utf8.COLUMN_TYPE_PARSED.user_details_age":      nil,
					"utf8.COLUMN_TYPE_PARSED.user_details_city":     nil,
					"utf8.COLUMN_TYPE_PARSED.user_name":             nil,
				},
				{
					colMsg: `{"nested": {"deep": {"very": {"deep": "value"}}}}`,
					"utf8.COLUMN_TYPE_PARSED.app_config_debug":      nil,
					"utf8.COLUMN_TYPE_PARSED.app_version":           nil,
					"utf8.COLUMN_TYPE_PARSED.level":                 nil,
					"utf8.COLUMN_TYPE_PARSED.nested_deep_very_deep": "value",
					"utf8.COLUMN_TYPE_PARSED.status":                nil,
					"utf8.COLUMN_TYPE_PARSED.user_details_age":      nil,
					"utf8.COLUMN_TYPE_PARSED.user_details_city":     nil,
					"utf8.COLUMN_TYPE_PARSED.user_name":             nil,
				},
			},
		},
		{
			name: "handle nested JSON with specific requested keys",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					colMsg:                                       `{"user": {"name": "alice", "profile": {"email": "alice@example.com"}}, "level": "debug"}`,
					"utf8.COLUMN_TYPE_PARSED.level":              "debug",
					"utf8.COLUMN_TYPE_PARSED.user_name":          "alice",
					"utf8.COLUMN_TYPE_PARSED.user_profile_email": "alice@example.com",
				},
				{
					colMsg:                                       `{"user": {"name": "bob"}, "level": "info"}`,
					"utf8.COLUMN_TYPE_PARSED.level":              "info",
					"utf8.COLUMN_TYPE_PARSED.user_name":          "bob",
					"utf8.COLUMN_TYPE_PARSED.user_profile_email": nil,
				},
				{
					colMsg:                                       `{"level": "error", "error": {"code": "500", "message": "internal"}}`,
					"utf8.COLUMN_TYPE_PARSED.level":              "error",
					"utf8.COLUMN_TYPE_PARSED.user_name":          nil,
					"utf8.COLUMN_TYPE_PARSED.user_profile_email": nil,
				},
			},
		},
		{
			name: "accept JSON numbers as strings (v1 compatibility)",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
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
					colMsg:                            `{"status": 200, "port": 8080, "timeout": 30.5, "retries": 0}`,
					"utf8.COLUMN_TYPE_PARSED.memory":  nil,
					"utf8.COLUMN_TYPE_PARSED.pid":     nil,
					"utf8.COLUMN_TYPE_PARSED.port":    "8080",
					"utf8.COLUMN_TYPE_PARSED.score":   nil,
					"utf8.COLUMN_TYPE_PARSED.status":  "200",
					"utf8.COLUMN_TYPE_PARSED.timeout": "30.5",
					"utf8.COLUMN_TYPE_PARSED.version": nil,
				},
				{
					colMsg:                            `{"level": "info", "pid": 12345, "memory": 256.8}`,
					"utf8.COLUMN_TYPE_PARSED.memory":  "256.8",
					"utf8.COLUMN_TYPE_PARSED.pid":     "12345",
					"utf8.COLUMN_TYPE_PARSED.port":    nil,
					"utf8.COLUMN_TYPE_PARSED.score":   nil,
					"utf8.COLUMN_TYPE_PARSED.status":  nil,
					"utf8.COLUMN_TYPE_PARSED.timeout": nil,
					"utf8.COLUMN_TYPE_PARSED.version": nil,
				},
				{
					colMsg:                            `{"score": -1, "version": 2.1, "enabled": true}`,
					"utf8.COLUMN_TYPE_PARSED.memory":  nil,
					"utf8.COLUMN_TYPE_PARSED.pid":     nil,
					"utf8.COLUMN_TYPE_PARSED.port":    nil,
					"utf8.COLUMN_TYPE_PARSED.score":   "-1",
					"utf8.COLUMN_TYPE_PARSED.status":  nil,
					"utf8.COLUMN_TYPE_PARSED.timeout": nil,
					"utf8.COLUMN_TYPE_PARSED.version": "2.1",
				},
			},
		},
		{
			name: "mixed nested objects and numbers",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"request": {"url": "/api/users", "method": "GET"}, "response": {"status": 200, "time": 45.2}}`},
				{colMsg: `{"user": {"id": 123, "profile": {"age": 25}}, "active": true}`},
			},
			requestedKeys:  nil, // Extract all keys
			expectedFields: 8,   // message, active, request_method, request_url, response_status, response_time, user_id, user_profile_age
			expectedOutput: arrowtest.Rows{
				{
					colMsg:                                     `{"request": {"url": "/api/users", "method": "GET"}, "response": {"status": 200, "time": 45.2}}`,
					"utf8.COLUMN_TYPE_PARSED.active":           nil,
					"utf8.COLUMN_TYPE_PARSED.request_method":   "GET",
					"utf8.COLUMN_TYPE_PARSED.request_url":      "/api/users",
					"utf8.COLUMN_TYPE_PARSED.response_status":  "200",
					"utf8.COLUMN_TYPE_PARSED.response_time":    "45.2",
					"utf8.COLUMN_TYPE_PARSED.user_id":          nil,
					"utf8.COLUMN_TYPE_PARSED.user_profile_age": nil,
				},
				{
					colMsg:                                     `{"user": {"id": 123, "profile": {"age": 25}}, "active": true}`,
					"utf8.COLUMN_TYPE_PARSED.active":           "true",
					"utf8.COLUMN_TYPE_PARSED.request_method":   nil,
					"utf8.COLUMN_TYPE_PARSED.request_url":      nil,
					"utf8.COLUMN_TYPE_PARSED.response_status":  nil,
					"utf8.COLUMN_TYPE_PARSED.response_time":    nil,
					"utf8.COLUMN_TYPE_PARSED.user_id":          "123",
					"utf8.COLUMN_TYPE_PARSED.user_profile_age": "25",
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
			parseNode := &physicalpb.Parse{
				Operation:     physicalpb.PARSE_OP_JSON,
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
