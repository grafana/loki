# goxml2json [![CircleCI](https://circleci.com/gh/basgys/goxml2json.svg?style=svg)](https://circleci.com/gh/basgys/goxml2json)

Go package that converts XML to JSON

### Install

    go get -u github.com/basgys/goxml2json

### Importing

    import github.com/basgys/goxml2json

### Usage

**Code example**

```go
  package main

  import (
  	"fmt"
  	"strings"

  	xj "github.com/basgys/goxml2json"
  )

  func main() {
  	// xml is an io.Reader
  	xml := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><hello>world</hello>`)
  	json, err := xj.Convert(xml)
  	if err != nil {
  		panic("That's embarrassing...")
  	}

  	fmt.Println(json.String())
  	// {"hello": "world"}
  }

```

**Input**

```xml
  <?xml version="1.0" encoding="UTF-8"?>
  <osm version="0.6" generator="CGImap 0.0.2">
   <bounds minlat="54.0889580" minlon="12.2487570" maxlat="54.0913900" maxlon="12.2524800"/>
   <foo>bar</foo>
  </osm>
```

**Output**

```json
  {
    "osm": {
      "-version": 0.6,
      "-generator": "CGImap 0.0.2",
      "bounds": {
        "-minlat": "54.0889580",
        "-minlon": "12.2487570",
        "-maxlat": "54.0913900",
        "-maxlon": "12.2524800"
      },
      "foo": "bar"
    }
  }
```

**With type conversion**

```go
  package main

  import (
  	"fmt"
  	"strings"

  	xj "github.com/basgys/goxml2json"
  )

  func main() {
  	// xml is an io.Reader
  	xml := strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?><price>19.95</price>`)
  	json, err := xj.Convert(xml, xj.WithTypeConverter(xj.Float))
  	if err != nil {
  		panic("That's embarrassing...")
  	}

  	fmt.Println(json.String())
  	// {"price": 19.95}
  }
```

### Contributing
Feel free to contribute to this project if you want to fix/extend/improve it.

### Contributors

  - [DirectX](https://github.com/directx)
  - [powerslacker](https://github.com/powerslacker)  
  - [samuelhug](https://github.com/samuelhug)

### TODO

   * Categorise errors
   * Option to prettify the JSON output
   * Benchmark
