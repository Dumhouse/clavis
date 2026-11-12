package authn

import (
	"strings"
	"testing"
)

func TestGenerateAndParseToken(t *testing.T) {
	for _, kind := range []string{TokenMachine, TokenAgent, TokenPersonal} {
		token, err := GenerateToken(kind)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(token, "clv_"+kind+"_") {
			t.Fatalf("token = %q", token)
		}
		got, err := ParseToken(token)
		if err != nil || got != kind {
			t.Fatalf("parse %q = %q %v", token, got, err)
		}
		if !IsClavisToken(token) {
			t.Fatal("IsClavisToken false")
		}
		if prefix := TokenPrefix(token); !strings.HasSuffix(prefix, "…") {
			t.Fatalf("prefix = %q", prefix)
		}
		if len(HashToken(token)) != 32 {
			t.Fatal("hash length")
		}
	}
	if _, err := GenerateToken("nope"); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestParseTokenRejectsTampering(t *testing.T) {
	token, _ := GenerateToken(TokenMachine)
	badEnd := "0"
	if token[len(token)-1:] == "0" {
		badEnd = "1"
	}
	badMid := "x"
	if token[len(token)-9:len(token)-8] == "x" {
		badMid = "y"
	}
	cases := []string{
		"",
		"clv_mt_short",
		"clx_mt_" + strings.Repeat("a", 40),
		"clv_zz_" + strings.Repeat("a", 40),
		"clv_mt_" + strings.Repeat("!", 40),
		token[:len(token)-1] + badEnd,
		token[:len(token)-9] + badMid + token[len(token)-8:],
	}
	for _, candidate := range cases {
		if _, err := ParseToken(candidate); err == nil {
			t.Errorf("accepted %q", candidate)
		}
	}
}

func TestBase62Encode(t *testing.T) {
	if got := base62Encode([]byte{0}); got != "0" {
		t.Fatalf("zero = %q", got)
	}
	long := base62Encode([]byte{0xff, 0xff, 0xff, 0xff})
	if long == "" {
		t.Fatal("empty")
	}
}
