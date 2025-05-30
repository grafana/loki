output "lambda_function_arn" {
  description = "The ARN of the Lambda function created to ingest logs to Loki. This is used to configure account-wide Log Subscription Policy which requires the arn of the Lambda function."
  value       = aws_lambda_function.this.arn
}
