# Verified NPM adapter templates

These pristine files were extracted from the NPM image identified by `UpstreamImageDigest` in `upstream_config.go`. The desired release is defined only by `image` in `npm.go`; `notes/npm-upstreams-compatibility.json` records the verified tag, image config ID, registry digest and local test evidence.

The Go adapter applies only the reserved managed-target port omission and backend TLS policy. Runtime image identity, template bytes and include layout are checked before mutations. The common proxy include alone permits trailing LF variation introduced by NPM startup.

Retain upstream licensing when redistributing these NPM-derived inputs. Every future image/template change requires a fresh disposable compatibility run. Upstream source: https://github.com/NginxProxyManager/nginx-proxy-manager/tree/v2.16.0
