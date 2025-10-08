package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestNewParsePipeline_logfmt(t *testing.T) {
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
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=error status=500"},
				{"message": "level=info status=200"},
				{"message": "level=debug status=201"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 3, // 3 columns: message, level, status
			expectedOutput: arrowtest.Rows{
				{"message": "level=error status=500", "level": "error", "status": "500"},
				{"message": "level=info status=200", "level": "info", "status": "200"},
				{"message": "level=debug status=201", "level": "debug", "status": "201"},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns},
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "app", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"timestamp": time.Unix(1, 0).UTC(), "message": "level=error status=500", "app": "frontend"},
				{"timestamp": time.Unix(2, 0).UTC(), "message": "level=info status=200", "app": "backend"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedOutput: arrowtest.Rows{
				{"timestamp": time.Unix(1, 0).UTC(), "message": "level=error status=500", "app": "frontend", "level": "error", "status": "500"},
				{"timestamp": time.Unix(2, 0).UTC(), "message": "level=info status=200", "app": "backend", "level": "info", "status": "200"},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=error"},
				{"message": "status=200"},
				{"message": "level=info"},
			},
			requestedKeys:  []string{"level"},
			expectedFields: 2, // 2 columns: message, level
			expectedOutput: arrowtest.Rows{
				{"message": "level=error", "level": "error"},
				{"message": "status=200", "level": nil},
				{"message": "level=info", "level": "info"},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=info status=200"},       // No errors
				{"message": "status==value level=error"},   // Double equals error on requested key
				{"message": "level=\"unclosed status=500"}, // Unclosed quote error
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: message, level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{"message": "level=info status=200", "level": "info", "status": "200", types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
				{"message": "status==value level=error", "level": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 8 : unexpected '='"},
				{"message": "level=\"unclosed status=500", "level": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 27 : unterminated quoted value"},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=info status=200 method=GET"},
				{"message": "level=warn code=304"},
				{"message": "level=error status=500 method=POST duration=123ms"},
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, code, duration, level, method, status
			expectedOutput: arrowtest.Rows{
				{"message": "level=info status=200 method=GET", "code": nil, "duration": nil, "level": "info", "method": "GET", "status": "200"},
				{"message": "level=warn code=304", "code": "304", "duration": nil, "level": "warn", "method": nil, "status": nil},
				{"message": "level=error status=500 method=POST duration=123ms", "code": nil, "duration": "123ms", "level": "error", "method": "POST", "status": "500"},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=info status=200 method=GET"},       // Valid line
				{"message": "level==error code=500"},                  // Double equals error
				{"message": "msg=\"unclosed duration=100ms code=400"}, // Unclosed quote error
				{"message": "level=debug method=POST"},                // Valid line
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, level, method, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{"message": "level=info status=200 method=GET", "level": "info", "method": "GET", "status": "200", types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
				{"message": "level==error code=500", "level": nil, "method": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 7 : unexpected '='"},
				{"message": "msg=\"unclosed duration=100ms code=400", "level": nil, "method": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 38 : unterminated quoted value"},
				{"message": "level=debug method=POST", "level": "debug", "method": "POST", "status": nil, types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
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
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"level": "error", "status": "500"}`},
				{"message": `{"level": "info", "status": "200"}`},
				{"message": `{"level": "debug", "status": "201"}`},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 3, // 3 columns: message, level, status
			expectedOutput: arrowtest.Rows{
				{"message": `{"level": "error", "status": "500"}`, "level": "error", "status": "500"},
				{"message": `{"level": "info", "status": "200"}`, "level": "info", "status": "200"},
				{"message": `{"level": "debug", "status": "201"}`, "level": "debug", "status": "201"},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns},
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "app", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"timestamp": time.Unix(1, 0).UTC(), "message": `{"level": "error", "status": "500"}`, "app": "frontend"},
				{"timestamp": time.Unix(2, 0).UTC(), "message": `{"level": "info", "status": "200"}`, "app": "backend"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedOutput: arrowtest.Rows{
				{"timestamp": time.Unix(1, 0).UTC(), "message": `{"level": "error", "status": "500"}`, "app": "frontend", "level": "error", "status": "500"},
				{"timestamp": time.Unix(2, 0).UTC(), "message": `{"level": "info", "status": "200"}`, "app": "backend", "level": "info", "status": "200"},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"level": "error"}`},
				{"message": `{"status": "200"}`},
				{"message": `{"level": "info"}`},
			},
			requestedKeys:  []string{"level"},
			expectedFields: 2, // 2 columns: message, level
			expectedOutput: arrowtest.Rows{
				{"message": `{"level": "error"}`, "level": "error"},
				{"message": `{"status": "200"}`, "level": nil},
				{"message": `{"level": "info"}`, "level": "info"},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"level": "info", "status": "200"}`}, // No errors
				{"message": `{"level": "error", "status":`},       // Missing closing brace and value
				{"message": `{"level": "info", "status": 200}`},   // Number should be converted to string
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: message, level, status, __error__, __error_details__ (due to malformed JSON)
			expectedOutput: arrowtest.Rows{
				{"message": `{"level": "info", "status": "200"}`, "level": "info", "status": "200", types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
				{"message": `{"level": "error", "status":`, "level": nil, "status": nil, types.ColumnNameParsedError: "JSONParserErr", types.ColumnNameParsedErrorDetails: "Malformed JSON error"},
				{"message": `{"level": "info", "status": 200}`, "level": "info", "status": "200", types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"level": "info", "status": "200", "method": "GET"}`},
				{"message": `{"level": "warn", "code": "304"}`},
				{"message": `{"level": "error", "status": "500", "method": "POST", "duration": "123ms"}`},
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, code, duration, level, method, status
			expectedOutput: arrowtest.Rows{
				{"message": `{"level": "info", "status": "200", "method": "GET"}`, "code": nil, "duration": nil, "level": "info", "method": "GET", "status": "200"},
				{"message": `{"level": "warn", "code": "304"}`, "code": "304", "duration": nil, "level": "warn", "method": nil, "status": nil},
				{"message": `{"level": "error", "status": "500", "method": "POST", "duration": "123ms"}`, "code": nil, "duration": "123ms", "level": "error", "method": "POST", "status": "500"},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"level": "info", "status": "200", "method": "GET"}`}, // Valid line
				{"message": `{"level": "error", "code": 500}`},                     // Also valid, adds code column
				{"message": `{"msg": "unclosed}`},                                  // Unclosed quote
				{"message": `{"level": "debug", "method": "POST"}`},                // Valid line
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 7,   // 7 columns: message, level, method, status, code, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{"message": `{"level": "info", "status": "200", "method": "GET"}`, "level": "info", "method": "GET", "status": "200", "code": nil, types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
				{"message": `{"level": "error", "code": 500}`, "level": "error", "method": nil, "status": nil, "code": "500", types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
				{"message": `{"msg": "unclosed}`, "level": nil, "method": nil, "status": nil, "code": nil, types.ColumnNameParsedError: "JSONParserErr", types.ColumnNameParsedErrorDetails: "Value is string, but can't find closing '\"' symbol"},
				{"message": `{"level": "debug", "method": "POST"}`, "level": "debug", "method": "POST", "status": nil, "code": nil, types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
			},
		},
		{
			name: "handle nested JSON objects with underscore flattening",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"user": {"name": "john", "details": {"age": "30", "city": "NYC"}}, "status": "active"}`},
				{"message": `{"app": {"version": "1.0", "config": {"debug": "true"}}, "level": "info"}`},
				{"message": `{"nested": {"deep": {"very": {"deep": "value"}}}}`},
			},
			requestedKeys:  nil, // Extract all keys including nested ones
			expectedFields: 9,   // message, app_config_debug, app_version, level, nested_deep_very_deep, status, user_details_age, user_details_city, user_name
			expectedOutput: arrowtest.Rows{
				{"message": `{"user": {"name": "john", "details": {"age": "30", "city": "NYC"}}, "status": "active"}`, "app_config_debug": nil, "app_version": nil, "level": nil, "nested_deep_very_deep": nil, "status": "active", "user_details_age": "30", "user_details_city": "NYC", "user_name": "john"},
				{"message": `{"app": {"version": "1.0", "config": {"debug": "true"}}, "level": "info"}`, "app_config_debug": "true", "app_version": "1.0", "level": "info", "nested_deep_very_deep": nil, "status": nil, "user_details_age": nil, "user_details_city": nil, "user_name": nil},
				{"message": `{"nested": {"deep": {"very": {"deep": "value"}}}}`, "app_config_debug": nil, "app_version": nil, "level": nil, "nested_deep_very_deep": "value", "status": nil, "user_details_age": nil, "user_details_city": nil, "user_name": nil},
			},
		},
		{
			name: "handle nested JSON with specific requested keys",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"user": {"name": "alice", "profile": {"email": "alice@example.com"}}, "level": "debug"}`},
				{"message": `{"user": {"name": "bob"}, "level": "info"}`},
				{"message": `{"level": "error", "error": {"code": "500", "message": "internal"}}`},
			},
			requestedKeys:  []string{"user_name", "user_profile_email", "level"},
			expectedFields: 4, // message, level, user_name, user_profile_email
			expectedOutput: arrowtest.Rows{
				{"message": `{"user": {"name": "alice", "profile": {"email": "alice@example.com"}}, "level": "debug"}`, "level": "debug", "user_name": "alice", "user_profile_email": "alice@example.com"},
				{"message": `{"user": {"name": "bob"}, "level": "info"}`, "level": "info", "user_name": "bob", "user_profile_email": nil},
				{"message": `{"level": "error", "error": {"code": "500", "message": "internal"}}`, "level": "error", "user_name": nil, "user_profile_email": nil},
			},
		},
		{
			name: "accept JSON numbers as strings (v1 compatibility)",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"status": 200, "port": 8080, "timeout": 30.5, "retries": 0}`},
				{"message": `{"level": "info", "pid": 12345, "memory": 256.8}`},
				{"message": `{"score": -1, "version": 2.1, "enabled": true}`},
			},
			requestedKeys:  []string{"status", "port", "timeout", "pid", "memory", "score", "version"},
			expectedFields: 8, // message, memory, pid, port, score, status, timeout, version
			expectedOutput: arrowtest.Rows{
				{"message": `{"status": 200, "port": 8080, "timeout": 30.5, "retries": 0}`, "memory": nil, "pid": nil, "port": "8080", "score": nil, "status": "200", "timeout": "30.5", "version": nil},
				{"message": `{"level": "info", "pid": 12345, "memory": 256.8}`, "memory": "256.8", "pid": "12345", "port": nil, "score": nil, "status": nil, "timeout": nil, "version": nil},
				{"message": `{"score": -1, "version": 2.1, "enabled": true}`, "memory": nil, "pid": nil, "port": nil, "score": "-1", "status": nil, "timeout": nil, "version": "2.1"},
			},
		},
		{
			name: "mixed nested objects and numbers",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": `{"request": {"url": "/api/users", "method": "GET"}, "response": {"status": 200, "time": 45.2}}`},
				{"message": `{"user": {"id": 123, "profile": {"age": 25}}, "active": true}`},
			},
			requestedKeys:  nil, // Extract all keys
			expectedFields: 8,   // message, active, request_method, request_url, response_status, response_time, user_id, user_profile_age
			expectedOutput: arrowtest.Rows{
				{"message": `{"request": {"url": "/api/users", "method": "GET"}, "response": {"status": 200, "time": 45.2}}`, "active": nil, "request_method": "GET", "request_url": "/api/users", "response_status": "200", "response_time": "45.2", "user_id": nil, "user_profile_age": nil},
				{"message": `{"user": {"id": 123, "profile": {"age": 25}}, "active": true}`, "active": "true", "request_method": nil, "request_url": nil, "response_status": nil, "response_time": nil, "user_id": "123", "user_profile_age": "25"},
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
