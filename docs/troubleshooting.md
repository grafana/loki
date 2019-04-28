# Troubleshooting

## "Loki: Bad Gateway. 502"
This error can appear in Grafana when you add Loki as a datasource.
It means that Grafana cannot connect to Loki. This can have several reasons:

- If you deploy in docker env, Grafana and Loki are not in same node, check iptables or firewalls to ensure they can connect.
- If you deploy in kubernetes env, please note:
  - Grafana and Loki are in same namespace, set Loki url as "http://$LOKI_SERVICE_NAME:$LOKI_PORT".
  - Grafana and Loki are in different namespace, set Loki url as "http://$LOKI_SERVICE_NAME.$LOKI_NAMESPACE:$LOKI_PORT".

## "Data source connected, but no labels received. Verify that Loki and Promtail is configured properly."

This error can appear in Grafana when you add Loki as a datasource.
It means that Grafana can connect to Loki, but Loki has not received any logs from promtail.
This can have several reasons:

- Promtail cannot reach Loki, check promtail's output.
- Promtail started sending logs before Loki was ready. This can happen in test environments where promtail already read all logs and sent them off. Here is what you can do:
  - Generally start promtail after Loki, e.g., 60 seconds later.
  - Restarting promtail will not necessarily resend log messages that have been read. To force sending all messages again, delete the positions file (default location `/tmp/positions.yaml`) or make sure new log messages are written after both promtail and Loki have started.
- Promtail is ignoring targets because of a configuration rule
  - Detect this by turning on debug logging and then look for `dropping target, no labels` or `ignoring target` messages.
- Promtail cannot find the location of your log files. Check that the scrape_configs contains valid path setting for finding the logs in your worker nodes.
- Your pods are running but not with the labels Promtail is expecting. Check the Promtail scape_configs.
- Kubernetes 1.14+ and GKE 1.12+: the default scape_configs need to be adapted to work. 
From
```
        - replacement: /var/log/pods/$1/*.log
          separator: /
          source_labels:
          - __meta_kubernetes_pod_uid
          - __meta_kubernetes_pod_container_name
          target_label: __path__
```
to
```
        - replacement: /var/log/pods/*$1*/*/*.log
          source_labels:
          - __meta_kubernetes_pod_uid
          target_label: __path__
```

## Debug output

Both binaries support a log level parameter on the command-line, e.g.: `loki —log.level= debug ...`

## Failed to create target, "ioutil.ReadDir: readdirent: not a directory"

The promtail configuration contains a `__path__` entry to a directory that promtail cannot find.

## Connecting to a promtail pod to troubleshoot

Say you are missing logs from your nginx pod and want to investigate promtail.

In your cluster if you are running promtail as a daemonset, you will have a promtail pod on each node, to figure out which promtail you want run:


```shell
$ kubectl get pods --all-namespaces -o wide
NAME                                   READY   STATUS    RESTARTS   AGE   IP             NODE        NOMINATED NODE
...
nginx-7b6fb56fb8-cw2cm                 1/1     Running   0          41d   10.56.4.12     node-ckgc   <none>
...
promtail-bth9q                         1/1     Running   0          3h    10.56.4.217    node-ckgc   <none>
```

That output is truncated to highlight just the two pods we are interseted in, you can see with the `-o wide` flag the NODE on which they are running.

You'll want to match the node for the pod you are interested in, in this example nginx, to the promtail running on the same node.

To debug you can connect to the promtail pod:

```shell
kubectl exec -it promtail-bth9q -- /bin/ash
```

Once connected, verify the config in `/etc/promtail/promtail.yml` is what you expected

Also check `/var/run/promtail/positions.yaml` and make sure promtail is tailing the logs you would expect

You can check the promtail log by looking in `/var/log/containers` at the promtail container log

## Enable tracing for loki

We support (jaeger)[https://www.jaegertracing.io/] to trace loki, just add env `JAEGER_AGENT_HOST` to where loki run, and you can use jaeger to trace.

If you deploy with helm, refer to following command:

```bash
$ helm upgrade --install loki loki/loki --set "loki.jaegerAgentHost=YOUR_JAEGER_AGENT_HOST"
```
