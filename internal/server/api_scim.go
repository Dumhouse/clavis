package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

type scimOrgKey struct{}

type scimMeta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created,omitempty"`
	LastModified time.Time `json:"lastModified,omitempty"`
	Location     string    `json:"location,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type scimName struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

type scimGroupRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type scimUser struct {
	Schemas     []string       `json:"schemas,omitempty"`
	ID          string         `json:"id,omitempty"`
	ExternalID  string         `json:"externalId,omitempty"`
	UserName    string         `json:"userName"`
	Name        *scimName      `json:"name,omitempty"`
	DisplayName string         `json:"displayName,omitempty"`
	Emails      []scimEmail    `json:"emails,omitempty"`
	Active      *bool          `json:"active,omitempty"`
	Groups      []scimGroupRef `json:"groups,omitempty"`
	Meta        *scimMeta      `json:"meta,omitempty"`
}

type scimGroup struct {
	Schemas     []string     `json:"schemas,omitempty"`
	ID          string       `json:"id,omitempty"`
	ExternalID  string       `json:"externalId,omitempty"`
	DisplayName string       `json:"displayName"`
	Members     []scimMember `json:"members,omitempty"`
	Meta        *scimMeta    `json:"meta,omitempty"`
}

type scimMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type scimList struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    any      `json:"Resources"`
}

type scimPatch struct {
	Schemas    []string `json:"schemas"`
	Operations []struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	} `json:"Operations"`
}

type scimTokenBody struct {
	ID         string     `json:"id"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type scimTokensOutput struct {
	Body struct {
		Tokens []scimTokenBody `json:"tokens"`
	}
}

type scimTokenOutput struct {
	Body struct {
		Token string        `json:"token"`
		Info  scimTokenBody `json:"info"`
	}
}

type scimTokenPath struct {
	Org string `path:"org"`
	ID  string `path:"id"`
}

func (s *Server) registerScimTokenRoutes() error {
	base := "/api/v1/orgs/{org}/scim/tokens"
	if err := s.registry.Declare(http.MethodGet, base, authz.SettingsManage, "list SCIM provisioning tokens"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-scim-tokens", Method: http.MethodGet, Path: base,
		Summary: "List SCIM tokens", Tags: []string{"scim"},
	}, func(ctx context.Context, in *orgPath) (*scimTokensOutput, error) {
		actor, _ := actorFrom(ctx)
		tokens, err := s.svc.ListScimTokens(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &scimTokensOutput{}
		out.Body.Tokens = []scimTokenBody{}
		for _, token := range tokens {
			out.Body.Tokens = append(out.Body.Tokens, scimTokenFromStore(token))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.SettingsManage, "create SCIM provisioning tokens"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-scim-token", Method: http.MethodPost, Path: base,
		Summary: "Create a SCIM token", Tags: []string{"scim"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *orgPath) (*scimTokenOutput, error) {
		actor, _ := actorFrom(ctx)
		result, err := s.svc.CreateScimToken(ctx, actor, in.Org, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &scimTokenOutput{}
		out.Body.Token = result.Token
		out.Body.Info = scimTokenFromStore(*result.Record)
		return out, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.SettingsManage, "revoke SCIM provisioning tokens"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "revoke-scim-token", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Revoke a SCIM token", Tags: []string{"scim"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *scimTokenPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RevokeScimToken(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func scimTokenFromStore(token store.ScimToken) scimTokenBody {
	return scimTokenBody{ID: token.ID, Prefix: token.Prefix, CreatedAt: token.CreatedAt, LastUsedAt: token.LastUsedAt}
}

// registerScimRoutes mounts the SCIM 2.0 API. Authentication is a dedicated
// bearer token scoped to one organization.
func (s *Server) registerScimRoutes() error {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/scim/v2/ServiceProviderConfig"},
		{http.MethodGet, "/scim/v2/Users"},
		{http.MethodPost, "/scim/v2/Users"},
		{http.MethodGet, "/scim/v2/Users/{id}"},
		{http.MethodPatch, "/scim/v2/Users/{id}"},
		{http.MethodPut, "/scim/v2/Users/{id}"},
		{http.MethodDelete, "/scim/v2/Users/{id}"},
		{http.MethodGet, "/scim/v2/Groups"},
		{http.MethodPost, "/scim/v2/Groups"},
		{http.MethodGet, "/scim/v2/Groups/{id}"},
		{http.MethodPatch, "/scim/v2/Groups/{id}"},
		{http.MethodDelete, "/scim/v2/Groups/{id}"},
	} {
		if err := s.registry.DeclarePublic(route.method, route.path, "SCIM clients authenticate with a dedicated org bearer token"); err != nil {
			return err
		}
	}

	s.router.Route("/scim/v2", func(r chi.Router) {
		r.Use(s.scimAuth)
		r.Get("/ServiceProviderConfig", s.scimServiceProviderConfig)
		r.Get("/Users", s.scimListUsers)
		r.Post("/Users", s.scimCreateUser)
		r.Get("/Users/{id}", s.scimGetUser)
		r.Patch("/Users/{id}", s.scimPatchUser)
		r.Put("/Users/{id}", s.scimPatchUser)
		r.Delete("/Users/{id}", s.scimDeleteUser)
		r.Get("/Groups", s.scimListGroups)
		r.Post("/Groups", s.scimCreateGroup)
		r.Get("/Groups/{id}", s.scimGetGroup)
		r.Patch("/Groups/{id}", s.scimPatchGroup)
		r.Delete("/Groups/{id}", s.scimDeleteGroup)
	})
	return nil
}

func (s *Server) scimAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		org, err := s.svc.AuthenticateScimToken(r.Context(), raw)
		if err != nil {
			scimError(w, http.StatusUnauthorized, "invalid SCIM token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scimOrgKey{}, org)))
	})
}

func scimOrg(r *http.Request) *store.Org {
	org, _ := r.Context().Value(scimOrgKey{}).(*store.Org)
	return org
}

func scimError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
		"detail":  detail,
		"status":  fmt.Sprintf("%d", status),
	})
}

func scimWrite(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) scimServiceProviderConfig(w http.ResponseWriter, _ *http.Request) {
	scimWrite(w, http.StatusOK, map[string]any{
		"schemas":               []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri":      "https://github.com/Dumhouse/clavis",
		"patch":                 map[string]any{"supported": true},
		"bulk":                  map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":                map[string]any{"supported": true, "maxResults": 200},
		"changePassword":        map[string]any{"supported": false},
		"sort":                  map[string]any{"supported": false},
		"etag":                  map[string]any{"supported": false},
		"authenticationSchemes": []map[string]any{{"type": "oauthbearertoken", "name": "SCIM bearer token", "description": "Organization-scoped token"}},
	})
}

func (s *Server) scimListUsers(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	members, err := s.store.ListMembers(r.Context(), org.ID)
	if err != nil {
		scimError(w, http.StatusInternalServerError, "could not list users")
		return
	}
	users := make([]scimUser, 0, len(members))
	for _, member := range members {
		user, err := s.store.GetUser(r.Context(), member.UserID)
		if err != nil {
			continue
		}
		users = append(users, scimUserFromStore(user, member.RoleName, scimBaseURL(s, org.Slug)))
	}
	scimWrite(w, http.StatusOK, scimList{
		Schemas:      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		TotalResults: len(users), StartIndex: 1, ItemsPerPage: len(users), Resources: users,
	})
}

func (s *Server) scimCreateUser(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	var in scimUser
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		scimError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email := strings.TrimSpace(in.UserName)
	if email == "" && len(in.Emails) > 0 {
		email = in.Emails[0].Value
	}
	normalized, err := authn.NormalizeEmail(email)
	if err != nil {
		scimError(w, http.StatusBadRequest, "a valid userName or email is required")
		return
	}
	name := scimDisplayName(in, normalized)
	user, err := s.store.GetUserByEmail(r.Context(), normalized)
	if err != nil {
		if err != store.ErrNotFound {
			scimError(w, http.StatusInternalServerError, "could not look up user")
			return
		}
		user = &store.User{
			ID: uuid.NewString(), Email: normalized, Name: name, EmailVerified: true,
			Preferences: `{"theme":"system","appearance":"classic"}`,
		}
		if err := s.store.CreateUser(r.Context(), user); err != nil {
			scimError(w, http.StatusConflict, "user could not be created")
			return
		}
	}
	role, err := s.store.GetRoleByName(r.Context(), org.ID, authz.RoleDeveloper)
	if err != nil {
		scimError(w, http.StatusInternalServerError, "default role missing")
		return
	}
	if err := s.store.ActivateMembership(r.Context(), org.ID, user.ID, role.ID); err != nil {
		scimError(w, http.StatusInternalServerError, "could not add membership")
		return
	}
	body := scimUserFromStore(user, role.Name, scimBaseURL(s, org.Slug))
	w.Header().Set("Location", body.Meta.Location)
	scimWrite(w, http.StatusCreated, body)
}

func (s *Server) scimGetUser(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	id := chi.URLParam(r, "id")
	user, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		scimError(w, http.StatusNotFound, "user not found")
		return
	}
	membership, err := s.store.GetMembership(r.Context(), org.ID, id)
	if err != nil {
		scimError(w, http.StatusNotFound, "user is not a member of this organization")
		return
	}
	body := scimUserFromStore(user, membership.RoleName, scimBaseURL(s, org.Slug))
	if membership.RemovedAt != nil || membership.Status != "active" {
		active := false
		body.Active = &active
	}
	scimWrite(w, http.StatusOK, body)
}

func (s *Server) scimPatchUser(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	id := chi.URLParam(r, "id")
	user, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		scimError(w, http.StatusNotFound, "user not found")
		return
	}
	var patch scimPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		scimError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	active := true
	name := user.Name
	for _, op := range patch.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		switch {
		case path == "active" || path == "" && strings.Contains(string(op.Value), "false"):
			var value bool
			if err := json.Unmarshal(op.Value, &value); err == nil {
				active = value
			}
		case strings.HasPrefix(path, "name") || path == "displayname":
			var value string
			if err := json.Unmarshal(op.Value, &value); err == nil && strings.TrimSpace(value) != "" {
				name = strings.TrimSpace(value)
			} else {
				var nested scimName
				if err := json.Unmarshal(op.Value, &nested); err == nil {
					name = scimDisplayName(scimUser{Name: &nested, DisplayName: user.Name}, user.Email)
				}
			}
		}
	}
	if name != "" && name != user.Name {
		if err := s.store.UpdateUserName(r.Context(), user.ID, name); err != nil {
			scimError(w, http.StatusInternalServerError, "could not update user")
			return
		}
	}
	if active {
		role, err := s.store.GetRoleByName(r.Context(), org.ID, authz.RoleDeveloper)
		if err == nil {
			_ = s.store.ActivateMembership(r.Context(), org.ID, user.ID, role.ID)
		}
	} else {
		_ = s.store.RemoveMember(r.Context(), org.ID, user.ID)
	}
	membership, _ := s.store.GetMembership(r.Context(), org.ID, user.ID)
	roleName := ""
	if membership != nil {
		roleName = membership.RoleName
	}
	body := scimUserFromStore(user, roleName, scimBaseURL(s, org.Slug))
	out := active
	body.Active = &out
	scimWrite(w, http.StatusOK, body)
}

func (s *Server) scimDeleteUser(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	id := chi.URLParam(r, "id")
	if err := s.store.RemoveMember(r.Context(), org.ID, id); err != nil {
		scimError(w, http.StatusNotFound, "user is not a member of this organization")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scimListGroups(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	groups, err := s.store.ListGroups(r.Context(), org.ID)
	if err != nil {
		scimError(w, http.StatusInternalServerError, "could not list groups")
		return
	}
	out := make([]scimGroup, 0, len(groups))
	for _, group := range groups {
		members, _ := s.store.ListGroupMembers(r.Context(), group.ID)
		out = append(out, scimGroupFromStore(group, members, scimBaseURL(s, org.Slug)))
	}
	scimWrite(w, http.StatusOK, scimList{
		Schemas:      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		TotalResults: len(out), StartIndex: 1, ItemsPerPage: len(out), Resources: out,
	})
}

func (s *Server) scimCreateGroup(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	var in scimGroup
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.DisplayName) == "" {
		scimError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	group := &store.Group{OrgID: org.ID, Name: strings.TrimSpace(in.DisplayName), ExternalID: in.ExternalID}
	if err := s.store.CreateGroup(r.Context(), group); err != nil {
		scimError(w, http.StatusConflict, "group could not be created")
		return
	}
	for _, member := range in.Members {
		_ = s.store.AddGroupMember(r.Context(), group.ID, member.Value)
	}
	members, _ := s.store.ListGroupMembers(r.Context(), group.ID)
	scimWrite(w, http.StatusCreated, scimGroupFromStore(*group, members, scimBaseURL(s, org.Slug)))
}

func (s *Server) scimGetGroup(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	group, err := s.store.GetGroup(r.Context(), org.ID, chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusNotFound, "group not found")
		return
	}
	members, _ := s.store.ListGroupMembers(r.Context(), group.ID)
	scimWrite(w, http.StatusOK, scimGroupFromStore(*group, members, scimBaseURL(s, org.Slug)))
}

func (s *Server) scimPatchGroup(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	group, err := s.store.GetGroup(r.Context(), org.ID, chi.URLParam(r, "id"))
	if err != nil {
		scimError(w, http.StatusNotFound, "group not found")
		return
	}
	var patch scimPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		scimError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	for _, op := range patch.Operations {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		switch {
		case path == "displayname":
			var value string
			if json.Unmarshal(op.Value, &value) == nil && strings.TrimSpace(value) != "" {
				_ = s.store.RenameGroup(r.Context(), org.ID, group.ID, strings.TrimSpace(value))
			}
		case strings.HasPrefix(path, "members"):
			var members []scimMember
			if json.Unmarshal(op.Value, &members) != nil {
				var single scimMember
				if json.Unmarshal(op.Value, &single) == nil && single.Value != "" {
					members = []scimMember{single}
				}
			}
			for _, member := range members {
				if strings.EqualFold(op.Op, "remove") {
					_ = s.store.RemoveGroupMember(r.Context(), group.ID, member.Value)
				} else {
					_ = s.store.AddGroupMember(r.Context(), group.ID, member.Value)
				}
			}
		}
	}
	updated, err := s.store.GetGroup(r.Context(), org.ID, group.ID)
	if err != nil {
		scimError(w, http.StatusInternalServerError, "could not load group")
		return
	}
	members, _ := s.store.ListGroupMembers(r.Context(), updated.ID)
	scimWrite(w, http.StatusOK, scimGroupFromStore(*updated, members, scimBaseURL(s, org.Slug)))
}

func (s *Server) scimDeleteGroup(w http.ResponseWriter, r *http.Request) {
	org := scimOrg(r)
	if err := s.store.DeleteGroup(r.Context(), org.ID, chi.URLParam(r, "id")); err != nil {
		scimError(w, http.StatusNotFound, "group not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func scimBaseURL(s *Server, orgSlug string) string {
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	return base + "/scim/v2"
}

func scimDisplayName(in scimUser, fallback string) string {
	if in.Name != nil {
		if in.Name.Formatted != "" {
			return in.Name.Formatted
		}
		combined := strings.TrimSpace(in.Name.GivenName + " " + in.Name.FamilyName)
		if combined != "" {
			return combined
		}
	}
	if strings.TrimSpace(in.DisplayName) != "" {
		return strings.TrimSpace(in.DisplayName)
	}
	return fallback
}

func scimUserFromStore(user *store.User, role, base string) scimUser {
	active := true
	return scimUser{
		Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		ID:          user.ID,
		UserName:    user.Email,
		DisplayName: user.Name,
		Name:        &scimName{Formatted: user.Name},
		Emails:      []scimEmail{{Value: user.Email, Primary: true}},
		Active:      &active,
		Groups:      []scimGroupRef{},
		Meta: &scimMeta{
			ResourceType: "User", Created: user.CreatedAt, LastModified: user.UpdatedAt,
			Location: base + "/Users/" + user.ID,
		},
	}
}

func scimGroupFromStore(group store.Group, members []store.GroupMember, base string) scimGroup {
	out := scimGroup{
		Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		ID:          group.ID,
		ExternalID:  group.ExternalID,
		DisplayName: group.Name,
		Members:     make([]scimMember, 0, len(members)),
		Meta: &scimMeta{
			ResourceType: "Group", Created: group.CreatedAt, LastModified: group.CreatedAt,
			Location: base + "/Groups/" + group.ID,
		},
	}
	for _, member := range members {
		out.Members = append(out.Members, scimMember{Value: member.UserID, Display: member.Name})
	}
	return out
}
