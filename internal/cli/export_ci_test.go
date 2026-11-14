package cli

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/pkg/problem"
)

func TestCIErrorAnnotation(t *testing.T) {
	t.Setenv("CLAVIS_ENV", "prod")
	t.Setenv("CLAVIS_SERVER", "https://clavis.test")
	cases := []struct {
		err   error
		title string
	}{
		{&problem.Problem{Status: http.StatusUnauthorized, Title: "Unauthorized"}, "token rejected"},
		{&problem.Problem{Status: http.StatusForbidden, Title: "Forbidden"}, "no access to prod"},
		{errors.New(`Get "https://clavis.test": dial tcp: connect: connection refused`), "server unreachable"},
		{errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority"), "certificate not trusted"},
		{errors.New("no project context: run `clavis init`"), "which project?"},
		{errors.New("line one\nline two"), "export failed"},
	}
	for _, tc := range cases {
		got := ciErrorAnnotation(tc.err)
		if !strings.HasPrefix(got, "::error title=Clavis: "+tc.title+"::") {
			t.Errorf("%v → %s", tc.err, got)
		}
		if strings.Contains(got, "\n") {
			t.Errorf("annotation spans lines: %q", got)
		}
	}
}
