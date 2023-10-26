---
title: Short-Lived Token Authentication
authors:
  - "@periklis"
reviewers:
  - "@xperimental"
  - "@JoaoBraveCoding"
  - "@btaani"
creation-date: 2023-10-26
last-updated: 2023-10-26
draft: false
menu:
  docs:
    parent: "enhancements"
weight: 100
toc: true
---

## Summary

Usually LokiStack's object storage access on major cloud providers is secured using a static service account that comprises of a set of client id and client secret. Although static service accounts provide a simple way to control access to the provider's resources centrally (e.g. access to S3 only) they impose a certain security risks lack of automatic ability for rotation and usually unset expiry. In addition the identity of any third-party gaining to the static service account cannot be controlled or tracked upfront.

Therefore all major cloud providers offer an OpenID Connect (OIDC) based workflow which ensures that requesting entity (e.g. LokiStack) must be associated with an IAM role and further trust relationships (e.g. mapping to Kubernetes ServiceAccount resources) upfront before gaining access to any cloud resources (e.g. S3). In addition such a workflow ensures handing out only short-lived token instead of real credentials to the requesting entity which need to be refreshed periodically. In turn this minimizes the security risk vector by a lot however imposes a higher administration effort for running LokiStack with that workflow.

__Note:__ Short Lived Token authentication is an arbitrary picked generic term that maps well enough to each cloud providers service offering:
- Azure: [Workload Identity Federation](https://learn.microsoft.com/en-us/entra/workload-id/workload-identity-federation)
- AWS: [Secure Service Token](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp.html)
- Google Cloud Platform: [Workload Identity Federation](https://cloud.google.com/iam/docs/workload-identity-federation)

## Motivation

Building upon the fact that short-lived token authentication is support by all three chunk storage clients in Loki (Azure, AWS, GAP), this proposal's main focus is to automate the administration effort to run LokiStack using such an authentication workflow for object storage access.

### Goals

- The LokiStack administrator provides only a minimal but valid object storage secrets that enable short-lived tokens authentication for all three major cloud providers.
- The LokiStack administrator is not required to expose credentials/identity-tokens/etc. to the cloud provider when operating in that workflow.

### Non-Goals

- Provide auxiliary and comprehensive tooling to automate preparing the cloud provider IAM resources for short-lived tokens authentication.

## Proposal

The following proposal describes the required changes and validations for each object storage secret for each major cloud provider (Azure, AWS, GCP).

### API Extensions

The implementation of this proposal does not require any custom resource extension or such.

### Implementation Details/Notes/Constraints

The following sections describe the required changes in LokiStack's object storage secret format for each chunk storage clients to enable short-lived token authentication

#### Azure Workload Identity Federation

##### Overview

The current Azure object storage secret requires the following mandatory set of fields to be available:

```yaml
data:
    environment:  # The Azure Storage account environment
    container:    # The Azure Storage account container
    account_name: # The Azure Storage account name
    account_key:  # The Azure Storage account key
```

In contrast a minimal configuration set of fields for short lived authentication requires:

```yaml
data:
    client_id: # The Azure Federated Identity ID
    tenant_id: # The Azure Federated Identity Tenant ID
```

#### AWS Secure Token Service

##### Overview

The current S3 object storage secret requires the following mandatory set of fields to be available:

```yaml
data:
  bucketnames:       # A comma-separated list of bucket names
  access_key_id:     # The AWS static service account's key ID
  access_key_secret: # The AWS static service account's key secret
  endpoint:          # The AWS endpoint URL.
```

In contrast a minimal configuration set of fields for short lived authentication requires:

```yaml
data:
  bucketnames: # A comma-separated list of bucket names
  region:      # A valid AWS region, e.g. us-east-1
  role_arn:    # The AWS IAM Role associated with a trust relationship to Lokistack's serviceaccount
```

##### Pre-requisites

The LokiStack administrator is required to execute a custom AWS IAM Role associated with a trust relationship to the LokiStack's Kubernetes ServiceAccount

1. Trust relationship: Ensures that each Lokistack container authenticating to AWS STS is using as identity it's serviceaccount token.

```json
{
 "Version": "2012-10-17",
 "Statement": [
   {
     "Effect": "Allow",
     "Principal": {
       "Federated": "arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"
     },
     "Action": "sts:AssumeRoleWithWebIdentity",
     "Condition": {
       "StringEquals": {
         "${OIDC_PROVIDER}:sub": [
           "system:serviceaccount:${LOKISTACK_NAMESPACE}:${LOKISTACK_SERVICE_ACCOUNT_NAME}"
         ]
       }
     }
   }
 ]
}
```

__Note:__ The Lokistack service account name is always the same as the LokiStack custom resource name.

2. Create an AWS IAM role:

```shell
$ aws iam create-role --role-name "my-lokistack-s3-access" --assume-role-policy-document file:///tmp/trust.json --query Role.Arn --output text
```

3. Attach a specific policy that role:

```shell
$ aws iam attach-role-policy --role-name "my-lokistack-s3-access" --policy-arn "arn:aws:iam::aws:policy/AmazonS3FullAccess"
```

#### GCP Workload Identity Federation

There no changed required for GCP WIF as a provider service for short-lived tokens authentication. However specific validation of the provided `serviceaccount.json` field will ensure that WIF is used only with GCP external serviceaccounts, i.e. the following format will be validated:

```json
{
   "type": "external_account",
   "audience": "//iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/test-pool/providers/test-provider",
   "subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
   "token_url": "https://sts.googleapis.com/v1/token",
   "service_account_impersonation_url": "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/test-service-account-42ssv@test-project.iam.gserviceaccount.com:generateAccessToken",
   "credential_source": {
      "file": "/path/to/oidc/token",
      "format": {
         "type": "text"
      }
   }
}
```

__Note:__ As a credentials source file the Loki Operator will overwrite any user provider input with using the Kubernetes ServiceAccount token per container.


## Implementation History

Major milestones in the life cycle of a proposal should be tracked in `Implementation
History`.
