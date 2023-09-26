variable "version" {
  type        = string
  description = "Loki version"
  default     = "2.7.5"
}

job "loki" {
  datacenters = ["dc1"]

  group "compactor" {
    count = 1

    ephemeral_disk {
      size    = 1000
      sticky  = true
    }

    network {
      port "http" {}
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

        "traefik.http.routers.loki-compactor-ring.entrypoints=https",
        "traefik.http.routers.loki-compactor-ring.rule=Host(`loki-compactor.service.consul`) && Path(`/compactor/ring`)",
      ]

      check {
        name     = "Loki compactor"
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }
    }

    task "compactor" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
        S3_ACCESS_KEY_ID=<access_key>
        S3_SECRET_ACCESS_KEY=<secret_access_key>
        EOH

        destination = "secrets/s3.env"
        env         = true
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
      sticky  = true
    }

    network {
      port "http" {}
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

        "traefik.http.routers.loki-ruler.entrypoints=https",
        "traefik.http.routers.loki-ruler.rule=Host(`loki-query-frontend.service.consul`) && (PathPrefix(`/loki/api/v1/rules`) || PathPrefix(`/api/prom/rules`) || PathPrefix (`/prometheus/api/v1`))",

        "traefik.http.routers.loki-ruler-ring.entrypoints=https",
        "traefik.http.routers.loki-ruler-ring.rule=Host(`loki-ruler.service.consul`) && Path(`/ruler/ring`)",
      ]

      check {
        name     = "Loki ruler"
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }
    }

    task "ruler" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
        S3_ACCESS_KEY_ID=<access_key>
        S3_SECRET_ACCESS_KEY=<secret_access_key>
        EOH

        destination = "secrets/s3.env"
        env         = true
      }

      resources {
        cpu        = 1000
        memory     = 256
        memory_max = 512
      }
    }
  }

  group "distributor" {
    count = 2

    network {
      port "http" {}
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

        "traefik.http.routers.loki-distributor.entrypoints=https",
        "traefik.http.routers.loki-distributor.rule=Host(`loki-distributor.service.consul`)",

        "traefik.http.routers.loki-distributor-ring.entrypoints=https",
        "traefik.http.routers.loki-distributor-ring.rule=Host(`loki-distributor.cinarra.com`) && Path(`/distributor/ring`)",
      ]

      check {
        name     = "Loki distributor"
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }
    }

    task "distributor" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
      sticky  = true
    }

    network {
      port "http" {}
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

        "traefik.http.routers.loki-ingester-ring.entrypoints=https",
        "traefik.http.routers.loki-ingester-ring.rule=Host(`loki-ingester.service.consul`) && Path(`/ring`)",
      ]

      check {
        name     = "Loki ingester"
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }
    }

    task "ingester" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
        S3_ACCESS_KEY_ID=<access_key>
        S3_SECRET_ACCESS_KEY=<secret_access_key>
        EOH

        destination = "secrets/s3.env"
        env         = true
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
      port "http" {}
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
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "50s"
        timeout  = "1s"
      }
    }

    task "querier" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
        S3_ACCESS_KEY_ID=<access_key>
        S3_SECRET_ACCESS_KEY=<secret_access_key>
        EOH

        destination = "secrets/s3.env"
        env         = true
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
      port "http" {}
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
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }
    }

    task "query-scheduler" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
      port "http" {}
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

        "traefik.http.routers.loki-query-frontend.entrypoints=https",
        "traefik.http.routers.loki-query-frontend.rule=Host(`loki-query-frontend.service.consul`)",
      ]

      check {
        name     = "Loki query-frontend"
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }
    }

    task "query-frontend" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
      sticky  = true
    }

    network {
      port "http" {}
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
        port     = "http"
        type     = "http"
        path     = "/ready"
        interval = "20s"
        timeout  = "1s"
      }
    }

    task "index-gateway" {
      driver       = "docker"
      user         = "nobody"
      kill_timeout = "90s"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
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
        S3_ACCESS_KEY_ID=<access_key>
        S3_SECRET_ACCESS_KEY=<secret_access_key>
        EOH

        destination = "secrets/s3.env"
        env         = true
      }

      resources {
        cpu        = 200
        memory     = 128
        memory_max = 1024
      }
    }
  }
}
