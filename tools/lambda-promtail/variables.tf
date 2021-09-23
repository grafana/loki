variable "write_address" {
  type        = string
  description = "This is the Loki Write API compatible endpoint that you want to write logs to, either promtail or Loki."
  default     = "http://localhost:8080/loki/api/v1/push"
}

variable "log_group_names" {
  type        = list(string)
  description = "List of CloudWatch Log Group names to create Subscription Filters for."
  default     = [""]
}

variable "lambda_promtail_image" {
  type        = string
  description = "The ECR image URI to pull and use for lambda-promtail."
  default     = "grafana-lambda-promtail-ecr:latest"
}

variable "username" {
  type        = string
  description = "The basic auth username, necessarry if writing directly to Grafana Cloud Loki."
  default     = ""
}

variable "password" {
  type        = string
  description = "The basic auth password, necessarry if writing directly to Grafana Cloud Loki."
  sensitive   = true
  default     = ""
}
