# Pinned NPM template inputs

These files were extracted unchanged from Nginx Proxy Manager 2.13.5 at the exact image identity recorded in notes/npm-upstreams-compatibility.json. Their paths and hashes are checked before applying the narrow adapter in upstream_config.go. They are not complete replacement host configurations.

Upstream sources:
- https://github.com/NginxProxyManager/nginx-proxy-manager/tree/v2.13.5/backend/templates
- https://github.com/NginxProxyManager/nginx-proxy-manager/blob/v2.13.5/docker/rootfs/etc/nginx/conf.d/include/proxy.conf

Retain upstream licensing when redistributing these NPM-derived inputs. Any image/template change requires a fresh disposable compatibility run before expanding the supported identity.
