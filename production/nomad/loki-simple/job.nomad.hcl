variable "version" {
  type        = string
  description = "Loki version"
  default     = "2.7.5"
}

job "loki" {
  datacenters = ["dc1"]

  group "read" {
    count = 1

    ephemeral_disk {
      size   = 1000
      sticky = true
    }

    network {
      port "http" {}
      port "grpc" {}
    }

    task "read" {
      driver = "docker"
      user   = "nobody"

      config {
        image = "grafana/loki:${var.version}"

        ports = [
          "http",
          "grpc",
        ]

        args = [
          "-target=read",
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
        name = "loki-read"
        port = "http"

        tags = [
          "traefik.enable=true",
          "traefik.http.routers.loki-read.entrypoints=https",
          "traefik.http.routers.loki-read.rule=Host(`loki-read.service.consul`)",
        ]

        check {
          name     = "Loki read"
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
        memory = 256
      }
    }
  }

  group "write" {
    count = 2

    ephemeral_disk {
      size   = 1000
      sticky = true
    }

    network {
      port "http" {}
      port "grpc" {}
    }

    task "write" {
      driver = "docker"
      user   = "nobody"

      config {
        image = "grafana/loki:${var.version}"

        ports = [
          "http",
          "grpc",
        ]

        args = [
          "-target=write",
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
        name = "loki-write"
        port = "http"

        tags = [
          "traefik.enable=true",
          "traefik.http.routers.loki-write.entrypoints=https",
          "traefik.http.routers.loki-write.rule=Host(`loki-write.service.consul`)",
        ]

        check {
          name     = "Loki write"
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
        memory = 256
      }
    }
  }
}
