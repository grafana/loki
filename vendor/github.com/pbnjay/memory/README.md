# memory

Package `memory` provides two methods reporting total physical system memory
accessible to the kernel, and free memory available to the running application.

This package has no external dependency besides the standard library and default operating system tools.

Documentation:
[![GoDoc](https://godoc.org/github.com/pbnjay/memory?status.svg)](https://godoc.org/github.com/pbnjay/memory)

This is useful for dynamic code to minimize thrashing and other contention, similar to the stdlib `runtime.NumCPU`
See some history of the proposal at https://github.com/golang/go/issues/21816


## Example

```go
fmt.Printf("Total system memory: %d\n", memory.TotalMemory())
fmt.Printf("Free memory: %d\n", memory.FreeMemory())
```


## Testing

Tested/working on:
 - macOS 10.12.6 (16G29), 10.15.7 (19H2)
 - Windows 10 1511 (10586.1045)
 - Linux RHEL (3.10.0-327.3.1.el7.x86_64)
 - Raspberry Pi 3 (ARMv8) on Raspbian, ODROID-C1+ (ARMv7) on Ubuntu, C.H.I.P
   (ARMv7).
 - Amazon Linux 2 aarch64 (m6a.large, 4.14.203-156.332.amzn2.aarch64)

Tested on virtual machines:
 - Windows 7 SP1 386
 - Debian stretch 386
 - NetBSD 7.1 amd64 + 386
 - OpenBSD 6.1 amd64 + 386
 - FreeBSD 11.1 amd64 + 386
 - DragonFly BSD 4.8.1 amd64

If you have access to untested systems please test and report success/bugs.
