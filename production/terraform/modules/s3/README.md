# Terraform module to create a data bucket

## Usage

```hcl
module "loki_storage" {
  source   = "github.com/grafana/loki.git?depth=1&ref=main//production/terraform/modules/s3"

  namespace    = "loki"
  bucket_name  = random_id.loki_bucket_id.hex
  cluster_name = module.eks.cluster_name
}

resource "random_id" "loki_bucket_id" {
  prefix      = "loki-data-tf-${var.cluster_name}-"
  byte_length = 8  # note: .hex doubles this length
}
```

The `region` parameter has been deprecated, and will generate an error if
provided.  In order to create the S3 bucket in an AWS region other than the
region your default aws provider uses, pass in a regioned provider by alias.
More information is avaialble in the [Terraform
docs](https://developer.hashicorp.com/terraform/language/meta-arguments/module-providers).

Here's the same example with a specific region.

```hcl
provider "aws" {
  alias  = "ue2"
  region = "us-east-2"
}

module "loki_storage" {
  source   = "github.com/grafana/loki.git?depth=1&ref=main//production/terraform/modules/s3"
  providers {
    aws = aws.ue2
  }

  namespace    = "loki"
  bucket_name  = random_id.loki_bucket_id.hex
  cluster_name = module.eks.cluster_name
}

resource "random_id" "loki_bucket_id" {
  prefix      = "loki-data-tf-${var.cluster_name}-"
  byte_length = 8  # note: .hex doubles this length
}
```
