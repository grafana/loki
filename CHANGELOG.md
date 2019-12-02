# 1.0.0 (2019-11-19)

:tada: Nearly a year since Loki was announced at KubeCon in Seattle 2018 we are very excited to announce the 1.0.0 release of Loki! :tada:

A lot has happened since the announcement, the project just recently passed 1000 commits by 138 contributors over 700+ PR's accumulating over 7700 GitHub stars!

Internally at Grafana Labs we have been using Loki to monitor all of our infrastructure and ingest around 1.5TB/10 billion log lines a day. Since the v0.2.0 release we have found Loki to be reliable and stable in our environments.

We are comfortable with the state of the project in our production environments and think it's time to promote Loki to a non-beta release to communicate to everyone that they should feel comfortable using Loki in their production environments too.

## API Stability

With the 1.0.0 release our intent is to try to follow Semver rules regarding stability with some aspects of Loki, focusing mainly on the operating experience of Loki as an application.  That is to say we are not planning any major changes to the HTTP API, and anything breaking would likely be accompanied by a major release with backwards compatibility support.

We are currently NOT planning on maintaining Go API stability with this release, if you are importing Loki as a library you should be prepared for any kind of change, including breaking, even in minor or bugfix releases.

Loki is still a young and active project and there might be some breaking config changes in non-major releases, rest assured this will be clearly communicated and backwards or overlapping compatibility will be provided if possible.

## Changes

There were not as many changes in this release as the last, mainly we wanted to make sure Loki was mostly stable before 1.0.0.  The most notable change is the inclusion of the V11 schema in PR's [1201](https://github.com/grafana/loki/pull/1201) and [1280](https://github.com/grafana/loki/pull/1280).  The V11 schema adds some more data to the index to improve label queries over large amounts of time and series.  Currently we have not updated the Helm or Ksonnet to use the new schema, this will come soon with more details on how it works.

The full list of changes:

* [1280](https://github.com/grafana/loki/pull/1280) **owen-d**: Fix duplicate labels (update cortex)
* [1260](https://github.com/grafana/loki/pull/1260) **rfratto**: pkg/loki: unmarshal module name from YAML
* [1257](https://github.com/grafana/loki/pull/1257) **rfratto**: helm: update default terminationGracePeriodSeconds to 4800
* [1251](https://github.com/grafana/loki/pull/1251) **obitech**: docs: Fix promtail releases download link
* [1248](https://github.com/grafana/loki/pull/1248) **rfratto**: docs: slightly modify language in community Loki packages section
* [1242](https://github.com/grafana/loki/pull/1242) **tarokkk**: fluentd: Suppress unread configuration warning
* [1239](https://github.com/grafana/loki/pull/1239) **pracucci**: Move ReservedLabelTenantID out from a dedicated file
* [1238](https://github.com/grafana/loki/pull/1238) **oke-py**: helm: loki-stack supports k8s 1.16
* [1237](https://github.com/grafana/loki/pull/1237) **joe-elliott**: Rollback google.golang.org/api to 0.8.0
* [1235](https://github.com/grafana/loki/pull/1235) **woodsaj**: ci: update triggers to use new deployment_tools location
* [1234](https://github.com/grafana/loki/pull/1234) **rfratto**: Standardize schema used in `match` stage
* [1233](https://github.com/grafana/loki/pull/1233) **wapmorgan**: Update docker-driver Dockerfile: add tzdb
* [1232](https://github.com/grafana/loki/pull/1232) **rfratto**: Fix drone deploy job
* [1231](https://github.com/grafana/loki/pull/1231) **joe-elliott**: Removed references to Loki free tier
* [1226](https://github.com/grafana/loki/pull/1226) **clickyotomy**: Update dependencies to use weaveworks/common upstream
* [1221](https://github.com/grafana/loki/pull/1221) **slim-bean**: use regex label matcher to not alert on any tail route latencies
* [1219](https://github.com/grafana/loki/pull/1219) **MightySCollins**: docs: Updated Kubernetes docs links in Helm charts
* [1218](https://github.com/grafana/loki/pull/1218) **slim-bean**: update dashboards to include the new /loki/api/v1/* endpoints
* [1217](https://github.com/grafana/loki/pull/1217) **slim-bean**: sum the bad words by name and level
* [1216](https://github.com/grafana/loki/pull/1216) **joe-elliott**: Remove rules that reference no longer existing metrics
* [1215](https://github.com/grafana/loki/pull/1215) **Eraac**: typo url
* [1214](https://github.com/grafana/loki/pull/1214) **takanabe**: Correct wrong document paths about querying
* [1213](https://github.com/grafana/loki/pull/1213) **slim-bean**: Fix docker latest and master tags
* [1212](https://github.com/grafana/loki/pull/1212) **joe-elliott**: Update loki operational
* [1206](https://github.com/grafana/loki/pull/1206) **sandlis**: ksonnet: fix replication always set to 3 in ksonnet
* [1203](https://github.com/grafana/loki/pull/1203) **joe-elliott**: Chunk iterator performance improvement
* [1202](https://github.com/grafana/loki/pull/1202) **beorn7**: Simplify regexp's
* [1201](https://github.com/grafana/loki/pull/1201) **cyriltovena**: Update cortex to bring v11 schema
* [1189](https://github.com/grafana/loki/pull/1189) **putrasattvika**: fluent-plugin: Add client certificate verification
* [1186](https://github.com/grafana/loki/pull/1186) **tarokkk**: fluentd: Refactor label_keys and and add extract_kubernetes_labels configuration

# 0.4.0 (2019-10-24)

A **huge** thanks to the **36 contributors** who submitted **148 PR's** since 0.3.0!

## Notable Changes

* With PR [654](https://github.com/grafana/loki/pull/654) @cyriltovena added a really exciting new capability to Loki, a Prometheus compatible API with support for running metric style queries against your logs! [Take a look at how to write metric queries for logs](https://github.com/grafana/loki/blob/master/docs/logql.md#counting-logs)
    > PLEASE NOTE: To use metric style queries in the current Grafana release 6.4.x you will need to add Loki as a Prometheus datasource in addition to having it as a Log datasource and you will have to select the correct source for querying logs vs metrics, coming soon Grafana will support both logs and metric queries directly to the Loki datasource!
* PR [1022](https://github.com/grafana/loki/pull/1022) (and a few others) @joe-elliott added a new set of HTTP endpoints in conjunction with the work @cyriltovena to create a Prometheus compatible API as well as improve how labels/timestamps are handled
    > IMPORTANT: The new `/api/v1/*` endpoints contain breaking changes on the query paths (push path is unchanged) Eventually the `/api/prom/*` endpoints will be removed
* PR [847](https://github.com/grafana/loki/pull/847) owes a big thanks to @cosmo0920 for contributing his Fluent Bit go plugin, now loki has Fluent Bit plugin support!!

* PR [982](https://github.com/grafana/loki/pull/982) was a couple weeks of painstaking work by @rfratto for a much needed improvement to Loki's docs! [Check them out!](https://github.com/grafana/loki/tree/master/docs) 

* PR [980](https://github.com/grafana/loki/pull/980) by @sh0rez improved how flags and config file's are loaded to honor a more traditional order of precedence:
    1. Defaults
    2. Config file
    3. User-supplied flag values (command line arguments)
    > PLEASE NOTE: This is potentially a breaking change if you were passing command line arguments that also existed in a config file in which case the order they are given priority now has changed!
  
* PR [1062](https://github.com/grafana/loki/pull/1062) and [1089](https://github.com/grafana/loki/pull/1089) have moved Loki from Dep to Go Modules and to Go 1.13


## Loki

### Features/Improvements/Changes

* **Loki** [1171](https://github.com/grafana/loki/pull/1171) **cyriltovena**: Moves request parsing into the loghttp package
* **Loki** [1145](https://github.com/grafana/loki/pull/1145) **joe-elliott**: Update `/loki/api/v1/push` to use the v1 json format
* **Loki** [1128](https://github.com/grafana/loki/pull/1128) **sandlis**: bigtable-backup: list backups just before starting deletion of wanted backups
* **Loki** [1100](https://github.com/grafana/loki/pull/1100) **sandlis**: logging: removed some noise in logs from live-tailing
* **Loki/build** [1089](https://github.com/grafana/loki/pull/1089) **joe-elliott**: Go 1.13
* **Loki** [1088](https://github.com/grafana/loki/pull/1088) **pstibrany**: Updated cortex to latest master.
* **Loki** [1085](https://github.com/grafana/loki/pull/1085) **pracucci**: Do not retry chunks transferring on shutdown in the local dev env
* **Loki** [1084](https://github.com/grafana/loki/pull/1084) **pracucci**: Skip ingester tailer filtering if no filter is set
* **Loki/build**[1062](https://github.com/grafana/loki/pull/1062) **joe-elliott**: dep => go mod
* **Loki** [1049](https://github.com/grafana/loki/pull/1049) **joe-elliott**: Update loki push path
* **Loki** [1044](https://github.com/grafana/loki/pull/1044) **joe-elliott**: Fixed broken logql request filtering
* **Loki/tools** [1043](https://github.com/grafana/loki/pull/1043) **sandlis**: bigtable-backup: use latest bigtable backup docker image with fix for list backups
* **Loki** [1030](https://github.com/grafana/loki/pull/1030) **polar3130**: fix typo in error messages
* **Loki/tools** [1028](https://github.com/grafana/loki/pull/1028) **sandlis**: bigtable-backup: verify backups to work on latest list of backups
* **Loki** [1022](https://github.com/grafana/loki/pull/1022) **joe-elliott**: Loki HTTP/JSON Model Layer
* **Loki** [1016](https://github.com/grafana/loki/pull/1016) **slim-bean**: Revert "Updated stream json objects to be more parse friendly (#1010)"
* **Loki** [1010](https://github.com/grafana/loki/pull/1010) **joe-elliott**: Updated stream json objects to be more parse friendly
* **Loki** [1009](https://github.com/grafana/loki/pull/1009) **cyriltovena**: Make Loki HTTP API more compatible with Prometheus
* **Loki** [1008](https://github.com/grafana/loki/pull/1008) **wardbekker**: Improved Ingester out-of-order error for faster troubleshooting
* **Loki** [1001](https://github.com/grafana/loki/pull/1001) **slim-bean**: Update new API paths
* **Loki** [998](https://github.com/grafana/loki/pull/998) **sandlis**: Change unit of duration params to hours to align it with duration config at other places in Loki
* **Loki** [980](https://github.com/grafana/loki/pull/980) **sh0rez**: feat: configuration source precedence
* **Loki** [948](https://github.com/grafana/loki/pull/948) **sandlis**: limits: limits implementation for loki
* **Loki** [947](https://github.com/grafana/loki/pull/947) **sandlis**: added a variable for storing periodic table duration as an int to be …
* **Loki** [938](https://github.com/grafana/loki/pull/938) **sandlis**: vendoring: update cortex to latest master
* **Loki/tools** [930](https://github.com/grafana/loki/pull/930) **sandlis**: fix incrementing of bigtable_backup_job_backups_created metric
* **Loki/tools** [920](https://github.com/grafana/loki/pull/920) **sandlis**: bigtable-backup tool fix
* **Loki/tools** [895](https://github.com/grafana/loki/pull/895) **sandlis**: bigtable-backup-tool: Improvements
* **Loki** [755](https://github.com/grafana/loki/pull/755) **sandlis**: Use grpc client config from cortex for Ingester to get more control
* **Loki** [654](https://github.com/grafana/loki/pull/654) **cyriltovena**: LogQL: Vector and Range Vector Aggregation.

### Bug Fixes
* **Loki** [1114](https://github.com/grafana/loki/pull/1114) **rfratto**: pkg/ingester: prevent shutdowns from processing during joining handoff
* **Loki** [1097](https://github.com/grafana/loki/pull/1097) **joe-elliott**: Reverted cloud.google.com/go to 0.44.1
* **Loki** [986](https://github.com/grafana/loki/pull/986) **pracucci**: Fix panic in tailer due to race condition between send() and close()
* **Loki** [975](https://github.com/grafana/loki/pull/975) **sh0rez**: fix(distributor): parseError BadRequest
* **Loki** [944](https://github.com/grafana/loki/pull/944) **rfratto**: pkg/querier: fix concurrent access to querier tail clients

## Promtail

### Features/Improvements/Changes

* **Promtail/pipeline** [1179](https://github.com/grafana/loki/pull/1179) **pracucci**: promtail: fix handling of JMESPath expression returning nil while parsing JSON
* **Promtail/pipeline** [1123](https://github.com/grafana/loki/pull/1123) **pracucci**: promtail: added action_on_failure support to timestamp stage
* **Promtail/pipeline** [1122](https://github.com/grafana/loki/pull/1122) **pracucci**: promtail: initialize extracted map with initial labels
* **Promtail/pipeline** [1112](https://github.com/grafana/loki/pull/1112) **cyriltovena**: Add logql filter to match stages and drop capability
* **Promtail/journal** [1109](https://github.com/grafana/loki/pull/1109) **rfratto**: Clarify journal warning
* **Promtail** [1083](https://github.com/grafana/loki/pull/1083) **pracucci**: Increased promtail's backoff settings in prod and improved doc
* **Promtail** [1026](https://github.com/grafana/loki/pull/1026) **erwinvaneyk**: promtail: fix externalURL and path prefix issues
* **Promtail** [976](https://github.com/grafana/loki/pull/976) **slim-bean**: Wrap debug log statements in conditionals to save allocations
* **Promtail** [973](https://github.com/grafana/loki/pull/973) **ctrox**: tests: Set default value for BatchWait as ticker does not accept 0
* **Promtail** [969](https://github.com/grafana/loki/pull/969) **ctrox**: promtail: Use ticker instead of timer for batch wait
* **Promtail** [952](https://github.com/grafana/loki/pull/952) **pracucci**: promtail: add metrics on sent and dropped log entries
* **Promtail** [934](https://github.com/grafana/loki/pull/934) **pracucci**: promtail: do not send the last batch - to ingester - if empty
* **Promtail** [921](https://github.com/grafana/loki/pull/921) **rfratto**: promtail: add "max_age" field to configure cutoff for journal reading
* **Promtail** [883](https://github.com/grafana/loki/pull/883) **adityacs**: Add pipeline unit testing to promtail

### Bugfixes

* **Promtail** [1194](https://github.com/grafana/loki/pull/1194) **slim-bean**: Improve how we record file size metric to avoid a race in our file lagging alert
* **Promtail/journal** [1072](https://github.com/grafana/loki/pull/1072) **rfratto**: build: enable journal in promtail linux release build

## Docs

* **Docs** [1176](https://github.com/grafana/loki/pull/1176) **rfratto**: docs: add exmaple and documentation about using JMESPath literals
* **Docs** [1139](https://github.com/grafana/loki/pull/1139) **joe-elliott**: Moved client docs and add serilog example
* **Docs** [1132](https://github.com/grafana/loki/pull/1132) **kailwallin**: FixedTypo.Update README.md
* **Docs** [1130](https://github.com/grafana/loki/pull/1130) **pracucci**: docs: fix Promtail / Loki capitalization
* **Docs** [1129](https://github.com/grafana/loki/pull/1129) **pracucci**: docs: clarified the relation between retention period and table period
* **Docs** [1124](https://github.com/grafana/loki/pull/1124) **geowa4**: Client recommendations documentation tweaks
* **Docs** [1106](https://github.com/grafana/loki/pull/1106) **cyriltovena**: Add fluent-bit missing link in the main documentation page.
* **Docs** [1099](https://github.com/grafana/loki/pull/1099) **pracucci**: docs: improve table manager documentation
* **Docs** [1094](https://github.com/grafana/loki/pull/1094) **rfratto**: docs: update stages README with the docker and cri stages
* **Docs** [1091](https://github.com/grafana/loki/pull/1091) **daixiang0**: docs(stage): add docker and cri
* **Docs** [1077](https://github.com/grafana/loki/pull/1077) **daixiang0**: doc(fluent-bit): add missing namespace
* **Docs** [1073](https://github.com/grafana/loki/pull/1073) **flouthoc**: Re Fix Docs: PR https://github.com/grafana/loki/pull/1053 got erased due to force push.
* **Docs** [1069](https://github.com/grafana/loki/pull/1069) **daixiang0**: doc: unify GOPATH
* **Docs** [1068](https://github.com/grafana/loki/pull/1068) **daixiang0**: doc: skip jb init when using Tanka
* **Docs** [1067](https://github.com/grafana/loki/pull/1067) **rfratto**: Fix broken links to docs in README.md
* **Docs** [1064](https://github.com/grafana/loki/pull/1064) **jonaskello**: Fix spelling of HTTP header
* **Docs** [1063](https://github.com/grafana/loki/pull/1063) **rfratto**: docs: fix deprecated warning in api.md
* **Docs** [1060](https://github.com/grafana/loki/pull/1060) **rfratto**: Add Drone CI badge to README.md
* **Docs** [1053](https://github.com/grafana/loki/pull/1053) **flouthoc**: Fix Docs: Change Imagepull policy to IfNotpresent / Add loki-canary b…
* **Docs** [1048](https://github.com/grafana/loki/pull/1048) **wassan128**: Loki: Fix README link
* **Docs** [1042](https://github.com/grafana/loki/pull/1042) **daixiang0**: doc(ksonnet): include ksonnet-lib
* **Docs** [1039](https://github.com/grafana/loki/pull/1039) **sh0rez**: doc(production): replace ksonnet with Tanka
* **Docs** [1036](https://github.com/grafana/loki/pull/1036) **sh0rez**: feat: -version flag
* **Docs** [1025](https://github.com/grafana/loki/pull/1025) **oddlittlebird**: Update CONTRIBUTING.md
* **Docs** [1024](https://github.com/grafana/loki/pull/1024) **oddlittlebird**: Update README.md
* **Docs** [1014](https://github.com/grafana/loki/pull/1014) **polar3130**: Fix a link to correct doc and fix a typo
* **Docs** [1006](https://github.com/grafana/loki/pull/1006) **slim-bean**: fixing lots of broken links and a few typos
* **Docs** [1005](https://github.com/grafana/loki/pull/1005) **SmilingNavern**: Fix links to correct doc
* **Docs** [1004](https://github.com/grafana/loki/pull/1004) **rfratto**: docs: fix example with pulling systemd logs
* **Docs** [1003](https://github.com/grafana/loki/pull/1003) **oddlittlebird**: Loki: Update README.md
* **Docs** [984](https://github.com/grafana/loki/pull/984) **tomgs**: Changing "Usage" link in main readme after docs change
* **Docs** [983](https://github.com/grafana/loki/pull/983) **daixiang0**: update positions.yaml location reference
* **Docs** [982](https://github.com/grafana/loki/pull/982) **rfratto**: Documentation Rewrite
* **Docs** [961](https://github.com/grafana/loki/pull/961) **worr**: doc: Add permissions that IAM roles for Loki need
* **Docs** [933](https://github.com/grafana/loki/pull/933) **pracucci**: doc: move promtail doc into dedicated subfolder
* **Docs** [924](https://github.com/grafana/loki/pull/924) **pracucci**: doc: promtail known failure modes
* **Docs** [910](https://github.com/grafana/loki/pull/910) **slim-bean**: docs(build): Update docs around releasing and fix bug in version updating script
* **Docs** [850](https://github.com/grafana/loki/pull/850) **sh0rez**: docs: general documentation rework

## Build

* **Build** [1157](https://github.com/grafana/loki/pull/1157) **daixiang0**: Update golint
* **Build** [1133](https://github.com/grafana/loki/pull/1133) **daixiang0**: bump up golangci to 1.20
* **Build** [1121](https://github.com/grafana/loki/pull/1121) **pracucci**: Publish loki-canary binaries on release
* **Build** [1054](https://github.com/grafana/loki/pull/1054) **pstibrany**: Fix dep check warnings by running dep ensure
* **Build/release** [1018](https://github.com/grafana/loki/pull/1018) **slim-bean**: updating the image version for loki-canary and adding the version increment to the release_prepare script
* **Build/CI** [997](https://github.com/grafana/loki/pull/997) **slim-bean**: full cirlce
* **Build/CI** [996](https://github.com/grafana/loki/pull/996) **rfratto**: ci/drone: fix deploy command by escaping double quotes in JSON body
* **Build/CI** [995](https://github.com/grafana/loki/pull/995) **slim-bean**: use the loki-build-image for calling circle
* **Build/CI** [994](https://github.com/grafana/loki/pull/994) **slim-bean**: Also need bash for the deploy step from drone
* **Build/CI** [993](https://github.com/grafana/loki/pull/993) **slim-bean**: Add make to the alpine image used for calling the circle deploy task from drone.
* **Build/CI** [992](https://github.com/grafana/loki/pull/992) **sh0rez**: chore(packaging): fix GOPATH being overwritten
* **Build/CI** [991](https://github.com/grafana/loki/pull/991) **sh0rez**: chore(packaging): deploy from drone
* **Build/CI** [990](https://github.com/grafana/loki/pull/990) **sh0rez**: chore(ci/cd): breaking the circle
* **Build** [989](https://github.com/grafana/loki/pull/989) **sh0rez**: chore(packaging): simplify tagging
* **Build** [981](https://github.com/grafana/loki/pull/981) **sh0rez**: chore(packaging): loki windows/amd64
* **Build** [958](https://github.com/grafana/loki/pull/958) **daixiang0**: sync release pkgs name with release note
* **Build/CI** [914](https://github.com/grafana/loki/pull/914) **rfratto**: ci: update apt-get before installing deps for rootless step
* **Build** [911](https://github.com/grafana/loki/pull/911) **daixiang0**: optimize image tag script

## Deployment

* **Ksonnet** [1023](https://github.com/grafana/loki/pull/1023) **slim-bean**: make promtail daemonset name configurable
* **Ksonnet** [1021](https://github.com/grafana/loki/pull/1021) **rfratto**: ksonnet: update memcached and memcached-exporter images
* **Ksonnet** [1020](https://github.com/grafana/loki/pull/1020) **rfratto**: ksonnet: use consistent hashing in memcached client configs
* **Ksonnet** [1017](https://github.com/grafana/loki/pull/1017) **slim-bean**: make promtail configmap name configurable
* **Ksonnet** [946](https://github.com/grafana/loki/pull/946) **rfratto**: ksonnet: remove prefix from kvstore.consul settings in loki config
* **Ksonnet** [926](https://github.com/grafana/loki/pull/926) **slim-bean**: feat(promtail): Make cluster role configurable
<!-- -->
* **Helm** [1174](https://github.com/grafana/loki/pull/1174) **rally25rs**: loki-stack: Add release name to prometheus service name.
* **Helm** [1152](https://github.com/grafana/loki/pull/1152) **nicr9**: docs(helm): fix broken link to grafana datasource
* **Helm** [1134](https://github.com/grafana/loki/pull/1134) **minhdanh**: Helm chart: Allow additional scrape_configs to be added
* **Helm** [1111](https://github.com/grafana/loki/pull/1111) **ekarlso**: helm: Add support for passing arbitrary secrets
* **Helm** [1110](https://github.com/grafana/loki/pull/1110) **marcosnils**: Bump grafana image in loki helm chart
* **Helm** [1104](https://github.com/grafana/loki/pull/1104) **marcosnils**: <Examples>: Deploy prometheus from helm chart
* **Helm** [1058](https://github.com/grafana/loki/pull/1058) **polar3130**: Helm: Remove default value of storageClassName in loki/loki helm chart
* **Helm** [1056](https://github.com/grafana/loki/pull/1056) **polar3130**: Helm: Fix the reference error of loki/loki helm chart
* **Helm** [967](https://github.com/grafana/loki/pull/967) **makocchi-git**: helm chart: Add missing operator to promtail
* **Helm** [937](https://github.com/grafana/loki/pull/937) **minhdanh**: helm chart: Add support for additional labels and scrapeTimeout for serviceMonitors
* **Helm** [909](https://github.com/grafana/loki/pull/909) **angelbarrera92**: Feature: Add extra containers to loki helm chart
* **Helm** [855](https://github.com/grafana/loki/pull/855) **ikeeip**: set helm chart appVersion while release
* **Helm** [675](https://github.com/grafana/loki/pull/675) **cyriltovena**: Helm default ingester config

## Loki Canary

* **Loki-canary** [1137](https://github.com/grafana/loki/pull/1137) **slim-bean**: Add some additional logging to the canary on queries
* **Loki-canary** [1131](https://github.com/grafana/loki/pull/1131) **rfratto**: pkg/canary: use default HTTP client when reading from Loki

## Logcli

* **Logcli** [1168](https://github.com/grafana/loki/pull/1168) **sh0rez**: feat(cli): order flags by categories
* **Logcli** [1115](https://github.com/grafana/loki/pull/1115) **pracucci**: logcli: introduced QueryStringBuilder utility to clean up query string encoding
* **Logcli** [1103](https://github.com/grafana/loki/pull/1103) **pracucci**: logcli: added --step support to query command
* **Logcli** [987](https://github.com/grafana/loki/pull/987) **joe-elliott**: Logcli: Add Support for New Query Path

## Tooling

* **Dashboards** [1188](https://github.com/grafana/loki/pull/1188) **joe-elliott**: Adding Operational dashboards
* **Dashboards** [1143](https://github.com/grafana/loki/pull/1143) **joe-elliott**: Improved compression ratio histogram
* **Dashboards** [1126](https://github.com/grafana/loki/pull/1126) **joe-elliott**: Fix Loki Chunks Dashboard
* **Tools** [1108](https://github.com/grafana/loki/pull/1108) **joe-elliott**: Updated push path to current prod

## Plugins 

* **DockerDriver** [972](https://github.com/grafana/loki/pull/972) **cyriltovena**: Add stream label to docker driver
* **DockerDriver** [971](https://github.com/grafana/loki/pull/971) **cyriltovena**: Allow to pass max-size and max-file to the docker driver
* **DockerDriver** [970](https://github.com/grafana/loki/pull/970) **mindfl**: docker-driver compose labels support
<!-- -->
* **Fluentd** [928](https://github.com/grafana/loki/pull/928) **candlerb**: fluent-plugin-grafana-loki: Escape double-quotes in labels, and suppress labels with value nil
<!-- -->
* **Fluent Bit** [1155](https://github.com/grafana/loki/pull/1155) **cyriltovena**: rollback fluent-bit push path until we release 0.4
* **Fluent Bit** [1096](https://github.com/grafana/loki/pull/1096) **JensErat**: fluent-bit: edge case tests
* **Fluent Bit** [847](https://github.com/grafana/loki/pull/847) **cosmo0920**: fluent-bit shared object go plugin

## Misc

Loki is now using a Bot to help keep issues and PR's pruned based on age/relevancy.  Please don't hesitate to comment on an issue or PR that you think was closed by the stale-bot which you think should remain open!!

* **Github** [965](https://github.com/grafana/loki/pull/965) **rfratto**: Change label used to keep issues from being marked as stale to keepalive
* **Github** [964](https://github.com/grafana/loki/pull/964) **rfratto**: Add probot-stale configuration to close stale issues.











# 0.3.0 (2019-08-16)

### Features/Enhancements


* **Loki** [877](https://github.com/grafana/loki/pull/877) **pracucci**: loki: Improve Tailer loop
* **Loki** [870](https://github.com/grafana/loki/pull/870) **sandlis**: bigtable-backup: update docker image for bigtable-backup tool
* **Loki** [862](https://github.com/grafana/loki/pull/862) **sandlis**: live-tailing: preload all the historic entries before query context is cancelled
* **Loki** [858](https://github.com/grafana/loki/pull/858) **pracucci**: loki: removed unused TestGZIPCompression
* **Loki** [854](https://github.com/grafana/loki/pull/854) **adityacs**: Readiness probe for querier
* **Loki** [851](https://github.com/grafana/loki/pull/851) **cyriltovena**: Add readiness probe to distributor deployment.
* **Loki** [894](https://github.com/grafana/loki/pull/894) **rfratto**: ksonnet: update ingester config to transfer chunks on rollout
<!-- -->
* **Build** [901](https://github.com/grafana/loki/pull/901) **sh0rez**: chore(packaging): set tag length to 7
* **Build** [900](https://github.com/grafana/loki/pull/900) **sh0rez**: chore(ci/cd): fix grafanasaur credentials and CircleCI image build
* **Build** [891](https://github.com/grafana/loki/pull/891) **sh0rez**: chore(ci/cd): build containers using drone.io
* **Build** [888](https://github.com/grafana/loki/pull/888) **rfratto**: Makefile: disable building promtail with systemd support on non-amd64 platforms
* **Build** [887](https://github.com/grafana/loki/pull/887) **slim-bean**: chore(packaging): Dockerfile make avoid containers
* **Build** [886](https://github.com/grafana/loki/pull/886) **sh0rez**: chore(packaging): wrong executable format
* **Build** [855](https://github.com/grafana/loki/pull/855) **ikeeip**: set helm chart appVersion while release
<!-- -->
* **Promtail** [856](https://github.com/grafana/loki/pull/856) **martinbaillie**: promtail: Add ServiceMonitor and headless Service
* **Promtail** [809](https://github.com/grafana/loki/pull/809) **rfratto**: Makefile: build promtail with CGO_ENABLED if GOHOSTOS=GOOS=linux
* **Promtail** [730](https://github.com/grafana/loki/pull/730) **rfratto**: promtail: Add systemd journal support

> 809, 730 NOTE: Systemd journal support is currently limited to amd64 images, arm support should come in the future when the transition to building the arm image and binaries is done natively via an arm container 
<!-- -->
* **Docs** [896](https://github.com/grafana/loki/pull/896) **dalance**: docs: fix link format
* **Docs** [876](https://github.com/grafana/loki/pull/876) **BouchaaraAdil**: update Docs: update Retention section on Operations doc file
* **Docs** [864](https://github.com/grafana/loki/pull/864) **temal-**: docs: Replace old values in operations.md
* **Docs** [853](https://github.com/grafana/loki/pull/853) **cyriltovena**: Add governance documentation
<!-- -->
* **Deployment** [874](https://github.com/grafana/loki/pull/874) **slim-bean**: make our ksonnet a little more modular by parameterizing the chunk and index stores
* **Deployment** [857](https://github.com/grafana/loki/pull/857) **slim-bean**: Reorder relabeling rules to prevent pod label from overwriting config define labels

> 857 POSSIBLY BREAKING: If you relied on a custom pod label to overwrite one of the labels configured by the other sections of the scrape config: `job`, `namespace`, `instance`, `container_name` and/or `__path__`, this will no longer happen, the custom pod labels are now loaded first and will be overwritten by any of these listed labels.


### Fixes

* **Loki** [897](https://github.com/grafana/loki/pull/897) **pracucci**: Fix panic in tailer when an ingester is removed from the ring while tailing
* **Loki** [880](https://github.com/grafana/loki/pull/880) **cyriltovena**: fix a bug where nil line buffer would be put back
* **Loki** [859](https://github.com/grafana/loki/pull/859) **pracucci**: loki: Fixed out of order entries allowed in a chunk on edge case
<!-- -->
* **Promtail** [893](https://github.com/grafana/loki/pull/893) **rfratto**: pkg/promtail/positions: remove executable bit from positions file
<!-- -->
* **Deployment** [867](https://github.com/grafana/loki/pull/867) **slim-bean**: Update read dashboard to include only query and label query routes
* **Deployment** [865](https://github.com/grafana/loki/pull/865) **sandlis**: fix broken jsonnet for querier
<!-- -->
* **Canary** [889](https://github.com/grafana/loki/pull/889) **slim-bean**: fix(canary): Fix Flaky Tests
<!-- -->
* **Pipeline** [869](https://github.com/grafana/loki/pull/869) **jojohappy**: Pipeline: Fixed labels process test with same objects
<!-- -->
* **Logcli** [863](https://github.com/grafana/loki/pull/863) **adityacs**: Fix Nolabels parse metrics


# 0.2.0 (2019-08-02)

There were over 100 PR's merged since 0.1.0 was released, here's a highlight:

### Features / Enhancements

* **Loki**:  [521](https://github.com/grafana/loki/pull/521) Query label values and names are now fetched from the store.
* **Loki**:  [541](https://github.com/grafana/loki/pull/541) Improvements in live tailing of logs.
* **Loki**: [713](https://github.com/grafana/loki/pull/713) Storage memory improvement.
* **Loki**: [764](https://github.com/grafana/loki/pull/764) Tailing can fetch previous logs for context.
* **Loki**: [782](https://github.com/grafana/loki/pull/782) Performance improvement: Query storage by iterating through chunks in batches.
* **Loki**: [788](https://github.com/grafana/loki/pull/788) Querier timeouts.
* **Loki**: [794](https://github.com/grafana/loki/pull/794) Support ingester chunk transfer on shutdown.
* **Loki**: [729](https://github.com/grafana/loki/pull/729) Bigtable backup tool support.
<!-- -->
* **Pipeline**: [738](https://github.com/grafana/loki/pull/738) Added a template stage for manipulating label values.
* **Pipeline**: [732](https://github.com/grafana/loki/pull/732) Support for Unix timestamps.
* **Pipeline**: [760](https://github.com/grafana/loki/pull/760) Support timestamps without year.
<!-- -->
* **Helm**:  [641](https://github.com/grafana/loki/pull/641) Helm integration testing.
* **Helm**: [824](https://github.com/grafana/loki/pull/824) Add service monitor.
* **Helm**: [830](https://github.com/grafana/loki/pull/830) Customize namespace.
<!-- -->
* **Docker-Plugin**: [663](https://github.com/grafana/loki/pull/663) Created a Docker logging driver plugin.
<!-- -->
* **Fluent-Plugin**: [669](https://github.com/grafana/loki/pull/669) Ability to specify keys to remove.
* **Fluent-Plugin**: [709](https://github.com/grafana/loki/pull/709) Multi-worker support.
* **Fluent-Plugin**: [792](https://github.com/grafana/loki/pull/792) Add prometheus for metrics and update gems.
<!-- -->
* **Build**: [668](https://github.com/grafana/loki/pull/668),[762](https://github.com/grafana/loki/pull/762) Build multiple architecture containers.
<!-- -->
* **Loki-Canary**: [772](https://github.com/grafana/loki/pull/772) Moved into Loki project.

### Bugfixes

There were many fixes, here are a few of the most important:

* **Promtail**: [650](https://github.com/grafana/loki/pull/650) Build on windows.
* **Fluent-Plugin**: [667](https://github.com/grafana/loki/pull/667) Rename fluent plugin.
* **Docker-Plugin**: [813](https://github.com/grafana/loki/pull/813) Fix panic for newer docker version (18.09.7+).


# 0.1.0 (2019-06-03)

First (beta) Release!
