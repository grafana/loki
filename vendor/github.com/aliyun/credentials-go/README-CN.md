[English](README.md) | 简体中文

# Alibaba Cloud Credentials for Go

[![Latest Stable Version](https://badge.fury.io/gh/aliyun%2Fcredentials-go.svg)](https://badge.fury.io/gh/aliyun%2Fcredentials-go)
[![Go](https://github.com/aliyun/credentials-go/actions/workflows/go.yml/badge.svg)](https://github.com/aliyun/credentials-go/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/aliyun/credentials-go/branch/master/graph/badge.svg)](https://codecov.io/gh/aliyun/credentials-go)
[![License](https://poser.pugx.org/alibabacloud/credentials/license)](https://packagist.org/packages/alibabacloud/credentials)
[![Go Report Card](https://goreportcard.com/badge/github.com/aliyun/credentials-go)](https://goreportcard.com/report/github.com/aliyun/credentials-go)
[![Scrutinizer Code Quality](https://scrutinizer-ci.com/g/aliyun/credentials-go/badges/quality-score.png?b=master)](https://scrutinizer-ci.com/g/aliyun/credentials-go/?branch=master)

![Alibaba Cloud Logo](https://aliyunsdk-pages.alicdn.com/icons/AlibabaCloud.svg)

Alibaba Cloud Credentials for Go 是帮助 GO 开发者管理凭据的工具。

本文将介绍如何获取和使用 Alibaba Cloud Credentials for Go。

## 要求

- 请确保你的系统安装了 1.12.x 或更新版本的 Go 环境。

## 安装

使用 `go get` 下载安装

```sh
go get -u github.com/aliyun/credentials-go
```

## 快速使用

在您开始之前，您需要注册阿里云帐户并获取您的[凭证](https://usercenter.console.aliyun.com/#/manage/ak)。

### 凭证类型

#### 使用默认凭据链
当您在初始化凭据客户端不传入任何参数时，Credentials工具会使用默认凭据链方式初始化客户端。默认凭据的读取逻辑请参见[默认凭据链](#默认凭证提供程序链)。

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
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
  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### AccessKey

通过[用户信息管理][ak]设置 access_key，它们具有该账户完全的权限，请妥善保管。有时出于安全考虑，您不能把具有完全访问权限的主账户 AccessKey 交于一个项目的开发者使用，您可以[创建RAM子账户][ram]并为子账户[授权][permissions]，使用RAM子用户的 AccessKey 来进行API调用。

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // 设置凭证类型
    SetType("access_key").
    // 用户 AccessKey Id
    SetAccessKeyId("AccessKeyId").
    // 用户 AccessKey Secret
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
  accessSecret := credential.AccessKeySecret
  credentialType := credential.Type
  fmt.Println(accessKeyId, accessSecret, credentialType)
}
```

#### STS

通过安全令牌服务（Security Token Service，简称 STS），申请临时安全凭证（Temporary Security Credentials，简称 TSC），创建临时安全凭证。

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main() {
  config := new(credentials.Config).
    // 设置凭证类型
    SetType("sts").
    // 临时用户 AccessKey Id
    SetAccessKeyId("AccessKeyId").
    // 临时用户 AccessKey Secret
    SetAccessKeySecret("AccessKeySecret").
    // 临时用户 Security Token
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
  accessSecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type
  fmt.Println(accessKeyId, accessSecret, securityToken, credentialType)
}
```

#### AssumeRoleWithOIDC

在容器服务 Kubernetes 版中设置了Worker节点RAM角色后，对应节点内的Pod中的应用也就可以像ECS上部署的应用一样，通过元数据服务（Meta Data Server）获取关联角色的STS Token。但如果容器集群上部署的是不可信的应用（比如部署您的客户提交的应用，代码也没有对您开放），您可能并不希望它们能通过元数据服务获取Worker节点关联实例RAM角色的STS Token。为了避免影响云上资源的安全，同时又能让这些不可信的应用安全地获取所需的 STS Token，实现应用级别的权限最小化，您可以使用RRSA（RAM Roles for Service Account）功能。阿里云容器集群会为不同的应用Pod创建和挂载相应的服务账户OIDC Token文件，并将相关配置信息注入到环境变量中，Credentials工具通过获取环境变量的配置信息，调用STS服务的AssumeRoleWithOIDC - OIDC角色SSO时获取扮演角色的临时身份凭证接口换取绑定角色的STS Token。详情请参见[通过RRSA配置ServiceAccount的RAM权限实现Pod权限隔离](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/user-guide/use-rrsa-to-authorize-pods-to-access-different-cloud-services#task-2142941)。

``` go
package main

import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main() {
  config := new(credentials.Config).
    // 设置凭证类型
    SetType("oidc_role_arn").
    // OIDC提供商ARN，可以通过环境变量ALIBABA_CLOUD_OIDC_PROVIDER_ARN设置OidcProviderArn
    SetOIDCProviderArn("OIDCProviderArn").
    // OIDC Token文件路径，可以通过环境变量ALIBABA_CLOUD_OIDC_TOKEN_FILE设置OidcTokenFilePath
    SetOIDCTokenFilePath("OIDCTokenFilePath").
    // RAM角色名称ARN，可以通过环境变量ALIBABA_CLOUD_ROLE_ARN设置RoleArn
    SetRoleArn("RoleArn").
    // 角色会话名称，可以通过环境变量ALIBABA_CLOUD_ROLE_SESSION_NAME设置RoleSessionName
    SetRoleSessionName("RoleSessionName").
    // 设置更小的权限策略，非必填。示例值：{"Statement": [{"Action": ["*"],"Effect": "Allow","Resource": ["*"]}],"Version":"1"}
    SetPolicy("Policy").
    // 设置session过期时间，非必填。
    SetRoleSessionExpiration(3600).
    // 非必填，默认为sts.aliyuncs.com，建议使用Region化的STS域名，选择地理位置更接近的Region可以保证网络连通性，Region对应的域名请参考：https://api.aliyun.com/product/Sts
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
  accessSecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### RamRoleArn

通过指定[RAM角色][RAM Role]，让凭证自动申请维护 STS Token。你可以通过为 `Policy` 赋值来限制获取到的 STS Token 的权限。

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // 设置凭证类型
    SetType("ram_role_arn").
    // 用户 AccessKey Id
    SetAccessKeyId("AccessKeyId").
    // 用户 AccessKey Secret
    SetAccessKeySecret("AccessKeySecret").
    // 要扮演的RAM角色ARN，示例值：acs:ram::123456789012****:role/adminrole，可以通过环境变量ALIBABA_CLOUD_ROLE_ARN设置RoleArn
    SetRoleArn("RoleArn").
    // 角色会话名称，可以通过环境变量ALIBABA_CLOUD_ROLE_SESSION_NAME设置RoleSessionName
    SetRoleSessionName("RoleSessionName").
    // 设置更小的权限策略，非必填。示例值：{"Statement": [{"Action": ["*"],"Effect": "Allow","Resource": ["*"]}],"Version":"1"}
    SetPolicy("Policy").
    // 设置session过期时间，非必填。
    SetRoleSessionExpiration(3600).
    // 非必填，角色外部 ID，该参数为外部提供的用于表示角色的参数信息，主要功能是防止混淆代理人问题。更多信息请参考：https://help.aliyun.com/zh/ram/use-cases/use-externalid-to-prevent-the-confused-deputy-problem
    SetExternalId("ExternalId").
    // 非必填，默认为sts.aliyuncs.com，建议使用Region化的STS域名，选择地理位置更接近的Region可以保证网络连通性，Region对应的域名请参考：https://api.aliyun.com/product/Sts
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
  accessSecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### Credentials URI

通过指定提供凭证的自定义网络服务地址，让凭证自动申请维护 STS Token。

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // 设置凭证类型
    SetType("credentials_uri").
    // 凭证的 URI，格式为http://local_or_remote_uri/，可以通过环境变量ALIBABA_CLOUD_CREDENTIALS_URI设置CredentialsUri
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
  accessSecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### EcsRamRole

Credentials工具会自动获取ECS实例绑定的RAM角色，调用ECS的元数据服务（Meta Data Server）换取STS Token，完成凭据客户端初始化。ECI实例，容器服务 Kubernetes 版的Worker节点均支持绑定实例RAM角色。

```go
import (
  "fmt"

  "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // 设置凭证类型
    SetType("ecs_ram_role").
    // 选填，该ECS角色的角色名称，不填会自动获取，但是建议加上以减少请求次数，可以通过环境变量 ALIBABA_CLOUD_ECS_METADATA 设置 RoleName
    SetRoleName("RoleName").
    // 选填，推荐设置为 true，关闭 IMDS V1 的兜底能力，默认使用 IMDS V2（安全加固）。也可以通过环境变量 ALIBABA_CLOUD_IMDSV1_DISABLED 设置
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
  accessSecret := credential.AccessKeySecret
  securityToken := credential.SecurityToken
  credentialType := credential.Type

  fmt.Println(accessKeyId, accessKeySecret, securityToken, credentialType)
}
```

#### Bearer Token

目前只有云呼叫中心 CCC 这款产品支持 Bearer Token 的凭据初始化方式。

```go
import (
 "fmt"

 "github.com/aliyun/credentials-go/credentials"
)

func main(){
  config := new(credentials.Config).
    // 设置凭证类型
    SetType("bearer").
    // 填入您的Bearer Token
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

### 凭证提供程序链

当开发环境与生产环境使用的凭据类型不一致时，常见做法是在代码中获取当前环境信息，编写获取不同凭据的分支代码。借助Credentials工具的默认凭据链，您可以用同一套代码，通过程序之外的配置来控制不同环境下的凭据获取方式。当您使用 `NewCredential()` 初始化凭据客户端，且不传入任何参数时，阿里云SDK将会尝试按照如下顺序查找相关凭据信息。

#### 1. 使用环境变量

Credentials工具会优先在环境变量中获取凭据信息。

- 如果系统环境变量 `ALIBABA_CLOUD_ACCESS_KEY_ID`（密钥Key） 和 `ALIBABA_CLOUD_ACCESS_KEY_SECRET`（密钥Value） 不为空，Credentials工具会优先使用它们作为默认凭据。

- 如果系统环境变量 `ALIBABA_CLOUD_ACCESS_KEY_ID`（密钥Key）、`ALIBABA_CLOUD_ACCESS_KEY_SECRET`（密钥Value）、`ALIBABA_CLOUD_SECURITY_TOKEN`（Token）均不为空，Credentials工具会优先使用STS Token作为默认凭据。

### 2. 使用OIDC RAM角色
若不存在优先级更高的凭据信息，Credentials工具会在环境变量中获取如下内容：

`ALIBABA_CLOUD_ROLE_ARN`：RAM角色名称ARN；

`ALIBABA_CLOUD_OIDC_PROVIDER_ARN`：OIDC提供商ARN；

`ALIBABA_CLOUD_OIDC_TOKEN_FILE`：OIDC Token文件路径；

若以上三个环境变量都已设置内容，Credentials将会使用变量内容调用STS服务的[AssumeRoleWithOIDC - OIDC角色SSO时获取扮演角色的临时身份凭证](https://help.aliyun.com/zh/ram/developer-reference/api-sts-2015-04-01-assumerolewithoidc)接口换取STS Token作为默认凭据。

### 3. 使用 Aliyun CLI 工具的 config.json 配置文件

若不存在优先级更高的凭据信息，Credentials工具会优先在如下位置查找 `config.json` 文件是否存在：
Linux系统：`~/.aliyun/config.json`
Windows系统： `C:\Users\USER_NAME\.aliyun\config.json`
如果文件存在，程序将会使用配置文件中 `current` 指定的凭据信息初始化凭据客户端。当然，您也可以通过环境变量 `ALIBABA_CLOUD_PROFILE` 来指定凭据信息，例如设置 `ALIBABA_CLOUD_PROFILE` 的值为 `AK`。

在config.json配置文件中每个module的值代表了不同的凭据信息获取方式：

- AK：使用用户的Access Key作为凭据信息；
- RamRoleArn：使用RAM角色的ARN来获取凭据信息；
- EcsRamRole：利用ECS绑定的RAM角色来获取凭据信息；
- OIDC：通过OIDC ARN和OIDC Token来获取凭据信息；
- ChainableRamRoleArn：采用角色链的方式，通过指定JSON文件中的其他凭据，以重新获取新的凭据信息。

配置示例信息如下：

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

### 4. 使用配置文件
>
> 如果用户主目录存在默认文件 `~/.alibabacloud/credentials` （Windows 为 `C:\Users\USER_NAME\.alibabacloud\credentials`），程序会自动创建指定类型和名称的凭证。您也可通过环境变量 `ALIBABA_CLOUD_CREDENTIALS_FILE` 指定配置文件路径。如果文件存在，程序将会使用配置文件中 default 指定的凭据信息初始化凭据客户端。当然，您也可以通过环境变量 `ALIBABA_CLOUD_PROFILE` 来指定凭据信息，例如设置 `ALIBABA_CLOUD_PROFILE` 的值为 `client1`。

配置示例信息如下：

```ini
[default]
type = access_key                  # 认证方式为 access_key
access_key_id = foo                # Key
access_key_secret = bar            # Secret

[project1]
type = ecs_ram_role                # 认证方式为 ecs_ram_role
role_name = EcsRamRoleTest         # Role Name，非必填，不填则自动获取，建议设置，可以减少网络请求。

[project2]
type = ram_role_arn                # 认证方式为 ram_role_arn
access_key_id = foo
access_key_secret = bar
role_arn = role_arn
role_session_name = session_name

[project3]
type=oidc_role_arn                 # 认证方式为 oidc_role_arn
oidc_provider_arn=oidc_provider_arn
oidc_token_file_path=oidc_token_file_path
role_arn=role_arn
role_session_name=session_name
```

### 5. 使用 ECS 实例RAM角色

如果定义了环境变量 `ALIBABA_CLOUD_ECS_METADATA` 且不为空，程序会将该环境变量的值作为角色名称，请求 `http://100.100.100.200/latest/meta-data/ram/security-credentials/` 获取临时安全凭证作为默认凭证。

### 6. 使用外部服务 Credentials URI

若不存在优先级更高的凭据信息，Credentials工具会在环境变量中获取ALIBABA_CLOUD_CREDENTIALS_URI，若存在，程序将请求该URI地址，获取临时安全凭证作为默认凭据信息。

外部服务响应结构应如下：

```json
{
  "Code": "Success",
  "AccessKeyId": "AccessKeyId",
  "AccessKeySecret": "AccessKeySecret",
  "SecurityToken": "SecurityToken",
  "Expiration": "2024-10-26T03:46:38Z"
}
```

## 许可证

[Apache-2.0](/LICENSE)

Copyright (c) 2009-present, Alibaba Cloud All rights reserved.

[ak]: https://usercenter.console.aliyun.com/#/manage/ak
[ram]: https://ram.console.aliyun.com/users
[permissions]: https://ram.console.aliyun.com/permissions
[RAM Role]: https://ram.console.aliyun.com/#/role/list
