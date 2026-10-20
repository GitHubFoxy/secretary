#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
for installer in install-linux-binary.sh install-macos-binary.sh; do
  count="$(grep -Fc 'curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors' "$root/scripts/$installer")"
  [[ "$count" -eq 3 ]] || { echo "$installer lacks retry flags for all three GitHub downloads" >&2; exit 1; }
done

stage="$(mktemp -d)"
server_pid=""
cleanup() {
  [[ -z "$server_pid" ]] || { kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; }
  rm -rf "$stage"
}
trap cleanup EXIT
cat > "$stage/openssl.cnf" <<'CFG'
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = localhost
[ext]
subjectAltName = DNS:localhost
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
CFG
openssl req -new -x509 -nodes -newkey rsa:2048 -days 1 -keyout "$stage/key.pem" -out "$stage/cert.pem" -config "$stage/openssl.cnf" >/dev/null 2>&1

python3 - "$stage" <<'PY' &
import pathlib, socket, ssl, sys
root = pathlib.Path(sys.argv[1])
tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
tls.load_cert_chain(str(root / "cert.pem"), str(root / "key.pem"))
server = socket.socket()
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
server.bind(("127.0.0.1", 0))
server.listen(4)
server.settimeout(20)
(root / "port").write_text(str(server.getsockname()[1]))
try:
    conn, _ = server.accept()
    conn.close()
    conn, _ = server.accept()
    with tls.wrap_socket(conn, server_side=True) as secure:
        secure.settimeout(10)
        secure.recv(8192)
        data = b"retry-ok"
        secure.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 8\r\nConnection: close\r\n\r\n" + data)
    (root / "attempts").write_text("2")
finally:
    server.close()
PY
server_pid=$!
for attempt in {1..100}; do
  [[ -f "$stage/port" ]] && break
  kill -0 "$server_pid" 2>/dev/null || { echo "TLS fixture stopped before opening listener" >&2; exit 1; }
  sleep 0.1
done
[[ -f "$stage/port" ]] || { echo "TLS fixture did not start" >&2; exit 1; }
port="$(cat "$stage/port")"
curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors --noproxy '*' --resolve "localhost:$port:127.0.0.1" --cacert "$stage/cert.pem" "https://localhost:$port/" -o "$stage/output"
[[ "$(cat "$stage/output")" == retry-ok ]]
wait "$server_pid"
server_pid=""
[[ "$(cat "$stage/attempts")" == 2 ]]
echo "TLS handshake EOF retry contract passed"
