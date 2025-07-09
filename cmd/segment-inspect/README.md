# Tools for inspecting Loki chunks

This tool can parse Loki WAL segments and print details from them. Useful for Loki developers.

To build the tool, simply run `go build` in this directory. Running resulting program with chunks file name gives you some basic chunks information:

```shell
$ ./segment-inspect 01J2F010G82AWFGAEC3WGKC84P

Segment file: 01J2F010G82AWFGAEC3WGKC84P
Compressed Filesize: 2.0 MB
Series Size: 1.8 MB
Index Size: 255 kB
Stream count: 865
Tenant count: 7
From: 2024-07-10 18:55:47.006697 UTC
To: 2024-07-10 18:55:51.454554 UTC
Duration: 4.447857915s
```

To print the individual streams contained in this segment, use `-s` parameter:

```shell script
$ ./segment-inspect -s 01J2F010G82AWFGAEC3WGKC84P

... segment file info, see above ...

{__loki_tenant__="29", cluster="dev-us-central-0", container="loki-canary", controller_revision_hash="9cbf8c4bb", job="promtail-ops/loki-canary", name="loki-canary", namespace="promtail-ops", pod="loki-canary-z6jrt", pod_template_generation="2008", service_name="loki/loki-canary", stream="stdout"}
{__loki_tenant__="29", cluster="dev-us-central-0", container="loki-canary", controller_revision_hash="9cbf8c4bb", job="promtail-ops/loki-canary", name="loki-canary", namespace="promtail-ops", pod="loki-canary-zpx5v", pod_template_generation="2008", service_name="loki/loki-canary", stream="stdout"}
{__loki_tenant__="29", cluster="dev-us-central-0", container="metadata-forwarder", job="metadata-forwarder/metadata-forwarder", name="metadata-forwarder", namespace="metadata-forwarder", pod="metadata-forwarder-676f57978c-dpd2s", pod_template_hash="676f57978c", service_name="metadata-forwarder", stream="stderr"}
{__loki_tenant__="29", cluster="dev-us-central-0", container="metadata-forwarder", job="metadata-forwarder/metadata-forwarder", name="metadata-forwarder", namespace="metadata-forwarder", pod="metadata-forwarder-676f57978c-sbnc9", pod_template_hash="676f57978c", service_name="metadata-forwarder", stream="stderr"}
{__loki_tenant__="29", cluster="dev-us-central-0", container="mimir-read", gossip_ring_member="true", job="mimir-dev-09/mimir-read", name="mimir-read", namespace="mimir-dev-09", pod="mimir-read-74d58cd64c-4kg55", pod_template_hash="74d58cd64c", service_name="mimir/mimir-read", stream="stderr"}
... and so on ...
```
