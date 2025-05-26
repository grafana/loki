## Main

## [0.8.0](https://github.com/grafana/loki/compare/operator/v0.7.1...operator/v0.8.0) (2025-03-17)


### ⚠ BREAKING CHANGES

* **operator:** Add configuration option for dropping OTLP attributes ([#15857](https://github.com/grafana/loki/issues/15857))

### Features

* **operator:** Add configuration option for dropping OTLP attributes ([#15857](https://github.com/grafana/loki/issues/15857)) ([bd1ea23](https://github.com/grafana/loki/commit/bd1ea2313220b9aa187ff5b252f55512434c1865))
* **operator:** Add support for Swift TLS CA configuration ([#15260](https://github.com/grafana/loki/issues/15260)) ([62a72f6](https://github.com/grafana/loki/commit/62a72f6405d5a5cbb0814fab8010c215b1782c93))
* **operator:** Enable time-based stream-sharding ([#16390](https://github.com/grafana/loki/issues/16390)) ([1b4f1f5](https://github.com/grafana/loki/commit/1b4f1f57fa6ceac405f5ab5d0314de9d97e309d3))
* **operator:** Update Loki operand to v3.4.2 ([#16360](https://github.com/grafana/loki/issues/16360)) ([42f87d3](https://github.com/grafana/loki/commit/42f87d3064b60438df09d1eb799fd50e5753d1f8))


### Bug Fixes

* **operator:** Fix minimum available ingesters for 1x.pico size ([#16035](https://github.com/grafana/loki/issues/16035)) ([40cf074](https://github.com/grafana/loki/commit/40cf074fba0ed0016a8ca64bed554f3d628e7ec6))
* **operator:** Select non-zero delete worker count for all sizes ([#16492](https://github.com/grafana/loki/issues/16492)) ([1e5579a](https://github.com/grafana/loki/commit/1e5579abef02ed03f9dc87cf7d09f52f53768152))
* **operator:** Update maximum OpenShift version ([#16443](https://github.com/grafana/loki/issues/16443)) ([ddf3cfb](https://github.com/grafana/loki/commit/ddf3cfbba7a6529a6902036c486b523b588818e3))
* **operator:** Update OTLP user guide to reflect change in LokiStack ([#16057](https://github.com/grafana/loki/issues/16057)) ([14e2c87](https://github.com/grafana/loki/commit/14e2c875d2bc5d6678f964f1477cfafe6f37e496))
* **operator:** Update skipRange in OpenShift variant ([#15984](https://github.com/grafana/loki/issues/15984)) ([dfbe00c](https://github.com/grafana/loki/commit/dfbe00c88a2f17da11b726a3461c11324c21fcca))

## [0.7.1](https://github.com/grafana/loki/compare/operator/v0.7.0...operator/v0.7.1) (2024-11-11)


### Features

* **operator:** Add support for managed GCP WorkloadIdentity ([#14752](https://github.com/grafana/loki/issues/14752)) ([7635a5c](https://github.com/grafana/loki/commit/7635a5cffa80cf5ff627b8de2ed00fa96c058629))


### Bug Fixes

* **operator:** Add log attribute for level to structured metadata ([#14776](https://github.com/grafana/loki/issues/14776)) ([036c131](https://github.com/grafana/loki/commit/036c1312d5fd797dda9839dc30d5028e8b7f6c59))
* **operator:** Fix maximum OpenShift version ([#14764](https://github.com/grafana/loki/issues/14764)) ([cc496c6](https://github.com/grafana/loki/commit/cc496c68b76b56c457f6c30d696de23698addaa9))
* **operator:** Fix operator release pipeline warnings ([#14817](https://github.com/grafana/loki/issues/14817)) ([e707a3d](https://github.com/grafana/loki/commit/e707a3dfb2e25df76585ab42f715e361233479c8))
* **operator:** update kube-rbac-proxy to upstream registry ([#14809](https://github.com/grafana/loki/issues/14809)) ([568d22f](https://github.com/grafana/loki/commit/568d22f598c763942e3d277aa6005050281e5f49))

## [0.7.0](https://github.com/grafana/loki/compare/operator/v0.6.2...operator/v0.7.0) (2024-11-01)


### ⚠ BREAKING CHANGES

* **operator:** Provide default OTLP attribute configuration ([#14410](https://github.com/grafana/loki/issues/14410))
* **operator:** Rename loki api go module ([#14568](https://github.com/grafana/loki/issues/14568))
* **operator:** Migrate project layout to kubebuilder go/v4 ([#14447](https://github.com/grafana/loki/issues/14447))

### Features

* **operator:** Declare feature FIPS support for OpenShift only ([#14308](https://github.com/grafana/loki/issues/14308)) ([720c303](https://github.com/grafana/loki/commit/720c3037923c174e71a02d99d4bee6271428fbdb))
* **operator:** introduce 1x.pico size ([#14407](https://github.com/grafana/loki/issues/14407)) ([57de81d](https://github.com/grafana/loki/commit/57de81d8c27e221832790443cebaf141353c3e3f))
* **operator:** Provide default OTLP attribute configuration ([#14410](https://github.com/grafana/loki/issues/14410)) ([1b52387](https://github.com/grafana/loki/commit/1b5238721994c00764b6a7e7d63269c5b56d2480))
* **operator:** Update Loki operand to v3.2.1 ([#14526](https://github.com/grafana/loki/issues/14526)) ([5e970e5](https://github.com/grafana/loki/commit/5e970e50b166e73f5563e21c23db3ea99b24642e))
* **operator:** User-guide for OTLP configuration ([#14620](https://github.com/grafana/loki/issues/14620)) ([27b4071](https://github.com/grafana/loki/commit/27b40713540bd60918780cdd4cb645e6761427cb))


### Bug Fixes

* **deps:** update module github.com/prometheus/client_golang to v1.20.5 ([#14655](https://github.com/grafana/loki/issues/14655)) ([e12f843](https://github.com/grafana/loki/commit/e12f8436b4080db54c6d31c6af38416c6fdd7eb4))
* **operator:** add 1x.pico OpenShift UI dropdown menu ([#14660](https://github.com/grafana/loki/issues/14660)) ([4687f37](https://github.com/grafana/loki/commit/4687f377db0a7ae07ffdea354582c882c10b72c4))
* **operator:** Add missing groupBy label for all rules on OpenShift ([#14279](https://github.com/grafana/loki/issues/14279)) ([ce7b2e8](https://github.com/grafana/loki/commit/ce7b2e89d9470e4e6a61a94f2b51ff8b938b5a5e))
* **operator:** correctly ignore again BlotDB dashboards ([#14587](https://github.com/grafana/loki/issues/14587)) ([4879d10](https://github.com/grafana/loki/commit/4879d106bbeea29e331ddb7c9a49274600190032))
* **operator:** Disable automatic discovery of service name ([#14506](https://github.com/grafana/loki/issues/14506)) ([3834c74](https://github.com/grafana/loki/commit/3834c74966b307411732cd3cbaf66305008b10eb))
* **operator:** Disable log level discovery for OpenShift tenancy modes ([#14613](https://github.com/grafana/loki/issues/14613)) ([5034d34](https://github.com/grafana/loki/commit/5034d34ad23451954ea2459c341456da8d93d020))
* **operator:** Fix building the size-calculator image ([#14573](https://github.com/grafana/loki/issues/14573)) ([a79b8fe](https://github.com/grafana/loki/commit/a79b8fe7802964cbb96bde75a7502a8b1e8a23ab))
* **operator:** Fix make build target for size-calculator ([#14551](https://github.com/grafana/loki/issues/14551)) ([e727187](https://github.com/grafana/loki/commit/e727187ec3be2f10c80e984d00c40dad0308b036))
* **operator:** Move OTLP attribute for statefulset name to stream labels ([#14630](https://github.com/grafana/loki/issues/14630)) ([5df3594](https://github.com/grafana/loki/commit/5df3594f791d77031c53d7b0f5b01191de8a23f2))
* **operator:** Use empty initiliazed pod status map when no pods ([#14314](https://github.com/grafana/loki/issues/14314)) ([6f533ed](https://github.com/grafana/loki/commit/6f533ed4386ee2db61680a9021934bfe9a9ba749))


### Code Refactoring

* **operator:** Migrate project layout to kubebuilder go/v4 ([#14447](https://github.com/grafana/loki/issues/14447)) ([dbb3b6e](https://github.com/grafana/loki/commit/dbb3b6edc96f3545a946319c0324518800d286cf))
* **operator:** Rename loki api go module ([#14568](https://github.com/grafana/loki/issues/14568)) ([976d8ab](https://github.com/grafana/loki/commit/976d8ab81c1a79f35d7cec96f6a9c35a9947fa48))

## [0.6.2](https://github.com/grafana/loki/compare/operator/v0.6.1...operator/v0.6.2) (2024-09-11)


### Features

* Ingester Stream Limit Improvements ([#13532](https://github.com/grafana/loki/issues/13532)) ([ec34aaa](https://github.com/grafana/loki/commit/ec34aaa1ff2e616ef223631657b63f7dffedd3cc))
* **operator:** Add alert for discarded samples ([#13512](https://github.com/grafana/loki/issues/13512)) ([5f2a02f](https://github.com/grafana/loki/commit/5f2a02f14222dab891b7851e8f48052d6c9b594a))
* **operator:** Add support for Loki OTLP limits config ([#13446](https://github.com/grafana/loki/issues/13446)) ([d02f435](https://github.com/grafana/loki/commit/d02f435d3bf121b19e15de4f139c95a6d010b25c))
* **operator:** Add support for the volume API ([#13369](https://github.com/grafana/loki/issues/13369)) ([d451e23](https://github.com/grafana/loki/commit/d451e23225047a11b4d5d82900cec4a46d6e7b39))
* **operator:** Enable leader-election ([#13760](https://github.com/grafana/loki/issues/13760)) ([1ba4bff](https://github.com/grafana/loki/commit/1ba4bff005930b173391df35248e6f58e076fa74))
* **operator:** Update Loki operand to v3.1.0 ([#13422](https://github.com/grafana/loki/issues/13422)) ([cf5f52d](https://github.com/grafana/loki/commit/cf5f52dca0db93847218cdd2c3f4860d983381ae))
* **operator:** Update Loki operand to v3.1.1 ([#14042](https://github.com/grafana/loki/issues/14042)) ([7ae1588](https://github.com/grafana/loki/commit/7ae1588200396b73a16fadd2610670a5ce5fd747))


### Bug Fixes

* **deps:** update k8s.io/utils digest to 702e33f ([#14033](https://github.com/grafana/loki/issues/14033)) ([b7eecc7](https://github.com/grafana/loki/commit/b7eecc7a693e96f4d0fe0dcd7583ecdc4dd7283f))
* **operator:** add alertmanager client config to ruler template ([#13182](https://github.com/grafana/loki/issues/13182)) ([6148c37](https://github.com/grafana/loki/commit/6148c3760d701768e442186d4e7d574c7dc16c91))
* **operator:** Allow structured metadata only if V13 schema provided ([#13463](https://github.com/grafana/loki/issues/13463)) ([3ac130b](https://github.com/grafana/loki/commit/3ac130b8a152169766cb173718f2312aeb4f694e))
* **operator:** Don't overwrite annotations for LokiStack ingress resources ([#13708](https://github.com/grafana/loki/issues/13708)) ([f523530](https://github.com/grafana/loki/commit/f52353060dd936cff587ff2060c8616941695ece))
* **operator:** Improve API documentation for schema version ([#13122](https://github.com/grafana/loki/issues/13122)) ([3a9f50f](https://github.com/grafana/loki/commit/3a9f50f5099a02e662b8ac10ddad0b36cd844161))
* **operator:** Remove duplicate conditions from status ([#13497](https://github.com/grafana/loki/issues/13497)) ([527510d](https://github.com/grafana/loki/commit/527510d1a84a981250047dbabba8d492177b8452))
* **operator:** Set object storage for delete requests when using retention ([#13562](https://github.com/grafana/loki/issues/13562)) ([46de4c1](https://github.com/grafana/loki/commit/46de4c1bc839ef682798bec5003123f7d5f4404b))
* **operator:** Skip updating annotations for serviceaccounts ([#13450](https://github.com/grafana/loki/issues/13450)) ([1b9b111](https://github.com/grafana/loki/commit/1b9b11116b48fb37b7015d27104668412fc04937))
* **operator:** Support v3.1.0 in OpenShift dashboards ([#13430](https://github.com/grafana/loki/issues/13430)) ([8279d59](https://github.com/grafana/loki/commit/8279d59f145df9c9132aeff9e3d46c738650027c))
* **operator:** Watch for CredentialsRequests on CCOAuthEnv only ([#13299](https://github.com/grafana/loki/issues/13299)) ([7fc926e](https://github.com/grafana/loki/commit/7fc926e36ea8fca7bd8e9955c8994574535dbbae))

## [0.6.1](https://github.com/grafana/loki/compare/operator/v0.6.0...operator/v0.6.1) (2024-06-03)


### Features

* prepare 3.0.0 release candidate ([#12348](https://github.com/grafana/loki/issues/12348)) ([664e569](https://github.com/grafana/loki/commit/664e569e14ef55a79cd77bdb49e9ffbe0c55bc37))


### Bug Fixes

* **operator:** Bump golang builder to 1.21.9 ([#12503](https://github.com/grafana/loki/issues/12503)) ([f680ee0](https://github.com/grafana/loki/commit/f680ee0453d1b7d315774591293927b988bca223))
* **operator:** Configure Loki to use virtual-host-style URLs for S3 AWS endpoints ([#12469](https://github.com/grafana/loki/issues/12469)) ([0084262](https://github.com/grafana/loki/commit/0084262269f4e2cb94d04e0cc0d40e9666177f06))
* **operator:** Improve validation of provided S3 storage configuration ([#12181](https://github.com/grafana/loki/issues/12181)) ([f9350d6](https://github.com/grafana/loki/commit/f9350d6415d45c3cc2f9c0b4f7cd6f8f219907f2))
* **operator:** Use a minimum value for replay memory ceiling ([#13066](https://github.com/grafana/loki/issues/13066)) ([4f3ed77](https://github.com/grafana/loki/commit/4f3ed77cb92c2ffd605743237e609c28f7841728))
* update to build image 0.33.2, fixes bug with promtail windows DNS resolution ([#12732](https://github.com/grafana/loki/issues/12732)) ([759f42d](https://github.com/grafana/loki/commit/759f42dd50bb4896f5e568691ef32245bb8fb25a))
* updated all dockerfiles go1.22 ([#12708](https://github.com/grafana/loki/issues/12708)) ([71a8f2c](https://github.com/grafana/loki/commit/71a8f2c2b11b419bd8c0af1f859671e5d8730448))

## 0.6.0 (2024-03-19)

- [12228](https://github.com/grafana/loki/pull/12228) **xperimental**: Restructure LokiStack metrics
- [12164](https://github.com/grafana/loki/pull/12164) **periklis**: Use safe bearer token authentication to scrape operator metrics
- [12216](https://github.com/grafana/loki/pull/12216) **xperimental**: Fix duplicate operator metrics due to ServiceMonitor selector
- [12212](https://github.com/grafana/loki/pull/12212) **xperimental**: Keep credentialMode in status when updating schemas
- [12165](https://github.com/grafana/loki/pull/12165) **JoaoBraveCoding**: Change attribute value used for CCO-based credential mode
- [12157](https://github.com/grafana/loki/pull/12157) **periklis**: Fix managed auth features annotation for community-openshift bundle
- [12104](https://github.com/grafana/loki/pull/12104) **periklis**: Upgrade build and runtime dependencies
- [11928](https://github.com/grafana/loki/pull/11928) **periklis**: Fix remote write client timeout config rename
- [12097](https://github.com/grafana/loki/pull/12097) **btaani**: Fix encoding of blocked query pattern in configuration
- [12106](https://github.com/grafana/loki/pull/12106) **xperimental**: Allow setting explicit CredentialMode in LokiStack storage spec
- [11968](https://github.com/grafana/loki/pull/11968) **xperimental**: Extend status to show difference between running and ready
- [12007](https://github.com/grafana/loki/pull/12007) **xperimental**: Extend Azure secret validation
- [12008](https://github.com/grafana/loki/pull/12008) **xperimental**: Support using multiple buckets with AWS STS
- [11964](https://github.com/grafana/loki/pull/11964) **xperimental**: Provide Azure region for managed credentials using environment variable
- [11920](https://github.com/grafana/loki/pull/11920) **xperimental**: Refactor handling of credentials in managed-auth mode
- [11869](https://github.com/grafana/loki/pull/11869) **periklis**: Add support for running with Google Workload Identity
- [11868](https://github.com/grafana/loki/pull/11868) **xperimental**: Integrate support for OpenShift-managed credentials in Azure
- [11854](https://github.com/grafana/loki/pull/11854) **periklis**: Allow custom audience for managed-auth on STS
- [11802](https://github.com/grafana/loki/pull/11802) **xperimental**: Add support for running with Azure Workload Identity
- [11824](https://github.com/grafana/loki/pull/11824) **xperimental**: Improve messages for errors in storage secret
- [11524](https://github.com/grafana/loki/pull/11524) **JoaoBraveCoding**, **periklis**: Add OpenShift cloud credentials support for AWS STS
- [11513](https://github.com/grafana/loki/pull/11513) **btaani**: Add a custom metric that collects Lokistacks requiring a schema upgrade
- [11718](https://github.com/grafana/loki/pull/11718) **periklis**: Upgrade k8s.io, sigs.k8s.io and openshift deps
- [11671](https://github.com/grafana/loki/pull/11671) **JoaoBraveCoding**: Update mixins to fix structured metadata dashboards
- [11624](https://github.com/grafana/loki/pull/11624) **xperimental**: React to changes in ConfigMap used for storage CA
- [11481](https://github.com/grafana/loki/pull/11481) **JoaoBraveCoding**: Adds AWS STS support
- [11533](https://github.com/grafana/loki/pull/11533) **periklis**: Add serviceaccount per LokiStack resource
- [11158](https://github.com/grafana/loki/pull/11158) **btaani**: operator: Add warning for old schema configuration
- [11473](https://github.com/grafana/loki/pull/11473) **JoaoBraveCoding**: Adds structured metadata dashboards
- [11448](https://github.com/grafana/loki/pull/11448) **periklis**: Update Loki operand to v2.9.3
- [11357](https://github.com/grafana/loki/pull/11357) **periklis**: Fix storing authentication credentials in the Loki ConfigMap
- [11393](https://github.com/grafana/loki/pull/11393) **periklis**: Add infra annotations for OpenShift based deployments
- [11094](https://github.com/grafana/loki/pull/11094) **periklis**: Add support for blocking queries per tenant
- [11288](https://github.com/grafana/loki/pull/11288) **periklis**: Fix custom CA for object-store in ruler component
- [11091](https://github.com/grafana/loki/pull/11091) **periklis**: Add automatic stream sharding support
- [11022](https://github.com/grafana/loki/pull/11022) **JoaoBraveCoding**: Remove outdated BoltDB dashboards 
- [10932](https://github.com/grafana/loki/pull/10932) **JoaoBraveCoding**: Adds new value v13 to schema
- [11232](https://github.com/grafana/loki/pull/11232) **periklis**: Update dependencies and dev tools
- [11129](https://github.com/grafana/loki/pull/11129) **periklis**: Update deps to secure webhooks for CVE-2023-44487

## 0.5.0 (2023-10-24)

- [10924](https://github.com/grafana/loki/pull/10924) **periklis**: Update Loki operand to v2.9.2
- [10874](https://github.com/grafana/loki/pull/10874) **periklis**: Bump deps to address CVE-2023-39325 and CVE-2023-44487
- [10854](https://github.com/grafana/loki/pull/10854) **periklis**: Add missing marker/sweeper panels in retention dashboard
- [10717](https://github.com/grafana/loki/pull/10717) **periklis**: Allow SSE settings in AWS S3 object storage secret
- [10715](https://github.com/grafana/loki/pull/10715) **periklis**: Allow endpoint_suffix in azure object storage secret
- [10562](https://github.com/grafana/loki/pull/10562) **periklis**: Add memberlist IPv6 support
- [10720](https://github.com/grafana/loki/pull/10720) **JoaoBraveCoding**: Change default replication factor of 1x.medium to 2
- [10600](https://github.com/grafana/loki/pull/10600) **periklis**: Update Loki operand to v2.9.1
- [10545](https://github.com/grafana/loki/pull/10545) **xperimental**: Update gateway arguments to enable namespace extraction
- [10558](https://github.com/grafana/loki/pull/10558) **periklis**: Upgrade dashboards for for Loki v2.9.0
- [10539](https://github.com/grafana/loki/pull/10539) **periklis**: Update Loki operand to v2.9.0
- [10418](https://github.com/grafana/loki/pull/10418) **btaani**: Use a condition to warn when labels for zone-awareness are empty
- [9468](https://github.com/grafana/loki/pull/9468) **periklis**: Add support for reconciling loki-mixin dashboards on OpenShift Console
- [9942](https://github.com/grafana/loki/pull/9942) **btaani**: Use a condition to warn when there are no nodes with matching labels for zone-awareness

## 0.4.0 (2023-07-27)

- [10019](https://github.com/grafana/loki/pull/10019) **periklis**: Update Loki operand to v2.8.3
- [9972](https://github.com/grafana/loki/pull/9972) **JoaoBraveCoding**: Fix OIDC.IssuerCAPath by updating it to type CASpec
- [9931](https://github.com/grafana/loki/pull/9931) **aminesnow**: Custom configuration for LokiStack admin groups
- [9971](https://github.com/grafana/loki/pull/9971) **aminesnow**: Add namespace and tenantId labels to RecordingRules
- [9906](https://github.com/grafana/loki/pull/9906) **JoaoBraveCoding**: Add mTLS authentication to tenants
- [9963](https://github.com/grafana/loki/pull/9963) **xperimental**: Fix application tenant alertmanager configuration
- [9795](https://github.com/grafana/loki/pull/9795) **JoaoBraveCoding**: Add initContainer to zone aware components to gatekeep them from starting without the AZ annotation
- [9503](https://github.com/grafana/loki/pull/9503) **shwetaap**: Add Pod annotations with node topology labels to support zone aware scheduling
- [9930](https://github.com/grafana/loki/pull/9930) **periklis**: Use PodAntiAffinity for all components
- [9860](https://github.com/grafana/loki/pull/9860) **xperimental**: Fix update of labels and annotations of PodTemplates
- [9830](https://github.com/grafana/loki/pull/9830) **periklis**: Expose limits config setting cardinality_limit
- [9600](https://github.com/grafana/loki/pull/9600) **periklis**: Add rules labels filters for openshift-logging application tenant
- [9735](https://github.com/grafana/loki/pull/9735) **JoaoBraveCoding** Adjust 1x.extra-small resources according to findings
- [9689](https://github.com/grafana/loki/pull/9689) **xperimental**: Fix availability of demo LokiStack size
- [9630](https://github.com/grafana/loki/pull/9630) **jpinsonneau**: Expose per_stream_rate_limit & burst
- [9623](https://github.com/grafana/loki/pull/9623) **periklis**: Fix timeout config constructor when only tenants limits
- [9457](https://github.com/grafana/loki/pull/9457) **Red-GV**: Set seccomp profile to runtime default
- [9448](https://github.com/grafana/loki/pull/9448) **btaani**: Include runtime-config in compiling the SHA1 checksum
- [9511](https://github.com/grafana/loki/pull/9511) **xperimental**: Do not update status after setting degraded condition
- [9405](https://github.com/grafana/loki/pull/9405) **periklis**: Add support for configuring HTTP server timeouts
- [9378](https://github.com/grafana/loki/pull/9378) **aminesnow**: Add zone aware API spec validation
- [9408](https://github.com/grafana/loki/pull/9408) **JoaoBraveCoding**: Add PodAntiAffinity overwrites per component
- [9429](https://github.com/grafana/loki/pull/9429) **aminesnow**: Add default TopologySpreadContraints to Gateway
- [9418](https://github.com/grafana/loki/pull/9418) **JoaoBraveCoding**: Add default TopologySpreadContaints to Querier
- [9383](https://github.com/grafana/loki/pull/9383) **JoaoBraveCoding**: Add default TopologySpreadContaints to Distributor
- [9406](https://github.com/grafana/loki/pull/9406) **aminesnow**: Add label selector to zone awareness TopologySpreadConstraints
- [9366](https://github.com/grafana/loki/pull/9366) **periklis**: Add support for custom tenant topology in rules
- [9315](https://github.com/grafana/loki/pull/9315) **aminesnow**: Add zone awareness spec to LokiStack
- [9343](https://github.com/grafana/loki/pull/9343) **JoaoBraveCoding**: Add default PodAntiAffinity to Query Frontend
- [9346](https://github.com/grafana/loki/pull/9346) **periklis**: Enable Route by default on OpenShift clusters
- [9339](https://github.com/grafana/loki/pull/9339) **JoaoBraveCoding**: Add default PodAntiAffinity to Ruler
- [9329](https://github.com/grafana/loki/pull/9329) **JoaoBraveCoding**: Add default PodAntiAffinity to Ingester
- [9262](https://github.com/grafana/loki/pull/9262) **btaani**: Add PodDisruptionBudget to the Ruler
- [9260](https://github.com/grafana/loki/pull/9260) **JoaoBraveCoding**: Add PodDisruptionBudgets to the ingestion path
- [9188](https://github.com/grafana/loki/pull/9188) **aminesnow**: Add PodDisruptionBudgets to the query path
- [9162](https://github.com/grafana/loki/pull/9162) **aminesnow**: Add a PodDisruptionBudget to lokistack-gateway

## 0.3.0 (2023-04-20)

- [9049](https://github.com/grafana/loki/pull/9049) **alanconway**: Revert 1x.extra-small changes, add 1x.demo
- [8661](https://github.com/grafana/loki/pull/8661) **xuanyunhui**: Add a new Object Storage Type for AlibabaCloud OSS
- [9036](https://github.com/grafana/loki/pull/9036) **periklis**: Update Loki operand to v2.8.0
- [8978](https://github.com/grafana/loki/pull/8978) **aminesnow**: Add watch for the object storage secret
- [8958](https://github.com/grafana/loki/pull/8958) **periklis**: Align common instance addr with memberlist advertise addr
- [8998](https://github.com/grafana/loki/pull/8998) **periklis**: Remove static placeholder suffix for openshift bundle
- [8930](https://github.com/grafana/loki/pull/8930) **periklis**: Fix makefile target operatorhub
- [8911](https://github.com/grafana/loki/pull/8911) **aminesnow**: Update LokiStack annotaion on RulerConfig delete

## 0.2.0 (2023-03-27)

- [8912](https://github.com/grafana/loki/pull/8912) **periklis**: Add missing replaces directives for release v0.2.0
- [8651](https://github.com/grafana/loki/pull/8651) **periklis**: Prepare Community Loki Operator release v0.2.0
- [8881](https://github.com/grafana/loki/pull/8881) **periklis**: Provide community bundle for openshift community hub
- [8863](https://github.com/grafana/loki/pull/8863) **periklis**: Break the API types out into their own module
- [8878](https://github.com/grafana/loki/pull/8878) **periklis**: Refactor all type validations into own package
- [8875](https://github.com/grafana/loki/pull/8875) **Red-GV**: Remove mutations to non-updatable statefulset fields
- [7451](https://github.com/grafana/loki/pull/7451) **btaani**: Add support for rules configmap sharding
- [8672](https://github.com/grafana/loki/pull/8672) **periklis**: Add support for memberlist bind network configuration
- [8748](https://github.com/grafana/loki/pull/8748) **periklis**: Add alertingrule tenant id label for all rules
- [8743](https://github.com/grafana/loki/pull/8743) **periklis**: Add alerting style guide validation
- [8192](https://github.com/grafana/loki/pull/8192) **jotak**: Allow multiple matchers for multi-tenancy with Network tenant (OpenShift)
- [8800](https://github.com/grafana/loki/pull/8800) **aminesnow**: Promote AlertingRules, RecordingRules and RulerConfig from v1beta1 to v1
- [8792](https://github.com/grafana/loki/pull/8792) **orenc1**: Improve documentation for LokiStack installation with ODF Object Storage
- [8791](https://github.com/grafana/loki/pull/8791) **periklis**: Expand OLM skip range for OpenShift Logging 5.7 release (OpenShift)
- [8771](https://github.com/grafana/loki/pull/8771) **periklis**: Update Loki operand to v2.7.4
- [8776](https://github.com/grafana/loki/pull/8776) **Red-GV**: Update go to v1.20, k8s libraries to v1.26.2, and OpenShift libraries to v4.13
- [8707](https://github.com/grafana/loki/pull/8707) **aminesnow**: Fix gateway's nodeSelector and tolerations
- [8666](https://github.com/grafana/loki/pull/8666) **xperimental**: Fix version inconsistency in generated OpenShift bundle
- [8578](https://github.com/grafana/loki/pull/8578) **xperimental**: Refactor status update to reduce API calls
- [8577](https://github.com/grafana/loki/pull/8577) **Red-GV**: Store gateway tenant information in secret instead of configmap
- [8397](https://github.com/grafana/loki/pull/8397) **periklis**: Update Loki operand to v2.7.3
- [8308](https://github.com/grafana/loki/pull/8308) **aminesnow**: operator: Cleanup ruler resources when disabled
- [8336](https://github.com/grafana/loki/pull/8336) **periklis**: Update Loki operand to v2.7.2
- [8253](https://github.com/grafana/loki/pull/8253) **aminesnow**: Add watch on the Alertmanager in openshift-monitoring and decouple it from the user-workload AM
- [8265](https://github.com/grafana/loki/pull/8265) **Red-GV**: Use gRPC compactor service instead of http for retention
- [8038](https://github.com/grafana/loki/pull/8038) **aminesnow**: Add watch on the Alertmanager in OCP's user-workload-monitoring namespace
- [8173](https://github.com/grafana/loki/pull/8173) **periklis**: Remove custom webhook cert mounts for OLM-based deployment (OpenShift)
- [8001](https://github.com/grafana/loki/pull/8001) **aminesnow**: Add API validation to Alertmanager header auth config
- [8087](https://github.com/grafana/loki/pull/8087) **xperimental**: Fix status not updating when state of pods changes

## 0.1.0 (2023-01-10)

- [8068](https://github.com/grafana/loki/pull/8068) **periklis**: Use lokistack-gateway replicas from size table
- [8068](https://github.com/grafana/loki/pull/8068) **periklis**: Use lokistack-gateway replicas from size table
- [7839](https://github.com/grafana/loki/pull/7839) **aminesnow**: Configure Alertmanager per-tenant
- [7910](https://github.com/grafana/loki/pull/7910) **periklis**: Update Loki operand to v2.7.1
- [7815](https://github.com/grafana/loki/pull/7815) **periklis**: Apply delete client changes for compat with release-2.7.x
- [7809](https://github.com/grafana/loki/pull/7809) **xperimental**: Fix histogram-based alerting rules
- [7808](https://github.com/grafana/loki/pull/7808) **xperimental**: Replace fifocache usage by embedded_cache
- [7753](https://github.com/grafana/loki/pull/7753) **periklis**: Check for mandatory CA configmap name in ObjectStorageTLS spec
- [7744](https://github.com/grafana/loki/pull/7744) **periklis**: Fix object storage TLS spec CAKey descriptor
- [7716](https://github.com/grafana/loki/pull/7716) **aminesnow**: Migrate API docs generation tool
- [7710](https://github.com/grafana/loki/pull/7710) **periklis**: Fix LokiStackController watches for cluster-scoped resources
- [7682](https://github.com/grafana/loki/pull/7682) **periklis**: Refactor cluster proxy to use configv1.Proxy on OpenShift
- [7711](https://github.com/grafana/loki/pull/7711) **Red-GV**: Remove default value from replicationFactor field
- [7617](https://github.com/grafana/loki/pull/7617) **Red-GV**: Modify ingestionRate for respective shirt size
- [7592](https://github.com/grafana/loki/pull/7592) **aminesnow**: Update API docs generation using gen-crd-api-reference-docs
- [7448](https://github.com/grafana/loki/pull/7448) **periklis**: Add TLS support for compactor delete client
- [7596](https://github.com/grafana/loki/pull/7596) **periklis**: Fix fresh-installs with built-in cert management enabled
- [7064](https://github.com/grafana/loki/pull/7064) **periklis**: Add support for built-in cert management
- [7471](https://github.com/grafana/loki/pull/7471) **aminesnow**: Expose and migrate query_timeout in limits config
- [7437](https://github.com/grafana/loki/pull/7437) **aminesnow**: Fix Custom TLS profile setting for LokiStack on OpenShift
- [7415](https://github.com/grafana/loki/pull/7415) **aminesnow**: Add alert relabel config
- [7418](https://github.com/grafana/loki/pull/7418) **Red-GV**: Update golang to v1.19 and k8s dependencies to v0.25.2
- [7322](https://github.com/grafana/loki/pull/7322) **Red-GV**: Configuring server and client HTTP and GRPC TLS options
- [7272](https://github.com/grafana/loki/pull/7272) **aminesnow**: Use cluster monitoring alertmanager by default on openshift clusters
- [7295](https://github.com/grafana/loki/pull/7295) **xperimental**: Add extended-validation for rules on OpenShift
- [6951](https://github.com/grafana/loki/pull/6951) **Red-GV**: Adding operational Lokistack alerts
- [7254](https://github.com/grafana/loki/pull/7254) **periklis**: Expose Loki Ruler API via the lokistack-gateway
- [7214](https://github.com/grafana/loki/pull/7214) **periklis**: Fix ruler GRPC tls client configuration
- [7201](https://github.com/grafana/loki/pull/7201) **xperimental**: Write configuration for per-tenant retention
- [7037](https://github.com/grafana/loki/pull/7037) **xperimental**: Skip enforcing matcher for certain tenants on OpenShift
- [7106](https://github.com/grafana/loki/pull/7106) **xperimental**: Manage global stream-based retention
- [7092](https://github.com/grafana/loki/pull/7092) **aminesnow**: Configure kube-rbac-proxy sidecar to use Intermediate TLS security profile in OCP
- [6870](https://github.com/grafana/loki/pull/6870) **aminesnow**: Configure gateway to honor the global tlsSecurityProfile on Openshift
- [6999](https://github.com/grafana/loki/pull/6999) **Red-GV**: Adding LokiStack Gateway alerts
- [7000](https://github.com/grafana/loki/pull/7000) **xperimental**: Configure default node affinity for all pods
- [6923](https://github.com/grafana/loki/pull/6923) **xperimental**: Reconcile owner reference for existing objects
- [6907](https://github.com/grafana/loki/pull/6907) **Red-GV**: Adding valid subscription annotation to operator metadata
- [6479](https://github.com/grafana/loki/pull/6749) **periklis**: Update Loki operand to v2.6.1
- [6748](https://github.com/grafana/loki/pull/6748) **periklis**: Update go4.org/unsafe/assume-no-moving-gc to latest
- [6741](https://github.com/grafana/loki/pull/6741) **aminesnow**: Golang version to 1.18 and k8s client to 1.24
- [6669](https://github.com/grafana/loki/pull/6669) **xperimental**: Set minimum TLS version to 1.2 to support FIPS
- [6663](https://github.com/grafana/loki/pull/6663) **aminesnow**: Generalize live tail fix to all clusters using TLS
- [6443](https://github.com/grafana/loki/pull/6443) **aminesnow**: Fix live tail of logs not working on OpenShift-based clusters
- [6646](https://github.com/grafana/loki/pull/6646) **periklis**: Update Loki operand to v2.6.0
- [6594](https://github.com/grafana/loki/pull/6594) **xperimental**: Disable client certificate authentication on gateway
- [6551](https://github.com/grafana/loki/pull/6561) **periklis**: Add operator docs for object storage
- [6549](https://github.com/grafana/loki/pull/6549) **periklis**: Refactor feature gates to use custom resource definition
- [6514](https://github.com/grafana/loki/pull/6514) **Red-GV** Update all pods and containers to be compliant with restricted Pod Security Standard
- [6531](https://github.com/grafana/loki/pull/6531) **periklis**: Use default interface_names for lokistack clusters (IPv6 Support)
- [6411](https://github.com/grafana/loki/pull/6478) **aminesnow**: Support TLS enabled lokistack-gateway for vanilla kubernetes deployments
- [6504](https://github.com/grafana/loki/pull/6504) **periklis**: Disable usage report on OpenShift
- [6474](https://github.com/grafana/loki/pull/6474) **periklis**: Bump loki.grafana.com/LokiStack from v1beta to v1
- [6411](https://github.com/grafana/loki/pull/6411) **Red-GV**: Extend schema validation in LokiStack webhook
- [6334](https://github.com/grafana/loki/pull/6433) **periklis**: Move operator cli flags to component config
- [6224](https://github.com/grafana/loki/pull/6224) **periklis**: Add support for GRPC over TLS for Loki components
- [5952](https://github.com/grafana/loki/pull/5952) **Red-GV**: Add api to change storage schema version
- [6363](https://github.com/grafana/loki/pull/6363) **periklis**: Allow optional installation of webhooks (Kind)
- [6362](https://github.com/grafana/loki/pull/6362) **periklis**: Allow reduced tenant OIDC authentication requirements
- [6288](https://github.com/grafana/loki/pull/6288) **aminesnow**: Expose only an HTTPS gateway when in openshift mode
- [6195](https://github.com/grafana/loki/pull/6195) **periklis**: Add ruler config support
- [6198](https://github.com/grafana/loki/pull/6198) **periklis**: Add support for custom S3 CA
- [6199](https://github.com/grafana/loki/pull/6199) **Red-GV**: Update GCP secret volume path
- [6125](https://github.com/grafana/loki/pull/6125) **sasagarw**: Add method to get authenticated from GCP
- [5986](https://github.com/grafana/loki/pull/5986) **periklis**: Add support for Loki Rules reconciliation
- [5987](https://github.com/grafana/loki/pull/5987) **Red-GV**: Update logerr to v2.0.0
- [5907](https://github.com/grafana/loki/pull/5907) **xperimental**: Do not include non-static labels in pod selectors
- [5893](https://github.com/grafana/loki/pull/5893) **periklis**: Align PVC storage size requests for all lokistack t-shirt sizes
- [5884](https://github.com/grafana/loki/pull/5884) **periklis**: Update Loki operand to v2.5.0
- [5748](https://github.com/grafana/loki/pull/5748) **Red-GV**: Update Prometheus go client to 12.1
- [5739](https://github.com/grafana/loki/pull/5739) **sasagarw**: Change UUIDs to tenant name in doc
- [5729](https://github.com/grafana/loki/pull/5729) **periklis**: Add missing label matcher for openshift logging tenant mode (OpenShift)
- [5691](https://github.com/grafana/loki/pull/5691) **sasagarw**: Fix immediate reset of degraded condition
- [5704](https://github.com/grafana/loki/pull/5704) **xperimental**: Update operator-sdk to 1.18.1
- [5693](https://github.com/grafana/loki/pull/5693) **periklis**: Replace frontend_worker parallelism with match_max_concurrent
- [5699](https://github.com/grafana/loki/pull/5699) **Red-GV**: Configure boltdb_shipper and schema to use Azure, GCS, and Swift storage
- [5701](https://github.com/grafana/loki/pull/5701) **sasagarw**: Make ReplicationFactor optional in LokiStack API
- [5695](https://github.com/grafana/loki/pull/5695) **xperimental**: Update Go to 1.17
- [5615](https://github.com/grafana/loki/pull/5615) **sasagarw**: Document how to connect to LokiStack gateway component
- [5655](https://github.com/grafana/loki/pull/5655) **xperimental**: Update Loki operand to 2.4.2
- [5579](https://github.com/grafana/loki/pull/5579) **Red-GV**: Add playbook for responding to operator alerts
- [5640](https://github.com/grafana/loki/pull/5640) **sasagarw**: Update CSV to point to candidate channel and use openshift-operators-redhat ns (OpenShift)
- [5551](https://github.com/grafana/loki/pull/5551) **sasagarw**: Document how to connect to distributor component
- [5624](https://github.com/grafana/loki/pull/5624) **periklis**: Use tenant name as id for mode openshift-logging (OpenShift)
- [5621](https://github.com/grafana/loki/pull/5621) **periklis**: Use recommended labels for LokiStack components
- [5607](https://github.com/grafana/loki/pull/5607) **periklis**: Use lokistack name as prefix for owned resources
- [5588](https://github.com/grafana/loki/pull/5588) **periklis**: Add RBAC for Prometheus service discovery to Loki component metrics (OpenShift)
- [5576](https://github.com/grafana/loki/pull/5576) **xperimental**: Change endpoints for generated liveness and readiness probes
- [5560](https://github.com/grafana/loki/pull/5560) **periklis**: Fix service monitor's server name for operator metrics
- [5345](https://github.com/grafana/loki/pull/5345) **ronensc**: Add flag to create Prometheus rules
- [5432](https://github.com/grafana/loki/pull/5432) **Red-GV**: Provide storage configuration for Azure, GCS, and Swift through common_config
- [4975](https://github.com/grafana/loki/pull/4975) **periklis**: Provide saner default for loki-operator managed chunk_target_size
