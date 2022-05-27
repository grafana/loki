locals {
  version = "2.5.0"
  certs = {
    "CA"   = "issuing_ca",
    "cert" = "certificate",
    "key"  = "private_key",
  }
}

job "loki" {
  datacenters = ["dc1"]

  vault {
    policies = ["loki"]
  }

  group "compactor" {
    count = 1

    ephemeral_disk {
      size    = 1000
      migrate = true
      sticky  = true
    }

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {}
    }

    service {
      name = "loki-compactor"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "compactor"
      }

      tags = [
        "traefik.enable=true",
        "traefik.consulcatalog.connect=true",

        "traefik.http.routers.loki-compactor-ring.entrypoints=https",
        "traefik.http.routers.loki-compactor-ring.rule=Host(`loki-compactor.service.consul`) && Path(`/compactor/ring`)",
      ]

      check {
        name     = "Loki compactor"
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

    task "compactor" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=compactor",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      template {
        data = <<-EOH
        {{ with secret "secret/minio/loki" }}
        S3_ACCESS_KEY_ID={{ .Data.data.access_key }}
        S3_SECRET_ACCESS_KEY={{ .Data.data.secret_key }}
        {{- end }}
        EOH

        destination = "secrets/s3.env"
        env         = true
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-compactor.service.consul" (env "attr.unique.network.ip-address" | printf "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "1m"
        }
      }

      resources {
        cpu        = 3000
        memory     = 256
        memory_max = 1024
      }
    }
  }

  group "ruler" {
    count = 1

    ephemeral_disk {
      size    = 1000
      migrate = true
      sticky  = true
    }

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {}
    }

    service {
      name = "loki-ruler"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "ruler"
      }

      tags = [
        "traefik.enable=true",
        "traefik.consulcatalog.connect=true",

        "traefik.http.routers.loki-ruler.entrypoints=https",
        "traefik.http.routers.loki-ruler.rule=Host(`loki-query-frontend.service.consul`) && (PathPrefix(`/loki/api/v1/rules`) || PathPrefix(`/api/prom/rules`) || PathPrefix (`/prometheus/api/v1`))",

        "traefik.http.routers.loki-ruler-ring.entrypoints=https",
        "traefik.http.routers.loki-ruler-ring.rule=Host(`loki-ruler.service.consul`) && Path(`/ruler/ring`)",
      ]

      check {
        name     = "Loki ruler"
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

    task "ruler" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=ruler",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      dynamic "template" {
        for_each = fileset(".", "rules/**")

        content {
          data            = file(template.value)
          destination     = "local/${template.value}"
          left_delimiter  = "[["
          right_delimiter = "]]"
        }
      }

      template {
        data = <<-EOH
        {{ with secret "secret/minio/loki" }}
        S3_ACCESS_KEY_ID={{ .Data.data.access_key }}
        S3_SECRET_ACCESS_KEY={{ .Data.data.secret_key }}
        {{- end }}
        EOH

        destination = "secrets/s3.env"
        env         = true
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-ruler.service.consul" (env "attr.unique.network.ip-address" | printf "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }

      resources {
        cpu        = 1000
        memory     = 256
        memory_max = 512
      }
    }
  }

  group "distibutor" {
    count = 2

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {}
    }

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

        "traefik.http.routers.loki-distributor-ring.entrypoints=https",
        "traefik.http.routers.loki-distributor-ring.rule=Host(`loki-distributor.cinarra.com`) && Path(`/distributor/ring`)",
      ]

      check {
        name     = "Loki distibutor"
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

    task "distibutor" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=distributor",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-distributer.service.consul" (env "attr.unique.network.ip-address" | printf "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }

      resources {
        cpu        = 200
        memory     = 128
        memory_max = 1024
      }
    }
  }

  group "ingester" {
    count = 2

    constraint {
      # choose your failure domain
      # must be the same as `instance_availability_zone` in config file
      distinct_property = node.unique.name
      # distinct_property = node.datacenter
      # distinct_property = attr.platform.aws.placement.availability-zone
    }

    ephemeral_disk {
      size    = 4000
      migrate = true
      sticky  = true
    }

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {}
    }

    service {
      name = "loki-ingester"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "ingester"
      }

      tags = [
        "traefik.enable=true",
        "traefik.consulcatalog.connect=true",

        "traefik.http.routers.loki-ingester-ring.entrypoints=https",
        "traefik.http.routers.loki-ingester-ring.rule=Host(`loki-ingester.service.consul`) && Path(`/ring`)",
      ]

      check {
        name     = "Loki ingester"
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

    task "ingester" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=ingester",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      template {
        data = <<-EOH
        {{ with secret "secret/minio/loki" }}
        S3_ACCESS_KEY_ID={{ .Data.data.access_key }}
        S3_SECRET_ACCESS_KEY={{ .Data.data.secret_key }}
        {{- end }}
        EOH

        destination = "secrets/s3.env"
        env         = true
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-ingestor.service.consul" (env "attr.unique.network.ip-address" | printf "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }

      resources {
        cpu        = 300
        memory     = 128
        memory_max = 2048
      }
    }
  }

  group "querier" {
    count = 2

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {}
    }

    service {
      name = "loki-querier"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "querier"
      }

      check {
        name     = "Loki querier"
        port     = "health"
        type     = "http"
        path     = "/ready"
        interval = "50s"
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

    task "querier" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=querier",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      template {
        data = <<-EOH
        {{ with secret "secret/minio/loki" }}
        S3_ACCESS_KEY_ID={{ .Data.data.access_key }}
        S3_SECRET_ACCESS_KEY={{ .Data.data.secret_key }}
        {{- end }}
        EOH

        destination = "secrets/s3.env"
        env         = true
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-querier.service.consul" (env "attr.unique.network.ip-address" | printf  "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }

      resources {
        cpu        = 200
        memory     = 128
        memory_max = 2048
      }
    }
  }

  group "query-scheduler" {
    count = 2

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {
        to     = 9096
        static = 9096
      }
    }

    service {
      name = "loki-query-scheduler"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "query-scheduler"
      }

      check {
        name     = "Loki query-scheduler"
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

    task "query-scheduler" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=query-scheduler",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-query-scheduler.service.consul" (env "attr.unique.network.ip-address" | printf  "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }

      resources {
        cpu        = 100
        memory     = 64
        memory_max = 128
      }
    }
  }

  group "query-frontend" {
    count = 2

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {}
    }

    service {
      name = "loki-query-frontend"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "query-frontend"
      }

      tags = [
        "traefik.enable=true",
        "traefik.consulcatalog.connect=true",

        "traefik.http.routers.loki-query-frontend.entrypoints=https",
        "traefik.http.routers.loki-query-frontend.rule=Host(`loki-query-frontend.service.consul`)",
      ]

      check {
        name     = "Loki query-frontend"
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

    task "query-frontend" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=query-frontend",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-query-frontend.service.consul" (env "attr.unique.network.ip-address" | printf  "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }

      resources {
        cpu        = 100
        memory     = 64
        memory_max = 128
      }
    }
  }

  group "index-gateway" {
    count = 1

    ephemeral_disk {
      size    = 1000
      migrate = true
      sticky  = true
    }

    network {
      mode = "bridge"

      port "http" {}
      port "health" {}
      port "grpc" {
        to     = 9097
        static = 9097
      }
    }

    service {
      name = "loki-index-gateway"
      port = "http"

      meta {
        alloc_id  = NOMAD_ALLOC_ID
        component = "index-gateway"
      }

      check {
        name     = "Loki index-gateway"
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

    task "index-gateway" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${local.version}"
        ports = [
          "http",
          "health",
          "grpc",
        ]

        args = [
          "-target=index-gateway",
          "-config.file=/local/config.yml",
          "-config.expand-env=true",
        ]
      }

      template {
        data        = file("config.yml")
        destination = "local/config.yml"
      }

      template {
        data = <<-EOH
        {{ with secret "secret/minio/loki" }}
        S3_ACCESS_KEY_ID={{ .Data.data.access_key }}
        S3_SECRET_ACCESS_KEY={{ .Data.data.secret_key }}
        {{- end }}
        EOH

        destination = "secrets/s3.env"
        env         = true
      }

      dynamic "template" {
        for_each = local.certs
        content {
          data = <<-EOH
          {{- with secret "pki/issue/internal" "ttl=10d" "common_name=loki-index-gateway.service.consul" (env "attr.unique.network.ip-address" | printf "ip_sans=%s") -}}
          {{ .Data.${template.value} }}
          {{- end -}}
          EOH

          destination = "secrets/certs/${template.key}.pem"
          change_mode = "restart"
          splay       = "5m"
        }
      }

      resources {
        cpu        = 200
        memory     = 128
        memory_max = 1024
      }
    }
  }
}
