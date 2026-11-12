// Package auditexport delivers append-only audit events to external systems.
package auditexport

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/config"
)

// Dispatcher fans events out to configured exporters without blocking the
// request path. Slow destinations drop events after their buffer fills.
type Dispatcher struct {
	logger    *slog.Logger
	exporters []*exporter
}

type exporter struct {
	cfg    config.AuditExporterConfig
	client *http.Client
	logger *slog.Logger
	queue  chan *audit.Event
}

// New builds and starts a dispatcher. A nil client uses a short-timeout default.
func New(cfgs []config.AuditExporterConfig, client *http.Client, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	d := &Dispatcher{logger: logger}
	for _, cfg := range cfgs {
		e := &exporter{cfg: cfg, client: client, logger: logger, queue: make(chan *audit.Event, 256)}
		d.exporters = append(d.exporters, e)
		go e.run()
	}
	return d
}

// Publish enqueues an event for every exporter that has room.
func (d *Dispatcher) Publish(event *audit.Event) {
	if d == nil {
		return
	}
	for _, e := range d.exporters {
		select {
		case e.queue <- event:
		default:
			e.logger.Warn("audit exporter queue full; dropping event",
				"type", e.cfg.Type, "event_id", event.ID)
		}
	}
}

func (e *exporter) run() {
	for event := range e.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := e.send(ctx, event)
		cancel()
		if err != nil {
			e.logger.Warn("audit export failed", "type", e.cfg.Type, "event_id", event.ID, "error", err)
		}
	}
}

func (e *exporter) send(ctx context.Context, event *audit.Event) error {
	payload, contentType, err := e.payload(event)
	if err != nil {
		return err
	}
	switch e.cfg.Type {
	case "webhook":
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.URL, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", contentType)
		if e.cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+e.cfg.Token)
		}
		return e.do(req)
	case "splunk":
		body, _ := json.Marshal(map[string]any{"event": json.RawMessage(payload), "sourcetype": "clavis:audit"})
		if e.cfg.Format == "cef" {
			body, _ = json.Marshal(map[string]any{"event": string(payload), "sourcetype": "clavis:audit"})
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Splunk "+e.cfg.Token)
		return e.do(req)
	case "datadog":
		message := string(payload)
		body, _ := json.Marshal([]map[string]string{{"message": message, "ddsource": "clavis", "service": "clavis"}})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("DD-API-KEY", e.cfg.APIKey)
		return e.do(req)
	case "elastic":
		body := payload
		if e.cfg.Format == "cef" {
			body, _ = json.Marshal(map[string]string{"message": string(payload)})
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if e.cfg.APIKey != "" {
			req.Header.Set("Authorization", "ApiKey "+e.cfg.APIKey)
		}
		return e.do(req)
	case "syslog":
		network := e.cfg.Network
		if network == "" {
			network = "udp"
		}
		conn, err := net.DialTimeout(network, e.cfg.Address, 5*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		line := fmt.Sprintf("<14>1 %s clavis clavis - - - %s",
			event.CreatedAt.UTC().Format(time.RFC3339), string(payload))
		if len(line) > 60000 {
			line = line[:60000]
		}
		_, err = io.WriteString(conn, line)
		return err
	case "s3":
		return e.sendS3(ctx, event, payload)
	default:
		return fmt.Errorf("auditexport: unsupported type %q", e.cfg.Type)
	}
}

func (e *exporter) payload(event *audit.Event) ([]byte, string, error) {
	if e.cfg.Format == "cef" {
		return []byte(CEF(event)), "text/plain; charset=utf-8", nil
	}
	body, err := json.Marshal(event)
	return body, "application/json", err
}

func (e *exporter) do(req *http.Request) error {
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("auditexport: destination returned %d", resp.StatusCode)
	}
	return nil
}

// CEF renders an audit event in ArcSight CEF format.
func CEF(event *audit.Event) string {
	escape := func(value string) string {
		value = strings.ReplaceAll(value, `\`, `\\`)
		value = strings.ReplaceAll(value, "|", `\|`)
		return strings.ReplaceAll(value, "\n", " ")
	}
	ext := func(value string) string {
		value = strings.ReplaceAll(value, `\`, `\\`)
		value = strings.ReplaceAll(value, "=", `\=`)
		return strings.ReplaceAll(value, "\n", " ")
	}
	return fmt.Sprintf("CEF:0|Clavis|Clavis|0.2|%s|%s|5|rt=%d suser=%s src=%s outcome=%s cs1=%s cs1Label=Resource cs2=%s cs2Label=Details",
		escape(event.Action), escape(event.Action), event.CreatedAt.UnixMilli(),
		ext(event.Actor.Name), ext(event.IP), ext(event.Action),
		ext(event.Resource.Type+" "+event.Resource.Name), ext(detailsString(event.Details)))
}

func detailsString(details audit.Details) string {
	encoded, _ := json.Marshal(details)
	return string(encoded)
}

func (e *exporter) sendS3(ctx context.Context, event *audit.Event, payload []byte) error {
	now := time.Now().UTC()
	prefix := strings.Trim(e.cfg.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	extension := ".json"
	if e.cfg.Format == "cef" {
		extension = ".cef"
	}
	key := fmt.Sprintf("%s%s-%s%s", prefix, now.Format("20060102T150405Z"), event.ID, extension)
	var target string
	if e.cfg.Endpoint != "" {
		target = strings.TrimRight(e.cfg.Endpoint, "/") + "/" + url.PathEscape(e.cfg.Bucket) + "/" + key
	} else {
		target = "https://" + e.cfg.Bucket + ".s3." + e.cfg.Region + ".amazonaws.com/" + key
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	signAWSV4(request, payload, now, e.cfg)
	return e.do(request)
}

// signAWSV4 signs a request with the AWS Signature Version 4 algorithm.
func signAWSV4(req *http.Request, payload []byte, now time.Time, cfg config.AuditExporterConfig) {
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	payloadHash := sha256.Sum256(payload)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(payloadHash[:]))
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + hex.EncodeToString(payloadHash[:]) + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		http.MethodPut, req.URL.EscapedPath(), "", canonicalHeaders, signedHeaders,
		hex.EncodeToString(payloadHash[:]),
	}, "\n")
	scope := date + "/" + cfg.Region + "/s3/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	key := hmacSHA256([]byte("AWS4"+cfg.SecretKey), date)
	key = hmacSHA256(key, cfg.Region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cfg.AccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
