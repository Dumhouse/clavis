package authz

import (
	"path"
	"regexp"
	"strings"
	"sync"
)

var (
	globCache sync.Map // pattern -> *regexp.Regexp
)

// MatchGlob matches value against a small glob dialect:
//
//   - matches any run of characters except '/'
//     **  matches any run of characters including '/'
//     ?   matches a single character except '/'
//
// An empty pattern matches everything. The empty string only matches "".
func MatchGlob(pattern, value string) bool {
	if pattern == "" {
		return true
	}
	if pattern == value {
		return true
	}
	re := compileGlob(pattern)
	return re.MatchString(value)
}

func compileGlob(pattern string) *regexp.Regexp {
	if cached, ok := globCache.Load(pattern); ok {
		return cached.(*regexp.Regexp)
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); {
		switch {
		case pattern[i] == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			b.WriteString(".*")
			i += 2
		case pattern[i] == '*':
			b.WriteString("[^/]*")
			i++
		case pattern[i] == '?':
			b.WriteString("[^/]")
			i++
		case pattern[i] == '/':
			b.WriteString("/")
			i++
		default:
			// Escape the remainder of the character. Multi-byte UTF-8 is
			// matched literally, which is sufficient for env and path globs.
			j := i + 1
			for j < len(pattern) && pattern[j]&0xC0 == 0x80 {
				j++
			}
			b.WriteString(regexp.QuoteMeta(pattern[i:j]))
			i = j
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		// QuoteMeta guarantees a valid expression; fall back to exact match.
		re = regexp.MustCompile("^" + regexp.QuoteMeta(pattern) + "$")
	}
	globCache.Store(pattern, re)
	return re
}

// NormalizePath ensures a folder path starts with "/" and does not end with
// one (except the root), then cleans it.
func NormalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	return cleaned
}
