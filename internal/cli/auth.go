package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type whoAmIResponse struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Orgs []struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"orgs"`
}

func newLoginCommand(opts *Options) *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with a browser (device code)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, opts, !noBrowser)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "do not open the browser automatically")
	return cmd
}

func runLogin(cmd *cobra.Command, opts *Options, open bool) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	out := opts.output()
	resolved, err := opts.resolveContext(false)
	if err != nil {
		return err
	}
	c, err := resolved.newAnonymousClient()
	if err != nil {
		return err
	}

	var start deviceCodeResponse
	if err := c.Post(ctx, "/api/v1/auth/device/code", map[string]any{}, &start); err != nil {
		return err
	}
	out.Info("")
	out.Info("  Open:  %s", start.VerificationURI)
	out.Info("  Code:  %s", start.UserCode)
	out.Info("")
	out.Info("Waiting for approval…")
	if open {
		openBrowser(start.VerificationURI)
	}

	interval := time.Duration(start.Interval) * time.Second
	if interval < time.Second {
		interval = 2 * time.Second
	}
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			return errors.New("login timed out. Run `clavis login` again")
		}
		var poll struct {
			Status    string    `json:"status"`
			Token     string    `json:"token"`
			CSRF      string    `json:"csrf_token"`
			ExpiresAt time.Time `json:"expires_at"`
			User      *struct {
				Email string `json:"email"`
				Name  string `json:"name"`
			} `json:"user"`
		}
		if err := c.Post(ctx, "/api/v1/auth/device/token", map[string]string{"device_code": start.DeviceCode}, &poll); err != nil {
			return err
		}
		if poll.Status == "pending" {
			continue
		}
		if poll.Token == "" {
			return errors.New("login was not approved")
		}
		if err := credentialStore().Set(resolved.Server, poll.Token); err != nil {
			return err
		}
		name := ""
		if poll.User != nil {
			name = poll.User.Name
			if name == "" {
				name = poll.User.Email
			}
		}
		out.Success("Signed in to %s as %s", resolved.Server, name)
		out.Info("Token stored in %s.", credentialStore().Description())
		return nil
	}
}

func openBrowser(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}

func newLogoutCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out and delete the saved token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Token != "" {
				if c, err := resolved.newClient(); err == nil {
					_ = c.Post(cmd.Context(), "/api/v1/auth/logout", nil, nil)
				}
			}
			if err := credentialStore().Delete(resolved.Server); err != nil {
				return err
			}
			out.Success("Signed out of %s", resolved.Server)
			return nil
		},
	}
}

func newWhoamiCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in user and organizations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var me whoAmIResponse
			if err := c.Get(cmd.Context(), "/api/v1/auth/me", &me); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(me)
			}
			out.Info("%s <%s>", me.User.Name, me.User.Email)
			out.Info("server: %s", resolved.Server)
			if len(me.Orgs) > 0 {
				rows := make([][]string, 0, len(me.Orgs))
				for _, org := range me.Orgs {
					rows = append(rows, []string{org.Slug, org.Name, org.Role})
				}
				out.Table([]string{"ORG", "NAME", "ROLE"}, rows)
			}
			return nil
		},
	}
}

func newInitCommand(opts *Options) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Link this directory to a project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, opts, projectName)
		},
	}
	cmd.Flags().StringVar(&projectName, "project-name", "", "create a project with this name if none exist")
	return cmd
}

func newLinkCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "link",
		Short: "Change the project linked to this directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, opts, "")
		},
	}
}

func newUnlinkCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Remove the .clavis.yaml in this directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := projectConfigName
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("no %s in this directory", projectConfigName)
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			opts.output().Success("Removed %s", path)
			return nil
		},
	}
}

func runInit(cmd *cobra.Command, opts *Options, projectName string) error {
	ctx := cmd.Context()
	out := opts.output()
	resolved, err := opts.resolveContext(false)
	if err != nil {
		return err
	}
	c, err := resolved.newClient()
	if err != nil {
		return err
	}

	var orgList struct {
		Orgs []struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"orgs"`
	}
	if err := c.Get(ctx, "/api/v1/orgs", &orgList); err != nil {
		return err
	}
	if len(orgList.Orgs) == 0 {
		return errors.New("you do not belong to an organization yet; create one with `clavis orgs create` or the web UI")
	}
	orgIdx := indexBySlug(resolved.Org, orgSlugs(orgList.Orgs))
	if orgIdx < 0 {
		if len(orgList.Orgs) == 1 {
			orgIdx = 0
		} else {
			labels := make([]string, len(orgList.Orgs))
			for i, org := range orgList.Orgs {
				labels[i] = fmt.Sprintf("%s (%s)", org.Name, org.Slug)
			}
			orgIdx, err = promptChoice("Organization:", labels, 0)
			if err != nil {
				return err
			}
		}
	}
	orgSlug := orgList.Orgs[orgIdx].Slug

	var projectList struct {
		Projects []struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := c.Get(ctx, "/api/v1/orgs/"+url.PathEscape(orgSlug)+"/projects", &projectList); err != nil {
		return err
	}
	if len(projectList.Projects) == 0 {
		name := projectName
		if name == "" {
			name, err = promptLine("No projects yet. New project name: ")
			if err != nil {
				return err
			}
		}
		if name == "" {
			return errors.New("project name is required")
		}
		var created struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		body := map[string]string{"name": name, "slug": slugify(name)}
		if err := c.Post(ctx, "/api/v1/orgs/"+url.PathEscape(orgSlug)+"/projects", body, &created); err != nil {
			return err
		}
		projectList.Projects = append(projectList.Projects, struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}{Slug: created.Slug, Name: created.Name})
	}

	projectIdx := indexBySlug(resolved.Project, projectSlugs(projectList.Projects))
	if projectIdx < 0 {
		if len(projectList.Projects) == 1 {
			projectIdx = 0
		} else {
			labels := make([]string, len(projectList.Projects))
			for i, project := range projectList.Projects {
				labels[i] = fmt.Sprintf("%s (%s)", project.Name, project.Slug)
			}
			projectIdx, err = promptChoice("Project:", labels, 0)
			if err != nil {
				return err
			}
		}
	}
	projectSlug := projectList.Projects[projectIdx].Slug

	var detail struct {
		Environments []struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"environments"`
	}
	if err := c.Get(ctx, "/api/v1/orgs/"+url.PathEscape(orgSlug)+"/projects/"+url.PathEscape(projectSlug), &detail); err != nil {
		return err
	}
	envSlug := "dev"
	if len(detail.Environments) > 0 {
		envs := make([]string, len(detail.Environments))
		for i, env := range detail.Environments {
			envs[i] = env.Slug
		}
		envSlug = envs[0]
		if index := indexBySlug(resolved.Env, envs); index >= 0 {
			envSlug = envs[index]
		} else {
			labels := make([]string, len(detail.Environments))
			for i, env := range detail.Environments {
				labels[i] = fmt.Sprintf("%s (%s, %s)", env.Name, env.Slug, env.Type)
			}
			idx, err := promptChoice("Default environment:", labels, 0)
			if err != nil {
				return err
			}
			envSlug = detail.Environments[idx].Slug
		}
	}

	cfg := &projectConfig{
		Server:  resolved.Server,
		Org:     orgSlug,
		Project: projectSlug,
		Env:     envSlug,
		Path:    "/",
		Cache:   true,
	}
	path, err := writeProjectConfig(".", cfg)
	if err != nil {
		return err
	}
	out.Success("Linked this directory to %s/%s (%s)", orgSlug, projectSlug, envSlug)
	out.Info("Wrote %s", path)
	out.Info("Next: clavis secrets ls")
	return nil
}

func orgSlugs(orgs []struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}) []string {
	out := make([]string, len(orgs))
	for i := range orgs {
		out[i] = orgs[i].Slug
	}
	return out
}

func projectSlugs(projects []struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}) []string {
	out := make([]string, len(projects))
	for i := range projects {
		out[i] = projects[i].Slug
	}
	return out
}

func indexBySlug(slug string, options []string) int {
	if slug == "" {
		return -1
	}
	for i, option := range options {
		if option == slug {
			return i
		}
	}
	return -1
}

func slugify(value string) string {
	out := make([]rune, 0, len(value))
	dash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			dash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r-'A'+'a')
			dash = false
		default:
			if !dash && len(out) > 0 {
				out = append(out, '-')
				dash = true
			}
		}
	}
	return string(out)
}
