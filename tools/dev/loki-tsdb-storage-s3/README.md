# Loki Docker-Compose

This folder contains the necessary to run the Loki distributed setup locally on docker-compose.

It runs the current code base in the repository this means you can debug new features and iterate very quickly by simply restarting the compose project.

To start the stack simply run:

```bash
./tools/dev/loki-tsdb-storage-s3/compose-up.sh
```

You can then access grafana locally with http://localhost:3000 (default account admin/admin). The grafana container should already have the datasource to correctly query the frontend.

To tear it down use:

```bash
./tools/dev/loki-tsdb-storage-s3/compose-down.sh
```

> On MacOS :apple: docker can get stuck when restarting the stack, you can restart docker to workaround the problem :shrug:

## Configuration

The configuration of the stack is in the [`config/`](./config/) folder.
The stack currently runs boltdb-shipper storage on minio (via s3 compatible API). All containers data are stored under `.data-.*` if you need it for debugging purpose.

Logs from all containers are ingested via the [loki docker driver](https://grafana.com/docs/loki/latest/clients/docker-driver/).

## Remote Debugging

You can also remote debug all Loki containers which are built in debug mode. Each container automatically runs [`dlv`](https://github.com/go-delve/delve) headless and then start Loki process.
You'll need to grab the `dlv` port from the [docker-compose file](./docker-compose.yml) for the container you want to debug. (a different one is used per container.)

### dlv

For example, if command line tooling is your thing, you can debug `ingester-1` with dlv by simply running:

```bash
dlv connect 127.0.0.1:18002
```

### vs-code

If you use vs-code, you can add this snippet bellow in your [`launch.json`](https://code.visualstudio.com/docs/editor/debugging) file:

```json
{
    "name": "Launch Loki remote",
    "type": "go",
    "request": "attach",
    "mode": "remote",
    "substitutePath": [
        {
            "from": "${workspaceFolder}",
            "to": "${workspaceFolder}"
        }
    ],
    "port": 18002,
    "host": "127.0.0.1",
    "cwd": "${workspaceFolder}/tools/dev/loki-tsdb-storage-s3/loki",
    "remotePath": "/loki/loki",
    "showLog": true,
    "trace": "log",
    "logOutput": "rpc"
}
```

Then you can debug `ingester-1` with the `Launch Loki remote` configuration within the debugging tab.

### GoLand IDE

If you use the [GoLand](https://www.jetbrains.com/go/) IDE, just create a Go remote debug configuration and use the appropriate port with the process you wish to debug.