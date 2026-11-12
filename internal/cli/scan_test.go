package cli

import (
	"testing"

	"github.com/dumhouse/clavis/internal/authn"
)

func TestScanBytesFindsTokensAndValues(t *testing.T) {
	token, err := authn.GenerateToken(authn.TokenMachine)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("first line\n" + token + "\nvalue=super-secret\nDUP super-secret\n")
	findings := scanBytes(data, "app.env", map[string]string{"DB_PASSWORD": "super-secret"})
	if len(findings) != 3 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].Type != "clavis-token" || findings[0].Line != 2 {
		t.Fatalf("token finding = %#v", findings[0])
	}
	if findings[1].Key != "DB_PASSWORD" || findings[1].Line != 3 || findings[2].Line != 4 {
		t.Fatalf("value findings = %#v", findings[1:])
	}
}

func TestScanBytesIgnoresInvalidToken(t *testing.T) {
	findings := scanBytes([]byte("clv_mt_abcdefghij\n"), "x", nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}
