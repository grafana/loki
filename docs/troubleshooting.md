# Troubleshooting

## "Data source connected, but no labels received. Verify that Loki and Promtail is configured properly."

This error can appear in Grafana when you add Loki as a datasource.
It means that Grafana can connect to Loki, but Loki has not received any logs from promtail.
This can have several reasons:

- Promtail cannot reach Loki, check promtail's output.
- Promtail started sending logs before Loki was ready. This can happen in test environments where promtail already read all logs and sent them off. Here is what you can do:
  - Generally start promtail after Loki, e.g., 60 seconds later.
  - Restarting promtail will not necessarily resend log messages that have been read. To force sending all messages again, delete the positions file (default location `/tmp/positions.yaml`) or make sure new log messages are written after both promtail and Loki have started.

## Failed to create target, "ioutil.ReadDir: readdirent: not a directory"

The promtail configuration contains a `__path__` entry to a directory that promtail cannot find.

## Connecting to a promtail pod to troubleshoot

Say you are missing logs from your nginx pod and want to investigate promtail.

In your cluster if you are running promtail as a daemonset, you will have a promtail pod on each node, to figure out which promtail you want run:


```shell
work-pc:.kube ewelch$ kubectl get pods -o wide
NAME                                   READY   STATUS    RESTARTS   AGE   IP             NODE                                                  NOMINATED NODE
...
nginx-7b6fb56fb8-cw2cm                 1/1     Running   0          41d   10.56.4.12     gke-ops-tools1-gke-u-ops-tools1-gke-u-9d232f9e-ckgc   <none>
...
promtail-bth9q                         1/1     Running   0          3h    10.56.4.217    gke-ops-tools1-gke-u-ops-tools1-gke-u-9d232f9e-ckgc   <none>
```

That output is truncated to highlight just the two pods we are interseted in, you can see with the `-o wide` flag the NODE on which they are running.

You'll want to match the node for the pod you are interested in, in this example nginx, to the promtail running on the same node.

To debug you can connect to the promtail pod:

```shell
kubectl exec -it promtail-bth9q -- /bin/ash
```

Once connected, verify the config in `/etc/promtail/promtail.yml` is what you expected

Also check `/var/log/positions.yaml` and make sure promtail is tailing the logs you would expect

You can check the promtail log by looking in `/var/log/containers` at the promtail container log