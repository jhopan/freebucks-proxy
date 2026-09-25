package stealth

import (
	"context"
	"net"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestBunSpecHandshakeLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live dial")
	}
	host := "www.codebuff.com:443"
	d := net.Dialer{Timeout: 15 * time.Second}
	raw, err := d.Dial("tcp", host)
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	cfg := &utls.Config{ServerName: "www.codebuff.com", NextProtos: []string{"http/1.1"}}
	uconn := utls.UClient(raw, cfg, utls.HelloCustom)
	if err := uconn.ApplyPreset(bunSpec()); err != nil {
		t.Fatalf("ApplyPreset: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := uconn.HandshakeContext(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	cs := uconn.ConnectionState()
	t.Logf("negotiated version: %#04x, cipher: %#04x", cs.Version, cs.CipherSuite)
	_ = uconn.Close()
}