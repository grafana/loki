variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-2"
}

variable "bucket_name" {
  description = "Bucket name for Loki storage"
  type    = string
  default = "loki-data"
}

variable "cluster_name" {
  description = "Name of EKS cluster"
  type = string
}

variable "namespace" {
  description = "Namespace of Loki installation"
  type        = string
  default     = "default"
}

variable "serviceaccount" {
  description = "Service account of Loki installation"
  type        = string
  default     = "loki"
}
