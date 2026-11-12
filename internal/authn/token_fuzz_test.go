package authn

import "testing"

func FuzzParseToken(f *testing.F) {
	for _, seed := range []string{
		"clv_mt_abc12345deadbeef", "clv_at_", "clv_pat_x", "", "clv_xx_123456789",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, token string) {
		kind, err := ParseToken(token)
		if err != nil {
			return
		}
		if kind != TokenMachine && kind != TokenAgent && kind != TokenPersonal {
			t.Fatalf("unexpected kind %q", kind)
		}
	})
}
