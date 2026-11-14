package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/updatecheck"
	"github.com/dumhouse/clavis/internal/version"
)

// updateStatus is the result of the daily release check, kept in memory.
type updateStatus struct {
	mu        sync.RWMutex
	latest    string
	checkedAt time.Time
	url       string // releases endpoint; tests point it at a fake
}

func (u *updateStatus) get() (string, time.Time) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.latest, u.checkedAt
}

func (u *updateStatus) check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	latest, err := updatecheck.Latest(ctx, nil, u.url)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checkedAt = time.Now().UTC()
	if err == nil {
		u.latest = latest
	}
	return err
}

// WithReleasesURL points the update check at another endpoint (tests).
func WithReleasesURL(url string) Option {
	return func(s *Server) { s.updates.url = url }
}

// updateLoop checks for a new release shortly after start and then once a day.
// Failures (offline, air-gapped, rate limited) are logged at debug level and
// retried an hour later.
func (s *Server) updateLoop(ctx context.Context) {
	if !s.cfg.Server.UpdateCheck {
		return
	}
	wait := time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := s.updates.check(ctx); err != nil {
			s.logger.Debug("update check failed", "err", err)
			wait = time.Hour
			continue
		}
		if latest, _ := s.updates.get(); updatecheck.Newer(latest, version.Version) {
			s.logger.Info("a new Clavis release is available", "current", version.Version, "latest", latest)
		}
		wait = updatecheck.Interval
	}
}

// UpdateBody tells the web UI whether this server can be updated and how.
type UpdateBody struct {
	Enabled         bool                `json:"enabled" doc:"False when the server's update check is turned off."`
	Current         string              `json:"current"`
	Latest          string              `json:"latest,omitempty"`
	UpdateAvailable bool                `json:"update_available"`
	CheckedAt       *time.Time          `json:"checked_at,omitempty"`
	ReleaseURL      string              `json:"release_url,omitempty"`
	Install         updatecheck.Install `json:"install"`
}

type updateOutput struct {
	Body UpdateBody
}

func (s *Server) registerUpdateRoutes() error {
	if err := s.registry.DeclareAuthenticated(http.MethodGet, "/api/v1/system/update", "signed-in users see whether a newer release exists; the web UI shows it to owners and admins"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-update-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/update",
		Summary:     "Whether a newer Clavis release exists, and how to update this server",
		Tags:        []string{"system"},
	}, func(_ context.Context, _ *struct{}) (*updateOutput, error) {
		latest, checkedAt := s.updates.get()
		env := updatecheck.CurrentEnv()
		env.Version = version.Version
		env.Latest = latest
		body := UpdateBody{
			Enabled:         s.cfg.Server.UpdateCheck,
			Current:         version.Version,
			Latest:          latest,
			UpdateAvailable: updatecheck.Newer(latest, version.Version),
			Install:         updatecheck.Detect(env),
		}
		if !checkedAt.IsZero() {
			body.CheckedAt = &checkedAt
		}
		if latest != "" {
			body.ReleaseURL = "https://github.com/Dumhouse/clavis/releases/tag/v" + latest
		}
		return &updateOutput{Body: body}, nil
	})
	return nil
}

// CheckForUpdates runs the release check now (the loop runs it daily).
func (s *Server) CheckForUpdates(ctx context.Context) error {
	return s.updates.check(ctx)
}
