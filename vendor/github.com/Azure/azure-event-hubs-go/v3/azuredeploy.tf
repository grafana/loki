provider "azuread" {
  version = "~> 0.6"
}

provider "azurerm" {
  version = "~> 1.34"
}

provider "random" {
  version = "~> 2.2"
}

variable "location" {
  # eastus support AAD authentication, which at the time of writing this is in preview.
  # see: https://docs.microsoft.com/en-us/azure/event-hubs/event-hubs-role-based-access-control
  description = "Azure datacenter to deploy to."
  default     = "eastus"
}

variable "eventhub_name_prefix" {
  description = "Input your unique Azure Service Bus Namespace name"
  default     = "azureehtests"
}

variable "resource_group_name_prefix" {
  description = "Resource group to provision test infrastructure in."
  default     = "eventhub-go-tests"
}

variable "azure_client_secret" {
  description = "(Optional) piped in from env var so .env will be updated if there is an existing client secret"
  default     = "foo"
}

# Data resources used to get SubID and Tennant Info
data "azurerm_client_config" "current" {
}

resource "random_string" "name" {
  length  = 8
  upper   = false
  special = false
  number  = false
}

# Create resource group for all of the things
resource "azurerm_resource_group" "test" {
  name     = "${var.resource_group_name_prefix}-${random_string.name.result}"
  location = var.location
}

# Create an Event Hub namespace for testing
resource "azurerm_eventhub_namespace" "test" {
  name                = "${var.eventhub_name_prefix}-${random_string.name.result}"
  location            = azurerm_resource_group.test.location
  resource_group_name = azurerm_resource_group.test.name
  sku                 = "standard"
}

resource "azurerm_storage_account" "test" {
  name                     = "${var.eventhub_name_prefix}${random_string.name.result}"
  resource_group_name      = azurerm_resource_group.test.name
  location                 = azurerm_resource_group.test.location
  account_replication_type = "LRS"
  account_tier             = "Standard"
}

# Generate a random secret fo the service principal
resource "random_string" "secret" {
  count   = data.azurerm_client_config.current.service_principal_application_id == "" ? 1 : 0
  length  = 32
  upper   = true
  special = true
  number  = true
}

// Application for AAD authentication
resource "azuread_application" "test" {
  count                      = data.azurerm_client_config.current.service_principal_application_id == "" ? 1 : 0
  name                       = "eventhubstest"
  homepage                   = "https://eventhubstest"
  identifier_uris            = ["https://eventhubstest"]
  reply_urls                 = ["https://eventhubstest"]
  available_to_other_tenants = false
  oauth2_allow_implicit_flow = true
}

# Create a service principal, which represents a linkage between the AAD application and the password
resource "azuread_service_principal" "test" {
  count          = data.azurerm_client_config.current.service_principal_application_id == "" ? 1 : 0
  application_id = azuread_application.test[0].application_id
}

# Create a new service principal password which will be the AZURE_CLIENT_SECRET env var
resource "azuread_service_principal_password" "test" {
  count                = data.azurerm_client_config.current.service_principal_application_id == "" ? 1 : 0
  service_principal_id = azuread_service_principal.test[0].id
  value                = random_string.secret[0].result
  end_date             = "2030-01-01T01:02:03Z"
}

# This provides the new AAD application the rights to managed the resource group
resource "azurerm_role_assignment" "service_principal_rg" {
  scope                = "subscriptions/${data.azurerm_client_config.current.subscription_id}/resourceGroups/${azurerm_resource_group.test.name}"
  role_definition_name = "Owner"
  principal_id         = data.azurerm_client_config.current.service_principal_application_id == "" ? azuread_service_principal.test[0].id : data.azurerm_client_config.current.service_principal_object_id
}

# This provides the new AAD application the rights to managed, send and receive from the Event Hubs instance
resource "azurerm_role_assignment" "service_principal_eh" {
	scope                = "subscriptions/${data.azurerm_client_config.current.subscription_id}/resourceGroups/${azurerm_resource_group.test.name}/providers/Microsoft.EventHub/namespaces/${azurerm_eventhub_namespace.test.name}"
	role_definition_name = "Azure Event Hubs Data Owner"
	principal_id         = data.azurerm_client_config.current.service_principal_application_id == "" ? azuread_service_principal.test[0].id : data.azurerm_client_config.current.service_principal_object_id
	depends_on 		     = [azurerm_eventhub_namespace.test]
}


output "TEST_EVENTHUB_RESOURCE_GROUP" {
  value = azurerm_resource_group.test.name
}

output "EVENTHUB_CONNECTION_STRING" {
  value     = "Endpoint=sb://${azurerm_eventhub_namespace.test.name}.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=${azurerm_eventhub_namespace.test.default_primary_key}"
  sensitive = true
}

output "EVENTHUB_NAMESPACE" {
  value = azurerm_eventhub_namespace.test.name
}

output "AZURE_SUBSCRIPTION_ID" {
  value = data.azurerm_client_config.current.subscription_id
}

output "TEST_EVENTHUB_LOCATION" {
  value = var.location
}

output "AZURE_TENANT_ID" {
  value = data.azurerm_client_config.current.tenant_id
}

output "AZURE_CLIENT_ID" {
  value = compact(
    concat(
      azuread_application.test.*.application_id,
      list(data.azurerm_client_config.current.client_id)
    )
  )[0]
}

output "AZURE_CLIENT_SECRET" {
  value = compact(
    concat(
      azuread_service_principal_password.test.*.value,
      list(var.azure_client_secret)
    )
  )[0]
  sensitive = true
}

output "STORAGE_ACCOUNT_NAME" {
  value = azurerm_storage_account.test.name
}
