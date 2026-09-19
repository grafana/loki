variable "version" {
  type        = string
  description = "Loki version"
  default     = "3.7.2"
}

job "loki" {
  datacenters = ["dc1"]

  meta {
    version = "${var.version}"
    mode    = "ha-monolithic"
  }

  # Update strategy for zero downtime
  update {
    max_parallel     = 1 
    health_check     = "checks"
    min_healthy_time = "30s"
    healthy_deadline = "5m"
  }

  group "loki" {
    count = 3

    ephemeral_disk {
      size   = 1000
      sticky = true
    }

    network {
      port "http" {
        static = 3100
        to     = 3100
      }
      port "grpc" {
        static = 9095
        to     = 9095
      }
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

      service {
        name = "loki"
        port = "http"

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
        cpu    = 500
        memory = 512
      }
    }
  }
}
