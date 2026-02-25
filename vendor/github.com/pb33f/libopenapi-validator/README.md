<p align="center">
	<img src="libopenapi-logo.png" alt="libopenapi" height="300px" width="450px"/>
</p>

# Enterprise grade OpenAPI validation tools for golang.

![Pipeline](https://github.com/pb33f/libopenapi-validator/workflows/Build/badge.svg)
[![codecov](https://codecov.io/gh/pb33f/libopenapi-validator/branch/main/graph/badge.svg?)](https://codecov.io/gh/pb33f/libopenapi-validator)
[![discord](https://img.shields.io/discord/923258363540815912)](https://discord.gg/x7VACVuEGP)
[![Docs](https://img.shields.io/badge/godoc-reference-5fafd7)](https://pkg.go.dev/github.com/pb33f/libopenapi-validator)

A validation module for [libopenapi](https://github.com/pb33f/libopenapi).

`libopenapi-validator` will validate the following elements against an OpenAPI 3+ specification

- *http.Request* - Validates the request against the OpenAPI specification
- *http.Response* - Validates the response against the OpenAPI specification
- *libopenapi.Document* - Validates the OpenAPI document against the OpenAPI specification
- *base.Schema* - Validates a schema against a JSON or YAML blob / unmarshalled object

👉👉 [Check out the full documentation](https://pb33f.io/libopenapi/validation/) 👈👈

---

## Installation

```bash
go get github.com/pb33f/libopenapi-validator
```

## Validate OpenAPI Document

```bash
go run github.com/pb33f/libopenapi-validator/cmd/validate@latest [--regexengine] [--yaml2json] <file>
```

### Options

#### --regexengine
🔍 Example: Use a custom regex engine/flag (e.g., ecmascript)
```bash
go run github.com/pb33f/libopenapi-validator/cmd/validate@latest --regexengine=ecmascript <file>
```
🔧 Supported **--regexengine** flags/values (ℹ️ Default: re2)
- none
- ignorecase
- multiline
- explicitcapture
- compiled
- singleline
- ignorepatternwhitespace
- righttoleft
- debug
- ecmascript
- re2
- unicode

#### --yaml2json
🔍 Convert YAML files to JSON before validation (ℹ️ Default: false)

[libopenapi](https://github.com/pb33f/libopenapi/blob/main/datamodel/spec_info.go#L115) passes `map[interface{}]interface{}` structures for deeply nested objects or complex mappings in the OpenAPI specification, which are not allowed in JSON.
These structures cannot be properly converted to JSON by libopenapi and cannot be validated by jsonschema, resulting in ambiguous errors.

This flag allows pre-converting from YAML to JSON to bypass this limitation of the libopenapi.

**When does this happen?**
- OpenAPI specs with deeply nested schema definitions
- Complex `allOf`, `oneOf`, or `anyOf` structures with multiple levels
- Specifications with intricate object mappings in examples or schema properties

Enabling this flag pre-converts the YAML document from YAML to JSON, ensuring a clean JSON structure before validation.

Example:
```bash
go run github.com/pb33f/libopenapi-validator/cmd/validate@latest --yaml2json <file>
```

## Documentation

- [The structure of the validator](https://pb33f.io/libopenapi/validation/#the-structure-of-the-validator)
  - [Validation errors](https://pb33f.io/libopenapi/validation/#validation-errors)
  - [Schema errors](https://pb33f.io/libopenapi/validation/#schema-errors)
  - [High-level validation](https://pb33f.io/libopenapi/validation/#high-level-validation)
- [Validating http.Request](https://pb33f.io/libopenapi/validation/#validating-httprequest)
- [Validating http.Request and http.Response](https://pb33f.io/libopenapi/validation/#validating-httprequest-and-httpresponse)
- [Validating just http.Response](https://pb33f.io/libopenapi/validation/#validating-just-httpresponse)
- [Validating HTTP Parameters](https://pb33f.io/libopenapi/validation/#validating-http-parameters)
- [Validating an OpenAPI document](https://pb33f.io/libopenapi/validation/#validating-an-openapi-document)
- [Validating Schemas](https://pb33f.io/libopenapi/validation/#validating-schemas)

[libopenapi](https://github.com/pb33f/libopenapi) and [libopenapi-validator](https://github.com/pb33f/libopenapi-validator) are
products of Princess Beef Heavy Industries, LLC
