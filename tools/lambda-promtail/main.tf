data "aws_region" "current" {}

#-------------------------------------------------------------------------------
# IAM role assigned to the lambda function
#-------------------------------------------------------------------------------

resource "aws_iam_role" "this" {
  name               = var.name
  assume_role_policy = data.aws_iam_policy_document.assume_role.json
}

data "aws_iam_policy_document" "assume_role" {
  statement {
    sid     = ""
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

#-------------------------------------------------------------------------------
# IAM policy assigned to lambda IAM role to be able to execute in VPC
#-------------------------------------------------------------------------------

resource "aws_iam_role_policy_attachment" "lambda_vpc_execution" {
  count = length(var.lambda_vpc_subnets) > 0 ? 1 : 0

  role       = aws_iam_role.this.name
  policy_arn = data.aws_iam_policy.lambda_vpc_execution[0].arn
}

data "aws_iam_policy" "lambda_vpc_execution" {
  count = length(var.lambda_vpc_subnets) > 0 ? 1 : 0

  name = "AWSLambdaVPCAccessExecutionRole"
}

#-------------------------------------------------------------------------------
# IAM policies attached to lambda IAM role
#-------------------------------------------------------------------------------

# CloudWatch

# These permissions are also included in the AWSLambdaVPCAccessExecutionRole IAM Policy
resource "aws_iam_role_policy" "lambda_cloudwatch" {
  count = length(var.lambda_vpc_subnets) == 0 ? 1 : 0

  name   = "cloudwatch"
  role   = aws_iam_role.this.name
  policy = data.aws_iam_policy_document.lambda_cloudwatch[0].json
}

# These permissions are also included in the AWSLambdaVPCAccessExecutionRole IAM Policy
data "aws_iam_policy_document" "lambda_cloudwatch" {
  count = length(var.lambda_vpc_subnets) == 0 ? 1 : 0

  statement {
    actions = [
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = [
      format("%s:*", aws_cloudwatch_log_group.this.arn),
    ]
  }
}

# KMS

resource "aws_iam_role_policy" "lambda_kms" {
  count = var.kms_key_arn != "" ? 1 : 0

  name   = "kms"
  role   = aws_iam_role.this.name
  policy = data.aws_iam_policy_document.lambda_kms[0].json
}

data "aws_iam_policy_document" "lambda_kms" {
  count = var.kms_key_arn != "" ? 1 : 0

  statement {
    actions = [
      "kms:Decrypt",
    ]
    resources = [
      var.kms_key_arn,
    ]
  }
}

# S3

resource "aws_iam_role_policy" "lambda_s3" {
  count = length(var.bucket_names) > 0 ? 1 : 0

  name   = "s3"
  role   = aws_iam_role.this.name
  policy = data.aws_iam_policy_document.lambda_s3[0].json
}

data "aws_iam_policy_document" "lambda_s3" {
  count = length(var.bucket_names) > 0 ? 1 : 0

  statement {
    actions = [
      "s3:GetObject",
    ]
    resources = [
      for _, bucket_name in var.bucket_names : "arn:aws:s3:::${bucket_name}/*"
    ]
  }

}

# Kinesis

resource "aws_iam_role_policy" "lambda_kinesis" {
  count = length(var.kinesis_stream_name) > 0 ? 1 : 0

  name   = "kinesis"
  role   = aws_iam_role.this.name
  policy = data.aws_iam_policy_document.lambda_kinesis[0].json
}

data "aws_iam_policy_document" "lambda_kinesis" {
  count = length(var.kinesis_stream_name) > 0 ? 1 : 0

  statement {
    actions = [
      "kinesis:*",
    ]
    resources = [
      for _, stream in aws_kinesis_stream.this : stream.arn
    ]
  }
}

#-------------------------------------------------------------------------------
# Lambda function
#-------------------------------------------------------------------------------

resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.name}"
  retention_in_days = 14
}

locals {
  binary_path  = "${path.module}/lambda-promtail/bootstrap"
  archive_path = "${path.module}/lambda-promtail/${var.name}.zip"
}

resource "null_resource" "function_binary" {
  count = var.lambda_promtail_image == "" ? 1 : 0
  triggers = {
    always_run = timestamp()
  }

  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOFLAGS=-trimpath go build -mod=readonly -ldflags='-s -w' -o bootstrap"
    working_dir = format("%s/%s", path.module, "lambda-promtail")
  }
}

data "archive_file" "lambda" {
  count      = var.lambda_promtail_image == "" ? 1 : 0
  depends_on = [null_resource.function_binary[0]]

  type        = "zip"
  source_file = local.binary_path
  output_path = local.archive_path
}

resource "aws_lambda_function" "this" {
  function_name = var.name
  role          = aws_iam_role.this.arn
  kms_key_arn   = var.kms_key_arn

  image_uri        = var.lambda_promtail_image == "" ? null : var.lambda_promtail_image
  filename         = var.lambda_promtail_image == "" ? local.archive_path : null
  source_code_hash = var.lambda_promtail_image == "" ? data.archive_file.lambda[0].output_base64sha256 : null
  runtime          = var.lambda_promtail_image == "" ? "provided.al2023" : null
  handler          = var.lambda_promtail_image == "" ? local.binary_path : null

  timeout      = 60
  memory_size  = 128
  package_type = var.lambda_promtail_image == "" ? "Zip" : "Image"

  # From the Terraform AWS Lambda docs: If both subnet_ids and security_group_ids are empty then vpc_config is considered to be empty or unset.
  vpc_config {
    # Every subnet should be able to reach an EFS mount target in the same Availability Zone. Cross-AZ mounts are not permitted.
    subnet_ids         = var.lambda_vpc_subnets
    security_group_ids = var.lambda_vpc_security_groups
  }

  environment {
    variables = {
      WRITE_ADDRESS            = var.write_address
      USERNAME                 = var.username
      PASSWORD                 = var.password
      BEARER_TOKEN             = var.bearer_token
      KEEP_STREAM              = var.keep_stream
      BATCH_SIZE               = var.batch_size
      EXTRA_LABELS             = var.extra_labels
      DROP_LABELS              = var.drop_labels
      OMIT_EXTRA_LABELS_PREFIX = var.omit_extra_labels_prefix ? "true" : "false"
      TENANT_ID                = var.tenant_id
      SKIP_TLS_VERIFY          = var.skip_tls_verify
      PRINT_LOG_LINE           = var.print_log_line
    }
  }

  depends_on = [
    aws_iam_role_policy.lambda_s3,
    aws_iam_role_policy.lambda_kms,
    aws_iam_role_policy.lambda_kinesis,
    aws_iam_role_policy.lambda_cloudwatch,

    aws_iam_role_policy_attachment.lambda_vpc_execution,

    # Ensure function is created after, and destroyed before, the log-group
    # This prevents the log-group from being re-created by an invocation of the lambda-function
    aws_cloudwatch_log_group.this,
  ]
}

resource "aws_lambda_function_event_invoke_config" "this" {
  function_name          = aws_lambda_function.this.function_name
  maximum_retry_attempts = 2
}

#-------------------------------------------------------------------------------
# Subscribe to CloudWatch log-groups
#-------------------------------------------------------------------------------

resource "aws_lambda_permission" "lambda_promtail_allow_cloudwatch" {
  count = length(var.log_group_names) > 0 ? 1 : 0

  statement_id  = "lambda-promtail-allow-cloudwatch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "logs.${data.aws_region.current.name}.amazonaws.com"
}

# This block allows for easily subscribing to multiple log groups via the `log_group_names` var.
# However, if you need to provide an actual filter_pattern for a specific log group you should
# copy this block and modify it accordingly.
resource "aws_cloudwatch_log_subscription_filter" "lambdafunction_logfilter" {
  for_each = var.log_group_names

  name            = "lambdafunction_logfilter_${each.value}"
  log_group_name  = each.value
  destination_arn = aws_lambda_function.this.arn

  # required but can be empty string
  filter_pattern = ""
}

#-------------------------------------------------------------------------------
# Subscribe to S3-objects
#-------------------------------------------------------------------------------

resource "aws_lambda_permission" "allow_s3_invoke_lambda_promtail" {
  for_each = var.bucket_names

  statement_id  = "lambda-promtail-allow-s3-bucket-${each.value}"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.arn
  principal     = "s3.amazonaws.com"
  source_arn    = "arn:aws:s3:::${each.value}"
}

resource "aws_s3_bucket_notification" "this" {
  for_each = var.sqs_enabled ? [] : var.bucket_names

  bucket = each.value

  lambda_function {
    lambda_function_arn = aws_lambda_function.this.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "AWSLogs/"
    filter_suffix       = var.filter_suffix
  }

  depends_on = [
    aws_lambda_permission.allow_s3_invoke_lambda_promtail
  ]
}

#-------------------------------------------------------------------------------
# Subscribe to Kinesis-streams
#-------------------------------------------------------------------------------

resource "aws_kinesis_stream" "this" {
  for_each = var.kinesis_stream_name

  name             = each.value
  shard_count      = 1
  retention_period = 48

  shard_level_metrics = [
    "IncomingBytes",
    "OutgoingBytes",
  ]

  stream_mode_details {
    stream_mode = "PROVISIONED"
  }
}

resource "aws_lambda_event_source_mapping" "this" {
  for_each = aws_kinesis_stream.this

  event_source_arn  = each.value.arn
  function_name     = aws_lambda_function.this.arn
  starting_position = "LATEST"
}
