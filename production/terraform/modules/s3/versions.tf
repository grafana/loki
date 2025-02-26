terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.88.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.7.1"
    }
  }

  required_version = "> 1.2.0"
}
