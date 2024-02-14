# Changelog

## [1.9.2](https://github.com/grafana/loki-release/compare/v1.9.1...v1.9.2) (2024-02-14)


### Bug Fixes

* missing docker steps ([46ca74e](https://github.com/grafana/loki-release/commit/46ca74e22e5e7597ddbf07bb85ab4bf7a8f67cb1))

## [1.9.1](https://github.com/grafana/loki-release/compare/v1.9.0...v1.9.1) (2024-02-14)


### Bug Fixes

* add id-token write permission to release pipeline ([03a6364](https://github.com/grafana/loki-release/commit/03a6364b4404ce1f226393d665a608a7a00e341d))

## [1.9.0](https://github.com/grafana/loki-release/compare/v1.8.15-alpha.1...v1.9.0) (2024-02-14)


### Features

* add build image to dist step ([cdff54a](https://github.com/grafana/loki-release/commit/cdff54a3f3fddb9bb5aa725143f6d95a95af00a4))
* extract version to shared step ([c0af535](https://github.com/grafana/loki-release/commit/c0af535d7bc78f83feca93922cb64f5727b8465d))
* further refine validation steps ([1bc0473](https://github.com/grafana/loki-release/commit/1bc04735e4b14a441af125f8b1b94eb57a06a6a4))
* make arm configurable ([6582474](https://github.com/grafana/loki-release/commit/6582474ca4ca750f4f779863d31a3c4e733082fd))
* patch release docker fixes from 1.8.x ([#88](https://github.com/grafana/loki-release/issues/88)) ([5b0f7c1](https://github.com/grafana/loki-release/commit/5b0f7c1a81ba4c28804d11f755148f7bdb7bf7b9))
* use build image ([4a1f123](https://github.com/grafana/loki-release/commit/4a1f1239de1342825e183e93475370504be4270e))
* working pipeline as a jsonnet library ([#81](https://github.com/grafana/loki-release/issues/81)) ([97f0254](https://github.com/grafana/loki-release/commit/97f0254db04c6dba53080aac4f6ec69bf4be7993))


### Bug Fixes

* add tar ([0d05ac4](https://github.com/grafana/loki-release/commit/0d05ac4dac550301fcf9c5ea6c0e3bd7d02330a2))
* bring back manual jsonnetfmt installation ([63abcdd](https://github.com/grafana/loki-release/commit/63abcdd7617a65b250bc348565c97793ba9a2b20))
* bring back manual shellcheck installation ([36ebc07](https://github.com/grafana/loki-release/commit/36ebc075d3f435407ae5de3defb29b467aefabfe))
* fetch release repo for version step ([d84462b](https://github.com/grafana/loki-release/commit/d84462bb07e6839b5d2ff35c4b46f7c24f4a3140))
* remove sudo and redundant deps ([259bb05](https://github.com/grafana/loki-release/commit/259bb05a24defabb774836559521e30ee9ee4e26))
* version outputs ([8a1e8bd](https://github.com/grafana/loki-release/commit/8a1e8bd51ff300716cc13c448235d5d4711f429b))

## [1.8.15-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.14-alpha.1...v1.8.15-alpha.1) (2024-02-13)

### Features

- support prerelease in docker push action
  ([a0c99cf](https://github.com/grafana/loki-release/commit/a0c99cf8260c84733e3044028b825c166a2d1777))

### Bug Fixes

- docker tests
  ([95f31d5](https://github.com/grafana/loki-release/commit/95f31d57d065e9f772cf82fd021a9c5bc5aae723))

## [1.8.14-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.13-alpha.1...v1.8.14-alpha.1) (2024-02-13)

### Features

- ability to get docker creds from vault
  ([b3826e1](https://github.com/grafana/loki-release/commit/b3826e16e3bf530aa70743f7e48dc20d1b16a7bf))

## [1.8.13-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.12-alpha.1...v1.8.13-alpha.1) (2024-02-12)

### Features

- cleanup node output
  ([9b52c56](https://github.com/grafana/loki-release/commit/9b52c563a012f4ccec2942427e9e10a70c74a284))
- fetch release lib
  ([1a0daa8](https://github.com/grafana/loki-release/commit/1a0daa8c2768d256b67f24a3c62929aca6d2af11))
- get version using node
  ([8598bc5](https://github.com/grafana/loki-release/commit/8598bc54ae263d8a2df388bf098ac7d4b4d4df66))
- include prerelease in image and binary version
  ([1d4b500](https://github.com/grafana/loki-release/commit/1d4b50047917a4dc73eeb74c029d71df1643044d))
- need to test something
  ([a6d3c56](https://github.com/grafana/loki-release/commit/a6d3c56e064d8a2d0e09ff0cd0966e1c99d2be8f))

### Bug Fixes

- add get-version script
  ([5bb3ef1](https://github.com/grafana/loki-release/commit/5bb3ef19368a3826c302c9f2061f517e9c8188bf))
- jq syntax
  ([3d633c7](https://github.com/grafana/loki-release/commit/3d633c7e3df1c3df6e4cd660b3893944b3ee357f))
- keep banging head at jq
  ([c296b17](https://github.com/grafana/loki-release/commit/c296b17130ad10fcaac6ddb2877b4252937195c4))
- quote exported version
  ([8b0769b](https://github.com/grafana/loki-release/commit/8b0769bd72c729f5b0223f2298628ca2243a2e39))

## [1.8.12-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.11...v1.8.12-alpha.1) (2024-02-12)

### Features

- attempt a release candidate release
  ([71a2480](https://github.com/grafana/loki-release/commit/71a248094509d206fa24fdeba8f17e7c05f631d0))

### Bug Fixes

- currently released version
  ([d1a4710](https://github.com/grafana/loki-release/commit/d1a4710756eacdbe12d33919ef76a35a785e71d7))

### Miscellaneous Chores

- release 1.8.12-alpha.1
  ([55c86a9](https://github.com/grafana/loki-release/commit/55c86a9f00de72362b492ed647ff2ab4631ddb0a))

## [1.8.11](https://github.com/grafana/loki-release/compare/v1.8.10...v1.8.11) (2024-02-09)

### Features

- upgrade setup-gcloud-action
  ([8e7c2ff](https://github.com/grafana/loki-release/commit/8e7c2fff09066908558a9363abbf9b71c260cc98))

## [1.8.10](https://github.com/grafana/loki-release/compare/v1.8.9...v1.8.10) (2024-02-09)

### Bug Fixes

- husky pre-commit logic, add rendered workflow
  ([8706bf3](https://github.com/grafana/loki-release/commit/8706bf320eff20cb3eeb3d713d88c65847e9df45))

## [1.8.9](https://github.com/grafana/loki-release/compare/v1.8.8...v1.8.9) (2024-02-09)

### Features

- make jsonnet a library rather than workflow templates
  ([ae9c304](https://github.com/grafana/loki-release/commit/ae9c304b9d98e7d4126cb4b32d3f6ac8c1989f71))

### Bug Fixes

- need to upload artifacts from git repo
  ([44e6a1f](https://github.com/grafana/loki-release/commit/44e6a1fd2fa95f206b2930c5b536064f3a339830))
- publishImage dependencies
  ([2c123aa](https://github.com/grafana/loki-release/commit/2c123aa0303b3089328972e7de475610be9f7ca2))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-09)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))
- make jsonnet a library rather than workflow templates
  ([ae9c304](https://github.com/grafana/loki-release/commit/ae9c304b9d98e7d4126cb4b32d3f6ac8c1989f71))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- need to upload artifacts from git repo
  ([44e6a1f](https://github.com/grafana/loki-release/commit/44e6a1fd2fa95f206b2930c5b536064f3a339830))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-09)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))
- make jsonnet a library rather than workflow templates
  ([ae9c304](https://github.com/grafana/loki-release/commit/ae9c304b9d98e7d4126cb4b32d3f6ac8c1989f71))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-09)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-08)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.7](https://github.com/grafana/loki-release/compare/v1.8.6...v1.8.7) (2024-02-08)

### Features

- use locker load instead of import to retain metadata
  ([33764f0](https://github.com/grafana/loki-release/commit/33764f05e40dc77c51b5e40cd160d2f2fa7c73c0))

### Bug Fixes

- short_platform
  ([712e182](https://github.com/grafana/loki-release/commit/712e182e83f818ccd52656a7dd5fd1d4415c068d))

## [1.8.6](https://github.com/grafana/loki-release/compare/v1.8.5...v1.8.6) (2024-02-08)

### Features

- fix image pushing
  ([921a5af](https://github.com/grafana/loki-release/commit/921a5afaa9a59620f9299aa3155dc2e73067cfe9))
- list images before pushing
  ([019da6d](https://github.com/grafana/loki-release/commit/019da6dee81fa2594b79affcdf80266f91859956))
- remove debug workflow
  ([356332a](https://github.com/grafana/loki-release/commit/356332a5107fe500091bb4604a3783b30c1a4456))
- use synchronous exec
  ([ff924d4](https://github.com/grafana/loki-release/commit/ff924d4fea197dc308a6a7b52bf56a380e3adbfa))

### Bug Fixes

- debug workflow inputs
  ([7c093fe](https://github.com/grafana/loki-release/commit/7c093fec0bed650a6268bb9f5be93a47772e5905))
- image prefix
  ([ce60c28](https://github.com/grafana/loki-release/commit/ce60c28da2d687e1e4484f03f5cdab33991a19c8))

## [1.8.5](https://github.com/grafana/loki-release/compare/v1.8.4...v1.8.5) (2024-02-08)

### Features

- always run push, incease step log level
  ([c768bdd](https://github.com/grafana/loki-release/commit/c768bddef2189e6f78b74328f4e53beba97da0bf))
- exec docker commands in img dir
  ([e49fb53](https://github.com/grafana/loki-release/commit/e49fb53f7341306beebd72423bb34e23015cb55e))

### Bug Fixes

- need the push images conditional back
  ([8abf3cb](https://github.com/grafana/loki-release/commit/8abf3cbeb94671c6880bf2d25729287cc5da89a1))

## [1.8.4](https://github.com/grafana/loki-release/compare/v1.8.3...v1.8.4) (2024-02-08)

### Features

- fix folder listing, better error handling
  ([2193521](https://github.com/grafana/loki-release/commit/21935215a13082416744dc6e77fce1155888e90c))

## [1.8.3](https://github.com/grafana/loki-release/compare/v1.8.2...v1.8.3) (2024-02-08)

### Features

- fix image dir
  ([b1b5bcd](https://github.com/grafana/loki-release/commit/b1b5bcde3b209aec1004e9d67d497868c04eaad1))

  =======

## [1.8.15-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.14-alpha.1...v1.8.15-alpha.1) (2024-02-13)

### Features

- support prerelease in docker push action
  ([a0c99cf](https://github.com/grafana/loki-release/commit/a0c99cf8260c84733e3044028b825c166a2d1777))

### Bug Fixes

- docker tests
  ([95f31d5](https://github.com/grafana/loki-release/commit/95f31d57d065e9f772cf82fd021a9c5bc5aae723))

## [1.8.14-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.13-alpha.1...v1.8.14-alpha.1) (2024-02-13)

### Features

- ability to get docker creds from vault
  ([b3826e1](https://github.com/grafana/loki-release/commit/b3826e16e3bf530aa70743f7e48dc20d1b16a7bf))

## [1.8.13-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.12-alpha.1...v1.8.13-alpha.1) (2024-02-12)

### Features

- cleanup node output
  ([9b52c56](https://github.com/grafana/loki-release/commit/9b52c563a012f4ccec2942427e9e10a70c74a284))
- fetch release lib
  ([1a0daa8](https://github.com/grafana/loki-release/commit/1a0daa8c2768d256b67f24a3c62929aca6d2af11))
- get version using node
  ([8598bc5](https://github.com/grafana/loki-release/commit/8598bc54ae263d8a2df388bf098ac7d4b4d4df66))
- include prerelease in image and binary version
  ([1d4b500](https://github.com/grafana/loki-release/commit/1d4b50047917a4dc73eeb74c029d71df1643044d))
- need to test something
  ([a6d3c56](https://github.com/grafana/loki-release/commit/a6d3c56e064d8a2d0e09ff0cd0966e1c99d2be8f))

### Bug Fixes

- add get-version script
  ([5bb3ef1](https://github.com/grafana/loki-release/commit/5bb3ef19368a3826c302c9f2061f517e9c8188bf))
- jq syntax
  ([3d633c7](https://github.com/grafana/loki-release/commit/3d633c7e3df1c3df6e4cd660b3893944b3ee357f))
- keep banging head at jq
  ([c296b17](https://github.com/grafana/loki-release/commit/c296b17130ad10fcaac6ddb2877b4252937195c4))
- quote exported version
  ([8b0769b](https://github.com/grafana/loki-release/commit/8b0769bd72c729f5b0223f2298628ca2243a2e39))

## [1.8.12-alpha.1](https://github.com/grafana/loki-release/compare/v1.8.11...v1.8.12-alpha.1) (2024-02-12)

### Features

- attempt a release candidate release
  ([71a2480](https://github.com/grafana/loki-release/commit/71a248094509d206fa24fdeba8f17e7c05f631d0))

### Bug Fixes

- currently released version
  ([d1a4710](https://github.com/grafana/loki-release/commit/d1a4710756eacdbe12d33919ef76a35a785e71d7))

### Miscellaneous Chores

- release 1.8.12-alpha.1
  ([55c86a9](https://github.com/grafana/loki-release/commit/55c86a9f00de72362b492ed647ff2ab4631ddb0a))

## [1.8.11](https://github.com/grafana/loki-release/compare/v1.8.10...v1.8.11) (2024-02-09)

### Features

- upgrade setup-gcloud-action
  ([8e7c2ff](https://github.com/grafana/loki-release/commit/8e7c2fff09066908558a9363abbf9b71c260cc98))

## [1.8.10](https://github.com/grafana/loki-release/compare/v1.8.9...v1.8.10) (2024-02-09)

### Bug Fixes

- husky pre-commit logic, add rendered workflow
  ([8706bf3](https://github.com/grafana/loki-release/commit/8706bf320eff20cb3eeb3d713d88c65847e9df45))

## [1.8.9](https://github.com/grafana/loki-release/compare/v1.8.8...v1.8.9) (2024-02-09)

### Features

- make jsonnet a library rather than workflow templates
  ([ae9c304](https://github.com/grafana/loki-release/commit/ae9c304b9d98e7d4126cb4b32d3f6ac8c1989f71))

### Bug Fixes

- need to upload artifacts from git repo
  ([44e6a1f](https://github.com/grafana/loki-release/commit/44e6a1fd2fa95f206b2930c5b536064f3a339830))
- publishImage dependencies
  ([2c123aa](https://github.com/grafana/loki-release/commit/2c123aa0303b3089328972e7de475610be9f7ca2))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-09)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))
- make jsonnet a library rather than workflow templates
  ([ae9c304](https://github.com/grafana/loki-release/commit/ae9c304b9d98e7d4126cb4b32d3f6ac8c1989f71))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- need to upload artifacts from git repo
  ([44e6a1f](https://github.com/grafana/loki-release/commit/44e6a1fd2fa95f206b2930c5b536064f3a339830))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-09)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))
- make jsonnet a library rather than workflow templates
  ([ae9c304](https://github.com/grafana/loki-release/commit/ae9c304b9d98e7d4126cb4b32d3f6ac8c1989f71))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-09)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.8](https://github.com/grafana/loki-release/compare/v1.8.7...v1.8.8) (2024-02-08)

### Features

- break release into multiple jobs
  ([0da93a4](https://github.com/grafana/loki-release/commit/0da93a4eba81c174eb3a1fedbb0e819d0b1a0b84))

### Bug Fixes

- docker image ls
  ([154caf7](https://github.com/grafana/loki-release/commit/154caf79c42e789d27a91b4c5fb6a2234e37b69f))
- fetch the correct repos
  ([ae60528](https://github.com/grafana/loki-release/commit/ae60528cf42fb2205ab52ad9874f8070e03a1f49))
- release pipeline
  ([d35c5cc](https://github.com/grafana/loki-release/commit/d35c5cc4674eeffd3832e39737e36b256f415d21))

## [1.8.7](https://github.com/grafana/loki-release/compare/v1.8.6...v1.8.7) (2024-02-08)

### Features

- use locker load instead of import to retain metadata
  ([33764f0](https://github.com/grafana/loki-release/commit/33764f05e40dc77c51b5e40cd160d2f2fa7c73c0))

### Bug Fixes

- short_platform
  ([712e182](https://github.com/grafana/loki-release/commit/712e182e83f818ccd52656a7dd5fd1d4415c068d))

## [1.8.6](https://github.com/grafana/loki-release/compare/v1.8.5...v1.8.6) (2024-02-08)

### Features

- fix image pushing
  ([921a5af](https://github.com/grafana/loki-release/commit/921a5afaa9a59620f9299aa3155dc2e73067cfe9))
- list images before pushing
  ([019da6d](https://github.com/grafana/loki-release/commit/019da6dee81fa2594b79affcdf80266f91859956))
- remove debug workflow
  ([356332a](https://github.com/grafana/loki-release/commit/356332a5107fe500091bb4604a3783b30c1a4456))
- use synchronous exec
  ([ff924d4](https://github.com/grafana/loki-release/commit/ff924d4fea197dc308a6a7b52bf56a380e3adbfa))

### Bug Fixes

- debug workflow inputs
  ([7c093fe](https://github.com/grafana/loki-release/commit/7c093fec0bed650a6268bb9f5be93a47772e5905))
- image prefix
  ([ce60c28](https://github.com/grafana/loki-release/commit/ce60c28da2d687e1e4484f03f5cdab33991a19c8))

## [1.8.5](https://github.com/grafana/loki-release/compare/v1.8.4...v1.8.5) (2024-02-08)

### Features

- always run push, incease step log level
  ([c768bdd](https://github.com/grafana/loki-release/commit/c768bddef2189e6f78b74328f4e53beba97da0bf))
- exec docker commands in img dir
  ([e49fb53](https://github.com/grafana/loki-release/commit/e49fb53f7341306beebd72423bb34e23015cb55e))

### Bug Fixes

- need the push images conditional back
  ([8abf3cb](https://github.com/grafana/loki-release/commit/8abf3cbeb94671c6880bf2d25729287cc5da89a1))

## [1.8.4](https://github.com/grafana/loki-release/compare/v1.8.3...v1.8.4) (2024-02-08)

### Features

- fix folder listing, better error handling
  ([2193521](https://github.com/grafana/loki-release/commit/21935215a13082416744dc6e77fce1155888e90c))

## [1.8.3](https://github.com/grafana/loki-release/compare/v1.8.2...v1.8.3) (2024-02-08)

### Features

- fix image dir
  ([b1b5bcd](https://github.com/grafana/loki-release/commit/b1b5bcde3b209aec1004e9d67d497868c04eaad1))
  > > > > > > > release-1.8.x

## [1.8.2](https://github.com/grafana/loki-release/compare/v1.8.1...v1.8.2) (2024-02-08)

### Features

- add push images action
  ([039c90e](https://github.com/grafana/loki-release/commit/039c90e1f9669c72b7ab23aa3e60f113d810cb1f))

### Bug Fixes

- errant semicolon
  ([069419e](https://github.com/grafana/loki-release/commit/069419e30d3cc192bc9ded19d7ea287f210dde98))
- fromJSON typo
  ([50d26b5](https://github.com/grafana/loki-release/commit/50d26b5b6282ece769e2c0765f1be7f9b2a93d57))

## [1.8.1](https://github.com/grafana/loki-release/compare/v1.8.0...v1.8.1) (2024-02-06)

### Bug Fixes

- use docker load instead of import
  ([9485994](https://github.com/grafana/loki-release/commit/948599450cde01cc182dd9d09e820a938664026c))

## [1.8.0](https://github.com/grafana/loki-release/compare/v1.7.1...v1.8.0) (2024-02-06)

### Features

- build all the images
  ([cc8ce7a](https://github.com/grafana/loki-release/commit/cc8ce7a2e7160f59c0d4b17676043d562dad0f5f))
- build images in test workflow
  ([53e8601](https://github.com/grafana/loki-release/commit/53e8601b5a9ba374aca28f9698fa72c627dfe33f))
- bump actions to latest node 20 versions
  ([#59](https://github.com/grafana/loki-release/issues/59))
  ([3f75548](https://github.com/grafana/loki-release/commit/3f755480e578c19c7b0e2128f8c492b72cd874ae))
- create images dir
  ([86c832c](https://github.com/grafana/loki-release/commit/86c832c24d293afd7b3954ec3ff7ab9c7e50ee20))
- guard pushing images on should release
  ([eaa75a8](https://github.com/grafana/loki-release/commit/eaa75a829a2cd7ffef9b0200cc0bcc2f5ff11e63))
- push the images
  ([931bcba](https://github.com/grafana/loki-release/commit/931bcba124f9e1b5d06121c2c572fcffd8cc5e20))
- skip steps not jobs ([#58](https://github.com/grafana/loki-release/issues/58))
  ([2fa7aa5](https://github.com/grafana/loki-release/commit/2fa7aa569fe30d8c9ce2a80b12bdb7b7f70844aa))
- user release-please to get the version
  ([1a2420f](https://github.com/grafana/loki-release/commit/1a2420ffd279c43a82f38c704f1db3b2c6be6707))

### Bug Fixes

- add google auth to image steps
  ([85cba30](https://github.com/grafana/loki-release/commit/85cba30090373b064159f82c09cd209ae809d2bc))
- attempt to fix jq parsing of version
  ([d598df2](https://github.com/grafana/loki-release/commit/d598df26c929eddccdcef11f45927a7e16eca022))
- clean target
  ([287ce46](https://github.com/grafana/loki-release/commit/287ce4633dff71bf435ad3ec5b19eca3be285fbd))
- dry run output filename
  ([679c235](https://github.com/grafana/loki-release/commit/679c235d351e28abac6c39d77cbb0658a346b1a2))
- extract branch name in build flow
  ([aafcdde](https://github.com/grafana/loki-release/commit/aafcddeef79be11790151614387cf20c32a1dacc))
- image version logic
  ([ddf4234](https://github.com/grafana/loki-release/commit/ddf4234c473aa28bda890e2f2bc7edeebfa44ebd))
- jq syntax error
  ([bd84c39](https://github.com/grafana/loki-release/commit/bd84c3951aea9896580b19f2a0ec31d2c032a9d1))
- limit operator to amd64 image
  ([be93744](https://github.com/grafana/loki-release/commit/be937442802b255e7be3767b0f752000400cb37f))
- need to fetch images before upload
  ([3a46a60](https://github.com/grafana/loki-release/commit/3a46a60f7ee7e1733a504d3a037f9e664015a669))
- version extraction
  ([81cca0d](https://github.com/grafana/loki-release/commit/81cca0dcdebc1907aeb5189363ef92609c996203))

## [1.7.1](https://github.com/grafana/loki-release/compare/v1.7.0...v1.7.1) (2024-01-23)

### Features

- the best feature
  ([9f4f208](https://github.com/grafana/loki-release/commit/9f4f2080ace065896ef7168df59a0b4279828413))

## [1.7.0](https://github.com/grafana/loki-release/compare/v1.2.0...v1.7.0) (2024-01-22)

### Features

- add artifacts to release
  ([4fea492](https://github.com/grafana/loki-release/commit/4fea4927fe360ce4031fa0553f6536d8fd980d17))
- add backport action
  ([4df43c6](https://github.com/grafana/loki-release/commit/4df43c665e46daa36fca0b9be0932b2393ebb5c7))
- add correct updaters to release pull request
  ([d50db7a](https://github.com/grafana/loki-release/commit/d50db7a6ce579a8b21c0f84b3767eb6f9c24f9dc))
- add create release step
  ([fe8c2fb](https://github.com/grafana/loki-release/commit/fe8c2fbe3d6bd7617226b6e7e9f5abdd77aec483))
- add install binary action
  ([947ed95](https://github.com/grafana/loki-release/commit/947ed95bf340634e24bfc316eda4f20d356190de))
- add more functionality from release please
  ([6c871fc](https://github.com/grafana/loki-release/commit/6c871fc3368e4eece45c7fa807e1831164f4debe))
- add release steps to jsonnet-build workflow
  ([55a14d6](https://github.com/grafana/loki-release/commit/55a14d67b6cdbda880abe16ed3cd1db969714b1c))
- added github interactions to release plugin
  ([808c34a](https://github.com/grafana/loki-release/commit/808c34aa4bc81a523683b2b345eccff75e628e2f))
- backport workflow
  ([2d58920](https://github.com/grafana/loki-release/commit/2d589204e7557a891a04662a8258597be37a5a54))
- bring back all steps
  ([ab86186](https://github.com/grafana/loki-release/commit/ab86186caf0e7218e9be1fd7a84df58545c08517))
- build images for multiple platforms
  ([49a846e](https://github.com/grafana/loki-release/commit/49a846e2da75e56cd22fd4bbadb2469919afed2e))
- build pipeline using jsonnet for reuse
  ([b6cc287](https://github.com/grafana/loki-release/commit/b6cc2876ac3a593ede5644ca2e5a3bbec5572837))
- **ci:** add prepare workflow
  ([b100d6f](https://github.com/grafana/loki-release/commit/b100d6fe25669928cb023e4b869af0cfe353b7b1))
- **ci:** add release-please action
  ([b994e1b](https://github.com/grafana/loki-release/commit/b994e1bb5a36e7f6e1f0134a1ea104143d0bce3f))
- **ci:** bump minor for k branches
  ([44d573d](https://github.com/grafana/loki-release/commit/44d573d107dd71ae26e2884a8d5e75c2e7a6d76f))
- **ci:** fix default-branch
  ([fe48dc3](https://github.com/grafana/loki-release/commit/fe48dc34c4e9cbfc42d5afff5ad79c0b1daf464a))
- **ci:** try to move versioning into workflow definition
  ([d66d51a](https://github.com/grafana/loki-release/commit/d66d51a562d6384e2966acd1cbf3755b99ff93a4))
- create release branch from k release
  ([07f2b06](https://github.com/grafana/loki-release/commit/07f2b064a9a0234a0cfe87cf390bb6f055dff967))
- create release first
  ([e2d4e73](https://github.com/grafana/loki-release/commit/e2d4e7318ec2f581296b5341363698c222352536))
- exclude component from tag for better release notes
  ([9841d98](https://github.com/grafana/loki-release/commit/9841d98bbfefd2a1d972c4bb81f5a4d6bcffc5e7))
- first try at storing build artifacts in GCS
  ([8801d68](https://github.com/grafana/loki-release/commit/8801d686e7b4084bb8e82f5776c8a7148fa219a5))
- fix typo in versioing-strategy
  ([5a47a62](https://github.com/grafana/loki-release/commit/5a47a62cdea90bbf21cefd8085eaf8b47650bd51))
- fix versioning strategy
  ([0008487](https://github.com/grafana/loki-release/commit/0008487cad2fe5e54fdacde3ff0b2724c21db979))
- make it a dry run
  ([4d63549](https://github.com/grafana/loki-release/commit/4d63549df4170dc67b4fe6a31175693504bab47a))
- make workflow reusable
  ([c01b721](https://github.com/grafana/loki-release/commit/c01b7213100dca261ddf9cad255cf4428bebd8a7))
- nest workflows in folder
  ([2eab631](https://github.com/grafana/loki-release/commit/2eab6317c6381b2827dac7409bfd8dfcaf96f4eb))
- output created/updated PR
  ([3d76523](https://github.com/grafana/loki-release/commit/3d76523376309db2e95d8f05716aa0c3d1b228e7))
- release the correct sha
  ([#47](https://github.com/grafana/loki-release/issues/47))
  ([70b72f2](https://github.com/grafana/loki-release/commit/70b72f26b74f3d999efbdbbd937c422adba27701))
- remove release-please config file
  ([#49](https://github.com/grafana/loki-release/issues/49))
  ([50b19ae](https://github.com/grafana/loki-release/commit/50b19ae967d0301fce1a00f3837a709753dfad1d))
- remove unused code
  ([0ad335c](https://github.com/grafana/loki-release/commit/0ad335cf7b13c6cb374d85ec05d127300c01edba))
- run create release on release branches
  ([c8ba75f](https://github.com/grafana/loki-release/commit/c8ba75ffe27b6288de7b048b716173a131352ddc))
- try a merge to main w/ backport strategy
  ([cf996f4](https://github.com/grafana/loki-release/commit/cf996f4cb2366df03c668af2b572f845c904e7ac))
- try using release-please for release again
  ([3ca6579](https://github.com/grafana/loki-release/commit/3ca6579cb00cde5843021c5ccd99c83139db54ed))

### Bug Fixes

- remove quotes from labels
  ([46b728f](https://github.com/grafana/loki-release/commit/46b728f2ea3c24058a3834f1adbff12f31341392))

### Miscellaneous Chores

- prepare 1.7.0
  ([a5600c6](https://github.com/grafana/loki-release/commit/a5600c672ae100a47d182c998d5c0021c04206bd))

## [1.6.0](https://github.com/grafana/loki-release/compare/v1.6.0...v1.6.0) (2024-01-19)

### Features

- add a go.mod and go file
  ([3713672](https://github.com/grafana/loki-release/commit/3713672ba00937015fb97fcc1efb26acfe5e5a7b))
- add artifacts to release
  ([4fea492](https://github.com/grafana/loki-release/commit/4fea4927fe360ce4031fa0553f6536d8fd980d17))
- add backport action
  ([4df43c6](https://github.com/grafana/loki-release/commit/4df43c665e46daa36fca0b9be0932b2393ebb5c7))
- add correct updaters to release pull request
  ([d50db7a](https://github.com/grafana/loki-release/commit/d50db7a6ce579a8b21c0f84b3767eb6f9c24f9dc))
- add create release step
  ([fe8c2fb](https://github.com/grafana/loki-release/commit/fe8c2fbe3d6bd7617226b6e7e9f5abdd77aec483))
- add install binary action
  ([947ed95](https://github.com/grafana/loki-release/commit/947ed95bf340634e24bfc316eda4f20d356190de))
- add more cli flags to get the others to work?
  ([8e8a635](https://github.com/grafana/loki-release/commit/8e8a6354757fad3cc91b20ce575ec20e4dc28685))
- add more functionality from release please
  ([6c871fc](https://github.com/grafana/loki-release/commit/6c871fc3368e4eece45c7fa807e1831164f4debe))
- add release steps to jsonnet-build workflow
  ([55a14d6](https://github.com/grafana/loki-release/commit/55a14d67b6cdbda880abe16ed3cd1db969714b1c))
- added github interactions to release plugin
  ([808c34a](https://github.com/grafana/loki-release/commit/808c34aa4bc81a523683b2b345eccff75e628e2f))
- bring back all steps
  ([ab86186](https://github.com/grafana/loki-release/commit/ab86186caf0e7218e9be1fd7a84df58545c08517))
- build images for multiple platforms
  ([49a846e](https://github.com/grafana/loki-release/commit/49a846e2da75e56cd22fd4bbadb2469919afed2e))
- build pipeline using jsonnet for reuse
  ([b6cc287](https://github.com/grafana/loki-release/commit/b6cc2876ac3a593ede5644ca2e5a3bbec5572837))
- **ci:** add prepare workflow
  ([b100d6f](https://github.com/grafana/loki-release/commit/b100d6fe25669928cb023e4b869af0cfe353b7b1))
- **ci:** add release-please action
  ([b994e1b](https://github.com/grafana/loki-release/commit/b994e1bb5a36e7f6e1f0134a1ea104143d0bce3f))
- **ci:** bump minor for k branches
  ([44d573d](https://github.com/grafana/loki-release/commit/44d573d107dd71ae26e2884a8d5e75c2e7a6d76f))
- **ci:** fix default-branch
  ([fe48dc3](https://github.com/grafana/loki-release/commit/fe48dc34c4e9cbfc42d5afff5ad79c0b1daf464a))
- **ci:** try to move versioning into workflow definition
  ([d66d51a](https://github.com/grafana/loki-release/commit/d66d51a562d6384e2966acd1cbf3755b99ff93a4))
- create release branch from k release
  ([07f2b06](https://github.com/grafana/loki-release/commit/07f2b064a9a0234a0cfe87cf390bb6f055dff967))
- create release first
  ([e2d4e73](https://github.com/grafana/loki-release/commit/e2d4e7318ec2f581296b5341363698c222352536))
- exclude component from tag for better release notes
  ([9841d98](https://github.com/grafana/loki-release/commit/9841d98bbfefd2a1d972c4bb81f5a4d6bcffc5e7))
- first try at storing build artifacts in GCS
  ([8801d68](https://github.com/grafana/loki-release/commit/8801d686e7b4084bb8e82f5776c8a7148fa219a5))
- fix typo in versioing-strategy
  ([5a47a62](https://github.com/grafana/loki-release/commit/5a47a62cdea90bbf21cefd8085eaf8b47650bd51))
- fix versioning strategy
  ([0008487](https://github.com/grafana/loki-release/commit/0008487cad2fe5e54fdacde3ff0b2724c21db979))
- make footer more informative
  ([d922830](https://github.com/grafana/loki-release/commit/d922830c6c7bd5293e09140e8245efa29f5dc7cb))
- make it a dry run
  ([4d63549](https://github.com/grafana/loki-release/commit/4d63549df4170dc67b4fe6a31175693504bab47a))
- make workflow reusable
  ([c01b721](https://github.com/grafana/loki-release/commit/c01b7213100dca261ddf9cad255cf4428bebd8a7))
- nest workflows in folder
  ([2eab631](https://github.com/grafana/loki-release/commit/2eab6317c6381b2827dac7409bfd8dfcaf96f4eb))
- output created/updated PR
  ([3d76523](https://github.com/grafana/loki-release/commit/3d76523376309db2e95d8f05716aa0c3d1b228e7))
- put images in different subfolder in bucket
  ([b4c9364](https://github.com/grafana/loki-release/commit/b4c9364a822bda9f6a85537deddf8056b75788f3))
- remove unused code
  ([0ad335c](https://github.com/grafana/loki-release/commit/0ad335cf7b13c6cb374d85ec05d127300c01edba))
- run create release on release branches
  ([c8ba75f](https://github.com/grafana/loki-release/commit/c8ba75ffe27b6288de7b048b716173a131352ddc))
- skip steps not jobs
  ([de6deb3](https://github.com/grafana/loki-release/commit/de6deb38dc877630ad77c70b0176e679509f9308))
- store sha in footer
  ([983a9d6](https://github.com/grafana/loki-release/commit/983a9d6dfa14bc53e0f306d46aa390f36a676f7c))
- try a merge to main w/ backport strategy
  ([cf996f4](https://github.com/grafana/loki-release/commit/cf996f4cb2366df03c668af2b572f845c904e7ac))
- try using release-please for release again
  ([3ca6579](https://github.com/grafana/loki-release/commit/3ca6579cb00cde5843021c5ccd99c83139db54ed))

### Bug Fixes

- add actual dependency to fake go program
  ([f9bed84](https://github.com/grafana/loki-release/commit/f9bed846d0377edcc5347a58ea564e3cdbe7619f))
- fix step reference
  ([2fdb566](https://github.com/grafana/loki-release/commit/2fdb56609031e06c252ba026b072f84546b1abe0))
- go.mod file reference
  ([1bde062](https://github.com/grafana/loki-release/commit/1bde0628c59950dc3169984c7492728b5a0a85a0))
- image conditional
  ([966a394](https://github.com/grafana/loki-release/commit/966a394de4b4a67b41e36010840a9b465753d526))

### Miscellaneous Chores

- release 1.6.0
  ([623481c](https://github.com/grafana/loki-release/commit/623481cf6788df9495affd69b292973fcbc16e6e))

## [1.6.0](https://github.com/grafana/loki-release/compare/v1.6.0...v1.6.0) (2024-01-19)

### Features

- add a go.mod and go file
  ([3713672](https://github.com/grafana/loki-release/commit/3713672ba00937015fb97fcc1efb26acfe5e5a7b))
- add artifacts to release
  ([4fea492](https://github.com/grafana/loki-release/commit/4fea4927fe360ce4031fa0553f6536d8fd980d17))
- add backport action
  ([4df43c6](https://github.com/grafana/loki-release/commit/4df43c665e46daa36fca0b9be0932b2393ebb5c7))
- add correct updaters to release pull request
  ([d50db7a](https://github.com/grafana/loki-release/commit/d50db7a6ce579a8b21c0f84b3767eb6f9c24f9dc))
- add create release step
  ([fe8c2fb](https://github.com/grafana/loki-release/commit/fe8c2fbe3d6bd7617226b6e7e9f5abdd77aec483))
- add install binary action
  ([947ed95](https://github.com/grafana/loki-release/commit/947ed95bf340634e24bfc316eda4f20d356190de))
- add more functionality from release please
  ([6c871fc](https://github.com/grafana/loki-release/commit/6c871fc3368e4eece45c7fa807e1831164f4debe))
- add release steps to jsonnet-build workflow
  ([55a14d6](https://github.com/grafana/loki-release/commit/55a14d67b6cdbda880abe16ed3cd1db969714b1c))
- added github interactions to release plugin
  ([808c34a](https://github.com/grafana/loki-release/commit/808c34aa4bc81a523683b2b345eccff75e628e2f))
- bring back all steps
  ([ab86186](https://github.com/grafana/loki-release/commit/ab86186caf0e7218e9be1fd7a84df58545c08517))
- build images for multiple platforms
  ([49a846e](https://github.com/grafana/loki-release/commit/49a846e2da75e56cd22fd4bbadb2469919afed2e))
- build pipeline using jsonnet for reuse
  ([b6cc287](https://github.com/grafana/loki-release/commit/b6cc2876ac3a593ede5644ca2e5a3bbec5572837))
- **ci:** add release-please action
  ([b994e1b](https://github.com/grafana/loki-release/commit/b994e1bb5a36e7f6e1f0134a1ea104143d0bce3f))
- **ci:** bump minor for k branches
  ([44d573d](https://github.com/grafana/loki-release/commit/44d573d107dd71ae26e2884a8d5e75c2e7a6d76f))
- **ci:** fix default-branch
  ([fe48dc3](https://github.com/grafana/loki-release/commit/fe48dc34c4e9cbfc42d5afff5ad79c0b1daf464a))
- **ci:** try to move versioning into workflow definition
  ([d66d51a](https://github.com/grafana/loki-release/commit/d66d51a562d6384e2966acd1cbf3755b99ff93a4))
- create release branch from k release
  ([07f2b06](https://github.com/grafana/loki-release/commit/07f2b064a9a0234a0cfe87cf390bb6f055dff967))
- create release first
  ([e2d4e73](https://github.com/grafana/loki-release/commit/e2d4e7318ec2f581296b5341363698c222352536))
- exclude component from tag for better release notes
  ([9841d98](https://github.com/grafana/loki-release/commit/9841d98bbfefd2a1d972c4bb81f5a4d6bcffc5e7))
- first try at storing build artifacts in GCS
  ([8801d68](https://github.com/grafana/loki-release/commit/8801d686e7b4084bb8e82f5776c8a7148fa219a5))
- fix typo in versioing-strategy
  ([5a47a62](https://github.com/grafana/loki-release/commit/5a47a62cdea90bbf21cefd8085eaf8b47650bd51))
- fix versioning strategy
  ([0008487](https://github.com/grafana/loki-release/commit/0008487cad2fe5e54fdacde3ff0b2724c21db979))
- make it a dry run
  ([4d63549](https://github.com/grafana/loki-release/commit/4d63549df4170dc67b4fe6a31175693504bab47a))
- make workflow reusable
  ([c01b721](https://github.com/grafana/loki-release/commit/c01b7213100dca261ddf9cad255cf4428bebd8a7))
- nest workflows in folder
  ([2eab631](https://github.com/grafana/loki-release/commit/2eab6317c6381b2827dac7409bfd8dfcaf96f4eb))
- output created/updated PR
  ([3d76523](https://github.com/grafana/loki-release/commit/3d76523376309db2e95d8f05716aa0c3d1b228e7))
- put images in different subfolder in bucket
  ([b4c9364](https://github.com/grafana/loki-release/commit/b4c9364a822bda9f6a85537deddf8056b75788f3))
- remove unused code
  ([0ad335c](https://github.com/grafana/loki-release/commit/0ad335cf7b13c6cb374d85ec05d127300c01edba))
- run create release on release branches
  ([c8ba75f](https://github.com/grafana/loki-release/commit/c8ba75ffe27b6288de7b048b716173a131352ddc))
- skip steps not jobs
  ([de6deb3](https://github.com/grafana/loki-release/commit/de6deb38dc877630ad77c70b0176e679509f9308))
- try a merge to main w/ backport strategy
  ([cf996f4](https://github.com/grafana/loki-release/commit/cf996f4cb2366df03c668af2b572f845c904e7ac))
- try using release-please for release again
  ([3ca6579](https://github.com/grafana/loki-release/commit/3ca6579cb00cde5843021c5ccd99c83139db54ed))

### Bug Fixes

- add actual dependency to fake go program
  ([f9bed84](https://github.com/grafana/loki-release/commit/f9bed846d0377edcc5347a58ea564e3cdbe7619f))
- go.mod file reference
  ([1bde062](https://github.com/grafana/loki-release/commit/1bde0628c59950dc3169984c7492728b5a0a85a0))
- image conditional
  ([966a394](https://github.com/grafana/loki-release/commit/966a394de4b4a67b41e36010840a9b465753d526))

### Miscellaneous Chores

- release 1.6.0
  ([623481c](https://github.com/grafana/loki-release/commit/623481cf6788df9495affd69b292973fcbc16e6e))

## [1.5.3](https://github.com/grafana/loki-release/compare/v1.5.2...v1.5.3) (2024-01-19)

### Features

- add GPG key and passphrase
  ([32cc695](https://github.com/grafana/loki-release/commit/32cc69564c89428681420c50842025be7e084d94))
- change GH token
  ([1d84a20](https://github.com/grafana/loki-release/commit/1d84a20011a4b4e3e26433dc1e1553498287affa))
- conditionally create tag
  ([a37a955](https://github.com/grafana/loki-release/commit/a37a9556c8a9cd97b05a1320f803ca94fbf556bf))
- create release tag
  ([c2f799b](https://github.com/grafana/loki-release/commit/c2f799b9d9fb528b46826da521ec3a3ecdfa279e))
- create release w/ release-please, remove lokiStep
  ([21a070f](https://github.com/grafana/loki-release/commit/21a070ff9f104fd110db3ac6aafaa96a8b8cab79))
- fix email/gpg mismatch
  ([594ff7b](https://github.com/grafana/loki-release/commit/594ff7b6c1a31d68ec64a070d390fcc6a45b481a))
- get version from PR title
  ([7745394](https://github.com/grafana/loki-release/commit/7745394c1e69bdb3552a7c66cb36ce51fd1ed5df))
- give release write-all permission
  ([27d4a81](https://github.com/grafana/loki-release/commit/27d4a81a78c3c8615327fe365e244fe8d559b9c7))
- give workflow write-all permission
  ([db91e71](https://github.com/grafana/loki-release/commit/db91e71b5b4092026031d1c126364fa25e3dd1fe))
- guard creating release and uploading artifacts
  ([243f2b9](https://github.com/grafana/loki-release/commit/243f2b9496d7d0a5de7a4a0ad1f1fc1c94655afb))
- keep release a draft until assets are uploaded
  ([e133e94](https://github.com/grafana/loki-release/commit/e133e94a425dec6f807787f3c316203fa43a3c41))
- manually setup github user
  ([4661ec0](https://github.com/grafana/loki-release/commit/4661ec023db3a3269d48d11e20e864655b71cd14))
- switch create tag actions
  ([c239916](https://github.com/grafana/loki-release/commit/c239916786e9052d515944d924f0336d50d3aaee))
- try create tag action
  ([3415973](https://github.com/grafana/loki-release/commit/3415973ed819021565ffbf16f2737947c7dc501c))
- try creating release again
  ([5500b1b](https://github.com/grafana/loki-release/commit/5500b1b25a711edec56dd368a917499647916297))
- try upload again
  ([ee86be7](https://github.com/grafana/loki-release/commit/ee86be793aa70c4c709e7f3355e7c7a49055ae8b))
- update GPG action
  ([9f196f1](https://github.com/grafana/loki-release/commit/9f196f10d9d5ec8766df7e401e32b0181ec98326))
- use gh to create release
  ([2ce1492](https://github.com/grafana/loki-release/commit/2ce1492254e3eb31b0acdf4762501f390de84bb0))
- why is this the hard part?
  ([ad1862c](https://github.com/grafana/loki-release/commit/ad1862c71582e1bbc3cb7b5d46aa958115602fe4))

### Bug Fixes

- args set via env
  ([8e3d65c](https://github.com/grafana/loki-release/commit/8e3d65c1f0ce129c237ff29f268d0e04c35dcd48))
- building of action
  ([a3b0662](https://github.com/grafana/loki-release/commit/a3b0662853b113d359d25a5ad4f496b96ba374ca))
- need to be in a repo to tag
  ([9e6705e](https://github.com/grafana/loki-release/commit/9e6705ed7161cae9b62d19d01126b4f919420276))
- set GH_TOKEN
  ([169b01a](https://github.com/grafana/loki-release/commit/169b01a2883eae5031f60e651d6a89ae852aa9b2))
- set source dir for create tag action
  ([3a93adb](https://github.com/grafana/loki-release/commit/3a93adbfe3aba0e28349753b60b91e488ed4766c))

## [1.5.2](https://github.com/grafana/loki-release/compare/v1.5.1...v1.5.2) (2024-01-18)

### Features

- conditionally create release
  ([ea53455](https://github.com/grafana/loki-release/commit/ea534553427abe4f568ce1c49e0381e1ef3d1b0f))
- custom action just for prepping the release
  ([2e14c0e](https://github.com/grafana/loki-release/commit/2e14c0efc60a0fe72bf92528f047401a7c6c1879))

### Bug Fixes

- build the correct action
  ([81e90e6](https://github.com/grafana/loki-release/commit/81e90e63a7c4233f49a9641f8feb2a221fc010bf))
- compile action
  ([f763ca5](https://github.com/grafana/loki-release/commit/f763ca590126d255e5c36dc28a7c3bb7d5a936fa))
- convert json boolean
  ([7fe0489](https://github.com/grafana/loki-release/commit/7fe0489181f85d6c745e8f201848c259bc73e98e))

## [1.5.1](https://github.com/grafana/loki-release/compare/v1.5.0...v1.5.1) (2024-01-18)

### Features

- create release first
  ([07cd4b3](https://github.com/grafana/loki-release/commit/07cd4b3087aa1e20f61912616a1437bddc82f4ba))
- release from release branch
  ([98f71c5](https://github.com/grafana/loki-release/commit/98f71c53a5290825e604fc20633bf5592cd95e89))
- return to using native release-pleae action
  ([2bbbfd4](https://github.com/grafana/loki-release/commit/2bbbfd4b49ca44cfc9fa0c2e655ec77184f25862))
- use correct target branch in release
  ([d72b0f7](https://github.com/grafana/loki-release/commit/d72b0f72b0144fcb234ae9bea8d678dc7e34b732))

### Bug Fixes

- pull both repos for release action
  ([043dd18](https://github.com/grafana/loki-release/commit/043dd18feefb8d9f611843343e16b179aa0d01d5))

## [1.5.0](https://github.com/grafana/loki-release/compare/v1.5.0...v1.5.0) (2024-01-18)

### Features

- add artifacts to release
  ([4fea492](https://github.com/grafana/loki-release/commit/4fea4927fe360ce4031fa0553f6536d8fd980d17))
- add backport action
  ([4df43c6](https://github.com/grafana/loki-release/commit/4df43c665e46daa36fca0b9be0932b2393ebb5c7))
- add correct updaters to release pull request
  ([d50db7a](https://github.com/grafana/loki-release/commit/d50db7a6ce579a8b21c0f84b3767eb6f9c24f9dc))
- add create release step
  ([fe8c2fb](https://github.com/grafana/loki-release/commit/fe8c2fbe3d6bd7617226b6e7e9f5abdd77aec483))
- add install binary action
  ([947ed95](https://github.com/grafana/loki-release/commit/947ed95bf340634e24bfc316eda4f20d356190de))
- add more functionality from release please
  ([6c871fc](https://github.com/grafana/loki-release/commit/6c871fc3368e4eece45c7fa807e1831164f4debe))
- add release please manifest back
  ([23b8935](https://github.com/grafana/loki-release/commit/23b8935189892e86516a930f4aa36611ea0258d3))
- add release steps to jsonnet-build workflow
  ([55a14d6](https://github.com/grafana/loki-release/commit/55a14d67b6cdbda880abe16ed3cd1db969714b1c))
- added github interactions to release plugin
  ([808c34a](https://github.com/grafana/loki-release/commit/808c34aa4bc81a523683b2b345eccff75e628e2f))
- always bump patch
  ([1893c6f](https://github.com/grafana/loki-release/commit/1893c6f4ec255720fe57dafd451caac497dc0200))
- build images for multiple platforms
  ([49a846e](https://github.com/grafana/loki-release/commit/49a846e2da75e56cd22fd4bbadb2469919afed2e))
- build pipeline using jsonnet for reuse
  ([b6cc287](https://github.com/grafana/loki-release/commit/b6cc2876ac3a593ede5644ca2e5a3bbec5572837))
- **ci:** add release-please action
  ([b994e1b](https://github.com/grafana/loki-release/commit/b994e1bb5a36e7f6e1f0134a1ea104143d0bce3f))
- **ci:** bump minor for k branches
  ([44d573d](https://github.com/grafana/loki-release/commit/44d573d107dd71ae26e2884a8d5e75c2e7a6d76f))
- **ci:** fix default-branch
  ([fe48dc3](https://github.com/grafana/loki-release/commit/fe48dc34c4e9cbfc42d5afff5ad79c0b1daf464a))
- **ci:** try to move versioning into workflow definition
  ([d66d51a](https://github.com/grafana/loki-release/commit/d66d51a562d6384e2966acd1cbf3755b99ff93a4))
- create release branch from k release
  ([07f2b06](https://github.com/grafana/loki-release/commit/07f2b064a9a0234a0cfe87cf390bb6f055dff967))
- exclude component from tag for better release notes
  ([9841d98](https://github.com/grafana/loki-release/commit/9841d98bbfefd2a1d972c4bb81f5a4d6bcffc5e7))
- first try at storing build artifacts in GCS
  ([8801d68](https://github.com/grafana/loki-release/commit/8801d686e7b4084bb8e82f5776c8a7148fa219a5))
- fix typo in versioing-strategy
  ([5a47a62](https://github.com/grafana/loki-release/commit/5a47a62cdea90bbf21cefd8085eaf8b47650bd51))
- fix versioning strategy
  ([0008487](https://github.com/grafana/loki-release/commit/0008487cad2fe5e54fdacde3ff0b2724c21db979))
- make workflow reusable
  ([c01b721](https://github.com/grafana/loki-release/commit/c01b7213100dca261ddf9cad255cf4428bebd8a7))
- nest workflows in folder
  ([2eab631](https://github.com/grafana/loki-release/commit/2eab6317c6381b2827dac7409bfd8dfcaf96f4eb))
- output created/updated PR
  ([3d76523](https://github.com/grafana/loki-release/commit/3d76523376309db2e95d8f05716aa0c3d1b228e7))
- try a merge to main w/ backport strategy
  ([cf996f4](https://github.com/grafana/loki-release/commit/cf996f4cb2366df03c668af2b572f845c904e7ac))
- try releasing into stable release branch
  ([d882d58](https://github.com/grafana/loki-release/commit/d882d585c6967a5fe698db3490c189b738edcbf6))

### Bug Fixes

- copy changelog step
  ([1eb022f](https://github.com/grafana/loki-release/commit/1eb022fabfadf2d3bfc359d4af2d58ffb5f91a19))
- label arg
  ([24d3e8b](https://github.com/grafana/loki-release/commit/24d3e8bb382f929ff2327950d76c5a6d70a54556))

### Miscellaneous Chores

- release 1.5.0
  ([abf1705](https://github.com/grafana/loki-release/commit/abf1705c254dc65b984763e01f8f9a47eaad34aa))

## [1.2.0](https://github.com/grafana/loki-release/compare/v1.1.3...v1.2.0) (2023-12-01)

### Features

- add backport action
  ([4df43c6](https://github.com/grafana/loki-release/commit/4df43c665e46daa36fca0b9be0932b2393ebb5c7))
- add install binary action
  ([947ed95](https://github.com/grafana/loki-release/commit/947ed95bf340634e24bfc316eda4f20d356190de))
- add release steps to jsonnet-build workflow
  ([55a14d6](https://github.com/grafana/loki-release/commit/55a14d67b6cdbda880abe16ed3cd1db969714b1c))
- build images for multiple platforms
  ([49a846e](https://github.com/grafana/loki-release/commit/49a846e2da75e56cd22fd4bbadb2469919afed2e))
- build pipeline using jsonnet for reuse
  ([b6cc287](https://github.com/grafana/loki-release/commit/b6cc2876ac3a593ede5644ca2e5a3bbec5572837))
- **ci:** bump minor for k branches
  ([44d573d](https://github.com/grafana/loki-release/commit/44d573d107dd71ae26e2884a8d5e75c2e7a6d76f))
- **ci:** try to move versioning into workflow definition
  ([d66d51a](https://github.com/grafana/loki-release/commit/d66d51a562d6384e2966acd1cbf3755b99ff93a4))
- create release branch from k release
  ([07f2b06](https://github.com/grafana/loki-release/commit/07f2b064a9a0234a0cfe87cf390bb6f055dff967))
- exclude component from tag for better release notes
  ([9841d98](https://github.com/grafana/loki-release/commit/9841d98bbfefd2a1d972c4bb81f5a4d6bcffc5e7))
- make workflow reusable
  ([c01b721](https://github.com/grafana/loki-release/commit/c01b7213100dca261ddf9cad255cf4428bebd8a7))
- nest workflows in folder
  ([2eab631](https://github.com/grafana/loki-release/commit/2eab6317c6381b2827dac7409bfd8dfcaf96f4eb))
- try a merge to main w/ backport strategy
  ([cf996f4](https://github.com/grafana/loki-release/commit/cf996f4cb2366df03c668af2b572f845c904e7ac))

## [1.1.3](https://github.com/grafana/loki-release/compare/v1.1.2...v1.1.3) (2023-11-22)

### Features

- add artifacts to release
  ([4fea492](https://github.com/grafana/loki-release/commit/4fea4927fe360ce4031fa0553f6536d8fd980d17))
- **ci:** add release-please action
  ([b994e1b](https://github.com/grafana/loki-release/commit/b994e1bb5a36e7f6e1f0134a1ea104143d0bce3f))
- **ci:** fix default-branch
  ([fe48dc3](https://github.com/grafana/loki-release/commit/fe48dc34c4e9cbfc42d5afff5ad79c0b1daf464a))
- fix typo in versioing-strategy
  ([5a47a62](https://github.com/grafana/loki-release/commit/5a47a62cdea90bbf21cefd8085eaf8b47650bd51))
- fix versioning strategy
  ([0008487](https://github.com/grafana/loki-release/commit/0008487cad2fe5e54fdacde3ff0b2724c21db979))

## [1.1.2](https://github.com/grafana/loki-release/compare/v1.1.1...v1.1.2) (2023-11-22)

### Features

- add artifacts to release
  ([4fea492](https://github.com/grafana/loki-release/commit/4fea4927fe360ce4031fa0553f6536d8fd980d17))
- **ci:** add release-please action
  ([b994e1b](https://github.com/grafana/loki-release/commit/b994e1bb5a36e7f6e1f0134a1ea104143d0bce3f))
- **ci:** fix default-branch
  ([fe48dc3](https://github.com/grafana/loki-release/commit/fe48dc34c4e9cbfc42d5afff5ad79c0b1daf464a))
- fix typo in versioing-strategy
  ([5a47a62](https://github.com/grafana/loki-release/commit/5a47a62cdea90bbf21cefd8085eaf8b47650bd51))
- fix versioning strategy
  ([0008487](https://github.com/grafana/loki-release/commit/0008487cad2fe5e54fdacde3ff0b2724c21db979))

## [1.1.1](https://github.com/grafana/loki-release/compare/v1.1.0...v1.1.1) (2023-11-22)

### Features

- add artifacts to release
  ([4fea492](https://github.com/grafana/loki-release/commit/4fea4927fe360ce4031fa0553f6536d8fd980d17))
- **ci:** add release-please action
  ([b994e1b](https://github.com/grafana/loki-release/commit/b994e1bb5a36e7f6e1f0134a1ea104143d0bce3f))
- **ci:** fix default-branch
  ([fe48dc3](https://github.com/grafana/loki-release/commit/fe48dc34c4e9cbfc42d5afff5ad79c0b1daf464a))
- fix typo in versioing-strategy
  ([5a47a62](https://github.com/grafana/loki-release/commit/5a47a62cdea90bbf21cefd8085eaf8b47650bd51))
- fix versioning strategy
  ([0008487](https://github.com/grafana/loki-release/commit/0008487cad2fe5e54fdacde3ff0b2724c21db979))
