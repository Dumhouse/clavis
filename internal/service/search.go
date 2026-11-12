package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// KeyMatch is one secret key name found by SearchKeys.
type KeyMatch struct {
	Key          string
	Project      string
	ProjectName  string
	Environments []string
}

// maxSearchResults bounds SearchKeys output.
const maxSearchResults = 30

// SearchKeys finds secret key names containing query (case-insensitive)
// across the organization. Only environments where the actor may read key
// names are searched, values are never decrypted, and agents are excluded
// from staging and production like everywhere else.
func (s *Service) SearchKeys(ctx context.Context, actor authz.Actor, orgSlug, query string) ([]KeyMatch, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if len(needle) < 2 {
		return nil, nil
	}
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.OrgRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	projects, err := s.Store.ListProjects(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	var matches []KeyMatch
	for i := range projects {
		project := &projects[i]
		envs, err := s.Store.ListEnvironments(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		found := map[string]*KeyMatch{}
		if err := s.searchProject(ctx, actor, org, project, envs, needle, found); err != nil {
			return nil, err
		}
		for _, match := range found {
			matches = append(matches, *match)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		// Prefix matches first, then alphabetical.
		pi := strings.HasPrefix(strings.ToLower(matches[i].Key), needle)
		pj := strings.HasPrefix(strings.ToLower(matches[j].Key), needle)
		if pi != pj {
			return pi
		}
		if matches[i].Key != matches[j].Key {
			return matches[i].Key < matches[j].Key
		}
		return matches[i].Project < matches[j].Project
	})
	if len(matches) > maxSearchResults {
		matches = matches[:maxSearchResults]
	}
	return matches, nil
}

func (s *Service) searchProject(ctx context.Context, actor authz.Actor, org *store.Org, project *store.Project, envs []store.Environment, needle string, found map[string]*KeyMatch) error {
	var readable []*store.Environment
	for i := range envs {
		if s.authorizeSecretAccess(actor, org, project, &envs[i], authz.SecretsReadNames) == nil {
			readable = append(readable, &envs[i])
		}
	}
	if len(readable) == 0 {
		return nil
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return err
	}
	defer valueKey.Zero()
	indexKey.Zero()
	for _, env := range readable {
		items, err := s.Store.ListItems(ctx, env.ID)
		if err != nil {
			return err
		}
		for i := range items {
			name, err := s.decryptKey(valueKey, org, project, env, &items[i])
			if err != nil || !strings.Contains(strings.ToLower(name), needle) {
				continue
			}
			match := found[name]
			if match == nil {
				match = &KeyMatch{Key: name, Project: project.Slug, ProjectName: project.Name}
				found[name] = match
			}
			match.Environments = append(match.Environments, env.Slug)
		}
	}
	return nil
}
