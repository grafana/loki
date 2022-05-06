---
title: Lambda Promtail
weight: 20
---
# Lambda Promtail

Grafana Loki includes [Terraform](https://www.terraform.io/) and [CloudFormation](https://aws.amazon.com/cloudformation/) for shipping Cloudwatch and loadbalancer logs to Loki via a [lambda function](https://aws.amazon.com/lambda/). This is done via [lambda-promtail](https://github.com/grafana/loki/tree/master/tools/lambda-promtail) which processes cloudwatch events and propagates them to Loki (or a Promtail instance) via the push-api [scrape config](../promtail/configuration#loki_push_api_config).

## Deployment

lambda-promtail can easily be deployed via provided [Terraform](https://github.com/grafana/loki/blob/main/tools/lambda-promtail/main.tf) and [CloudFormation](https://github.com/grafana/loki/blob/main/tools/lambda-promtail/template.yaml) files. The Terraform deployment also pulls variable values defined from [variables.tf](https://github.com/grafana/loki/blob/main/tools/lambda-promtail/variables.tf).

For both deployment types there are a few values that must be defined:
- the write address, a Loki Write API compatible endpoint (Loki or Promtail)
- basic auth username/password if the write address is a Loki endpoint and has authentication
- the lambda-promtail image, full ECR repo path:tag

The Terraform deployment also takes in an array of log group and bucket names, and can take arrays for VPC subnets and security groups.

There's also a flag to keep the log stream label when propagating the logs from Cloudwatch, which defaults to false. This can be helpful when the cardinality is too large, such as the case of a log stream per lambda invocation.

Additionally, an environment variable can be configured to add extra lables to the logs streamed by lambda-protmail.
These extra labels will take the form `__extra_<name>=<value>`

Optional environment variable can be configured to add tenant id to the logs streamed by lambda-protmail.

In an effort to make deployment of lambda-promtail as simple as possible, we've created a [public ECR repo](https://gallery.ecr.aws/grafana/lambda-promtail) to publish our builds of lambda-promtail. Users are still able to clone this repo, make their own modifications to the Go code, and upload their own image to their own ECR repo if they wish.

### Examples

Terraform:
```
terraform apply -var "lambda_promtail_image=<repo:tag>" -var "write_address=https://logs-prod-us-central1.grafana.net/loki/api/v1/push" -var "password=<password>" -var "username=<user>" -var 'log_group_names=["/aws/lambda/log-group-1", "/aws/lambda/log-group-2"]' -var 'bucket_names=["bucket-a", "bucket-b"]' -var 'batch_size=131072'
```

The first few lines of `main.tf` define the AWS region to deploy to, you are free to modify this or remove and deploy to 
```
provider "aws" {
  region = "us-east-2"
}
```

To keep the log group label add `-var "keep_stream=true"`.

To add extra labels add `-var 'extra_labels="name1,value1,name2,value2"'`

To add tenant id add `-var "tenant_id=value"`

Note that the creation of subscription filter on Cloudwatch in the provided Terraform file only accepts an array of log group names, it does **not** accept strings for regex filtering on the logs contents via the subscription filters. We suggest extending the Terraform file to do so, or having lambda-promtail write to Promtail and using [pipeline stages](https://grafana.com/docs/loki/latest/clients/promtail/stages/drop/).

CloudFormation:
```
aws cloudformation create-stack --stack-name lambda-promtail --template-body file://template.yaml --capabilities CAPABILITY_IAM CAPABILITY_NAMED_IAM --region us-east-2 --parameters ParameterKey=WriteAddress,ParameterValue=https://logs-prod-us-central1.grafana.net/loki/api/v1/push ParameterKey=Username,ParameterValue=<user> ParameterKey=Password,ParameterValue=<password> ParameterKey=LambdaPromtailImage,ParameterValue=<repo:tag>
```

Within the CloudFormation template file you should copy/paste and modify the subscription filter section as needed for each log group:
```
MainLambdaPromtailSubscriptionFilter:
  Type: AWS::Logs::SubscriptionFilter
  Properties:
    DestinationArn: !GetAtt LambdaPromtailFunction.Arn
    FilterPattern: ""
    LogGroupName: "/aws/lambda/some-lamda-log-group"
```

To keep the log group label add `ParameterKey=KeepStream,ParameterValue=true`.

To add extra labels, include `ParameterKey=ExtraLabels,ParameterValue="name1,value1,name2,value2"`

To add tenant id add `ParameterKey=TenantID,ParameterValue=value`.

To modify an already created CloudFormation stack you need to use [update-stack](https://docs.aws.amazon.com/cli/latest/reference/cloudformation/update-stack.html).

## Uses

### Ephemeral Jobs

This workflow is intended to be an effective approach for monitoring ephemeral jobs such as those run on AWS Lambda which are otherwise hard/impossible to monitor via one of the other Loki [clients](../).

Ephemeral jobs can quite easily run afoul of cardinality best practices. During high request load, an AWS lambda function might balloon in concurrency, creating many log streams in Cloudwatch. For this reason lambda-promtail defaults to **not** keeping the log stream value as a label when propagating the logs to Loki. This is only possible because new versions of Loki no longer have an ingestion ordering constraing on logs within a single stream. 

### Proof of concept Loki deployments

For those using Cloudwatch and wishing to test out Loki in a low-risk way, this workflow allows piping Cloudwatch logs to Loki regardless of the event source (EC2, Kubernetes, Lambda, ECS, etc) without setting up a set of Promtail daemons across their infrastructure. However, running Promtail as a daemon on your infrastructure is the best-practice deployment strategy in the long term for flexibility, reliability, performance, and cost.

Note: Propagating logs from Cloudwatch to Loki means you'll still need to _pay_ for Cloudwatch.

### Loadbalancer logs

This workflow allows ingesting AWS loadbalancer logs stored on S3 to Loki.

## Propagated Labels

Incoming logs can have six special labels assigned to them which can be used in [relabeling](../promtail/configuration/#relabel_config) or later stages in a Promtail [pipeline](../promtail/pipelines/):

- `__aws_log_type`: Where this log came from (Cloudwatch or S3).
- `__aws_cloudwatch_log_group`: The associated Cloudwatch Log Group for this log.
- `__aws_cloudwatch_log_stream`: The associated Cloudwatch Log Stream for this log (if `KEEP_STREAM=true`).
- `__aws_cloudwatch_owner`: The AWS ID of the owner of this event.
- `__aws_s3_log_lb`: The name of the loadbalancer.
- `__aws_s3_log_lb_owner`: The Account ID of the loadbalancer owner.

## Limitations

### Promtail labels

Note: This section is relevant if running Promtail between lambda-promtail and the end Loki deployment and was used to circumvent `out of order` problems prior to the v2.4 Loki release which removed the ordering constraint.

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
      - source_labels: ['__aws_log_type']
        taget_label: 'log_type'
      # Maps the cloudwatch log group into a label called `log_group` for use in Loki.
      - source_labels: ['__aws_cloudwatch_log_group']
        target_label: 'log_group'
      # Maps the loadbalancer name into a label called `loadbalancer_name` for use in Loki.
      - source_label: ['__aws_s3_log_lb']
        taget_label: 'loadbalancer_name'
```

## Multiple Promtail Deployment

**Disclaimer: The following section is only relevant for older versions of Loki that cannot accept out of order logs.**

However, these may only be active for a very short while. This creates a problem for combining these short-lived log streams in Loki because timestamps may not strictly increase across multiple log streams. The other obvious route is creating labels based on log streams, which is also undesirable because it leads to cardinality problems via many low-throughput log streams.

Instead we can pipeline Cloudwatch logs to a set of Promtails, which can mitigate these problem in two ways:

1) Using Promtail's push api along with the `use_incoming_timestamp: false` config, we let Promtail determine the timestamp based on when it ingests the logs, not the timestamp assigned by cloudwatch. Obviously, this means that we lose the origin timestamp because Promtail now assigns it, but this is a relatively small difference in a real time ingestion system like this.
2) In conjunction with (1), Promtail can coalesce logs across  Cloudwatch log streams because it's no longer susceptible to out-of-order errors when combining multiple sources (lambda invocations).

One important aspect to keep in mind when running with a set of Promtails behind a load balancer is that we're effectively moving the cardinality problems from the  number of log streams -> number of Promtails. If you have not configured Loki to [accept out-of-order writes](../../configuration#accept-out-of-order-writes), you'll need to assign a Promtail-specific label on each Promtail so that you don't run into out-of-order errors when the Promtails send data for the same log groups to Loki. This can easily be done via a configuration like `--client.external-labels=promtail=${HOSTNAME}` passed to Promtail.
