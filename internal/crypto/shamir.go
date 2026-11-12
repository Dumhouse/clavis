package crypto

import (
	"crypto/rand"
	"fmt"
)

var gfExp [512]byte
var gfLog [256]int

func init() {
	x := byte(1)
	for i := 0; i < 255; i++ {
		gfExp[i] = x
		gfLog[x] = i
		x = gfMul(x, 3)
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	var result byte
	for b > 0 {
		if b&1 != 0 {
			result ^= a
		}
		high := a & 0x80
		a <<= 1
		if high != 0 {
			a ^= 0x1b
		}
		b >>= 1
	}
	return result
}

func gfDiv(a, b byte) byte {
	if a == 0 {
		return 0
	}
	if b == 0 {
		return 0
	}
	return gfExp[(gfLog[a]-gfLog[b]+255)%255]
}

// SplitSecret splits secret into shares requiring threshold shares to rebuild.
func SplitSecret(secret []byte, shares, threshold int) ([][]byte, error) {
	if shares < 2 || shares > 255 {
		return nil, fmt.Errorf("crypto: share count must be between 2 and 255")
	}
	if threshold < 2 || threshold > shares {
		return nil, fmt.Errorf("crypto: threshold must be between 2 and the share count")
	}
	if len(secret) == 0 {
		return nil, fmt.Errorf("crypto: secret is empty")
	}
	out := make([][]byte, shares)
	for i := range out {
		out[i] = make([]byte, len(secret)+1)
		out[i][0] = byte(i + 1)
	}
	coefficients := make([]byte, threshold)
	for offset := range secret {
		coefficients[0] = secret[offset]
		if _, err := rand.Read(coefficients[1:]); err != nil {
			return nil, err
		}
		for share := 0; share < shares; share++ {
			x := byte(share + 1)
			value := coefficients[threshold-1]
			for i := threshold - 2; i >= 0; i-- {
				value = gfMul(value, x) ^ coefficients[i]
			}
			out[share][offset+1] = value
		}
	}
	return out, nil
}

// CombineShares reconstructs a secret from threshold or more shares.
func CombineShares(shares [][]byte, threshold int) ([]byte, error) {
	if threshold < 2 {
		return nil, fmt.Errorf("crypto: threshold must be at least 2")
	}
	if len(shares) < threshold {
		return nil, fmt.Errorf("crypto: need at least %d shares", threshold)
	}
	length := len(shares[0])
	if length < 2 {
		return nil, fmt.Errorf("crypto: invalid share")
	}
	for _, share := range shares {
		if len(share) != length {
			return nil, fmt.Errorf("crypto: shares have different lengths")
		}
	}
	selected := shares[:threshold]
	secret := make([]byte, length-1)
	for offset := 1; offset < length; offset++ {
		var value byte
		for i := 0; i < threshold; i++ {
			xi := selected[i][0]
			numerator, denominator := byte(1), byte(1)
			for j := 0; j < threshold; j++ {
				if i == j {
					continue
				}
				xj := selected[j][0]
				numerator = gfMul(numerator, xj)
				denominator = gfMul(denominator, xj^xi)
			}
			value ^= gfMul(selected[i][offset], gfDiv(numerator, denominator))
		}
		secret[offset-1] = value
	}
	return secret, nil
}
