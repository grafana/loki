output "annotation" {
  description = "Service account annotation"
  value       = "eks.amazonaws.com/role-arn=${aws_iam_role.loki.arn}" 
}
