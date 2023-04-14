variable "version" {
  type        = string
  description = "Loki version"
  default     = "2.7.5"
}

job "loki" {
  datacenters = ["dc1"]

  group "loki" {
    count = 1

    ephemeral_disk {
      # Used to store index, cache, WAL
      # Nomad will try to preserve the disk between job updates
      size   = 1000
      sticky = true
    }

    network {
      port "http" {
        to     = 3100
        static = 3100
      }
      port "grpc" {}
    }

    task "loki" {
      driver = "docker"
      user   = "nobody"

      config {
        image = "grafana/loki:${var.version}"
        ports = [
          "http",
          "grpc",
        ]
        args = [
          "-target=all",
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

      dynamic "template" {
        for_each = fileset(".", "rules/**")

        content {
          data            = file(template.value)
          destination     = "local/${template.value}"
          left_delimiter  = "[["
          right_delimiter = "]]"
        }
      }

      service {
        name = "loki"
        port = "http"

        # use Traefik to loadbalance between Loki instances
        tags = [
          "traefik.enable=true",
          "traefik.http.routers.loki.entrypoints=https",
          "traefik.http.routers.loki.rule=Host(`loki.service.consul`)",
        ]

        check {
          name     = "Loki"
          port     = "http"
          type     = "http"
          path     = "/ready"
          interval = "20s"
          timeout  = "1s"

          initial_status = "passing"
        }
      }

      resources {
        # adjust to suite your load
        cpu    = 500
        memory = 256
        # requiers memory_oversubscription
        # https://www.nomadproject.io/api-docs/operator/scheduler#update-scheduler-configuration
        # memory_max = 512
      }
    }
  }
}
