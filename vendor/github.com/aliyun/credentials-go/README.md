English | [简体中文](README-CN.md)

# Alibaba Cloud Credentials for Go

[![Latest Stable Version](https://badge.fury.io/gh/aliyun%2Fcredentials-go.svg)](https://badge.fury.io/gh/aliyun%2Fcredentials-go)
[![Go](https://github.com/aliyun/credentials-go/actions/workflows/go.yml/badge.svg)](https://github.com/aliyun/credentials-go/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/aliyun/credentials-go/branch/master/graph/badge.svg)](https://codecov.io/gh/aliyun/credentials-go)
[![License](https://poser.pugx.org/alibabacloud/credentials/license)](https://packagist.org/packages/alibabacloud/credentials)
[![Go Report Card](https://goreportcard.com/badge/github.com/aliyun/credentials-go)](https://goreportcard.com/report/github.com/aliyun/credentials-go)
[![Scrutinizer Code Quality](https://scrutinizer-ci.com/g/aliyun/credentials-go/badges/quality-score.png?b=master)](https://scrutinizer-ci.com/g/aliyun/credentials-go/?branch=master)

![Alibaba Cloud Logo](https://aliyunsdk-pages.alicdn.com/icons/AlibabaCloud.svg)

Alibaba Cloud Credentials for Go is a tool for Go developers to manage credentials.

This document introduces how to obtain and use Alibaba Cloud Credentials for Go.

## Requirements

- It's necessary for you to make sure your system have installed a Go environment which is 1.12.x or newer.

## Installation

Use `go get` to install SDK：

```sh
go get -u github.com/aliyun/credentials-go
```

## Quick Examples

Before you begin, you need to sign up for an Alibaba Cloud account and retrieve your [Credentials](https://usercenter.console.aliyun.com/#/manage/ak).

### Credential Type

#### Default credential provider chain

If you do not specify a method to initialize a Credentials client, the default credential provider chain is used. For more information, see the Default credential provider chain section of this topic.

```go
package main

import (
	"fmt"

	"github.com/aliyun/credentials-go/credentials"
)

func main() {
	provider, err := credentials.NewCredential(nil)
	if err != nil {
		return
	}
	credential, err := provider.GetCredential()
	if err != nil {
		return
	}
	accessKeyId := credential.AccessKeyId
	accessSecret := credential.AccessKeySecret
	securityToken := credential.SecurityToken
	credentialType := credential.Type
	fmt.Println(*accessKeyId, *accessSecret, *securityToken, *credentialType)
}
```

#### AccessKey

Setup access_key credential through [User Information Management][ak], it have full authority over the account, please keep it safe. Sometimes for security reasons, you cannot hand over a primary account AccessKey with full access to the developer of a project. You may create a sub-account [RAM Sub-account][ram] , grant its [authorization][permissions]，and use the AccessKey of RAM Sub-account.

```go
package main

import (
	"fmt"

	"github.com/aliyun/credentials-go/credentials"
)

func main() {
	config := new(credentials.Config).
		// Which type of credential you want
		SetType("access_key").
		// AccessKeyId of your account
		SetAccessKeyId("AccessKeyId").
		// AccessKeySecret of your account
		SetAccessKeySecret("AccessKeySecret")

	provider, err := credentials.NewCredential(config)
	if err != nil {
		return
	}

	credential, err := provider.GetCredential()
	if err != nil {
		return
	}

	accessKeyId := credential.AccessKeyId
	accessKeySecret := credential.AccessKeySecret
	credentialType := credential.Type
	fmt.Println(*accessKeyId, *accessKeySecret, *credentialType)
}
```

#### STS

Create a temporary security credential by applying Temporary Security Credentials (TSC) through the Security Token Service (STS).

```go
package main

import (
	"fmt"

	"github.com/aliyun/credentials-go/credentials"
)

func main() {
	config := new(credentials.Config).
		// Which type of credential you want
		SetType("sts").
		// AccessKeyId of your account
		SetAccessKeyId("AccessKeyId").
		// AccessKeySecret of your account
		SetAccessKeySecret("AccessKeySecret").
		// Temporary Security Token
		SetSecurityToken("SecurityToken")

	provider, err := credentials.NewCredential(config)
	if err != nil {
		return
	}

	credential, err := provider.GetCredential()
	if err != nil {
		return
	}

	accessKeyId := credential.AccessKeyId
	accessKeySecret := credential.AccessKeySecret
	securityToken := credential.SecurityToken
	credentialType := credential.Type

	fmt.Println(*accessKeyId, *accessKeySecret, *securityToken, *credentialType)
}
```

#### AssumeRoleWithOIDC

After you attach a RAM role to a worker node in an Container Service for Kubernetes, applications in the pods on the worker node can use the metadata server to obtain an STS token the same way in which applications on ECS instances do. However, if an untrusted application is deployed on the worker node, such as an application that is submitted by your customer and whose code is unavailable to you, you may not want the application to use the metadata server to obtain an STS token of the RAM role attached to the worker node. To ensure the security of cloud resources and enable untrusted applications to securely obtain required STS tokens, you can use the RAM Roles for Service Accounts (RRSA) feature to grant minimum necessary permissions to an application. In this case, the ACK cluster creates a service account OpenID Connect (OIDC) token file, associates the token file with a pod, and then injects relevant environment variables into the pod. Then, the Credentials tool uses the environment variables to call the AssumeRoleWithOIDC operation of STS and obtains an STS token of the RAM role. For more information about the RRSA feature, see [Use RRSA to authorize different pods to access different cloud services](https://www.alibabacloud.com/help/en/ack/ack-managed-and-ack-dedicated/user-guide/use-rrsa-to-authorize-pods-to-access-different-cloud-services#task-2142941).

``` go
package main

import (
  "fmt"
  "net/http"

  "github.com/aliyun/credentials-go/credentials"
)

func main() {
  config := new(credentials.Config).
    SetType("oidc_role_arn").
    // Specify the ARN of the OIDC IdP by specifying the ALIBABA_CLOUD_OIDC_PROVIDER_ARN environment variable.
    SetOIDCProviderArn("OIDCProviderArn").
    // Specify the path of the OIDC token file by specifying the ALIBABA_CLOUD_OIDC_TOKEN_FILE environment variable.
    SetOIDCTokenFilePath("OIDCTokenFilePath").
    // Specify the ARN of the RAM role by specifying the ALIBABA_CLOUD_ROLE_ARN environment variable.
    SetRoleArn("RoleArn").
    // Specify the role session name by specifying the ALIBABA_CLOUD_ROLE_SESSION_NAME environment variable.
    SetRoleSessionName("RoleSessionName").
    // Optional. Specify limited permissions for the RAM role. Example: {"Statement": [{"Action": ["*"],"Effect": "Allow","Resource": ["*"]}],"Version":"1"}.
    SetPolicy("Policy").
    // Optional. Specify the validity period of the session.
    SetRoleSessionExpiration(3600).
    // Optional. The default value is sts.aliyuncs.com. It is recommended to use a regionalized STS domain name. Selecting a region that is geographically closer can ensure network connectivity. For the domain name corresponding to the region, please refer to: https://api.alibabacloud.com/product/Sts
    SetSTSEndpoint("sts.cn-hangzhou.aliyuncs.com")

  provider, err := credentials.NewCredential(config)
  if err != nil {
    return
  }

  credential, err := provider.GetCredential()
  if err != nil {
    return
  }

  accessKeyId := credential.AccessKeyId
  accessKeySecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### RamRoleArn

By specifying [RAM Role][RAM Role], the credential will be able to automatically request maintenance of STS Token. If you want to limit the permissions([How to make a policy][policy]) of STS Token, you can assign value for `Policy`.

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // Which type of credential you want
    SetType("ram_role_arn").
    // AccessKeyId of your account
    SetAccessKeyId("AccessKeyId").
    // AccessKeySecret of your account
    SetAccessKeySecret("AccessKeySecret").
    // Specify the ARN of the RAM role to be assumed. Example: acs:ram::123456789012****:role/adminrole.
    SetRoleArn("RoleArn").
    // Specify the name of the role session.
    SetRoleSessionName("RoleSessionName").
    // Optional. Specify limited permissions for the RAM role. Example: {"Statement": [{"Action": ["*"],"Effect": "Allow","Resource": ["*"]}],"Version":"1"}.
    SetPolicy("Policy").
    // Optional. Specify the expiration of the session
    SetRoleSessionExpiration(3600).
    // Optional, role external ID, this parameter is the parameter information provided externally to represent the role, and its main function is to prevent the confused deputy problem. For more information, please refer to: https://www.alibabacloud.com/help/en/ram/use-cases/use-externalid-to-prevent-the-confused-deputy-problem
    SetExternalId("ExternalId").
    // Optional. The default value is sts.aliyuncs.com. It is recommended to use a regionalized STS domain name. Selecting a region that is geographically closer can ensure network connectivity. For the domain name corresponding to the region, please refer to: https://api.alibabacloud.com/product/Sts
    SetSTSEndpoint("sts.cn-hangzhou.aliyuncs.com")

  provider, err := credentials.NewCredential(config)
  if err != nil {
    return
  }
  credential, err := provider.GetCredential()
  if err != nil {
    return
  }

  accessKeyId := credential.AccessKeyId
  accessKeySecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### Credentials URI

By specifying the url, the credential will be able to automatically request maintenance of STS Token.

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    SetType("credentials_uri").
    // Format: http url. `credentialsURI` can be replaced by setting environment variable: ALIBABA_CLOUD_CREDENTIALS_URI
    SetURL("http://127.0.0.1")
  provider, err := credentials.NewCredential(config)
  if err != nil {
    return
  }

  credential, err := provider.GetCredential()
  if err != nil {
    return
  }

  accessKeyId := credential.AccessKeyId
  accessKeySecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### EcsRamRole

The Credentials tool automatically obtains the RAM role attached to an ECS instance and uses the metadata server of ECS to obtain an STS token. The STS token is then used to initialize a Credentials client. You can also attach a RAM role to an elastic container instance or a worker node in an Alibaba Cloud Container Service for Kubernetes (ACK) cluster.

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // Which type of credential you want
    SetType("ecs_ram_role").
    // Optional. Specify the name of the RAM role of the ECS instance. If you do not specify this parameter, its value is automatically obtained. To reduce the number of requests, we recommend that you specify this parameter.
    SetRoleName("RoleName").
    // `DisableIMDSv1` is optional and is recommended to be turned on. It can be replaced by setting environment variable: ALIBABA_CLOUD_IMDSV1_DISABLED
    SetDisableIMDSv1(true)

  provider, err := credentials.NewCredential(config)
  if err != nil {
    return
  }

  credential, err := provider.GetCredential()
  if err != nil {
    return
  }
  accessKeyId := credential.AccessKeyId
  accessKeySecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### Bearer Token

If credential is required by the Cloud Call Centre (CCC), please apply for Bearer Token maintenance by yourself.

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // Which type of credential you want
    SetType("bearer").
    // BearerToken of your account
    SetBearerToken("BearerToken")

  provider, err := credentials.NewCredential(config)
  if err != nil {
    return
  }

  credential, err := provider.GetCredential()
  if err != nil {
    return
  }

  bearerToken := credential.BearerToken
  credentialType := credential.Type
  fmt.Println(bearerToken, credentialType)
}
```

### Credential Provider Chain

If you want to use different types of credentials in the development and production environments of your application, you generally need to obtain the environment information from the code and write code branches to obtain different credentials for the development and production environments. The default credential provider chain of the Credentials tool allows you to use the same code to obtain credentials for different environments based on configurations independent of the application. If you call `NewCredential()` with nil, it will use provider chain to get credential for you.

### 1. Environmental certificate

Look for environment credentials in environment variable.
- If the `ALIBABA_CLOUD_ACCESS_KEY_ID` and `ALIBABA_CLOUD_ACCESS_KEY_SECRET` environment variables are defined and are not empty, the program will use them to create default credentials.
- If the `ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET` and `ALIBABA_CLOUD_SECURITY_TOKEN` environment variables are defined and are not empty, the program will use them to create temporary security credentials(STS). Note: This token has an expiration time, it is recommended to use it in a temporary environment.

### 2. The RAM role of an OIDC IdP

If no credentials are found in the previous step, the Credentials tool obtains the values of the following environment variables:

`ALIBABA_CLOUD_ROLE_ARN`: the ARN of the RAM role.

`ALIBABA_CLOUD_OIDC_PROVIDER_ARN`: the ARN of the OIDC IdP.

`ALIBABA_CLOUD_OIDC_TOKEN_FILE`: the path of the OIDC token file.

If the preceding three environment variables are specified, the Credentials tool uses the environment variables to call the [AssumeRoleWithOIDC](https://www.alibabacloud.com/help/en/ram/developer-reference/api-sts-2015-04-01-assumerolewithoidc) operation of STS to obtain an STS token as the default credential.

### 3. Using the config.json Configuration File of Aliyun CLI Tool
If there is no higher-priority credential information, the Credentials tool will first check the following locations to see if the config.json file exists:

Linux system: `~/.aliyun/config.json`
Windows system: `C:\Users\USER_NAME\.aliyun\config.json`
If the file exists, the program will use the credential information specified by `current` in the configuration file to initialize the credentials client. Of course, you can also use the environment variable `ALIBABA_CLOUD_PROFILE` to specify the credential information, for example by setting the value of `ALIBABA_CLOUD_PROFILE` to `AK`.

In the config.json configuration file, the value of each module represents different ways to obtain credential information:

- AK: Use the Access Key of the user as credential information;
- RamRoleArn: Use the ARN of the RAM role to obtain credential information;
- EcsRamRole: Use the RAM role bound to the ECS to obtain credential information;
- OIDC: Obtain credential information through OIDC ARN and OIDC Token;
- ChainableRamRoleArn: Use the role chaining method to obtain new credential information by specifying other credentials in the JSON file.

The configuration example information is as follows:

```json
{
    "current": "AK",
    "profiles": [
        {
            "name": "AK",
            "mode": "AK",
            "access_key_id": "access_key_id",
            "access_key_secret": "access_key_secret"
        },
        {
            "name": "RamRoleArn",
            "mode": "RamRoleArn",
            "access_key_id": "access_key_id",
            "access_key_secret": "access_key_secret",
            "ram_role_arn": "ram_role_arn",
            "ram_session_name": "ram_session_name",
            "expired_seconds": 3600,
            "sts_region": "cn-hangzhou"
        },
        {
            "name": "EcsRamRole",
            "mode": "EcsRamRole",
            "ram_role_name": "ram_role_name"
        },
        {
            "name": "OIDC",
            "mode": "OIDC",
            "ram_role_arn": "ram_role_arn",
            "oidc_token_file": "path/to/oidc/file",
            "oidc_provider_arn": "oidc_provider_arn",
            "ram_session_name": "ram_session_name",
            "expired_seconds": 3600,
            "sts_region": "cn-hangzhou"
        },
        {
            "name": "ChainableRamRoleArn",
            "mode": "ChainableRamRoleArn",
            "source_profile": "AK",
            "ram_role_arn": "ram_role_arn",
            "ram_session_name": "ram_session_name",
            "expired_seconds": 3600,
            "sts_region": "cn-hangzhou"
        }
    ]
}
```

### 4. Configuration file
>
> If the user's home directory has the default file `~/.alibabacloud/credentials` (Windows is `C:\Users\USER_NAME\.alibabacloud\credentials`), the program will automatically create credentials with the specified type and name. You can also specify the configuration file path by configuring the `ALIBABA_CLOUD_CREDENTIALS_FILE` environment variable. If the configuration file exists, the application initializes a Credentials client by using the credential information that is specified by default in the configuration file. You can also configure the `ALIBABA_CLOUD_PROFILE` environment variable to modify the default credential information that is read.

Configuration example:
```ini
[default]
type = access_key                  # Authentication method is access_key
access_key_id = foo                # Key
access_key_secret = bar            # Secret

[project1]
type = ecs_ram_role                # Authentication method is ecs_ram_role
role_name = EcsRamRoleTest         # Role name, optional. It will be retrieved automatically if not set. It is highly recommended to set it up to reduce requests.

[project2]
type = ram_role_arn                # Authentication method is ram_role_arn
access_key_id = foo
access_key_secret = bar
role_arn = role_arn
role_session_name = session_name

[project3]
type=oidc_role_arn                 # Authentication method is oidc_role_arn
oidc_provider_arn=oidc_provider_arn
oidc_token_file_path=oidc_token_file_path
role_arn=role_arn
role_session_name=session_name
```

### 5. Instance RAM role

If the environment variable `ALIBABA_CLOUD_ECS_METADATA` is defined and not empty, the program will take the value of the environment variable as the role name and request `http://100.100.100.200/latest/meta-data/ram/security-credentials/` to get the temporary Security credentials are used as default credentials.

### 6. Using External Service Credentials URI

If there are no higher-priority credential information, the Credentials tool will obtain the `ALIBABA_CLOUD_CREDENTIALS_URI` from the environment variables. If it exists, the program will request the URI address to obtain temporary security credentials as the default credential information.

The external service response structure should be as follows:

```json
{
  "Code": "Success",
  "AccessKeyId": "AccessKeyId",
  "AccessKeySecret": "AccessKeySecret",
  "SecurityToken": "SecurityToken",
  "Expiration": "2024-10-26T03:46:38Z"
}
```

## License

[Apache-2.0](/LICENSE)

Copyright (c) 2009-present, Alibaba Cloud All rights reserved.

[ak]: https://usercenter.console.aliyun.com/#/manage/ak
[ram]: https://ram.console.aliyun.com/users
[policy]: https://www.alibabacloud.com/help/doc-detail/28664.htm?spm=a2c63.p38356.a3.3.27a63b01khWgdh
[permissions]: https://ram.console.aliyun.com/permissions
[RAM Role]: https://ram.console.aliyun.com/#/role/list
