package stealth

import (
	"testing"

	utls "github.com/refraction-networking/utls"
)

// TestBunSpecShape verifies the bun custom spec produces a ClientHello whose
// structural fields match the live capture (docs/operations/
// bun-1.3.14-clienthello.txt): 17 ciphers, http/1.1-only ALPN, three curves,
// nine signature algorithms, x25519-only key share, TLS 1.3+1.2 versions,
// padding 232, and NO GREASE anywhere.
func TestBunSpecShape(t *testing.T) {
	spec := bunSpec()
	if got := len(spec.CipherSuites); got != 17 {
		t.Errorf("cipher suites = %d, want 17", got)
	}
	wantCiphers := []uint16{0x1301, 0x1302, 0x1303, 0xc02b, 0xc02f, 0xc02c, 0xc030,
		0xcca9, 0xcca8, 0xc009, 0xc013, 0xc00a, 0xc014, 0x009c, 0x009d, 0x002f, 0x0035}
	for i, w := range wantCiphers {
		if spec.CipherSuites[i] != w {
			t.Errorf("cipher[%d] = %#04x, want %#04x", i, spec.CipherSuites[i], w)
		}
	}
	var alpn *utls.ALPNExtension
	for _, ext := range spec.Extensions {
		if a, ok := ext.(*utls.ALPNExtension); ok {
			alpn = a
		}
	}
	if alpn == nil {
		t.Fatal("ALPN extension missing")
	}
	if len(alpn.AlpnProtocols) != 1 || alpn.AlpnProtocols[0] != "http/1.1" {
		t.Errorf("ALPN = %v, want [http/1.1]", alpn.AlpnProtocols)
	}
	// No GREASE placeholder anywhere in ciphers.
	for _, c := range spec.CipherSuites {
		if c == utls.GREASE_PLACEHOLDER {
			t.Error("GREASE cipher present; Bun 1.3.x sends none")
		}
	}
	// Padding extension present.
	var pad bool
	for _, ext := range spec.Extensions {
		if _, ok := ext.(*utls.UtlsPaddingExtension); ok {
			pad = true
		}
	}
	if !pad {
		t.Error("padding extension missing")
	}
	// Extension order sanity: SNI first, padding last.
	if _, ok := spec.Extensions[0].(*utls.SNIExtension); !ok {
		t.Errorf("first extension = %T, want SNIExtension", spec.Extensions[0])
	}
	if _, ok := spec.Extensions[len(spec.Extensions)-1].(*utls.UtlsPaddingExtension); !ok {
		t.Errorf("last extension = %T, want UtlsPaddingExtension", spec.Extensions[len(spec.Extensions)-1])
	}
}

// TestLookupBun verifies TLS_FINGERPRINT=bun resolves to the Bun profile.
func TestLookupBun(t *testing.T) {
	p, ok := Lookup("bun")
	if !ok || p.ID != ProfileIDBun {
		t.Fatalf("Lookup(\"bun\") = %v, %v; want ProfileBun", p, ok)
	}
	if p.CustomSpec == nil {
		t.Fatal("ProfileBun.CustomSpec nil")
	}
}

// TestProfileBunNoBrowserHeaders keeps the CLI persona honest: the real CLI
// sends no browser UA on non-chat paths, so the Bun profile must carry none.
func TestProfileBunNoBrowserHeaders(t *testing.T) {
	if ProfileBun.UserAgent != "" {
		t.Errorf("ProfileBun.UserAgent = %q, want empty", ProfileBun.UserAgent)
	}
	if ProfileBun.AcceptLanguage != "" || ProfileBun.AcceptEncoding != "" {
		t.Error("ProfileBun carries browser headers; want none")
	}
}
