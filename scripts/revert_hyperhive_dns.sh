#!/usr/bin/env bash
set -e

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo: sudo $0" >&2; exit 1; }

rm -f /etc/systemd/resolved.conf.d/hyperhive.conf
systemctl reload-or-restart systemd-resolved

DNSMASQ_CONF=/etc/dnsmasq.d/512rede-host.conf
DNSMASQ_BACKUP=/etc/hyperhive/512rede-host.conf.before-local-dns

if [[ -f ${DNSMASQ_BACKUP} ]]; then
  cp -a "${DNSMASQ_BACKUP}" "${DNSMASQ_CONF}"
  rm -f "${DNSMASQ_BACKUP}" /etc/hyperhive/dnsmasq-upstream.conf
  systemctl restart dnsmasq-512rede-host.service
fi
