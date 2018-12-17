
## Configuring the Loki Datasource in Grafana

Grafana ships with built-in support for Loki in the [latest nightly builds](https://grafana.com/grafana/download). Loki support will be officially released in Grafana 6.0.

1. Open the side menu by clicking the Grafana icon in the top left.
2. In the side menu on the left, under the Setting link you should find a link named Data Sources.
3. Click the `+ Add data source` button in the top header.
4. Choose Loki from the list.
5. The http URL field should be the address of your Loki server e.g. `http://localhost:3100` or `http://loki:3100` when running with docker and docker-compose.
6. To see the logs, click "Explore" on the sidebar, select the Loki datasource, and then choose a log stream using the "Log labels" button.

Read more about the Explore feature in the [Grafana docs](http://docs.grafana.org/features/explore) and on how to search and filter logs with Loki.

## Grafana Provisioning

It is possible to configure Grafana datasources using config files with Grafana’s provisioning system. You can read more about how it works in the [Grafana documentation](http://docs.grafana.org/administration/provisioning/#datasources).

Here is a simple example of the provisioning yaml config for the Grafana Loki datasource:

```yaml
apiVersion: 1

datasources:
  - name: Loki
    type: loki
    access: proxy
    url: http://localhost:3100
    editable: false
```

Example with basic auth:

```yaml
apiVersion: 1

datasources:
  - name: Loki
    type: loki
    access: proxy
    url: http://localhost:3100
    editable: false
    basicAuth: true
    basicAuthUser: my_user
    basicAuthPassword: test_password
```

Make sure to adjust the url and authentication to your needs, the `url` should be:

- `http://localhost:3100` when run Loki locally
- `http://loki:3100` when run Loki with docker-compose

`basicAuthUser` and `basicAuthPassword` should same as your Grafana setting.

## Searching with Labels and Distributed Grep

A log query consists of two parts: **log stream selector**, and a **search expression**. For performance reasons you need to start by choosing a log stream by selecting a log label.

The log stream selector will reduce the number of log streams to a manageable volume and then the regex search expression is used to do a distributed grep over those log streams.

Searching can be done in the Explore section of Grafana (latest nightly builds) or via the `logcli` tool which is documented [here](https://github.com/grafana/loki/blob/master/docs/logcli.md).

### Log Stream Selector

For the label part of the query expression, wrap it in curly braces `{}` and then use the key value syntax for selecting labels. Multiple label expressions are separated by a comma:

`{app="mysql",name="mysql-backup"}`

The following label matching operators are currently supported:

- `=` exactly equal.
- `!=` not equal.
- `=~` regex-match.
- `!~` do not regex-match.

Examples:

- `{name=~"mysql.+"}`
- `{name!~"mysql.+"}`

The [same rules that apply for Prometheus Label Selectors](https://prometheus.io/docs/prometheus/latest/querying/basics/#instant-vector-selectors) apply for Loki Log Stream Selectors.

### Regex Search Expression

After writing the Log Stream Selector, you can filter the results further by writing a search expression. The search expression can be just text or a regex expression.

Example queries:

- `{job="mysql"} error`
- `{name="kafka"} tsdb-ops.*io:2003`
- `{instance=~"kafka-[23]",name="kafka"} kafka.server:type=ReplicaManager`