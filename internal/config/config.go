// Package config 解析 quicsmb JSON 配置，对应 C 版 src/core/config.c。
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

const (
	DefaultALPN       = "quicsmb/1"
	DefaultQUICPort   = 4433
	DefaultSMBPort    = 445
	DefaultListenPort = 8445
)

type Mode string

const (
	ModeClient Mode = "client"
	ModeServer Mode = "server"
)

// Config 对应 qs_config_t。
type Config struct {
	Mode               Mode   `json:"mode"`
	ServerHost         string `json:"server_host"`
	ServerPort         int    `json:"server_port"`
	ListenHost         string `json:"listen_host"`
	ListenPort         int    `json:"listen_port"`
	QUICPort           int    `json:"quic_port"`
	BackendHost        string `json:"backend_host"`
	BackendPort        int    `json:"backend_port"`
	CertFile           string `json:"cert_file"`
	KeyFile            string `json:"key_file"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	ALPN               string `json:"alpn"`
	IdleTimeoutSec     int    `json:"idle_timeout_sec"`
	MaxSessions        int    `json:"max_sessions"`
	WebAddr            string `json:"web_addr"`
}

// Defaults 返回带默认值的 Config。
func Defaults() Config {
	return Config{
		Mode:               ModeClient,
		ServerHost:         "127.0.0.1",
		ServerPort:         DefaultQUICPort,
		ListenHost:         "127.0.0.1",
		ListenPort:         DefaultListenPort,
		QUICPort:           DefaultQUICPort,
		BackendHost:        "127.0.0.1",
		BackendPort:        DefaultSMBPort,
		InsecureSkipVerify: true,
		ALPN:               DefaultALPN,
		IdleTimeoutSec:     300,
		MaxSessions:        1024,
		WebAddr:            ":8080",
	}
}

// Load 读 JSON 文件；path 为空返回默认值。未知字段报错。
func Load(path string) (Config, error) {
	c := Defaults()
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("bad config %s: %w", path, err)
	}
	if c.Mode != ModeClient && c.Mode != ModeServer {
		return c, fmt.Errorf("bad mode %q: want client|server", c.Mode)
	}
	return c, nil
}
