package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	c := Defaults()
	if c.ALPN != "quicsmb/1" {
		t.Fatalf("alpn = %q", c.ALPN)
	}
	if c.ServerPort != 4433 || c.ListenPort != 8445 || c.BackendPort != 445 {
		t.Fatalf("ports = %d/%d/%d", c.ServerPort, c.ListenPort, c.BackendPort)
	}
}

func TestLoadEmptyPathReturnsDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Mode != ModeClient {
		t.Fatalf("mode = %q", c.Mode)
	}
}

func TestLoadServerJSON(t *testing.T) {
	body := `{"mode":"server","quic_port":4433,"backend_host":"10.0.0.1","backend_port":445,"alpn":"smb","max_sessions":8}`
	p := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Mode != ModeServer || c.BackendHost != "10.0.0.1" || c.ALPN != "smb" || c.MaxSessions != 8 {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestLoadBadMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(p, []byte(`{"mode":"nope"}`), 0600)
	if _, err := Load(p); err == nil {
		t.Fatal("want error for bad mode")
	}
}

func TestLoadUnknownField(t *testing.T) {
	p := filepath.Join(t.TempDir(), "u.json")
	_ = os.WriteFile(p, []byte(`{"mode":"client","nope":1}`), 0600)
	if _, err := Load(p); err == nil {
		t.Fatal("want error for unknown field")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("want error for missing file")
	}
}
