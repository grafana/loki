// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

/*
Package datetime provides specialized time type wrappers for serializing and deserializing
time values in various formats used by Azure services.

This package is part of the runtime package, and the content is intended for SDK authors.

This package extends Go's standard time.Time type with support for multiple RFC standards
and specialized date/time representations commonly encountered in Azure APIs.

# Supported Types

The package provides five main time types:

  - RFC3339 - date and time with RFC 3339 format support
  - RFC1123 - date and time with RFC 1123 format support
  - Unix - Unix timestamp (seconds since epoch)
  - PlainDate - Date-only values (YYYY-MM-DD)
  - PlainTime - Time-only values (HH:MM:SS with optional timezone)

# Implementation Notes

  - All types are built on Go's standard time.Time and can be directly cast to time.Time
  - The types implement standard marshaling interfaces for JSON and text encoding
  - RFC3339 uses case-insensitive parsing to accommodate Azure's formatting variations
  - Timezone handling respects both 'Z' notation and offset notation (e.g., +05:30)

See https://tools.ietf.org/html/rfc3339 for RFC 3339 specification details and
https://tools.ietf.org/html/rfc1123 for RFC 1123 specification details.
*/
package datetime
