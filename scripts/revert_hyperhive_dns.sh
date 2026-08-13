#!/usr/bin/env bash
set -e

[[ ${EUID} -eq 0 ]] || { echo "Run with sudo: sudo $0" >&2; exit 1; }

rm -f /etc/systemd/resolved.conf.d/hyperhive.conf
systemctl restart systemd-resolved
