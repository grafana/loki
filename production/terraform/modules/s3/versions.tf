terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.64.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.6.2"
    }
  }

  required_version = "> 1.2.0"
}
