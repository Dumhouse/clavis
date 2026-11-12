package service

import (
	"context"
	"fmt"
	"regexp"

	"github.com/dumhouse/clavis/internal/authz"
)

var referencePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_.-]*)\}`)
var referenceSetPattern = regexp.MustCompile(`\$\{set:([A-Za-z_][A-Za-z0-9_.-]*)\}`)

// ResolveSecrets returns the environment with `${KEY}` references expanded.
// Cycles and unknown references are rejected, and the caller must hold
// secrets:read-values for the environment.
func (s *Service) ResolveSecrets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, channel string, meta AuditMeta) ([]Secret, string, error) {
	secrets, etag, err := s.ListSecrets(ctx, actor, orgSlug, projectSlug, envSlug, true, channel, meta)
	if err != nil {
		return nil, "", err
	}
	if err := resolveSecretReferences(secrets); err != nil {
		return nil, "", err
	}
	return secrets, etag, nil
}

// resolveSecretReferences expands `${KEY}` references in place.
func resolveSecretReferences(secrets []Secret) error {
	byKey := make(map[string]*Secret, len(secrets))
	for i := range secrets {
		byKey[secrets[i].Key] = &secrets[i]
	}
	resolved := make(map[string]string, len(secrets))
	var resolve func(key string, trail map[string]bool) (string, error)
	resolve = func(key string, trail map[string]bool) (string, error) {
		if value, ok := resolved[key]; ok {
			return value, nil
		}
		secret, ok := byKey[key]
		if !ok {
			return "", fmt.Errorf("%w: reference to unknown key %q", ErrValidation, key)
		}
		if trail[key] {
			return "", fmt.Errorf("%w: reference cycle includes %q", ErrValidation, key)
		}
		if secret.Kind == "file" {
			resolved[key] = secret.Value
			return secret.Value, nil
		}
		trail[key] = true
		value := secret.Value
		var firstErr error
		value = referencePattern.ReplaceAllStringFunc(value, func(match string) string {
			if firstErr != nil {
				return match
			}
			sub := referencePattern.FindStringSubmatch(match)
			replacement, err := resolve(sub[1], trail)
			if err != nil {
				firstErr = err
				return match
			}
			return replacement
		})
		delete(trail, key)
		if firstErr != nil {
			return "", firstErr
		}
		resolved[key] = value
		return value, nil
	}
	for i := range secrets {
		if secrets[i].Kind == "file" {
			continue
		}
		value, err := resolve(secrets[i].Key, map[string]bool{})
		if err != nil {
			return err
		}
		secrets[i].Value = value
	}
	return nil
}
