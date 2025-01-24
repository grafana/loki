kprom
===

kprom is a plug-in package to provide prometheus
[metrics](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus)
through a
[`kgo.Hook`](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo#Hook).

This package tracks the following metrics under the following names, all
metrics being counter vecs:

```go
#{ns}_connects_total{node_id="#{node}"}
#{ns}_connect_errors_total{node_id="#{node}"}
#{ns}_write_errors_total{node_id="#{node}"}
#{ns}_write_bytes_total{node_id="#{node}"}
#{ns}_read_errors_total{node_id="#{node}"}
#{ns}_read_bytes_total{node_id="#{node}"}
#{ns}_produce_bytes_total{node_id="#{node}",topic="#{topic}"}
#{ns}_fetch_bytes_total{node_id="#{node}",topic="#{topic}"}
#{ns}_buffered_produce_records_total
#{ns}_buffered_fetch_records_total
```

The above metrics can be expanded considerably with options in this package,
allowing timings, uncompressed and compressed bytes, and different labels.

Note that seed brokers use broker IDs prefixed with "seed_", with the number
corresponding to which seed it is.

To use,

```go
metrics := kprom.NewMetrics("namespace")
cl, err := kgo.NewClient(
	kgo.WithHooks(metrics),
	// ...other opts
)
```

You can use your own prometheus registry, as well as a few other options.
See the package [documentation](https://pkg.go.dev/github.com/twmb/franz-go/plugin/kprom) for more info!
