---
title: Lambda Promtail client
menuTitle:  Lambda Promtail
description: Configuring the Lambda Promtail client to send logs to Loki.
aliases:
- ../clients/lambda-promtail/
weight:  700
---

# Lambda Promtail client

Grafana Loki includes [Terraform](https://www.terraform.io/) and [CloudFormation](https://aws.amazon.com/cloudformation/) for shipping Cloudwatch, Cloudtrail, VPC Flow Logs and loadbalancer logs to Loki via a [lambda function](https://aws.amazon.com/lambda/). This is done via [lambda-promtail](https://github.com/grafana/loki/blob/main/tools/lambda-promtail) which processes cloudwatch events and propagates them to Loki (or a Promtail instance) via the push-api [scrape config]({{< relref "../../send-data/promtail/configuration#loki_push_api" >}}).

## Deployment

lambda-promtail can easily be deployed via provided [Terraform](https://github.com/grafana/loki/blob/main/tools/lambda-promtail/main.tf) and [CloudFormation](https://github.com/grafana/loki/blob/main/tools/lambda-promtail/template.yaml) files. The Terraform deployment also pulls variable values defined from [variables.tf](https://github.com/grafana/loki/blob/main/tools/lambda-promtail/variables.tf).

For both deployment types there are a few values that must be defined:
- the write address, a Loki Write API compatible endpoint (Loki or Promtail)
- basic auth username/password if the write address is a Loki endpoint and has authentication
- the lambda-promtail image, full ECR repo path:tag

The Terraform deployment also takes in an array of log group and bucket names, and can take arrays for VPC subnets and security groups.

There's also a flag to keep the log stream label when propagating the logs from Cloudwatch, which defaults to false. This can be helpful when the cardinality is too large, such as the case of a log stream per lambda invocation.

Additionally, an environment variable can be configured to add extra labels to the logs streamed by lambda-protmail.
These extra labels will take the form `__extra_<name>=<value>`.

An optional environment variable can be configured to add the tenant ID to the logs streamed by lambda-protmail.

In an effort to make deployment of lambda-promtail as simple as possible, we've created a [public ECR repo](https://gallery.ecr.aws/grafana/lambda-promtail) to publish our builds of lambda-promtail. Users may clone this repo, make their own modifications to the Go code, and upload their own image to their own ECR repo.

### Examples

Terraform:
```
## use cloudwatch log group
terraform apply -var "lambda_promtail_image=<repo:tag>" -var "write_address=https://logs-prod-us-central1.grafana.net/loki/api/v1/push" -var "password=<password>" -var "username=<user>" -var 'log_group_names=["/aws/lambda/log-group-1", "/aws/lambda/log-group-2"]' -var 'bucket_names=["bucket-a", "bucket-b"]' -var 'batch_size=131072'
```

```
## use kinesis data stream
terraform apply -var "<ecr-repo>:<tag>" -var "write_address=https://your-loki-url/loki/api/v1/push" -var "password=<basic-auth-pw>" -var "username=<basic-auth-username>" -var 'kinesis_stream_name=["kinesis-stream-01", "kinesis-stream-02"]' -var 'extra_labels="name1,value1,name2,value2"' -var "tenant_id=<value>"
```

The first few lines of `main.tf` define the AWS region to deploy to.
Modify as desired, or remove and deploy to
```
provider "aws" {
  region = "us-east-2"
}
```

To keep the log group label add `-var "keep_stream=true"`.

To add extra labels add `-var 'extra_labels="name1,value1,name2,value2"'`.

To add tenant id add `-var "tenant_id=value"`.

Note that the creation of a subscription filter on Cloudwatch in the provided Terraform file only accepts an array of log group names.
It does **not** accept strings for regex filtering on the logs contents via the subscription filters. We suggest extending the Terraform file to do so.
Or, have lambda-promtail write to Promtail and use [pipeline stages](/docs/loki/<LOKI_VERSION>/send-data/promtail/stages/drop/).

CloudFormation:
```
aws cloudformation create-stack --stack-name lambda-promtail --template-body file://template.yaml --capabilities CAPABILITY_IAM CAPABILITY_NAMED_IAM --region us-east-2 --parameters ParameterKey=WriteAddress,ParameterValue=https://logs-prod-us-central1.grafana.net/loki/api/v1/push ParameterKey=Username,ParameterValue=<user> ParameterKey=Password,ParameterValue=<password> ParameterKey=LambdaPromtailImage,ParameterValue=<repo:tag>
```

Within the CloudFormation template file, copy, paste, and modify the subscription filter section as needed for each log group:
```
MainLambdaPromtailSubscriptionFilter:
  Type: AWS::Logs::SubscriptionFilter
  Properties:
    DestinationArn: !GetAtt LambdaPromtailFunction.Arn
    FilterPattern: ""
    LogGroupName: "/aws/lambda/some-lamda-log-group"
```

To keep the log group label, add `ParameterKey=KeepStream,ParameterValue=true`.

To add extra labels, include `ParameterKey=ExtraLabels,ParameterValue="name1,value1,name2,value2"`.

To add a tenant ID, add `ParameterKey=TenantID,ParameterValue=value`.

To modify an existing CloudFormation stack, use [update-stack](https://docs.aws.amazon.com/cli/latest/reference/cloudformation/update-stack.html).

If using CloudFormation to write your infrastructure code, you should consider the [EventBridge based solution](#s3-based-logging-and-cloudformation) for easier deployment.

## Uses

### Ephemeral Jobs

This workflow is intended to be an effective approach for monitoring ephemeral jobs such as those run on AWS Lambda which are otherwise hard/impossible to monitor via one of the other Loki [clients]({{< relref ".." >}}).

Ephemeral jobs can quite easily run afoul of cardinality best practices. During high request load, an AWS lambda function might balloon in concurrency, creating many log streams in Cloudwatch. For this reason lambda-promtail defaults to **not** keeping the log stream value as a label when propagating the logs to Loki. This is only possible because new versions of Loki no longer have an ingestion ordering constraint on logs within a single stream.

### Proof of concept Loki deployments

For those using Cloudwatch and wishing to test out Loki in a low-risk way, this workflow allows piping Cloudwatch logs to Loki regardless of the event source (EC2, Kubernetes, Lambda, ECS, etc) without setting up a set of Promtail daemons across their infrastructure. However, running Promtail as a daemon on your infrastructure is the best-practice deployment strategy in the long term for flexibility, reliability, performance, and cost.

{{< admonition type="note" >}}
Propagating logs from Cloudwatch to Loki means you'll still need to _pay_ for Cloudwatch.
{{< /admonition >}}

### VPC Flow logs

This workflow allows ingesting AWS VPC Flow logs from s3.

One thing to be aware of with this is that the default flow log format doesn't have a timestamp, so the log timestamp will be set to the time the lambda starts processing the log file.

### Loadbalancer logs

This workflow allows ingesting AWS Application/Network Load Balancer logs stored on S3 to Loki.

### Cloudtrail logs

This workflow allows ingesting AWS Cloudtrail logs stored on S3 to Loki.

### Cloudfront logs
Cloudfront logs can be either batched or streamed in real time to Loki:
+ Logging can be activated on a Cloudfront distribution with an S3 bucket as the destination. In this case, the workflow is the same as for other services (VPC Flow logs, Loadbalancer logs, Cloudtrail logs).
+ Cloudfront [real-time logs](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/real-time-logs.html) can be sent to a Kinesis data stream. The data stream can be mapped to be an [event source](https://docs.aws.amazon.com/lambda/latest/dg/invocation-eventsourcemapping.html) for lambda-promtail to deliver the logs to Loki.

### Triggering Lambda-Promtail via SQS
For AWS services supporting sending messages to SQS (for example, S3 with an S3 Notification to SQS), events can be processed through an [SQS queue using a lambda trigger](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html) instead of directly configuring the source service to trigger lambda. Lambda-promtail will retrieve the nested events from the SQS messages' body and process them as if them came directly from the source service.

### On-Failure log recovery using SQS
Triggering lambda-promtail through SQS allows handling on-failure recovery of the logs using a secondary SQS queue as a dead-letter-queue (DLQ). You can configure lambda so that unsuccessfully processed messages will be sent to the DLQ. After fixing the issue, operators will be able to reprocess the messages by sending back messages from the DLQ to the source queue using the [SQS DLQ redrive](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-configure-dead-letter-queue-redrive.html) feature.

### S3 based logging and CloudFormation

Lambda-promtail lets you send logs from different services that use S3 as their logs destination (ALB, VPC Flow, CloudFront access logs, etc.). For this, you need to configure S3 bucket notifications to trigger the lambda-promtail deployment. However, when using CloudFormation to encode infrastructure, there is a [known issue](https://github.com/aws-cloudformation/cloudformation-coverage-roadmap/issues/79) when configuring `AWS::S3::BucketNotification` and the resource that will be triggered by the notification in the same stack.

To manage this issue, AWS introduced [S3 event notifications with Event Bridge](https://aws.amazon.com/blogs/aws/new-use-amazon-s3-event-notifications-with-amazon-eventbridge/). When an object gets created in a S3 bucket, this sends an event to an EventBridge bus, and you can create a rule to send those events to Lambda-promtail.

The diagram below shows how notifications logs will be written from the source service into an S3 bucket. From there on, the S3 bucket will send an `Object created` notification into the EventBridge `default` bus, where we can configure a rule to trigger Lambda Promtail.

{{< figure src="https://grafana.com/media/docs/loki/lambda-promtail-with-eventbridge.png" alt="The diagram shows how notifications logs are written from the source service into an S3 bucket">}}

The [template-eventbridge.yaml](https://github.com/grafana/loki/blob/main/tools/lambda-promtail/template-eventbridge.yaml) CloudFormation template configures Lambda-promtail with EventBridge to address this known issue. To deploy the template, use the snippet below, completing appropriately the `ParameterValue` arguments.

```bash
aws cloudformation create-stack \
  --stack-name lambda-promtail-stack \
  --template-body file://template-eventbridge.yaml \
  --capabilities CAPABILITY_IAM CAPABILITY_NAMED_IAM \
  --region us-east-2 \
  --parameters ParameterKey=WriteAddress,ParameterValue=https://your-loki-url/loki/api/v1/push ParameterKey=Username,ParameterValue=<basic-auth-username> ParameterKey=Password,ParameterValue=<basic-auth-pw> ParameterKey=BearerToken,ParameterValue=<bearer-token> ParameterKey=LambdaPromtailImage,ParameterValue=<ecr-repo>:<tag> ParameterKey=ExtraLabels,ParameterValue="name1,value1,name2,value2" ParameterKey=TenantID,ParameterValue=<value> ParameterKey=SkipTlsVerify,ParameterValue="false" ParameterKey=EventSourceS3Bucket,ParameterValue=<S3 where target logs are stored>
```

## Propagated Labels

Incoming logs can have seven special labels assigned to them which can be used in [relabeling]({{< relref "../../send-data/promtail/configuration#relabel_configs" >}}) or later stages in a Promtail [pipeline]({{< relref "../../send-data/promtail/pipelines" >}}):

- `__aws_log_type`: Where this log came from (Cloudwatch, Kinesis or S3).
- `__aws_cloudwatch_log_group`: The associated Cloudwatch Log Group for this log.
- `__aws_cloudwatch_log_stream`: The associated Cloudwatch Log Stream for this log (if `KEEP_STREAM=true`).
- `__aws_cloudwatch_owner`: The AWS ID of the owner of this event.
- `__aws_kinesis_event_source_arn`: The Kinesis event source ARN.
- `__aws_s3_log_lb`: The name of the loadbalancer.
- `__aws_s3_log_lb_owner`: The Account ID of the loadbalancer owner.

## Relabeling Configuration

Lambda-promtail supports Prometheus-style relabeling through the `RELABEL_CONFIGS` environment variable. This allows you to modify, keep, or drop labels before sending logs to Loki. The configuration is provided as a JSON array of relabel configurations. The relabeling functionality follows the same principles as Prometheus relabeling - for a detailed explanation of how relabeling works, see [How relabeling in Prometheus works](https://grafana.com/blog/2022/03/21/how-relabeling-in-prometheus-works/).

Example configurations:

1. Rename a label and capture regex groups:
```json
{
  "RELABEL_CONFIGS": [
    {
      "source_labels": ["__aws_log_type"],
      "target_label": "log_type",
      "action": "replace",
      "regex": "(.*)",
      "replacement": "${1}"
    }
  ]
}
```

2. Keep only specific log types (useful for filtering):
```json
{
  "RELABEL_CONFIGS": [
    {
      "source_labels": ["__aws_log_type"],
      "regex": "s3_.*",
      "action": "keep"
    }
  ]
}
```

3. Drop internal AWS labels (cleanup):
```json
{
  "RELABEL_CONFIGS": [
    {
      "regex": "__aws_.*",
      "action": "labeldrop"
    }
  ]
}
```

4. Multiple relabeling rules (combining different actions):
```json
{
  "RELABEL_CONFIGS": [
    {
      "source_labels": ["__aws_log_type"],
      "target_label": "log_type",
      "action": "replace",
      "regex": "(.*)",
      "replacement": "${1}"
    },
    {
      "source_labels": ["__aws_s3_log_lb"],
      "target_label": "loadbalancer",
      "action": "replace"
    },
    {
      "regex": "__aws_.*",
      "action": "labeldrop"
    }
  ]
}
```

### Supported Actions

The following actions are supported, matching Prometheus relabeling capabilities:

- `replace`: Replace a label value with a new value using regex capture groups
- `keep`: Keep entries where labels match the regex (useful for filtering)
- `drop`: Drop entries where labels match the regex (useful for excluding)
- `hashmod`: Set a label to the modulus of a hash of labels (useful for sharding)
- `labelmap`: Copy labels to other labels based on regex matching
- `labeldrop`: Remove labels matching the regex pattern
- `labelkeep`: Keep only labels matching the regex pattern
- `lowercase`: Convert label values to lowercase
- `uppercase`: Convert label values to uppercase

### Configuration Fields

Each relabel configuration supports these fields (all fields are optional except for `action`):

- `source_labels`: List of label names to use as input for the action
- `separator`: String to join source label values (default: ";")
- `target_label`: Label to modify (required for replace and hashmod actions)
- `regex`: Regular expression to match against (defaults to "(.+)" for most actions)
- `replacement`: Replacement pattern for matched regex, supports ${1}, ${2}, etc. for capture groups
- `modulus`: Modulus for hashmod action
- `action`: One of the supported actions listed above

### Important Notes

1. Relabeling is applied after merging extra labels and dropping labels specified by `DROP_LABELS`.
2. If all labels are removed after relabeling, the log entry will be dropped entirely.
3. The relabeling configuration follows the same format as Prometheus's relabel_configs, making it familiar for users of Prometheus.
4. Relabeling rules are processed in order, and each rule can affect the input of subsequent rules.
5. Regular expressions in the `regex` field support full RE2 syntax.
6. For the `replace` action, if the `regex` doesn't match, the target label remains unchanged.

For more details about how relabeling works and advanced use cases, refer to the [Prometheus relabeling blog post](https://grafana.com/blog/2022/03/21/how-relabeling-in-prometheus-works/).

## Limitations

### Promtail labels

{{< admonition type="note" >}}
This section is relevant if running Promtail between lambda-promtail and the end Loki deployment and was used to circumvent `out of order` problems prior to the v2.4 Loki release which removed the ordering constraint.
{{< /admonition >}}

As stated earlier, this workflow moves the worst case stream cardinality from `number_of_log_streams` -> `number_of_log_groups` * `number_of_promtails`. For this reason, each Promtail must have a unique label attached to logs it processes (ideally via something like `--client.external-labels=promtail=${HOSTNAME}`) and it's advised to run a small number of Promtails behind a load balancer according to your throughput and redundancy needs.

This trade-off is very effective when you have a large number of log streams but want to aggregate them by the log group. This is very common in AWS Lambda, where log groups are the "application" and log streams are the individual application containers which are spun up and down at a whim, possibly just for a single function invocation.

### Data Persistence

#### Availability

For availability concerns, run a set of Promtails behind a load balancer.

#### Batching

Relevant if lambda-promtail is configured to write to Promtail. Since Promtail batches writes to Loki for performance, it's possible that Promtail will receive a log, issue a successful `204` http status code for the write, then be killed at a later time before it writes upstream to Loki. This should be rare, but is a downside this workflow has.

This lambda will flush logs when the batch size hits the default value of `131072` (128KB), this can be changed with `BATCH_SIZE` environment variable, which is set to the number of bytes to use.

### Templating/Deployment

The current CloudFormation template is rudimentary. If you need to add vpc configs, extra log groups to monitor, subnet declarations, etc, you'll need to edit the template manually. If you need to subscribe to more than one Cloudwatch Log Group you'll also need to copy paste that section of the template for each group.

The Terraform file is a bit more fleshed out, and can be configured to take in an array of log group and bucket names, as well as vpc configuration.

The provided Terraform and CloudFormation files are meant to cover the default use case, and more complex deployments will likely require some modification and extenstion of the provided files.

## Example Promtail Config

{{< admonition type="note" >}}
This should be run in conjunction with a Promtail-specific label attached, ideally via a flag argument like `--client.external-labels=promtail=${HOSTNAME}`. It will receive writes via the push-api on ports `3500` (http) and `3600` (grpc).
{{< /admonition >}}

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
      - source_labels: ['__aws_log_type']
        target_label: 'log_type'
      # Maps the cloudwatch log group into a label called `log_group` for use in Loki.
      - source_labels: ['__aws_cloudwatch_log_group']
        target_label: 'log_group'
      # Maps the loadbalancer name into a label called `loadbalancer_name` for use in Loki.
      - source_labels: ['__aws_s3_log_lb']
        target_label: 'loadbalancer_name'
```

## Multiple Promtail Deployment

**Disclaimer: The following section is only relevant for older versions of Loki that cannot accept out of order logs.**

However, these may only be active for a very short while. This creates a problem for combining these short-lived log streams in Loki because timestamps may not strictly increase across multiple log streams. The other obvious route is creating labels based on log streams, which is also undesirable because it leads to cardinality problems via many low-throughput log streams.

Instead we can pipeline Cloudwatch logs to a set of Promtails, which can mitigate these problem in two ways:

1) Using Promtail's push api along with the `use_incoming_timestamp: false` config, we let Promtail determine the timestamp based on when it ingests the logs, not the timestamp assigned by cloudwatch. Obviously, this means that we lose the origin timestamp because Promtail now assigns it, but this is a relatively small difference in a real time ingestion system like this.
2) In conjunction with (1), Promtail can coalesce logs across  Cloudwatch log streams because it's no longer susceptible to out-of-order errors when combining multiple sources (lambda invocations).

One important aspect to keep in mind when running with a set of Promtails behind a load balancer is that we're effectively moving the cardinality problems from the  number of log streams -> number of Promtails. If you have not configured Loki to [accept out-of-order writes](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#accept-out-of-order-writes), you'll need to assign a Promtail-specific label on each Promtail so that you don't run into out-of-order errors when the Promtails send data for the same log groups to Loki. This can easily be done via a configuration like `--client.external-labels=promtail=${HOSTNAME}` passed to Promtail.
