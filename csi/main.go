// Command clavis-csi is a Secrets Store CSI driver node plugin that writes
// resolved Clavis secrets as files for pod volumes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	driverName = "clavis.csi.dumhouse.github.io"
	version    = "0.1.0"
)

type identityServer struct {
	csi.UnimplementedIdentityServer
}

func (identityServer) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{Name: driverName, VendorVersion: version}, nil
}

func (identityServer) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{Capabilities: []*csi.PluginCapability{
		{Type: &csi.PluginCapability_Service_{Service: &csi.PluginCapability_Service{Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS}}},
	}}, nil
}

func (identityServer) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

type nodeServer struct {
	csi.UnimplementedNodeServer
	nodeID string
	server string
	token  string
}

func (n *nodeServer) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{NodeId: n.nodeID}, nil
}

func (n *nodeServer) NodeGetCapabilities(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{}, nil
}

func (n *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.GetTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_path is required")
	}
	values, err := n.fetch(ctx, req.GetVolumeContext())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve secrets: %v", err)
	}
	if err := os.MkdirAll(req.GetTargetPath(), 0o750); err != nil {
		return nil, status.Errorf(codes.Internal, "create target: %v", err)
	}
	for key, value := range values {
		name := filepath.Base(key)
		if name == "." || name == "" || name != key || strings.ContainsAny(name, `/\\`) {
			return nil, status.Errorf(codes.InvalidArgument, "invalid secret file name %q", key)
		}
		path := filepath.Join(req.GetTargetPath(), name)
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			return nil, status.Errorf(codes.Internal, "write %s: %v", name, err)
		}
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

func (n *nodeServer) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.GetTargetPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_path is required")
	}
	entries, err := os.ReadDir(req.GetTargetPath())
	if err == nil {
		for _, entry := range entries {
			_ = os.Remove(filepath.Join(req.GetTargetPath(), entry.Name()))
		}
	}
	_ = os.Remove(req.GetTargetPath())
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (n *nodeServer) NodeStageVolume(context.Context, *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *nodeServer) NodeUnstageVolume(context.Context, *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return &csi.NodeUnstageVolumeResponse{}, nil
}

type secretResponse struct {
	Secrets []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Kind  string `json:"kind"`
	} `json:"secrets"`
}

func (n *nodeServer) fetch(ctx context.Context, volumeContext map[string]string) (map[string]string, error) {
	org := firstNonEmpty(volumeContext["org"], os.Getenv("CLAVIS_ORG"))
	project := firstNonEmpty(volumeContext["project"], os.Getenv("CLAVIS_PROJECT"))
	env := firstNonEmpty(volumeContext["environment"], os.Getenv("CLAVIS_ENV"))
	if org == "" || project == "" || env == "" {
		return nil, fmt.Errorf("org, project, and environment are required")
	}
	endpoint := strings.TrimRight(firstNonEmpty(volumeContext["server"], n.server, os.Getenv("CLAVIS_SERVER")), "/") +
		"/api/v1/orgs/" + url.PathEscape(org) + "/projects/" + url.PathEscape(project) +
		"/environments/" + url.PathEscape(env) + "/secrets:resolve?channel=csi"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+firstNonEmpty(n.token, os.Getenv("CLAVIS_TOKEN")))
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Clavis returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var result secretResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, key := range strings.Split(volumeContext["keys"], ",") {
		if key = strings.TrimSpace(key); key != "" {
			allowed[key] = true
		}
	}
	values := make(map[string]string, len(result.Secrets))
	for _, secret := range result.Secrets {
		if len(allowed) > 0 && !allowed[secret.Key] {
			continue
		}
		values[secret.Key] = secret.Value
	}
	return values, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func main() {
	var endpoint, nodeID string
	flag.StringVar(&endpoint, "endpoint", "/csi/csi.sock", "CSI unix socket")
	flag.StringVar(&nodeID, "node-id", "", "node identifier")
	flag.Parse()
	if nodeID == "" {
		nodeID, _ = os.Hostname()
	}
	_ = os.Remove(endpoint)
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o750); err != nil {
		panic(err)
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		panic(err)
	}
	_ = os.Chmod(endpoint, 0o660)
	server := grpc.NewServer()
	csi.RegisterIdentityServer(server, identityServer{})
	csi.RegisterNodeServer(server, &nodeServer{
		nodeID: nodeID, server: os.Getenv("CLAVIS_SERVER"), token: os.Getenv("CLAVIS_TOKEN"),
	})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		server.GracefulStop()
	}()
	fmt.Fprintf(os.Stderr, "clavis-csi listening on %s\n", endpoint)
	if err := server.Serve(listener); err != nil {
		panic(err)
	}
}
