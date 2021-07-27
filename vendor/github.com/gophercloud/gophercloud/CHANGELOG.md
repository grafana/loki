## 0.19.0 (Unreleased)

## 0.18.0 (June 11, 2021)

NOTES / BREAKING CHANGES

* As of [GH-2160](https://github.com/gophercloud/gophercloud/pull/2160), Gophercloud no longer URL encodes Object Storage containers and object names. You can still encode them yourself before passing the names to the Object Storage functions.

* `baremetal/v1/nodes.ListBIOSSettings` now takes three parameters. The third, new, parameter is `ListBIOSSettingsOptsBuilder` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)

BUG FIXES

* Fixed expected OK codes to use default codes [GH-2173](https://github.com/gophercloud/gophercloud/pull/2173)
* Fixed inablity to create sub-containers (objects with `/` in their name) [GH-2160](https://github.com/gophercloud/gophercloud/pull/2160)

IMPROVEMENTS

* Added `orchestration/v1/stacks.ListOpts.ShowHidden` [GH-2104](https://github.com/gophercloud/gophercloud/pull/2104)
* Added `loadbalancer/v2/listeners.ProtocolSCTP` [GH-2149](https://github.com/gophercloud/gophercloud/pull/2149)
* Added `loadbalancer/v2/listeners.CreateOpts.TLSVersions` [GH-2150](https://github.com/gophercloud/gophercloud/pull/2150)
* Added `loadbalancer/v2/listeners.UpdateOpts.TLSVersions` [GH-2150](https://github.com/gophercloud/gophercloud/pull/2150)
* Added `baremetal/v1/nodes.CreateOpts.NetworkData` [GH-2154](https://github.com/gophercloud/gophercloud/pull/2154)
* Added `baremetal/v1/nodes.Node.NetworkData` [GH-2154](https://github.com/gophercloud/gophercloud/pull/2154)
* Added `loadbalancer/v2/pools.ProtocolPROXYV2` [GH-2158](https://github.com/gophercloud/gophercloud/pull/2158)
* Added `loadbalancer/v2/pools.ProtocolSCTP` [GH-2158](https://github.com/gophercloud/gophercloud/pull/2158)
* Added `placement/v1/resourceproviders.GetAllocations` [GH-2162](https://github.com/gophercloud/gophercloud/pull/2162)
* Added `baremetal/v1/nodes.CreateOpts.BIOSInterface` [GH-2164](https://github.com/gophercloud/gophercloud/pull/2164)
* Added `baremetal/v1/nodes.Node.BIOSInterface` [GH-2164](https://github.com/gophercloud/gophercloud/pull/2164)
* Added `baremetal/v1/nodes.NodeValidation.BIOS` [GH-2164](https://github.com/gophercloud/gophercloud/pull/2164)
* Added `baremetal/v1/nodes.ListBIOSSettings` [GH-2171](https://github.com/gophercloud/gophercloud/pull/2171)
* Added `baremetal/v1/nodes.GetBIOSSetting` [GH-2171](https://github.com/gophercloud/gophercloud/pull/2171)
* Added `baremetal/v1/nodes.ListBIOSSettingsOpts` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.AttributeType` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.AllowableValues` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.LowerBound` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.UpperBound` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.MinLength` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.MaxLength` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.ReadOnly` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.ResetRequired` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)
* Added `baremetal/v1/nodes.BIOSSetting.Unique` [GH-2174](https://github.com/gophercloud/gophercloud/pull/2174)

## 0.17.0 (April 9, 2021)

IMPROVEMENTS

* `networking/v2/extensions/quotas.QuotaDetail.Reserved` can handle both `int` and `string` values [GH-2126](https://github.com/gophercloud/gophercloud/pull/2126)
* Added `blockstorage/v3/volumetypes.ListExtraSpecs` [GH-2123](https://github.com/gophercloud/gophercloud/pull/2123)
* Added `blockstorage/v3/volumetypes.GetExtraSpec` [GH-2123](https://github.com/gophercloud/gophercloud/pull/2123)
* Added `blockstorage/v3/volumetypes.CreateExtraSpecs` [GH-2123](https://github.com/gophercloud/gophercloud/pull/2123)
* Added `blockstorage/v3/volumetypes.UpdateExtraSpec` [GH-2123](https://github.com/gophercloud/gophercloud/pull/2123)
* Added `blockstorage/v3/volumetypes.DeleteExtraSpec` [GH-2123](https://github.com/gophercloud/gophercloud/pull/2123)
* Added `identity/v3/roles.ListAssignmentOpts.IncludeNames` [GH-2133](https://github.com/gophercloud/gophercloud/pull/2133)
* Added `identity/v3/roles.AssignedRoles.Name` [GH-2133](https://github.com/gophercloud/gophercloud/pull/2133)
* Added `identity/v3/roles.Domain.Name` [GH-2133](https://github.com/gophercloud/gophercloud/pull/2133)
* Added `identity/v3/roles.Project.Name` [GH-2133](https://github.com/gophercloud/gophercloud/pull/2133)
* Added `identity/v3/roles.User.Name` [GH-2133](https://github.com/gophercloud/gophercloud/pull/2133)
* Added `identity/v3/roles.Group.Name` [GH-2133](https://github.com/gophercloud/gophercloud/pull/2133)
* Added `blockstorage/extensions/availabilityzones.List` [GH-2135](https://github.com/gophercloud/gophercloud/pull/2135)
* Added `blockstorage/v3/volumetypes.ListAccesses` [GH-2138](https://github.com/gophercloud/gophercloud/pull/2138)
* Added `blockstorage/v3/volumetypes.AddAccess` [GH-2138](https://github.com/gophercloud/gophercloud/pull/2138)
* Added `blockstorage/v3/volumetypes.RemoveAccess` [GH-2138](https://github.com/gophercloud/gophercloud/pull/2138)
* Added `blockstorage/v3/qos.Create` [GH-2140](https://github.com/gophercloud/gophercloud/pull/2140)
* Added `blockstorage/v3/qos.Delete` [GH-2140](https://github.com/gophercloud/gophercloud/pull/2140)

## 0.16.0 (February 23, 2021)

UPGRADE NOTES

* `baremetal/v1/nodes.CleanStep.Interface` has changed from `string` to `StepInterface` [GH-2120](https://github.com/gophercloud/gophercloud/pull/2120)

BUG FIXES

* Fixed `xor` logic issues in `loadbalancers/v2/l7policies.CreateOpts` [GH-2087](https://github.com/gophercloud/gophercloud/pull/2087)
* Fixed `xor` logic issues in `loadbalancers/v2/listeners.CreateOpts` [GH-2087](https://github.com/gophercloud/gophercloud/pull/2087)
* Fixed `If-Modified-Since` so it's correctly sent in a `objectstorage/v1/objects.Download` request [GH-2108](https://github.com/gophercloud/gophercloud/pull/2108)
* Fixed `If-Unmodified-Since` so it's correctly sent in a `objectstorage/v1/objects.Download` request [GH-2108](https://github.com/gophercloud/gophercloud/pull/2108)

IMPROVEMENTS

* Added `blockstorage/extensions/limits.Get` [GH-2084](https://github.com/gophercloud/gophercloud/pull/2084)
* `clustering/v1/clusters.RemoveNodes` now returns an `ActionResult` [GH-2089](https://github.com/gophercloud/gophercloud/pull/2089)
* Added `identity/v3/projects.ListAvailable` [GH-2090](https://github.com/gophercloud/gophercloud/pull/2090)
* Added `blockstorage/extensions/backups.ListDetail` [GH-2085](https://github.com/gophercloud/gophercloud/pull/2085)
* Allow all ports to be removed in `networking/v2/extensions/fwaas_v2/groups.UpdateOpts` [GH-2073]
* Added `imageservice/v2/images.ListOpts.Hidden` [GH-2094](https://github.com/gophercloud/gophercloud/pull/2094)
* Added `imageservice/v2/images.CreateOpts.Hidden` [GH-2094](https://github.com/gophercloud/gophercloud/pull/2094)
* Added `imageservice/v2/images.ReplaceImageHidden` [GH-2094](https://github.com/gophercloud/gophercloud/pull/2094)
* Added `imageservice/v2/images.Image.Hidden` [GH-2094](https://github.com/gophercloud/gophercloud/pull/2094)
* Added `containerinfra/v1/clusters.CreateOpts.MasterLBEnabled` [GH-2102](https://github.com/gophercloud/gophercloud/pull/2102)
* Added the ability to define a custom function to handle "Retry-After" (429) responses [GH-2097](https://github.com/gophercloud/gophercloud/pull/2097)
* Added `baremetal/v1/nodes.JBOD` constant for the `RAIDLevel` type [GH-2103](https://github.com/gophercloud/gophercloud/pull/2103)
* Added support for Block Storage quotas of volume typed resources [GH-2109](https://github.com/gophercloud/gophercloud/pull/2109)
* Added `blockstorage/extensions/volumeactions.ChangeType` [GH-2113](https://github.com/gophercloud/gophercloud/pull/2113)
* Added `baremetal/v1/nodes.DeployStep` [GH-2120](https://github.com/gophercloud/gophercloud/pull/2120)
* Added `baremetal/v1/nodes.ProvisionStateOpts.DeploySteps` [GH-2120](https://github.com/gophercloud/gophercloud/pull/2120)
* Added `baremetal/v1/nodes.CreateOpts.AutomatedClean` [GH-2122](https://github.com/gophercloud/gophercloud/pull/2122)

## 0.15.0 (December 27, 2020)

BREAKING CHANGES

* `compute/v2/extensions/servergroups.List` now takes a `ListOpts` parameter. You can pass `nil` if you don't need to use this.

IMPROVEMENTS

* Added `loadbalancer/v2/pools.CreateMemberOpts.Tags` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.UpdateMemberOpts.Backup` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.UpdateMemberOpts.MonitorAddress` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.UpdateMemberOpts.MonitorPort` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.UpdateMemberOpts.Tags` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.BatchUpdateMemberOpts.Backup` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.BatchUpdateMemberOpts.MonitorAddress` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.BatchUpdateMemberOpts.MonitorPort` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `loadbalancer/v2/pools.BatchUpdateMemberOpts.Tags` [GH-2056](https://github.com/gophercloud/gophercloud/pull/2056)
* Added `networking/v2/extensions/quotas.GetDetail` [GH-2061](https://github.com/gophercloud/gophercloud/pull/2061)
* Added `networking/v2/extensions/quotas.UpdateOpts.Trunk` [GH-2061](https://github.com/gophercloud/gophercloud/pull/2061)
* Added `objectstorage/v1/accounts.UpdateOpts.RemoveMetadata` [GH-2063](https://github.com/gophercloud/gophercloud/pull/2063)
* Added `objectstorage/v1/objects.UpdateOpts.RemoveMetadata` [GH-2063](https://github.com/gophercloud/gophercloud/pull/2063)
* Added `identity/v3/catalog.List` [GH-2067](https://github.com/gophercloud/gophercloud/pull/2067)
* Added `networking/v2/extensions/fwaas_v2/policies.List` [GH-2057](https://github.com/gophercloud/gophercloud/pull/2057)
* Added `networking/v2/extensions/fwaas_v2/policies.Create` [GH-2057](https://github.com/gophercloud/gophercloud/pull/2057)
* Added `networking/v2/extensions/fwaas_v2/policies.Get` [GH-2057](https://github.com/gophercloud/gophercloud/pull/2057)
* Added `networking/v2/extensions/fwaas_v2/policies.Update` [GH-2057](https://github.com/gophercloud/gophercloud/pull/2057)
* Added `networking/v2/extensions/fwaas_v2/policies.Delete` [GH-2057](https://github.com/gophercloud/gophercloud/pull/2057)
* Added `compute/v2/extensions/servergroups.ListOpts.AllProjects` [GH-2070](https://github.com/gophercloud/gophercloud/pull/2070)
* Added `objectstorage/v1/containers.CreateOpts.StoragePolicy` [GH-2075](https://github.com/gophercloud/gophercloud/pull/2075)
* Added `blockstorage/v3/snapshots.Update` [GH-2081](https://github.com/gophercloud/gophercloud/pull/2081)
* Added `loadbalancer/v2/l7policies.CreateOpts.Rules` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)
* Added `loadbalancer/v2/listeners.CreateOpts.DefaultPool` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)
* Added `loadbalancer/v2/listeners.CreateOpts.L7Policies` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)
* Added `loadbalancer/v2/listeners.Listener.DefaultPool` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)
* Added `loadbalancer/v2/loadbalancers.CreateOpts.Listeners` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)
* Added `loadbalancer/v2/loadbalancers.CreateOpts.Pools` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)
* Added `loadbalancer/v2/pools.CreateOpts.Members` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)
* Added `loadbalancer/v2/pools.CreateOpts.Monitor` [GH-2077](https://github.com/gophercloud/gophercloud/pull/2077)


## 0.14.0 (November 11, 2020)

IMPROVEMENTS

* Added `identity/v3/endpoints.Endpoint.Enabled` [GH-2030](https://github.com/gophercloud/gophercloud/pull/2030)
* Added `containerinfra/v1/clusters.Upgrade` [GH-2032](https://github.com/gophercloud/gophercloud/pull/2032)
* Added `compute/apiversions.List` [GH-2037](https://github.com/gophercloud/gophercloud/pull/2037)
* Added `compute/apiversions.Get` [GH-2037](https://github.com/gophercloud/gophercloud/pull/2037)
* Added `compute/v2/servers.ListOpts.IP` [GH-2038](https://github.com/gophercloud/gophercloud/pull/2038)
* Added `compute/v2/servers.ListOpts.IP6` [GH-2038](https://github.com/gophercloud/gophercloud/pull/2038)
* Added `compute/v2/servers.ListOpts.UserID` [GH-2038](https://github.com/gophercloud/gophercloud/pull/2038)
* Added `dns/v2/transfer/accept.List` [GH-2041](https://github.com/gophercloud/gophercloud/pull/2041)
* Added `dns/v2/transfer/accept.Get` [GH-2041](https://github.com/gophercloud/gophercloud/pull/2041)
* Added `dns/v2/transfer/accept.Create` [GH-2041](https://github.com/gophercloud/gophercloud/pull/2041)
* Added `dns/v2/transfer/requests.List` [GH-2041](https://github.com/gophercloud/gophercloud/pull/2041)
* Added `dns/v2/transfer/requests.Get` [GH-2041](https://github.com/gophercloud/gophercloud/pull/2041)
* Added `dns/v2/transfer/requests.Update` [GH-2041](https://github.com/gophercloud/gophercloud/pull/2041)
* Added `dns/v2/transfer/requests.Delete` [GH-2041](https://github.com/gophercloud/gophercloud/pull/2041)
* Added `baremetal/v1/nodes.RescueWait` [GH-2052](https://github.com/gophercloud/gophercloud/pull/2052)
* Added `baremetal/v1/nodes.Unrescuing` [GH-2052](https://github.com/gophercloud/gophercloud/pull/2052)
* Added `networking/v2/extensions/fwaas_v2/groups.List` [GH-2050](https://github.com/gophercloud/gophercloud/pull/2050)
* Added `networking/v2/extensions/fwaas_v2/groups.Get` [GH-2050](https://github.com/gophercloud/gophercloud/pull/2050)
* Added `networking/v2/extensions/fwaas_v2/groups.Create` [GH-2050](https://github.com/gophercloud/gophercloud/pull/2050)
* Added `networking/v2/extensions/fwaas_v2/groups.Update` [GH-2050](https://github.com/gophercloud/gophercloud/pull/2050)
* Added `networking/v2/extensions/fwaas_v2/groups.Delete` [GH-2050](https://github.com/gophercloud/gophercloud/pull/2050)

BUG FIXES

* Changed `networking/v2/extensions/layer3/routers.Routes` from `[]Route` to `*[]Route` [GH-2043](https://github.com/gophercloud/gophercloud/pull/2043)

## 0.13.0 (September 27, 2020)

IMPROVEMENTS

* Added `ProtocolTerminatedHTTPS` as a valid listener protocol to `loadbalancer/v2/listeners` [GH-1992](https://github.com/gophercloud/gophercloud/pull/1992)
* Added `objectstorage/v1/objects.CreateTempURLOpts.Timestamp` [GH-1994](https://github.com/gophercloud/gophercloud/pull/1994)
* Added `compute/v2/extensions/schedulerhints.SchedulerHints.DifferentCell` [GH-2012](https://github.com/gophercloud/gophercloud/pull/2012)
* Added `loadbalancer/v2/quotas.Get` [GH-2010](https://github.com/gophercloud/gophercloud/pull/2010)
* Added `messaging/v2/queues.CreateOpts.EnableEncryptMessages` [GH-2016](https://github.com/gophercloud/gophercloud/pull/2016)
* Added `messaging/v2/queues.ListOpts.Name` [GH-2018](https://github.com/gophercloud/gophercloud/pull/2018)
* Added `messaging/v2/queues.ListOpts.WithCount` [GH-2018](https://github.com/gophercloud/gophercloud/pull/2018)
* Added `loadbalancer/v2/quotas.Update` [GH-2023](https://github.com/gophercloud/gophercloud/pull/2023)
* Added `loadbalancer/v2/loadbalancers.ListOpts.AvailabilityZone` [GH-2026](https://github.com/gophercloud/gophercloud/pull/2026)
* Added `loadbalancer/v2/loadbalancers.CreateOpts.AvailabilityZone` [GH-2026](https://github.com/gophercloud/gophercloud/pull/2026)
* Added `loadbalancer/v2/loadbalancers.LoadBalancer.AvailabilityZone` [GH-2026](https://github.com/gophercloud/gophercloud/pull/2026)
* Added `networking/v2/extensions/layer3/routers.ListL3Agents` [GH-2025](https://github.com/gophercloud/gophercloud/pull/2025)

BUG FIXES

* Fixed URL escaping in `objectstorage/v1/objects.CreateTempURL` [GH-1994](https://github.com/gophercloud/gophercloud/pull/1994)
* Remove unused `ServiceClient` from `compute/v2/servers.CreateOpts` [GH-2004](https://github.com/gophercloud/gophercloud/pull/2004)
* Changed `objectstorage/v1/objects.CreateOpts.DeleteAfter` from `int` to `int64` [GH-2014](https://github.com/gophercloud/gophercloud/pull/2014)
* Changed `objectstorage/v1/objects.CreateOpts.DeleteAt` from `int` to `int64` [GH-2014](https://github.com/gophercloud/gophercloud/pull/2014)
* Changed `objectstorage/v1/objects.UpdateOpts.DeleteAfter` from `int` to `int64` [GH-2014](https://github.com/gophercloud/gophercloud/pull/2014)
* Changed `objectstorage/v1/objects.UpdateOpts.DeleteAt` from `int` to `int64` [GH-2014](https://github.com/gophercloud/gophercloud/pull/2014)


## 0.12.0 (June 25, 2020)

UPGRADE NOTES

* The URL used in the `compute/v2/extensions/bootfromvolume` package has been changed from `os-volumes_boot` to `servers`.

IMPROVEMENTS

* The URL used in the `compute/v2/extensions/bootfromvolume` package has been changed from `os-volumes_boot` to `servers` [GH-1973](https://github.com/gophercloud/gophercloud/pull/1973)
* Modify `baremetal/v1/nodes.LogicalDisk.PhysicalDisks` type to support physical disks hints [GH-1982](https://github.com/gophercloud/gophercloud/pull/1982)
* Added `baremetalintrospection/httpbasic` which provides an HTTP Basic Auth client [GH-1986](https://github.com/gophercloud/gophercloud/pull/1986)
* Added `baremetal/httpbasic` which provides an HTTP Basic Auth client [GH-1983](https://github.com/gophercloud/gophercloud/pull/1983)
* Added `containerinfra/v1/clusters.CreateOpts.MergeLabels` [GH-1985](https://github.com/gophercloud/gophercloud/pull/1985)

BUG FIXES

* Changed `containerinfra/v1/clusters.Cluster.HealthStatusReason` from `string` to `map[string]interface{}` [GH-1968](https://github.com/gophercloud/gophercloud/pull/1968)
* Fixed marshalling of `blockstorage/extensions/backups.ImportBackup.Metadata` [GH-1967](https://github.com/gophercloud/gophercloud/pull/1967)
* Fixed typo of "OAUth" to "OAuth" in `identity/v3/extensions/oauth1` [GH-1969](https://github.com/gophercloud/gophercloud/pull/1969)
* Fixed goroutine leak during reauthentication [GH-1978](https://github.com/gophercloud/gophercloud/pull/1978)
* Changed `baremetalintrospection/v1/introspection.RootDiskType.Size` from `int` to `int64` [GH-1988](https://github.com/gophercloud/gophercloud/pull/1988)

## 0.11.0 (May 14, 2020)

UPGRADE NOTES

* Object storage container and object names are now URL encoded [GH-1930](https://github.com/gophercloud/gophercloud/pull/1930)
* All responses now have access to the returned headers. Please report any issues this has caused [GH-1942](https://github.com/gophercloud/gophercloud/pull/1942)
* Changes have been made to the internal HTTP client to ensure response bodies are handled in a way that enables connections to be re-used more efficiently [GH-1952](https://github.com/gophercloud/gophercloud/pull/1952)

IMPROVEMENTS

* Added `objectstorage/v1/containers.BulkDelete` [GH-1930](https://github.com/gophercloud/gophercloud/pull/1930)
* Added `objectstorage/v1/objects.BulkDelete` [GH-1930](https://github.com/gophercloud/gophercloud/pull/1930)
* Object storage container and object names are now URL encoded [GH-1930](https://github.com/gophercloud/gophercloud/pull/1930)
* All responses now have access to the returned headers [GH-1942](https://github.com/gophercloud/gophercloud/pull/1942)
* Added `compute/v2/extensions/injectnetworkinfo.InjectNetworkInfo` [GH-1941](https://github.com/gophercloud/gophercloud/pull/1941)
* Added `compute/v2/extensions/resetnetwork.ResetNetwork` [GH-1941](https://github.com/gophercloud/gophercloud/pull/1941)
* Added `identity/v3/extensions/trusts.ListRoles` [GH-1939](https://github.com/gophercloud/gophercloud/pull/1939)
* Added `identity/v3/extensions/trusts.GetRole` [GH-1939](https://github.com/gophercloud/gophercloud/pull/1939)
* Added `identity/v3/extensions/trusts.CheckRole` [GH-1939](https://github.com/gophercloud/gophercloud/pull/1939)
* Added `identity/v3/extensions/oauth1.Create` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.CreateConsumer` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.DeleteConsumer` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.ListConsumers` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.GetConsumer` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.UpdateConsumer` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.RequestToken` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.AuthorizeToken` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.CreateAccessToken` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.GetAccessToken` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.RevokeAccessToken` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.ListAccessTokens` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.ListAccessTokenRoles` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `identity/v3/extensions/oauth1.GetAccessTokenRole` [GH-1935](https://github.com/gophercloud/gophercloud/pull/1935)
* Added `networking/v2/extensions/agents.Update` [GH-1954](https://github.com/gophercloud/gophercloud/pull/1954)
* Added `networking/v2/extensions/agents.Delete` [GH-1954](https://github.com/gophercloud/gophercloud/pull/1954)
* Added `networking/v2/extensions/agents.ScheduleDHCPNetwork` [GH-1954](https://github.com/gophercloud/gophercloud/pull/1954)
* Added `networking/v2/extensions/agents.RemoveDHCPNetwork` [GH-1954](https://github.com/gophercloud/gophercloud/pull/1954)
* Added `identity/v3/projects.CreateOpts.Extra` [GH-1951](https://github.com/gophercloud/gophercloud/pull/1951)
* Added `identity/v3/projects.CreateOpts.Options` [GH-1951](https://github.com/gophercloud/gophercloud/pull/1951)
* Added `identity/v3/projects.UpdateOpts.Extra` [GH-1951](https://github.com/gophercloud/gophercloud/pull/1951)
* Added `identity/v3/projects.UpdateOpts.Options` [GH-1951](https://github.com/gophercloud/gophercloud/pull/1951)
* Added `identity/v3/projects.Project.Extra` [GH-1951](https://github.com/gophercloud/gophercloud/pull/1951)
* Added `identity/v3/projects.Options.Options` [GH-1951](https://github.com/gophercloud/gophercloud/pull/1951)
* Added `imageservice/v2/images.Image.OpenStackImageImportMethods` [GH-1962](https://github.com/gophercloud/gophercloud/pull/1962)
* Added `imageservice/v2/images.Image.OpenStackImageStoreIDs` [GH-1962](https://github.com/gophercloud/gophercloud/pull/1962)

BUG FIXES

* Changed`identity/v3/extensions/trusts.Trust.RemainingUses` from `bool` to `int` [GH-1939](https://github.com/gophercloud/gophercloud/pull/1939)
* Changed `identity/v3/applicationcredentials.CreateOpts.ExpiresAt` from `string` to `*time.Time` [GH-1937](https://github.com/gophercloud/gophercloud/pull/1937)
* Fixed issue with unmarshalling/decoding slices of composed structs [GH-1964](https://github.com/gophercloud/gophercloud/pull/1964)

## 0.10.0 (April 12, 2020)

UPGRADE NOTES

* The various `IDFromName` convenience functions have been moved to https://github.com/gophercloud/utils [GH-1897](https://github.com/gophercloud/gophercloud/pull/1897)
* `sharedfilesystems/v2/shares.GetExportLocations` was renamed to `sharedfilesystems/v2/shares.ListExportLocations` [GH-1932](https://github.com/gophercloud/gophercloud/pull/1932)

IMPROVEMENTS 

* Added `blockstorage/extensions/volumeactions.SetBootable` [GH-1891](https://github.com/gophercloud/gophercloud/pull/1891)
* Added `blockstorage/extensions/backups.Export` [GH-1894](https://github.com/gophercloud/gophercloud/pull/1894)
* Added `blockstorage/extensions/backups.Import` [GH-1894](https://github.com/gophercloud/gophercloud/pull/1894)
* Added `placement/v1/resourceproviders.GetTraits` [GH-1899](https://github.com/gophercloud/gophercloud/pull/1899)
* Added the ability to authenticate with Amazon EC2 Credentials [GH-1900](https://github.com/gophercloud/gophercloud/pull/1900)
* Added ability to list Nova services by binary and host [GH-1904](https://github.com/gophercloud/gophercloud/pull/1904)
* Added `compute/v2/extensions/services.Update` [GH-1902](https://github.com/gophercloud/gophercloud/pull/1902)
* Added system scope to v3 authentication [GH-1908](https://github.com/gophercloud/gophercloud/pull/1908)
* Added `identity/v3/extensions/ec2tokens.ValidateS3Token` [GH-1906](https://github.com/gophercloud/gophercloud/pull/1906)
* Added `containerinfra/v1/clusters.Cluster.HealthStatus` [GH-1910](https://github.com/gophercloud/gophercloud/pull/1910)
* Added `containerinfra/v1/clusters.Cluster.HealthStatusReason` [GH-1910](https://github.com/gophercloud/gophercloud/pull/1910)
* Added `loadbalancer/v2/amphorae.Failover` [GH-1912](https://github.com/gophercloud/gophercloud/pull/1912)
* Added `identity/v3/extensions/ec2credentials.List` [GH-1916](https://github.com/gophercloud/gophercloud/pull/1916)
* Added `identity/v3/extensions/ec2credentials.Get` [GH-1916](https://github.com/gophercloud/gophercloud/pull/1916)
* Added `identity/v3/extensions/ec2credentials.Create` [GH-1916](https://github.com/gophercloud/gophercloud/pull/1916)
* Added `identity/v3/extensions/ec2credentials.Delete` [GH-1916](https://github.com/gophercloud/gophercloud/pull/1916)
* Added `ErrUnexpectedResponseCode.ResponseHeader` [GH-1919](https://github.com/gophercloud/gophercloud/pull/1919)
* Added support for TOTP authentication [GH-1922](https://github.com/gophercloud/gophercloud/pull/1922)
* `sharedfilesystems/v2/shares.GetExportLocations` was renamed to `sharedfilesystems/v2/shares.ListExportLocations` [GH-1932](https://github.com/gophercloud/gophercloud/pull/1932)
* Added `sharedfilesystems/v2/shares.GetExportLocation` [GH-1932](https://github.com/gophercloud/gophercloud/pull/1932)
* Added `sharedfilesystems/v2/shares.Revert` [GH-1931](https://github.com/gophercloud/gophercloud/pull/1931)
* Added `sharedfilesystems/v2/shares.ResetStatus` [GH-1931](https://github.com/gophercloud/gophercloud/pull/1931)
* Added `sharedfilesystems/v2/shares.ForceDelete` [GH-1931](https://github.com/gophercloud/gophercloud/pull/1931)
* Added `sharedfilesystems/v2/shares.Unmanage` [GH-1931](https://github.com/gophercloud/gophercloud/pull/1931)
* Added `blockstorage/v3/attachments.Create` [GH-1934](https://github.com/gophercloud/gophercloud/pull/1934)
* Added `blockstorage/v3/attachments.List` [GH-1934](https://github.com/gophercloud/gophercloud/pull/1934)
* Added `blockstorage/v3/attachments.Get` [GH-1934](https://github.com/gophercloud/gophercloud/pull/1934)
* Added `blockstorage/v3/attachments.Update` [GH-1934](https://github.com/gophercloud/gophercloud/pull/1934)
* Added `blockstorage/v3/attachments.Delete` [GH-1934](https://github.com/gophercloud/gophercloud/pull/1934)
* Added `blockstorage/v3/attachments.Complete` [GH-1934](https://github.com/gophercloud/gophercloud/pull/1934)

BUG FIXES

* Fixed issue with Orchestration `get_file` only being able to read JSON and YAML files [GH-1915](https://github.com/gophercloud/gophercloud/pull/1915)

## 0.9.0 (March 10, 2020)

UPGRADE NOTES

* The way we implement new API result fields added by microversions has changed. Previously, we would declare a dedicated `ExtractFoo` function in a file called `microversions.go`. Now, we are declaring those fields inline of the original result struct as a pointer. [GH-1854](https://github.com/gophercloud/gophercloud/pull/1854)

* `compute/v2/servers.CreateOpts.Networks` has changed from `[]Network` to `interface{}` in order to support creating servers that have no networks. [GH-1884](https://github.com/gophercloud/gophercloud/pull/1884)

IMPROVEMENTS

* Added `compute/v2/extensions/instanceactions.List` [GH-1848](https://github.com/gophercloud/gophercloud/pull/1848)
* Added `compute/v2/extensions/instanceactions.Get` [GH-1848](https://github.com/gophercloud/gophercloud/pull/1848)
* Added `networking/v2/ports.List.FixedIPs` [GH-1849](https://github.com/gophercloud/gophercloud/pull/1849)
* Added `identity/v3/extensions/trusts.List` [GH-1855](https://github.com/gophercloud/gophercloud/pull/1855)
* Added `identity/v3/extensions/trusts.Get` [GH-1855](https://github.com/gophercloud/gophercloud/pull/1855)
* Added `identity/v3/extensions/trusts.Trust.ExpiresAt` [GH-1857](https://github.com/gophercloud/gophercloud/pull/1857)
* Added `identity/v3/extensions/trusts.Trust.DeletedAt` [GH-1857](https://github.com/gophercloud/gophercloud/pull/1857)
* Added `compute/v2/extensions/instanceactions.InstanceActionDetail` [GH-1851](https://github.com/gophercloud/gophercloud/pull/1851)
* Added `compute/v2/extensions/instanceactions.Event` [GH-1851](https://github.com/gophercloud/gophercloud/pull/1851)
* Added `compute/v2/extensions/instanceactions.ListOpts` [GH-1858](https://github.com/gophercloud/gophercloud/pull/1858)
* Added `objectstorage/v1/containers.UpdateOpts.TempURLKey` [GH-1864](https://github.com/gophercloud/gophercloud/pull/1864)
* Added `objectstorage/v1/containers.UpdateOpts.TempURLKey2` [GH-1864](https://github.com/gophercloud/gophercloud/pull/1864)
* Added `placement/v1/resourceproviders.GetUsages` [GH-1862](https://github.com/gophercloud/gophercloud/pull/1862)
* Added `placement/v1/resourceproviders.GetInventories` [GH-1862](https://github.com/gophercloud/gophercloud/pull/1862)
* Added `imageservice/v2/images.ReplaceImageMinRam` [GH-1867](https://github.com/gophercloud/gophercloud/pull/1867)
* Added `objectstorage/v1/containers.UpdateOpts.TempURLKey` [GH-1865](https://github.com/gophercloud/gophercloud/pull/1865)
* Added `objectstorage/v1/containers.CreateOpts.TempURLKey2` [GH-1865](https://github.com/gophercloud/gophercloud/pull/1865)
* Added `blockstorage/extensions/volumetransfers.List` [GH-1869](https://github.com/gophercloud/gophercloud/pull/1869)
* Added `blockstorage/extensions/volumetransfers.Create` [GH-1869](https://github.com/gophercloud/gophercloud/pull/1869)
* Added `blockstorage/extensions/volumetransfers.Accept` [GH-1869](https://github.com/gophercloud/gophercloud/pull/1869)
* Added `blockstorage/extensions/volumetransfers.Get` [GH-1869](https://github.com/gophercloud/gophercloud/pull/1869)
* Added `blockstorage/extensions/volumetransfers.Delete` [GH-1869](https://github.com/gophercloud/gophercloud/pull/1869)
* Added `blockstorage/extensions/backups.RestoreFromBackup` [GH-1871](https://github.com/gophercloud/gophercloud/pull/1871)
* Added `blockstorage/v3/volumes.CreateOpts.BackupID` [GH-1871](https://github.com/gophercloud/gophercloud/pull/1871)
* Added `blockstorage/v3/volumes.Volume.BackupID` [GH-1871](https://github.com/gophercloud/gophercloud/pull/1871)
* Added `identity/v3/projects.ListOpts.Tags` [GH-1882](https://github.com/gophercloud/gophercloud/pull/1882)
* Added `identity/v3/projects.ListOpts.TagsAny` [GH-1882](https://github.com/gophercloud/gophercloud/pull/1882)
* Added `identity/v3/projects.ListOpts.NotTags` [GH-1882](https://github.com/gophercloud/gophercloud/pull/1882)
* Added `identity/v3/projects.ListOpts.NotTagsAny` [GH-1882](https://github.com/gophercloud/gophercloud/pull/1882)
* Added `identity/v3/projects.CreateOpts.Tags` [GH-1882](https://github.com/gophercloud/gophercloud/pull/1882)
* Added `identity/v3/projects.UpdateOpts.Tags` [GH-1882](https://github.com/gophercloud/gophercloud/pull/1882)
* Added `identity/v3/projects.Project.Tags` [GH-1882](https://github.com/gophercloud/gophercloud/pull/1882)
* Changed `compute/v2/servers.CreateOpts.Networks` from `[]Network` to `interface{}` to support creating servers with no networks. [GH-1884](https://github.com/gophercloud/gophercloud/pull/1884)


BUG FIXES

* Added support for `int64` headers, which were previously being silently dropped [GH-1860](https://github.com/gophercloud/gophercloud/pull/1860)
* Allow image properties with empty values [GH-1875](https://github.com/gophercloud/gophercloud/pull/1875)
* Fixed `compute/v2/extensions/extendedserverattributes.ServerAttributesExt.Userdata` JSON tag [GH-1881](https://github.com/gophercloud/gophercloud/pull/1881)

## 0.8.0 (February 8, 2020)

UPGRADE NOTES

* The behavior of `keymanager/v1/acls.SetOpts` has changed. Instead of a struct, it is now `[]SetOpt`. See [GH-1816](https://github.com/gophercloud/gophercloud/pull/1816) for implementation details.

IMPROVEMENTS

* The result of `containerinfra/v1/clusters.Resize` now returns only the UUID when calling `Extract`. This is a backwards-breaking change from the previous struct that was returned [GH-1649](https://github.com/gophercloud/gophercloud/pull/1649)
* Added `compute/v2/extensions/shelveunshelve.Shelve` [GH-1799](https://github.com/gophercloud/gophercloud/pull/1799)
* Added `compute/v2/extensions/shelveunshelve.ShelveOffload` [GH-1799](https://github.com/gophercloud/gophercloud/pull/1799)
* Added `compute/v2/extensions/shelveunshelve.Unshelve` [GH-1799](https://github.com/gophercloud/gophercloud/pull/1799)
* Added `containerinfra/v1/nodegroups.Get` [GH-1774](https://github.com/gophercloud/gophercloud/pull/1774)
* Added `containerinfra/v1/nodegroups.List` [GH-1774](https://github.com/gophercloud/gophercloud/pull/1774)
* Added `orchestration/v1/resourcetypes.List` [GH-1806](https://github.com/gophercloud/gophercloud/pull/1806)
* Added `orchestration/v1/resourcetypes.GetSchema` [GH-1806](https://github.com/gophercloud/gophercloud/pull/1806)
* Added `orchestration/v1/resourcetypes.GenerateTemplate` [GH-1806](https://github.com/gophercloud/gophercloud/pull/1806)
* Added `keymanager/v1/acls.SetOpt` and changed `keymanager/v1/acls.SetOpts` to `[]SetOpt` [GH-1816](https://github.com/gophercloud/gophercloud/pull/1816)
* Added `blockstorage/apiversions.List` [GH-458](https://github.com/gophercloud/gophercloud/pull/458)
* Added `blockstorage/apiversions.Get` [GH-458](https://github.com/gophercloud/gophercloud/pull/458)
* Added `StatusCodeError` interface and `GetStatusCode` convenience method [GH-1820](https://github.com/gophercloud/gophercloud/pull/1820)
* Added pagination support to `compute/v2/extensions/usage.SingleTenant` [GH-1819](https://github.com/gophercloud/gophercloud/pull/1819)
* Added pagination support to `compute/v2/extensions/usage.AllTenants` [GH-1819](https://github.com/gophercloud/gophercloud/pull/1819)
* Added `placement/v1/resourceproviders.List` [GH-1815](https://github.com/gophercloud/gophercloud/pull/1815)
* Allow `CreateMemberOptsBuilder` to be passed in `loadbalancer/v2/pools.Create` [GH-1822](https://github.com/gophercloud/gophercloud/pull/1822)
* Added `Backup` to `loadbalancer/v2/pools.CreateMemberOpts` [GH-1824](https://github.com/gophercloud/gophercloud/pull/1824)
* Added `MonitorAddress` to `loadbalancer/v2/pools.CreateMemberOpts` [GH-1824](https://github.com/gophercloud/gophercloud/pull/1824)
* Added `MonitorPort` to `loadbalancer/v2/pools.CreateMemberOpts` [GH-1824](https://github.com/gophercloud/gophercloud/pull/1824)
* Changed `Impersonation` to a non-required field in `identity/v3/extensions/trusts.CreateOpts` [GH-1818](https://github.com/gophercloud/gophercloud/pull/1818)
* Added `InsertHeaders` to `loadbalancer/v2/listeners.UpdateOpts` [GH-1835](https://github.com/gophercloud/gophercloud/pull/1835)
* Added `NUMATopology` to `baremetalintrospection/v1/introspection.Data` [GH-1842](https://github.com/gophercloud/gophercloud/pull/1842)
* Added `placement/v1/resourceproviders.Create` [GH-1841](https://github.com/gophercloud/gophercloud/pull/1841)
* Added `blockstorage/extensions/volumeactions.UploadImageOpts.Visibility` [GH-1873](https://github.com/gophercloud/gophercloud/pull/1873)
* Added `blockstorage/extensions/volumeactions.UploadImageOpts.Protected` [GH-1873](https://github.com/gophercloud/gophercloud/pull/1873)
* Added `blockstorage/extensions/volumeactions.VolumeImage.Visibility` [GH-1873](https://github.com/gophercloud/gophercloud/pull/1873)
* Added `blockstorage/extensions/volumeactions.VolumeImage.Protected` [GH-1873](https://github.com/gophercloud/gophercloud/pull/1873)

BUG FIXES

* Changed `sort_key` to `sort_keys` in ` workflow/v2/crontriggers.ListOpts` [GH-1809](https://github.com/gophercloud/gophercloud/pull/1809)
* Allow `blockstorage/extensions/schedulerstats.Capabilities.MaxOverSubscriptionRatio` to accept both string and int/float responses [GH-1817](https://github.com/gophercloud/gophercloud/pull/1817)
* Fixed bug in `NewLoadBalancerV2` for situations when the LBaaS service was advertised without a `/v2.0` endpoint [GH-1829](https://github.com/gophercloud/gophercloud/pull/1829)
* Fixed JSON tags in `baremetal/v1/ports.UpdateOperation` [GH-1840](https://github.com/gophercloud/gophercloud/pull/1840)
* Fixed JSON tags in `networking/v2/extensions/lbaas/vips.commonResult.Extract()` [GH-1840](https://github.com/gophercloud/gophercloud/pull/1840)

## 0.7.0 (December 3, 2019)

IMPROVEMENTS

* Allow a token to be used directly for authentication instead of generating a new token based on a given token [GH-1752](https://github.com/gophercloud/gophercloud/pull/1752)
* Moved `tags.ServerTagsExt` to servers.TagsExt` [GH-1760](https://github.com/gophercloud/gophercloud/pull/1760)
* Added `tags`, `tags-any`, `not-tags`, and `not-tags-any` to `compute/v2/servers.ListOpts` [GH-1759](https://github.com/gophercloud/gophercloud/pull/1759)
* Added `AccessRule` to `identity/v3/applicationcredentials` [GH-1758](https://github.com/gophercloud/gophercloud/pull/1758)
* Gophercloud no longer returns an error when multiple endpoints are found. Instead, it will choose the first endpoint and discard the others [GH-1766](https://github.com/gophercloud/gophercloud/pull/1766)
* Added `networking/v2/extensions/fwaas_v2/rules.Create` [GH-1768](https://github.com/gophercloud/gophercloud/pull/1768)
* Added `networking/v2/extensions/fwaas_v2/rules.Delete` [GH-1771](https://github.com/gophercloud/gophercloud/pull/1771)
* Added `loadbalancer/v2/providers.List` [GH-1765](https://github.com/gophercloud/gophercloud/pull/1765)
* Added `networking/v2/extensions/fwaas_v2/rules.Get` [GH-1772](https://github.com/gophercloud/gophercloud/pull/1772)
* Added `networking/v2/extensions/fwaas_v2/rules.Update` [GH-1776](https://github.com/gophercloud/gophercloud/pull/1776)
* Added `networking/v2/extensions/fwaas_v2/rules.List` [GH-1783](https://github.com/gophercloud/gophercloud/pull/1783)
* Added `MaxRetriesDown` into `loadbalancer/v2/monitors.CreateOpts` [GH-1785](https://github.com/gophercloud/gophercloud/pull/1785)
* Added `MaxRetriesDown` into `loadbalancer/v2/monitors.UpdateOpts` [GH-1786](https://github.com/gophercloud/gophercloud/pull/1786)
* Added `MaxRetriesDown` into `loadbalancer/v2/monitors.Monitor` [GH-1787](https://github.com/gophercloud/gophercloud/pull/1787)
* Added `MaxRetriesDown` into `loadbalancer/v2/monitors.ListOpts` [GH-1788](https://github.com/gophercloud/gophercloud/pull/1788)
* Updated `go.mod` dependencies, specifically to account for CVE-2019-11840 with `golang.org/x/crypto` [GH-1793](https://github.com/gophercloud/gophercloud/pull/1788)

## 0.6.0 (October 17, 2019)

UPGRADE NOTES

* The way reauthentication works has been refactored. This should not cause a problem, but please report bugs if it does. See [GH-1746](https://github.com/gophercloud/gophercloud/pull/1746) for more information.

IMPROVEMENTS

* Added `networking/v2/extensions/quotas.Get` [GH-1742](https://github.com/gophercloud/gophercloud/pull/1742)
* Added `networking/v2/extensions/quotas.Update` [GH-1747](https://github.com/gophercloud/gophercloud/pull/1747)
* Refactored the reauthentication implementation to use goroutines and added a check to prevent an infinite loop in certain situations. [GH-1746](https://github.com/gophercloud/gophercloud/pull/1746)

BUG FIXES

* Changed `Flavor` to `FlavorID` in `loadbalancer/v2/loadbalancers` [GH-1744](https://github.com/gophercloud/gophercloud/pull/1744)
* Changed `Flavor` to `FlavorID` in `networking/v2/extensions/lbaas_v2/loadbalancers` [GH-1744](https://github.com/gophercloud/gophercloud/pull/1744)
* The `go-yaml` dependency was updated to `v2.2.4` to fix possible DDOS vulnerabilities [GH-1751](https://github.com/gophercloud/gophercloud/pull/1751)

## 0.5.0 (October 13, 2019)

IMPROVEMENTS

* Added `VolumeType` to `compute/v2/extensions/bootfromvolume.BlockDevice`[GH-1690](https://github.com/gophercloud/gophercloud/pull/1690)
* Added `networking/v2/extensions/layer3/portforwarding.List` [GH-1688](https://github.com/gophercloud/gophercloud/pull/1688)
* Added `networking/v2/extensions/layer3/portforwarding.Get` [GH-1698](https://github.com/gophercloud/gophercloud/pull/1696)
* Added `compute/v2/extensions/tags.ReplaceAll` [GH-1696](https://github.com/gophercloud/gophercloud/pull/1696)
* Added `compute/v2/extensions/tags.Add` [GH-1696](https://github.com/gophercloud/gophercloud/pull/1696)
* Added `networking/v2/extensions/layer3/portforwarding.Update` [GH-1703](https://github.com/gophercloud/gophercloud/pull/1703)
* Added `ExtractDomain` method to token results in `identity/v3/tokens` [GH-1712](https://github.com/gophercloud/gophercloud/pull/1712)
* Added `AllowedCIDRs` to `loadbalancer/v2/listeners.CreateOpts` [GH-1710](https://github.com/gophercloud/gophercloud/pull/1710)
* Added `AllowedCIDRs` to `loadbalancer/v2/listeners.UpdateOpts` [GH-1710](https://github.com/gophercloud/gophercloud/pull/1710)
* Added `AllowedCIDRs` to `loadbalancer/v2/listeners.Listener` [GH-1710](https://github.com/gophercloud/gophercloud/pull/1710)
* Added `compute/v2/extensions/tags.Add` [GH-1695](https://github.com/gophercloud/gophercloud/pull/1695)
* Added `compute/v2/extensions/tags.ReplaceAll` [GH-1694](https://github.com/gophercloud/gophercloud/pull/1694)
* Added `compute/v2/extensions/tags.Delete` [GH-1699](https://github.com/gophercloud/gophercloud/pull/1699)
* Added `compute/v2/extensions/tags.DeleteAll` [GH-1700](https://github.com/gophercloud/gophercloud/pull/1700)
* Added `ImageStatusImporting` as an image status [GH-1725](https://github.com/gophercloud/gophercloud/pull/1725)
* Added `ByPath` to `baremetalintrospection/v1/introspection.RootDiskType` [GH-1730](https://github.com/gophercloud/gophercloud/pull/1730)
* Added `AttachedVolumes` to `compute/v2/servers.Server` [GH-1732](https://github.com/gophercloud/gophercloud/pull/1732)
* Enable unmarshaling server tags to a `compute/v2/servers.Server` struct [GH-1734]
* Allow setting an empty members list in `loadbalancer/v2/pools.BatchUpdateMembers` [GH-1736](https://github.com/gophercloud/gophercloud/pull/1736)
* Allow unsetting members' subnet ID and name in `loadbalancer/v2/pools.BatchUpdateMemberOpts` [GH-1738](https://github.com/gophercloud/gophercloud/pull/1738)

BUG FIXES

* Changed struct type for options in `networking/v2/extensions/lbaas_v2/listeners` to `UpdateOptsBuilder` interface instead of specific UpdateOpts type [GH-1705](https://github.com/gophercloud/gophercloud/pull/1705)
* Changed struct type for options in `networking/v2/extensions/lbaas_v2/loadbalancers` to `UpdateOptsBuilder` interface instead of specific UpdateOpts type [GH-1706](https://github.com/gophercloud/gophercloud/pull/1706)
* Fixed issue with `blockstorage/v1/volumes.Create` where the response was expected to be 202 [GH-1720](https://github.com/gophercloud/gophercloud/pull/1720)
* Changed `DefaultTlsContainerRef` from `string` to `*string` in `loadbalancer/v2/listeners.UpdateOpts` to allow the value to be removed during update. [GH-1723](https://github.com/gophercloud/gophercloud/pull/1723)
* Changed `SniContainerRefs` from `[]string{}` to `*[]string{}` in `loadbalancer/v2/listeners.UpdateOpts` to allow the value to be removed during update. [GH-1723](https://github.com/gophercloud/gophercloud/pull/1723)
* Changed `DefaultTlsContainerRef` from `string` to `*string` in `networking/v2/extensions/lbaas_v2/listeners.UpdateOpts` to allow the value to be removed during update. [GH-1723](https://github.com/gophercloud/gophercloud/pull/1723)
* Changed `SniContainerRefs` from `[]string{}` to `*[]string{}` in `networking/v2/extensions/lbaas_v2/listeners.UpdateOpts` to allow the value to be removed during update. [GH-1723](https://github.com/gophercloud/gophercloud/pull/1723)


## 0.4.0 (September 3, 2019)

IMPROVEMENTS

* Added `blockstorage/extensions/quotasets.results.QuotaSet.Groups` [GH-1668](https://github.com/gophercloud/gophercloud/pull/1668)
* Added `blockstorage/extensions/quotasets.results.QuotaUsageSet.Groups` [GH-1668](https://github.com/gophercloud/gophercloud/pull/1668)
* Added `containerinfra/v1/clusters.CreateOpts.FixedNetwork` [GH-1674](https://github.com/gophercloud/gophercloud/pull/1674)
* Added `containerinfra/v1/clusters.CreateOpts.FixedSubnet` [GH-1676](https://github.com/gophercloud/gophercloud/pull/1676)
* Added `containerinfra/v1/clusters.CreateOpts.FloatingIPEnabled` [GH-1677](https://github.com/gophercloud/gophercloud/pull/1677)
* Added `CreatedAt` and `UpdatedAt` to `loadbalancers/v2/loadbalancers.LoadBalancer` [GH-1681](https://github.com/gophercloud/gophercloud/pull/1681)
* Added `networking/v2/extensions/layer3/portforwarding.Create` [GH-1651](https://github.com/gophercloud/gophercloud/pull/1651)
* Added `networking/v2/extensions/agents.ListDHCPNetworks` [GH-1686](https://github.com/gophercloud/gophercloud/pull/1686)
* Added `networking/v2/extensions/layer3/portforwarding.Delete` [GH-1652](https://github.com/gophercloud/gophercloud/pull/1652)
* Added `compute/v2/extensions/tags.List` [GH-1679](https://github.com/gophercloud/gophercloud/pull/1679)
* Added `compute/v2/extensions/tags.Check` [GH-1679](https://github.com/gophercloud/gophercloud/pull/1679)

BUG FIXES

* Changed `identity/v3/endpoints.ListOpts.RegionID` from `int` to `string` [GH-1664](https://github.com/gophercloud/gophercloud/pull/1664)
* Fixed issue where older time formats in some networking APIs/resources were unable to be parsed [GH-1671](https://github.com/gophercloud/gophercloud/pull/1664)
* Changed `SATA`, `SCSI`, and `SAS` types to `InterfaceType` in `baremetal/v1/nodes` [GH-1683]

## 0.3.0 (July 31, 2019)

IMPROVEMENTS

* Added `baremetal/apiversions.List` [GH-1577](https://github.com/gophercloud/gophercloud/pull/1577)
* Added `baremetal/apiversions.Get` [GH-1577](https://github.com/gophercloud/gophercloud/pull/1577)
* Added `compute/v2/extensions/servergroups.CreateOpts.Policy` [GH-1636](https://github.com/gophercloud/gophercloud/pull/1636)
* Added `identity/v3/extensions/trusts.Create` [GH-1644](https://github.com/gophercloud/gophercloud/pull/1644)
* Added `identity/v3/extensions/trusts.Delete` [GH-1644](https://github.com/gophercloud/gophercloud/pull/1644)
* Added `CreatedAt` and `UpdatedAt` to `networking/v2/extensions/layer3/floatingips.FloatingIP` [GH-1647](https://github.com/gophercloud/gophercloud/issues/1646)
* Added `CreatedAt` and `UpdatedAt` to `networking/v2/extensions/security/groups.SecGroup` [GH-1654](https://github.com/gophercloud/gophercloud/issues/1654)
* Added `CreatedAt` and `UpdatedAt` to `networking/v2/networks.Network` [GH-1657](https://github.com/gophercloud/gophercloud/issues/1657)
* Added `keymanager/v1/containers.CreateSecretRef` [GH-1659](https://github.com/gophercloud/gophercloud/issues/1659)
* Added `keymanager/v1/containers.DeleteSecretRef` [GH-1659](https://github.com/gophercloud/gophercloud/issues/1659)
* Added `sharedfilesystems/v2/shares.GetMetadata` [GH-1656](https://github.com/gophercloud/gophercloud/issues/1656)
* Added `sharedfilesystems/v2/shares.GetMetadatum` [GH-1656](https://github.com/gophercloud/gophercloud/issues/1656)
* Added `sharedfilesystems/v2/shares.SetMetadata` [GH-1656](https://github.com/gophercloud/gophercloud/issues/1656)
* Added `sharedfilesystems/v2/shares.UpdateMetadata` [GH-1656](https://github.com/gophercloud/gophercloud/issues/1656)
* Added `sharedfilesystems/v2/shares.DeleteMetadatum` [GH-1656](https://github.com/gophercloud/gophercloud/issues/1656)
* Added `sharedfilesystems/v2/sharetypes.IDFromName` [GH-1662](https://github.com/gophercloud/gophercloud/issues/1662)



BUG FIXES

* Changed `baremetal/v1/nodes.CleanStep.Args` from `map[string]string` to `map[string]interface{}` [GH-1638](https://github.com/gophercloud/gophercloud/pull/1638)
* Removed `URLPath` and `ExpectedCodes` from `loadbalancer/v2/monitors.ToMonitorCreateMap` since Octavia now provides default values when these fields are not specified [GH-1640](https://github.com/gophercloud/gophercloud/pull/1540)


## 0.2.0 (June 17, 2019)

IMPROVEMENTS

* Added `networking/v2/extensions/qos/rules.ListBandwidthLimitRules` [GH-1584](https://github.com/gophercloud/gophercloud/pull/1584)
* Added `networking/v2/extensions/qos/rules.GetBandwidthLimitRule` [GH-1584](https://github.com/gophercloud/gophercloud/pull/1584)
* Added `networking/v2/extensions/qos/rules.CreateBandwidthLimitRule` [GH-1584](https://github.com/gophercloud/gophercloud/pull/1584)
* Added `networking/v2/extensions/qos/rules.UpdateBandwidthLimitRule` [GH-1589](https://github.com/gophercloud/gophercloud/pull/1589)
* Added `networking/v2/extensions/qos/rules.DeleteBandwidthLimitRule` [GH-1590](https://github.com/gophercloud/gophercloud/pull/1590)
* Added `networking/v2/extensions/qos/policies.List` [GH-1591](https://github.com/gophercloud/gophercloud/pull/1591)
* Added `networking/v2/extensions/qos/policies.Get` [GH-1593](https://github.com/gophercloud/gophercloud/pull/1593)
* Added `networking/v2/extensions/qos/rules.ListDSCPMarkingRules` [GH-1594](https://github.com/gophercloud/gophercloud/pull/1594)
* Added `networking/v2/extensions/qos/policies.Create` [GH-1595](https://github.com/gophercloud/gophercloud/pull/1595)
* Added `compute/v2/extensions/diagnostics.Get` [GH-1592](https://github.com/gophercloud/gophercloud/pull/1592)
* Added `networking/v2/extensions/qos/policies.Update` [GH-1603](https://github.com/gophercloud/gophercloud/pull/1603)
* Added `networking/v2/extensions/qos/policies.Delete` [GH-1603](https://github.com/gophercloud/gophercloud/pull/1603)
* Added `networking/v2/extensions/qos/rules.CreateDSCPMarkingRule` [GH-1605](https://github.com/gophercloud/gophercloud/pull/1605)
* Added `networking/v2/extensions/qos/rules.UpdateDSCPMarkingRule` [GH-1605](https://github.com/gophercloud/gophercloud/pull/1605)
* Added `networking/v2/extensions/qos/rules.GetDSCPMarkingRule` [GH-1609](https://github.com/gophercloud/gophercloud/pull/1609)
* Added `networking/v2/extensions/qos/rules.DeleteDSCPMarkingRule` [GH-1609](https://github.com/gophercloud/gophercloud/pull/1609)
* Added `networking/v2/extensions/qos/rules.ListMinimumBandwidthRules` [GH-1615](https://github.com/gophercloud/gophercloud/pull/1615)
* Added `networking/v2/extensions/qos/rules.GetMinimumBandwidthRule` [GH-1615](https://github.com/gophercloud/gophercloud/pull/1615)
* Added `networking/v2/extensions/qos/rules.CreateMinimumBandwidthRule` [GH-1615](https://github.com/gophercloud/gophercloud/pull/1615)
* Added `Hostname` to `baremetalintrospection/v1/introspection.Data` [GH-1627](https://github.com/gophercloud/gophercloud/pull/1627)
* Added `networking/v2/extensions/qos/rules.UpdateMinimumBandwidthRule` [GH-1624](https://github.com/gophercloud/gophercloud/pull/1624)
* Added `networking/v2/extensions/qos/rules.DeleteMinimumBandwidthRule` [GH-1624](https://github.com/gophercloud/gophercloud/pull/1624)
* Added `networking/v2/extensions/qos/ruletypes.GetRuleType` [GH-1625](https://github.com/gophercloud/gophercloud/pull/1625)
* Added `Extra` to `baremetalintrospection/v1/introspection.Data` [GH-1611](https://github.com/gophercloud/gophercloud/pull/1611)
* Added `blockstorage/extensions/volumeactions.SetImageMetadata` [GH-1621](https://github.com/gophercloud/gophercloud/pull/1621)

BUG FIXES

* Updated `networking/v2/extensions/qos/rules.UpdateBandwidthLimitRule` to use return code 200 [GH-1606](https://github.com/gophercloud/gophercloud/pull/1606)
* Fixed bug in `compute/v2/extensions/schedulerhints.SchedulerHints.Query` where contents will now be marshalled to a string [GH-1620](https://github.com/gophercloud/gophercloud/pull/1620)

## 0.1.0 (May 27, 2019)

Initial tagged release. 
