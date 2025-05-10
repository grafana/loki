terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.96.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.7.2"
    }
  }

  required_version = "> 1.2.0"
}
