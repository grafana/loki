---
title: Run the Promtail client on AWS EC2
menuTitle:  Promtail on EC2
description: Tutorial for running Promtail client on AWS EC2
aliases: 
- ../../../clients/aws/ec2/
weight: 100
---

# Run the Promtail client on AWS EC2

In this tutorial we're going to setup [Promtail]({{< relref "../../../../send-data/promtail" >}}) on an AWS EC2 instance and configure it to sends all its logs to a Grafana Loki instance.

## Requirements

Before you start you'll need:

- An AWS account (with the `AWS_ACCESS_KEY` and `AWS_SECRET_KEY`)
- A VPC that is routable from the internet. (Follow those [instructions][create an vpc] if you need to create one)
- A SSH public key. (Follow those [instructions][create an ssh key] if you need a new one)
- The [AWS CLI][aws cli] configured (run `aws configure`).
- A Grafana instance with a Loki data source already configured, you can use [GrafanaCloud][GrafanaCloud] free trial.

For the sake of simplicity we'll use a Grafana Cloud Loki and Grafana instances, you can get a free account for this tutorial at [Grafana Cloud], but all the steps are the same if you're running your own Open Source version of Loki and Grafana instances.

To make it easy to learn all the following instructions are manual, however in a real setup we recommend you to use provisioning tools such as [Terraform][terraform], [CloudFormation][cloud formation], [Ansible][ansible] or [Chef][chef].

## Creating an EC2 instance

As a first step we're going to import our SSH key to AWS so that we can SSH to our future EC2 instance, let's run our first command:

```bash
aws ec2 import-key-pair --key-name "promtail-ec2" --public-key-material fileb://~/.ssh/id_rsa.pub
```

Next we're going to create a [security group][security group], make sure to note the group id, we'll need it for the following command:

```bash
aws ec2 create-security-group --group-name promtail-ec2  --description "promtail on ec2" --vpc-id vpc-668d120f
{
    "GroupId": "sg-02c489bbdeffdca1d"
}
```

Now let's authorize inbound access for SSH and [Promtail]({{< relref "../../../../send-data/promtail" >}}) server:

```bash
aws ec2 authorize-security-group-ingress --group-id sg-02c489bbdeffdca1d --protocol tcp --port 22 --cidr 0.0.0.0/0
aws ec2 authorize-security-group-ingress --group-id sg-02c489bbdeffdca1d --protocol tcp --port 3100 --cidr 0.0.0.0/0
```

{{< admonition type="note" >}}
You don't need to open those ports to all IPs as shown above you can use your own IP range.
{{< /admonition >}}

We're going to create an [Amazon Linux 2][Amazon Linux 2] instance as this is one of the most popular but feel free to use the AMI of your choice.

To create the instance use the following command, make sure to note the instance id:

```bash
aws ec2 run-instances --image-id ami-016b213e65284e9c9 --count 1 --instance-type t2.micro --key-name promtail-ec2 --security-groups promtail-ec2
```

To make it more interesting later let's tag (`Name=promtail-demo`) our instance:

```bash
aws ec2 create-tags --resources i-041b0be05c2d5cfad --tags Key=Name,Value=promtail-demo
```

{{< admonition type="note" >}}
Tags enable you to categorize your AWS resources in different ways, for example, by purpose, owner, or environment. This is useful when you have many resources of the same type—you can quickly identify a specific resource based on the tags that you've assigned to it. You'll see later, Promtail can transform those tags into [Loki labels][labels].
{{< /admonition >}}

Finally let's grab the public DNS of our instance:

```bash
aws ec2 describe-instances --filters "Name=tag:Name,Values=promtail-demo" --query "Reservations[].Instances[].NetworkInterfaces[].Association.PublicDnsName"
```

and start an SSH session:

```bash
ssh ec2-user@ec2-13-59-62-37.us-east-2.compute.amazonaws.com
```

## Setting up Promtail

First let's make sure we're running as root by using `sudo -s`.
Next we'll download, install and give executable right to [Promtail]({{< relref "../../../../send-data/promtail" >}}).

```bash
mkdir /opt/promtail && cd /opt/promtail
curl -O -L "https://github.com/grafana/loki/releases/download/v2.0.0/promtail-linux-amd64.zip"
unzip "promtail-linux-amd64.zip"
chmod a+x "promtail-linux-amd64"
```

Now we're going to download the [Promtail configuration]({{< relref "../../../../send-data/promtail" >}}) file below and edit it, don't worry we will explain what those means.
The file is also available as a gist at [cyriltovena/promtail-ec2.yaml][config gist].

```bash
curl https://raw.githubusercontent.com/grafana/loki/main/docs/sources/send-data/promtail/cloud/ec2/promtail-ec2.yaml > ec2-promtail.yaml
vi ec2-promtail.yaml
```

```yaml
server:
  http_listen_port: 3100
  grpc_listen_port: 0

clients:
  - url: https://<user id>:<api secret>@logs-prod-us-central1.grafana.net/loki/api/v1/push

positions:
  filename: /opt/promtail/positions.yaml

scrape_configs:
  - job_name: ec2-logs
    ec2_sd_configs:
      - region: us-east-2
        access_key: REDACTED
        secret_key: REDACTED
    relabel_configs:
      - source_labels: [__meta_ec2_tag_Name]
        target_label: name
        action: replace
      - source_labels: [__meta_ec2_instance_id]
        target_label: instance
        action: replace
      - source_labels: [__meta_ec2_availability_zone]
        target_label: zone
        action: replace
      - action: replace
        replacement: /var/log/**.log
        target_label: __path__
      - source_labels: [__meta_ec2_private_dns_name]
        regex: "(.*)"
        target_label: __host__
```

The **server** section indicates Promtail to bind his http server to 3100. Promtail serves HTTP pages for [troubleshooting]({{< relref "../../../../send-data/promtail/troubleshooting" >}}) service discovery and targets.

The **clients** section allow you to target your loki instance, if you're using GrafanaCloud simply replace `<user id>` and `<api secret>` with your credentials. Otherwise just replace the whole URL with your custom Loki instance.(e.g `http://my-loki-instance.my-org.com/loki/api/v1/push`)

[Promtail]({{< relref "../../../../send-data/promtail" >}}) uses the same [Prometheus **scrape_configs**][prometheus scrape config]. This means if you already own a Prometheus instance the config will be very similar and easy to grasp.

Since we're running on AWS EC2 we want to uses EC2 service discovery, this will allows us to scrape metadata about the current instance (and even your custom tags) and attach those to our logs. This way managing and querying on logs will be much easier.

Make sure to replace accordingly you current `region`, `access_key` and `secret_key`, alternatively you can use an [AWS Role][role] ARN, for more information about this, see documentation for [`ec2_sd_config`][ec2_sd_config].

Finally the [`relabeling_configs`][relabel] section has three purposes:

1. Selecting the labels discovered you want to attach to your targets. In our case here, we're keeping `instance_id` as instance, the tag `Name` as name and the `zone` of the instance. Make sure to check out the Prometheus [`ec2_sd_config`][ec2_sd_config] documentation for the full list of available labels.

2. Choosing where Promtail should find log files to tail, in our example we want to include all log files that exist in `/var/log` using the glob `/var/log/**.log`. If you need to use multiple glob, you can simply add another job in your `scrape_configs`.

3. Ensuring discovered targets are only for the machine Promtail currently runs on. This is achieved by adding the label `__host__` using the incoming metadata `__meta_ec2_private_dns_name`. If it doesn't match the current `HOSTNAME` environment variable, the target will be dropped.
If `__meta_ec2_private_dns_name` doesn't match your instance's hostname (on EC2 Windows instance for example, where it is the IP address and not the hostname), you can hardcode the hostname at this stage, or check if any of the instances tag contain the hostname (`__meta_ec2_tag_<tagkey>: each tag value of the instance`)

Alright we should be ready to fire up Promtail, we're going to run it using the flag `--dry-run`. This is perfect to ensure everything is correctly, specially when you're still playing around with the configuration. Don't worry when using this mode, Promtail won't send any logs and won't remember any file positions.

```bash
 ./promtail-linux-amd64 -config.file=./ec2-promtail.yaml --dry-run
```

If everything is going well Promtail should print out log lines with their labels discovered instead of sending them to Loki, like shown below:

```bash
2020-07-08T14:51:38-0700	{filename="/var/log/cloud-init.log", instance="i-041b0be05c2d5cfad", name="promtail-demo", zone="us-east-2c"}	Jul 07 21:37:24 cloud-init[3035]: util.py[DEBUG]: loaded blob returned None, returning default.
```

Don't hesitate to edit the your config file and start Promtail again to try your config out.

If you want to see existing targets and available labels you can reach Promtail server using the public dns assigned to your instance:

```bash
open http://ec2-13-59-62-37.us-east-2.compute.amazonaws.com:3100/
```

For example the page below is the service discovery page. It shows you all discovered targets, with their respective available labels and the reason it was dropped if it was the case.

{{< figure alt="Service discovery page" align="center" src="./promtail-ec2-discovery.png" >}}

This page is really useful to understand what labels are available to forward with the `relabeling` configuration but also why Promtail is not scraping your target.

## Configuring Promtail as a service

Now that we have correctly configured Promtail. We usually want to make sure it runs as a [systemd service][systemd], so it can automatically restart on failure or when the instance restart.

Let's create a new service using `vim /etc/systemd/system/promtail.service` and copy the service definition below:

```systemd
[Unit]
Description=Promtail

[Service]
User=root
WorkingDirectory=/opt/promtail/
ExecStartPre=/bin/sleep 30
ExecStart=/opt/promtail/promtail-linux-amd64 --config.file=./ec2-promtail.yaml
SuccessExitStatus=143
TimeoutStopSec=10
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Let's reload the systemd, enable then start the Promtail service:

```bash
systemctl daemon-reload
systemctl enable promtail.service
systemctl start promtail.service
```

You can verify that the service run correctly using the following command:

```bash
systemctl status promtail.service -l

● promtail.service - Promtail
   Loaded: loaded (/etc/systemd/system/promtail.service; enabled; vendor preset: disabled)
   Active: active (running) since Wed 2020-07-08 15:48:57 UTC; 4s ago
 Main PID: 2732 (promtail-linux-)
   CGroup: /system.slice/promtail.service
           └─2732 /opt/promtail/promtail-linux-amd64 --config.file=./ec2-promtail.yaml

Jul 08 15:48:57 ip-172-31-45-69.us-east-2.compute.internal systemd[1]: Started Promtail.
Jul 08 15:48:57 ip-172-31-45-69.us-east-2.compute.internal systemd[1]: Starting Promtail...
Jul 08 15:48:57 ip-172-31-45-69.us-east-2.compute.internal promtail-linux-amd64[2732]: level=warn ts=2020-07-08T15:48:57.559085451Z caller=filetargetmanager.go:98 msg="WARNING!!! entry_parser config is deprecated, please change to pipeline_stages"
Jul 08 15:48:57 ip-172-31-45-69.us-east-2.compute.internal promtail-linux-amd64[2732]: level=info ts=2020-07-08T15:48:57.559869071Z caller=server.go:179 http=[::]:3100 grpc=[::]:35127 msg="server listening on addresses"
Jul 08 15:48:57 ip-172-31-45-69.us-east-2.compute.internal promtail-linux-amd64[2732]: level=info ts=2020-07-08T15:48:57.56029474Z caller=main.go:67 msg="Starting Promtail" version="(version=1.6.0, branch=HEAD, revision=12c7eab8)"
```

You can now verify in Grafana that Loki has correctly received your instance logs by using the [LogQL]({{< relref "../../../../query" >}}) query `{zone="us-east-2"}`.

{{< figure alt="Grafana Loki logs" align="center" src="./promtail-ec2-logs.png" >}}

## Sending systemd logs

Just like we did with Promtail, you'll most likely manage your applications with [systemd][systemd] which usually store applications logs in [journald][journald]. Promtail actually support scraping logs from [journald][journald] so let's configure it.

We will edit our previous config (`vi ec2-promtail.yaml`) and add the following block in the `scrape_configs` section.

```yaml
  - job_name: journal
    journal:
      json: false
      max_age: 12h
      path: /var/log/journal
      labels:
        job: systemd-journal
    relabel_configs:
      - source_labels: ['__journal__systemd_unit']
        target_label: 'unit'
```

Note that you can use [relabeling][relabeling] to convert systemd labels to match what you want. Finally make sure that the path of journald logs is correct, it might be different on some systems.

{{< admonition type="note" >}}
You can download the final config example from our [GitHub repository][final config].
{{< /admonition >}}

That's it, save the config and you can `reboot` the machine (or simply restart the service `systemctl restart promtail.service`).

Let's head back to Grafana and verify that your Promtail logs are available in Grafana by using the [LogQL]({{< relref "../../../../query" >}}) query `{unit="promtail.service"}` in Explore. Finally make sure to checkout [live tailing][live tailing] to see logs appearing as they are ingested in Loki.

[promtail]: ../../promtail/README
[aws cli]: https://aws.amazon.com/cli/
[create an vpc]: https://docs.aws.amazon.com/vpc/latest/userguide/vpc-subnets-commands-example.html
[create an ssh key]: https://confluence.atlassian.com/bitbucketserver/creating-ssh-keys-776639788.html
[GrafanaCloud]: https://grafana.com/signup/
[terraform]: https://www.terraform.io/
[cloud formation]: https://aws.amazon.com/fr/cloudformation/
[ansible]: https://www.ansible.com/
[chef]: https://www.chef.io/
[security group]: https://docs.aws.amazon.com/vpc/latest/userguide/VPC_SecurityGroups.html
[Amazon Linux 2]: https://aws.amazon.com/amazon-linux-2/
[promtail configuration]: ../../promtail/configuration
[prometheus scrape config]: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config
[ec2_sd_config]: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#ec2_sd_config
[role]: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles.html
[relabel]: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
[systemd]: https://www.freedesktop.org/software/systemd/man/systemd.service.html
[logql]: ../../../query
[config gist]: https://gist.github.com/cyriltovena/d0881cc717757db951b642be48c01445
[labels]: https://grafana.com/blog/2020/04/21/how-labels-in-loki-can-make-log-queries-faster-and-easier/
[live tailing]: https://grafana.com/docs/grafana/latest/features/datasources/loki/#live-tailing
[systemd]: ../../../installation/helm#run-promtail-with-systemd-journal-support
[journald]: https://www.freedesktop.org/software/systemd/man/systemd-journald.service.html
[final config]: https://github.com/grafana/loki/blob/main/docs/sources/send-data/promtail/cloud/ec2/promtail-ec2-final.yaml
[relabeling]: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
