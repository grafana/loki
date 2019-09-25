# Troubleshooting Loki

## "Loki: Bad Gateway. 502"

This error can appear in Grafana when Loki is added as a
datasource, indicating that Grafana in unable to connect to Loki. There may
one of many root causes:

- If Loki is deployed with Docker, and Grafana and Loki are not running in the
  same node, check your firewall to make sure the nodes can connect.
- If Loki is deployed with Kubernetes:
    - If Grafana and Loki are in the same namespace, set the Loki URL as
      `http://$LOKI_SERVICE_NAME:$LOKI_PORT`
    - Otherwise, set the Loki URL as
      `http://$LOKI_SERVICE_NAME.$LOKI_NAMESPACE:$LOKI_PORT`

## "Data source connected, but no labels received. Verify that Loki and Promtail is configured properly."

This error can appear in Grafana when Loki is added as a datasource, indicating
that although Grafana has connected to Loki, Loki hasn't received any logs from
Promtail yet. There may be one of many root causes:

- Promtail is running and collecting logs but is unable to connect to Loki to
  send the logs. Check Promtail's output.
- Promtail started sending logs to Loki before Loki was ready. This can
  happen in test environment where Promtail has already read all logs and sent
  them off. Here is what you can do:
    - Start Promtail after Loki, e.g., 60 seconds later.
    - To force Promtail to re-send log messages, delete the positions file
      (default location `/tmp/positions.yaml`).
- Promtail is ignoring targets and isn't reading any logs because of a
  configuration issue.
    - This can be detected by turning on debug logging in Promtail and looking
      for `dropping target, no labels` or `ignoring target` messages.
- Promtail cannot find the location of your log files. Check that the
  `scrape_configs` contains valid path settings for finding the logs on your
  worker nodes.
- Your pods are running with different labels than the ones Promtail is
  configured to read. Check `scrape_configs` to validate.

## Troubleshooting targets

Promtail exposes two web pages that can be used to understand how its service
discovery works.

The service discovery page (`/service-discovery`) shows all
discovered targets with their labels before and after relabeling as well as
the reason why the target has been dropped.

The targets page (`/targets`) displays only targets that are being actively
scraped and their respective labels, files, and positions.

On Kubernetes, you can access those two pages by port-forwarding the Promtail
port (`9080` or `3101` if using Helm) locally:

```bash
$ kubectl port-forward loki-promtail-jrfg7 9080
# Then, in a web browser, visit http://localhost:9080/service-discovery
```

## Debug output

Both `loki` and `promtail` support a log level flag on the command-line:

```bash
$ loki —log.level=debug
$ promtail -log.level=debug
```

## Failed to create target, `ioutil.ReadDir: readdirent: not a directory`

The Promtail configuration contains a `__path__` entry to a directory that
Promtail cannot find.

## Connecting to a Promtail pod to troubleshoot

First check [Troubleshooting targets](#troubleshooting-targets) section above.
If that doesn't help answer your questions, you can connect to the Promtail pod
to investigate further.

If you are running Promtail as a DaemonSet in your cluster, you will have a
Promtail pod on each node, so figure out which Promtail you need to debug first:


```shell
$ kubectl get pods --all-namespaces -o wide
NAME                                   READY   STATUS    RESTARTS   AGE   IP             NODE        NOMINATED NODE
...
nginx-7b6fb56fb8-cw2cm                 1/1     Running   0          41d   10.56.4.12     node-ckgc   <none>
...
promtail-bth9q                         1/1     Running   0          3h    10.56.4.217    node-ckgc   <none>
```

That output is truncated to highlight just the two pods we are interested in,
you can see with the `-o wide` flag the NODE on which they are running.

You'll want to match the node for the pod you are interested in, in this example
NGINX, to the Promtail running on the same node.

To debug you can connect to the Promtail pod:

```shell
kubectl exec -it promtail-bth9q -- /bin/sh
```

Once connected, verify the config in `/etc/promtail/promtail.yml` has the
contents you expect.

Also check `/var/log/positions.yaml` (`/run/promtail/positions.yaml` when
deployed by Helm or whatever value is specified for `positions.file`) and make
sure Promtail is tailing the logs you would expect.

You can check the Promtail log by looking in `/var/log/containers` at the
Promtail container log.

## Enable tracing for Loki

Loki can be traced using [Jaeger](https://www.jaegertracing.io/) by setting
the environment variable `JAEGER_AGENT_HOST` to the hostname and port where
Loki is running.

If you deploy with Helm, use the following command:

```bash
$ helm upgrade --install loki loki/loki --set "loki.tracing.jaegerAgentHost=YOUR_JAEGER_AGENT_HOST"
```
