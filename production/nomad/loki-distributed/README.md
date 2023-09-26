# Microservices mode

This Nomad job will deploy Loki in
[microservices mode](https://grafana.com/docs/loki/latest/fundamentals/architecture/deployment-modes/#microservices-mode)
using boltdb-shipper and S3 backend.

## Usage

Have a look at the job file and Loki configuration file and change it to suite
your environment.

### Run job

Inside directory with job run:

```shell
nomad run job.nomad.hcl
```

To deploy a different version change `variable.version` default value or
specify from command line:

```shell
nomad job run -var="version=2.7.5" job.nomad.hcl
```

### Scale Loki

Change `count` in job file in `group "loki"` and run:

```shell
nomad run job.nomad.hcl
```

or use Nomad CLI

```shell
nomad job scale loki distributor <count>
```

## Recommendations for running in production

### Gather metrics

To collect metrics from all components use this config:

```yaml
- job_name: "loki"
  consul_sd_configs:
    - services:
        - "loki-compactor"
        - "loki-ruler"
        - "loki-distributor"
        - "loki-ingestor"
        - "loki-querier"
        - "loki-index-gateway"
        - "loki-query-frontend"
        - "loki-query-scheduler"
  relabel_configs:
    - source_labels: ["__meta_consul_service_metadata_alloc_id"]
      target_label: "instance"
    - source_labels: ["__meta_consul_service_metadata_component"]
      target_label: "component"
```

### Secure HTTP endpoints with Consul Connect

Set network to `bridge` mode and add `health` port, that will be used by Consul
healthcheck:

```hcl
    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {}
    }
```

```hcl
    task "distributor" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.versions.loki}"
        ports = [
          "http",
          "health", # do not forget to publish health port
          "grpc",
        ]
```

Bind HTTP endpoint to `127.0.0.1:80` so it is not accessible from outside:

```yml
server:
  http_listen_address: 127.0.0.1
  http_listen_port: 80
```

Add service registration with Consul Connect enabled, `/metrics` and `/ready`
endpoint [exposed](https://www.nomadproject.io/docs/job-specification/expose)
and API accessible with basicauth through Traefik with Consul Connect
integration:

```hcl
    service {
      name = "loki-distributor"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "distributor"
      }

      tags = [
        "traefik.enable=true",
        "traefik.consulcatalog.connect=true",

        "traefik.http.routers.loki-distributor.entrypoints=https",
        "traefik.http.routers.loki-distributor.rule=Host(`loki-distributor.service.consul`)",
        "traefik.http.middlewares.loki-distributor.basicauth.users=promtail:$$apr1$$wnih40yf$$vcxJYiqcEQLknQAZcpy/I1",
        "traefik.http.routers.loki-distirbutor.middlewares=loki-distributor@consulcatalog",

        "traefik.http.routers.loki-distributor-ring.entrypoints=https",
        "traefik.http.routers.loki-distributor-ring.rule=Host(`loki-distributor.service.consul`) && Path(`/distributor/ring`)",
        "traefik.http.middlewares.loki-distributor-ring.basicauth.users=devops:$apr1$bNIZL02A$QrOgT3NAOx.koXWnqfXbo0",
        "traefik.http.routers.loki-distributor-ring.middlewares=loki-distributor-ring@consulcatalog",
      ]

      check {
        name     = "Loki distributor"
        port     = "health"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }

      connect {
        sidecar_service {
          proxy {
            local_service_port = 80

            expose {
              path {
                path            = "/metrics"
                protocol        = "http"
                local_path_port = 80
                listener_port   = "http"
              }

              path {
                path            = "/ready"
                protocol        = "http"
                local_path_port = 80
                listener_port   = "health"
              }
            }
          }
        }
      }
    }
```

## Secure GRPC endpoints with mTLS

Unfortenately Consul Connect cannot be used to secure GRPC communication between
Loki components, since some components should be able to connect to all
instances of other components. We can secure components GRPC communication with
Vault [PKI engine](https://www.vaultproject.io/docs/secrets/pki).

Certificate generation can be made less verbose with the following HCL trick:

1. Add the following to `locals`:

```hcl
locals {
  certs = {
    "CA"   = "issuing_ca",
    "cert" = "certificate",
    "key"  = "private_key",
  }
}
```

2. Add dynamic template per service:

```hcl
      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-<component_name>.service.consul" (env "attr.unique.network.ip-address" | printf "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }
```

3. Update config to use generated certificates
