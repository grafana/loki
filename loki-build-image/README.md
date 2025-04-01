# Build image

## Versions

### 0.34.1-loki-2.9.x

- Update faillint to v1.15.0

### 0.34.0-loki-2.9.x

- Update Go version to 1.23.x

* This release should only be used for the release branches such as 2.9.x, 2.8.x and 2.7.x. 

### 0.33.1-golangci.1.51.2

- Update to Go version 1.20.9 but restore golangci-lint to v1.51.2

* This release should only be used for the release branches such as 2.9.x, 2.8.x and 2.7.x. 
* The current release of the build image uses golangci-lint to v1.53.2 which makes a lot of linter checks mandatory causing a huge amount of fixes See https://github.com/grafana/loki/pull/9601. To avoid the integration problems this build image will be used in those branches.

### 0.29.3-golangci.1.51.2

- Update to Go version 1.20.6 but restore golangci-lint to v1.51.2

* This release should only be used for the release branches such as 2.8.x and 2.7.x. *
The current release of the build image uses golangci-lint to v1.53.2 which makes
a lot of linter checks mandatory causing a huge amount of fixes 
See https://github.com/grafana/loki/pull/9601. To avoid the integration problems this
build image will be used in those branches.

### 0.29.3

- Update to Go version 1.20.6
