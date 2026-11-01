# quicsmb — SMB over QUIC 隧道（Go）

client 监听 TCP、server 落地后端 SMB，中间经 **真实 QUIC/UDP**（quic-go，
ALPN=`quicsmb/1`，Windows 互通预留 `smb`）透传字节。每条 TCP 一条 QUIC 双向 stream。

```
[SMB客户端] --TCP--> [quicsmb client :8445] --QUIC/UDP--> [quicsmb server :4433] --TCP445--> [真SMB/samba]
```

C 版（libevent+msquic stub）已归档到 `archive-c/`，`qs-tunnel.py`（aioquic 验证版）归档到
`archive-c/` 同级 `archive-py/`，仅供对照；当前主实现是 Go。

## 跑起来

```sh
make              # 前端打包 + 编译 → dist/quicsmb
./dist/quicsmb -c configs/server.json   # server：QUIC :4433 → 后端 127.0.0.1:445
./dist/quicsmb -c configs/client.json   # client：TCP 127.0.0.1:8445 → server
# SMB 客户端连 127.0.0.1:8445 即可
```

`-m client|server` 可覆盖配置文件 mode；`--web` 覆盖面板地址；`--web-root web` 为前端 dev 模式。

## 验证

```sh
make test-all   # go test + vitest
make e2e        # 假 SMB 回显后端 + server + client + 隧道 roundtrip + 面板检查
```

## 面板

server `:8080` / client `:8081`（configs 可改）：状态（mode/会话数/字节）、
会话表（peer/上下行/SMB 方言+命令）、日志。API 信封统一 `{ok,data|error}`：

- `GET /healthz` `GET /api/status` `GET /api/sessions` `GET /api/logs?limit=50`

## 结构（对照 mytun）

```
cmd/quicsmb/      main.go（client/server 双模式 + 面板）
internal/config/  JSON 配置（原 C src/core/config.c）
internal/smb/     SMB2 头嗅探，仅日志（原 src/smb/smb.c）
internal/tunnel/  TCP<->stream 中继（原 src/proxy/tunnel.c）
internal/quic/    真实 QUIC 拨号/监听（quic-go，原 stub 的替代）
internal/session/ 会话注册表 + 环形日志（给面板）
internal/web/     标准库 HTTP + 信封（抄 mytun internal/web）
internal/webdist/ 内嵌前端产物（scripts/build_web.cjs 生成）
web/src/          qs-status/qs-sessions/qs-log（WebComponent 一文件一组件）
configs/          client.json / server.json
scripts/e2e.sh    本地回环验证
```

## 证书

- 自用：cert/key 留空，server 自动生成内存自签 + client `insecure_skip_verify` 即可。
- Windows 原生（`New-SmbMapping -TransportType QUIC`）：挂受信证书（SAN=主机名），
  `alpn` 改 `"smb"`，UDP 443 对外。
