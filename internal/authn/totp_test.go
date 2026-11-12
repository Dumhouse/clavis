package authn

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, tc := range cases {
		at := time.Unix(tc.unix, 0)
		if got := TOTPCode(secret, at); got != tc.want {
			t.Errorf("TOTP(%d) = %s, want %s", tc.unix, got, tc.want)
		}
		if !ValidateTOTP(secret, tc.want, at) {
			t.Errorf("ValidateTOTP(%d) rejected %s", tc.unix, tc.want)
		}
	}
	if ValidateTOTP(secret, "000000", time.Unix(59, 0)) {
		t.Fatal("wrong code accepted")
	}
	if ValidateTOTP(secret, "12345", time.Unix(59, 0)) {
		t.Fatal("short code accepted")
	}
}

func TestTOTPURL(t *testing.T) {
	target := TOTPURL("Clavis", "user@example.com", []byte("12345678901234567890"))
	if !strings.HasPrefix(target, "otpauth://totp/Clavis:user@example.com?") {
		t.Fatalf("url = %q", target)
	}
	if !regexp.MustCompile(`issuer=Clavis`).MatchString(target) {
		t.Fatalf("url = %q", target)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, hashes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount || len(hashes) != RecoveryCodeCount {
		t.Fatalf("counts = %d/%d", len(codes), len(hashes))
	}
	for i, code := range codes {
		if !regexp.MustCompile(`^[A-Z2-9]{5}-[A-Z2-9]{5}$`).MatchString(code) {
			t.Fatalf("code = %q", code)
		}
		if string(hashes[i]) != string(HashRecoveryCode(code)) {
			t.Fatalf("hash mismatch for %q", code)
		}
	}
	if string(HashRecoveryCode("abcde-fghij")) != string(HashRecoveryCode("ABCDE FGHIJ")) {
		t.Fatal("normalization failed")
	}
}
