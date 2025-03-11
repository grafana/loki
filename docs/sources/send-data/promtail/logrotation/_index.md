---
title: Promtail and Log Rotation 
menuTitle:  Configure log rotation
description: Configuring Promtail log rotation.
aliases: 
- ../../clients/promtail/logrotation/
weight:  500
---

# Promtail and Log Rotation

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

## Why does log rotation matter?

At any point in time, there may be three processes working on a log file as shown in the image below.

{{< figure alt="block diagram showing three processes" align="center" src="./logrotation-components.png" >}}

1. Appender - A writer that keeps appending to a log file. This can be your application or some system daemons like Syslog, Docker log driver or Kubelet, etc.
2. Tailer - A reader that reads log lines as they are appended, for example, agents like Promtail.
3. Log Rotator - A process that rotates the log file either based on time (for example, scheduled every day) or size (for example, a log file reached its maximum size).

{{< admonition type="note" >}}
Here `fd` defines a file descriptor. Once a file is open for read or write, The Operating System returns a unique file descriptor (usually an integer) per process, and all the operations like read and write are done over that file descriptor. In other words, once the file is opened successfully, the file descriptor matters more than the file name.
{{< /admonition >}}

One of the critical components here is the log rotator. Let's understand how it impacts other components like the appender and tailer.

In general, when a software program rotates a log, it can be done in two ways internally.

Given a log file named `error.log`

1. Copy the log file with a different name, for example, `error.log.1` and truncate the original log file `error.log`.
2. Rename the log file with a different name, for example,  `error.log.1` and create a new log file with the original name `error.log`.

In both cases, after log rotation, all new log lines are written to the original `error.log` file.

These two methods of log rotation are shown in the following images.

### Copy and Truncate

{{< figure alt="Copy and truncate log rotation" align="center" src="./logrotation-copy-and-truncate.png" >}}

### Rename and Create

{{< figure alt="Rename and create log rotation" align="center" src="./logrotation-rename-and-create.png" >}}

Both types of log rotation produce the same result. However, there are some subtle differences.

Copy and Truncate(1) favors the `appender` as its descriptor for the original log file `error.log` doesn't change. Therefore, it can keep writing to the same file descriptor. In other words, re-opening the file is not needed.

However, (1) has a serious problem when considering the `tailer`. There is a race between truncating the file and the `tailer` finishing reading that log file. Meaning, there is a high chance of the log rotation mechanism truncating the file `error.log` before the `tailer` reads everything from it.

This is where Rename and Create(2) can help. Here, when the log file `error.log` is renamed to `error.log.1`, the `tailer` still holds the file descriptor of `error.log.1`. Therefore, it can continue reading the log file until it is completed. But that comes with the tradeoff: with (2), you have to signal the `appender` to reopen `error.log` (and `appender` should be able to reopen it). Otherwise, it would keep writing to `error.log.1` as the file descriptor won't change. The good news is that most of the popular `appender` solutions (for example, Syslog, Kubelet, Docker log driver) support reopening log files when they are renamed.

We recommend Rename and Create(2) as that is the method which works well with Promtail (or any similar log scraping agent) without any data loss. Now let's understand exactly how we configure log rotation in different platforms.

## Configure log rotation

Your logs can be rotated by different components depending on where you are running your applications or services. If you are running them on Linux machines, there is a high chance you are using the [`logrotate`](https://man7.org/linux/man-pages/man8/logrotate.8.html) utility. However, if you are running in Kubernetes, it's not that obvious what rotates the logs and interestingly it may depend on what container runtime your Kubernetes cluster is using.

### Non-Kubernetes

As mentioned above, in Linux machines, log rotation is often handled by the [`logrotate`](https://man7.org/linux/man-pages/man8/logrotate.8.html) utility.

The configuration for `logrotate` is usually located in `/etc/logrotate/`.

It has a wide range of [options](https://man7.org/linux/man-pages/man8/logrotate.8.html) for compression, mailing, running scripts pre- and post-rotation, etc.

It supports both methods of log rotation described previously.

#### Copy and Truncate

```
/var/log/apache2/*.log {
        weekly
        maxsize 1G
        copytruncate
}
```

Here `copytruncate` mode works exactly like (1) explained above.

#### Rename and Create **(Recommend)**

```
/var/log/apache2/*.log {
        weekly
        maxsize 1G
        create
}
```

Here, the `create` mode works as explained in (2) above. The `create` mode is optional because it's the default mode in `logrotate`.

### Kubernetes

[Kubernetes Service Discovery in Promtail](../scraping/#kubernetes-discovery) also uses file-based scraping. Meaning, logs from your pods are stored on the nodes and Promtail scrapes the pod logs from the node files.

You can [configure](https://kubernetes.io/docs/concepts/cluster-administration/logging/#log-rotation) the `kubelet` process running on each node to manage log rotation via two configuration settings.

1. `containerLogMaxSize` - It defines the maximum size of the container log file before it is rotated. For example: "5Mi" or "256Ki". Default: "10Mi".
2. `containerLogMaxFiles` - It specifies the maximum number of container log files that can be present per container. Default: 5

Both should be part of the `kubelet` config. If you run a managed version of Kubernetes in Cloud, refer to your cloud provider documentation for configuring `kubelet`. Examples [GKE](https://cloud.google.com/kubernetes-engine/docs/how-to/node-system-config#create), [AKS](https://learn.microsoft.com/en-us/azure/aks/custom-node-configuration#use-custom-node-configuration) and [EKS](https://eksctl.io/usage/customizing-the-kubelet/#customizing-kubelet-configuration).

{{< admonition type="note" >}}
Log rotation managed by `kubelet` supports only rename + create and doesn't support copy + truncate.
{{< /admonition >}}

If `kubelet` is not configured to manage the log rotation, then it's up to the Container Runtime Interface (CRI) the cluster uses. Alternatively, log rotation can be managed by the `logrotate` utility in the Kubernetes node itself.

Check your container runtime (CRI) on your nodes by running:

```bash
$ kubectl get nodes -o wide
```

Two of the commonly used CRI implementations are `containerd` and `docker`.

#### containerd CRI

At the time of writing this guide, `containerd` [doesn't support any method of log rotation](https://github.com/containerd/containerd/issues/4830#issuecomment-744744375). In this case, rotation is often handled by `kubelet` itself. Managed Kubernetes clusters like GKE and AKS use `containerd` as runtime and log rotation is handled by `kubelet`. [EKS after version 1.24](https://docs.aws.amazon.com/eks/latest/userguide/dockershim-deprecation.html) also uses `containerd` as its default container runtime.

#### docker CRI

When using `docker` as runtime (EKS before 1.24 uses it by default), log rotation is managed by its logging driver (if supported). Docker has [support for several logging drivers](https://docs.docker.com/config/containers/logging/configure/#supported-logging-drivers).

You can determine which logging driver `docker` is using by running the following command:

```bash
 docker info --format '{{.LoggingDriver}}'
```

Out of all these logging drivers only the `local` (default) and the `json-file` drivers support log rotation. You can configure the following `log-opts` under `/etc/docker/daemon.json`

1. `max-size` - The maximum size of the log file before it is rotated. A positive integer plus a modifier representing the unit of measure (k, m, or g). Defaults to `20m` (20 megabytes).
2. `max-file` - The maximum number of log files that can be present. If rolling the logs creates excess files, the oldest file is removed. A positive integer. Defaults to 5.

Example `/etc/docker/daemon.json`:

```json
{
  "log-driver": "local",
  "log-opts": {
    "max-size": "10m",
    "max-file": "10"
  }
}
```

If neither `kubelet` nor `CRI` is configured for rotating logs, then the `logrotate` utility can be used on the Kubernetes nodes as explained previously.

{{< admonition type="note" >}}
We recommend using kubelet for log rotation.
{{< /admonition >}}

## Configure Promtail

Promtail uses `polling` to watch for file changes. A `polling` mechanism combined with a [copy and truncate](#copy-and-truncate) log rotation may result in losing some logs. As explained earlier in this topic, this happens when the file is truncated before Promtail reads all the log lines from such a file.

Therefore, for a long-term solution, we strongly recommend changing the log rotation strategy to [rename and create](#rename-and-create). Alternatively, as a workaround in the short term, you can tweak the promtail client's `batchsize` [config](../configuration/#clients) to set higher values (like 5M or 8M). This gives Promtail more room to read loglines without frequently waiting for push responses from the Loki server.
