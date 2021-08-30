---
title: Lambda Promtail
---
# Lambda Promtail

Loki includes an [AWS SAM](https://aws.amazon.com/serverless/sam/) package template for shipping Cloudwatch logs to Loki via a [set of Promtails](https://github.com/grafana/loki/tree/master/tools/lambda-promtail). This is done via an intermediary [lambda function](https://aws.amazon.com/lambda/) which processes cloudwatch events and propagates them to a Promtail instance (or set of instances behind a load balancer) via the push-api [scrape config](../promtail/configuration#loki_push_api_config).

## Uses

### Ephemeral Jobs

This workflow is intended to be an effective approach for monitoring ephemeral jobs such as those run on AWS Lambda which are otherwise hard/impossible to monitor via one of the other Loki [clients](../).

Ephemeral jobs can quite easily run afoul of cardinality best practices. During high request load, an AWS lambda function might balloon in concurrency, creating many log streams in Cloudwatch. However, these may only be active for a very short while. This creates a problem for combining these short-lived log streams in Loki because timestamps may not strictly increase across multiple log streams. The other obvious route is creating labels based on log streams, which is also undesirable because it leads to cardinality problems via many low-throughput log streams.

Instead we can pipeline Cloudwatch logs to a set of Promtails, which can mitigate these problem in two ways:

1) Using Promtail's push api along with the `use_incoming_timestamp: false` config, we let Promtail determine the timestamp based on when it ingests the logs, not the timestamp assigned by cloudwatch. Obviously, this means that we lose the origin timestamp because Promtail now assigns it, but this is a relatively small difference in a real time ingestion system like this.
2) In conjunction with (1), Promtail can coalesce logs across  Cloudwatch log streams because it's no longer susceptible to out-of-order errors when combining multiple sources (lambda invocations).

One important aspect to keep in mind when running with a set of Promtails behind a load balancer is that we're effectively moving the cardinality problems from the  number of log streams -> number of Promtails. If you have not configured Loki to [accept out-of-order writes](../../configuration#accept-out-of-order-writes), you'll need to assign a Promtail-specific label on each Promtail so that you don't run into out-of-order errors when the Promtails send data for the same log groups to Loki. This can easily be done via a configuration like `--client.external-labels=promtail=${HOSTNAME}` passed to Promtail.

### Proof of concept Loki deployments

For those using Cloudwatch and wishing to test out Loki in a low-risk way, this workflow allows piping Cloudwatch logs to Loki regardless of the event source (EC2, Kubernetes, Lambda, ECS, etc) without setting up a set of Promtail daemons across their infrastructure. However, running Promtail as a daemon on your infrastructure is the best-practice deployment strategy in the long term for flexibility, reliability, performance, and cost.

Note: Propagating logs from Cloudwatch to Loki means you'll still need to _pay_ for Cloudwatch.

## Propagated Labels

Incoming logs will have three special labels assigned to them which can be used in [relabeling](../promtail/configuration/#relabel_config) or later stages in a Promtail [pipeline](../promtail/pipelines/):

- `__aws_cloudwatch_log_group`: The associated Cloudwatch Log Group for this log.
- `__aws_cloudwatch_log_stream`: The associated Cloudwatch Log Stream for this log.
- `__aws_cloudwatch_owner`: The AWS ID of the owner of this event.

## Limitations

### Promtail labels

As stated earlier, this workflow moves the worst case stream cardinality from `number_of_log_streams` -> `number_of_log_groups` * `number_of_promtails`. For this reason, each Promtail must have a unique label attached to logs it processes (ideally via something like `--client.external-labels=promtail=${HOSTNAME}`) and it's advised to run a small number of Promtails behind a load balancer according to your throughput and redundancy needs. 

This trade-off is very effective when you have a large number of log streams but want to aggregate them by the log group. This is very common in AWS Lambda, where log groups are the "application" and log streams are the individual application containers which are spun up and down at a whim, possibly just for a single function invocation.

### Data Persistence

#### Availability

For availability concerns, run a set of Promtails behind a load balancer.

#### Batching

Since Promtail batches writes to Loki for performance, it's possible that Promtail will receive a log, issue a successful `204` http status code for the write, then be killed at a later time before it writes upstream to Loki. This should be rare, but is a downside this workflow has.

### Templating

The current SAM template is rudimentary. If you need to add vpc configs, extra log groups to monitor, subnet declarations, etc, you'll need to edit the template manually. Currently this requires pulling the Loki source.

## Example Promtail Config

Note: this should be run in conjunction with a Promtail-specific label attached, ideally via a flag argument like `--client.external-labels=promtail=${HOSTNAME}`. It will receive writes via the push-api on ports `3500` (http) and `3600` (grpc).

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://ip_or_hostname_where_Loki_run:3100/loki/api/v1/push

scrape_configs:
  - job_name: push1
    loki_push_api:
      server:
        http_listen_port: 3500
        grpc_listen_port: 3600
      labels:
        # Adds a label on all streams indicating it was processed by the lambda-promtail workflow.
        promtail: 'lambda-promtail'
      relabel_configs:
        # Maps the cloudwatch log group into a label called `log_group` for use in Loki.
        - source_labels: ['__aws_cloudwatch_log_group']
          target_label: 'log_group'
```
