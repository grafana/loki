terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
      version = "6.0.1"
    }
  }
}

provider "google" {
  credentials = file(var.credentials_file)
  project = var.project
  zone = var.zone
  region= var.region
}

resource "google_pubsub_topic" "cloud-logs" {
  name = var.name
}

resource "google_logging_project_sink" "main" {
  name        = var.name
  destination = "pubsub.googleapis.com/projects/${var.project}/topics/${var.name}"
  filter      = var.inclusion_filter
  dynamic "exclusions" {
    for_each = var.exclusions
    content {
      name   = exclusions.value.name
      filter = exclusions.value.filter
    }
  }
  unique_writer_identity = true
}

resource "google_pubsub_subscription" "main" {
  name  = var.name
  topic = google_pubsub_topic.cloud-logs.name
}

resource "google_pubsub_topic_iam_binding" "log-writer" {
  topic = google_pubsub_topic.cloud-logs.name
  role  = "roles/pubsub.publisher"
  members = [
    google_logging_project_sink.main.writer_identity,
  ]
}

# Variables

variable "credentials_file" {}
variable "zone" {}
variable "region" {}
variable "project" {}

variable "name" {
  description = "Name of the gcplog setup"
  default     = "cloud-logs"
}

variable "project" {
  description = "Google cloud project name"
}

variable "inclusion_filter" {
  description = "Logs inclusion filter that tells what kind of logs should be ingested into pubsub topic"
  default     = "" // default no logs should be ingested
}

variable "exclusions" {
  default     = []
  description = "Logs exclusion filter that tells what kind of logs should be ignored"
  type = list(object({
    name   = string
    filter = string
  }))
}
