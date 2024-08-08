terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 4.15"
    }

    random = {
      source  = "hashicorp/random"
      version = ">= 3.1"
    }
  }

  required_version = "> 1.2.0"
}
