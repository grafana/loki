locals {
  bucket_arns       = [for name in var.bucket_names : "arn:aws:s3:::${name}"]
  queue_name_prefix = "s3-to-lambda-promtail"
}

resource "aws_s3_bucket_notification" "push-to-sqs" {
  for_each = var.sqs_enabled ? var.bucket_names : []
  bucket   = each.value
  queue {
    queue_arn     = aws_sqs_queue.main-queue[0].arn
    events        = ["s3:ObjectCreated:*"]
    filter_suffix = ".log.gz"
    filter_prefix = "VPC_FL/"
  }
}

resource "aws_sqs_queue" "main-queue" {
  count                      = var.sqs_enabled ? 1 : 0
  name                       = "${local.queue_name_prefix}-main-queue"
  sqs_managed_sse_enabled    = true
  policy                     = data.aws_iam_policy_document.queue-policy[0].json
  visibility_timeout_seconds = 300
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dead-letter-queue[0].arn
    maxReceiveCount     = 5
  })
}

data "aws_iam_policy_document" "queue-policy" {
  count = var.sqs_enabled ? 1 : 0
  statement {
    actions = [
      "sqs:SendMessage"
    ]
    resources = ["arn:aws:sqs:*:*:${local.queue_name_prefix}-main-queue"]
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = local.bucket_arns
    }
  }
}

resource "aws_sqs_queue" "dead-letter-queue" {
  count                   = var.sqs_enabled ? 1 : 0
  name                    = "${local.queue_name_prefix}-dead-letter-queue"
  sqs_managed_sse_enabled = true
}

resource "aws_sqs_queue_redrive_allow_policy" "from-dql-to-main" {
  count     = var.sqs_enabled ? 1 : 0
  queue_url = aws_sqs_queue.dead-letter-queue[0].id

  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.main-queue[0].arn]
  })
}

data "aws_iam_policy" "lambda_sqs_execution" {
  name = "AWSLambdaSQSQueueExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_sqs_execution" {
  count      = var.sqs_enabled ? 1 : 0
  role       = aws_iam_role.this.name
  policy_arn = data.aws_iam_policy.lambda_sqs_execution.arn
}
