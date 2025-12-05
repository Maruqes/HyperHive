#!/usr/bin/env bash
set -euo pipefail

# HyperHive helper to uninstall any k3s server/agent bits from a host.
# It relies on the official k3s uninstall script when available and then
# scrubs the well-known files/directories we deploy via our automation.

require_root() {
	if [[ $EUID -ne 0 ]]; then
		echo "This script must be run as root." >&2
		exit 1
	fi
}

log() {
	printf '[k3s-uninstall] %s\n' "$*"
}

stop_service_if_exists() {
	local svc=$1
	if systemctl list-unit-files | grep -q "^${svc}.service"; then
		if systemctl is-active --quiet "$svc"; then
			log "Stopping $svc.service"
			systemctl stop "$svc" || true
		fi
		log "Disabling $svc.service"
		systemctl disable "$svc" || true
	fi
}

remove_path() {
	local path=$1
	if [[ -e $path || -L $path ]]; then
		rm -rf "$path"
		log "Removed $path"
	fi
}

cleanup_firewalld() {
	if ! command -v firewall-cmd >/dev/null 2>&1; then
		return
	fi

	if ! firewall-cmd --state >/dev/null 2>&1; then
		return
	fi

	local ports=("6443/tcp" "8472/udp")
	for port in "${ports[@]}"; do
		if firewall-cmd --permanent --query-port="$port" >/dev/null 2>&1; then
			firewall-cmd --permanent --remove-port="$port" || true
			log "Removed permanent firewall rule for $port"
		fi
		if firewall-cmd --query-port="$port" >/dev/null 2>&1; then
			firewall-cmd --remove-port="$port" || true
			log "Removed runtime firewall rule for $port"
		fi
	done

	firewall-cmd --reload >/dev/null 2>&1 || true
}

main() {
	require_root

	stop_service_if_exists k3s
	stop_service_if_exists k3s-agent

	if [[ -x /usr/local/bin/k3s-killall.sh ]]; then
		log "Running k3s-killall.sh"
		/usr/local/bin/k3s-killall.sh || true
	fi

	if [[ -x /usr/local/bin/k3s-uninstall.sh ]]; then
		log "Running k3s-uninstall.sh"
		/usr/local/bin/k3s-uninstall.sh || true
	fi

	if [[ -x /usr/local/bin/k3s-agent-uninstall.sh ]]; then
		log "Running k3s-agent-uninstall.sh"
		/usr/local/bin/k3s-agent-uninstall.sh || true
	fi

	# Remove known leftovers.
	remove_path /var/lib/rancher/k3s
	remove_path /etc/rancher/k3s
	remove_path /var/lib/kubelet
	remove_path /etc/systemd/system/k3s.service
	remove_path /etc/systemd/system/k3s.service.env
	remove_path /etc/systemd/system/k3s-agent.service
	remove_path /etc/systemd/system/k3s-agent.service.env
	remove_path /usr/local/bin/k3s
	remove_path /usr/local/bin/kubectl
	remove_path /usr/local/bin/crictl

	systemctl daemon-reload

	cleanup_firewalld

	log "k3s removal completed."
}

main "$@"
