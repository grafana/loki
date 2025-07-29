terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.6.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.7.2"
    }
  }

  required_version = "> 1.2.0"
}
