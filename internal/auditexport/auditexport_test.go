package auditexport

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/config"
)

func testEvent() *audit.Event {
	return &audit.Event{
		ID: "evt-1", OrgID: "org-1", Seq: 7,
		Actor:    audit.Actor{ID: "user-1", Kind: "user", Name: "Ada"},
		Action:   "secret.reveal",
		Resource: audit.Resource{Type: "secret", Name: "STRIPE_KEY"},
		IP:       "203.0.113.5", CreatedAt: time.Unix(1700000000, 0).UTC(),
	}
}

func TestHTTPExporters(t *testing.T) {
	cases := []struct {
		name      string
		cfg       config.AuditExporterConfig
		checkAuth func(*testing.T, *http.Request)
		checkBody func(*testing.T, []byte)
	}{
		{
			name: "webhook",
			cfg:  config.AuditExporterConfig{Type: "webhook", Format: "json", Token: "tok"},
			checkAuth: func(t *testing.T, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer tok" {
					t.Fatalf("auth = %q", r.Header.Get("Authorization"))
				}
			},
			checkBody: func(t *testing.T, body []byte) {
				if !strings.Contains(string(body), `"action":"secret.reveal"`) {
					t.Fatalf("body = %s", body)
				}
			},
		},
		{
			name: "webhook-cef",
			cfg:  config.AuditExporterConfig{Type: "webhook", Format: "cef"},
			checkBody: func(t *testing.T, body []byte) {
				if !strings.HasPrefix(string(body), "CEF:0|Clavis|Clavis|") || !strings.Contains(string(body), "suser=Ada") {
					t.Fatalf("cef = %s", body)
				}
			},
		},
		{
			name: "splunk",
			cfg:  config.AuditExporterConfig{Type: "splunk", Format: "json", Token: "hec"},
			checkAuth: func(t *testing.T, r *http.Request) {
				if r.Header.Get("Authorization") != "Splunk hec" {
					t.Fatalf("auth = %q", r.Header.Get("Authorization"))
				}
			},
			checkBody: func(t *testing.T, body []byte) {
				if !strings.Contains(string(body), `"sourcetype":"clavis:audit"`) {
					t.Fatalf("body = %s", body)
				}
			},
		},
		{
			name: "datadog",
			cfg:  config.AuditExporterConfig{Type: "datadog", Format: "json", APIKey: "dd-key"},
			checkAuth: func(t *testing.T, r *http.Request) {
				if r.Header.Get("DD-API-KEY") != "dd-key" {
					t.Fatalf("api key = %q", r.Header.Get("DD-API-KEY"))
				}
			},
			checkBody: func(t *testing.T, body []byte) {
				var logs []map[string]string
				if err := json.Unmarshal(body, &logs); err != nil || len(logs) != 1 || logs[0]["ddsource"] != "clavis" {
					t.Fatalf("logs = %s (%v)", body, err)
				}
			},
		},
		{
			name: "elastic",
			cfg:  config.AuditExporterConfig{Type: "elastic", Format: "json", APIKey: "es-key"},
			checkAuth: func(t *testing.T, r *http.Request) {
				if r.Header.Get("Authorization") != "ApiKey es-key" {
					t.Fatalf("auth = %q", r.Header.Get("Authorization"))
				}
			},
			checkBody: func(t *testing.T, body []byte) {
				if !strings.Contains(string(body), `"id":"evt-1"`) {
					t.Fatalf("body = %s", body)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan *http.Request, 1)
			bodies := make(chan []byte, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				received <- r
				bodies <- body
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			tc.cfg.URL = srv.URL
			dispatcher := New([]config.AuditExporterConfig{tc.cfg}, srv.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			dispatcher.Publish(testEvent())
			select {
			case req := <-received:
				body := <-bodies
				if tc.checkAuth != nil {
					tc.checkAuth(t, req)
				}
				if tc.checkBody != nil {
					tc.checkBody(t, body)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("exporter did not deliver")
			}
		})
	}
}

func TestSyslogExporter(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	dispatcher := New([]config.AuditExporterConfig{{
		Type: "syslog", Format: "cef", Address: conn.LocalAddr().String(), Network: "udp",
	}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dispatcher.Publish(testEvent())
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 65535)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	message := string(buf[:n])
	if !strings.HasPrefix(message, "<14>1 ") || !strings.Contains(message, "CEF:0|Clavis") {
		t.Fatalf("syslog = %q", message)
	}
}

func TestS3ExporterSignsPut(t *testing.T) {
	received := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- r
		bodies <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	dispatcher := New([]config.AuditExporterConfig{{
		Type: "s3", Format: "json", Bucket: "audit", Region: "us-east-1",
		Endpoint: srv.URL, Prefix: "clavis", AccessKey: "AKID", SecretKey: "SECRET",
	}}, srv.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	dispatcher.Publish(testEvent())
	select {
	case req := <-received:
		body := <-bodies
		if req.Method != http.MethodPut {
			t.Fatalf("method = %s", req.Method)
		}
		auth := req.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKID/") || !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") {
			t.Fatalf("authorization = %q", auth)
		}
		if !strings.Contains(req.URL.Path, "/audit/clavis/") || !strings.HasSuffix(req.URL.Path, ".json") {
			t.Fatalf("path = %s", req.URL.Path)
		}
		if !strings.Contains(string(body), `"id":"evt-1"`) {
			t.Fatalf("body = %s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("s3 exporter did not deliver")
	}
}
