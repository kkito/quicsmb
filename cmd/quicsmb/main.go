// Command quicsmb 是 SMB over QUIC 隧道：client 监听 TCP、server 落地后端 SMB，
// 中间经 QUIC/UDP（ALPN=quicsmb/1）透传字节。每条 TCP 一条 QUIC 双向 stream。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"

	"quicsmb/internal/config"
	"quicsmb/internal/session"
	qquic "quicsmb/internal/quic"
	"quicsmb/internal/tunnel"
	"quicsmb/internal/web"
)

var version = "dev"

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	mode := flag.String("m", "", "client|server（覆盖 -c 配置）")
	cfgPath := flag.String("c", "configs/client.json", "JSON 配置路径")
	webAddr := flag.String("web", "", "Web 面板监听地址（覆盖配置，空=关闭）")
	webRoot := flag.String("web-root", "", "Web 静态目录（dev 模式）")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *mode == "client" {
		cfg.Mode = config.ModeClient
	} else if *mode == "server" {
		cfg.Mode = config.ModeServer
	}
	if *webAddr != "" {
		cfg.WebAddr = *webAddr
	}
	web.WebRoot = *webRoot

	reg := session.New(cfg.MaxSessions)

	if cfg.WebAddr != "" {
		srv := &http.Server{Addr: cfg.WebAddr, Handler: web.NewRouter(reg, version, string(cfg.Mode))}
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("web %s: %v（面板关闭，主隧道继续）", cfg.WebAddr, err)
			}
		}()
		log.Printf("web 面板监听: %s", cfg.WebAddr)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Printf("退出")
		os.Exit(0)
	}()

	ctx := context.Background()
	if cfg.Mode == config.ModeServer {
		if err := runServer(ctx, cfg, reg); err != nil {
			log.Fatalf("server: %v", err)
		}
		return
	}
	if err := runClient(ctx, cfg, reg); err != nil {
		log.Fatalf("client: %v", err)
	}
}

// runClient: 监听 TCP，每条连接拨一条 QUIC stream。
func runClient(ctx context.Context, cfg config.Config, reg *session.Registry) error {
	addr := net.JoinHostPort(cfg.ListenHost, fmt.Sprint(cfg.ListenPort))
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()
	log.Printf("quicsmb client: tcp %s -> quic %s:%d alpn=%s", addr, cfg.ServerHost, cfg.ServerPort, cfg.ALPN)
	for {
		tcp, err := lis.Accept()
		if err != nil {
			return err
		}
		go handleClientConn(ctx, tcp, cfg, reg)
	}
}

func handleClientConn(ctx context.Context, tcp net.Conn, cfg config.Config, reg *session.Registry) {
	peer := tcp.RemoteAddr().String()
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := qquic.Dial(dialCtx, cfg.ServerHost, cfg.ServerPort, cfg.ALPN, cfg.InsecureSkipVerify, 10*time.Second)
	if err != nil {
		log.Printf("client %s: quic dial failed: %v", peer, err)
		_ = tcp.Close()
		return
	}
	streamCtx, scancel := context.WithTimeout(ctx, 10*time.Second)
	defer scancel()
	st, err := qquic.OpenStream(streamCtx, conn)
	if err != nil {
		log.Printf("client %s: open stream failed: %v", peer, err)
		_ = tcp.Close()
		_ = conn.CloseWithError(0, "open stream failed")
		return
	}
	s := tunnel.Start(tcp, wrapStream(st), log.Default())
	if !reg.Add(s, "client", peer) {
		log.Printf("client %s: max sessions, rejecting", peer)
		_ = tcp.Close()
		_ = st.Close()
		return
	}
	reg.PushLog("connect", "client "+peer+" sess="+fmt.Sprint(s.ID()))
	<-s.Done()
	stt := s.Stats()
	reg.AddCounters(stt.BytesUp, stt.BytesDown)
	reg.Remove(s.ID())
	reg.PushLog("close", "client "+peer+" sess="+fmt.Sprint(s.ID()))
}

// runServer: 监听 QUIC，每条 stream 拨后端 TCP。
func runServer(ctx context.Context, cfg config.Config, reg *session.Registry) error {
	l, err := qquic.Listen(cfg.QUICPort, cfg.ALPN, cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return err
	}
	defer l.Close()
	backend := net.JoinHostPort(cfg.BackendHost, fmt.Sprint(cfg.BackendPort))
	log.Printf("quicsmb server: quic :%d -> smb %s alpn=%s", cfg.QUICPort, backend, cfg.ALPN)
	for {
		conn, err := l.Accept(ctx)
		if err != nil {
			return err
		}
		go acceptConn(ctx, conn, cfg, reg)
	}
}

func acceptConn(ctx context.Context, conn *quic.Conn, cfg config.Config, reg *session.Registry) {
	for {
		st, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go handleServerStream(st, cfg, reg)
	}
}

func handleServerStream(st *quic.Stream, cfg config.Config, reg *session.Registry) {
	backend := net.JoinHostPort(cfg.BackendHost, fmt.Sprint(cfg.BackendPort))
	tcp, err := net.DialTimeout("tcp", backend, 10*time.Second)
	if err != nil {
		log.Printf("server: dial backend %s failed: %v", backend, err)
		_ = st.Close()
		return
	}
	s := tunnel.Start(tcp, wrapStream(st), log.Default())
	if !reg.Add(s, "server", connRemote(st)) {
		log.Printf("server: max sessions, rejecting")
		_ = tcp.Close()
		_ = st.Close()
		return
	}
	reg.PushLog("connect", "server sess="+fmt.Sprint(s.ID())+" -> "+backend)
	<-s.Done()
	stt := s.Stats()
	reg.AddCounters(stt.BytesUp, stt.BytesDown)
	reg.Remove(s.ID())
	reg.PushLog("close", "server sess="+fmt.Sprint(s.ID()))
}

func connRemote(st *quic.Stream) string {
	return fmt.Sprintf("stream-%d", st.StreamID())
}

// streamAdapter 把 *quic.Stream 适配到 tunnel.Stream。
type streamAdapter struct {
	*quic.Stream
}

func (a *streamAdapter) CancelWrite(code uint64) {
	a.Stream.CancelWrite(quic.StreamErrorCode(code))
}

func (a *streamAdapter) CancelRead(code uint64) {
	a.Stream.CancelRead(quic.StreamErrorCode(code))
}

func wrapStream(st *quic.Stream) tunnel.Stream { return &streamAdapter{st} }
