# go-json

![Go](https://github.com/goccy/go-json/workflows/Go/badge.svg)
[![GoDoc](https://godoc.org/github.com/goccy/go-json?status.svg)](https://pkg.go.dev/github.com/goccy/go-json?tab=doc)
[![codecov](https://codecov.io/gh/goccy/go-json/branch/master/graph/badge.svg)](https://codecov.io/gh/goccy/go-json)

Fast JSON encoder/decoder compatible with encoding/json for Go

<img width="400px" src="https://user-images.githubusercontent.com/209884/92572337-42b42900-f2bf-11ea-973a-c74a359553a5.png"></img>

# Features

- Drop-in replacement of `encoding/json`: values are decoded and encoded, and errors are reported, as `encoding/json` of the Go version in use does, including Go 1.27, whose `encoding/json` is built on `encoding/json/v2`
- Fast ( See [Benchmark section](https://github.com/goccy/go-json#benchmarks) )
- `MarshalOf` and `UnmarshalOf`, which take the value by its type and save the allocations `Marshal` and `Unmarshal` need for an `interface{}` argument
- Flexible customization with options
- Coloring the encoded string
- Can propagate context.Context to `MarshalJSON` or `UnmarshalJSON`
- Can dynamically filter the fields of the structure type-safely

# Installation

```
go get github.com/goccy/go-json
```

# How to use

Replace import statement from `encoding/json` to `github.com/goccy/go-json`

```
-import "encoding/json"
+import "github.com/goccy/go-json"
```

# JSON library comparison

|  name  |  encoder | decoder | compatible with `encoding/json` |
| :----: | :------: | :-----: | :-----------------------------: |
| encoding/json |  yes | yes | N/A |
| [json-iterator/go](https://github.com/json-iterator/go) | yes | yes | partial |
| [easyjson](https://github.com/mailru/easyjson) | yes | yes |  no |
| [gojay](https://github.com/francoispqt/gojay) | yes | yes |  no |
| [segmentio/encoding/json](https://github.com/segmentio/encoding/tree/master/json) | yes | yes | partial |
| [jettison](https://github.com/wI2L/jettison) | yes | no | no |
| [simdjson-go](https://github.com/minio/simdjson-go) | no | yes | no |
| [bytedance/sonic](https://github.com/bytedance/sonic) | yes | yes | partial |
| goccy/go-json | yes | yes | yes |

- `json-iterator/go` isn't compatible with `encoding/json` in many ways (e.g. https://github.com/json-iterator/go/issues/229 ), but it hasn't been supported for a long time.
- `segmentio/encoding/json` is well supported for encoders, but some are not supported for decoder APIs such as `Token` ( streaming decode )
- `bytedance/sonic` is compatible with `encoding/json` in its `ConfigStd` configuration; by default it doesn't escape HTML, sort the keys of maps or validate strings. It decodes and encodes by native code: SIMD code and, on amd64, code generated at run time. The benchmarks of this repository compare go-json with sonic in both configurations ( see below ).

## Other libraries

- [jingo](https://github.com/bet365/jingo)

I tried the benchmark but it didn't work.
Also, it seems to panic when it receives an unexpected value because there is no error handling...

- [ffjson](https://github.com/pquerna/ffjson)

Benchmarking gave very slow results.
It seems that it is assumed that the user will use the buffer pool properly.
Also, development seems to have already stopped

# Benchmarks

[![Speed relative to encoding/json](https://goccy.github.io/go-json/summary.svg)](https://goccy.github.io/go-json/)

The JSON libraries of Go are measured doing the same work on GitHub Actions, on amd64 and arm64, and the results are published at **https://goccy.github.io/go-json/**, measured again whenever go-json, the version of a library or the report changes. The page has every payload, the encode and decode of each library, the allocations, and the results without a live heap.

A comparison is fair only between libraries doing the same work, so the results are shown by category of behavior: the behavior of `encoding/json`, the behavior of `encoding/json/v2`, the behavior of `encoding/json` without HTML escaping, key sorting and string copying, and every library at its fastest. In each category, every library is configured by its options to behave as the category requires, as far as its options allow, and as fast as they allow. Every run checks the behavior of each library on small probes before it measures it, and a library which behaves differently is still shown, marked, with what differs. The result files are attested by GitHub Artifact Attestations: `gh attestation verify` tells that they were produced by the workflow of this repository.

To run the report locally:

```
$ make bench-report
```

To compare go-json with `bytedance/sonic` side by side, benchmark by benchmark:

```
$ make bench-compare-encode
$ make bench-compare-decode
```

`BENCH_LIVE_HEAP_MB=64 make bench-compare-decode` runs the decode benchmarks with 64 MB of live heap, as a real program has: the GC then runs less often, as it does in such a program.

# Fuzzing

[go-json-fuzz](https://github.com/goccy/go-json-fuzz) is the repository for fuzzing tests.
If you run the test in this repository and find a bug, please commit to corpus to go-json-fuzz and report the issue to [go-json](https://github.com/goccy/go-json/issues).

# How it works

`go-json` is very fast in both encoding and decoding compared to other libraries.
It's easier to implement by using automatic code generation for performance or by using a dedicated interface, but `go-json` dares to stick to compatibility with `encoding/json` and is the simple interface. Despite this, we are developing with the aim of being the fastest library.

Here, we explain the various speed-up techniques implemented by `go-json`.

## Basic technique

The techniques listed here are the ones used by most of the libraries listed above.

### Buffer reuse

Since the only value required for the result of `json.Marshal(interface{}) ([]byte, error)` is `[]byte`, the only value that must be allocated during encoding is the return value `[]byte` .

Also, as the number of allocations increases, the performance will be affected, so the number of allocations should be kept as low as possible when creating `[]byte`.

Therefore, there is a technique to reduce the number of times a new buffer must be allocated by reusing the buffer used for the previous encoding by using `sync.Pool`.

Finally, you allocate a buffer that is as long as the resulting buffer and copy the contents into it, you only need to allocate the buffer once in theory.

```go
type buffer struct {
    data []byte
}

var bufPool = sync.Pool{
    New: func() any {
        return &buffer{data: make([]byte, 0, 1024)}
    },
}

buf := bufPool.Get().(*buffer)
data := encode(buf.data) // reuse buf.data

newBuf := make([]byte, len(data))
copy(newBuf, buf)

buf.data = data
bufPool.Put(buf)
```

### Elimination of reflection

As you know, the reflection operation is very slow.

Therefore, using the fact that the address position where the type information is stored is fixed for each binary ( we call this `typeptr` ),
we can use the address in the type information to call a pre-built optimized process.

For example, you can get the address to the type information from `interface{}` as follows and you can use that information to call a process that does not have reflection.

To process without reflection, pass a pointer (`unsafe.Pointer`) to the value is stored.

```go

type emptyInterface struct {
    typ unsafe.Pointer
    ptr unsafe.Pointer
}

var typeToEncoder = map[uintptr]func(unsafe.Pointer)([]byte, error){}

func Marshal(v any) ([]byte, error) {
    iface := (*emptyInterface)(unsafe.Pointer(&v)
    typeptr := uintptr(iface.typ)
    if enc, exists := typeToEncoder[typeptr]; exists {
        return enc(iface.ptr)
    }
    ...
}
```

※ In reality, `typeToEncoder` can be referenced by multiple goroutines, so exclusive control is required.

## Unique speed-up technique

## Encoder

### Encode a value without copying it to the heap by `MarshalOf`

`json.Marshal` receives an `interface{}` value. A value which is not a pointer is copied to the heap when it is converted to an `interface{}` value, so `json.Marshal(v)` allocates the copy of `v` for every call, in addition to the result.

`json.MarshalOf[T]` receives the value by its type. It copies the value to a value in the heap which is reused, so the result is the only allocation.

```go
b, err := json.MarshalOf(v)
```

Which one to use depends on what is passed:

| What is passed | Recommended | Why |
|---|---|---|
| A value which is not a pointer ( a struct, an `int`, a `string`, ... ) | `json.MarshalOf(v)` | It saves the allocation of the copy: about 15% faster for a small struct. `v` can also stay on the stack of the caller, which `json.Marshal(&v)` doesn't allow. |
| A pointer or a map | either | Such a value is stored in an `interface{}` value without an allocation, so they are the same. |
| A large value which is already referred to by a pointer `p` | `json.Marshal(p)` | `json.MarshalOf(*p)` copies the whole value, which costs more as the value gets larger. |

`MarshalNoEscape`, which left the value on the stack, is deprecated: the encoder refers to the value by its address, and the address gets invalid when the stack of the goroutine is moved. It is now the same as `Marshal`.

### Let the encoder order the fields of a struct by `OptimizeFieldOrder`

`encoding/json` writes the fields of a struct in the order of the struct, and so does `go-json` by default. A JSON object doesn't define the order of its keys, so when the order doesn't matter to the reader of the JSON, the option `json.OptimizeFieldOrder()` lets the encoder order the fields as it encodes them fastest:

```go
b, err := json.MarshalWithOption(v, json.OptimizeFieldOrder())
```

- The fields of the same kind ( `int`, `uint`, `float64`, `string`, `bool` ) are put together, in the order of the first field of each kind, and are encoded without a dispatch of the VM between them ( see below ).
- A field of the struct's own type ( the next node of a list ), if there is one, is put last, so that a list of values is encoded in one frame of the VM instead of one frame for each value.

The keys are the same, only their order differs. The opcodes of a type are compiled for the option apart from the ones in the order of the struct, so the two can be used together.

### Encoding using opcode sequence

I explained that you can use `typeptr` to call a pre-built process from type information.

In other libraries, this dedicated process is processed by making it an function calling like anonymous function, but function calls are inherently slow processes and should be avoided as much as possible.

Therefore, `go-json` adopted the Instruction-based execution processing system, which is also used to implement virtual machines for programming language.

If it is the first type to encode, create the opcode ( instruction ) sequence required for encoding.
From the second time onward, use `typeptr` to get the cached pre-built opcode sequence and encode it based on it. An example of the opcode sequence is shown below.

```go
json.Marshal(struct{
    X int `json:"x"`
    Y string `json:"y"`
}{X: 1, Y: "hello"})
```

When encoding a structure like the one above, create a sequence of opcodes like this:

```
- opStructFieldHead ( `{` )
- opStructFieldInt ( `"x": 1,` )
- opStructFieldString ( `"y": "hello"` )
- opStructEnd ( `}` )
- opEnd
```

※ When processing each operation, write the letters on the right.

In addition, each opcode is managed by the following structure ( 
Pseudo code ).

```go
type opType int
const (
    opStructFieldHead opType = iota
    opStructFieldInt
    opStructFieldStirng
    opStructEnd
    opEnd
)
type opcode struct {
    op opType
    key []byte
    next *opcode
}
```

The process of encoding using the opcode sequence is roughly implemented as follows.

```go
func encode(code *opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
    for {
        switch code.op {
        case opStructFieldHead:
            b = append(b, '{')
            code = code.next
        case opStructFieldInt:
            b = append(b, code.key...)
            b = appendInt((*int)(unsafe.Pointer(uintptr(p)+code.offset)))
            code = code.next
        case opStructFieldString:
            b = append(b, code.key...)
            b = appendString((*string)(unsafe.Pointer(uintptr(p)+code.offset)))
            code = code.next
        case opStructEnd:
            b = append(b, '}')
            code = code.next
        case opEnd:
            goto END
        }
    }
END:
    return b, nil
}
```

In this way, the huge `switch-case` is used to encode by manipulating the linked list opcodes to avoid unnecessary function calls.

### Opcode sequence optimization

One of the advantages of encoding using the opcode sequence is the ease of optimization.
The opcode sequence mentioned above is actually converted into the following optimized operations and used.

```
- opStructFieldHeadInt ( `{"x": 1,` )
- opStructEndString ( `"y": "hello"}` )
- opEnd
```

It has been reduced from 5 opcodes to 3 opcodes !
Reducing the number of opcodees means reducing the number of branches with `switch-case`.
In other words, the closer the number of operations is to 1, the faster the processing can be performed.

In `go-json`, optimization to reduce the number of opcodes itself like the above and it speeds up by preparing opcodes with optimized paths.

### Change recursive call from CALL to JMP

Recursive processing is required during encoding if the type is defined recursively as follows:

```go
type T struct {
    X int
    U *U
}

type U struct {
    T *T
}

b, err := json.Marshal(&T{
    X: 1,
    U: &U{
        T: &T{
            X: 2,
        },
    },
})
fmt.Println(string(b)) // {"X":1,"U":{"T":{"X":2,"U":null}}}
```

In `go-json`, recursive processing is processed by the operation type of ` opStructFieldRecursive`.

In this operation, after acquiring the opcode sequence used for recursive processing, the function is **not** called recursively as it is, but the necessary values ​​are saved by itself and implemented by moving to the next operation.

The technique of implementing recursive processing with the `JMP` operation while avoiding the `CALL` operation is a famous technique for implementing a high-speed virtual machine.

For more details, please refer to [the article](https://engineering.mercari.com/blog/entry/1599563768-081104c850) ( but Japanese only ).

### Dispatch by typeptr without a lock

When retrieving the data cached from the type information by `typeptr`, we usually use map.
Map requires exclusive control, so use `sync.Map` for a naive implementation.

However, this is slow: as a result of profiling, `runtime.mapaccess2` accounted for a significant percentage of the execution time.

`go-json` looks up the cache in two steps, neither of which takes a lock:

1. The runtime context, which is taken from a pool for every call, remembers the opcodes of the types it encoded last. Most of the programs encode the same types again and again, so this is a load and a comparison in most cases.
2. Otherwise a hash table with open addressing is looked up by the address of the type. An entry is written once ( the value, and then the key, by the `atomic` package ), and the table is replaced by a larger one when it gets half full, so a reader never waits for a writer. A value is stored only when a type is compiled for the first time.

An earlier version used a slice which had an element for every address a type of the program can be at, found by `typelinks` of the `runtime` package through `go:linkname`. It was replaced because it depended on the internals of the runtime, used memory in proportion to the size of the program, had to fall back to a map for a large program, and made the GC scan the whole slice in every cycle.

If you want to know more, please refer to the implementation [here](https://github.com/goccy/go-json/blob/master/internal/runtime/type_cache.go)

## Decoder

### Dispatch by typeptr without a lock

Like the encoder, the decoder uses `typeptr` to call the decoder built for the type. The runtime context of a call remembers the decoders of the types it decoded last, and otherwise a hash table which is read without a lock is looked up, as the encoder does.

### Decode a value without an allocation for the argument by `UnmarshalOf`

`json.Unmarshal` receives an `interface{}` value, which makes the value escape to the heap. `json.UnmarshalOf[T]` receives the pointer by its type: it decodes into a value in the heap which is reused and copies the result to `*v`, so `v` may point to a variable on the stack of the caller, and a value without a pointer, slice or map to fill is decoded without an allocation.

```go
var v T
err := json.UnmarshalOf(data, &v)
```

The value is copied twice, so `json.Unmarshal` is faster for a large value which is in the heap anyway.

### Faster termination character inspection using NUL character

In order to decode, you have to traverse the input buffer character by position.
At that time, if you check whether the buffer has reached the end, it will be very slow.

`buf` : `[]byte` type variable. holds the string passed to the decoder
`cursor` : `int64` type variable. holds the current read position

```go
buflen := len(buf)
for ; cursor < buflen; cursor++ { // compare cursor and buflen at all times, it is so slow.
    switch buf[cursor] {
    case ' ', '\n', '\r', '\t':
    }
}
```

Therefore, by adding the `NUL` (`\000`) character to the end of the read buffer as shown below, it is possible to check the termination character at the same time as other characters.

```go
for {
    switch buf[cursor] {
    case ' ', '\n', '\r', '\t':
    case '\000':
        return nil
    }
    cursor++
}
```

`Unmarshal` copies the input once, into a buffer followed by the `NUL` character which the runtime context keeps from a call to the next, so the copy allocates nothing in most calls. The stream decoder ( `Decoder` ) reads until its buffer holds a whole value, puts the `NUL` character after it, and decodes it by the same decoders.

### Use Boundary Check Elimination

Due to the `NUL` character optimization, the Go compiler does a boundary check every time, even though `buf[cursor]` does not cause out-of-range access.

Therefore, `go-json` eliminates boundary check by fetching characters for hotspot by pointer operation. For example, the following code.

```go
func char(ptr unsafe.Pointer, offset int64) byte {
	return *(*byte)(unsafe.Pointer(uintptr(ptr) + uintptr(offset)))
}

p := (*sliceHeader)(&unsafe.Pointer(buf)).data
for {
    switch char(p, cursor) {
    case ' ', '\n', '\r', '\t':
    case '\000':
        return nil
    }
    cursor++
}
```

### Scanning strings eight bytes at a time, and by SIMD

A string is scanned a word ( eight bytes ) at a time: a few bit operations on the word tell whether one of its bytes is a quote, a backslash or a control character, and whether one is not ASCII, so a byte is looked at alone only where the string ends or has an escape. After its first 64 bytes, the rest of a long string is scanned by AVX2 on amd64.

A string with an escape is decoded in the same pass as it is scanned from its first escape on: the runs of plain bytes between the escapes are moved at once, and the escapes are validated and decoded as they are met.

### Strings copied into an arena, or referring to the input

A decoded string is a copy, as with `encoding/json`, so the input may be modified after the call. The short strings are copied into chunks of up to 16 KB shared by the strings of a runtime context, instead of an allocation for each. With the option `json.DecodeNoCopyString()`, a string without an escape refers to the input without a copy.

### Finding the field of a key by its words

The fields of a struct are in a hash table keyed by their keys folded to lower case, and a key of the input is looked up by two words: its first eight bytes and its last eight bytes, which overlap for a key shorter than 16 bytes. A key of up to 16 bytes is compared by its length and these two words only, and a key of ASCII is folded eight bytes at a time, so a field is found by a few word operations whatever the number of fields and the length of the keys. A key matches the field with the same key, or else the first field with the same key by case folding, as with `encoding/json`.

An earlier version found the field by bitmaps of the characters of the keys, `[maxKeyLength][256]int8` or `int16`. It was replaced because it worked only for structs of up to 16 fields and keys shorter than 64 bytes, and fell back to a map for the others.

### Parsing numbers in one pass

The digits of a number are accumulated as they are read and validated by the grammar of JSON. A float with a mantissa of up to 19 digits is computed from the mantissa and a power of ten: directly when both are held exactly by a float64, and else by a table of the powers of ten as 128-bit significands. The result is the one of `strconv.ParseFloat`, which is called only for the rare numbers the table doesn't decide.

### Skipping values while validating them

The value of a key which matches no field is not decoded, but it is still checked by the grammar of JSON, as `encoding/json` checks the whole input: an invalid value is a syntax error wherever it is. The skip is a state machine which calls no function in its loop, so that its state stays in the registers, and the rare cases it doesn't handle itself ( an escape, a number which is not an integer, a deep nesting ) are handled by its caller, which resumes it.

To find the end of an object or an array without decoding it, as the stream decoder does, the bytes are scanned 64 at a time: masks of the quotes, backslashes and brackets of a block are made by AVX2 on amd64, by NEON on arm64 and by words elsewhere, and a prefix XOR of the quotes tells which bytes are inside a string.

### Decoding `interface{}` values without reflection

The values of an array or an object decoded into `interface{}` are pushed to a stack of the runtime context, and a `[]interface{}` or a `map[string]interface{}` is made of their number at the end: a map filled entry by entry grows and moves its entries several times on the way. The numbers and the strings are stored into `interface{}` values from slabs, instead of an allocation for each, and the empty arrays share one empty slice.

### Sizing slices and maps by the last value

The elements of an array are decoded directly into the slice, which is allocated for the length of the array the decoder decoded last, and a map is made for the number of the entries of the last object: the values of a type often have the same size. The zero values the entries of a map are decoded into are reused from a pool.

### Others

I have done a lot of other optimizations. I will find time to write about them. If you have any questions about what's written here or other optimizations, please visit the `#go-json` channel on `gophers.slack.com` .

## Reference

Regarding the story of go-json, there are the following articles in Japanese only.

- https://speakerdeck.com/goccy/zui-su-falsejsonraiburariwoqiu-mete
- https://engineering.mercari.com/blog/entry/1599563768-081104c850/

# Looking for Sponsors

I'm looking for sponsors this library. This library is being developed as a personal project in my spare time. If you want a quick response or problem resolution when using this library in your project, please register as a [sponsor](https://github.com/sponsors/goccy). I will cooperate as much as possible. Of course, this library is developed as an MIT license, so you can use it freely for free.

# License

MIT
