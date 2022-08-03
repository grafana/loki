provider "aws" {
  region = "us-east-2"
}

data "aws_region" "current" {}

resource "aws_iam_role" "iam_for_lambda" {
  name = "iam_for_lambda"

  assume_role_policy = jsonencode({
    "Version" : "2012-10-17",
    "Statement" : [
      {
        "Action" : "sts:AssumeRole",
        "Principal" : {
          "Service" : "lambda.amazonaws.com"
        },
        "Effect" : "Allow",
      }
    ]
  })
}

resource "aws_iam_role_policy" "logs" {
  name = "lambda-logs"
  role = aws_iam_role.iam_for_lambda.name
  policy = jsonencode({
    "Statement" : [
      {
        "Action" : [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ],
        "Effect" : "Allow",
        "Resource" : "arn:aws:logs:*:*:*",
      },
      {
        "Action" : [
          "s3:GetObject",
        ],
        "Effect" : "Allow",
        "Resource" : [
          for bucket in toset(var.bucket_names) : "arn:aws:s3:::${bucket}/*"
        ]
      }
    ]
  })
}

data "aws_iam_policy" "lambda_vpc_execution" {
  name = "AWSLambdaVPCAccessExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_vpc_execution" {
  role       = aws_iam_role.iam_for_lambda.name
  policy_arn = data.aws_iam_policy.lambda_vpc_execution.arn
}

resource "aws_cloudwatch_log_group" "lambda_promtail" {
  name              = "/aws/lambda/lambda_promtail"
  retention_in_days = 14
}

resource "aws_lambda_function" "lambda_promtail" {
  image_uri     = var.lambda_promtail_image
  function_name = "lambda_promtail"
  role          = aws_iam_role.iam_for_lambda.arn

  timeout      = 60
  memory_size  = 128
  package_type = "Image"

  # From the Terraform AWS Lambda docs: If both subnet_ids and security_group_ids are empty then vpc_config is considered to be empty or unset.
  vpc_config {
    # Every subnet should be able to reach an EFS mount target in the same Availability Zone. Cross-AZ mounts are not permitted.
    subnet_ids         = var.lambda_vpc_subnets
    security_group_ids = var.lambda_vpc_security_groups
  }

  environment {
    variables = {
      WRITE_ADDRESS = var.write_address
      USERNAME      = var.username
      PASSWORD      = var.password
      KEEP_STREAM   = var.keep_stream
      BATCH_SIZE    = var.batch_size
      EXTRA_LABELS  = var.extra_labels
      TENANT_ID     = var.tenant_id
    }
  }

  depends_on = [
    aws_iam_role_policy.logs,
    aws_iam_role_policy_attachment.lambda_vpc_execution,
  ]
}

resource "aws_lambda_function_event_invoke_config" "lambda_promtail_invoke_config" {
  function_name          = aws_lambda_function.lambda_promtail.function_name
  maximum_retry_attempts = 2
}

resource "aws_lambda_permission" "lambda_promtail_allow_cloudwatch" {
  statement_id  = "lambda-promtail-allow-cloudwatch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.lambda_promtail.function_name
  principal     = "logs.${data.aws_region.current.name}.amazonaws.com"
}

# This block allows for easily subscribing to multiple log groups via the `log_group_names` var.
# However, if you need to provide an actual filter_pattern for a specific log group you should
# copy this block and modify it accordingly.
resource "aws_cloudwatch_log_subscription_filter" "lambdafunction_logfilter" {
  for_each        = toset(var.log_group_names)
  name            = "lambdafunction_logfilter_${each.value}"
  log_group_name  = each.value
  destination_arn = aws_lambda_function.lambda_promtail.arn
  # required but can be empty string
  filter_pattern = ""
  depends_on     = [aws_iam_role_policy.logs]
}

resource "aws_lambda_permission" "allow-s3-invoke-lambda-promtail" {
  for_each      = toset(var.bucket_names)
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.lambda_promtail.arn
  principal     = "s3.amazonaws.com"
  source_arn    = "arn:aws:s3:::${each.value}"
}

resource "aws_s3_bucket_notification" "push-to-lambda-promtail" {
  for_each = toset(var.bucket_names)
  bucket   = each.value

  lambda_function {
    lambda_function_arn = aws_lambda_function.lambda_promtail.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "AWSLogs/"
    filter_suffix       = ".log.gz"
  }

  depends_on = [aws_lambda_permission.allow-s3-invoke-lambda-promtail]
}
