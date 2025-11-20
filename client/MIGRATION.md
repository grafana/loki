# Migration Guide: From loki/v3 to loki/client

This guide helps you migrate from importing packages from `github.com/grafana/loki/v3`
to the new lightweight `github.com/grafana/loki/client` module.

## Overview

The `loki/client` module provides a minimal-dependency version of Loki packages
that external consumers like Grafana Alloy need. It avoids heavy dependencies
on OpenTelemetry Collector and Prometheus server.

## Package Mappings

### Constants

**Before:**
```go
import "github.com/grafana/loki/v3/pkg/util/constants"

fmt.Println(constants.Loki)
fmt.Println(constants.LogLevelInfo)
```

**After:**
```go
import "github.com/grafana/loki/client/util"

fmt.Println(util.Loki)
fmt.Println(util.LogLevelInfo)
```

### Push API Types

**Before:**
```go
import lokipush "github.com/grafana/loki/v3/pkg/loghttp/push"
```

**After:**
```go
import "github.com/grafana/loki/pkg/push"

// Types are the same: push.PushRequest, push.Stream, push.Entry
```

Note: The push types are in a separate module `github.com/grafana/loki/pkg/push`
which already has minimal dependencies. This is unchanged from before.

### Label Parsing

**Before:**
```go
import (
    "github.com/grafana/loki/v3/pkg/logql/syntax"
    "github.com/prometheus/prometheus/model/labels"
)

lbs, err := syntax.ParseLabels(`{job="test"}`)
// Returns labels.Labels (Prometheus type)
```

**After:**
```go
import "github.com/grafana/loki/client/logql/syntax"

lbs, err := syntax.ParseLabels(`{job="test"}`)
// Returns syntax.Labels (simplified type, no Prometheus dependency)
```

**Breaking Changes:**
- `syntax.Labels` is a simplified type, not `labels.Labels`
- Methods are similar: `Get(name)`, `Has(name)`, `String()`
- Can be converted to/from `map[string]string` if needed

### Pattern Matching

**Before:**
```go
import "github.com/grafana/loki/v3/pkg/logql/log/pattern"

matcher, err := pattern.New(`<timestamp> <level> <message>`)
```

**After:**
```go
import "github.com/grafana/loki/client/logql/pattern"

matcher, err := pattern.New(`<timestamp> <level> <message>`)
```

**No changes needed** - the API is identical.

### Logging

**Before:**
```go
import (
    "github.com/grafana/loki/v3/pkg/util/log"
    "github.com/go-kit/log"
)

logger := log.Logger // go-kit logger
```

**After:**
```go
import "github.com/grafana/loki/client/util"

// Implement the Logger interface
type MyLogger struct{}

func (l MyLogger) Debug(msg string, kv ...interface{}) { /* ... */ }
func (l MyLogger) Info(msg string, kv ...interface{})  { /* ... */ }
func (l MyLogger) Warn(msg string, kv ...interface{})   { /* ... */ }
func (l MyLogger) Error(msg string, kv ...interface{}) { /* ... */ }

util.DefaultLogger = MyLogger{}
```

**Breaking Changes:**
- No longer uses go-kit/log
- Simple interface that you can implement with any logging library
- Default is a no-op logger

### Encoding Utilities

**Before:**
```go
import "github.com/grafana/loki/v3/pkg/util/encoding"

enc := encoding.EncWith([]byte{})
enc.PutString("test")
```

**After:**
```go
import "github.com/grafana/loki/client/util"

enc := util.Encbuf{B: []byte{}}
enc.PutString("test")
```

**Breaking Changes:**
- Types are now in the `util` package directly
- API is simplified but similar

### WAL Utilities

**Before:**
```go
import "github.com/grafana/loki/v3/pkg/util/wal"

reader, closer, err := wal.NewWalReader(dir, -1)
```

**After:**
WAL utilities are **not available** in the client module to avoid Prometheus
TSDB dependencies. Options:

1. **Continue using loki/v3** for WAL utilities (if you need them)
2. **Implement your own** WAL reader
3. **Import from Prometheus directly** if acceptable:
   ```go
   import "github.com/prometheus/prometheus/tsdb/wlog"
   ```

## Step-by-Step Migration

### Step 1: Update go.mod

Add the new client module:

```bash
go get github.com/grafana/loki/client@latest
```

### Step 2: Update Imports

Replace imports one package at a time:

```go
// Old
import "github.com/grafana/loki/v3/pkg/util/constants"

// New
import "github.com/grafana/loki/client/util"
```

### Step 3: Update Code

Adapt your code to use the new types:

```go
// If you were using labels.Labels, convert to syntax.Labels
oldLabels, _ := syntax.ParseLabels(`{job="test"}`) // Prometheus type
newLabels, _ := syntax.ParseLabels(`{job="test"}`) // Client type

// Convert if needed
labelMap := make(map[string]string)
for _, l := range newLabels {
    labelMap[l.Name] = l.Value
}
```

### Step 4: Update Tests

Update tests to use the new types and interfaces.

### Step 5: Remove Old Dependencies

Once migration is complete, you can remove the old loki/v3 dependency:

```bash
go mod edit -droprequire github.com/grafana/loki/v3
go mod tidy
```

## Common Issues

### Issue: Need Full LogQL Parser

If you need full LogQL query parsing (not just labels), you may need to keep
importing from `loki/v3/pkg/logql/syntax` for now. The client module provides
a simplified label parser only.

**Workaround:** Import both modules:
```go
import (
    labelsyntax "github.com/grafana/loki/client/logql/syntax"  // For labels
    fullsyntax "github.com/grafana/loki/v3/pkg/logql/syntax"     // For full queries
)
```

### Issue: Prometheus Labels Required

If you need Prometheus `labels.Labels` type, you can convert:

```go
import (
    "github.com/grafana/loki/client/logql/syntax"
    "github.com/prometheus/prometheus/model/labels"
)

clientLabels, _ := syntax.ParseLabels(`{job="test"}`)

// Convert to Prometheus labels
promLabels := make(labels.Labels, 0, len(clientLabels))
for _, l := range clientLabels {
    promLabels = append(promLabels, labels.Label{
        Name:  l.Name,
        Value: l.Value,
    })
}
```

### Issue: WAL Reading Needed

If you need WAL reading functionality, you have these options:

1. Keep importing from `loki/v3/pkg/util/wal` (brings Prometheus dependency)
2. Import directly from Prometheus: `github.com/prometheus/prometheus/tsdb/wlog`
3. Implement your own WAL reader

## Benefits After Migration

- ✅ **No OpenTelemetry Collector dependencies** - can upgrade OTel independently
- ✅ **No Prometheus server dependencies** - can upgrade Prometheus independently  
- ✅ **Smaller go.mod** - fewer transitive dependencies
- ✅ **Faster builds** - less code to compile
- ✅ **Independent versioning** - client module versions independently

## Getting Help

If you encounter issues during migration:

1. Check this guide for common solutions
2. Review the [README.md](./README.md) for API documentation
3. Open an issue in the [Loki repository](https://github.com/grafana/loki/issues)

## Backward Compatibility

The main `loki/v3` module continues to work as before. You can migrate gradually,
importing from both modules if needed during the transition period.
