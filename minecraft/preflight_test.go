package minecraft

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServerAddrFromProperties(t *testing.T) {
	tests := []struct {
		name     string
		contents *string // nil means no file
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{name: "missing file", contents: nil, wantPort: 25565},
		{name: "empty file", contents: ptr(""), wantPort: 25565},
		{name: "no keys", contents: ptr("motd=hello\n"), wantPort: 25565},
		{name: "port set", contents: ptr("server-port=25570\n"), wantPort: 25570},
		{name: "ip and port", contents: ptr("server-ip=127.0.0.1\nserver-port=25570\n"), wantHost: "127.0.0.1", wantPort: 25570},
		{name: "spaces around values", contents: ptr("server-port = 25570\nserver-ip = 127.0.0.1\n"), wantHost: "127.0.0.1", wantPort: 25570},
		{name: "windows line endings", contents: ptr("server-port=25570\r\n"), wantPort: 25570},
		{name: "commented out port", contents: ptr("#server-port=25570\n"), wantPort: 25565},
		{name: "blank port", contents: ptr("server-port=\n"), wantPort: 25565},
		{name: "blank ip", contents: ptr("server-ip=\n"), wantHost: "", wantPort: 25565},
		{name: "ipv6", contents: ptr("server-ip=::1\n"), wantHost: "::1", wantPort: 25565},
		{name: "port not a number", contents: ptr("server-port=abc\n"), wantErr: true},
		{name: "port zero", contents: ptr("server-port=0\n"), wantErr: true},
		{name: "port too large", contents: ptr("server-port=70000\n"), wantErr: true},
		{name: "invalid ip", contents: ptr("server-ip=not-an-ip\n"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.contents != nil {
				path := filepath.Join(dir, "server.properties")
				if err := os.WriteFile(path, []byte(*tt.contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			host, port, err := serverAddrFromProperties(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("serverAddrFromProperties() = (%q, %d, nil), want error", host, port)
				}
				return
			}
			if err != nil {
				t.Fatalf("serverAddrFromProperties() error: %v", err)
			}
			if host != tt.wantHost || port != tt.wantPort {
				t.Errorf("serverAddrFromProperties() = (%q, %d), want (%q, %d)", host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
