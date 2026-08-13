#!/usr/bin/env bash
set -e

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo: sudo $0" >&2; exit 1; }

DNSMASQ_CONF=/etc/dnsmasq.d/512rede-host.conf
DNSMASQ_BACKUP=/etc/hyperhive/512rede-host.conf.before-local-dns
DNSMASQ_UPSTREAM=/etc/hyperhive/dnsmasq-upstream.conf

mkdir -p /etc/hyperhive
[[ -f ${DNSMASQ_BACKUP} ]] || cp -a "${DNSMASQ_CONF}" "${DNSMASQ_BACKUP}"
grep -Fqx "conf-file=${DNSMASQ_UPSTREAM}" "${DNSMASQ_CONF}" || printf '\nconf-file=%s\n' "${DNSMASQ_UPSTREAM}" >> "${DNSMASQ_CONF}"
printf 'no-resolv\nserver=192.168.1.1\n' > "${DNSMASQ_UPSTREAM}"
dnsmasq --test --conf-file="${DNSMASQ_CONF}"
systemctl restart dnsmasq-512rede-host.service

mkdir -p /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNS=192.168.76.1\nDomains=~.\n' > /etc/systemd/resolved.conf.d/hyperhive.conf
systemctl reload-or-restart systemd-resolved
