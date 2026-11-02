#!/bin/bash
# qvfs e2e：qvps + qhome 本地回环，真 QUIC。
# 1) 起 qvps（新端口） 2) 起 qhome（对 key） 3) 正确 key 取块成功 4) 错 key 黑洞无响应 5) 面板含 qvfs 字段
set -e
BIN=/tmp/qvfs-e2e
PORT=14711
WEB=127.0.0.1:18071
export QVFS_KEY=e2e-key-456
pkill -f "qvfs-e2e/qvps" 2>/dev/null || true
pkill -f "qvfs-e2e/qhome" 2>/dev/null || true
sleep 1
(cd /Users/kkito/proj/demo/quicsmb && GOFLAGS=-mod=mod GOPROXY="file:///Users/kkito/go/pkg/mod/cache/download" GOSUMDB=off go build -o $BIN/qvps ./cmd/qvps && GOFLAGS=-mod=mod GOPROXY="file:///Users/kkito/go/pkg/mod/cache/download" GOSUMDB=off go build -o $BIN/qhome ./cmd/qhome)
rm -rf $BIN/meta-e2e $BIN/home-e2e && mkdir -p $BIN/meta-e2e $BIN/home-e2e
echo "hello-qvfs-e2e-block-0" > $BIN/home-e2e/seed.bin
(QVFS_DELAY_SEC=1 QVFS_WIN=0-1439 QVFS_TICK_SEC=2 QVFS_KEY=e2e-key-456 $BIN/qvps -port $PORT -web $WEB -root $BIN/meta-e2e > $BIN/qvps-e2e.log 2>&1 &)
sleep 1
(QVFS_KEY=e2e-key-456 $BIN/qhome -server 127.0.0.1 -port $PORT -root $BIN/home-e2e > $BIN/qhome-e2e.log 2>&1 &)
sleep 2
echo "--- 1. status 含 qvfs ---"
curl -sf http://$WEB/api/status | grep -q '"qvfs"' && echo PASS-qvfs-field || (echo FAIL-qvfs-field; cat $BIN/qvps-e2e.log; exit 1)
echo "--- 2. 黑洞：错 key 零回包 ---"
cd /Users/kkito/proj/demo/quicsmb
cat > /tmp/qvfs-e2e/probe.go <<EOF
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"quicsmb/internal/auth"
	"quicsmb/internal/quic"
	"quicsmb/internal/xfer"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := quic.Dial(ctx, "127.0.0.1", $PORT, "qvfs/1", true, 5*time.Second)
	if err != nil {
		panic(err)
	}
	defer c.CloseWithError(0, "")
	st, err := quic.OpenStream(ctx, c)
	if err != nil {
		panic(err)
	}
	defer st.Close()
	n, _ := auth.Nonce16()
	mode := os.Args[1]
	key := []byte("e2e-key-456")
	if mode == "bad" {
		key = []byte("wrong-key")
	}
	hello := auth.Seal(key, n[:], []byte("probe"))
	if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchReq, Data: hello}); err != nil {
		panic(err)
	}
	if mode == "good" {
		if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchReq, File: "seed.bin", Index: 0}); err != nil {
			panic(err)
		}
		m, err := xfer.Decode(st)
		if err != nil {
			panic(err)
		}
		fmt.Printf("FETCH-OK len=%d head=%q\n", len(m.Data), string(m.Data[:22]))
		return
	}
	st.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err = xfer.Decode(st)
	if err == nil {
		fmt.Println("BLACKHOLE-FAIL: got response")
		os.Exit(1)
	}
	fmt.Println("BLACKHOLE-OK: no response")
}
EOF
cp /tmp/qvfs-e2e/probe.go ./probe_e2e_main.go
QVFS_KEY=e2e-key-456 GOFLAGS=-mod=mod GOPROXY="file:///Users/kkito/go/pkg/mod/cache/download" GOSUMDB=off go run ./probe_e2e_main.go good
GOFLAGS=-mod=mod GOPROXY="file:///Users/kkito/go/pkg/mod/cache/download" GOSUMDB=off go run ./probe_e2e_main.go bad
rm -f ./probe_e2e_main.go
echo "--- 3. 回写 roundtrip：qvps 写入→家里落盘 ---"
cat > /tmp/qvfs-e2e/push.go <<EOF
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"quicsmb/internal/auth"
	"quicsmb/internal/quic"
	"quicsmb/internal/xfer"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, err := quic.Dial(ctx, "127.0.0.1", $PORT, "qvfs/1", true, 5*time.Second)
	if err != nil {
		panic(err)
	}
	defer c.CloseWithError(0, "")
	st, err := quic.OpenStream(ctx, c)
	if err != nil {
		panic(err)
	}
	defer st.Close()
	n, _ := auth.Nonce16()
	hello := auth.Seal([]byte("e2e-key-456"), n[:], []byte("pusher"))
	if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpFetchReq, Data: hello}); err != nil {
		panic(err)
	}
	data := []byte("roundtrip-payload-12345")
	if err := xfer.Encode(st, xfer.Msg{Op: xfer.OpPushReq, File: "seed.bin", Index: 0, Data: data}); err != nil {
		panic(err)
	}
	m, err := xfer.Decode(st)
	if err != nil {
		panic(err)
	}
	fmt.Printf("PUSH-OK op=%d file=%s idx=%d\n", m.Op, m.File, m.Index)
	os.Exit(0)
}
EOF
cp /tmp/qvfs-e2e/push.go ./push_e2e_main.go
# 注：qvps 的 serveStream 对 PushReq 只回 ACK 不落盘；真正的回写走调度器。
# 这里先验证 ACK 通路；完整 roundtrip 由下面的 volume 级验证覆盖。
GOFLAGS=-mod=mod GOPROXY="file:///Users/kkito/go/pkg/mod/cache/download" GOSUMDB=off go run ./push_e2e_main.go
rm -f ./push_e2e_main.go
echo "--- 4. 等调度回写（~6s），验家里落盘 ---"
sleep 8
if grep -q "roundtrip-payload" $BIN/home-e2e/seed.bin 2>/dev/null; then echo "WRITEBACK-OK: home has pushed data"; else echo "WRITEBACK-FAIL"; ls -la $BIN/home-e2e/; exit 1; fi
pkill -f "qvfs-e2e/qvps"; pkill -f "qvfs-e2e/qhome"; true
echo "ALL-E2E-PASS"
