# Change Log

## [v1.75.0] - 2022-01-27

- #508 - @ElanHasson - Synchronize public protos and add multiple specs

## [v1.74.0] - 2022-01-20

- #506 - @ZachEddy - Add new component type to apps-related structs

## [v1.73.0] - 2021-12-03

- #501 - @CollinShoop - Add support for Registry ListManifests and ListRepositoriesV2

## [v1.72.0] - 2021-11-29

- #500 - @ElanHasson - APPS-4420: Add PreservePathPrefix to AppRouteSpec

## [v1.71.0] - 2021-11-09

- #498 - @bojand - apps: update spec to include log destinations

## [v1.70.0] - 2021-11-01

- #491 - @andrewsomething - Add support for retrieving Droplet monitoring metrics.
- #494 - @alexandear - Refactor tests: replace t.Errorf with assert/require
- #495 - @alexandear - Fix typos and grammar issues in comments
- #492 - @andrewsomething - Update golang.org/x/net
- #486 - @abeltay - Fix typo on "DigitalOcean"

## [v1.69.1] - 2021-10-06

- #484 - @sunny-b - k8s/godo: remove ha field from update request

## [v1.69.0] - 2021-10-04

- #482 - @dikshant - godo/load-balancers: add DisableLetsEncryptDNSRecords field for LBaaS

## [v1.68.0] - 2021-09-29

- #480 - @sunny-b - kubernetes: add support for HA control plane

## [v1.67.0] - 2021-09-22

- #478 - @sunny-b - kubernetes: add supported_features field to the kubernetes/options response
- #477 - @wez470 - Add size unit to LB API.

## [v1.66.0] - 2021-09-21

- #473 - @andrewsomething - Add Go 1.17.x to test matrix and drop unsupported versions.
- #472 - @bsnyder788 - insights: add private (in/out)bound and public inbound bandwidth aler…
- #470 - @gottwald - domains: remove invalid json struct tag option

## [v1.65.0] - 2021-08-05

- #468 - @notxarb - New alerts feature for App Platform
- #467 - @andrewsomething - docs: Update links to API documentation.
- #466 - @andrewsomething - Mark Response.Monitor as deprecated.

## [v1.64.2] - 2021-07-23

- #464 - @bsnyder788 - insights: update HTTP method for alert policy update

## [v1.64.1] - 2021-07-19

- #462 - @bsnyder788 - insights: fix alert policy update endpoint

## [v1.64.0] - 2021-07-19

- #460 - @bsnyder788 - insights: add CRUD APIs for alert policies

## [v1.63.0] - 2021-07-06

- #458 - @ZachEddy - apps: Add tail_lines query parameter to GetLogs function

## [v1.62.0] - 2021-06-07

- #454 - @house-lee - add with_droplet_agent option to create requests

## [v1.61.0] - 2021-05-12

- #452 - @caiofilipini - Add support for DOKS clusters as peers in Firewall rules
- #448 - @andrewsomething - flip: Set omitempty for Region in FloatingIPCreateRequest.
- #451 - @andrewsomething - CheckResponse: Add RequestID from header to ErrorResponse when missing from body.
- #450 - @nanzhong - dbaas: handle ca certificates as base64 encoded
- #449 - @nanzhong - dbaas: add support for getting cluster CA
- #446 - @kamaln7 - app spec: update cors policy

## [v1.60.0] - 2021-04-04

- #443 - @andrewsomething - apps: Support pagination.
- #442 - @andrewsomething - dbaas: Support restoring from a backup.
- #441 - @andrewsomething - k8s: Add URN method to KubernetesCluster.

## [v1.59.0] - 2021-03-29

- #439 - @andrewsomething - vpcs: Support listing members of a VPC.
- #438 - @andrewsomething - Add Go 1.16.x to the testing matrix.

## [v1.58.0] - 2021-02-17

- #436 - @MorrisLaw - kubernetes: add name field to associated resources
- #434 - @andrewsomething - sizes: Add description field.
- #433 - @andrewsomething - Deprecate Name field in godo.DropletCreateVolume

## [v1.57.0] - 2021-01-15

- #429 - @varshavaradarajan - kubernetes: support optional cascading deletes for clusters
- #430 - @jonfriesen - apps: updates apps.gen.go for gitlab addition
- #431 - @nicktate - apps: update proto to support dockerhub registry type

## [v1.56.0] - 2021-01-08

- #422 - @kamaln7 - apps: add ProposeApp method

## [v1.55.0] - 2021-01-07

- #425 - @adamwg - registry: Support the storage usage indicator
- #423 - @ChiefMateStarbuck - Updated README example
- #421 - @andrewsomething - Add some basic input cleaning to NewFromToken
- #420 - @bentranter - Don't set "Content-Type" header on GET requests

## [v1.54.0] - 2020-11-24

- #417 - @waynr - registry: add support for garbage collection types

## [v1.53.0] - 2020-11-20

- #414 - @varshavaradarajan - kubernetes: add clusterlint support
- #413 - @andrewsomething - images: Support updating distribution and description.

## [v1.52.0] - 2020-11-05

- #411 - @nicktate - apps: add unspecified type to image source registry types
- #409 - @andrewsomething - registry: Add support for updating a subscription.
- #408 - @nicktate - apps: update spec to include image source
- #407 - @kamaln7 - apps: add the option to force build a new deployment

## [v1.51.0] - 2020-11-02

- #405 - @adamwg - registry: Support subscription options
- #398 - @reeseconor - Add support for caching dependencies between GitHub Action runs
- #404 - @andrewsomething - CONTRIBUTING.md: Suggest using github-changelog-generator.

## [v1.50.0] - 2020-10-26

- #400 - @waynr - registry: add garbage collection support
- #402 - @snormore - apps: add catchall_document static site spec field and failed-deploy job type
- #401 - @andrewlouis93 - VPC: adds option to set a VPC as the regional default

## [v1.49.0] - 2020-10-21

- #383 - @kamaln7 - apps: add ListRegions, Get/ListTiers, Get/ListInstanceSizes
- #390 - @snormore - apps: add service spec internal_ports

## [v1.48.0] - 2020-10-16

- #388 - @varshavaradarajan - kubernetes - change docr integration api routes
- #386 - @snormore - apps: pull in recent updates to jobs and domains

## [v1.47.0] - 2020-10-14

- #384 kubernetes - add registry related doks apis - @varshavaradarajan
- #385 Fixed some typo in apps.gen.go and databases.go file - @devil-cyber
- #382 Add GetKubeConfigWithExpiry (#334) - @ivanlemeshev
- #381 Fix golint issues #377 - @sidsbrmnn
- #380 refactor: Cyclomatic complexity issue - @DonRenando
- #379 Run gofmt to fix some issues in codebase - @mycodeself

## [v1.46.0] - 2020-10-05

- #373 load balancers: add LB size field, currently in closed beta - @anitgandhi

## [v1.45.0] - 2020-09-25

**Note**: This release contains breaking changes to App Platform features currently in closed beta.

- #369 update apps types to latest - @kamaln7
- #368 Kubernetes: add taints field to node pool create and update requests - @timoreimann
- #367 update apps types, address marshaling bug - @kamaln7

## [v1.44.0] - 2020-09-08

- #364 apps: support aggregate deployment logs - @kamaln7

## [v1.43.0] - 2020-09-08

- #362 update apps types - @kamaln7

## [v1.42.1] - 2020-08-06

- #360 domains: Allow for SRV records with port 0. - @andrewsomething

## [v1.42.0] - 2020-07-22

- #357 invoices: add category to InvoiceItem - @rbutler
- #358 apps: add support for following logs - @nanzhong

## [v1.41.0] - 2020-07-17

- #355 kubernetes: Add support for surge upgrades - @varshavaradarajan

## [v1.40.0] - 2020-07-16

- #347 Make Rate limits thread safe - @roidelapluie
- #353 Reuse TCP connection - @itsksaurabh

## [v1.39.0] - 2020-07-14

- #345, #346 Add app platform support [beta] - @nanzhong

## [v1.38.0] - 2020-06-18

- #341 Install 1-click applications on a Kubernetes cluster - @keladhruv
- #340 Add RecordsByType, RecordsByName and RecordsByTypeAndName to the DomainsService - @viola

## [v1.37.0] - 2020-06-01

- #336 registry: URL encode repository names when building URLs. @adamwg
- #335 Add 1-click service and request. @scottcrawford03

## [v1.36.0] - 2020-05-12

- #331 Expose expiry_seconds for Registry.DockerCredentials. @andrewsomething

## [v1.35.1] - 2020-04-21

- #328 Update vulnerable x/crypto dependency - @bentranter

## [v1.35.0] - 2020-04-20

- #326 Add TagCount field to registry/Repository - @nicktate
- #325 Add DOCR EA routes - @nicktate
- #324 Upgrade godo to Go 1.14 - @bentranter

## [v1.34.0] - 2020-03-30

- #320 Add VPC v3 attributes - @viola

## [v1.33.1] - 2020-03-23

- #318 upgrade github.com/stretchr/objx past 0.1.1 - @hilary

## [v1.33.0] - 2020-03-20

- #310 Add BillingHistory service and List endpoint - @rbutler
- #316 load balancers: add new enable_backend_keepalive field - @anitgandhi

## [v1.32.0] - 2020-03-04

- #311 Add reset database user auth method - @zbarahal-do

## [v1.31.0] - 2020-02-28

- #305 invoices: GetPDF and GetCSV methods - @rbutler
- #304 Add NewFromToken convenience method to init client - @bentranter
- #301 invoices: Get, Summary, and List methods - @rbutler
- #299 Fix param expiry_seconds for kubernetes.GetCredentials request - @velp

## [v1.30.0] - 2020-02-03

- #295 registry: support the created_at field - @adamwg
- #293 doks: node pool labels - @snormore

## [v1.29.0] - 2019-12-13

- #288 Add Balance Get method - @rbutler
- #286,#289 Deserialize meta field - @timoreimann

## [v1.28.0] - 2019-12-04

- #282 Add valid Redis eviction policy constants - @bentranter
- #281 Remove databases info from top-level godoc string - @bentranter
- #280 Fix VolumeSnapshotResourceType value volumesnapshot -> volume_snapshot - @aqche

## [v1.27.0] - 2019-11-18

- #278 add mysql user auth settings for database users - @gregmankes

## [v1.26.0] - 2019-11-13

- #272 dbaas: get and set mysql sql mode - @mikejholly

## [v1.25.0] - 2019-11-13

- #275 registry/docker-credentials: add support for the read/write parameter - @kamaln7
- #273 implement the registry/docker-credentials endpoint - @kamaln7
- #271 Add registry resource - @snormore

## [v1.24.1] - 2019-11-04

- #264 Update isLast to check p.Next - @aqche

## [v1.24.0] - 2019-10-30

- #267 Return []DatabaseFirewallRule in addition to raw response. - @andrewsomething

## [v1.23.1] - 2019-10-30

- #265 add support for getting/setting firewall rules - @gregmankes
- #262 remove ResolveReference call - @mdanzinger
- #261 Update CONTRIBUTING.md - @mdanzinger

## [v1.22.0] - 2019-09-24

- #259 Add Kubernetes GetCredentials method - @snormore

## [v1.21.1] - 2019-09-19

- #257 Upgrade to Go 1.13 - @bentranter

## [v1.21.0] - 2019-09-16

- #255 Add DropletID to Kubernetes Node instance - @snormore
- #254 Add tags to Database, DatabaseReplica - @Zyqsempai

## [v1.20.0] - 2019-09-06

- #252 Add Kubernetes autoscale config fields - @snormore
- #251 Support unset fields on Kubernetes cluster and node pool updates - @snormore
- #250 Add Kubernetes GetUser method - @snormore

## [v1.19.0] - 2019-07-19

- #244 dbaas: add private-network-uuid field to create request

## [v1.18.0] - 2019-07-17

- #241 Databases: support for custom VPC UUID on migrate @mikejholly
- #240 Add the ability to get URN for a Database @stack72
- #236 Fix omitempty typos in JSON struct tags @amccarthy1

## [v1.17.0] - 2019-06-21

- #238 Add support for Redis eviction policy in Databases @mikejholly

## [v1.16.0] - 2019-06-04

- #233 Add Kubernetes DeleteNode method, deprecate RecycleNodePoolNodes @bouk

## [v1.15.0] - 2019-05-13

- #231 Add private connection fields to Databases - @mikejholly
- #223 Introduce Go modules - @andreiavrammsd

## [v1.14.0] - 2019-05-13

- #229 Add support for upgrading Kubernetes clusters - @adamwg

## [v1.13.0] - 2019-04-19

- #213 Add tagging support for volume snapshots - @jcodybaker

## [v1.12.0] - 2019-04-18

- #224 Add maintenance window support for Kubernetes- @fatih

## [v1.11.1] - 2019-04-04

- #222 Fix Create Database Pools json fields - @sunny-b

## [v1.11.0] - 2019-04-03

- #220 roll out vpc functionality - @jheimann

## [v1.10.1] - 2019-03-27

- #219 Fix Database Pools json field - @sunny-b

## [v1.10.0] - 2019-03-20

- #215 Add support for Databases - @mikejholly

## [v1.9.0] - 2019-03-18

- #214 add support for enable_proxy_protocol. - @mregmi

## [v1.8.0] - 2019-03-13

- #210 Expose tags on storage volume create/list/get. - @jcodybaker

## [v1.7.5] - 2019-03-04

- #207 Add support for custom subdomains for Spaces CDN [beta] - @xornivore

## [v1.7.4] - 2019-02-08

- #202 Allow tagging volumes - @mchitten

## [v1.7.3] - 2018-12-18

- #196 Expose tag support for creating Load Balancers.

## [v1.7.2] - 2018-12-04

- #192 Exposes more options for Kubernetes clusters.

## [v1.7.1] - 2018-11-27

- #190 Expose constants for the state of Kubernetes clusters.

## [v1.7.0] - 2018-11-13

- #188 Kubernetes support [beta] - @aybabtme

## [v1.6.0] - 2018-10-16

- #185 Projects support [beta] - @mchitten

## [v1.5.0] - 2018-10-01

- #181 Adding tagging images support - @hugocorbucci

## [v1.4.2] - 2018-08-30

- #178 Allowing creating domain records with weight of 0 - @TFaga
- #177 Adding `VolumeLimit` to account - @lxfontes

## [v1.4.1] - 2018-08-23

- #176 Fix cdn flush cache API endpoint - @sunny-b

## [v1.4.0] - 2018-08-22

- #175 Add support for Spaces CDN - @sunny-b

## [v1.3.0] - 2018-05-24

- #170 Add support for volume formatting - @adamwg

## [v1.2.0] - 2018-05-08

- #166 Remove support for Go 1.6 - @iheanyi
- #165 Add support for Let's Encrypt Certificates - @viola

## [v1.1.3] - 2018-03-07

- #156 Handle non-json errors from the API - @aknuds1
- #158 Update droplet example to use latest instance type - @dan-v

## [v1.1.2] - 2018-03-06

- #157 storage: list volumes should handle only name or only region params - @andrewsykim
- #154 docs: replace first example with fully-runnable example - @xmudrii
- #152 Handle flags & tag properties of domain record - @jaymecd

## [v1.1.1] - 2017-09-29

- #151 Following user agent field recommendations - @joonas
- #148 AsRequest method to create load balancers requests - @lukegb

## [v1.1.0] - 2017-06-06

### Added
- #145 Add FirewallsService for managing Firewalls with the DigitalOcean API. - @viola
- #139 Add TTL field to the Domains. - @xmudrii

### Fixed
- #143 Fix oauth2.NoContext depreciation. - @jbowens
- #141 Fix DropletActions on tagged resources. - @xmudrii

## [v1.0.0] - 2017-03-10

### Added
- #130 Add Convert to ImageActionsService. - @xmudrii
- #126 Add CertificatesService for managing certificates with the DigitalOcean API. - @viola
- #125 Add LoadBalancersService for managing load balancers with the DigitalOcean API. - @viola
- #122 Add GetVolumeByName to StorageService. - @protochron
- #113 Add context.Context to all calls. - @aybabtme
