---
title: Functions
---

# Functions

## label_replace()

For each timeseries in `v`, `label_replace(v instant-vector, dst_label string, replacement string, src_label string, regex string)` matches the regular expression `regex` against the label `src_label`. If it matches, then the timeseries is returned with the label `dst_label` replaced by the expansion of `replacement`. `$1` is replaced with the first matching subgroup, `$2` with the second etc. If the regular expression doesn't match then the timeseries is returned unchanged.

This example will return a vector with each time series having a `foo` label with the value `a` added to it:

```logql
label_replace(rate({job="api-server",service="a:c"} |= "err" [1m]), "foo", "$1", "service", "(.*):.*")
```
