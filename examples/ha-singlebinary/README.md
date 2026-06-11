# High Available Single Binary

High available single binary is a replacement for the Simple Scalable Deployment (SSD).
SSD has been deprecated and will be removed with the release of Loki 4.0.

This example demonstrates how to run a number of single binary Loki instances (`-target=all`) behind a reverse proxy (NGINX) for high availability and durability. The demo uses Docker compose, but the mechanics can be applied to any other deployment model, such as Helm or Tanka.

This folder contains a `docker-compose.yaml` and a `config` directory with configuration files for Loki and NGIX.

```console
$ tree
.
├── config
│   ├── loki.yaml
│   └── nginx.conf
└── docker-compose.yaml
```

## Usage

To use the latest features of Loki, you need to build the Docker image from main locally.

```bash
make loki-image
```

Then replace the `image` in `docker-compose.yaml`.

```bash
docker compose up
```

## Architecture

```mermaid
graph TB
    Client(["Client"])
    nginx["Load Balancer\n:3100"]
    subgraph ring ["Ring (RF=3)"]
        subgraph loki1_container ["loki-1"]
            loki1["-target=all\n:3100\n:9095\n:7946"]
            loki1 --- loki1_vol[("local disk")]
        end
        subgraph loki2_container ["loki-2"]
            loki2["-target=all\n:3100\n:9095\n:7946"]
            loki2 --- loki2_vol[("local disk")]
        end
        subgraph loki3_container ["loki-3"]
            loki3["-target=all\n:3100\n:9095\n:7946"]
            loki3 --- loki3_vol[("local disk")]
        end
        subgraph loki4_container ["loki-4"]
            loki4["-target=all\n:3100\n:9095\n:7946"]
            loki4 --- loki4_vol[("local disk")]
        end
        subgraph loki5_container ["loki-5"]
            loki5["-target=all\n:3100\n:9095\n:7946"]
            loki5 --- loki5_vol[("local disk")]
        end
    end


    objstore[("Object Storage\n(S3, GCS, Azure Blob, ...)")]

    Client -->|"HTTP :3100"| nginx

    nginx ---> loki1
    nginx ---> loki2
    nginx ---> loki3
    nginx ---> loki4
    nginx ---> loki5

    loki1 & loki2 & loki3 & loki4 & loki5 -----|"write/read"| objstore
```
