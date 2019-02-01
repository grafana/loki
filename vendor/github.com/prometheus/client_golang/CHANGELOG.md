## 0.9.2 / 2018-12-06
* [FEATURE] Support for Go modules. #501
* [FEATURE] `Timer.ObserveDuration` returns observed duration. #509
* [ENHANCEMENT] Improved doc comments and error messages. #504 
* [BUGFIX] Fix race condition during metrics gathering. #512
* [BUGFIX] Fix testutil metric comparison for Histograms and empty labels. #494
  #498

## 0.9.1 / 2018-11-03
* [FEATURE] Add `WriteToTextfile` function to facilitate the creation of
  *.prom files for the textfile collector of the node exporter. #489
* [ENHANCEMENT] More descriptive error messages for inconsistent label
  cardinality. #487
* [ENHANCEMENT] Exposition: Use a GZIP encoder pool to avoid allocations in
  high-frequency scrape scenarios. #366
* [ENHANCEMENT] Exposition: Streaming serving of metrics data while encoding.
  #482
* [ENHANCEMENT] API client: Add a way to return the body of a 5xx response.
  #479

## 0.9.0 / 2018-10-15
* [CHANGE] Go1.6 is no longer supported.
* [CHANGE] More refinements of the `Registry` consistency checks: Duplicated
  labels are now detected, but inconsistent label dimensions are now allowed.
  Collisions with the “magic” metric and label names in Summaries and
  Histograms are detected now. #108 #417 #471
* [CHANGE] Changed `ProcessCollector` constructor. #219
* [CHANGE] Changed Go counter `go_memstats_heap_released_bytes_total` to gauge
  `go_memstats_heap_released_bytes`. #229
* [CHANGE] Unexported `LabelPairSorter`. #453
* [CHANGE] Removed the `Untyped` metric from direct instrumentation. #340
* [CHANGE] Unexported `MetricVec`. #319
* [CHANGE] Removed deprecated `Set` method from `Counter` #247
* [CHANGE] Removed deprecated `RegisterOrGet` and `MustRegisterOrGet`. #247
* [CHANGE] API client: Introduced versioned packages.
* [FEATURE] A `Registerer` can be wrapped with prefixes and labels. #357
* [FEATURE] “Describe by collect” helper function. #239
* [FEATURE] Added package `testutil`. #58
* [FEATURE] Timestamp can be explicitly set for const metrics. #187
* [FEATURE] “Unchecked” collectors are possible now without cheating. #47
* [FEATURE] Pushing to the Pushgateway reworked in package `push` to support
  many new features. (The old functions are still usable but deprecated.) #372
  #341
* [FEATURE] Configurable connection limit for scrapes. #179
* [FEATURE] New HTTP middlewares to instrument `http.Handler` and
  `http.RoundTripper`. The old middlewares and the pre-instrumented `/metrics`
  handler are (strongly) deprecated. #316 #57 #101 #224
* [FEATURE] “Currying” for metric vectors. #320
* [FEATURE] A `Summary` can be created without quantiles. #118
* [FEATURE] Added a `Timer` helper type. #231
* [FEATURE] Added a Graphite bridge. #197
* [FEATURE] Help strings are now optional. #460
* [FEATURE] Added `process_virtual_memory_max_bytes` metric. #438 #440
* [FEATURE] Added `go_gc_cpu_fraction` and `go_threads` metrics. #281 #277
* [FEATURE] Added `promauto` package with auto-registering metrics. #385 #393
* [FEATURE] Add `SetToCurrentTime` method to `Gauge`. #259
* [FEATURE] API client: Add AlertManager, Status, and Target methods. #402
* [FEATURE] API client: Add admin methods. #398
* [FEATURE] API client: Support series API. #361
* [FEATURE] API client: Support querying label values.
* [ENHANCEMENT] Smarter creation of goroutines during scraping. Solves memory
  usage spikes in certain situations. #369
* [ENHANCEMENT] Counters are now faster if dealing with integers only. #367
* [ENHANCEMENT] Improved label validation. #274 #335
* [BUGFIX] Creating a const metric with an invalid `Desc` returns an error. #460
* [BUGFIX] Histogram observations don't race any longer with exposition. #275
* [BUGFIX] Fixed goroutine leaks. #236 #472
* [BUGFIX] Fixed an error message for exponential histogram buckets. #467
* [BUGFIX] Fixed data race writing to the metric map. #401
* [BUGFIX] API client: Decode JSON on a 4xx respons but do not on 204
  responses. #476 #414

## 0.8.0 / 2016-08-17
* [CHANGE] Registry is doing more consistency checks. This might break
  existing setups that used to export inconsistent metrics.
* [CHANGE] Pushing to Pushgateway moved to package `push` and changed to allow
  arbitrary grouping.
* [CHANGE] Removed `SelfCollector`.
* [CHANGE] Removed `PanicOnCollectError` and `EnableCollectChecks` methods.
* [CHANGE] Moved packages to the prometheus/common repo: `text`, `model`,
  `extraction`.
* [CHANGE] Deprecated a number of functions.
* [FEATURE] Allow custom registries. Added `Registerer` and `Gatherer`
  interfaces.
* [FEATURE] Separated HTTP exposition, allowing custom HTTP handlers (package
  `promhttp`) and enabling the creation of other exposition mechanisms.
* [FEATURE] `MustRegister` is variadic now, allowing registration of many
  collectors in one call.
* [FEATURE] Added HTTP API v1 package.
* [ENHANCEMENT] Numerous documentation improvements.
* [ENHANCEMENT] Improved metric sorting.
* [ENHANCEMENT] Inlined fnv64a hashing for improved performance.
* [ENHANCEMENT] Several test improvements.
* [BUGFIX] Handle collisions in MetricVec.

## 0.7.0 / 2015-07-27
* [CHANGE] Rename ExporterLabelPrefix to ExportedLabelPrefix.
* [BUGFIX] Closed gaps in metric consistency check.
* [BUGFIX] Validate LabelName/LabelSet on JSON unmarshaling.
* [ENHANCEMENT] Document the possibility to create "empty" metrics in
  a metric vector.
* [ENHANCEMENT] Fix and clarify various doc comments and the README.md.
* [ENHANCEMENT] (Kind of) solve "The Proxy Problem" of http.InstrumentHandler.
* [ENHANCEMENT] Change responseWriterDelegator.written to int64.

## 0.6.0 / 2015-06-01
* [CHANGE] Rename process_goroutines to go_goroutines.
* [ENHANCEMENT] Validate label names during YAML decoding.
* [ENHANCEMENT] Add LabelName regular expression.
* [BUGFIX] Ensure alignment of struct members for 32-bit systems.

## 0.5.0 / 2015-05-06
* [BUGFIX] Removed a weakness in the fingerprinting aka signature code.
  This makes fingerprinting slower and more allocation-heavy, but the
  weakness was too severe to be tolerated.
* [CHANGE] As a result of the above, Metric.Fingerprint is now returning
  a different fingerprint. To keep the same fingerprint, the new method
  Metric.FastFingerprint was introduced, which will be used by the
  Prometheus server for storage purposes (implying that a collision
  detection has to be added, too).
* [ENHANCEMENT] The Metric.Equal and Metric.Before do not depend on
  fingerprinting anymore, removing the possibility of an undetected
  fingerprint collision.
* [FEATURE] The Go collector in the exposition library includes garbage
  collection stats.
* [FEATURE] The exposition library allows to create constant "throw-away"
  summaries and histograms.
* [CHANGE] A number of new reserved labels and prefixes.

## 0.4.0 / 2015-04-08
* [CHANGE] Return NaN when Summaries have no observations yet.
* [BUGFIX] Properly handle Summary decay upon Write().
* [BUGFIX] Fix the documentation link to the consumption library.
* [FEATURE] Allow the metric family injection hook to merge with existing
  metric families.
* [ENHANCEMENT] Removed cgo dependency and conditional compilation of procfs.
* [MAINTENANCE] Adjusted to changes in matttproud/golang_protobuf_extensions.

## 0.3.2 / 2015-03-11
* [BUGFIX] Fixed the receiver type of COWMetric.Set(). This method is
  only used by the Prometheus server internally.
* [CLEANUP] Added licenses of vendored code left out by godep.

## 0.3.1 / 2015-03-04
* [ENHANCEMENT] Switched fingerprinting functions from own free list to
  sync.Pool.
* [CHANGE] Makefile uses Go 1.4.2 now (only relevant for examples and tests).

## 0.3.0 / 2015-03-03
* [CHANGE] Changed the fingerprinting for metrics. THIS WILL INVALIDATE ALL
  PERSISTED FINGERPRINTS. IF YOU COMPILE THE PROMETHEUS SERVER WITH THIS
  VERSION, YOU HAVE TO WIPE THE PREVIOUSLY CREATED STORAGE.
* [CHANGE] LabelValuesToSignature removed. (Nobody had used it, and it was
  arguably broken.)
* [CHANGE] Vendored dependencies. Those are only used by the Makefile. If
  client_golang is used as a library, the vendoring will stay out of your way.
* [BUGFIX] Remove a weakness in the fingerprinting for metrics. (This made
  the fingerprinting change above necessary.)
* [FEATURE] Added new fingerprinting functions SignatureForLabels and
  SignatureWithoutLabels to be used by the Prometheus server. These functions
  require fewer allocations than the ones currently used by the server.

## 0.2.0 / 2015-02-23
* [FEATURE] Introduce new Histagram metric type.
* [CHANGE] Ignore process collector errors for now (better error handling
  pending).
* [CHANGE] Use clear error interface for process pidFn.
* [BUGFIX] Fix Go download links for several archs and OSes.
* [ENHANCEMENT] Massively improve Gauge and Counter performance.
* [ENHANCEMENT] Catch illegal label names for summaries in histograms.
* [ENHANCEMENT] Reduce allocations during fingerprinting.
* [ENHANCEMENT] Remove cgo dependency. procfs package will only be included if
  both cgo is available and the build is for an OS with procfs.
* [CLEANUP] Clean up code style issues.
* [CLEANUP] Mark slow test as such and exclude them from travis.
* [CLEANUP] Update protobuf library package name.
* [CLEANUP] Updated vendoring of beorn7/perks.

## 0.1.0 / 2015-02-02
* [CLEANUP] Introduced semantic versioning and changelog. From now on,
  changes will be reported in this file.
