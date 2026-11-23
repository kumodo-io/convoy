# Changelog

## [0.11.1](https://github.com/kumodo-io/convoy/compare/v0.11.0...v0.11.1) (2026-11-08)


### Bug Fixes

* kb-10 target namespace ([#113](https://github.com/kumodo-io/convoy/issues/113)) ([992ddb7](https://github.com/kumodo-io/convoy/commit/992ddb7fb4cd921467f4b502b85aa2447b27170f))
* kb-11 last healthy applied ([#114](https://github.com/kumodo-io/convoy/issues/114)) ([00a0065](https://github.com/kumodo-io/convoy/commit/00a0065a07c489985605584facaacbf62ca2dc96))
* kb-12 delete protected kinds ([#115](https://github.com/kumodo-io/convoy/issues/115)) ([f91d325](https://github.com/kumodo-io/convoy/commit/f91d325ab3d835a36888a70b9fab3d807377aa46))
* kb-13 health only entries ([#116](https://github.com/kumodo-io/convoy/issues/116)) ([3b25e22](https://github.com/kumodo-io/convoy/commit/3b25e22604f4ad2eb872512f21f6d99b5760705e))
* kb-14 keep anchor on unknown ([#118](https://github.com/kumodo-io/convoy/issues/118)) ([4ae0a15](https://github.com/kumodo-io/convoy/commit/4ae0a15fe60f3f1a2de1a83b9ef71da8d49c1ce9))
* kb-4 manual sync rollout ([#107](https://github.com/kumodo-io/convoy/issues/107)) ([306cd3a](https://github.com/kumodo-io/convoy/commit/306cd3af85d668f6675176af83884ea0c3d827f1))
* kb-5 observer shard routing ([#108](https://github.com/kumodo-io/convoy/issues/108)) ([38945e7](https://github.com/kumodo-io/convoy/commit/38945e786692bbadf04c14b0797bb34091c81887))
* kb-7 bound the wave timeout to the current render ([#109](https://github.com/kumodo-io/convoy/issues/109)) ([37b911f](https://github.com/kumodo-io/convoy/commit/37b911fbeb7c53d130ff4e95f50fb2659858660f))
* kb-8 gate waves on health of the current render ([#111](https://github.com/kumodo-io/convoy/issues/111)) ([1b4f2eb](https://github.com/kumodo-io/convoy/commit/1b4f2ebac09668ecd4a47215e7ebec728ba53ae5))
* kb-9 manual sync wake ([#112](https://github.com/kumodo-io/convoy/issues/112)) ([b8d6e0c](https://github.com/kumodo-io/convoy/commit/b8d6e0c3700af435df0c778ecb6741da9fbdeea8))
* observer inventory uid ([#117](https://github.com/kumodo-io/convoy/issues/117)) ([d4f4117](https://github.com/kumodo-io/convoy/commit/d4f4117b8db1767710c70d59d88929e3ad3bdc35))
* rollback without store ([#105](https://github.com/kumodo-io/convoy/issues/105)) ([98ea110](https://github.com/kumodo-io/convoy/commit/98ea110052bce536013355a91cb808d287df5fdf))

## [0.11.0](https://github.com/kumodo-io/convoy/compare/v0.10.0...v0.11.0) (2026-11-02)


### Features

* draft releases ([#102](https://github.com/kumodo-io/convoy/issues/102)) ([dcb5393](https://github.com/kumodo-io/convoy/commit/dcb5393e45450cfec8b1aba44c9181ffd784b02e))

## [0.10.0](https://github.com/kumodo-io/convoy/compare/v0.9.0...v0.10.0) (2026-11-02)


### Features

* deploy chart ([#100](https://github.com/kumodo-io/convoy/issues/100)) ([6c235db](https://github.com/kumodo-io/convoy/commit/6c235dbe609c5bbd4ec4325c778941c92da49f06))
* deploy kustomization ([#99](https://github.com/kumodo-io/convoy/issues/99)) ([472cfd5](https://github.com/kumodo-io/convoy/commit/472cfd5c5f7f8fc33af40457df7d030fbe576c5a))
* plugin release ([#101](https://github.com/kumodo-io/convoy/issues/101)) ([cb78ed5](https://github.com/kumodo-io/convoy/commit/cb78ed5518352095791aa223d52554a647d7007e))


### Miscellaneous

* docs followup ([#97](https://github.com/kumodo-io/convoy/issues/97)) ([c212ebc](https://github.com/kumodo-io/convoy/commit/c212ebc1b1e989e1f3e6e7a5296f78fda00c042f))

## [0.9.0](https://github.com/kumodo-io/convoy/compare/v0.8.1...v0.9.0) (2026-10-31)


### Features

* kustomize content type ([#67](https://github.com/kumodo-io/convoy/issues/67)) ([521a7c7](https://github.com/kumodo-io/convoy/commit/521a7c7ed774c979a7af8892bc0f66e61782e952))


### Bug Fixes

* **apiserver:** stop advertising protobuf for convoy API groups ([#91](https://github.com/kumodo-io/convoy/issues/91)) ([3c560c9](https://github.com/kumodo-io/convoy/commit/3c560c9115efa253de5772f1d10c287ec2fab76f))
* **deps:** update module github.com/go-git/go-git/v5 to v5.19.3 ([#77](https://github.com/kumodo-io/convoy/issues/77)) ([6f41a82](https://github.com/kumodo-io/convoy/commit/6f41a8232ef609efb5ec7d1a032c1d330af68798))
* **deps:** update module github.com/google/cel-go to v0.30.0 ([#85](https://github.com/kumodo-io/convoy/issues/85)) ([ae02194](https://github.com/kumodo-io/convoy/commit/ae021949ecbb3ae620b6f54a1fb07869ea463e01))


### Miscellaneous

* codegen inputs and cleanup ([#76](https://github.com/kumodo-io/convoy/issues/76)) ([08f8274](https://github.com/kumodo-io/convoy/commit/08f8274b02b51388b3479324545f853ec2872c58))
* delete renovate ([eedd072](https://github.com/kumodo-io/convoy/commit/eedd072e2e0c2f01530ffe6ee047d1a254e24701))
* **deps:** update github actions ([#82](https://github.com/kumodo-io/convoy/issues/82)) ([158fdda](https://github.com/kumodo-io/convoy/commit/158fdda7356aa157822500f1f1ea06daa4c58416))
* **deps:** update go toolchain ([#73](https://github.com/kumodo-io/convoy/issues/73)) ([0603116](https://github.com/kumodo-io/convoy/commit/0603116e22a6adabe7e6f21a9382b37196303d31))
* **deps:** update mise tools ([#84](https://github.com/kumodo-io/convoy/issues/84)) ([35a2350](https://github.com/kumodo-io/convoy/commit/35a23509e5a003f904565442e0ee670629f9d052))
* update renovate config ([#83](https://github.com/kumodo-io/convoy/issues/83)) ([1f335a7](https://github.com/kumodo-io/convoy/commit/1f335a73473ffaac6a555c24219eaf7ac294f834))
* update tooling and workflows ([#69](https://github.com/kumodo-io/convoy/issues/69)) ([b7b7c42](https://github.com/kumodo-io/convoy/commit/b7b7c42504caf915ee751ed9e9419f272ee64ad8))

## [0.8.1](https://github.com/kumodo-io/convoy/compare/v0.8.0...v0.8.1) (2026-08-22)


### Bug Fixes

* preserve persisted rollout ([#65](https://github.com/kumodo-io/convoy/issues/65)) ([fb25d5f](https://github.com/kumodo-io/convoy/commit/fb25d5f56ea6b6ad429095d9d26d8f15998b3846))

## [0.8.0](https://github.com/kumodo-io/convoy/compare/v0.7.0...v0.8.0) (2026-08-21)


### Features

* history and rollback ([#58](https://github.com/kumodo-io/convoy/issues/58)) ([9070ac9](https://github.com/kumodo-io/convoy/commit/9070ac942de8e42dc77d62620ae545b7bd308bfe))
* persistent renders ([#57](https://github.com/kumodo-io/convoy/issues/57)) ([8653db9](https://github.com/kumodo-io/convoy/commit/8653db9bb4ae205f9666fb0a93487a46be34f416))
* progressive rollout ([#59](https://github.com/kumodo-io/convoy/issues/59)) ([aeff09e](https://github.com/kumodo-io/convoy/commit/aeff09ee69515a538079de857e56e35ba7595397))
* sharding ([#56](https://github.com/kumodo-io/convoy/issues/56)) ([7f431f1](https://github.com/kumodo-io/convoy/commit/7f431f1b46c7ccc8bfb5464d4c6efd2619f81bf3))


### Bug Fixes

* status update lags ([#63](https://github.com/kumodo-io/convoy/issues/63)) ([8936304](https://github.com/kumodo-io/convoy/commit/8936304b5458c947501878a4f5a0dc20a4681965))


### Miscellaneous

* add release-please ([#60](https://github.com/kumodo-io/convoy/issues/60)) ([95d0918](https://github.com/kumodo-io/convoy/commit/95d0918a5d38e2318b2895e6c416253771ad40ce))
* **ci:** parallel image build ([#64](https://github.com/kumodo-io/convoy/issues/64)) ([47aca96](https://github.com/kumodo-io/convoy/commit/47aca960c15c83f1f9dbb44a5daabc7ab28c7688))
* replace make with mise ([#62](https://github.com/kumodo-io/convoy/issues/62)) ([10ea5ea](https://github.com/kumodo-io/convoy/commit/10ea5eaf8c6fd8b2d30bbd10fcc69d3cdd44e146))
