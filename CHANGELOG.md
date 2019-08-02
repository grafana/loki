# 0.3.0 (unreleased)

# 0.2.0 (2019-08-02)

There were over 100 PR's merged since 0.1.0 was released, here's a highlight:

### Features / Enhancements

* **Loki**:  [521](https://github.com/grafana/loki/pull/521) Query label values and names are now fetched from the store.
* **Loki**:  [541](https://github.com/grafana/loki/pull/541) Improvements in live tailing of logs.
* **Loki**: [713](https://github.com/grafana/loki/pull/713) Storage memory improvement.
* **Loki**: [764](https://github.com/grafana/loki/pull/764) Tailing can fetch previous logs for context.
* **Loki**: [782](https://github.com/grafana/loki/pull/782) Performance improvement: Query storage by iterating through chunks in batches.
* **Loki**: [788](https://github.com/grafana/loki/pull/788) Querier timeouts.
* **Loki**: [794](https://github.com/grafana/loki/pull/794) Support ingester chunk transfer on shutdown.
* **Loki**: [729](https://github.com/grafana/loki/pull/729) Bigtable backup tool support.


* **Pipeline**: [738](https://github.com/grafana/loki/pull/738) Added a template stage for manipulating label values.
* **Pipeline**: [732](https://github.com/grafana/loki/pull/732) Support for Unix timestamps.
* **Pipeline**: [760](https://github.com/grafana/loki/pull/760) Support timestamps without year.


* **Helm**:  [641](https://github.com/grafana/loki/pull/641) Helm integration testing.
* **Helm**: [824](https://github.com/grafana/loki/pull/824) Add service monitor.
* **Helm**: [830](https://github.com/grafana/loki/pull/830) Customize namespace.


* **Docker-Plugin**: [663](https://github.com/grafana/loki/pull/663) Created a Docker logging driver plugin.


* **Fluent-Plugin**: [669](https://github.com/grafana/loki/pull/669) Ability to specify keys to remove.
* **Fluent-Plugin**: [709](https://github.com/grafana/loki/pull/709) Multi-worker support.
* **Fluent-Plugin**: [792](https://github.com/grafana/loki/pull/792) Add prometheus for metrics and update gems.


* **Build**: [668](https://github.com/grafana/loki/pull/668),[762](https://github.com/grafana/loki/pull/762) Build multiple architecture containers.


* **Loki-Canary**: [772](https://github.com/grafana/loki/pull/772) Moved into Loki project.

### Bugfixes

There were many fixes, here are a few of the most important:

* **Promtail**: [650](https://github.com/grafana/loki/pull/650) Build on windows.
* **Fluent-Plugin**: [667](https://github.com/grafana/loki/pull/667) Rename fluent plugin.
* **Docker-Plugin**: [813](https://github.com/grafana/loki/pull/813) Fix panic for newer docker version (18.09.7+).


# 0.1.0 (2019-06-03)

First (beta) Release!
