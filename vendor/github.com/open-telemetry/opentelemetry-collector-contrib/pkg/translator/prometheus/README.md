# Prometheus Normalization

[OpenTelemetry's metric semantic convention](https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/metrics/semantic_conventions/README.md) is not compatible with [Prometheus' own metrics naming convention](https://prometheus.io/docs/practices/naming/). This module provides centralized functions to convert OpenTelemetry metrics to Prometheus-compliant metrics. These functions are used by the following components for Prometheus:

* [prometheusreceiver](../../../receiver/prometheusreceiver/)
* [prometheusexporter](../../../exporter/prometheusexporter/)
* [prometheusremotewriteexporter](../../../exporter/prometheusremotewriteexporter/)

## Metric name

### Full normalization

> **Warning**
>
> This feature can be controlled with [feature gate](https://github.com/open-telemetry/opentelemetry-collector/tree/main/featuregate) `pkg.translator.prometheus.NormalizeName`. It is currently enabled by default (beta stage).
>
>  Example of how to disable it:
> ```shell-session
> $ otelcol --config=config.yaml --feature-gates=-pkg.translator.prometheus.NormalizeName
> ```

#### List of transformations to convert OpenTelemetry metrics to Prometheus metrics

| Case                                                     | Transformation                                                                                                                   | Example                                                                                   |
|----------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------|
| Unsupported characters and extraneous underscores        | Replace unsupported characters with underscores (`_`). Drop redundant, leading and trailing underscores.                         | `(lambda).function.executions(#)` → `lambda_function_executions`                          |
| Standard unit                                            | Convert the unit from [Unified Code for Units of Measure](http://unitsofmeasure.org/ucum.html) to Prometheus standard and append | `system.filesystem.usage` with unit `By` → `system_filesystem_usage_bytes`                |
| Non-standard unit (unit is surrounded with `{}`)         | Drop the unit                                                                                                                    | `system.network.dropped` with unit `{packets}` → `system_network_dropped`                 |
| Non-standard unit (unit is **not** surrounded with `{}`) | Append the unit, if not already present, after sanitization (all non-alphanumeric chars are dropped)                             | `system.network.dropped` with unit `packets` → `system_network_dropped_packets`           |
| Percentages (unit is `1`)                                | Append `_ratio` (for gauges only)                                                                                                | `system.memory.utilization` with unit `1` → `system_memory_utilization_ratio`             |
| Percentages (unit is `%`)                                | Replace `%` with `percent` `_percent`                                                                                            | `storage.filesystem.utilization` with unit `%` → `storage_filesystem_utilization_percent` |
| Rates (unit contains `/`)                                | Replace `/` with `per`                                                                                                           | `astro.light.speed` with unit `m/s` → `astro_light_speed_meters_per_second`               |
| Counter                                                  | Append `_total`                                                                                                                  | `system.processes.created` → `system_processes_created_total`                             |

List of standard OpenTelemetry units that will be translated to [Prometheus standard base units](https://prometheus.io/docs/practices/naming/#base-units):

| OpenTelemetry Unit | Corresponding Prometheus Unit |
| ------------------ | ----------------------------- |
| **Time**           |                               |
| `d`                | `days`                        |
| `h`                | `hours`                       |
| `min`              | `minutes`                     |
| `s`                | `seconds`                     |
| `ms`               | `milliseconds`                |
| `us`               | `microseconds`                |
| `ns`               | `nanoseconds`                 |
| **Bytes**          |                               |
| `By`               | `bytes`                       |
| `KiBy`             | `kibibytes`                   |
| `MiBy`             | `mebibytes`                   |
| `GiBy`             | `gibibytes`                   |
| `TiBy`             | `tibibytes`                   |
| `KBy`              | `kilobytes`                   |
| `MBy`              | `megabytes`                   |
| `GBy`              | `gigabytes`                   |
| `TBy`              | `terabytes`                   |
| **SI Units**       |                               |
| `m`                | `meters`                      |
| `V`                | `volts`                       |
| `A`                | `amperes`                     |
| `J`                | `joules`                      |
| `W`                | `watts`                       |
| `g`                | `grams`                       |
| **Misc.**          |                               |
| `Cel`              | `celsius`                     |
| `Hz`               | `hertz`                       |
| `%`                | `percent`                     |

> **Note**
> Prometheus also recommends using base units (no kilobytes, or milliseconds, for example) but these functions will not attempt to convert non-base units to base units.

#### List of transformations performed to convert Prometheus metrics to OpenTelemetry metrics

| Case                               | Transformation                                                         | Example                                                                         |
|------------------------------------|------------------------------------------------------------------------|---------------------------------------------------------------------------------|
| UNIT defined in OpenMetrics format | Drop the unit suffix and set it in the OpenTelemetry metric unit field | `system_network_dropped_packets` → `system_network_dropped` with `packets` unit |
| Counter                            | Drop `_total` suffix                                                   | `system_processes_created_total`→ `system_processes_created`                    |

### Simple normalization

If feature `pkg.translator.prometheus.NormalizeName` is not enabled, a simple sanitization of the OpenTelemetry metric name is performed to ensure it follows Prometheus naming conventions:

* Drop unsupported characters and replace with underscores (`_`)
* Remove redundant, leading and trailing underscores
* Ensure metric name doesn't start with a digit by prefixing with an underscore

No processing of the unit is performed, and `_total` is not appended for *Counters*.

## Labels

OpenTelemetry *Attributes* are converted to Prometheus labels and normalized to follow the [Prometheus labels naming rules](https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels).

The following transformations are performed on OpenTelemetry *Attributes* to produce Prometheus labels:

* Drop unsupported characters and replace with underscores (`_`)
* Prefix label with `key_` if it doesn't start with a letter, except if it's already prefixed with double-underscore (`__`)

By default, labels that start with a simple underscore (`_`) are prefixed with `key`, which is strictly unnecessary to follow Prometheus labels naming rules. This behavior can be disabled with the feature `pkg.translator.prometheus.PermissiveLabelSanitization`, which must be activated with the feature gate option of the collector:

```shell-session
$ otelcol --config=config.yaml --feature-gates=pkg.translator.prometheus.PermissiveLabelSanitization
```

Examples:

| OpenTelemetry Attribute | Prometheus Label |
|---|---|
| `name` | `name` |
| `host.name` | `host_name` |
| `host_name` | `host_name` |
| `name (of the host)` | `name__of_the_host_` |
| `2 cents` | `key_2_cents` |
| `__name` | `__name` |
| `_name` | `key_name` |
| `_name` | `_name` (if `PermissiveLabelSanitization` is enabled) |
