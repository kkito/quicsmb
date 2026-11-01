# docs/quic-msquic.md — 接入真实 MsQuic 的步骤（当前为 stub，后续替换）

当前 `src/quic/quic_stub.c` 用 TCP 直通实现了一套与 `include/quicsmb/quic.h`
完全同构的 API，所以隧道、多路复用、SMB 嗅探、测试都可以先跑通。
换成真实 QUIC 只需实现同一个头文件，`tunnel.c / main.c` 一行不用改。

## 1. 安装 MsQuic（含 QUIC-TLS 的 OpenSSL 分支）

vcpkg 的 msquic port 需要从 GitHub 拉 quictls/openssl（本机网络曾超时），
建议重试或手动装：

```sh
# 重试 vcpkg（网络好时）
~/vcpkg install msquic --triplet arm64-osx   # linux: x64-linux
# 或 brew（若有 bottle）
brew install msquic
```

然后把 `vcpkg.json` 加回 `"msquic"`，`CMakeLists.txt` 已有
`QUICSMB_HAVE_MSQUIC` 自动切换逻辑（找到即编 `quic_msquic.c`，找不到即 stub）。

## 2. 需要实现的函数（对照 quic_stub.c 逐个替换）

| 函数 | MsQuic 对应做法 |
|---|---|
| `qs_quic_init_client` | `MsQuicOpen2 + RegistrationOpen + ConfigurationOpen(ALPN) + ConfigurationLoadCredential(NULL)` |
| `qs_dial` | `ConnectionOpen + ConnectionStart(ServerName, Port)`，等 `CONNECTED` 事件 |
| `qs_stream_open` | `StreamOpen(QUIC_STREAM_OPEN_FLAG_NONE) + StreamStart(QUIC_STREAM_START_FLAG_NONE)` |
| `qs_stream_send` | `StreamSend(buf, len, QUIC_SEND_FLAG_NONE)`；拷贝由 MsQuic 接管，需处理 `SEND_COMPLETE` 释放 |
| 收包 | `QUIC_STREAM_EVENT_RECEIVE` 回调 → 调 `cbs.on_data`；注意回调在 MsQuic 线程，需 `event_active`/pipe 踢回 libevent 线程再 `bufferevent_write`（当前 stub 是直调，仅 PoC 可用） |
| `qs_quic_init_server` | `ListenerOpen + ListenerStart(ALPN, addr) + ConfigurationLoadCredential(FILE cert/key)`；Windows 互通时 ALPN 改 `smb`（`configs/` 里 `alpn` 字段已预留） |
| accept | `QUIC_LISTENER_EVENT_NEW_CONNECTION → ConnectionSetConfiguration + AcceptStream`，每条入向 bidi stream 调 `accept_cb` |

关键语义（SMB3 要求）：stream 是**有序字节流**，和 TCP 一样直接透传，
不要自己加帧；`SHUTDOWN` 只在会话 teardown 时发
（`QUIC_STREAM_SHUTDOWN_FLAG_GRACEFUL`），SMB 全双工期间不断流。

## 3. 证书（SMB over QUIC / Windows 互通要求）

- 自用隧道：自签 cert + client `insecure_skip_verify` 即可（已在 config 预留）。
- Windows 原生客户端（`New-SmbMapping -TransportType QUIC`）：server 必须挂
  受信 CA 证书（SAN=主机名），ALPN=`smb`，UDP 443 对外。
  详见 MS-SMB2 §2.1 + 微软文档 "SMB over QUIC"。

## 4. 验证

```sh
# stub（现在）
make test && <e2e 见 README>
# 换 msquic 后同样命令应通过；另加：
smbclient //127.0.0.1/share -p 8445 -c 'put bigfile'   # 经隧道存取
mount -t smbfs //user@127.0.0.1/share /mnt/q            # macOS 经 8445 口
```
