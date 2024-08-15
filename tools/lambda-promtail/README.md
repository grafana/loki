# lambda-promtail

This is a sample deployment for lambda-promtail - Below is a brief explanation of what we have generated for you:

```bash
.
├── Makefile                    <-- Make to automate build
├── Dockerfile                  <-- Uses the AWS Lambda Go base image
├── README.md                   <-- This instructions file
├── lambda-promtail             <-- Source code for a lambda function
│   └── main.go                 <-- Lambda function code
```

## Requirements

* AWS CLI already configured with Administrator permission
* [Terraform](https://www.terraform.io/downloads.html)

If you want to modify the lambda-promtail code you will also need:
* [Docker installed](https://www.docker.com/community-edition)
* [Golang](https://golang.org)

## Setup process

### Building and Packaging

The provided Makefile has targets `build`, and `clean`.

`build` builds the lambda-promtail as a Go static binary. To build the container image properly you should run `docker build . -f tools/lambda-promtail/Dockerfile` from the root of the Loki repository, you can upload this image to your AWS ECR and use via Lambda or if you don't pass a `lambda_promtail_image` value, the terraform will build it from the Loki repository, zip it and use it via Lambda. `clean` will remove the built Go binary.

### Packaging and deployment

The easiest way to deploy to AWS Lambda using the Golang runtime is to build the `lambda-promtail` go file, zip it and upload it to the lambda function with terraform.

To deploy your application for the first time, first make sure you've set the following value in the Terraform file:
- `WRITE_ADDRESS`

This is the [Loki Write API](https://grafana.com/docs/loki/latest/api/#post-lokiapiv1push) compatible endpoint that you want to write logs to, either promtail or Loki.

The `lambda-promtail` code picks this value up via an environment variable.

Also, if your deployment requires a [VPC configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#vpc_config), make sure to edit the `vpc_config` field in `main.tf` manually. Additonal documentation for the Lambda specific Terraform configuration is [here](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#vpc_config). If you want to link kinesis data stream to Lambda as event source, see [here](https://docs.aws.amazon.com/ko_kr/lambda/latest/dg/with-kinesis.html).

`lambda-promtail` supports authentication either using HTTP Basic Auth or using Bearer Token.
For development purposes, you can set the environment variable SKIP_TLS_VERIFY to `true`, so you can use self-signed certificates, but this is not recommended in production. Default is `false`.

Then use Terraform to deploy:

```bash
## use cloudwatch log group
terraform apply -var "write_address=https://your-loki-url/loki/api/v1/push" -var "password=<basic-auth-pw>" -var "username=<basic-auth-username>" -var 'bearer_token=<bearer-token>' -var 'log_group_names=["log-group-01", "log-group-02"]' -var 'extra_labels="name1,value1,name2,value2"' -var 'drop_labels="name1,name2"' -var "tenant_id=<value>" -var 'skip_tls_verify="false"'
```

```bash
## use docker image uploaded to ECR
terraform apply -var "lambda_promtail_image=<ecr-repo>:<tag>" -var "write_address=https://your-loki-url/loki/api/v1/push" -var "password=<basic-auth-pw>" -var "username=<basic-auth-username>" -var 'bearer_token=<bearer-token>' -var 'extra_labels="name1,value1,name2,value2"' -var 'drop_labels="name1,name2"' -var "tenant_id=<value>" -var 'skip_tls_verify="false"'
```

```bash
## use kinesis data stream
terraform apply -var "write_address=https://your-loki-url/loki/api/v1/push" -var "password=<basic-auth-pw>" -var "username=<basic-auth-username>" -var 'kinesis_stream_name=["kinesis-stream-01", "kinesis-stream-02"]' -var 'extra_labels="name1,value1,name2,value2"'  -var 'drop_labels="name1,name2"' -var "tenant_id=<value>" -var 'skip_tls_verify="false"'
```

or CloudFormation:

```bash
aws cloudformation create-stack --stack-name lambda-promtail-stack --template-body file://template.yaml --capabilities CAPABILITY_IAM CAPABILITY_NAMED_IAM --region us-east-2 --parameters ParameterKey=WriteAddress,ParameterValue=https://your-loki-url/loki/api/v1/push ParameterKey=Username,ParameterValue=<basic-auth-username> ParameterKey=Password,ParameterValue=<basic-auth-pw> ParameterKey=BearerToken,ParameterValue=<bearer-token> ParameterKey=LambdaPromtailImage,ParameterValue=<ecr-repo>:<tag> ParameterKey=ExtraLabels,ParameterValue="name1,value1,name2,value2" ParameterKey=TenantID,ParameterValue=<value> ParameterKey=SkipTlsVerify,ParameterValue="false"
```

**NOTE:** To use CloudFormation, you must build the docker image with `docker build . -f tools/lambda-promtail/Dockerfile` from the root of the Loki repository, upload it to an ECR, and pass it as the `LambdaPromtailImage` parameter to cloudformation.

# Appendix

## Golang installation

Please ensure Go 1.x (where 'x' is the latest version) is installed as per the instructions on the official golang website: https://golang.org/doc/install

For example:

```bash
GO_VERSION=go1.16.6.linux-amd64.tar.gz

rm -rf /usr/local/bin/go*
rm -rf /usr/local/go
curl -O https://storage.googleapis.com/golang/$GO_VERSION
tar -zxvf $GO_VERSION
sudo mv go /usr/local/
rm $GO_VERSION
ln -s /usr/local/go/bin/* /usr/local/bin/
```

A quickstart way would be to use Homebrew, chocolatey or your Linux package manager.

#### Homebrew (Mac)

Issue the following command from the terminal:

```shell
brew install golang
```

If it's already installed, run the following command to ensure it's the latest version:

```shell
brew update
brew upgrade golang
```

#### Chocolatey (Windows)

Issue the following command from the powershell:

```shell
choco install golang
```

If it's already installed, run the following command to ensure it's the latest version:

```shell
choco upgrade golang
```

## CloudFormation and S3 events

Lambda-promtail lets you send logs from different services that use S3 as their logs destination (ALB, VPC Flow, CloudFront access logs, etc.). For this, you need to configure S3 bucket notifications to trigger the lambda-promtail deployment. However, when using CloudFormation to encode infrastructure, there is a [known issue](https://github.com/aws-cloudformation/cloudformation-coverage-roadmap/issues/79) when configuring `AWS::S3::BucketNotification` and the resource that will be triggered by the notification in the same stack.

To manage the issue, AWS introduced [S3 event notifications with Event Bridge](https://aws.amazon.com/blogs/aws/new-use-amazon-s3-event-notifications-with-amazon-eventbridge/). In that way, when an object gets created in a S3 bucket, this sends an event to an EventBridge bus, and you can create a rule to send those events to Lambda-promtail.

The [template-eventbridge.yaml](./template-eventbridge.yaml) CloudFormation template configures Lambda-promtail with EventBridge, for the use case mentioned above:

```bash
aws cloudformation create-stack --stack-name lambda-promtail-stack --template-body file://template-eventbridge.yaml --capabilities CAPABILITY_IAM CAPABILITY_NAMED_IAM --region us-east-2 --parameters ParameterKey=WriteAddress,ParameterValue=https://your-loki-url/loki/api/v1/push ParameterKey=Username,ParameterValue=<basic-auth-username> ParameterKey=Password,ParameterValue=<basic-auth-pw> ParameterKey=BearerToken,ParameterValue=<bearer-token> ParameterKey=LambdaPromtailImage,ParameterValue=<ecr-repo>:<tag> ParameterKey=ExtraLabels,ParameterValue="name1,value1,name2,value2" ParameterKey=TenantID,ParameterValue=<value> ParameterKey=SkipTlsVerify,ParameterValue="false" ParameterKey=EventSourceS3Bucket,ParameterValue="alb-logs-bucket-name"
```

## Limitations
- Error handling: If promtail is unresponsive, `lambda-promtail` will drop logs after `retry_count`, which defaults to 2.
- AWS CloudWatch [quotas](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/cloudwatch_limits_cwl.html) state that the event size is limited to 256kb. `256 KB (maximum). This quota can't be changed.`
