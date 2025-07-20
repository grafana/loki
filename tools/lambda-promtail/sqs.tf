locals {
  bucket_arns = [for name in var.bucket_names : "arn:aws:s3:::${name}"]
}

resource "aws_s3_bucket_notification" "sqs" {
  for_each = var.sqs_enabled ? var.bucket_names : []
  bucket   = each.value
  queue {
    queue_arn     = aws_sqs_queue.main[0].arn
    events        = ["s3:ObjectCreated:*"]
    filter_suffix = ".log.gz"
    filter_prefix = "VPC_FL/"
  }
}

resource "aws_sqs_queue" "main" {
  count                      = var.sqs_enabled ? 1 : 0
  name                       = "${var.sqs_queue_name_prefix}-main-queue"
  sqs_managed_sse_enabled    = true
  policy                     = data.aws_iam_policy_document.queue_policy[0].json
  visibility_timeout_seconds = 300
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dead_letter[0].arn
    maxReceiveCount     = 5
  })
}

data "aws_iam_policy_document" "queue_policy" {
  count = var.sqs_enabled ? 1 : 0
  statement {
    actions = [
      "sqs:SendMessage"
    ]
    resources = ["arn:aws:sqs:*:*:${var.sqs_queue_name_prefix}-main-queue"]
    principals {
      type        = "Service"
      identifiers = ["s3.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = local.bucket_arns
    }
  }
}

resource "aws_sqs_queue" "dead_letter" {
  count                   = var.sqs_enabled ? 1 : 0
  name                    = "${var.sqs_queue_name_prefix}-dead-letter-queue"
  sqs_managed_sse_enabled = true
}

resource "aws_sqs_queue_redrive_allow_policy" "this" {
  count     = var.sqs_enabled ? 1 : 0
  queue_url = aws_sqs_queue.dead_letter[0].id

  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue",
    sourceQueueArns   = [aws_sqs_queue.main[0].arn]
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
