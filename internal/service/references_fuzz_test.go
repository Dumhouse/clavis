package service

import "testing"

func FuzzResolveSecretReferences(f *testing.F) {
	f.Add("${A}", "x")
	f.Add("${A}-${B}", "y")
	f.Add("no refs", "z")
	f.Fuzz(func(_ *testing.T, value, replacement string) {
		secrets := []Secret{
			{Key: "A", Kind: "variable", Value: replacement},
			{Key: "B", Kind: "variable", Value: value},
		}
		_ = resolveSecretReferences(secrets)
	})
}
