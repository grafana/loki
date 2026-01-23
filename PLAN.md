# XML Support in Loki - Parity Implementation Plan

## Executive Summary

This plan outlines the implementation of XML log support in Loki with feature parity to JSON handling. The effort is divided into phases: analysis, architecture design, implementation, testing, and performance optimization.

## Current JSON Capabilities

Loki has comprehensive JSON support across:

1. **Ingestion & Parsing**
   - JSONParser: Parses JSON logs and extracts properties as labels
   - JSONExpressionParser: Extracts specific fields using JSONPath expressions
   - Nested object flattening with underscore separators (e.g., `pod_uuid`)
   - UTF-8 validation and escaping
   - Field detection and metadata extraction

2. **Querying & Filtering**
   - LogQL `json` filter operator
   - JSON path expressions with dot notation and bracket indexing
   - Field extraction with label creation
   - Error handling with dedicated error labels

3. **Output & Formatting**
   - JSON Lines (JSONL) output format
   - Custom JSON marshaling/unmarshaling
   - Efficient label serialization

4. **Performance**
   - Stack-allocated buffers
   - Streaming JSON parsing (jsonparser library)
   - Fast unmarshaling via jsoniter
   - New engine with columnar support

5. **Testing**
   - Comprehensive unit tests for all JSON components
   - Nested object tests
   - Unicode/UTF-8 tests
   - Benchmark tests
   - Parser error handling tests

## XML Support Target Capabilities

### Phase 1: XML Parser Implementation (Foundation)

#### 1.1 Core XML Parser
- **File**: `pkg/logql/log/xmlparser.go` (new)
- **Capabilities**:
  - Parse XML log lines and extract attributes/elements as labels
  - Support nested element flattening with underscore separators (e.g., `pod_uuid`)
  - Handle namespaces (strip or preserve)
  - UTF-8 validation and escaping
  - Attribute extraction from elements
  - Text content extraction from leaf elements
  - Handle CDATA sections
  - Proper escaping of special XML characters
- **API**:
  ```go
  type XMLParser struct { ... }
  func NewXMLParser(captureXMLPath bool) *XMLParser
  func (p *XMLParser) Process(labels model.LabelSet, line string) (model.LabelSet, string, error)
  ```

#### 1.2 XML Expression Parser
- **File**: `pkg/logql/log/xmlexpr/` (new directory)
- **Components**:
  - Main parser: `parser.go`
  - Lexer: `lexer.go`
  - YACC grammar: `xmlexpr.y`
  - Tests: `xmlexpr_test.go`
- **XPath-like syntax** supporting:
  - Element paths: `/root/child/element`
  - Attribute access: `/root/element/@attr`
  - Bracket notation: `/root/element[0]` for selecting by index
  - Wildcards: `/root/*/element`
  - Text extraction: `/root/element/text()`
  - Full XPath subset: `//element`, `element[@attr='value']`

#### 1.3 XML Unpacker
- **File**: `pkg/logql/log/parser.go` (extend existing)
- **Capabilities**:
  - Parse XML as map[string]string
  - Convert elements/attributes to labels
  - Special `_entry` key handling
  - Integration with Promtail pack stage

### Phase 2: Namespace & Advanced Handling

#### 2.1 Namespace Support
- Strip namespaces for simplicity or preserve as prefix
- Configuration option: `strip_namespaces` (default: true)
- Examples:
  - `<ns:pod>` → `pod` (stripped) or `ns_pod` (preserved)

#### 2.2 CDATA Support
- Properly handle CDATA sections as text content
- Test mixed content scenarios

#### 2.3 Entity References
- Handle XML entity references (`&lt;`, `&gt;`, `&amp;`, etc.)
- Support numeric character references (`&#123;`)

### Phase 3: Engine Integration

#### 3.1 New Engine XML Parser
- **File**: `pkg/engine/internal/executor/parse_xml.go` (new)
- **Capabilities**:
  - High-performance XML parsing for columnar execution
  - `buildXMLColumns()` - Build arrow columns from XML
  - `parseXMLLine()` - Parse individual XML lines
  - `parseElement()` - Recursive element parsing
  - Support `requestedKeys` filtering
  - Type conversion matching JSON implementation

#### 3.2 Field Detection
- **File**: `pkg/distributor/field_detection.go` (extend)
- **Capabilities**:
  - Auto-detect log level fields in XML
  - Discover generic fields in XML
  - Integration with field_detection capabilities

### Phase 4: LogQL Integration

#### 4.1 XML Filter Operator
- Add `xml` filter operator to LogQL (similar to existing `json`)
- **Syntax**: `{job="test"} | xml`
- **Behavior**: Extract all XML elements/attributes as labels

#### 4.2 XML Expression Extraction
- Add `xml_extract()` function support
- **Syntax**: `| xml_extract("xpath/expression")`
- Similar to `json` filter with field extraction

#### 4.3 LogQL Parser Updates
- **File**: `pkg/logql/logql.y` (extend grammar)
- Add XML filter to parser
- Add XML expression stage to pipeline

### Phase 5: Output & Formatting

#### 5.1 XML Output
- **File**: `pkg/logcli/output/xml.go` (new)
- Format query results as XML
- Structure: `<logs><entry timestamp="..." line="..."><labels>...</labels></entry></logs>`

#### 5.2 HTTP Query Response
- **File**: `pkg/loghttp/query.go` (extend)
- Support XML unmarshaling for responses
- Custom XML marshaling if needed

### Phase 6: Testing Strategy

#### 6.1 Unit Tests - XML Parser
- **File**: `pkg/logql/log/xmlparser_test.go` (new)
- Test cases mirroring JSON parser tests:
  - Basic element extraction
  - Nested elements flattening
  - Attribute extraction
  - Mixed content
  - CDATA sections
  - Namespace handling
  - UTF-8 and special characters
  - Duplicate field handling
  - Error cases (malformed XML)

#### 6.2 Unit Tests - XML Expression Parser
- **File**: `pkg/logql/log/xmlexpr/xmlexpr_test.go` (new)
- Test XPath-like expressions:
  - Single elements: `/root/element`
  - Nested paths: `/root/parent/child`
  - Attributes: `/root/element/@attr`
  - Indices: `/element[0]`
  - Wildcards: `/root/*/child`
  - Text extraction: `/element/text()`
  - Complex expressions: `//element[@id='123']`

#### 6.3 Unit Tests - Engine XML Parser
- **File**: `pkg/engine/internal/executor/parse_xml_test.go` (new)
- Columnar format tests
- Multi-depth nesting
- Type handling
- UTF-8 error handling
- Benchmark tests

#### 6.4 Integration Tests
- **File**: `integration/loki_test.go` (new test cases)
- XML log ingestion end-to-end
- XML querying with LogQL
- Mixed JSON and XML logs
- Label extraction accuracy
- Performance under load

#### 6.5 Benchmark Tests
- Compare XML vs JSON parsing performance
- Throughput: logs/second
- Memory allocation patterns
- Label extraction speed
- Column building speed

### Phase 7: Performance Parity

#### 7.1 Performance Targets
- XML parsing speed: ≥ 90% of JSON parsing speed
- Memory allocation: ≤ 110% of JSON allocation
- Label extraction: ≥ 90% of JSON speed
- Columnar conversion: ≥ 90% of JSON speed

#### 7.2 Optimization Areas
- Use streaming XML parser (similar to jsonparser)
- Stack-allocated buffers for path building
- Efficient XPath evaluation
- Element/attribute caching
- Namespace stripping optimization

#### 7.3 Benchmarking Tools
- Go benchmarks with `-benchmem`
- Flame graphs for profiling
- Memory allocation tracking
- Label creation overhead analysis

### Phase 8: Documentation

#### 8.1 User Documentation
- **File**: `docs/sources/send-data/promtail/stages/xml.md` (new)
- XML stage configuration
- XPath expression guide
- Examples with nested XML
- Comparison with JSON
- Best practices

#### 8.2 Developer Documentation
- **File**: `docs/sources/architecture/xml-support.md` (new)
- XML parser architecture
- XPath expression language spec
- Integration points
- Performance considerations

#### 8.3 API Documentation
- Code comments for all public functions
- Example usage in docstrings
- Error conditions documented

## Implementation Order

1. **Core Components** (High Priority)
   - XMLParser (pkg/logql/log/xmlparser.go)
   - XML expression parser (pkg/logql/log/xmlexpr/)
   - XMLParser unit tests

2. **Engine & Performance** (Medium Priority)
   - Engine XML parser (pkg/engine/internal/executor/parse_xml.go)
   - Benchmarks and performance tuning
   - Engine tests

3. **LogQL Integration** (Medium Priority)
   - LogQL xml filter operator
   - XML expression extraction
   - Parser grammar updates

4. **Advanced Features** (Lower Priority)
   - Field detection
   - Output formatting
   - Documentation

## Success Criteria

### Functional Parity
- [ ] All JSON test scenarios have equivalent XML tests
- [ ] XML filter operator works in LogQL
- [ ] XML path expressions fully functional
- [ ] Namespace handling configurable
- [ ] CDATA section support
- [ ] Entity reference handling
- [ ] Field detection works with XML

### Performance Parity
- [ ] XML parsing ≥ 90% of JSON speed
- [ ] Memory usage ≤ 110% of JSON
- [ ] Label extraction ≥ 90% of JSON speed
- [ ] No performance regression in JSON

### Testing Parity
- [ ] Test coverage matches JSON tests (≥95%)
- [ ] All edge cases covered
- [ ] Integration tests passing
- [ ] Benchmark baselines established

### Quality Targets
- [ ] All tests passing
- [ ] Linter checks passing
- [ ] Code coverage ≥ 80%
- [ ] No warnings in build

## Risks & Mitigation

| Risk | Impact | Mitigation |
|------|--------|-----------|
| XML parsing performance | High | Use streaming parser, optimize path evaluation early |
| XPath complexity | Medium | Start with subset, expand gradually, thorough testing |
| Namespace handling | Medium | Make configurable, provide default behavior |
| Type conversion differences | Medium | Mirror JSON type handling exactly |
| Memory allocation overhead | Medium | Profile early, use stack allocation |

## Dependencies

- Standard Go libraries: `encoding/xml`
- Third-party: May need lightweight XML streaming library if needed
- Internal: LogQL parser framework, label handling

## Timeline & Iterations

This is a Ralph loop task, so implementation happens iteratively:
- **Iteration 1**: Create plan and start core parser
- **Iteration 2+**: Implement components, add tests, optimize
- Continue until full parity achieved

## File Structure Summary

```
pkg/logql/log/
├── xmlparser.go (NEW)
├── xmlparser_test.go (NEW)
└── xmlexpr/ (NEW)
    ├── parser.go
    ├── lexer.go
    ├── xmlexpr.y
    └── xmlexpr_test.go

pkg/engine/internal/executor/
├── parse_xml.go (NEW)
└── parse_xml_test.go (NEW)

pkg/logcli/output/
└── xml.go (NEW)

integration/
└── xml_test.go (NEW)

docs/sources/
├── send-data/promtail/stages/
│   └── xml.md (NEW)
└── architecture/
    └── xml-support.md (NEW)
```

---

## Iteration 1 - Core XML Parser Implementation

### Completed
- [x] Created comprehensive PLAN.md based on JSON capabilities
- [x] Implemented XMLParser (pkg/logql/log/xmlparser.go)
  - Nested element flattening with underscore separators
  - Attribute extraction with element prefixes
  - Configurable namespace stripping
  - XML path tracking for label origin
- [x] Comprehensive XMLParser unit tests with 11 passing tests
  - Simple elements
  - Multiple elements
  - Nested elements
  - Element attributes
  - Numeric values
  - Duplicate handling
  - Empty elements
  - Whitespace trimming
  - CDATA sections
  - Mixed attributes and elements
  - Error handling
- [x] Implemented XMLExpr parser (pkg/logql/log/xmlexpr/parser.go)
  - XPath-like expression parser
  - Dot notation support (pod.uuid)
  - Slash separator support
  - Nested path extraction
- [x] XML expression parser tests (7 passing tests)
- [x] All tests passing (no regressions)

### Status Summary
**Iteration 1**: Core XML parsing foundation complete
- XMLParser provides feature parity with JSON parsing for basic use cases
- XML attributes properly extracted as labels
- Nested elements flattened with underscore separators
- XML path tracking implemented
- Expression parser for extracting specific fields

### Next Steps (Future Iterations)
- Integrate XML filter into LogQL parser
- Engine integration for columnar processing
- LogQL `xml` filter operator
- XML output formatting
- Field detection for XML
- Performance benchmarking and optimization
- Integration tests
- Documentation

---

**Status**: Iteration 1 - Core Implementation Complete
**Tests Passing**: 18/18 XML-specific tests + all existing log tests
**Git Commits**: 2 commits with feature implementations
