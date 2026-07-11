// Package debug is a library to display debug info that control by enviroment variable DEBUG
//
// # Example
//
//	package main
//	// import the package
//	import "github.com/alibabacloud-go/debug/debug"
//
//	// init a debug method
//	var d = debug.Init("sdk")
//
//	func main() {
//		// try `go run demo.go`
//		// and `DEBUG=sdk go run demo.go`
//		d("this debug information just print when DEBUG environment variable was set")
//	}
//
// When you run application with `DEBUG=sdk go run main.go`, it will display logs. Otherwise
// it do nothing
package debug

import (
	"fmt"
	"os"
	"strings"
)

// Debug is a method that display logs, it is useful for developer to trace program running
// details when troubleshooting
type Debug func(format string, v ...interface{})

var hookGetEnv = func() string {
	return os.Getenv("DEBUG")
}

var hookPrint = func(input string) {
	fmt.Println(input)
}

// Init returns a debug method that based the enviroment variable DEBUG value
func Init(flag string) Debug {
	enable := false

	env := hookGetEnv()
	parts := strings.Split(env, ",")
	for _, part := range parts {
		if part == flag {
			enable = true
			break
		}
	}

	return func(format string, v ...interface{}) {
		if enable {
			hookPrint(fmt.Sprintf(format, v...))
		}
	}
}
