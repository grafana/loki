# Go String To Duration (go-str2duration)

This package allows to get a time.Duration from a string. The string can be a string retorned for time.Duration or a similar string with weeks or days too!.

<a href="https://goreportcard.com/report/github.com/xhit/go-str2duration/v2"><img src="https://goreportcard.com/badge/github.com/xhit/go-str2duration" alt="Go Report Card"></a>
<a href="https://pkg.go.dev/github.com/xhit/go-str2duration/v2?tab=doc"><img src="https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white" alt="go.dev"></a>

## Download

```bash
go get github.com/xhit/go-str2duration/v2
```

## Features

Go String To Duration supports this strings conversions to duration:
- All strings returned in time.Duration String.
- A string more readable like 1w2d6h3ns (1 week 2 days 6 hours and 3 nanoseconds).
- `µs` and `us` are microsecond.

It's the same `time.ParseDuration` standard function in Go, but with days and week support.

**Note**: a day is 24 hour.

If you don't need days and weeks, use [`time.ParseDuration`](https://golang.org/pkg/time/#ParseDuration).

## Usage

```go
package main

import (
	"fmt"
	str2duration "github.com/xhit/go-str2duration/v2"
	"os"
	"time"
)

func main() {

    for i, tt := range []struct {
            dur      string
            expected time.Duration
        }{
            //This times are returned with time.Duration string
            {"1h", time.Duration(time.Hour)},
            {"1m", time.Duration(time.Minute)},
            {"1s", time.Duration(time.Second)},
            {"1ms", time.Duration(time.Millisecond)},
            {"1µs", time.Duration(time.Microsecond)},
            {"1us", time.Duration(time.Microsecond)},
            {"1ns", time.Duration(time.Nanosecond)},
            {"4.000000001s", time.Duration(4*time.Second + time.Nanosecond)},
            {"1h0m4.000000001s", time.Duration(time.Hour + 4*time.Second + time.Nanosecond)},
            {"1h1m0.01s", time.Duration(61*time.Minute + 10*time.Millisecond)},
            {"1h1m0.123456789s", time.Duration(61*time.Minute + 123456789*time.Nanosecond)},
            {"1.00002ms", time.Duration(time.Millisecond + 20*time.Nanosecond)},
            {"1.00000002s", time.Duration(time.Second + 20*time.Nanosecond)},
            {"693ns", time.Duration(693 * time.Nanosecond)},

            //This times aren't returned with time.Duration string, but are easily readable and can be parsed too!
            {"1ms1ns", time.Duration(time.Millisecond + 1*time.Nanosecond)},
            {"1s20ns", time.Duration(time.Second + 20*time.Nanosecond)},
            {"60h8ms", time.Duration(60*time.Hour + 8*time.Millisecond)},
            {"96h63s", time.Duration(96*time.Hour + 63*time.Second)},

            //And works with days and weeks!
            {"2d3s96ns", time.Duration(48*time.Hour + 3*time.Second + 96*time.Nanosecond)},
            {"1w2d3s96ns", time.Duration(168*time.Hour + 48*time.Hour + 3*time.Second + 96*time.Nanosecond)},

            {"10s1us693ns", time.Duration(10*time.Second + time.Microsecond + 693*time.Nanosecond)},

        } {
            durationFromString, err := str2duration.ParseDuration(tt.dur)
            if err != nil {
                panic(err)

            //Check if expected time is the time returned by the parser
            } else if tt.expected != durationFromString {
                 fmt.Println(fmt.Sprintf("index %d -> in: %s returned: %s\tnot equal to %s", i, tt.dur, durationFromString.String(), tt.expected.String()))
            }else{
                fmt.Println(fmt.Sprintf("index %d -> in: %s parsed succesfully", i, tt.dur))
            }
        }
}
```

Also, you can convert to string the duration using `String(t time.Duration)` function. This support weeks and days and not return the ugly decimals from golang standard `t.String()` function. Units with 0 values aren't returned. For example: `1d1ms` means 1 day 1 millisecond.