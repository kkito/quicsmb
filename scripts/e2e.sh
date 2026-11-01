#!/bin/bash
# 本地回环 e2e：假 SMB 后端（回显）+ quicsmb server + client，经隧道发包验证。
# 用法：./scripts/e2e.sh
set -euo pipefail
cd "$(dirname "$0")/.."

BACKEND_PORT="${BACKEND_PORT:-4455}"
QUIC_PORT="${QUIC_PORT:-4433}"
LISTEN_PORT="${LISTEN_PORT:-8445}"
WEB_SRV="${WEB_SRV:-18081}"
WEB_CLI="${WEB_CLI:-18082}"

cleanup() {
  for p in ${SRV_PID:-} ${CLI_PID:-} ${BE_PID:-}; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT

echo "==> build"
go build -o /tmp/quicsmb-e2e ./cmd/quicsmb

echo "==> fake smb backend (echo) on :$BACKEND_PORT"
python3 -c "
import socket, threading
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', $BACKEND_PORT)); s.listen(16)
def h(c):
    try:
        while True:
            d = c.recv(65536)
            if not d: break
            c.sendall(d)
    except Exception: pass
    finally: c.close()
while True:
    c, _ = s.accept(); threading.Thread(target=h, args=(c,), daemon=True).start()
" &
BE_PID=$!

echo "==> server :$QUIC_PORT -> 127.0.0.1:$BACKEND_PORT"
cat > /tmp/qs-srv.json <<EOF
{"mode":"server","quic_port":$QUIC_PORT,"backend_host":"127.0.0.1","backend_port":$BACKEND_PORT,"cert_file":"","key_file":"","alpn":"quicsmb/1","idle_timeout_sec":300,"max_sessions":64,"web_addr":"127.0.0.1:$WEB_SRV"}
EOF
/tmp/quicsmb-e2e -c /tmp/qs-srv.json > /tmp/qs-srv.log 2>&1 &
SRV_PID=$!
sleep 1

echo "==> client :$LISTEN_PORT -> 127.0.0.1:$QUIC_PORT"
cat > /tmp/qs-cli.json <<EOF
{"mode":"client","server_host":"127.0.0.1","server_port":$QUIC_PORT,"listen_host":"127.0.0.1","listen_port":$LISTEN_PORT,"alpn":"quicsmb/1","insecure_skip_verify":true,"idle_timeout_sec":300,"max_sessions":64,"web_addr":"127.0.0.1:$WEB_CLI"}
EOF
/tmp/quicsmb-e2e -c /tmp/qs-cli.json > /tmp/qs-cli.log 2>&1 &
CLI_PID=$!
sleep 1

echo "==> roundtrip through tunnel"
python3 - "$LISTEN_PORT" <<'EOF'
import socket, sys
port = int(sys.argv[1])
payload = b"\x00\x00\x00\x40" + b"\xfeSMB" + b"\x00" * 59 + b"ping-quicsmb"
s = socket.create_connection(("127.0.0.1", port), timeout=10)
s.sendall(payload)
got = b""
while len(got) < len(payload):
    d = s.recv(65536)
    if not d: break
    got += d
assert got == payload, f"mismatch: {got!r}"
print("roundtrip ok:", len(got), "bytes")
EOF

echo "==> web panels"
curl -sf "http://127.0.0.1:$WEB_SRV/healthz" > /dev/null && echo "server web ok"
curl -sf "http://127.0.0.1:$WEB_CLI/api/status" | head -c 200; echo
echo "==> PASS"
