// Package quic 提供真实 QUIC 传输（quic-go）：client 拨号、server 监听，
// 每条 TCP 连接对应一条 QUIC 双向 stream，payload 透明透传。
// ALPN 默认 quicsmb/1，Windows 互通时配成 smb。
package quic

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

// Dial 用 QUIC 拨 server，返回 *quic.Conn。insecure 跳过证书校验（自签 dev 用）。
func Dial(ctx context.Context, host string, port int, alpn string, insecure bool, timeout time.Duration) (*quic.Conn, error) {
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	tlsConf := &tls.Config{
		InsecureSkipVerify: insecure, //nolint:gosec // dev 自签；生产配受信证书
		NextProtos:         []string{alpn},
		ServerName:         host,
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := quic.Dial(dialCtx, udpConn, udpAddr, tlsConf, quicConfig())
	if err != nil {
		_ = udpConn.Close()
		return nil, err
	}
	return conn, nil
}

// Listen 在 UDP port 上起 QUIC server。certFile/keyFile 为空时自动生成自签证书
// （dev 方便；Windows 互通需挂受信证书）。返回 listener，调用者负责 Accept 循环
// 与 Close；ctx 取消时 Accept 返回错误退出。
func Listen(port int, alpn, certFile, keyFile string) (*quic.Listener, error) {
	cert, err := loadCert(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	tlsConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{alpn},
	}
	addr := fmt.Sprintf(":%d", port)
	l, err := quic.ListenAddr(addr, tlsConf, quicConfig())
	if err != nil {
		return nil, err
	}
	return l, nil
}

// OpenStream 在 conn 上开一条双向 stream。
func OpenStream(ctx context.Context, conn *quic.Conn) (*quic.Stream, error) {
	return conn.OpenStreamSync(ctx)
}

// loadCert 读 PEM 证书；为空则生成内存自签（dev 用）。
func loadCert(certFile, keyFile string) (tls.Certificate, error) {
	if certFile != "" && keyFile != "" {
		return tls.LoadX509KeyPair(certFile, keyFile)
	}
	return selfSigned()
}

func selfSigned() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "quicsmb"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func quicConfig() *quic.Config {
	return &quic.Config{
		KeepAlivePeriod: 25 * time.Second,
	}
}
