#!/usr/bin/env bash
set -e

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo: sudo $0" >&2; exit 1; }

mkdir -p /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNS=192.168.76.1\nDomains=~.\n' > /etc/systemd/resolved.conf.d/hyperhive.conf
systemctl restart systemd-resolved
