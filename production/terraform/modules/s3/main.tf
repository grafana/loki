provider "aws" {
  region = var.region
}

data "aws_caller_identity" "current" {}

data "aws_eks_cluster" "current" {
  name = var.cluster_name
}

locals {
  oidc_id = replace(data.aws_eks_cluster.current.identity[0].oidc[0].issuer, "https://", "")
}

data "aws_iam_policy_document" "oidc" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_id}:sub"
      values   = ["system:serviceaccount:${var.namespace}:${var.serviceaccount}"]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_id}:aud"
      values   = ["sts.amazonaws.com"]
    }

    principals {
      identifiers = [
        "arn:aws:iam::${data.aws_caller_identity.current.account_id}:oidc-provider/${local.oidc_id}"
      ]
      type        = "Federated"
    }
  }
}

resource "aws_s3_bucket" "loki-data" {
  bucket = "${var.bucket_name}"
}

resource "aws_s3_bucket_policy" "grant-access" {
  bucket = aws_s3_bucket.loki-data.id
  policy = jsonencode({
    Version: "2012-10-17",
    Statement: [
        {
            Sid: "Statement1",
            Effect: "Allow",
            Principal: {
                AWS: aws_iam_role.loki.arn  
            },
            Action: [
              "s3:PutObject",
              "s3:GetObject",
              "s3:DeleteObject",
              "s3:ListBucket"
            ],
            Resource: [
	      aws_s3_bucket.loki-data.arn,
	      "${aws_s3_bucket.loki-data.arn}/*"
            ]
        }
    ]
  })
}

resource "aws_iam_role" "loki" {
  name               = "LokiStorage-${var.cluster_name}"
  assume_role_policy = data.aws_iam_policy_document.oidc.json

  inline_policy {}
}

resource "aws_iam_policy" "loki" {
  name        = "LokiStorageAccessPolicy-${var.bucket_name}"
  path        = "/"
  description = "Allows Loki to access bucket"

  policy = jsonencode({
    Version: "2012-10-17",
    Statement: [
        {
            Effect: "Allow",
            Action: [
		"s3:ListBucket",
		"s3:PutObject",
		"s3:GetObject",
		"s3:DeleteObject"
	    ],
            Resource: [
		    aws_s3_bucket.loki-data.arn,
		    "${aws_s3_bucket.loki-data.arn}/*"
	    ]
        }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "loki-attach" {
  role       = aws_iam_role.loki.name
  policy_arn = aws_iam_policy.loki.arn
}
