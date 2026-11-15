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

// maskWriter streams output and masks secrets, including ones split across
// writes. It holds back only a trailing run of bytes that could still grow
// into a secret (a proper prefix of one), so ordinary output such as a
// console prompt with no newline is shown at once instead of waiting for the
// next write.
type maskWriter struct {
	dst  io.Writer
	m    *masker
	tail []byte
	// hits counts the values replaced so far, so a console can say once at
	// the end that something was hidden.
	hits int
}

func (w *maskWriter) Write(p []byte) (int, error) {
	if w.m == nil || len(w.m.patterns) == 0 {
		return w.dst.Write(p)
	}
	w.tail = append(w.tail, p...)
	out := make([]byte, 0, len(w.tail))
	i := 0
	for i < len(w.tail) {
		if pattern := w.matchAt(w.tail, i); pattern != nil {
			out = append(out, maskReplacement...)
			w.hits++
			i += len(pattern)
			continue
		}
		if w.m.couldGrow(w.tail[i:]) {
			break
		}
		out = append(out, w.tail[i])
		i++
	}
	w.tail = append([]byte(nil), w.tail[i:]...)
	if len(out) > 0 {
		if _, err := w.dst.Write(out); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// couldGrow reports whether data is a proper prefix of any secret, so more
// output could still complete it.
func (m *masker) couldGrow(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, pattern := range m.byFirst[data[0]] {
		if len(data) < len(pattern) && bytes.HasPrefix(pattern, data) {
			return true
		}
	}
	return false
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
	if !bytes.Equal(out, w.tail) {
		w.hits++
	}
	w.tail = nil
	_, err := w.dst.Write(out)
	return err
}
