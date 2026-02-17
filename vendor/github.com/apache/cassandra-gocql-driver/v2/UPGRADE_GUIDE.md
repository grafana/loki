<!--
#
# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements.  See the NOTICE file
# distributed with this work for additional information
# regarding copyright ownership.  The ASF licenses this file
# to you under the Apache License, Version 2.0 (the
# "License"); you may not use this file except in compliance
# with the License.  You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
-->

# GoCQL Major Version Upgrade Guide

This guide helps you migrate between major versions of the GoCQL driver. Each major version introduces significant changes that may require code modifications.



## Available Upgrade Paths

- [v1.x → v2.x](#upgrading-from-v1x-to-v2x)
- [Future version upgrades will be documented here as they become available]

---

## Upgrading from v1.x to v2.x

Version 2.0.0 represents a major overhaul of the GoCQL driver with significant API changes, new features, and improvements. This migration requires careful planning and testing.

**Important Prerequisites:**
- **Minimum Go version**: Go 1.19+
- **Minimum Cassandra version**: 2.1+ (recommended 4.1+ for full feature support)
- **Supported protocol versions**: 3, 4, 5 (Cassandra 2.1+, versions 1 and 2 are no longer supported)

### Table of Contents

- [Protocol Version / Cassandra Version Support](#protocol-version--cassandra-version-support)
- [Breaking Changes (build-time errors)](#breaking-changes-build-time-errors)
  - [Query and Batch Reusability Changes](#query-and-batch-reusability-changes)
  - [Module and Import Changes](#module-and-import-changes)
  - [Removed Global Functions](#removed-global-functions)
  - [Session Setter Methods Removed](#session-setter-methods-removed)
  - [Methods Moved from Query/Batch to Iter](#methods-moved-from-querybatch-to-iter)
  - [Logging System Overhaul](#logging-system-overhaul)
  - [TimeoutLimit Variable Removal](#timeoutlimit-variable-removal)
  - [Advanced API Changes](#advanced-api-changes)
    - [HostInfo Method Visibility Changes](#hostinfo-method-visibility-changes)
    - [HostSelectionPolicy Interface Changes](#hostselectionpolicy-interface-changes)
    - [ExecutableQuery Interface Deprecated](#executablequery-interface-deprecated)
- [Behavior Changes (runtime errors)](#behavior-changes-runtime-errors)
  - [PasswordAuthenticator Behavior Change](#passwordauthenticator-behavior-change)
  - [CQL to Go type mapping for inet columns changed in MapScan/SliceMap](#cql-to-go-type-mapping-for-inet-columns-changed-in-mapscanslicemap)
  - [NULL Collections Now Return nil Instead of Empty Collections in MapScan/SliceMap](#null-collections-now-return-nil-instead-of-empty-collections-in-mapscanslicemap)
- [Deprecation Notices](#deprecation-notices)
  - [Example Migrations](#example-migrations)

---

### Protocol Version / Cassandra Version Support

**Protocol versions 1 and 2 removed:**

gocql v2.x dropped support for old protocol versions. Here's the mapping of protocol versions to Cassandra versions:

| Protocol Version | Cassandra Versions | Status in v2.x |
|------------------|-------------------|----------------|
| 1 | 1.2 - 2.0 | ❌ **REMOVED** |
| 2 | 2.0 - 2.2 | ❌ **REMOVED** |
| 3 | 2.1+ | ✅ **Minimum supported** |
| 4 | 2.2+ | ✅  |
| 5 | 4.0+ | ✅ **Latest** |

```go
// OLD (v1.x) - NO LONGER SUPPORTED
cluster.ProtoVersion = 1  // ❌ Runtime error during connection
cluster.ProtoVersion = 2  // ❌ Runtime error during connection 

// NEW (v2.x) - Minimum version 3
cluster.ProtoVersion = 3  // ✅ Minimum supported (Cassandra 2.1+)
cluster.ProtoVersion = 4  // ✅ (Cassandra 2.2+)
cluster.ProtoVersion = 5  // ✅ Latest (Cassandra 4.0+)
// OR omit for auto-negotiation (recommended)
```

**Runtime error you'll see:**
```bash
gocql: unsupported protocol response version: 1
# OR
gocql: unsupported protocol response version: 2
```

**Migration:** Update your cluster configuration to use protocol version 3 or higher, or remove the explicit ProtoVersion setting to use auto-negotiation:
```go
// Option 1: Explicit version (minimum 3)
cluster.ProtoVersion = 3  // For Cassandra 2.1+

// Option 2: Auto-negotiation (recommended)
cluster := gocql.NewCluster("127.0.0.1")
// ProtoVersion will be auto-negotiated to the highest supported version
// This is recommended as it works with any Cassandra 2.1+ version
```

**Note:** Since gocql v2.x requires Cassandra 2.1+ anyway, most users should use auto-negotiation for the best compatibility unless you have a cluster with nodes that have different Cassandra versions.

---

### Breaking Changes (build-time errors)

#### Query and Batch Reusability Changes

**Query and Batch objects are now safely reusable - COMPILATION ERRORS:**

In v1.x, Query and Batch objects were mutated during execution and used sync.Pool for object pooling. In v2.x, these objects are now immutable during execution and can be safely reused without pooling.

```go
// OLD (v1.x) - Objects were mutated during execution
query := session.Query("SELECT * FROM users WHERE id = ?", userID)
iter1 := query.Iter()
// query object was mutated with execution metrics
attempts1 := query.Attempts()  // ❌ Compilation error in v2.x

// Reusing the same query was unsafe due to mutations
iter2 := query.Iter()  // ❌ Could have stale state from previous execution

// Optional manual pooling management
query.Release()  // ❌ Compilation error in v2.x - method removed
```

**Compilation errors you'll see:**
```bash
./main.go:52:12: query.Attempts undefined (type *gocql.Query has no method Attempts)
./main.go:53:11: query.Latency undefined (type *gocql.Query has no method Latency)
./main.go:54:9: query.Release undefined (type *gocql.Query has no method Release)
```

**Migration:**
```go
// NEW (v2.x) - Objects are immutable and safely reusable
query := session.Query("SELECT * FROM users WHERE id = ?", userID)

// Execute multiple times safely - no mutations
iter1 := query.Iter()
attempts1 := iter1.Attempts()  // ✅ Execution metrics on Iter
latency1 := iter1.Latency()    // ✅ Execution metrics on Iter
iter1.Close()

// Safe to reuse the same query object
iter2 := query.Iter()
attempts2 := iter2.Attempts()  // ✅ Independent execution metrics
latency2 := iter2.Latency()    // ✅ Independent execution metrics  
iter2.Close()

// No need for Release() - objects are not pooled
// query.Release()  // ❌ Remove this - no longer needed
```

**Key Changes:**

1. **No more sync.Pool**: Query and Batch objects are no longer pooled internally
2. **No more Query.Release()**: Method removed since pooling is gone
3. **Execution metrics moved to Iter**: `Attempts()`, `Latency()`, `Host()` now available on Iter objects
4. **Safe reusability**: Same Query/Batch can be executed multiple times without side effects
5. **Independent execution state**: Each `Iter()` call creates independent execution context

**Benefits:**
- **Thread-safe reuse**: Query/Batch objects can be safely shared across goroutines as long as they aren't modified by those goroutines
- **Predictable behavior**: No hidden state mutations during execution  

Batch objects have the same changes.

#### Module and Package Changes

**CRITICAL: All users must update import paths**

The module has been moved to the Apache Software Foundation with a new import path:

```go
// OLD (v1.x)
import "github.com/gocql/gocql"

// NEW (v2.x)
import "github.com/apache/cassandra-gocql-driver/v2"
```

**Compressor modules converted to packages:**

The Snappy and LZ4 compressors have been reorganized from separate modules into packages within the main driver:

```go
// OLD (v1.x) - Snappy was part of main module
cluster.Compressor = &gocql.SnappyCompressor{}

// OLD (v1.x) - LZ4 was a separate module  
import "github.com/gocql/gocql/lz4"

// NEW (v2.x) - Both are now packages within the main module
import "github.com/apache/cassandra-gocql-driver/v2/snappy"
import "github.com/apache/cassandra-gocql-driver/v2/lz4"

cluster.Compressor = &snappy.SnappyCompressor{}  // ✅ New package syntax
cluster.Compressor = &lz4.LZ4Compressor{}        // ✅ New package syntax
```

**HostPoolHostPolicy moved to hostpool package:**

The `HostPoolHostPolicy` function has been moved from the main gocql package to the hostpool package:

```go
// OLD (v1.x) - COMPILATION ERROR in v2.x
cluster.PoolConfig.HostSelectionPolicy = gocql.HostPoolHostPolicy(hostpool.New(nil))  // ❌ undefined: gocql.HostPoolHostPolicy

// NEW (v2.x) - Import from hostpool package
import "github.com/apache/cassandra-gocql-driver/v2/hostpool"
cluster.PoolConfig.HostSelectionPolicy = hostpool.HostPoolHostPolicy(hostpool.New(nil))
```

**All import path changes:**
```go
// OLD (v1.x)
import "github.com/gocql/gocql"
import "github.com/gocql/gocql/lz4"  // Was separate module

// NEW (v2.x)
import "github.com/apache/cassandra-gocql-driver/v2"
import "github.com/apache/cassandra-gocql-driver/v2/snappy"  // Now package
import "github.com/apache/cassandra-gocql-driver/v2/lz4"     // Now package
import "github.com/apache/cassandra-gocql-driver/v2/hostpool"  // For HostPoolHostPolicy
```

#### Removed Global Functions

**`NewBatch()` function removed (was deprecated):**
```go
// OLD (v1.x) - COMPILATION ERROR in v2.x
batch := gocql.NewBatch(gocql.LoggedBatch)  // ❌ undefined: gocql.NewBatch
```

**Compilation error you'll see:**
```bash
./main.go:42:10: undefined: gocql.NewBatch
```

**Migration:**
```go
// NEW (v2.x) - Use fluent API
batch := session.Batch(gocql.LoggedBatch)
```

**`MustParseConsistency()` function removed (was deprecated):**
```go
// OLD (v1.x) - COMPILATION ERROR in v2.x
cons, err := gocql.MustParseConsistency("quorum")  // ❌ undefined: gocql.MustParseConsistency
```

**Compilation error you'll see:**
```bash
./main.go:45:11: undefined: gocql.MustParseConsistency
```

**Migration:**
```go
// NEW (v2.x) - Use ParseConsistency (panics on error instead of returning unused error)
cons := gocql.ParseConsistency("quorum")  // ✅ Direct panic on invalid input
```

#### Session Setter Methods Removed

**Session setter methods removed for immutability:**
```go
// OLD (v1.x) - COMPILATION ERRORS in v2.x
session.SetTrace(tracer)       // ❌ session.SetTrace undefined
session.SetConsistency(gocql.Quorum)  // ❌ session.SetConsistency undefined  
session.SetPageSize(1000)      // ❌ session.SetPageSize undefined
session.SetPrefetch(0.25)      // ❌ session.SetPrefetch undefined
```

**Compilation errors you'll see:**
```bash
./main.go:45:9: session.SetTrace undefined (type *gocql.Session has no method SetTrace)
./main.go:46:9: session.SetConsistency undefined (type *gocql.Session has no method SetConsistency)
./main.go:47:9: session.SetPageSize undefined (type *gocql.Session has no method SetPageSize)
./main.go:48:9: session.SetPrefetch undefined (type *gocql.Session has no method SetPrefetch)
```

**Migration options:**
```go
// NEW (v2.x) - Option 1: Set defaults via ClusterConfig
cluster := gocql.NewCluster("127.0.0.1")
cluster.Consistency = gocql.Quorum
cluster.PageSize = 1000
cluster.NextPagePrefetch = 0.25
cluster.Tracer = tracer

// NEW (v2.x) - Option 2: Configure per Query/Batch
query := session.Query("SELECT ...").
    Trace(tracer).
    Consistency(gocql.Quorum).
    PageSize(1000).
    Prefetch(0.25)

batch := session.Batch(gocql.LoggedBatch).
    Trace(tracer).
    Consistency(gocql.Quorum)
```

#### Methods Moved from Query/Batch to Iter

**Execution-specific methods moved from Query/Batch objects to Iter objects:**
```go
// OLD (v1.x) - COMPILATION ERRORS in v2.x
query := session.Query("SELECT * FROM users WHERE id = ?", userID)
iter := query.Iter()
// ... process results
attempts := query.Attempts()    // ❌ query.Attempts undefined
latency := query.Latency()      // ❌ query.Latency undefined
host := query.Host()            // ❌ query.Host undefined
```

**Compilation errors you'll see:**
```bash
./main.go:52:12: query.Attempts undefined (type *gocql.Query has no method Attempts)
./main.go:53:11: query.Latency undefined (type *gocql.Query has no method Latency)
./main.go:54:8: query.Host undefined (type *gocql.Query has no method Host)
```

**Migration:**
```go
// NEW (v2.x) - Methods available on Iter
query := session.Query("SELECT * FROM users WHERE id = ?", userID)
iter := query.Iter()
// ... process results
attempts := iter.Attempts()     // ✅ Now available on Iter
latency := iter.Latency()       // ✅ Now available on Iter  
host := iter.Host()             // ✅ Now available on Iter
defer iter.Close()
```

**Why this change?** Methods moved to Iter because they represent execution-specific data that only exists after a query is executed, not properties of the query definition itself.

#### Logging System Overhaul

**Complete replacement of logging interface - COMPILATION ERRORS:**
```go
// OLD (v1.x) - StdLogger interface - NO LONGER EXISTS
type StdLogger interface {                   // ❌ Interface removed
    Print(v ...interface{})                  // ❌ Interface removed
    Printf(format string, v ...interface{})  // ❌ Interface removed  
    Println(v ...interface{})                // ❌ Interface removed
}

// Trying to use old interface causes compilation error
cluster.Logger = myOldLogger  // ❌ cannot use myOldLogger (type StdLogger) as StructuredLogger
```

**Compilation error you'll see:**
```bash
./main.go:67:16: cannot use myOldLogger (type StdLogger) as type StructuredLogger in assignment:
	StdLogger does not implement StructuredLogger (missing Debug method)
```

**Migration - NEW StructuredLogger interface:**
```go
// NEW (v2.x) - StructuredLogger interface
type StructuredLogger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)  
    Error(msg string, fields ...Field)
}
```

**Migration options:**
```go
// Option 1: Use built-in default logger
cluster.Logger = gocql.NewLogger(gocql.LogLevelInfo)

// Option 2: Use provided adapters
cluster.Logger = gocqlzap.New(zapLogger)      // For Zap
cluster.Logger = gocqlzerolog.New(zeroLogger) // For Zerolog

// Option 3: Implement StructuredLogger interface
type MyStructuredLogger struct{}
func (l MyStructuredLogger) Debug(msg string, fields ...gocql.Field) { /* ... */ }
func (l MyStructuredLogger) Info(msg string, fields ...gocql.Field)  { /* ... */ }
func (l MyStructuredLogger) Warn(msg string, fields ...gocql.Field)  { /* ... */ }
func (l MyStructuredLogger) Error(msg string, fields ...gocql.Field) { /* ... */ }

cluster.Logger = MyStructuredLogger{}
```

For comprehensive StructuredLogger documentation, see [pkg.go.dev/github.com/apache/cassandra-gocql-driver/v2#hdr-Structured_Logging](https://pkg.go.dev/github.com/apache/cassandra-gocql-driver/v2#hdr-Structured_Logging).

#### Advanced API Changes

**⚠️ This section only applies to advanced users who implement custom interfaces ⚠️**

*Most users can skip this section. These changes only affect you if you've implemented custom `HostSelectionPolicy`, `RetryPolicy`, or other advanced driver interfaces.*

##### HostInfo Method Visibility Changes

**HostInfo method visibility changes - COMPILATION ERRORS:**

Several HostInfo methods have been removed or made private:

```go
// OLD (v1.x) - COMPILATION ERRORS in v2.x
host.SetConnectAddress(addr)    // ❌ method undefined
host.SetHostID(id)              // ❌ method undefined (became private setHostID)

// Runtime behavior changes:
addr := host.ConnectAddress()   // ⚠️ No longer panics on invalid address, driver validates before creating the object
```

**Compilation errors you'll see:**
```bash
./main.go:45:9: host.SetConnectAddress undefined (type *gocql.HostInfo has no method SetConnectAddress)
./main.go:46:9: host.SetHostID undefined (type *gocql.HostInfo has no method SetHostID)
```

**Migration:**
```go
// OLD (v1.x) - Setting host connection address
host.SetConnectAddress(net.ParseIP("192.168.1.100"))

// NEW (v2.x) - Use AddressTranslator instead
cluster.AddressTranslator = gocql.AddressTranslatorFunc(func(addr net.IP, port int) (net.IP, int) {
    // Translate addresses here
    return net.ParseIP("192.168.1.100"), port
})
```

##### HostSelectionPolicy Interface Changes

**HostSelectionPolicy.Pick() method signature changed:**
```go
// OLD (v1.x) - COMPILATION ERROR in v2.x
type HostSelectionPolicy interface {
    Pick(qry ExecutableQuery) NextHost  // ❌ ExecutableQuery no longer exists
}
```

**Compilation error you'll see:**
```bash
./main.go:25:17: undefined: ExecutableQuery
```

**Migration:**
```go
// NEW (v2.x) - Use ExecutableStatement interface
type HostSelectionPolicy interface {
    Pick(stmt ExecutableStatement) NextHost  // ✅ New interface
}
```

##### ExecutableQuery Interface Deprecated

**ExecutableQuery interface deprecated and replaced:**
```go
// OLD (v1.x) - DEPRECATED in v2.x
type ExecutableQuery interface {
    // ... methods
}

// NEW (v2.x) - Replacement interfaces
type ExecutableStatement interface {
    GetRoutingKey() ([]byte, error)
    Keyspace() string
    Table() string
    IsIdempotent() bool
    GetHostID() string
    Statement() Statement
}

// implemented by Query and Batch
type Statement interface {
    Iter() *Iter
    IterContext(ctx context.Context) *Iter
    Exec() error
    ExecContext(ctx context.Context) error
}
```

**Migration for custom HostSelectionPolicy implementations:**
```go
// OLD (v1.x)
func (p *MyCustomPolicy) Pick(qry ExecutableQuery) NextHost {
    routingKey, _ := qry.GetRoutingKey()
    keyspace := qry.Keyspace()
    // Access to internal query properties...
    // ...
}

// NEW (v2.x) - Core methods available on ExecutableStatement
func (p *MyCustomPolicy) Pick(stmt ExecutableStatement) NextHost {
    routingKey, _ := stmt.GetRoutingKey()  // ✅ Same method
    keyspace := stmt.Keyspace()            // ✅ Same method
    
    // For additional properties, type cast the underlying Statement
    switch s := stmt.Statement().(type) {
    case *Query:
        // Access Query-specific properties (READ-ONLY)
        consistency := s.GetConsistency()
        pageSize := s.PageSize()
        // ... other Query methods
    case *Batch:
        // Access Batch-specific properties (READ-ONLY)
        batchType := s.Type
        consistency := s.GetConsistency()
        // ... other Batch methods
    }
    
    // ⚠️ WARNING: Only READ from the statement - do NOT modify it!
    // Modifying the statement in HostSelectionPolicy will not affect the current request execution,
    // but it WILL modify the original Query/Batch object that the user has a reference to.
    // Since Query/Batch are not thread-safe, this can cause race conditions and unexpected behavior.
    
    // ...
}
```

**Impact:** If you have implemented custom `HostSelectionPolicy`, `RetryPolicy`, or other interfaces that accept `ExecutableQuery`, you'll need to update the parameter type to `ExecutableStatement`. The available methods remain mostly the same.



#### TimeoutLimit Variable Removal

**TimeoutLimit variable removed - COMPILATION ERROR:**

The deprecated `TimeoutLimit` global variable has been removed:

```go
// OLD (v1.x) - COMPILATION ERROR in v2.x
gocql.TimeoutLimit = 5  // ❌ undefined: gocql.TimeoutLimit
```

**Compilation error you'll see:**
```bash
./main.go:45:9: undefined: gocql.TimeoutLimit
```

**Behavior after fix:**
- **v1.x default**: `TimeoutLimit = 0` meant timeouts never closed connections
- **v2.x**: Behavior will match v1.x default - timeouts never close connections
- **Impact**: Only affects users who explicitly set `TimeoutLimit > 0`

**Migration:**
```go
// OLD (v1.x) - Setting timeout limit (deprecated approach)
gocql.TimeoutLimit = 5  // Close connection after 5 timeouts

// NEW (v2.x) - No direct replacement recommended
// Remove any code that sets gocql.TimeoutLimit
```

**Recommended Approach:**
We do not recommend a specific code-level migration path for `TimeoutLimit` because the old approach was fundamentally flawed. Instead, focus on proper operational practices:

**Better Solution: Infrastructure Monitoring & Management**
- **Monitor Cassandra node health** using proper metrics (CPU, memory, disk I/O, GC pauses)
- **Shut down unhealthy nodes** rather than trying to work around them at the client level
- **Use proper alerting** on node performance metrics
- **Address root causes** of node problems rather than masking symptoms

**Why TimeoutLimit was deprecated:**
In real-world scenarios, if a Cassandra node is unhealthy enough to cause timeouts, simply closing and reopening connections won't solve the underlying problem. The node will likely continue causing latency issues and other performance problems. The correct solution is to identify and fix unhealthy nodes at the infrastructure level.

---

### Behavior Changes (runtime errors)

#### PasswordAuthenticator Behavior Change

**PasswordAuthenticator now allows any server authenticator by default - POTENTIAL SECURITY ISSUE:**

In v1.x, PasswordAuthenticator had a hardcoded list of approved authenticators and would reject connections to servers using other authenticators.

In v2.x, PasswordAuthenticator will authenticate with **any** authenticator provided by the server unless you explicitly restrict it:

```go
// v1.x behavior: Only allowed specific authenticators by default
// v2.x behavior: Allows ANY authenticator by default

// OLD (v1.x) - Automatic rejection of non-standard authenticators
cluster.Authenticator = PasswordAuthenticator{
    Username: "user",
    Password: "password",
    // Automatically rejected servers with non-standard authenticators
}

// NEW (v2.x) - To maintain v1.x security behavior:
cluster.Authenticator = PasswordAuthenticator{
    Username: "user",
    Password: "password",
    AllowedAuthenticators: []string{
        "org.apache.cassandra.auth.PasswordAuthenticator",
        // Add other allowed authenticators here
    },
}

// NEW (v2.x) - To allow any authenticator (new default behavior):
cluster.Authenticator = PasswordAuthenticator{
    Username: "user", 
    Password: "password",
    // AllowedAuthenticators: nil, // or empty slice allows any
}
```

**Security Impact:** 
- **v1.x**: Connections to servers with non-standard authenticators were automatically rejected
- **v2.x**: Same code will now successfully authenticate with any server authenticator
- **Risk**: May connect to servers with weaker or unexpected authentication mechanisms

**Migration:** If security is a concern, explicitly set `AllowedAuthenticators` to maintain the restrictive v1.x behavior.

#### CQL to Go type mapping for inet columns changed in MapScan/SliceMap

**inet columns now return `net.IP` instead of `string` in MapScan/SliceMap - RUNTIME PANIC:**

In v1.x, `MapScan()` and `SliceMap()` returned inet columns as `string` values. In v2.x, they now return `net.IP` values, causing runtime panics for existing type assertions.

```go
// v1.x: inet columns returned as string
result, _ := session.Query("SELECT inet_col FROM table").Iter().SliceMap()
ip := result[0]["inet_col"].(string)  // ✅ Worked in v1.x

// v2.x: Same code causes runtime panic
result, _ := session.Query("SELECT inet_col FROM table").Iter().SliceMap()
ip := result[0]["inet_col"].(string)  // ❌ PANIC: interface conversion: interface {} is net.IP, not string
```

**Runtime panic you'll see:**
```bash
panic: interface conversion: interface {} is net.IP, not string
```

**Migration:**
```go
// NEW (v2.x) - Use net.IP type assertion
result, _ := session.Query("SELECT inet_col FROM table").Iter().SliceMap()
ip := result[0]["inet_col"].(net.IP)  // ✅ Correct type for v2.x

// Convert to string if needed
ipString := ip.String()

// Or use type switching for compatibility during migration
switch v := result[0]["inet_col"].(type) {
case net.IP:
    ipString := v.String()  // v2.x behavior
case string:
    ipString := v          // v1.x behavior (shouldn't happen in v2.x)
}
```

**Impact:** This only affects code using `MapScan()` and `SliceMap()` with inet columns. Direct `Scan()` calls are not affected since they require explicit type specification.

#### NULL Collections Now Return nil Instead of Empty Collections in MapScan/SliceMap

**NULL collections (lists, sets, maps) now return `nil` instead of empty collections - POTENTIAL RUNTIME PANIC:**

In v1.x, `MapScan()` and `SliceMap()` returned NULL collections as empty slices/maps. In v2.x, they now return `nil` slices/maps, which can cause panics in code that assumes non-nil collections.

```go
// v1.x: NULL collections returned as empty
result, _ := session.Query("SELECT null_list_col FROM table").Iter().SliceMap()
list := result[0]["null_list_col"].([]string)
// list was []string{} (empty slice with len=0, cap=0)
fmt.Println(list == nil)     // false
fmt.Println(len(list) == 0)  // true

// v2.x: NULL collections now return nil
result, _ := session.Query("SELECT null_list_col FROM table").Iter().SliceMap()
list := result[0]["null_list_col"].([]string)
// list is []string(nil) (nil slice)
fmt.Println(list == nil)     // true
fmt.Println(len(list) == 0)  // true (len of nil slice is 0)
```

**Code that may cause runtime panics:**
```go
// ❌ BREAKS: Code checking for non-nil before processing
if myList != nil && len(myList) > 0 {  // Condition now fails for NULL collections
    processItems(myList)  // Never called for NULL collections
}

// ❌ PANICS: Operations that don't handle nil slices
copy(destination, myList)  // Panics if myList is nil
myList[0] = "value"        // Panics if myList is nil

// ❌ BREAKS: Code assuming initialized slice for direct assignment
myList[index] = newValue   // Index assignment panics on nil slice
```

**Migration - Use nil-safe patterns:**
```go
// ✅ SAFE: Check length only (works for both nil and empty slices)
if len(myList) > 0 {           // len of nil slice is 0
    processItems(myList)
}

// ✅ SAFE: Use append (handles nil slices)
myList = append(myList, item)  // append works with nil slices

// ✅ SAFE: Range over slices (safe with nil)
for _, item := range myList {  // range over nil slice is safe (no iterations)
    processItem(item)
}

// ✅ SAFE: Initialize before direct assignment
if myList == nil {
    myList = make([]string, desiredLength)
}
myList[index] = newValue

// ✅ SAFE: Use copy with proper nil check
if len(myList) > 0 {
    copy(destination, myList)
}
```

**Why this change was made:** 
- **Semantic correctness**: NULL ≠ empty. A NULL collection should be `nil`, not an empty collection
- **Memory efficiency**: `nil` slices use less memory than empty slices
- **Consistency**: Direct `Scan()` calls already behaved this way

**Impact:** This affects code using `MapScan()` and `SliceMap()` with collection columns (lists, sets, maps) that explicitly checks for `nil` or performs operations that don't handle `nil` slices/maps safely.

---

### Deprecation Notices

The following features are deprecated but still functional in v2.x. **These should not be used in new code.** Plan to migrate away from these before v3.x:

**Session Methods:**

1. **`Session.ExecuteBatch()`** → Use `Batch.Exec()` fluent API
2. **`Session.ExecuteBatchCAS()`** → Use `Batch.ExecCAS()` fluent API  
3. **`Session.MapExecuteBatchCAS()`** → Use `Batch.MapExecCAS()` fluent API
4. **`Session.NewBatch()`** → Use `Session.Batch()` fluent API

**Query Methods:**

5. **`Query.SetConsistency()`** → Use `Query.Consistency()` fluent API instead
6. **`Query.Context()`** → Pass context directly to `ExecContext()`, `IterContext()`, `ScanContext()`, `ScanCASContext()`, `MapScanContext()`, or `MapScanCASContext()`
7. **`Query.WithContext()`** → Use context methods like `ExecContext()`, `IterContext()`, `ScanContext()`, `ScanCASContext()`, `MapScanContext()`, or `MapScanCASContext()` instead

**Batch Methods:**

8. **`Batch.SetConsistency()`** → Use `Batch.Consistency()` fluent API instead
9. **`Batch.Context()`** → Pass context directly to `ExecContext()`, `ExecCASContext()`, or `MapExecCASContext()`
10. **`Batch.WithContext()`** → Use context methods like `ExecContext()`, `ExecCASContext()`, or `MapExecCASContext()` instead

**Host Filters:**

11. **`DataCentreHostFilter()`** → Use `DataCenterHostFilter()` (spelling consistency)

**Type Aliases:**

12. **`SerialConsistency`** → Use `Consistency` instead
13. **`ExecutableQuery`** → Use `Statement` for Query/Batch objects or `ExecutableStatement` in HostSelectionPolicy implementations

**Error Variables:**

14. **`ErrTooManyTimeouts`** → No longer returned by the driver (timeouts no longer close connections)
15. **`ErrQueryArgLength`** → This wasn't returned by the driver even before 2.0

**Migration Timeline:**
- v2.x: Deprecated APIs work but may emit warnings
- v3.x: Deprecated APIs will be removed

#### Example Migrations

**Batch Operations:**
```go
// DEPRECATED (still works in v2.x)
batch := session.NewBatch(gocql.LoggedBatch)
batch.Query("INSERT INTO users (id, name) VALUES (?, ?)", id, name)
err := session.ExecuteBatch(batch)

// RECOMMENDED (future-proof)
err := session.Batch(gocql.LoggedBatch).
    Query("INSERT INTO users (id, name) VALUES (?, ?)", id, name).
    Exec()
```

**Context Handling:**
```go
// DEPRECATED (still works in v2.x)
query := session.Query("SELECT * FROM users WHERE id = ?", userID)
query = query.WithContext(ctx)
iter := query.Iter()

// RECOMMENDED (future-proof) - use appropriate context method:
iter := session.Query("SELECT * FROM users WHERE id = ?", userID).IterContext(ctx)
// OR for single row scans:
var user User
err := session.Query("SELECT * FROM users WHERE id = ?", userID).ScanContext(ctx, &user.ID, &user.Name)
// OR for execution without results:
err := session.Query("INSERT INTO users (id, name) VALUES (?, ?)", userID, name).ExecContext(ctx)
```

**Consistency Setting:**
```go
// DEPRECATED (still works in v2.x)
query := session.Query("SELECT * FROM users")
query.SetConsistency(gocql.Quorum)

batch := session.Batch(gocql.LoggedBatch)
batch.SetConsistency(gocql.Quorum)

// RECOMMENDED (future-proof)
query := session.Query("SELECT * FROM users").Consistency(gocql.Quorum)
batch := session.Batch(gocql.LoggedBatch).Consistency(gocql.Quorum)
```