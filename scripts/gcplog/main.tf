terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
      version = "3.5.0"
    }
  }
}

variable "credentials_file" {}
variable "zone" {}
variable "region" {}
variable "project" {}
variable "logname" {
  default = "cloud-logs"
}

provider "google" {
  credentials = file(var.credentials_file)
  project = var.project
  zone = var.zone
  region= var.region

}

resource "google_pubsub_topic" "cloud-logs" {
  name= var.logname
}

resource "google_logging_project_sink" "cloud-logs" {
  name = var.logname
  destination = "pubsub.googleapis.com/projects/${var.project}/topics/${var.logname}"
  filter = "resource.type = http_load_balancer AND httpRequest.status >= 200"
  unique_writer_identity = true
}

resource "google_pubsub_subscription" "coud-logs" {
  name = var.logname
  topic = google_pubsub_topic.cloud-logs.name
}
