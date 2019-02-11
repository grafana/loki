# Configuring Grafana via Provisioning

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

- `http://localhost:3100` when run Loki locally or with docker
- `http://loki:3100` when run Loki with docker-compose, or with helm in kubernetes

`basicAuthUser` and `basicAuthPassword` should same as your Grafana setting.
