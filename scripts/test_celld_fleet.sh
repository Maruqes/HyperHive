#!/usr/bin/env bash
set -euo pipefail

# Disposable SILO and real celld nodes; never start the privileged HyperHive mains.
repo="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
container="hyperhive-celld-test-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; rm -rf "$work"; }
trap cleanup EXIT

curl -fsSL https://celld.dev/install.sh -o "$work/install.sh"
CELLD_VERSION=v0.6.2 CELLD_INSTALL_ROOT="$work/runtime" sh "$work/install.sh"
npm install --prefix "$work/tools" --no-audit --no-fund esbuild@0.25.12

docker run -d --name "$container" --rm -p 127.0.0.1::9000 \
  -e MINIO_ROOT_USER=hyperhive-test-admin \
  -e MINIO_ROOT_PASSWORD=hyperhive-test-admin-password \
  --tmpfs /data:rw,size=1g \
  docker.io/pgsty/silo:RELEASE.2026-09-03T13-18-01Z server /data --console-address :9001
endpoint="http://$(docker port "$container" 9000/tcp)"
for attempt in {1..60}; do
  if curl -fsS "$endpoint/minio/health/ready" >/dev/null; then break; fi
  if [[ "$attempt" == 60 ]]; then echo "SILO did not become ready" >&2; exit 1; fi
  sleep 1
done
mcli() { docker exec -i "$container" mcli "$@"; }
mcli alias set local http://127.0.0.1:9000 hyperhive-test-admin hyperhive-test-admin-password
mcli mb local/hyperhive-celld
mcli admin policy create local hyperhive-celld /dev/stdin <<'POLICY'
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::hyperhive-celld","arn:aws:s3:::hyperhive-celld/*"]}]}
POLICY
mcli admin user add local celld-test celld-test-secret
mcli admin policy attach local hyperhive-celld --user celld-test

export CELLD_TEST_ENDPOINT="$endpoint"
export CELLD_TEST_BINARY="$work/runtime/bin/celld"
export AWS_ACCESS_KEY_ID=celld-test AWS_SECRET_ACCESS_KEY=celld-test-secret
export PATH="$work/tools/node_modules/.bin:$PATH"

(cd "$repo/master" && go test ./services -run TestSiloCompatibilityIntegration -count=1 -v)
"$CELLD_TEST_BINARY" deploy "$repo/master/services/celld_bootstrap" \
  --bucket s3://hyperhive-celld --endpoint "$endpoint" --region us-east-1
(cd "$repo/master" && go test ./services -run TestSiloCompatibilityIntegration -count=1 -v)
(cd "$repo/slave" && go test ./celld -run TestFleetIntegration -count=1 -v -timeout 8m)
