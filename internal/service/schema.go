package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// Schema types describe how a secret value is validated.
const (
	SchemaTypeString = "string"
	SchemaTypeURL    = "url"
	SchemaTypeInt    = "int"
	SchemaTypeBool   = "bool"
	SchemaTypeJSON   = "json"
	SchemaTypeEnum   = "enum"
)

var schemaTypes = map[string]bool{
	SchemaTypeString: true,
	SchemaTypeURL:    true,
	SchemaTypeInt:    true,
	SchemaTypeBool:   true,
	SchemaTypeJSON:   true,
	SchemaTypeEnum:   true,
}

// SchemaEntry describes one project key: its type, kind, required environment
// types, validation pattern, documentation, and enum values.
type SchemaEntry struct {
	Key         string
	Type        string
	Kind        string
	Required    map[string]bool
	Pattern     string
	Description string
	Example     string
	Enum        []string
}

// ListSchema returns a project's schema.
func (s *Service) ListSchema(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string) ([]SchemaEntry, error) {
	_, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsRead)
	if err != nil {
		return nil, err
	}
	items, err := s.Store.ListSecretSchema(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	out := make([]SchemaEntry, 0, len(items))
	for i := range items {
		entry, err := schemaEntryFromStore(&items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// ReplaceSchema replaces a project's entire schema after validating every
// entry. The schema is project-level configuration, so it needs
// projects:manage.
func (s *Service) ReplaceSchema(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, entries []SchemaEntry, meta AuditMeta) ([]SchemaEntry, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsManage)
	if err != nil {
		return nil, err
	}
	if len(entries) > 1000 {
		return nil, fmt.Errorf("%w: a project may define at most 1000 schema entries", ErrValidation)
	}
	seen := make(map[string]bool, len(entries))
	rows := make([]store.SecretSchema, 0, len(entries))
	normalized := make([]SchemaEntry, 0, len(entries))
	for i := range entries {
		entry, err := normalizeSchemaEntry(entries[i])
		if err != nil {
			return nil, err
		}
		if seen[entry.Key] {
			return nil, fmt.Errorf("%w: duplicate schema key %q", ErrValidation, entry.Key)
		}
		seen[entry.Key] = true
		required, err := json.Marshal(entry.Required)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid required map for %q", ErrValidation, entry.Key)
		}
		enumValues, err := json.Marshal(entry.Enum)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid enum values for %q", ErrValidation, entry.Key)
		}
		rows = append(rows, store.SecretSchema{
			Key: entry.Key, Type: entry.Type, Kind: entry.Kind,
			Required: string(required), Pattern: entry.Pattern,
			Description: entry.Description, Example: entry.Example,
			EnumValues: string(enumValues),
		})
		normalized = append(normalized, entry)
	}
	if err := s.Store.ReplaceSecretSchema(ctx, project.ID, rows); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "schema.update",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Count: len(normalized)}, meta); err != nil {
		s.Logger.Error("audit schema.update failed", "project_id", project.ID, "error", err)
	}
	return normalized, nil
}

// DeleteSchemaKey removes one key from a project's schema.
func (s *Service) DeleteSchemaKey(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, key string, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.ProjectsManage)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteSecretSchema(ctx, project.ID, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "schema.delete",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{SchemaKey: key}, meta); err != nil {
		s.Logger.Error("audit schema.delete failed", "project_id", project.ID, "error", err)
	}
	return nil
}

func normalizeSchemaEntry(entry SchemaEntry) (SchemaEntry, error) {
	entry.Key = strings.TrimSpace(entry.Key)
	if entry.Key == "" || len(entry.Key) > maxKeyLength || !keyPattern.MatchString(entry.Key) {
		return SchemaEntry{}, fmt.Errorf("%w: schema key %q must start with a letter or underscore and contain only letters, digits, '_', '.', or '-'", ErrValidation, entry.Key)
	}
	entry.Type = strings.ToLower(strings.TrimSpace(entry.Type))
	if entry.Type == "" {
		entry.Type = SchemaTypeString
	}
	if !schemaTypes[entry.Type] {
		return SchemaEntry{}, fmt.Errorf("%w: schema type for %q must be string, url, int, bool, json, or enum", ErrValidation, entry.Key)
	}
	entry.Kind = strings.ToLower(strings.TrimSpace(entry.Kind))
	if entry.Kind == "" {
		entry.Kind = "secret"
	}
	switch entry.Kind {
	case "secret", "variable", "file":
	default:
		return SchemaEntry{}, fmt.Errorf("%w: schema kind for %q must be secret, variable, or file", ErrValidation, entry.Key)
	}
	if entry.Required == nil {
		entry.Required = map[string]bool{}
	}
	for envType := range entry.Required {
		if !validEnvType(envType) {
			return SchemaEntry{}, fmt.Errorf("%w: required for %q must use environment types development, preview, staging, or production", ErrValidation, entry.Key)
		}
	}
	if len(entry.Pattern) > 512 {
		return SchemaEntry{}, fmt.Errorf("%w: pattern for %q exceeds 512 bytes", ErrValidation, entry.Key)
	}
	if entry.Pattern != "" {
		if _, err := regexp.Compile(entry.Pattern); err != nil {
			return SchemaEntry{}, fmt.Errorf("%w: pattern for %q is not a valid regular expression", ErrValidation, entry.Key)
		}
	}
	if len(entry.Description) > 512 {
		return SchemaEntry{}, fmt.Errorf("%w: description for %q exceeds 512 bytes", ErrValidation, entry.Key)
	}
	if len(entry.Example) > 1024 {
		return SchemaEntry{}, fmt.Errorf("%w: example for %q exceeds 1024 bytes", ErrValidation, entry.Key)
	}
	if entry.Type == SchemaTypeEnum {
		if len(entry.Enum) == 0 {
			return SchemaEntry{}, fmt.Errorf("%w: enum type for %q needs at least one value", ErrValidation, entry.Key)
		}
		seen := make(map[string]bool, len(entry.Enum))
		for i := range entry.Enum {
			entry.Enum[i] = strings.TrimSpace(entry.Enum[i])
			if entry.Enum[i] == "" {
				return SchemaEntry{}, fmt.Errorf("%w: enum values for %q cannot be empty", ErrValidation, entry.Key)
			}
			if len(entry.Enum[i]) > 256 {
				return SchemaEntry{}, fmt.Errorf("%w: enum values for %q must be at most 256 bytes", ErrValidation, entry.Key)
			}
			if seen[entry.Enum[i]] {
				return SchemaEntry{}, fmt.Errorf("%w: enum values for %q must be unique", ErrValidation, entry.Key)
			}
			seen[entry.Enum[i]] = true
		}
	} else if len(entry.Enum) > 0 {
		return SchemaEntry{}, fmt.Errorf("%w: only enum schemas may define enum values (%q)", ErrValidation, entry.Key)
	}
	return entry, nil
}

func schemaEntryFromStore(row *store.SecretSchema) (SchemaEntry, error) {
	entry := SchemaEntry{
		Key: row.Key, Type: row.Type, Kind: row.Kind, Pattern: row.Pattern,
		Description: row.Description, Example: row.Example,
	}
	if row.Required != "" {
		if err := json.Unmarshal([]byte(row.Required), &entry.Required); err != nil {
			return SchemaEntry{}, fmt.Errorf("service: decode schema required for %s: %w", row.Key, err)
		}
	}
	if entry.Required == nil {
		entry.Required = map[string]bool{}
	}
	if row.EnumValues != "" {
		if err := json.Unmarshal([]byte(row.EnumValues), &entry.Enum); err != nil {
			return SchemaEntry{}, fmt.Errorf("service: decode schema enum for %s: %w", row.Key, err)
		}
	}
	return entry, nil
}

func (s *Service) schemaEntries(ctx context.Context, projectID string) (map[string]*SchemaEntry, error) {
	rows, err := s.Store.ListSecretSchema(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*SchemaEntry, len(rows))
	for i := range rows {
		entry, err := schemaEntryFromStore(&rows[i])
		if err != nil {
			return nil, err
		}
		out[entry.Key] = &entry
	}
	return out, nil
}

// validateValue checks a plaintext value against a schema entry. Empty values
// are allowed; required-key presence is enforced by `clavis check`.
func (entry *SchemaEntry) ValidateValue(value string) error {
	if value == "" {
		return nil
	}
	switch entry.Type {
	case "", SchemaTypeString:
	case SchemaTypeURL:
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("%w: %q must be an absolute URL", ErrValidation, entry.Key)
		}
	case SchemaTypeInt:
		if _, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err != nil {
			return fmt.Errorf("%w: %q must be an integer", ErrValidation, entry.Key)
		}
	case SchemaTypeBool:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "false", "1", "0", "yes", "no":
		default:
			return fmt.Errorf("%w: %q must be a boolean", ErrValidation, entry.Key)
		}
	case SchemaTypeJSON:
		if !json.Valid([]byte(value)) {
			return fmt.Errorf("%w: %q must be valid JSON", ErrValidation, entry.Key)
		}
	case SchemaTypeEnum:
		for _, allowed := range entry.Enum {
			if value == allowed {
				return nil
			}
		}
		return fmt.Errorf("%w: %q must be one of %s", ErrValidation, entry.Key, strings.Join(entry.Enum, ", "))
	}
	if entry.Pattern != "" {
		matched, err := regexp.MatchString(entry.Pattern, value)
		if err != nil {
			return fmt.Errorf("%w: schema pattern for %q is invalid", ErrValidation, entry.Key)
		}
		if !matched {
			return fmt.Errorf("%w: %q does not match the required pattern", ErrValidation, entry.Key)
		}
	}
	return nil
}

func (s *Service) validateAgainstSchema(ctx context.Context, project *store.Project, in *SecretInput) error {
	entries, err := s.schemaEntries(ctx, project.ID)
	if err != nil {
		return err
	}
	return validateAgainstSchemaEntries(entries, in)
}

func validateAgainstSchemaEntries(entries map[string]*SchemaEntry, in *SecretInput) error {
	entry := entries[in.Key]
	if entry == nil {
		return nil
	}
	if entry.Kind == "file" || in.Kind == "file" {
		return nil
	}
	return entry.ValidateValue(in.Value)
}

// RequiredIn reports whether the key is required for an environment type.
func (entry *SchemaEntry) RequiredIn(envType string) bool {
	return entry.Required[envType]
}
