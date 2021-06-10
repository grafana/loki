# Trace Abstraction (tab)
OpenTracing and OpenCensus abstraction for tracing and logging. 

Why? Well, sometimes you want to let the consumer choose the tracing / logging implementation.

## Getting Started
### Installing the library

```
go get -u github.com/devigned/tab/...
```

If you need to install Go, follow [the official instructions](https://golang.org/dl/)

### Usage

```go
package main

import (
	"context"
	"fmt"
	
	"github.com/devigned/tab"
	_ "github.com/devigned/tab/opencensus" // use OpenCensus
	// _ "github.com/devigned/tab/opentracing" // use OpenTracing
)

func main() {
	// start a root span
	ctx, span := tab.StartSpan(context.Background(), "main")
	defer span.End() // close span when done
	
	// pass context w/ span to child func
	printHelloWorld(ctx)
}

func printHelloWorld(ctx context.Context) {
	// start new span from parent
	_, span := tab.StartSpan(ctx, "printHelloWorld")
	defer span.End() // close span when done
	
	// add attribute to span
	span.AddAttributes(tab.StringAttribute("interesting", "value"))
	fmt.Println("Hello World!")
	tab.For(ctx).Info("after println call")
}

```