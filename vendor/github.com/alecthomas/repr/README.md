# Python's repr() for Go [![](https://godoc.org/github.com/alecthomas/repr?status.svg)](http://godoc.org/github.com/alecthomas/repr) [![Build Status](https://travis-ci.org/alecthomas/repr.png)](https://travis-ci.org/alecthomas/repr)

This package attempts to represent Go values in a form that can be used almost directly in Go source
code.

Unfortunately, some values (such as pointers to basic types) can not be represented directly in Go.
These values will be represented as `&<value>`. eg. `&23`

## Example

```go
type test struct {
  S string
  I int
  A []int
}

func main() {
  repr.Print(&test{
    S: "String",
    I: 123,
    A: []int{1, 2, 3},
  })
}
```

Outputs

```
&main.test{S: "String", I: 123, A: []int{1, 2, 3}}
```

## Why repr and not [pp](https://github.com/k0kubun/pp)?

pp is designed for printing coloured output to consoles, with (seemingly?) no way to disable this. If you don't want coloured output (eg. for use in diffs, logs, etc.) repr is for you.

## Why repr and not [go-spew](https://github.com/davecgh/go-spew)?

Repr deliberately contains much less metadata about values. It is designed to (generally) be copyable directly into source code.

Compare go-spew:

```go
(parser.expression) (len=1 cap=1) {
 (parser.alternative) (len=1 cap=1) {
  ([]interface {}) (len=1 cap=1) {
   (*parser.repitition)(0xc82000b220)({
    expression: (parser.expression) (len=2 cap=2) {
     (parser.alternative) (len=1 cap=1) {
      ([]interface {}) (len=1 cap=1) {
       (parser.str) (len=1) "a"
      }
     },
     (parser.alternative) (len=1 cap=1) {
      ([]interface {}) (len=1 cap=1) {
       (*parser.self)(0x593ef0)({
       })
      }
     }
    }
   })
  }
 }
}
```

To repr:

```go
parser.expression{
  parser.alternative{
    []interface {}{
      &parser.repitition{
        expression: parser.expression{
          parser.alternative{
            []interface {}{
              parser.str("a"),
            },
          },
          parser.alternative{
            []interface {}{
              &parser.self{              },
            },
          },
        },
      },
    },
  },
}
```
