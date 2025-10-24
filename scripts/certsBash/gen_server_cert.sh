#!/usr/bin/env bash
set -euo pipefail

# ---------------- helpers ----------------
ask() { local p="$1" d="${2-}"; local a; read -r -p "$p ${d:+[$d]}: " a; echo "${a:-$d}"; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing '$1'." >&2; exit 1; }; }
parse_days() {
  local in="$1"
  shopt -s nocasematch
  if [[ "$in" =~ ^inf(inite)?$ ]]; then echo 36500; shopt -u nocasematch; return; fi # ~100y
  shopt -u nocasematch
  [[ "$in" =~ ^[0-9]+$ ]] || { echo "Invalid days: $in" >&2; exit 1; }
  echo "$in"
}
sha256_hex_of_der() {
  if command -v sha256sum >/dev/null 2>&1; then
    openssl x509 -in "$1" -outform der | sha256sum | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    openssl x509 -in "$1" -outform der | shasum -a 256 | awk '{print $1}'
  else
    openssl x509 -in "$1" -outform der | openssl dgst -sha256 | awk '{print $2}'
  fi
}
trim_csv() { # normalize comma lists (remove extra spaces/commas)
  awk -v RS=',' -v ORS=',' '{gsub(/^[ \t\r\n]+|[ \t\r\n]+$/,""); if(length) printf "%s",$0 ","}' <<<"$1" | sed 's/,\{1,\}$//'
}

need openssl

echo "==> TLS BOOTSTRAP (SERVER): create a CA (if missing) + a server certificate with DNS/IP SANs."
OUT_DIR="$(ask 'Output directory' 'tls')"
mkdir -p "$OUT_DIR"
cd "$OUT_DIR"

echo
echo "📌 Certificate Authority (CA)"
use_existing="$(ask 'Reuse an existing CA? (y/n)' 'y')"
if [[ "$use_existing" =~ ^[Yy]$ ]]; then
  CA_CRT="$(ask 'Path to ca.crt' 'ca.crt')"
  CA_KEY="$(ask 'Path to ca.key' 'ca.key')"
  [[ -f "$CA_CRT" && -f "$CA_KEY" ]] || { echo 'CA files not found.' >&2; exit 1; }
  # if paths are outside OUT_DIR, copy for convenience
  [[ "$CA_CRT" != "ca.crt" ]] && cp "$CA_CRT" ca.crt
  [[ "$CA_KEY" != "ca.key" ]] && cp "$CA_KEY" ca.key && chmod 600 ca.key
else
  echo "   Example CA CN: My Private CA"
  CA_CN="$(ask 'CA Common Name (CN)' 'My Private CA')"
  echo "   Validity: number of days (e.g. 3650) or 'inf' (~100 years)"
  CA_D_IN="$(ask 'CA validity (days/inf)' '3650')"; CA_D="$(parse_days "$CA_D_IN")"
  openssl genpkey -algorithm Ed25519 -out ca.key
  openssl req -x509 -new -key ca.key -sha256 -days "$CA_D" \
    -subj "/CN=${CA_CN}" -out ca.crt
  chmod 600 ca.key
fi

echo
echo "🔐 Server key algorithm (RSA = widest compatibility; Ed25519 = modern & small)"
ALG="$(ask 'Choose key type (rsa/ed25519)' 'ed25519')"
if [[ "$ALG" == "rsa" ]]; then
  RSA_BITS="$(ask 'RSA key size (2048/3072/4096)' '4096')"
fi

echo
echo "🌐 Subject Alternative Name (SAN) — MUST include the DNS/IP clients will dial."
MODE="$(ask 'SAN mode (dns/ip/both)' 'both')"
DNS_LIST=""; IP_LIST=""
if [[ "$MODE" == "dns" || "$MODE" == "both" ]]; then
  echo "   Example: api.example.com,internal.example,hyperhive.local"
  DNS_LIST="$(ask 'Comma-separated DNS names' 'api.example.com,hyperhive.local')"
  DNS_LIST="$(trim_csv "$DNS_LIST")"
fi
if [[ "$MODE" == "ip" || "$MODE" == "both" ]]; then
  echo "   Example: 10.0.0.5,127.0.0.1,::1"
  IP_LIST="$(ask 'Comma-separated IPs' '127.0.0.1,::1')"
  IP_LIST="$(trim_csv "$IP_LIST")"
fi

DEFAULT_CN="${DNS_LIST%%,*}"; [[ -z "$DEFAULT_CN" ]] && DEFAULT_CN="${IP_LIST%%,*}"
echo
echo "🪪 Server CN (label only; SAN is what’s verified)"
SERVER_CN="$(ask 'Server CN' "${DEFAULT_CN:-grpc-server}")"
PREFIX="$(ask 'Filename prefix (no extension)' 'server')"
echo "   Validity for the SERVER certificate (days or 'inf')"
SRV_D_IN="$(ask 'Server cert validity (days/inf)' '825')"; SRV_D="$(parse_days "$SRV_D_IN")"

# Build SAN string for OpenSSL
SAN=""
IFS=',' read -ra DARR <<< "${DNS_LIST:-}"; for d in "${DARR[@]:-}"; do [[ -n "$d" ]] && SAN+="DNS:${d},"; done
IFS=',' read -ra IARR <<< "${IP_LIST:-}";  for i in "${IARR[@]:-}"; do [[ -n "$i" ]] && SAN+="IP:${i},";  done
SAN="${SAN%,}"
[[ -z "$SAN" ]] && { echo "You must provide at least one DNS or IP in SAN." >&2; exit 1; }

KEY="${PREFIX}.key"; CSR="${PREFIX}.csr"; CRT="${PREFIX}.crt"; EXT="${PREFIX}.ext"

echo
echo "🔧 Generating server key + CSR…"
if [[ "$ALG" == "rsa" ]]; then
  openssl genrsa -out "$KEY" "$RSA_BITS"
else
  openssl genpkey -algorithm Ed25519 -out "$KEY"
fi
chmod 600 "$KEY"
openssl req -new -key "$KEY" -subj "/CN=${SERVER_CN}" -out "$CSR"

cat > "$EXT" <<EOF
subjectAltName=${SAN}
basicConstraints=CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
EOF

echo "✍️  Signing server certificate with CA (${SRV_D} days)…"
openssl x509 -req -in "$CSR" -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out "$CRT" -days "$SRV_D" -sha256 -extfile "$EXT"

FP="$(sha256_hex_of_der ca.crt)"
echo "$FP" > ca.sha256

echo
echo "✅ Done. Files in $(pwd):"
printf " - %s\n" ca.crt ca.key "$CRT" "$KEY" ca.sha256
echo
echo "🔎 CA SHA-256 fingerprint: $FP"
echo
echo "📤 Serve the CA publicly (read-only). Examples:"
echo "   python3 -m http.server 8080   # then CA URL is http://<host>:8080/ca.crt"
echo "   (Clients can verify with the fingerprint above.)"
