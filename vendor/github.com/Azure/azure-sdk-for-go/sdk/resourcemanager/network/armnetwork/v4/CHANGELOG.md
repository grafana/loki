# Release History

## 4.3.0 (2023-11-24)
### Features Added

- Support for test fakes and OpenTelemetry trace spans.


## 4.3.0-beta.1 (2023-10-09)
### Features Added

- Support for test fakes and OpenTelemetry trace spans.

## 4.2.0 (2023-09-22)
### Features Added

- New struct `BastionHostPropertiesFormatNetworkACLs`
- New struct `IPRule`
- New struct `VirtualNetworkGatewayAutoScaleBounds`
- New struct `VirtualNetworkGatewayAutoScaleConfiguration`
- New field `NetworkACLs`, `VirtualNetwork` in struct `BastionHostPropertiesFormat`
- New field `Size` in struct `FirewallPolicyPropertiesFormat`
- New field `Size` in struct `FirewallPolicyRuleCollectionGroupProperties`
- New field `DefaultOutboundAccess` in struct `SubnetPropertiesFormat`
- New field `AutoScaleConfiguration` in struct `VirtualNetworkGatewayPropertiesFormat`


## 4.1.0 (2023-08-25)
### Features Added

- New value `ApplicationGatewaySKUNameBasic` added to enum type `ApplicationGatewaySKUName`
- New value `ApplicationGatewayTierBasic` added to enum type `ApplicationGatewayTier`
- New enum type `SyncMode` with values `SyncModeAutomatic`, `SyncModeManual`
- New function `*LoadBalancersClient.MigrateToIPBased(context.Context, string, string, *LoadBalancersClientMigrateToIPBasedOptions) (LoadBalancersClientMigrateToIPBasedResponse, error)`
- New struct `MigrateLoadBalancerToIPBasedRequest`
- New struct `MigratedPools`
- New field `SyncMode` in struct `BackendAddressPoolPropertiesFormat`


## 4.0.0 (2023-07-11)
### Breaking Changes

- `ApplicationGatewayCustomErrorStatusCodeHTTPStatus499` from enum `ApplicationGatewayCustomErrorStatusCode` has been removed

### Features Added

- New enum type `AdminState` with values `AdminStateDisabled`, `AdminStateEnabled`
- New field `ResourceGUID` in struct `AdminPropertiesFormat`
- New field `ResourceGUID` in struct `AdminRuleCollectionPropertiesFormat`
- New field `DefaultPredefinedSSLPolicy` in struct `ApplicationGatewayPropertiesFormat`
- New field `ResourceGUID` in struct `ConnectivityConfigurationProperties`
- New field `ResourceGUID` in struct `DefaultAdminPropertiesFormat`
- New field `ResourceGUID` in struct `GroupProperties`
- New field `ResourceGUID` in struct `ManagerProperties`
- New field `ResourceGUID` in struct `SecurityAdminConfigurationPropertiesFormat`
- New field `AdminState` in struct `VirtualNetworkGatewayPropertiesFormat`


## 3.0.0 (2023-05-26)
### Breaking Changes

- Type of `EffectiveRouteMapRoute.Prefix` has been changed from `[]*string` to `*string`
- `LoadBalancerBackendAddressAdminStateDrain` from enum `LoadBalancerBackendAddressAdminState` has been removed
- Struct `PeerRouteList` has been removed
- Field `PeerRouteList` of struct `VirtualHubBgpConnectionsClientListAdvertisedRoutesResponse` has been removed
- Field `PeerRouteList` of struct `VirtualHubBgpConnectionsClientListLearnedRoutesResponse` has been removed

### Features Added

- New value `NetworkInterfaceAuxiliaryModeAcceleratedConnections` added to enum type `NetworkInterfaceAuxiliaryMode`
- New value `WebApplicationFirewallRuleTypeRateLimitRule` added to enum type `WebApplicationFirewallRuleType`
- New enum type `ApplicationGatewayFirewallRateLimitDuration` with values `ApplicationGatewayFirewallRateLimitDurationFiveMins`, `ApplicationGatewayFirewallRateLimitDurationOneMin`
- New enum type `ApplicationGatewayFirewallUserSessionVariable` with values `ApplicationGatewayFirewallUserSessionVariableClientAddr`, `ApplicationGatewayFirewallUserSessionVariableGeoLocation`, `ApplicationGatewayFirewallUserSessionVariableNone`
- New enum type `AzureFirewallPacketCaptureFlagsType` with values `AzureFirewallPacketCaptureFlagsTypeAck`, `AzureFirewallPacketCaptureFlagsTypeFin`, `AzureFirewallPacketCaptureFlagsTypePush`, `AzureFirewallPacketCaptureFlagsTypeRst`, `AzureFirewallPacketCaptureFlagsTypeSyn`, `AzureFirewallPacketCaptureFlagsTypeUrg`
- New enum type `NetworkInterfaceAuxiliarySKU` with values `NetworkInterfaceAuxiliarySKUA1`, `NetworkInterfaceAuxiliarySKUA2`, `NetworkInterfaceAuxiliarySKUA4`, `NetworkInterfaceAuxiliarySKUA8`, `NetworkInterfaceAuxiliarySKUNone`
- New enum type `PublicIPAddressDNSSettingsDomainNameLabelScope` with values `PublicIPAddressDNSSettingsDomainNameLabelScopeNoReuse`, `PublicIPAddressDNSSettingsDomainNameLabelScopeResourceGroupReuse`, `PublicIPAddressDNSSettingsDomainNameLabelScopeSubscriptionReuse`, `PublicIPAddressDNSSettingsDomainNameLabelScopeTenantReuse`
- New enum type `ScrubbingRuleEntryMatchOperator` with values `ScrubbingRuleEntryMatchOperatorEquals`, `ScrubbingRuleEntryMatchOperatorEqualsAny`
- New enum type `ScrubbingRuleEntryMatchVariable` with values `ScrubbingRuleEntryMatchVariableRequestArgNames`, `ScrubbingRuleEntryMatchVariableRequestCookieNames`, `ScrubbingRuleEntryMatchVariableRequestHeaderNames`, `ScrubbingRuleEntryMatchVariableRequestIPAddress`, `ScrubbingRuleEntryMatchVariableRequestJSONArgNames`, `ScrubbingRuleEntryMatchVariableRequestPostArgNames`
- New enum type `ScrubbingRuleEntryState` with values `ScrubbingRuleEntryStateDisabled`, `ScrubbingRuleEntryStateEnabled`
- New enum type `WebApplicationFirewallScrubbingState` with values `WebApplicationFirewallScrubbingStateDisabled`, `WebApplicationFirewallScrubbingStateEnabled`
- New function `*AzureFirewallsClient.BeginPacketCapture(context.Context, string, string, FirewallPacketCaptureParameters, *AzureFirewallsClientBeginPacketCaptureOptions) (*runtime.Poller[AzureFirewallsClientPacketCaptureResponse], error)`
- New function `*ClientFactory.NewVirtualApplianceConnectionsClient() *VirtualApplianceConnectionsClient`
- New function `NewVirtualApplianceConnectionsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*VirtualApplianceConnectionsClient, error)`
- New function `*VirtualApplianceConnectionsClient.BeginCreateOrUpdate(context.Context, string, string, string, VirtualApplianceConnection, *VirtualApplianceConnectionsClientBeginCreateOrUpdateOptions) (*runtime.Poller[VirtualApplianceConnectionsClientCreateOrUpdateResponse], error)`
- New function `*VirtualApplianceConnectionsClient.BeginDelete(context.Context, string, string, string, *VirtualApplianceConnectionsClientBeginDeleteOptions) (*runtime.Poller[VirtualApplianceConnectionsClientDeleteResponse], error)`
- New function `*VirtualApplianceConnectionsClient.Get(context.Context, string, string, string, *VirtualApplianceConnectionsClientGetOptions) (VirtualApplianceConnectionsClientGetResponse, error)`
- New function `*VirtualApplianceConnectionsClient.NewListPager(string, string, *VirtualApplianceConnectionsClientListOptions) *runtime.Pager[VirtualApplianceConnectionsClientListResponse]`
- New struct `AzureFirewallPacketCaptureFlags`
- New struct `AzureFirewallPacketCaptureRule`
- New struct `EffectiveRouteMapRouteList`
- New struct `FirewallPacketCaptureParameters`
- New struct `FirewallPacketCaptureParametersFormat`
- New struct `FirewallPolicyHTTPHeaderToInsert`
- New struct `GroupByUserSession`
- New struct `GroupByVariable`
- New struct `PolicySettingsLogScrubbing`
- New struct `PropagatedRouteTableNfv`
- New struct `RoutingConfigurationNfv`
- New struct `RoutingConfigurationNfvSubResource`
- New struct `VirtualApplianceAdditionalNicProperties`
- New struct `VirtualApplianceConnection`
- New struct `VirtualApplianceConnectionList`
- New struct `VirtualApplianceConnectionProperties`
- New struct `WebApplicationFirewallScrubbingRules`
- New field `HTTPHeadersToInsert` in struct `ApplicationRule`
- New field `EnableKerberos` in struct `BastionHostPropertiesFormat`
- New field `AuxiliarySKU` in struct `InterfacePropertiesFormat`
- New field `FileUploadEnforcement`, `LogScrubbing`, `RequestBodyEnforcement`, `RequestBodyInspectLimitInKB` in struct `PolicySettings`
- New field `PrivateEndpointLocation` in struct `PrivateEndpointConnectionProperties`
- New field `DomainNameLabelScope` in struct `PublicIPAddressDNSSettings`
- New field `InstanceName` in struct `VirtualApplianceNicProperties`
- New field `AdditionalNics`, `VirtualApplianceConnections` in struct `VirtualAppliancePropertiesFormat`
- New field `Value` in struct `VirtualHubBgpConnectionsClientListAdvertisedRoutesResponse`
- New field `Value` in struct `VirtualHubBgpConnectionsClientListLearnedRoutesResponse`
- New anonymous field `VirtualHubEffectiveRouteList` in struct `VirtualHubsClientGetEffectiveVirtualHubRoutesResponse`
- New anonymous field `EffectiveRouteMapRouteList` in struct `VirtualHubsClientGetInboundRoutesResponse`
- New anonymous field `EffectiveRouteMapRouteList` in struct `VirtualHubsClientGetOutboundRoutesResponse`
- New field `GroupByUserSession`, `RateLimitDuration`, `RateLimitThreshold` in struct `WebApplicationFirewallCustomRule`


## 2.2.1 (2023-04-14)
### Bug Fixes

- Fix serialization bug of empty value of `any` type.


## 2.2.0 (2023-03-24)
### Features Added

- New struct `ClientFactory` which is a client factory used to create any client in this module
- New value `ApplicationGatewayCustomErrorStatusCodeHTTPStatus400`, `ApplicationGatewayCustomErrorStatusCodeHTTPStatus404`, `ApplicationGatewayCustomErrorStatusCodeHTTPStatus405`, `ApplicationGatewayCustomErrorStatusCodeHTTPStatus408`, `ApplicationGatewayCustomErrorStatusCodeHTTPStatus499`, `ApplicationGatewayCustomErrorStatusCodeHTTPStatus500`, `ApplicationGatewayCustomErrorStatusCodeHTTPStatus503`, `ApplicationGatewayCustomErrorStatusCodeHTTPStatus504` added to enum type `ApplicationGatewayCustomErrorStatusCode`
- New enum type `WebApplicationFirewallState` with values `WebApplicationFirewallStateDisabled`, `WebApplicationFirewallStateEnabled`
- New field `AuthorizationStatus` in struct `ExpressRouteCircuitPropertiesFormat`
- New field `IPConfigurationID` in struct `VPNGatewaysClientBeginResetOptions`
- New field `FlowLogs` in struct `VirtualNetworkPropertiesFormat`
- New field `State` in struct `WebApplicationFirewallCustomRule`


## 2.1.0 (2022-12-23)
### Features Added

- New struct `DelegationProperties`
- New struct `PartnerManagedResourceProperties`
- New field `VirtualNetwork` in struct `BackendAddressPoolPropertiesFormat`
- New field `CustomBlockResponseBody` in struct `PolicySettings`
- New field `CustomBlockResponseStatusCode` in struct `PolicySettings`
- New field `Delegation` in struct `VirtualAppliancePropertiesFormat`
- New field `DeploymentType` in struct `VirtualAppliancePropertiesFormat`
- New field `PartnerManagedResource` in struct `VirtualAppliancePropertiesFormat`


## 2.0.1 (2022-10-14)
### Others Changes
- Update live test dependencies

## 2.0.0 (2022-09-29)
### Breaking Changes

- Const `DdosCustomPolicyProtocolSyn` has been removed
- Const `DdosCustomPolicyTriggerSensitivityOverrideHigh` has been removed
- Const `DdosSettingsProtectionCoverageBasic` has been removed
- Const `DdosCustomPolicyProtocolUDP` has been removed
- Const `DdosCustomPolicyProtocolTCP` has been removed
- Const `DdosCustomPolicyTriggerSensitivityOverrideLow` has been removed
- Const `DdosCustomPolicyTriggerSensitivityOverrideDefault` has been removed
- Const `DdosSettingsProtectionCoverageStandard` has been removed
- Const `DdosCustomPolicyTriggerSensitivityOverrideRelaxed` has been removed
- Type alias `DdosSettingsProtectionCoverage` has been removed
- Type alias `DdosCustomPolicyTriggerSensitivityOverride` has been removed
- Type alias `DdosCustomPolicyProtocol` has been removed
- Function `PossibleDdosCustomPolicyProtocolValues` has been removed
- Function `PossibleDdosSettingsProtectionCoverageValues` has been removed
- Function `PossibleDdosCustomPolicyTriggerSensitivityOverrideValues` has been removed
- Struct `ProtocolCustomSettingsFormat` has been removed
- Field `PublicIPAddresses` of struct `DdosCustomPolicyPropertiesFormat` has been removed
- Field `ProtocolCustomSettings` of struct `DdosCustomPolicyPropertiesFormat` has been removed
- Field `DdosCustomPolicy` of struct `DdosSettings` has been removed
- Field `ProtectedIP` of struct `DdosSettings` has been removed
- Field `ProtectionCoverage` of struct `DdosSettings` has been removed

### Features Added

- New const `ApplicationGatewayWafRuleStateTypesEnabled`
- New const `RouteMapMatchConditionNotEquals`
- New const `ActionTypeBlock`
- New const `RouteMapActionTypeUnknown`
- New const `GeoAFRI`
- New const `IsWorkloadProtectedFalse`
- New const `ApplicationGatewayRuleSetStatusOptionsDeprecated`
- New const `ApplicationGatewayWafRuleActionTypesAllow`
- New const `RouteMapActionTypeRemove`
- New const `ApplicationGatewayClientRevocationOptionsNone`
- New const `NextStepContinue`
- New const `SlotTypeProduction`
- New const `NetworkIntentPolicyBasedServiceAllowRulesOnly`
- New const `ApplicationGatewayTierTypesWAFV2`
- New const `ActionTypeLog`
- New const `CommissionedStateDeprovisioned`
- New const `RouteMapMatchConditionEquals`
- New const `GeoOCEANIA`
- New const `GeoGLOBAL`
- New const `WebApplicationFirewallTransformUppercase`
- New const `NextStepUnknown`
- New const `ApplicationGatewayTierTypesWAF`
- New const `ApplicationGatewayWafRuleActionTypesNone`
- New const `CustomIPPrefixTypeSingular`
- New const `GeoME`
- New const `GeoLATAM`
- New const `ApplicationGatewayWafRuleActionTypesBlock`
- New const `ApplicationGatewayRuleSetStatusOptionsGA`
- New const `RouteMapMatchConditionUnknown`
- New const `ApplicationGatewayWafRuleStateTypesDisabled`
- New const `ApplicationGatewayTierTypesStandardV2`
- New const `VnetLocalRouteOverrideCriteriaEqual`
- New const `ManagedRuleEnabledStateEnabled`
- New const `RouteMapMatchConditionContains`
- New const `DdosSettingsProtectionModeDisabled`
- New const `ActionTypeAnomalyScoring`
- New const `ActionTypeAllow`
- New const `SlotTypeStaging`
- New const `GeoAQ`
- New const `RouteMapMatchConditionNotContains`
- New const `ApplicationGatewayClientRevocationOptionsOCSP`
- New const `RouteMapActionTypeReplace`
- New const `GeoNAM`
- New const `CustomIPPrefixTypeChild`
- New const `GeoEURO`
- New const `ExpressRoutePortsBillingTypeMeteredData`
- New const `GeoAPAC`
- New const `CustomIPPrefixTypeParent`
- New const `VnetLocalRouteOverrideCriteriaContains`
- New const `DdosSettingsProtectionModeVirtualNetworkInherited`
- New const `ApplicationGatewayWafRuleActionTypesLog`
- New const `ApplicationGatewayWafRuleActionTypesAnomalyScoring`
- New const `ApplicationGatewayRuleSetStatusOptionsSupported`
- New const `ExpressRoutePortsBillingTypeUnlimitedData`
- New const `DdosSettingsProtectionModeEnabled`
- New const `IsWorkloadProtectedTrue`
- New const `ApplicationGatewayRuleSetStatusOptionsPreview`
- New const `RouteMapActionTypeDrop`
- New const `ApplicationGatewayTierTypesStandard`
- New const `NextStepTerminate`
- New const `RouteMapActionTypeAdd`
- New type alias `DdosSettingsProtectionMode`
- New type alias `ApplicationGatewayWafRuleActionTypes`
- New type alias `ApplicationGatewayClientRevocationOptions`
- New type alias `NextStep`
- New type alias `ActionType`
- New type alias `SlotType`
- New type alias `IsWorkloadProtected`
- New type alias `RouteMapMatchCondition`
- New type alias `ApplicationGatewayWafRuleStateTypes`
- New type alias `ApplicationGatewayTierTypes`
- New type alias `CustomIPPrefixType`
- New type alias `RouteMapActionType`
- New type alias `ExpressRoutePortsBillingType`
- New type alias `ApplicationGatewayRuleSetStatusOptions`
- New type alias `Geo`
- New type alias `VnetLocalRouteOverrideCriteria`
- New function `PossibleSlotTypeValues() []SlotType`
- New function `NewVipSwapClient(string, azcore.TokenCredential, *arm.ClientOptions) (*VipSwapClient, error)`
- New function `PossibleNextStepValues() []NextStep`
- New function `*RouteMapsClient.BeginDelete(context.Context, string, string, string, *RouteMapsClientBeginDeleteOptions) (*runtime.Poller[RouteMapsClientDeleteResponse], error)`
- New function `PossibleRouteMapActionTypeValues() []RouteMapActionType`
- New function `*RouteMapsClient.Get(context.Context, string, string, string, *RouteMapsClientGetOptions) (RouteMapsClientGetResponse, error)`
- New function `*VirtualHubsClient.BeginGetOutboundRoutes(context.Context, string, string, GetOutboundRoutesParameters, *VirtualHubsClientBeginGetOutboundRoutesOptions) (*runtime.Poller[VirtualHubsClientGetOutboundRoutesResponse], error)`
- New function `PossibleGeoValues() []Geo`
- New function `PossibleApplicationGatewayClientRevocationOptionsValues() []ApplicationGatewayClientRevocationOptions`
- New function `*ApplicationGatewayWafDynamicManifestsClient.NewGetPager(string, *ApplicationGatewayWafDynamicManifestsClientGetOptions) *runtime.Pager[ApplicationGatewayWafDynamicManifestsClientGetResponse]`
- New function `*ApplicationGatewayWafDynamicManifestsDefaultClient.Get(context.Context, string, *ApplicationGatewayWafDynamicManifestsDefaultClientGetOptions) (ApplicationGatewayWafDynamicManifestsDefaultClientGetResponse, error)`
- New function `PossibleActionTypeValues() []ActionType`
- New function `*RouteMapsClient.NewListPager(string, string, *RouteMapsClientListOptions) *runtime.Pager[RouteMapsClientListResponse]`
- New function `PossibleApplicationGatewayTierTypesValues() []ApplicationGatewayTierTypes`
- New function `NewApplicationGatewayWafDynamicManifestsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ApplicationGatewayWafDynamicManifestsClient, error)`
- New function `PossibleApplicationGatewayRuleSetStatusOptionsValues() []ApplicationGatewayRuleSetStatusOptions`
- New function `PossibleCustomIPPrefixTypeValues() []CustomIPPrefixType`
- New function `NewApplicationGatewayWafDynamicManifestsDefaultClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ApplicationGatewayWafDynamicManifestsDefaultClient, error)`
- New function `PossibleVnetLocalRouteOverrideCriteriaValues() []VnetLocalRouteOverrideCriteria`
- New function `*VirtualHubsClient.BeginGetInboundRoutes(context.Context, string, string, GetInboundRoutesParameters, *VirtualHubsClientBeginGetInboundRoutesOptions) (*runtime.Poller[VirtualHubsClientGetInboundRoutesResponse], error)`
- New function `*VipSwapClient.Get(context.Context, string, string, *VipSwapClientGetOptions) (VipSwapClientGetResponse, error)`
- New function `*PublicIPAddressesClient.BeginDdosProtectionStatus(context.Context, string, string, *PublicIPAddressesClientBeginDdosProtectionStatusOptions) (*runtime.Poller[PublicIPAddressesClientDdosProtectionStatusResponse], error)`
- New function `PossibleExpressRoutePortsBillingTypeValues() []ExpressRoutePortsBillingType`
- New function `*VipSwapClient.List(context.Context, string, string, *VipSwapClientListOptions) (VipSwapClientListResponse, error)`
- New function `*VirtualNetworksClient.BeginListDdosProtectionStatus(context.Context, string, string, *VirtualNetworksClientBeginListDdosProtectionStatusOptions) (*runtime.Poller[*runtime.Pager[VirtualNetworksClientListDdosProtectionStatusResponse]], error)`
- New function `PossibleIsWorkloadProtectedValues() []IsWorkloadProtected`
- New function `PossibleDdosSettingsProtectionModeValues() []DdosSettingsProtectionMode`
- New function `PossibleApplicationGatewayWafRuleStateTypesValues() []ApplicationGatewayWafRuleStateTypes`
- New function `NewRouteMapsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*RouteMapsClient, error)`
- New function `PossibleRouteMapMatchConditionValues() []RouteMapMatchCondition`
- New function `*VipSwapClient.BeginCreate(context.Context, string, string, SwapResource, *VipSwapClientBeginCreateOptions) (*runtime.Poller[VipSwapClientCreateResponse], error)`
- New function `PossibleApplicationGatewayWafRuleActionTypesValues() []ApplicationGatewayWafRuleActionTypes`
- New function `*RouteMapsClient.BeginCreateOrUpdate(context.Context, string, string, string, RouteMap, *RouteMapsClientBeginCreateOrUpdateOptions) (*runtime.Poller[RouteMapsClientCreateOrUpdateResponse], error)`
- New struct `Action`
- New struct `ApplicationGatewayFirewallManifestRuleSet`
- New struct `ApplicationGatewayWafDynamicManifestPropertiesResult`
- New struct `ApplicationGatewayWafDynamicManifestResult`
- New struct `ApplicationGatewayWafDynamicManifestResultList`
- New struct `ApplicationGatewayWafDynamicManifestsClient`
- New struct `ApplicationGatewayWafDynamicManifestsClientGetOptions`
- New struct `ApplicationGatewayWafDynamicManifestsClientGetResponse`
- New struct `ApplicationGatewayWafDynamicManifestsDefaultClient`
- New struct `ApplicationGatewayWafDynamicManifestsDefaultClientGetOptions`
- New struct `ApplicationGatewayWafDynamicManifestsDefaultClientGetResponse`
- New struct `Criterion`
- New struct `DefaultRuleSetPropertyFormat`
- New struct `EffectiveRouteMapRoute`
- New struct `GetInboundRoutesParameters`
- New struct `GetOutboundRoutesParameters`
- New struct `ListRouteMapsResult`
- New struct `Parameter`
- New struct `PublicIPAddressesClientBeginDdosProtectionStatusOptions`
- New struct `PublicIPAddressesClientDdosProtectionStatusResponse`
- New struct `PublicIPDdosProtectionStatusResult`
- New struct `RouteMap`
- New struct `RouteMapProperties`
- New struct `RouteMapRule`
- New struct `RouteMapsClient`
- New struct `RouteMapsClientBeginCreateOrUpdateOptions`
- New struct `RouteMapsClientBeginDeleteOptions`
- New struct `RouteMapsClientCreateOrUpdateResponse`
- New struct `RouteMapsClientDeleteResponse`
- New struct `RouteMapsClientGetOptions`
- New struct `RouteMapsClientGetResponse`
- New struct `RouteMapsClientListOptions`
- New struct `RouteMapsClientListResponse`
- New struct `StaticRoutesConfig`
- New struct `SwapResource`
- New struct `SwapResourceListResult`
- New struct `SwapResourceProperties`
- New struct `VipSwapClient`
- New struct `VipSwapClientBeginCreateOptions`
- New struct `VipSwapClientCreateResponse`
- New struct `VipSwapClientGetOptions`
- New struct `VipSwapClientGetResponse`
- New struct `VipSwapClientListOptions`
- New struct `VipSwapClientListResponse`
- New struct `VirtualHubsClientBeginGetInboundRoutesOptions`
- New struct `VirtualHubsClientBeginGetOutboundRoutesOptions`
- New struct `VirtualHubsClientGetInboundRoutesResponse`
- New struct `VirtualHubsClientGetOutboundRoutesResponse`
- New struct `VirtualNetworkDdosProtectionStatusResult`
- New struct `VirtualNetworkGatewayPolicyGroup`
- New struct `VirtualNetworkGatewayPolicyGroupMember`
- New struct `VirtualNetworkGatewayPolicyGroupProperties`
- New struct `VirtualNetworksClientBeginListDdosProtectionStatusOptions`
- New struct `VirtualNetworksClientListDdosProtectionStatusResponse`
- New struct `VngClientConnectionConfiguration`
- New struct `VngClientConnectionConfigurationProperties`
- New field `RouteMaps` in struct `VirtualHubProperties`
- New field `Tiers` in struct `ApplicationGatewayFirewallRuleSetPropertiesFormat`
- New field `EnablePrivateLinkFastPath` in struct `VirtualNetworkGatewayConnectionListEntityPropertiesFormat`
- New field `ColoLocation` in struct `ExpressRouteLinkPropertiesFormat`
- New field `EnablePrivateLinkFastPath` in struct `VirtualNetworkGatewayConnectionPropertiesFormat`
- New field `DisableTCPStateTracking` in struct `InterfacePropertiesFormat`
- New field `Top` in struct `ManagementClientListNetworkManagerEffectiveConnectivityConfigurationsOptions`
- New field `Action` in struct `ManagedRuleOverride`
- New field `VngClientConnectionConfigurations` in struct `VPNClientConfiguration`
- New field `StaticRoutesConfig` in struct `VnetRoute`
- New field `AllowVirtualWanTraffic` in struct `VirtualNetworkGatewayPropertiesFormat`
- New field `VirtualNetworkGatewayPolicyGroups` in struct `VirtualNetworkGatewayPropertiesFormat`
- New field `AllowRemoteVnetTraffic` in struct `VirtualNetworkGatewayPropertiesFormat`
- New field `RuleIDString` in struct `ApplicationGatewayFirewallRule`
- New field `State` in struct `ApplicationGatewayFirewallRule`
- New field `Action` in struct `ApplicationGatewayFirewallRule`
- New field `Top` in struct `ManagerDeploymentStatusClientListOptions`
- New field `InboundRouteMap` in struct `RoutingConfiguration`
- New field `OutboundRouteMap` in struct `RoutingConfiguration`
- New field `VerifyClientRevocation` in struct `ApplicationGatewayClientAuthConfiguration`
- New field `Top` in struct `ManagementClientListActiveSecurityAdminRulesOptions`
- New field `ProbeThreshold` in struct `ProbePropertiesFormat`
- New field `AllowNonVirtualWanTraffic` in struct `ExpressRouteGatewayProperties`
- New field `Top` in struct `ManagementClientListActiveConnectivityConfigurationsOptions`
- New field `PublicIPAddresses` in struct `DdosProtectionPlanPropertiesFormat`
- New field `ProtectionMode` in struct `DdosSettings`
- New field `DdosProtectionPlan` in struct `DdosSettings`
- New field `ExpressRouteAdvertise` in struct `CustomIPPrefixPropertiesFormat`
- New field `Geo` in struct `CustomIPPrefixPropertiesFormat`
- New field `PrefixType` in struct `CustomIPPrefixPropertiesFormat`
- New field `Asn` in struct `CustomIPPrefixPropertiesFormat`
- New field `Top` in struct `ManagementClientListNetworkManagerEffectiveSecurityAdminRulesOptions`
- New field `EnablePrivateLinkFastPath` in struct `ExpressRouteConnectionProperties`
- New field `BillingType` in struct `ExpressRoutePortPropertiesFormat`


## 1.1.0 (2022-08-05)
### Features Added

- New const `SecurityConfigurationRuleDirectionInbound`
- New const `IsGlobalFalse`
- New const `EndpointTypeAzureVMSS`
- New const `ScopeConnectionStateConflict`
- New const `SecurityConfigurationRuleDirectionOutbound`
- New const `GroupConnectivityDirectlyConnected`
- New const `ScopeConnectionStateRejected`
- New const `ConfigurationTypeConnectivity`
- New const `AutoLearnPrivateRangesModeEnabled`
- New const `UseHubGatewayFalse`
- New const `NetworkIntentPolicyBasedServiceNone`
- New const `DeleteExistingPeeringFalse`
- New const `EffectiveAdminRuleKindDefault`
- New const `DeploymentStatusFailed`
- New const `AddressPrefixTypeIPPrefix`
- New const `AddressPrefixTypeServiceTag`
- New const `UseHubGatewayTrue`
- New const `WebApplicationFirewallOperatorAny`
- New const `SecurityConfigurationRuleAccessAlwaysAllow`
- New const `CreatedByTypeUser`
- New const `EndpointTypeAzureArcVM`
- New const `DeploymentStatusNotStarted`
- New const `SecurityConfigurationRuleProtocolTCP`
- New const `SecurityConfigurationRuleAccessDeny`
- New const `SecurityConfigurationRuleProtocolEsp`
- New const `IsGlobalTrue`
- New const `DeploymentStatusDeployed`
- New const `NetworkIntentPolicyBasedServiceAll`
- New const `SecurityConfigurationRuleProtocolUDP`
- New const `CreatedByTypeKey`
- New const `PacketCaptureTargetTypeAzureVMSS`
- New const `ApplicationGatewaySSLPolicyTypeCustomV2`
- New const `DeleteExistingPeeringTrue`
- New const `ScopeConnectionStateConnected`
- New const `ApplicationGatewaySSLPolicyNameAppGwSSLPolicy20220101S`
- New const `ConnectivityTopologyMesh`
- New const `CreatedByTypeManagedIdentity`
- New const `AdminRuleKindCustom`
- New const `ApplicationGatewaySSLProtocolTLSv13`
- New const `ConnectivityTopologyHubAndSpoke`
- New const `ScopeConnectionStateRevoked`
- New const `ConfigurationTypeSecurityAdmin`
- New const `SecurityConfigurationRuleProtocolAh`
- New const `CommissionedStateCommissionedNoInternetAdvertise`
- New const `ScopeConnectionStatePending`
- New const `SecurityConfigurationRuleAccessAllow`
- New const `SecurityConfigurationRuleProtocolIcmp`
- New const `AutoLearnPrivateRangesModeDisabled`
- New const `SecurityConfigurationRuleProtocolAny`
- New const `ApplicationGatewaySSLPolicyNameAppGwSSLPolicy20220101`
- New const `CreatedByTypeApplication`
- New const `GroupConnectivityNone`
- New const `EffectiveAdminRuleKindCustom`
- New const `AdminRuleKindDefault`
- New const `DeploymentStatusDeploying`
- New const `PacketCaptureTargetTypeAzureVM`
- New function `*ManagementClient.ListActiveConnectivityConfigurations(context.Context, string, string, ActiveConfigurationParameter, *ManagementClientListActiveConnectivityConfigurationsOptions) (ManagementClientListActiveConnectivityConfigurationsResponse, error)`
- New function `*ManagersClient.NewListBySubscriptionPager(*ManagersClientListBySubscriptionOptions) *runtime.Pager[ManagersClientListBySubscriptionResponse]`
- New function `NewStaticMembersClient(string, azcore.TokenCredential, *arm.ClientOptions) (*StaticMembersClient, error)`
- New function `NewAdminRulesClient(string, azcore.TokenCredential, *arm.ClientOptions) (*AdminRulesClient, error)`
- New function `*EffectiveDefaultSecurityAdminRule.GetEffectiveBaseSecurityAdminRule() *EffectiveBaseSecurityAdminRule`
- New function `PossibleAddressPrefixTypeValues() []AddressPrefixType`
- New function `PossibleUseHubGatewayValues() []UseHubGateway`
- New function `*ScopeConnectionsClient.Delete(context.Context, string, string, string, *ScopeConnectionsClientDeleteOptions) (ScopeConnectionsClientDeleteResponse, error)`
- New function `PossibleIsGlobalValues() []IsGlobal`
- New function `*ManagementClient.ListActiveSecurityAdminRules(context.Context, string, string, ActiveConfigurationParameter, *ManagementClientListActiveSecurityAdminRulesOptions) (ManagementClientListActiveSecurityAdminRulesResponse, error)`
- New function `*ManagersClient.NewListPager(string, *ManagersClientListOptions) *runtime.Pager[ManagersClientListResponse]`
- New function `NewConnectivityConfigurationsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ConnectivityConfigurationsClient, error)`
- New function `*GroupsClient.Get(context.Context, string, string, string, *GroupsClientGetOptions) (GroupsClientGetResponse, error)`
- New function `PossibleAdminRuleKindValues() []AdminRuleKind`
- New function `*ScopeConnectionsClient.Get(context.Context, string, string, string, *ScopeConnectionsClientGetOptions) (ScopeConnectionsClientGetResponse, error)`
- New function `*AdminRuleCollectionsClient.CreateOrUpdate(context.Context, string, string, string, string, AdminRuleCollection, *AdminRuleCollectionsClientCreateOrUpdateOptions) (AdminRuleCollectionsClientCreateOrUpdateResponse, error)`
- New function `PossibleScopeConnectionStateValues() []ScopeConnectionState`
- New function `*ConnectivityConfigurationsClient.NewListPager(string, string, *ConnectivityConfigurationsClientListOptions) *runtime.Pager[ConnectivityConfigurationsClientListResponse]`
- New function `*BaseAdminRule.GetBaseAdminRule() *BaseAdminRule`
- New function `PossibleSecurityConfigurationRuleProtocolValues() []SecurityConfigurationRuleProtocol`
- New function `*AdminRulesClient.CreateOrUpdate(context.Context, string, string, string, string, string, BaseAdminRuleClassification, *AdminRulesClientCreateOrUpdateOptions) (AdminRulesClientCreateOrUpdateResponse, error)`
- New function `PossibleNetworkIntentPolicyBasedServiceValues() []NetworkIntentPolicyBasedService`
- New function `*ManagementGroupNetworkManagerConnectionsClient.Delete(context.Context, string, string, *ManagementGroupNetworkManagerConnectionsClientDeleteOptions) (ManagementGroupNetworkManagerConnectionsClientDeleteResponse, error)`
- New function `PossibleSecurityConfigurationRuleAccessValues() []SecurityConfigurationRuleAccess`
- New function `*ManagersClient.BeginDelete(context.Context, string, string, *ManagersClientBeginDeleteOptions) (*runtime.Poller[ManagersClientDeleteResponse], error)`
- New function `*ManagementClient.ExpressRouteProviderPort(context.Context, string, *ManagementClientExpressRouteProviderPortOptions) (ManagementClientExpressRouteProviderPortResponse, error)`
- New function `*ActiveBaseSecurityAdminRule.GetActiveBaseSecurityAdminRule() *ActiveBaseSecurityAdminRule`
- New function `*ConnectivityConfigurationsClient.BeginDelete(context.Context, string, string, string, *ConnectivityConfigurationsClientBeginDeleteOptions) (*runtime.Poller[ConnectivityConfigurationsClientDeleteResponse], error)`
- New function `*AdminRuleCollectionsClient.BeginDelete(context.Context, string, string, string, string, *AdminRuleCollectionsClientBeginDeleteOptions) (*runtime.Poller[AdminRuleCollectionsClientDeleteResponse], error)`
- New function `*ConnectivityConfigurationsClient.CreateOrUpdate(context.Context, string, string, string, ConnectivityConfiguration, *ConnectivityConfigurationsClientCreateOrUpdateOptions) (ConnectivityConfigurationsClientCreateOrUpdateResponse, error)`
- New function `*SecurityAdminConfigurationsClient.Get(context.Context, string, string, string, *SecurityAdminConfigurationsClientGetOptions) (SecurityAdminConfigurationsClientGetResponse, error)`
- New function `*StaticMembersClient.Delete(context.Context, string, string, string, string, *StaticMembersClientDeleteOptions) (StaticMembersClientDeleteResponse, error)`
- New function `*ManagerDeploymentStatusClient.List(context.Context, string, string, ManagerDeploymentStatusParameter, *ManagerDeploymentStatusClientListOptions) (ManagerDeploymentStatusClientListResponse, error)`
- New function `*SubscriptionNetworkManagerConnectionsClient.Delete(context.Context, string, *SubscriptionNetworkManagerConnectionsClientDeleteOptions) (SubscriptionNetworkManagerConnectionsClientDeleteResponse, error)`
- New function `PossibleEffectiveAdminRuleKindValues() []EffectiveAdminRuleKind`
- New function `*AdminRulesClient.NewListPager(string, string, string, string, *AdminRulesClientListOptions) *runtime.Pager[AdminRulesClientListResponse]`
- New function `*GroupsClient.NewListPager(string, string, *GroupsClientListOptions) *runtime.Pager[GroupsClientListResponse]`
- New function `*GroupsClient.BeginDelete(context.Context, string, string, string, *GroupsClientBeginDeleteOptions) (*runtime.Poller[GroupsClientDeleteResponse], error)`
- New function `*StaticMembersClient.NewListPager(string, string, string, *StaticMembersClientListOptions) *runtime.Pager[StaticMembersClientListResponse]`
- New function `NewGroupsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*GroupsClient, error)`
- New function `PossibleCreatedByTypeValues() []CreatedByType`
- New function `PossibleAutoLearnPrivateRangesModeValues() []AutoLearnPrivateRangesMode`
- New function `*ManagementGroupNetworkManagerConnectionsClient.CreateOrUpdate(context.Context, string, string, ManagerConnection, *ManagementGroupNetworkManagerConnectionsClientCreateOrUpdateOptions) (ManagementGroupNetworkManagerConnectionsClientCreateOrUpdateResponse, error)`
- New function `*GroupsClient.CreateOrUpdate(context.Context, string, string, string, Group, *GroupsClientCreateOrUpdateOptions) (GroupsClientCreateOrUpdateResponse, error)`
- New function `*ActiveSecurityAdminRule.GetActiveBaseSecurityAdminRule() *ActiveBaseSecurityAdminRule`
- New function `*AdminRuleCollectionsClient.Get(context.Context, string, string, string, string, *AdminRuleCollectionsClientGetOptions) (AdminRuleCollectionsClientGetResponse, error)`
- New function `*ManagersClient.CreateOrUpdate(context.Context, string, string, Manager, *ManagersClientCreateOrUpdateOptions) (ManagersClientCreateOrUpdateResponse, error)`
- New function `*SubscriptionNetworkManagerConnectionsClient.NewListPager(*SubscriptionNetworkManagerConnectionsClientListOptions) *runtime.Pager[SubscriptionNetworkManagerConnectionsClientListResponse]`
- New function `*AdminRule.GetBaseAdminRule() *BaseAdminRule`
- New function `*AdminRulesClient.Get(context.Context, string, string, string, string, string, *AdminRulesClientGetOptions) (AdminRulesClientGetResponse, error)`
- New function `PossiblePacketCaptureTargetTypeValues() []PacketCaptureTargetType`
- New function `*ManagementClient.ListNetworkManagerEffectiveSecurityAdminRules(context.Context, string, string, QueryRequestOptions, *ManagementClientListNetworkManagerEffectiveSecurityAdminRulesOptions) (ManagementClientListNetworkManagerEffectiveSecurityAdminRulesResponse, error)`
- New function `*ManagementGroupNetworkManagerConnectionsClient.Get(context.Context, string, string, *ManagementGroupNetworkManagerConnectionsClientGetOptions) (ManagementGroupNetworkManagerConnectionsClientGetResponse, error)`
- New function `NewExpressRouteProviderPortsLocationClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ExpressRouteProviderPortsLocationClient, error)`
- New function `*DefaultAdminRule.GetBaseAdminRule() *BaseAdminRule`
- New function `*ConnectivityConfigurationsClient.Get(context.Context, string, string, string, *ConnectivityConfigurationsClientGetOptions) (ConnectivityConfigurationsClientGetResponse, error)`
- New function `NewManagersClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ManagersClient, error)`
- New function `*SubscriptionNetworkManagerConnectionsClient.Get(context.Context, string, *SubscriptionNetworkManagerConnectionsClientGetOptions) (SubscriptionNetworkManagerConnectionsClientGetResponse, error)`
- New function `*EffectiveSecurityAdminRule.GetEffectiveBaseSecurityAdminRule() *EffectiveBaseSecurityAdminRule`
- New function `*EffectiveBaseSecurityAdminRule.GetEffectiveBaseSecurityAdminRule() *EffectiveBaseSecurityAdminRule`
- New function `NewScopeConnectionsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ScopeConnectionsClient, error)`
- New function `NewAdminRuleCollectionsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*AdminRuleCollectionsClient, error)`
- New function `*ManagementClient.ListNetworkManagerEffectiveConnectivityConfigurations(context.Context, string, string, QueryRequestOptions, *ManagementClientListNetworkManagerEffectiveConnectivityConfigurationsOptions) (ManagementClientListNetworkManagerEffectiveConnectivityConfigurationsResponse, error)`
- New function `PossibleGroupConnectivityValues() []GroupConnectivity`
- New function `NewSubscriptionNetworkManagerConnectionsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*SubscriptionNetworkManagerConnectionsClient, error)`
- New function `*AzureFirewallsClient.BeginListLearnedPrefixes(context.Context, string, string, *AzureFirewallsClientBeginListLearnedPrefixesOptions) (*runtime.Poller[AzureFirewallsClientListLearnedPrefixesResponse], error)`
- New function `*ManagersClient.Patch(context.Context, string, string, PatchObject, *ManagersClientPatchOptions) (ManagersClientPatchResponse, error)`
- New function `*ManagersClient.Get(context.Context, string, string, *ManagersClientGetOptions) (ManagersClientGetResponse, error)`
- New function `*StaticMembersClient.CreateOrUpdate(context.Context, string, string, string, string, StaticMember, *StaticMembersClientCreateOrUpdateOptions) (StaticMembersClientCreateOrUpdateResponse, error)`
- New function `*AdminRuleCollectionsClient.NewListPager(string, string, string, *AdminRuleCollectionsClientListOptions) *runtime.Pager[AdminRuleCollectionsClientListResponse]`
- New function `*ScopeConnectionsClient.NewListPager(string, string, *ScopeConnectionsClientListOptions) *runtime.Pager[ScopeConnectionsClientListResponse]`
- New function `*ActiveDefaultSecurityAdminRule.GetActiveBaseSecurityAdminRule() *ActiveBaseSecurityAdminRule`
- New function `*ExpressRouteProviderPortsLocationClient.List(context.Context, *ExpressRouteProviderPortsLocationClientListOptions) (ExpressRouteProviderPortsLocationClientListResponse, error)`
- New function `*ManagerCommitsClient.BeginPost(context.Context, string, string, ManagerCommit, *ManagerCommitsClientBeginPostOptions) (*runtime.Poller[ManagerCommitsClientPostResponse], error)`
- New function `NewManagerCommitsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ManagerCommitsClient, error)`
- New function `PossibleConfigurationTypeValues() []ConfigurationType`
- New function `NewManagerDeploymentStatusClient(string, azcore.TokenCredential, *arm.ClientOptions) (*ManagerDeploymentStatusClient, error)`
- New function `*ScopeConnectionsClient.CreateOrUpdate(context.Context, string, string, string, ScopeConnection, *ScopeConnectionsClientCreateOrUpdateOptions) (ScopeConnectionsClientCreateOrUpdateResponse, error)`
- New function `*SecurityAdminConfigurationsClient.CreateOrUpdate(context.Context, string, string, string, SecurityAdminConfiguration, *SecurityAdminConfigurationsClientCreateOrUpdateOptions) (SecurityAdminConfigurationsClientCreateOrUpdateResponse, error)`
- New function `NewManagementGroupNetworkManagerConnectionsClient(azcore.TokenCredential, *arm.ClientOptions) (*ManagementGroupNetworkManagerConnectionsClient, error)`
- New function `PossibleDeleteExistingPeeringValues() []DeleteExistingPeering`
- New function `PossibleDeploymentStatusValues() []DeploymentStatus`
- New function `*ManagementGroupNetworkManagerConnectionsClient.NewListPager(string, *ManagementGroupNetworkManagerConnectionsClientListOptions) *runtime.Pager[ManagementGroupNetworkManagerConnectionsClientListResponse]`
- New function `*SecurityAdminConfigurationsClient.NewListPager(string, string, *SecurityAdminConfigurationsClientListOptions) *runtime.Pager[SecurityAdminConfigurationsClientListResponse]`
- New function `PossibleConnectivityTopologyValues() []ConnectivityTopology`
- New function `*StaticMembersClient.Get(context.Context, string, string, string, string, *StaticMembersClientGetOptions) (StaticMembersClientGetResponse, error)`
- New function `PossibleSecurityConfigurationRuleDirectionValues() []SecurityConfigurationRuleDirection`
- New function `*SecurityAdminConfigurationsClient.BeginDelete(context.Context, string, string, string, *SecurityAdminConfigurationsClientBeginDeleteOptions) (*runtime.Poller[SecurityAdminConfigurationsClientDeleteResponse], error)`
- New function `NewSecurityAdminConfigurationsClient(string, azcore.TokenCredential, *arm.ClientOptions) (*SecurityAdminConfigurationsClient, error)`
- New function `*AdminRulesClient.BeginDelete(context.Context, string, string, string, string, string, *AdminRulesClientBeginDeleteOptions) (*runtime.Poller[AdminRulesClientDeleteResponse], error)`
- New function `*SubscriptionNetworkManagerConnectionsClient.CreateOrUpdate(context.Context, string, ManagerConnection, *SubscriptionNetworkManagerConnectionsClientCreateOrUpdateOptions) (SubscriptionNetworkManagerConnectionsClientCreateOrUpdateResponse, error)`
- New struct `ActiveBaseSecurityAdminRule`
- New struct `ActiveConfigurationParameter`
- New struct `ActiveConnectivityConfiguration`
- New struct `ActiveConnectivityConfigurationsListResult`
- New struct `ActiveDefaultSecurityAdminRule`
- New struct `ActiveSecurityAdminRule`
- New struct `ActiveSecurityAdminRulesListResult`
- New struct `AddressPrefixItem`
- New struct `AdminPropertiesFormat`
- New struct `AdminRule`
- New struct `AdminRuleCollection`
- New struct `AdminRuleCollectionListResult`
- New struct `AdminRuleCollectionPropertiesFormat`
- New struct `AdminRuleCollectionsClient`
- New struct `AdminRuleCollectionsClientBeginDeleteOptions`
- New struct `AdminRuleCollectionsClientCreateOrUpdateOptions`
- New struct `AdminRuleCollectionsClientCreateOrUpdateResponse`
- New struct `AdminRuleCollectionsClientDeleteResponse`
- New struct `AdminRuleCollectionsClientGetOptions`
- New struct `AdminRuleCollectionsClientGetResponse`
- New struct `AdminRuleCollectionsClientListOptions`
- New struct `AdminRuleCollectionsClientListResponse`
- New struct `AdminRuleListResult`
- New struct `AdminRulesClient`
- New struct `AdminRulesClientBeginDeleteOptions`
- New struct `AdminRulesClientCreateOrUpdateOptions`
- New struct `AdminRulesClientCreateOrUpdateResponse`
- New struct `AdminRulesClientDeleteResponse`
- New struct `AdminRulesClientGetOptions`
- New struct `AdminRulesClientGetResponse`
- New struct `AdminRulesClientListOptions`
- New struct `AdminRulesClientListResponse`
- New struct `AzureFirewallsClientBeginListLearnedPrefixesOptions`
- New struct `AzureFirewallsClientListLearnedPrefixesResponse`
- New struct `BaseAdminRule`
- New struct `ChildResource`
- New struct `ConfigurationGroup`
- New struct `ConnectivityConfiguration`
- New struct `ConnectivityConfigurationListResult`
- New struct `ConnectivityConfigurationProperties`
- New struct `ConnectivityConfigurationsClient`
- New struct `ConnectivityConfigurationsClientBeginDeleteOptions`
- New struct `ConnectivityConfigurationsClientCreateOrUpdateOptions`
- New struct `ConnectivityConfigurationsClientCreateOrUpdateResponse`
- New struct `ConnectivityConfigurationsClientDeleteResponse`
- New struct `ConnectivityConfigurationsClientGetOptions`
- New struct `ConnectivityConfigurationsClientGetResponse`
- New struct `ConnectivityConfigurationsClientListOptions`
- New struct `ConnectivityConfigurationsClientListResponse`
- New struct `ConnectivityGroupItem`
- New struct `CrossTenantScopes`
- New struct `DefaultAdminPropertiesFormat`
- New struct `DefaultAdminRule`
- New struct `EffectiveBaseSecurityAdminRule`
- New struct `EffectiveConnectivityConfiguration`
- New struct `EffectiveDefaultSecurityAdminRule`
- New struct `EffectiveSecurityAdminRule`
- New struct `ExpressRouteProviderPort`
- New struct `ExpressRouteProviderPortListResult`
- New struct `ExpressRouteProviderPortProperties`
- New struct `ExpressRouteProviderPortsLocationClient`
- New struct `ExpressRouteProviderPortsLocationClientListOptions`
- New struct `ExpressRouteProviderPortsLocationClientListResponse`
- New struct `Group`
- New struct `GroupListResult`
- New struct `GroupProperties`
- New struct `GroupsClient`
- New struct `GroupsClientBeginDeleteOptions`
- New struct `GroupsClientCreateOrUpdateOptions`
- New struct `GroupsClientCreateOrUpdateResponse`
- New struct `GroupsClientDeleteResponse`
- New struct `GroupsClientGetOptions`
- New struct `GroupsClientGetResponse`
- New struct `GroupsClientListOptions`
- New struct `GroupsClientListResponse`
- New struct `Hub`
- New struct `IPPrefixesList`
- New struct `ManagementClientExpressRouteProviderPortOptions`
- New struct `ManagementClientExpressRouteProviderPortResponse`
- New struct `ManagementClientListActiveConnectivityConfigurationsOptions`
- New struct `ManagementClientListActiveConnectivityConfigurationsResponse`
- New struct `ManagementClientListActiveSecurityAdminRulesOptions`
- New struct `ManagementClientListActiveSecurityAdminRulesResponse`
- New struct `ManagementClientListNetworkManagerEffectiveConnectivityConfigurationsOptions`
- New struct `ManagementClientListNetworkManagerEffectiveConnectivityConfigurationsResponse`
- New struct `ManagementClientListNetworkManagerEffectiveSecurityAdminRulesOptions`
- New struct `ManagementClientListNetworkManagerEffectiveSecurityAdminRulesResponse`
- New struct `ManagementGroupNetworkManagerConnectionsClient`
- New struct `ManagementGroupNetworkManagerConnectionsClientCreateOrUpdateOptions`
- New struct `ManagementGroupNetworkManagerConnectionsClientCreateOrUpdateResponse`
- New struct `ManagementGroupNetworkManagerConnectionsClientDeleteOptions`
- New struct `ManagementGroupNetworkManagerConnectionsClientDeleteResponse`
- New struct `ManagementGroupNetworkManagerConnectionsClientGetOptions`
- New struct `ManagementGroupNetworkManagerConnectionsClientGetResponse`
- New struct `ManagementGroupNetworkManagerConnectionsClientListOptions`
- New struct `ManagementGroupNetworkManagerConnectionsClientListResponse`
- New struct `Manager`
- New struct `ManagerCommit`
- New struct `ManagerCommitsClient`
- New struct `ManagerCommitsClientBeginPostOptions`
- New struct `ManagerCommitsClientPostResponse`
- New struct `ManagerConnection`
- New struct `ManagerConnectionListResult`
- New struct `ManagerConnectionProperties`
- New struct `ManagerDeploymentStatus`
- New struct `ManagerDeploymentStatusClient`
- New struct `ManagerDeploymentStatusClientListOptions`
- New struct `ManagerDeploymentStatusClientListResponse`
- New struct `ManagerDeploymentStatusListResult`
- New struct `ManagerDeploymentStatusParameter`
- New struct `ManagerEffectiveConnectivityConfigurationListResult`
- New struct `ManagerEffectiveSecurityAdminRulesListResult`
- New struct `ManagerListResult`
- New struct `ManagerProperties`
- New struct `ManagerPropertiesNetworkManagerScopes`
- New struct `ManagerSecurityGroupItem`
- New struct `ManagersClient`
- New struct `ManagersClientBeginDeleteOptions`
- New struct `ManagersClientCreateOrUpdateOptions`
- New struct `ManagersClientCreateOrUpdateResponse`
- New struct `ManagersClientDeleteResponse`
- New struct `ManagersClientGetOptions`
- New struct `ManagersClientGetResponse`
- New struct `ManagersClientListBySubscriptionOptions`
- New struct `ManagersClientListBySubscriptionResponse`
- New struct `ManagersClientListOptions`
- New struct `ManagersClientListResponse`
- New struct `ManagersClientPatchOptions`
- New struct `ManagersClientPatchResponse`
- New struct `PacketCaptureMachineScope`
- New struct `PatchObject`
- New struct `QueryRequestOptions`
- New struct `ScopeConnection`
- New struct `ScopeConnectionListResult`
- New struct `ScopeConnectionProperties`
- New struct `ScopeConnectionsClient`
- New struct `ScopeConnectionsClientCreateOrUpdateOptions`
- New struct `ScopeConnectionsClientCreateOrUpdateResponse`
- New struct `ScopeConnectionsClientDeleteOptions`
- New struct `ScopeConnectionsClientDeleteResponse`
- New struct `ScopeConnectionsClientGetOptions`
- New struct `ScopeConnectionsClientGetResponse`
- New struct `ScopeConnectionsClientListOptions`
- New struct `ScopeConnectionsClientListResponse`
- New struct `SecurityAdminConfiguration`
- New struct `SecurityAdminConfigurationListResult`
- New struct `SecurityAdminConfigurationPropertiesFormat`
- New struct `SecurityAdminConfigurationsClient`
- New struct `SecurityAdminConfigurationsClientBeginDeleteOptions`
- New struct `SecurityAdminConfigurationsClientCreateOrUpdateOptions`
- New struct `SecurityAdminConfigurationsClientCreateOrUpdateResponse`
- New struct `SecurityAdminConfigurationsClientDeleteResponse`
- New struct `SecurityAdminConfigurationsClientGetOptions`
- New struct `SecurityAdminConfigurationsClientGetResponse`
- New struct `SecurityAdminConfigurationsClientListOptions`
- New struct `SecurityAdminConfigurationsClientListResponse`
- New struct `StaticMember`
- New struct `StaticMemberListResult`
- New struct `StaticMemberProperties`
- New struct `StaticMembersClient`
- New struct `StaticMembersClientCreateOrUpdateOptions`
- New struct `StaticMembersClientCreateOrUpdateResponse`
- New struct `StaticMembersClientDeleteOptions`
- New struct `StaticMembersClientDeleteResponse`
- New struct `StaticMembersClientGetOptions`
- New struct `StaticMembersClientGetResponse`
- New struct `StaticMembersClientListOptions`
- New struct `StaticMembersClientListResponse`
- New struct `SubscriptionNetworkManagerConnectionsClient`
- New struct `SubscriptionNetworkManagerConnectionsClientCreateOrUpdateOptions`
- New struct `SubscriptionNetworkManagerConnectionsClientCreateOrUpdateResponse`
- New struct `SubscriptionNetworkManagerConnectionsClientDeleteOptions`
- New struct `SubscriptionNetworkManagerConnectionsClientDeleteResponse`
- New struct `SubscriptionNetworkManagerConnectionsClientGetOptions`
- New struct `SubscriptionNetworkManagerConnectionsClientGetResponse`
- New struct `SubscriptionNetworkManagerConnectionsClientListOptions`
- New struct `SubscriptionNetworkManagerConnectionsClientListResponse`
- New struct `SystemData`
- New struct `VirtualRouterAutoScaleConfiguration`
- New field `NoInternetAdvertise` in struct `CustomIPPrefixPropertiesFormat`
- New field `FlushConnection` in struct `SecurityGroupPropertiesFormat`
- New field `EnablePacFile` in struct `ExplicitProxySettings`
- New field `Scope` in struct `PacketCaptureParameters`
- New field `TargetType` in struct `PacketCaptureParameters`
- New field `Scope` in struct `PacketCaptureResultProperties`
- New field `TargetType` in struct `PacketCaptureResultProperties`
- New field `AutoLearnPrivateRanges` in struct `FirewallPolicySNAT`
- New field `VirtualRouterAutoScaleConfiguration` in struct `VirtualHubProperties`
- New field `Priority` in struct `ApplicationGatewayRoutingRulePropertiesFormat`


## 1.0.0 (2022-05-16)

The package of `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork` is using our [next generation design principles](https://azure.github.io/azure-sdk/general_introduction.html) since version 1.0.0, which contains breaking changes.

To migrate the existing applications to the latest version, please refer to [Migration Guide](https://aka.ms/azsdk/go/mgmt/migration).

To learn more, please refer to our documentation [Quick Start](https://aka.ms/azsdk/go/mgmt).
