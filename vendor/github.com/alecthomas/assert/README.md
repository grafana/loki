# Go assertion library (fork of [stretchr/testify/require](https://github.com/stretchr/testify/tree/master/require))

This is a fork of stretchr's assertion library that does two things:

1. It makes spotting differences in equality much easier. It uses [repr](https://github.com/alecthomas/repr) and
    [diffmatchpatch](https://github.com/sergi/go-diff/diffmatchpatch) to display structural differences
    in colour.
2. Aborts tests on first assertion failure (the same behaviour as `stretchr/testify/require`).

## Example

Given the following test:

```go
type Person struct {
  Name string
  Age  int
}

// Use github.com/alecthomas/assert
func TestDiff(t *testing.T) {
  expected := []*Person{{"Alec", 20}, {"Bob", 21}, {"Sally", 22}}
  actual := []*Person{{"Alex", 20}, {"Bob", 22}, {"Sally", 22}}
  assert.Equal(t, expected, actual)
}

// Use github.com/stretchr/testify/require
func TestTestifyDiff(t *testing.T) {
  expected := []*Person{{"Alec", 20}, {"Bob", 21}, {"Sally", 22}}
  actual := []*Person{{"Alex", 20}, {"Bob", 22}, {"Sally", 22}}
  require.Equal(t, expected, actual)
}
```

The following output illustrates the problems this solves. Firstly, it shows
nested structures correctly, and secondly it highlights the differences between
actual and expected text.

<img style="overflow-x: auto;" src="./_example/diff.png">
