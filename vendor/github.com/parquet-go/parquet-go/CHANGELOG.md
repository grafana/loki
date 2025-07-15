 # v0.17.0

 ## Breaking Changes

 -  migrate to module github.com/parquet-go/parquet-go [#3](https://github.com/parquet-go/parquet-go/pull/3) @kevinburke 
 -  drop support for `go1.17` [#16](https://github.com/parquet-go/parquet-go/pull/16) @gernest

 ## Bug fixes

 - fix error handling when reading from io.ReaderAt  [#18](https://github.com/parquet-go/parquet-go/pull/18) @gernest
 - fix zero value of nested field point [#9](https://github.com/parquet-go/parquet-go/pull/9) @gernest
 - fix memory corruption in `MergeRowGroups` [#31](https://github.com/parquet-go/parquet-go/pull/31) @gernest

 ## Enhancements
 - performance improvement on GenericReader [#17](https://github.com/parquet-go/parquet-go/pull/17) @gernest, @zolstein
 - stabilize flakey `TestOpenFile` [#11](https://github.com/parquet-go/parquet-go/pull/11) @gernest
