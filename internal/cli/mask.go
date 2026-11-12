package cli

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/url"
	"sort"
)

const maskReplacement = "••••••"

// masker replaces secret values and their common encoded forms in command
// output. This is a convenience, not a security boundary: a process that
// deliberately transforms a value can still leak it.
type masker struct {
	patterns [][]byte
	byFirst  map[byte][][]byte
	maxLen   int
}

func newMasker(values []string) *masker {
	seen := make(map[string]bool)
	var patterns [][]byte
	add := func(value string) {
		if len(value) < 4 || seen[value] {
			return
		}
		seen[value] = true
		patterns = append(patterns, []byte(value))
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		add(value)
		add(base64.StdEncoding.EncodeToString([]byte(value)))
		add(base64.RawStdEncoding.EncodeToString([]byte(value)))
		add(base64.URLEncoding.EncodeToString([]byte(value)))
		add(base64.RawURLEncoding.EncodeToString([]byte(value)))
		add(url.QueryEscape(value))
	}
	sort.Slice(patterns, func(i, j int) bool { return len(patterns[i]) > len(patterns[j]) })
	m := &masker{patterns: patterns, byFirst: make(map[byte][][]byte)}
	for _, pattern := range patterns {
		if len(pattern) > m.maxLen {
			m.maxLen = len(pattern)
		}
		m.byFirst[pattern[0]] = append(m.byFirst[pattern[0]], pattern)
	}
	return m
}

func (m *masker) mask(data []byte) []byte {
	if len(m.patterns) == 0 {
		return data
	}
	out := data
	for _, pattern := range m.patterns {
		out = bytes.ReplaceAll(out, pattern, []byte(maskReplacement))
	}
	return out
}

// maskWriter streams output while holding back enough bytes to catch values
// split across writes.
type maskWriter struct {
	dst  io.Writer
	m    *masker
	tail []byte
}

func (w *maskWriter) Write(p []byte) (int, error) {
	if w.m == nil || len(w.m.patterns) == 0 {
		return w.dst.Write(p)
	}
	w.tail = append(w.tail, p...)
	limit := len(w.tail) - (w.m.maxLen - 1)
	if limit <= 0 {
		return len(p), nil
	}
	out := make([]byte, 0, len(w.tail))
	i := 0
	for i < len(w.tail) {
		if i < limit {
			if pattern := w.matchAt(w.tail, i); pattern != nil {
				out = append(out, maskReplacement...)
				i += len(pattern)
				continue
			}
		}
		if i >= limit {
			break
		}
		out = append(out, w.tail[i])
		i++
	}
	w.tail = append([]byte(nil), w.tail[i:]...)
	if _, err := w.dst.Write(out); err != nil {
		return len(p), err
	}
	return len(p), nil
}

func (w *maskWriter) matchAt(data []byte, i int) []byte {
	for _, pattern := range w.m.byFirst[data[i]] {
		if bytes.HasPrefix(data[i:], pattern) {
			return pattern
		}
	}
	return nil
}

func (w *maskWriter) Flush() error {
	if len(w.tail) == 0 {
		return nil
	}
	out := w.m.mask(w.tail)
	w.tail = nil
	_, err := w.dst.Write(out)
	return err
}
