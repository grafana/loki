# envsubst

`envsubst` is a Go package for expanding variables in a string using `${var}` syntax.
Includes support for bash string replacement functions.

## Documentation

[Documentation can be found on GoDoc][doc].

## Supported Functions

* `${var^}`
* `${var^^}`
* `${var,}`
* `${var,,}`
* `${var:position}`
* `${var:position:length}`
* `${var#substring}`
* `${var##substring}`
* `${var%substring}`
* `${var%%substring}`
* `${var/substring/replacement}`
* `${var//substring/replacement}`
* `${var/#substring/replacement}`
* `${var/%substring/replacement}`
* `${#var}`
* `${var=default}`
* `${var:=default}`
* `${var:-default}`

## Unsupported Functions

* `${var-default}`
* `${var+default}`
* `${var:?default}`
* `${var:+default}`

  [doc]: http://godoc.org/github.com/drone/envsubst