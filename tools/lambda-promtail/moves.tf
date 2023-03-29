moved {
  from = aws_iam_role.iam_for_lambda
  to   = aws_iam_role.this
}

moved {
  from = aws_cloudwatch_log_group.lambda_promtail
  to   = aws_cloudwatch_log_group.this
}

moved {
  from = aws_lambda_function.lambda_promtail
  to   = aws_lambda_function.this
}

moved {
  from = aws_lambda_function_event_invoke_config.lambda_promtail_invoke_config
  to   = aws_lambda_function_event_invoke_config.this
}
