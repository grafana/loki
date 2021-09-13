provider "aws" {
  region = "us-east-2"
}

data "aws_region" "current" {}

resource "aws_iam_role" "iam_for_lambda" {
  name = "iam_for_lambda"

  assume_role_policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Principal": {
        "Service": "lambda.amazonaws.com"
      },
      "Effect": "Allow",
      "Sid": ""
    }
  ]
}
EOF
}

resource "aws_iam_role_policy" "logs" {
  name   = "lambda-logs"
  role   = aws_iam_role.iam_for_lambda.name
  policy = jsonencode({
    "Statement": [
      {
        "Action": [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ],
        "Effect": "Allow",
        "Resource": "arn:aws:logs:*:*:*",
      }
    ]
  })
}

resource "aws_lambda_function" "lambda_promtail" {
  image_uri = "your-image-ecr:latest"
  function_name = "lambda_promtail"
  role          = aws_iam_role.iam_for_lambda.arn

  timeout=60
  memory_size=128
  package_type="Image"

  # vpc_config {
  #   # Every subnet should be able to reach an EFS mount target in the same Availability Zone. Cross-AZ mounts are not permitted.
  #   subnet_ids         = [aws_subnet.subnet_for_lambda.id]
  #   security_group_ids = [aws_security_group.sg_for_lambda.id]
  # }

  environment {
    variables = {
      WRITE_ADDRESS = "http://localhost:8080/loki/api/v1/push"
    }
  }
}

resource "aws_lambda_function_event_invoke_config" "lambda_promtail_invoke_config" {
  function_name                = aws_lambda_function.lambda_promtail.function_name
  maximum_retry_attempts       = 2
}

resource "aws_lambda_permission" "lambda_promtail_allow_cloudwatch" {
  statement_id  = "lambda-promtail-allow-cloudwatch"
  action        = "lambda:InvokeFunction"
  function_name = "${aws_lambda_function.lambda_promtail.function_name}"
  principal     = "logs.${data.aws_region.current.name}.amazonaws.com"
}

resource "aws_cloudwatch_log_subscription_filter" "test_lambdafunction_logfilter" {
  name            = "test_lambdafunction_logfilter"
  log_group_name  = "/aws/lambda/some-lamda-log-group"
  destination_arn = "${aws_lambda_function.lambda_promtail.arn}"
  # required but can be empty string
  filter_pattern = ""
  depends_on = [aws_iam_role_policy.logs]
}