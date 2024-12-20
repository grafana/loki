terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.81.0"
    }

    random = {
      source  = "hashicorp/random"
      version = "3.6.3"
    }
  }

  required_version = "> 1.2.0"
}
