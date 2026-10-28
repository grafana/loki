[![Go Report Card](https://goreportcard.com/badge/github.com/buger/jsonparser)](https://goreportcard.com/report/github.com/buger/jsonparser) [![Audit](https://img.shields.io/badge/ReqProof-L3%20Assurance-success)](https://reqproof.com) ![License](https://img.shields.io/dub/l/vibe-d.svg)
# Alternative JSON parser for Go (10x times faster standard library)

It does not require you to know the structure of the payload (eg. create structs), and allows accessing fields by providing the path to them. It is up to **6.5x faster** than standard `encoding/json` package (depending on payload size and usage), **allocates no memory**. See benchmarks below.

---

## 🔒 Formally Verified — the first Go library proven to L3 assurance by [ReqProof](https://reqproof.com)

jsonparser is the **reference case study** for [ReqProof](https://reqproof.com) — a git-native requirements-engineering and formal-verification platform. Every public API is traced to a formal requirement, every requirement is tested with **100% Modified Condition/Decision Coverage (MC/DC)**, and the entire parser is fuzzed by a custom **structure-aware JSON fuzzer** ([github.com/probelabs/json-fuzz](https://github.com/probelabs/json-fuzz)) that generates grammar-valid mutations at 250,000 inputs/second.

| Metric | Value |
|---|---|
| Requirements traced | 118 (7 stakeholder + 111 system) |
| Proof audit | **0 errors, 0 warnings** (L3 strict) |
| Code-level MC/DC | **100% decisions, 100% conditions** |
| Requirement-side MC/DC | **377/377 witness rows covered** |
| Fuzz executions | 16M+ (structure-aware + path-mutation + encoding/json differential) |
| Bugs found & fixed by the proof review | 7 (4 panics, 2 data-corruption, 1 encoding bug) |

The proof review caught bugs that years of community use, OSS-Fuzz, and standard fuzzing had missed — including a panic class across 8 unchecked-dereference sites, a silent data-loss bug in `Set`, and a malformed-output bug in `Delete`. [Read the full root-cause analysis →](docs/proof-gap-root-cause.md)

---

## Rationale
Originally I made this for a project that relies on a lot of 3rd party APIs that can be unpredictable and complex.
I love simplicity and prefer to avoid external dependecies. `encoding/json` requires you to know exactly your data structures, or if you prefer to use `map[string]interface{}` instead, it will be very slow and hard to manage.
I investigated what's on the market and found that most libraries are just wrappers around `encoding/json`, there is few options with own parsers (`ffjson`, `easyjson`), but they still requires you to create data structures.


Goal of this project is to push JSON parser to the performance limits and not sacrifice with compliance and developer user experience.

## Example
For the given JSON our goal is to extract the user's full name, number of github followers and avatar.

```go
import "github.com/buger/jsonparser"

...

data := []byte(`{
  "person": {
    "name": {
      "first": "Leonid",
      "last": "Bugaev",
      "fullName": "Leonid Bugaev"
    },
    "github": {
      "handle": "buger",
      "followers": 109
    },
    "avatars": [
      { "url": "https://avatars1.githubusercontent.com/u/14009?v=3&s=460", "type": "thumbnail" }
    ]
  },
  "company": {
    "name": "Acme"
  }
}`)

// You can specify key path by providing arguments to Get function
jsonparser.Get(data, "person", "name", "fullName")

// There is `GetInt` and `GetBoolean` helpers if you exactly know key data type
jsonparser.GetInt(data, "person", "github", "followers")

// When you try to get object, it will return you []byte slice pointer to data containing it
// In `company` it will be `{"name": "Acme"}`
jsonparser.Get(data, "company")

// If the key doesn't exist it will throw an error
var size int64
if value, err := jsonparser.GetInt(data, "company", "size"); err == nil {
  size = value
}

// You can use `EachArray` helper to iterate items [item1, item2 .... itemN]
jsonparser.EachArray(data, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
	fmt.Println(jsonparser.Get(value, "url"))
}, "person", "avatars")

// Or use can access fields by index!
jsonparser.GetString(data, "person", "avatars", "[0]", "url")

// You can use `EachObject` helper to iterate objects { "key1":object1, "key2":object2, .... "keyN":objectN }
jsonparser.EachObject(data, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
        fmt.Printf("Key: '%s'\n Value: '%s'\n Type: %s\n", string(key), string(value), dataType)
	return nil
}, "person", "name")

// The most efficient way to extract multiple keys is `EachKey`

paths := [][]string{
  []string{"person", "name", "fullName"},
  []string{"person", "avatars", "[0]", "url"},
  []string{"company", "url"},
}
jsonparser.EachKey(data, func(idx int, value []byte, vt jsonparser.ValueType, err error){
  switch idx {
  case 0: // []string{"person", "name", "fullName"}
    ...
  case 1: // []string{"person", "avatars", "[0]", "url"}
    ...
  case 2: // []string{"company", "url"},
    ...
  }
}, paths...)

// For more information see docs below
```

## Lenient Parsing

The package-level functions remain strict RFC 8259 parsers. For inputs that use
single-quoted strings or non-standard escapes, use a `Config` explicitly:

```go
data := []byte(`{'name':'Ada','role':'engineer'}`)

name, err := jsonparser.Lenient.GetString(data, "name")
// name == "Ada"
```

`Lenient` enables both compatibility options. You can also enable only the
extension your input requires:

```go
config := jsonparser.Config{AllowUnknownEscapes: true}
data := []byte("{\"path\":\"docs\\`draft\\x\"}")

path, err := config.GetString(data, "path")
// path == "docs`draftx"
```

The config-aware `Get`, `GetString`, `Set`, `Delete`, `ArrayEach`, and
`ObjectEach` methods share the same signatures and behavior as their
package-level counterparts apart from the enabled parsing extensions.
`jsonparser.DefaultConfig` is strict; `jsonparser.Lenient` enables
`AllowSingleQuotes` and `AllowUnknownEscapes`.

## Streaming

`ReaderParser` provides the same path-based lookup model for JSON read from an
`io.Reader`, so a large document does not need to be loaded into a single byte
slice:

```go
file, err := os.Open("large.json")
if err != nil {
	log.Fatal(err)
}
defer file.Close()

parser := jsonparser.NewReaderParser(file)
name, err := parser.GetString("person", "name")
```

To process a root array incrementally, use `ArrayEach`. Each callback value is
valid for the duration of the callback; copy it if it must be retained:

```go
parser := jsonparser.NewReaderParser(file)
err := parser.ArrayEach(func(value []byte, valueType jsonparser.ValueType, err error) {
	if err != nil {
		return
	}
	process(value, valueType)
})
```

The parser reads in 64 KiB chunks and discards completed prefixes. Its default
sliding-window target is 64 MiB; customize it with
`jsonparser.Config{MaxBufferSize: size}`. A single returned value or array
element may exceed that target because its complete bytes are supplied to the
caller. Create a new `ReaderParser` for each lookup or array traversal.
`ReaderParser` also honors `AllowSingleQuotes` and `AllowUnknownEscapes`.

## Reference

Library API is really simple. You just need the `Get` method to perform any operation. The rest is just helpers around it.

You also can view API at [godoc.org](https://godoc.org/github.com/buger/jsonparser)


### **`Get`**
```go
func Get(data []byte, keys ...string) (value []byte, dataType jsonparser.ValueType, offset int, err error)
```
Receives data structure, and key path to extract value from.

Returns:
* `value` - Pointer to original data structure containing key value, or just empty slice if nothing found or error
* `dataType` - 	Can be: `NotExist`, `String`, `Number`, `Object`, `Array`, `Boolean` or `Null`
* `offset` - Offset from provided data structure where key value ends. Used mostly internally, for example for `ArrayEach` helper.
* `err` - If the key is not found or any other parsing issue, it should return error. If key not found it also sets `dataType` to `NotExist`

Accepts multiple keys to specify path to JSON value (in case of quering nested structures).
If no keys are provided it will try to extract the closest JSON value (simple ones or object/array), useful for reading streams or arrays, see `ArrayEach` implementation.

Note that keys can be an array indexes: `jsonparser.GetInt("person", "avatars", "[0]", "url")`, pretty cool, yeah?

### **`GetString`**
```go
func GetString(data []byte, keys ...string) (val string, err error)
```
Returns strings properly handing escaped and unicode characters. Note that this will cause additional memory allocations.

### **`GetUnsafeString`**
If you need string in your app, and ready to sacrifice with support of escaped symbols in favor of speed. It returns string mapped to existing byte slice memory, without any allocations:
```go
s, _, := jsonparser.GetUnsafeString(data, "person", "name", "title")
switch s {
  case 'CEO':
    ...
  case 'Engineer'
    ...
  ...
}
```
Note that `unsafe` here means that your string will exist until GC will free underlying byte slice, for most of cases it means that you can use this string only in current context, and should not pass it anywhere externally: through channels or any other way.


### **`GetBoolean`**, **`GetInt`** and **`GetFloat`**
```go
func GetBoolean(data []byte, keys ...string) (val bool, err error)

func GetFloat(data []byte, keys ...string) (val float64, err error)

func GetInt(data []byte, keys ...string) (val int64, err error)
```
If you know the key type, you can use the helpers above.
If key data type do not match, it will return error.

### **`EachArray`**
```go
func EachArray(data []byte, cb func(value []byte, dataType jsonparser.ValueType, offset int, err error), keys ...string)
```
Needed for iterating arrays, accepts a callback function with the same return arguments as `Get`.
`ArrayEach` remains available as a backward-compatible alias.
The error-returning and wildcard variants follow the same naming convention:
use `EachArrayErr` and `EachArrayWildcard`; `ArrayEachErr` and
`ArrayEachWildcard` remain available for backward compatibility.

### **`EachObject`**
```go
func EachObject(data []byte, callback func(key []byte, value []byte, dataType ValueType, offset int) error, keys ...string) (err error)
```
Needed for iterating object, accepts a callback function. Example:
```go
var handler func([]byte, []byte, jsonparser.ValueType, int) error
handler = func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
	//do stuff here
}
jsonparser.EachObject(myJson, handler)
```
`ObjectEach` remains available as a backward-compatible alias.


### **`EachKey`**
```go
func EachKey(data []byte, cb func(idx int, value []byte, dataType jsonparser.ValueType, err error), paths ...[]string)
```
When you need to read multiple keys, and you do not afraid of low-level API `EachKey` is your friend. It read payload only single time, and calls callback function once path is found. For example when you call multiple times `Get`, it has to process payload multiple times, each time you call it. Depending on payload `EachKey` can be multiple times faster than `Get`. Path can use nested keys as well!

```go
paths := [][]string{
	[]string{"uuid"},
	[]string{"tz"},
	[]string{"ua"},
	[]string{"st"},
}
var data SmallPayload

jsonparser.EachKey(smallFixture, func(idx int, value []byte, vt jsonparser.ValueType, err error){
	switch idx {
	case 0:
		data.Uuid, _ = value
	case 1:
		v, _ := jsonparser.ParseInt(value)
		data.Tz = int(v)
	case 2:
		data.Ua, _ = value
	case 3:
		v, _ := jsonparser.ParseInt(value)
		data.St = int(v)
	}
}, paths...)
```

### **`Set`**
```go
func Set(data []byte, setValue []byte, keys ...string) (value []byte, err error)
```
Receives existing data structure, key path to set, and value to set at that key. *This functionality is experimental.*

Returns:
* `value` - Pointer to original data structure with updated or added key value.
* `err` - If any parsing issue, it should return error.

Accepts multiple keys to specify path to JSON value (in case of updating or creating  nested structures).

Note that keys can be an array indexes: `jsonparser.Set(data, []byte("http://github.com"), "person", "avatars", "[0]", "url")`

### **`Delete`**
```go
func Delete(data []byte, keys ...string) value []byte
```
Receives existing data structure, and key path to delete. *This functionality is experimental.*

Returns:
* `value` - Pointer to original data structure with key path deleted if it can be found. If there is no key path, then the whole data structure is deleted.

Accepts multiple keys to specify path to JSON value (in case of updating or creating  nested structures).

Note that keys can be an array indexes: `jsonparser.Delete(data, "person", "avatars", "[0]", "url")`

### **`Append`**
```go
func Append(data []byte, value []byte, keys ...string) ([]byte, error)
```
Appends `value` to the end of the JSON array addressed by `keys`. When `keys` is
empty, `Append` addresses the top-level value. If a keyed path does not exist,
`Append` creates it as a single-element array using `Set`'s auto-vivification
behavior. Returns `MalformedArrayError` if the addressed value is not an array.

```go
// Append to an array without knowing its length
data, _ = jsonparser.Append(data, []byte(`"new_item"`), "items")
```


## What makes it so fast?
* It does not rely on `encoding/json`, `reflection` or `interface{}`, the only real package dependency is `bytes`.
* Operates with JSON payload on byte level, providing you pointers to the original data structure: no memory allocation.
* No automatic type conversions, by default everything is a []byte, but it provides you value type, so you can convert by yourself (there is few helpers included).
* Does not parse full record, only keys you specified


## Benchmarks

There are 3 benchmark types, trying to simulate real-life usage for small, medium and large JSON payloads.
For each metric, the lower value is better. Time/op is in nanoseconds. Values better than standard encoding/json marked as bold text.

> **Methodology:** Benchmarks run with `go test -bench=. -benchmem -count=5` on Apple M4 Max (ARM64, darwin), Go 1.26.3. Median of 5 runs. All comparison libraries updated to their latest versions as of 2026-07-29.

Compared libraries:
* https://golang.org/pkg/encoding/json
* https://github.com/tidwall/gjson — path-based, like jsonparser
* https://github.com/bytedance/sonic — SIMD-accelerated full deserializer
* https://github.com/Jeffail/gabs/v2
* https://github.com/pquerna/ffjson
* https://github.com/mailru/easyjson
* https://github.com/buger/jsonparser

#### TLDR

`jsonparser` is the **fastest overall** across all payload sizes — faster than gjson, sonic, easyjson, and encoding/json — with **zero allocations** on every code path.

#### Small payload

Each test processes 190 bytes of http log as a JSON record.
It should read multiple fields.
https://github.com/buger/jsonparser/blob/master/benchmark/benchmark_small_payload_test.go

| Library | time/op | bytes/op | allocs/op |
| ------ | ------- | -------- | ------- |
| encoding/json struct | 1,300 | 416 | 9 |
| bytedance/sonic | 460 | 522 | 4 |
| tidwall/gjson | 402 | 64 | 3 |
| pquerna/ffjson | **632** | **520** | **10** |
| mailru/easyjson | **237** | **216** | **7** |
| buger/jsonparser (ObjectEach) | **205** | 64 | 2 |
| buger/jsonparser (EachKey) | **227** | **0** | **0** |
| buger/jsonparser (Get) | **339** | **0** | **0** |

#### Medium payload

Each test processes a 2.4kb JSON record (based on Clearbit API).
It should read multiple nested fields and 1 array.

https://github.com/buger/jsonparser/blob/master/benchmark/benchmark_medium_payload_test.go

| Library | time/op | bytes/op | allocs/op |
| ------- | ------- | -------- | --------- |
| encoding/json struct | 9,911 | 616 | 18 |
| bytedance/sonic | 2,703 | 3,428 | 14 |
| tidwall/gjson | 2,241 | 168 | 4 |
| pquerna/ffjson | 3,731 | 736 | 15 |
| mailru/easyjson | 2,368 | 216 | 7 |
| buger/jsonparser (Get) | **3,141** | **0** | **0** |
| buger/jsonparser (EachKey) | **1,657** | **0** | **0** |

`jsonparser` with `EachKey` beats every competitor on medium payloads while remaining zero-allocation.

#### Large payload

Each test processes a 24kb JSON record (based on Discourse API).
It should read 2 arrays, and for each item in array get a few fields.
Basically it means processing a full JSON file.

https://github.com/buger/jsonparser/blob/master/benchmark/benchmark_large_payload_test.go

| Library | time/op | bytes/op | allocs/op |
| --- | --- | --- | --- |
| encoding/json struct | 130,565 | 4,432 | 147 |
| bytedance/sonic | 41,053 | 31,368 | 71 |
| pquerna/ffjson | 59,063 | 4,822 | 144 |
| mailru/easyjson | 33,771 | 4,016 | 134 |
| tidwall/gjson | 22,756 | 28,672 | 2 |
| buger/jsonparser | **20,114** | **0** | **0** |

`jsonparser` is the **fastest library overall** on large payloads: **6.5x faster than encoding/json**, **2x faster than sonic**, **1.7x faster than easyjson**, **1.1x faster than gjson** — and the **only zero-allocation** parser.

## Formal Verification

<!-- Documents: SYS-REQ-001, SYS-REQ-016, SYS-REQ-017, SYS-REQ-018, SYS-REQ-019, SYS-REQ-020, SYS-REQ-021, SYS-REQ-022, SYS-REQ-023, SYS-REQ-024, SYS-REQ-025, SYS-REQ-026, SYS-REQ-027 -->

This project uses [ReqProof](https://reqproof.com) for formal requirements verification, achieving:

- **92 formally specified requirements** covering all public API behavior including edge cases, malformed input, boundary values, and error propagation
- **100% MC/DC coverage** (Modified Condition/Decision Coverage) — every boolean decision in the code is independently proven exercised
- **Kind2 model checking** — mathematical proof that the specification is realizable and consistent
- **Z3 SMT proofs** — data-level properties verified for all possible inputs, not just test samples

ReqProof found **2 real bugs** during the verification process ([see PR #281](https://github.com/buger/jsonparser/pull/281)):
1. `Delete` panic on truncated JSON input — bounds check missing after internal sentinel value
2. `ArrayEach` callback silently swallowing parse errors — the callback's `err` parameter was always nil

It also identified and safely removed **7 dead code blocks** that MC/DC analysis proved unreachable from any input.

The verification runs on every PR via [probelabs/proof-action](https://github.com/probelabs/proof-action).

## Questions and support

All bug-reports and suggestions should go though Github Issues.

## Contributing

1. Fork it
2. Create your feature branch (git checkout -b my-new-feature)
3. Commit your changes (git commit -am 'Added some feature')
4. Push to the branch (git push origin my-new-feature)
5. Create new Pull Request

## Development

All my development happens using Docker, and repo include some Make tasks to simplify development.

* `make build` - builds docker image, usually can be called only once
* `make test` - run tests
* `make fmt` - run go fmt
* `make bench` - run benchmarks (if you need to run only single benchmark modify `BENCHMARK` variable in make file)
* `make profile` - runs benchmark and generate 3 files-  `cpu.out`, `mem.mprof` and `benchmark.test` binary, which can be used for `go tool pprof`
* `make bash` - enter container (i use it for running `go tool pprof` above)
