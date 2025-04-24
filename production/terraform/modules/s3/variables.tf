variable "region" {
  description = "AWS region"
  type        = string
  default     = "deprecated"
  validation {
    condition = var.region != "deprecated"
    error_message = <<-EOT
    Passing in an AWS region is deprecated.  In order to create the bucket
    in a region other than the default specified in your aws proder, pass
    in a reference to a provider with the desired region using a providers
    block.  For more information, see the Terraform documentation at
    https://developer.hashicorp.com/terraform/language/meta-arguments/module-providers
    EOT
  }
}

variable "bucket_name" {
  description = "Bucket name for Loki storage"
  type    = string
}

variable "cluster_name" {
  description = "Name of EKS cluster"
  type = string
}

variable "namespace" {
  description = "Namespace of Loki installation"
  type        = string
}

variable "serviceaccount" {
  description = "Service account of Loki installation"
  type        = string
  default     = "loki"
}
