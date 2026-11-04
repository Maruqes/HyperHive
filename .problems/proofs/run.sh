#!/usr/bin/env bash
# Runs the audit proofs through `go test -overlay`, so nothing is added to the modules.
# Each proof asserts the correct behaviour: FAIL means the bug is still present.
# Usage: bash .problems/proofs/run.sh
set -u -o pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

printf '{"Replace":{"%s":"%s"}}' "$root/master/services/zz_audit_test.go" "$here/audit_master_test.go" > "$work/master.json"
printf '{"Replace":{"%s":"%s"}}' "$root/slave/smartdisk/zz_audit_test.go" "$here/audit_slave_test.go" > "$work/slave.json"

show() { grep -E '^(--- |ok|FAIL|PASS)|zz_audit'; }
status=0

echo "=== master"
(cd "$root/master" && go test -tags libvirt_dlopen -overlay "$work/master.json" -run TestAudit -v ./services) 2>&1 | show || status=1

echo "=== slave"
(cd "$root/slave" && go test -tags libvirt_dlopen -overlay "$work/slave.json" -run TestAudit -v ./smartdisk) 2>&1 | show || status=1

exit "$status"
