# Troubleshoot Azure Identity authentication issues

`azidentity` credential types return errors when authentication fails or they're otherwise unable to authenticate. This troubleshooting guide describes steps you can take to resolve such errors.

## Table of contents

- [Troubleshoot Default Azure Credential authentication issues](#troubleshoot-default-azure-credential-authentication-issues)
- [Troubleshoot environment credential authentication issues](#troubleshoot-environment-credential-authentication-issues)
- [Troubleshoot service principal authentication issues](#troubleshoot-service-principal-authentication-issues)
- [Troubleshoot user password authentication issues](#troubleshoot-user-password-authentication-issues)
- [Troubleshoot Managed Identity authentication issues](#troubleshoot-managed-identity-authentication-issues)
- [Troubleshoot Azure CLI authentication issues](#troubleshoot-azure-cli-authentication-issues)

## Troubleshoot Default Azure Credential authentication issues

`DefaultAzureCredential` attempts to retrieve an access token by sequentially invoking a chain of credentials. An error from this credential signifies that every credential in the chain failed to acquire a token. To address this, follow the configuration instructions for the respective credential you intend to use. This document contains more guidance for handling errors from each credential type:

-  [Environment credential](#troubleshoot-environment-credential-authentication-issues)
-  [Managed Identity](#troubleshoot-managed-identity-authentication-issues)
-  [Azure CLI](#troubleshoot-azure-cli-authentication-issues)

## Troubleshoot environment credential authentication issues

### Environment variables not configured

`EnvironmentCredential` supports service principal and user password authentication. `NewEnvironmentCredential()` returns an error when environment configuration is incomplete. To fix this, set the appropriate environment variables for the identity you want to authenticate:

#### Service principal with secret

| Variable name | Value |
| --- | --- |
`AZURE_CLIENT_ID` | ID of an Azure AD app. |
`AZURE_TENANT_ID` | ID of the app's Azure AD tenant. |
`AZURE_CLIENT_SECRET` | One of the app's client secrets. |

#### Service principal with certificate

| Variable name | Value |
| --- | --- |
`AZURE_CLIENT_ID` | ID of an Azure AD app. |
`AZURE_TENANT_ID` | ID of the app's Azure AD tenant. |
`AZURE_CLIENT_CERTIFICATE_PATH` | Path to a PEM-encoded or PKCS12 certificate file including private key (without password protection). |

#### User password

| Variable name | Value |
| --- | --- |
`AZURE_CLIENT_ID` | ID of an Azure AD app. |
`AZURE_USERNAME` | A username (usually an email address). |
`AZURE_PASSWORD` | The associated password for the given username. |

### Authentication failures

`EnvironmentCredential` supports service principal and user password authentication. Follow the troubleshooting guidelines below for the respective authentication method.

- [service principal](#troubleshoot-service-principal-authentication-issues)
- [user password](#troubleshoot-user-password-authentication-issues)

## Troubleshoot user password authentication issues

### Two-factor authentication required

`UsernamePasswordCredential` isn't compatible with any kind of multifactor authentication.

## Troubleshoot service principal authentication issues

### Create a new service principal

To create a new service principal, follow the instructions at [Create an Azure service principal with the Azure CLI](https://docs.microsoft.com/cli/azure/create-an-azure-service-principal-azure-cli).

### Invalid arguments

#### Client ID

Authenticating a service principal requires a client or "application" ID and tenant ID. These are required parameters for `NewClientSecretCredential()` and `NewClientCertificateCredential()`. If you've already created your service principal, you can retrieve these IDs by following the instructions at [Get tenant and app ID values for signing in](https://docs.microsoft.com/azure/active-directory/develop/howto-create-service-principal-portal#get-tenant-and-app-id-values-for-signing-in).

#### Client secret

A client secret, also called an application password, is a secret string that an app uses to prove its identity. Azure AD doesn't expose the values of existing secrets. If you've already created a service principal, follow the instructions at [Create a new app secret](https://docs.microsoft.com/azure/active-directory/develop/howto-create-service-principal-portal#option-2-create-a-new-application-secret) to create a new secret.

### Client certificate credential issues

`ClientCertificateCredential` authenticates with a certificate in PKCS12 (PFX) or PEM format. The certificate must first be registered for your service principal. To register a certificate, see [Upload a certificate](https://docs.microsoft.com/azure/active-directory/develop/howto-create-service-principal-portal#option-1-upload-a-certificate).

## Troubleshoot managed identity authentication issues

### Managed identity unavailable

[Managed identity authentication](https://docs.microsoft.com/azure/active-directory/managed-identities-azure-resources/overview) requires support from the hosting environment, which may require configuration to expose a managed identity to your app. `azidentity` has been tested with managed identities on these Azure services:

- [Azure App Service](https://docs.microsoft.com/azure/app-service/overview-managed-identity)
- [Azure Arc](https://docs.microsoft.com/azure/azure-arc/servers/managed-identity-authentication)
- [Azure Cloud Shell](https://docs.microsoft.com/azure/cloud-shell/overview)
- [Azure Kubernetes Service](https://docs.microsoft.com/azure/aks/use-managed-identity)
- [Azure Service Fabric](https://docs.microsoft.com/azure/service-fabric/configure-existing-cluster-enable-managed-identity-token-service)
- [Azure Virtual Machines](https://docs.microsoft.com/azure/active-directory/managed-identities-azure-resources/qs-configure-portal-windows-vm)

## Troubleshoot Azure CLI authentication issues

### Azure CLI not installed

The Azure CLI must be installed and on the app's path. To install it, see [How to install the Azure CLI](https://docs.microsoft.com/cli/azure/install-azure-cli). Then try authenticating again.

### Azure account not logged in

`AzureCLICredential` authenticates as the identity currently logged in to Azure CLI. You need to log in to your account in Azure CLI via the `az login` command. You can read further instructions to [Sign in with Azure CLI](https://docs.microsoft.com/cli/azure/authenticate-azure-cli). Once logged in, try running the credential again.
