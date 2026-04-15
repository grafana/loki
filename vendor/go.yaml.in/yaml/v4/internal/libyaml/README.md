# internal/libyaml

This package provides low-level YAML processing functionality through a 3-stage
pipeline: Scanner → Parser → Emitter.
It implements the libyaml C library functionality in Go.

## Directory Overview

The `internal/libyaml` package implements the core YAML processing stages:

1. **Scanner** - Tokenizes YAML text into tokens
2. **Parser** - Converts tokens into events following YAML grammar rules
3. **Emitter** - Serializes events back into YAML text

## File Organization

### Main Source Files

- **scanner.go** - YAML scanner/tokenizer implementation
- **parser.go** - YAML parser (tokens → events)
- **emitter.go** - YAML emitter (events → YAML output)
- **api.go** - Public API for Parser and Emitter types
- **yaml.go** - Core types and constants (Event, Token, enums)
- **reader.go** - Input handling and encoding detection
- **writer.go** - Output handling
- **yamlprivate.go** - Internal types and helper functions

### Test Files

- **scanner_test.go** - Scanner tests
- **parser_test.go** - Parser tests
- **emitter_test.go** - Emitter tests
- **api_test.go** - API tests
- **yaml_test.go** - Utility function tests
- **reader_test.go** - Reader tests
- **writer_test.go** - Writer tests
- **yamlprivate_test.go** - Character classification tests
- **loader_test.go** - Data loader scalar resolution tests
- **yamldatatest_test.go** - YAML test data loading framework
- **yamldatatest_loader.go** - YAML test data loader with scalar type resolution (exported for reuse)

### Test Data Files (in `testdata/`)

- **scanner.yaml** - Scanner test cases
- **parser.yaml** - Parser test cases
- **emitter.yaml** - Emitter test cases
- **api.yaml** - API test cases
- **yaml.yaml** - Utility function test cases
- **reader.yaml** - Reader test cases
- **writer.yaml** - Writer test cases
- **yamlprivate.yaml** - Character classification test cases
- **loader.yaml** - Data loader scalar resolution test cases

## Processing Pipeline

### 1. Scanner (scanner.go)

The scanner converts YAML text into tokens.

**Input**: Raw YAML text (string or []byte)
**Output**: Stream of tokens

**Token types include**:
- `SCALAR_TOKEN` - Plain, quoted, or block scalar values
- `KEY_TOKEN`, `VALUE_TOKEN` - Mapping key/value indicators
- `BLOCK_MAPPING_START_TOKEN`, `FLOW_MAPPING_START_TOKEN` - Mapping delimiters
- `BLOCK_SEQUENCE_START_TOKEN`, `FLOW_SEQUENCE_START_TOKEN` - Sequence delimiters
- `ANCHOR_TOKEN`, `ALIAS_TOKEN` - Anchor definitions and references
- `TAG_TOKEN` - Type tags
- `DOCUMENT_START_TOKEN`, `DOCUMENT_END_TOKEN` - Document boundaries

**Responsibilities**:
- Character encoding detection (UTF-8, UTF-16LE, UTF-16BE)
- Line break normalization
- Indentation tracking
- Quote and escape sequence handling

### 2. Parser (parser.go)

The parser converts tokens into events following YAML grammar rules.

**Input**: Stream of tokens from Scanner
**Output**: Stream of events

**Event types include**:
- `STREAM_START_EVENT`, `STREAM_END_EVENT` - Stream boundaries
- `DOCUMENT_START_EVENT`, `DOCUMENT_END_EVENT` - Document boundaries
- `SCALAR_EVENT` - Scalar values
- `MAPPING_START_EVENT`, `MAPPING_END_EVENT` - Mapping boundaries
- `SEQUENCE_START_EVENT`, `SEQUENCE_END_EVENT` - Sequence boundaries
- `ALIAS_EVENT` - Anchor references

**Responsibilities**:
- Implementing YAML grammar and validation
- Managing document directives (%YAML, %TAG)
- Resolving anchors and aliases
- Tracking implicit vs explicit markers
- Style preservation (plain, single-quoted, double-quoted, literal, folded)

### 3. Emitter (emitter.go)

The emitter converts events back into YAML text.

**Input**: Stream of events
**Output**: YAML text

**Responsibilities**:
- Style selection (plain/quoted scalars, block/flow collections)
- Formatting control (canonical mode, indentation, line width)
- Character encoding
- Anchor and tag serialization
- Document marker generation (---, ...)

**Configuration options**:
- `Canonical` - Emit in canonical YAML form
- `Indent` - Indentation width (2-9 spaces)
- `Width` - Line width (-1 for unlimited)
- `Unicode` - Enable Unicode character output
- `LineBreak` - Line break style (LN, CR, CRLN)

## Testing Framework

### Test Architecture

The testing framework uses a data-driven approach:

1. **Test data** is stored in YAML files in the `testdata/` directory
2. **Test logic** is implemented in Go files (`*_test.go`)
3. **One-to-one pairing**: Each `testdata/foo.yaml` has a corresponding `foo_test.go`

**Benefits**:
- Easy to add new test cases without writing Go code
- Test data is human-readable and self-documenting
- Test logic is reusable across many test cases
- Test data is separated from test code for clarity
- Tests can become a common suite for multiple YAML frameworks

### Test Data Files

Each YAML file contains test cases for a specific component:

- **scanner.yaml** - Scanner/tokenization tests
  - Token sequence verification
  - Token property validation (value, style)
  - Error detection

- **parser.yaml** - Parser/event generation tests
  - Event sequence verification
  - Event property validation (anchor, tag, value, directives)
  - Error detection

- **emitter.yaml** - Emitter/serialization tests
  - Event-to-YAML conversion
  - Configuration options testing
  - Roundtrip testing (parse → emit)
  - Writer integration

- **api.yaml** - API constructor and method tests
  - Constructor validation
  - Method behavior and state changes
  - Panic conditions
  - Cleanup verification

- **yaml.yaml** - Utility function tests
  - Enum String() methods
  - Style accessor methods

- **reader.yaml** - Reader/input handling tests
  - Encoding detection (UTF-8, UTF-16LE, UTF-16BE)
  - Buffer management
  - Error handling

- **writer.yaml** - Writer/output handling tests
  - Buffer flushing
  - Output handlers (string, io.Writer)
  - Error conditions

- **yamlprivate.yaml** - Character classification tests
  - Character type predicates (isAlpha, isDigit, isHex, etc.)
  - Character conversion functions (asDigit, asHex, width)
  - Unicode handling

- **loader.yaml** - Data loader scalar resolution tests
  - Numeric type resolution (integers, floats)
  - Boolean and null value handling
  - String vs numeric type disambiguation
  - Mixed-type collections

### Test Framework Implementation

The test framework is implemented in `yamldatatest_loader.go` and `yamldatatest_test.go`:

**Core functions**:
- `LoadYAML(data []byte) (interface{}, error)` - Parses YAML using libyaml parser with scalar type resolution (exported)
- `UnmarshalStruct(target interface{}, data map[string]interface{}) error` - Populates structs (exported)
- `LoadTestCases(filename string) ([]TestCase, error)` - Loads and parses test YAML files
- `coerceScalar(value string) interface{}` - Resolves scalar strings to appropriate Go types (int, float64, bool, nil, string)

**Core types**:
- `TestCase` struct - Umbrella structure containing fields for all test types
  - Uses `interface{}` for flexible field types
  - Post-processing converts generic fields to specific types

**Post-processing**:
After loading, the framework processes test data:
- Converts `Want` (interface{}) to `WantEvents`, `WantTokens`, or `WantSpecs` based on test type
- Converts `Want` (interface{}) to `WantContains` (handles both scalar and sequence)
- Converts `Checks` to field validation specifications

### Test Types

#### Scanner Tests

**scan-tokens** - Verify token sequence

```yaml
- scan-tokens:
    name: Simple scalar
    yaml: |-
      hello
    want:
    - STREAM_START_TOKEN
    - SCALAR_TOKEN
    - STREAM_END_TOKEN
```

**scan-tokens-detailed** - Verify token properties

```yaml
- scan-tokens-detailed:
    name: Single quoted scalar
    yaml: |-
      'hello world'
    want:
    - STREAM_START_TOKEN
    - SCALAR_TOKEN:
        style: SINGLE_QUOTED_SCALAR_STYLE
        value: hello world
    - STREAM_END_TOKEN
```

**scan-error** - Verify error detection

```yaml
- scan-error:
    name: Invalid character
    yaml: "\x01"
```

#### Parser Tests

**parse-events** - Verify event sequence

```yaml
- parse-events:
    name: Simple mapping
    yaml: |
      key: value
    want:
    - STREAM_START_EVENT
    - DOCUMENT_START_EVENT
    - MAPPING_START_EVENT
    - SCALAR_EVENT
    - SCALAR_EVENT
    - MAPPING_END_EVENT
    - DOCUMENT_END_EVENT
    - STREAM_END_EVENT
```

**parse-events-detailed** - Verify event properties

```yaml
- parse-events-detailed:
    name: Anchor and alias
    yaml: |
      - &anchor value
      - *anchor
    want:
    - STREAM_START_EVENT
    - DOCUMENT_START_EVENT
    - SEQUENCE_START_EVENT
    - SCALAR_EVENT:
        anchor: anchor
        value: value
    - ALIAS_EVENT:
        anchor: anchor
    - SEQUENCE_END_EVENT
    - DOCUMENT_END_EVENT
    - STREAM_END_EVENT
```

**parse-error** - Verify error detection

```yaml
- parse-error:
    name: Error state
    yaml: |
      key: : invalid
```

#### Emitter Tests

**emit** - Emit events and verify output contains expected strings

```yaml
- emit:
    name: Simple scalar
    data:
    - STREAM_START_EVENT:
        encoding: UTF8_ENCODING
    - DOCUMENT_START_EVENT:
        implicit: true
    - SCALAR_EVENT:
        value: hello
        implicit: true
        style: PLAIN_SCALAR_STYLE
    - DOCUMENT_END_EVENT:
        implicit: true
    - STREAM_END_EVENT
    want: hello
```

**emit-config** - Emit with configuration

```yaml
- emit-config:
    name: Custom indent
    conf:
      indent: 4
    data:
    - STREAM_START_EVENT:
        encoding: UTF8_ENCODING
    - DOCUMENT_START_EVENT:
        implicit: true
    - MAPPING_START_EVENT:
        implicit: true
        style: BLOCK_MAPPING_STYLE
    # ... more events
    want: key
```

**roundtrip** - Parse → emit, verify output

```yaml
- roundtrip:
    name: Roundtrip
    yaml: |
      key: value
      list:
        - item1
        - item2
    want:
    - key
    - value
    - item1
```

**emit-writer** - Emit to io.Writer

```yaml
- emit-writer:
    name: Writer
    data:
    - STREAM_START_EVENT:
        encoding: UTF8_ENCODING
    # ... more events
    want: test
```

#### API Tests

**api-new** - Test constructors

```yaml
- api-new:
    name: New parser
    with: NewParser
    test:
    - nil: [raw-buffer, false]
    - cap: [raw-buffer, 512]
    - nil: [buffer, false]
    - cap: [buffer, 1536]
```

**api-method** - Test methods and field state

```yaml
- api-method:
    name: Parser set input string
    with: NewParser
    byte: true
    call: [SetInputString, 'key: value']
    test:
    - eq: [input, 'key: value']
    - eq: [input-pos, 0]
    - nil: [read-handler, false]
```

**api-panic** - Test methods that should panic

```yaml
- api-panic:
    name: Parser set input string twice
    with: NewParser
    byte: true
    init: [SetInputString, first]
    call: [SetInputString, second]
    want: must set the input source only once
```

**api-delete** - Test cleanup

```yaml
- api-delete:
    name: Parser delete
    with: NewParser
    byte: true
    init: [SetInputString, test]
    test:
    - len: [input, 0]
    - len: [buffer, 0]
```

**api-new-event** - Test event constructors

```yaml
- api-new-event:
    name: New stream start event
    call: [NewStreamStartEvent, UTF8_ENCODING]
    test:
    - eq: [Type, STREAM_START_EVENT]
    - eq: [encoding, UTF8_ENCODING]
```

#### Utility Tests

**enum-string** - Test String() methods of enums

```yaml
- enum-string:
    name: Scalar style plain
    enum: [ScalarStyle, PLAIN_SCALAR_STYLE]
    want: Plain
```

**style-accessor** - Test style accessor methods

```yaml
- style-accessor:
    name: Event scalar style
    test: [ScalarStyle, DOUBLE_QUOTED_SCALAR_STYLE]
```

#### Loader Tests

**scalar-resolution** - Test scalar type resolution

```yaml
- scalar-resolution:
    name: Positive integer
    yaml: "42"
    want: 42

- scalar-resolution:
    name: Negative float
    yaml: "-2.5"
    want: -2.5
```

**Resolution order**:
1. Boolean (true, false)
2. Null (null keyword only)
3. Hexadecimal integer (0x prefix)
4. Float (contains .)
5. Decimal integer
6. String (fallback)

### Common Keys in Test YAML Files

Test cases use a **type-as-key** format where the test type is the map key:

```yaml
- test-type:
    name: Test case name
    # ... other fields
```

**Common fields**:
- **name** - Test case name (title case convention)
- **yaml** - Input YAML string to test
- **want** - Expected result (format varies by test type)
  - For api-panic: string containing expected panic message substring
  - For scan-error/parse-error: boolean (defaults to true if omitted; set to false if no error expected)
  - For enum-string: string representing expected String() output
  - For other types: varies (may be sequence or scalar)
- **data** - For emitter tests: list of event specifications to emit
- **conf** - For emitter config tests: emitter configuration options
- **with** - For API tests: constructor name (NewParser, NewEmitter)
- **call** - For API tests: method call [MethodName, arg1, arg2, ...]
- **init** - For API panic tests: setup method call before main method
- **byte** - For API tests: boolean flag to convert string args to []byte
- **test** - For API tests: list of field validation checks in format `operator: [field, value]` where operator is one of: nil, cap, len, eq, gte, len-gt.
- **test** - For style-accessor tests: array of [Method, STYLE] where Method is the accessor method (e.g., ScalarStyle) and STYLE is the style constant (e.g., DOUBLE_QUOTED_SCALAR_STYLE).
- **enum** - For enum tests: array of [Type, Value] where Type is the enum type (e.g., ScalarStyle) and Value is the constant (e.g., PLAIN_SCALAR_STYLE)

**Note on scalar type resolution**: Unquoted scalar values in test data are automatically resolved to appropriate Go types (int, float64, bool, nil) by the `LoadYAML` function. Quoted scalars remain as strings.

### Running Tests

```bash
# Run all tests in the package
go test ./internal/libyaml

# Run specific test file
go test ./internal/libyaml -run TestScanner
go test ./internal/libyaml -run TestParser
go test ./internal/libyaml -run TestEmitter
go test ./internal/libyaml -run TestAPI
go test ./internal/libyaml -run TestYAML
go test ./internal/libyaml -run TestLoader

# Run specific test case (using subtest name)
go test ./internal/libyaml -run TestScanner/Block_sequence
go test ./internal/libyaml -run TestParser/Anchor_and_alias
go test ./internal/libyaml -run TestEmitter/Flow_mapping
go test ./internal/libyaml -run TestLoader/Scientific_notation_lowercase_e

# Run with verbose output
go test -v ./internal/libyaml

# Run with coverage
go test -cover ./internal/libyaml
```
