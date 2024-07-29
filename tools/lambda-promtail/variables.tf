variable "name" {
  type        = string
  description = "Name used for created AWS resources."
  default     = "lambda_promtail"
}

variable "write_address" {
  type        = string
  description = "This is the Loki Write API compatible endpoint that you want to write logs to, either promtail or Loki."
  default     = "http://localhost:8080/loki/api/v1/push"
}

variable "bucket_names" {
  type        = set(string)
  description = "List of S3 bucket names to create Event Notifications for."
  default     = []
}

variable "filter_suffix" {
  type        = string
  description = "Suffix for S3 bucket notification filter"
  default     = ".gz"
}

variable "log_group_names" {
  type        = set(string)
  description = "List of CloudWatch Log Group names to create Subscription Filters for."
  default     = []
}

variable "lambda_promtail_image" {
  type        = string
  description = "The ECR image URI to pull and use for lambda-promtail."
  default     = ""
}

variable "username" {
  type        = string
  description = "The basic auth username, necessary if writing directly to Grafana Cloud Loki."
  default     = ""
}

variable "password" {
  type        = string
  description = "The basic auth password, necessary if writing directly to Grafana Cloud Loki."
  sensitive   = true
  default     = ""
}

variable "bearer_token" {
  type        = string
  description = "The bearer token, necessary if target endpoint requires it."
  sensitive   = true
  default     = ""
}

variable "tenant_id" {
  type        = string
  description = "Tenant ID to be added when writing logs from lambda-promtail."
  default     = ""
}

variable "keep_stream" {
  type        = string
  description = "Determines whether to keep the CloudWatch Log Stream value as a Loki label when writing logs from lambda-promtail."
  default     = "false"
}

variable "print_log_line" {
  type        = string
  description = "Determines whether we want the lambda to output the parsed log line before sending it on to promtail. Value needed to disable is the string 'false'"
  default     = "true"
}

variable "extra_labels" {
  type        = string
  description = "Comma separated list of extra labels, in the format 'name1,value1,name2,value2,...,nameN,valueN' to add to entries forwarded by lambda-promtail."
  default     = ""
}

variable "drop_labels" {
  type        = string
  description = "Comma separated list of labels to be drop, in the format 'name1,name2,...,nameN' to be omitted to entries forwarded by lambda-promtail."
  default     = ""
}

variable "omit_extra_labels_prefix" {
  type        = bool
  description = "Whether or not to omit the prefix `__extra_` from extra labels defined in the variable `extra_labels`."
  default     = false
}

variable "batch_size" {
  type        = string
  description = "Determines when to flush the batch of logs (bytes)."
  default     = ""
}

variable "lambda_vpc_subnets" {
  type        = list(string)
  description = "List of subnet IDs associated with the Lambda function."
  default     = []
}

variable "lambda_vpc_security_groups" {
  type        = list(string)
  description = "List of security group IDs associated with the Lambda function."
  default     = []
}

variable "kms_key_arn" {
  type        = string
  description = "kms key arn for encrypting env vars."
  default     = ""
}

variable "skip_tls_verify" {
  type        = string
  description = "Determines whether to verify the TLS certificate"
  default     = "false"
}

variable "kinesis_stream_name" {
  type        = set(string)
  description = "Enter kinesis name if kinesis stream is configured as event source in lambda."
  default     = []
}

variable "sqs_enabled" {
  type        = bool
  description = "Enables sending S3 logs to an SQS queue which will trigger lambda-promtail, unsuccessfully processed message are sent to a dead-letter-queue"
  default     = false
}

variable "sqs_queue_name_prefix" {
  type        = string
  description = "Name prefix for SQS queues"
  default     = "s3-to-lambda-promtail"
}
