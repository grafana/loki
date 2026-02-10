terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.31.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.8.1"
    }
  }

  required_version = "> 1.2.0"
}
