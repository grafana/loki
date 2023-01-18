[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go)](https://pkg.go.dev/github.com/felixge/fgprof)
![GitHub Workflow Status](https://img.shields.io/github/workflow/status/felixge/fgprof/Go)
![GitHub](https://img.shields.io/github/license/felixge/fgprof)

# :rocket: fgprof - The Full Go Profiler

fgprof is a sampling [Go](https://golang.org/) profiler that allows you to analyze On-CPU as well as [Off-CPU](http://www.brendangregg.com/offcpuanalysis.html) (e.g. I/O) time together.

Go's builtin sampling CPU profiler can only show On-CPU time, but it's better than fgprof at that. Go also includes tracing profilers that can analyze I/O, but they can't be combined with the CPU profiler.

fgprof is designed for analyzing applications with mixed I/O and CPU workloads. This kind of profiling is also known as wall-clock profiling.

## Quick Start

If this is the first time you hear about fgprof, you should start by reading about [The Problem](#the-problem) & [How it Works](#how-it-works).

There is no need to choose between fgprof and the builtin profiler. Here is how to add both to your application:

```go
package main

import(
	_ "net/http/pprof"
	"github.com/felixge/fgprof"
)

func main() {
	http.DefaultServeMux.Handle("/debug/fgprof", fgprof.Handler())
	go func() {
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	// <code to profile>
}
```

fgprof is compatible with the `go tool pprof` visualizer, so taking and analyzing a 3s profile is as simple as:

```
go tool pprof --http=:6061 http://localhost:6060/debug/fgprof?seconds=3
```

![](./assets/fgprof_pprof.png)

Additionally fgprof supports the plain text format used by Brendan Gregg's [FlameGraph](http://www.brendangregg.com/flamegraphs.html) utility:

```
git clone https://github.com/brendangregg/FlameGraph
cd FlameGraph
curl -s 'localhost:6060/debug/fgprof?seconds=3&format=folded' > fgprof.folded
./flamegraph.pl fgprof.folded > fgprof.svg
```

![](./assets/fgprof_gregg.png)

Which tool you prefer is up to you, but one thing I like about Gregg's tool is that you can filter the plaintext files using grep which can be very useful when analyzing large programs.

If you don't have a program to profile right now, you can `go run ./example` which should allow you to reproduce the graphs you see above. If you've never seen such graphs before, and are unsure how to read them, head over to Brendan Gregg's [Flame Graph](http://www.brendangregg.com/flamegraphs.html) page.

## The Problem

Let's say you've been tasked to optimize a simple program that has a loop calling out to three functions:

```go
func main() {
	for {
		// Http request to a web service that might be slow.
		slowNetworkRequest()
		// Some heavy CPU computation.
		cpuIntensiveTask()
		// Poorly named function that you don't understand yet.
		weirdFunction()
	}
}
```

One way to decide which of these three functions you should focus your attention on would be to wrap each function call like this:

```go
start := time.Start()
slowNetworkRequest()
fmt.Printf("slowNetworkRequest: %s\n", time.Since(start))
// ...
```

However, this can be very tedious for large programs. You'll also have to figure out how to average the numbers in case they fluctuate. And once you've done that, you'll have to repeat the process for the functions called by the function you decide to focus on.

### /debug/pprof/profile

So, this seems like a perfect use case for a profiler. Let's try the `/debug/pprof/profile` endpoint of the builtin `net/http/pprof` pkg to analyze our program for 10s:

```go
import _ "net/http/pprof"

func main() {
	go func() {
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	// <code to profile>
}
```

```
go tool pprof -http=:6061 http://localhost:6060/debug/pprof/profile?seconds=10
```

That was easy! Looks like we're spending all our time in `cpuIntensiveTask()`, so let's focus on that?

![](./assets/pprof_cpu.png)

But before we get carried away, let's quickly double check this assumption by manually timing our function calls with `time.Since()` as described above:

```
slowNetworkRequest: 66.815041ms
cpuIntensiveTask: 30.000672ms
weirdFunction: 10.64764ms
slowNetworkRequest: 67.194516ms
cpuIntensiveTask: 30.000912ms
weirdFunction: 10.105371ms
// ...
```

Oh no, the builtin CPU profiler is misleading us! How is that possible? Well, it turns out the builtin profiler only shows On-CPU time. Time spent waiting on I/O is completely hidden from us.

### /debug/pprof/trace

Let's try something else. The `/debug/pprof/trace` endpoint includes a "synchronization blocking profile", maybe that's what we need?

```
curl -so pprof.trace http://localhost:6060/debug/pprof/trace?seconds=10
go tool trace --pprof=sync pprof.trace > sync.pprof
go tool pprof --http=:6061 sync.pprof
```

Oh no, we're being mislead again. This profiler thinks all our time is spent on `slowNetworkRequest()`. It's completely missing `cpuIntensiveTask()`. And what about `weirdFunction()`? It seems like no builtin profiler can see it?

![](./assets/pprof_trace.png)

### /debug/fgprof

So what can we do? Let's try fgprof, which is designed to analyze mixed I/O and CPU workloads like the one we're dealing with here. We can easily add it alongside the builtin profilers.

```go
import(
	_ "net/http/pprof"
	"github.com/felixge/fgprof"
)

func main() {
	http.DefaultServeMux.Handle("/debug/fgprof", fgprof.Handler())
	go func() {
		log.Println(http.ListenAndServe(":6060", nil))
	}()

	// <code to profile>
}
```



```
go tool pprof --http=:6061 http://localhost:6060/debug/fgprof?seconds=10
```

Finally, a profile that shows all three of our functions and how much time we're spending on them. It also turns out our `weirdFunction()` was simply calling `time.Sleep()`, how weird indeed!

![](./assets/fgprof_pprof.png)

## How it Works

### fgprof

fgprof is implemented as a background goroutine that wakes up 99 times per second and calls `runtime.GoroutineProfile`. This returns a list of all goroutines regardless of their current On/Off CPU scheduling status and their call stacks.

This data is used to maintain an in-memory stack counter which can be converted to the pprof or folded output format. The meat of the implementation is super simple and < 100 lines of code, you should [check it out](./fgprof.go).

The overhead of fgprof increases with the number of active goroutines (including those waiting on I/O, Channels, Locks, etc.) executed by your program. If your program typically has less than 1000 active goroutines, you shouldn't have much to worry about. However, at 10k or more goroutines fgprof is likely to become unable to maintain its sampling rate and to significantly degrade the performance of your application, see [BenchmarkProfilerGoroutines.txt](./BenchmarkProfilerGoroutines.txt). The latter is due to `runtime.GoroutineProfile()` calling `stopTheWorld()`. For now the advise is to test the impact of the profiler on a development environment before running it against production instances. There are ideas for making fgprof more scalable and safe for programs with a high number of goroutines, but they will likely need improved APIs from the Go core.

### Go's builtin CPU Profiler

The builtin Go CPU profiler uses the [setitimer(2)](https://linux.die.net/man/2/setitimer) system call to ask the operating system to be sent a `SIGPROF` signal 100 times a second. Each signal stops the Go process and gets delivered to a random thread's `sigtrampgo()` function. This function then proceeds to call `sigprof()` or `sigprofNonGo()` to record the thread's current stack.

Since Go uses non-blocking I/O, Goroutines that wait on I/O are parked and not running on any threads. Therefore they end up being largely invisible to Go's builtin CPU profiler.

## The Future of Go Profiling

There is a great proposal for [hardware performance counters for CPU profiling](https://go.googlesource.com/proposal/+/refs/changes/08/219508/2/design/36821-perf-counter-pprof.md#5-empirical-evidence-on-the-accuracy-and-precision-of-pmu-profiles) in Go. The proposal is aimed at making the builtin CPU Profiler even more accurate, especially under highly parallel workloads on many CPUs. It also includes a very in-depth analysis of the current profiler. Based on the design, I think the proposed profiler would also be blind to I/O workloads, but still seems appealing for CPU based workloads.

As far as fgprof itself is concerned, I might implement streaming output, leaving the final aggregation to other tools. This would open the door to even more advanced analysis, perhaps by integrating with tools such as [flamescope](https://github.com/Netflix/flamescope).

Additionally I'm also open to the idea of contributing fgprof to the Go project itself. I've [floated the idea](https://groups.google.com/g/golang-dev/c/LCJyvL90xv8) on the golang-dev mailing list, so let's see what happens.


## Known Issues

There is no perfect approach to profiling, and fgprof is no exception. Below is a list of known issues that will hopefully not be of practical concern for most users, but are important to highlight.

- fgprof can't catch goroutines while they are running in loops without function calls, only when they get asynchronously preempted. This can lead to reporting inaccuracies. Use the builtin CPU profiler if this is a problem for you.
- fgprof may not work in Go 1.13 if another goroutine is in a loop without function calls the whole time. Async preemption in Go 1.14 should mostly fix this issue.
- Internal C functions are not showing up in the stack traces, e.g. `runtime.nanotime` which is called by `time.Since` in the example program.
- The current implementation is relying on the Go scheduler to schedule the internal goroutine at a fixed sample rate. Scheduler delays, especially biased ones, might cause inaccuracies.

## Credits

The following articles helped me to learn more about how profilers in general, and the Go profiler in particular work.

- [How do Ruby & Python profilers work?](https://jvns.ca/blog/2017/12/17/how-do-ruby---python-profilers-work-/) by Julia Evans
- [Profiling Go programs with pprof](https://jvns.ca/blog/2017/09/24/profiling-go-with-pprof/) by Julia Evans

## License

fgprof is licensed under the MIT License.
