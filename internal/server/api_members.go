package server

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/email"
	"github.com/dumhouse/clavis/pkg/problem"
)

type memberBody struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type inviteBody struct {
	ID        string    `json:"id"`
	Email     string    `json:"email,omitempty"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	URL       string    `json:"url,omitempty"`
}

type membersOutput struct {
	Body struct {
		Members []memberBody `json:"members"`
		Invites []inviteBody `json:"invites"`
	}
}

type createInviteInput struct {
	Org  string `path:"org"`
	Body struct {
		Email         string `json:"email,omitempty" format:"email"`
		Role          string `json:"role,omitempty" default:"Developer"`
		ExpiresInDays int    `json:"expires_in_days,omitempty" minimum:"1" maximum:"30" default:"7"`
	}
}

type inviteOutput struct {
	Body inviteBody
}

type memberPath struct {
	Org    string `path:"org"`
	User   string `path:"user"`
	Reauth string `header:"X-Clavis-Reauth"`
}

type invitePath struct {
	Org string `path:"org"`
	ID  string `path:"id"`
}

type updateMemberInput struct {
	Org  string `path:"org"`
	User string `path:"user"`
	Body struct {
		Role string `json:"role"`
	}
}

type tokenPath struct {
	Token string `path:"token" maxLength:"256"`
}

type inviteInfoOutput struct {
	Body struct {
		Org       string    `json:"org"`
		OrgSlug   string    `json:"org_slug"`
		Role      string    `json:"role"`
		Email     string    `json:"email,omitempty"`
		ExpiresAt time.Time `json:"expires_at"`
	}
}

type acceptInviteInput struct {
	Token string `path:"token"`
	Body  struct {
		Name     string `json:"name" minLength:"1" maxLength:"100"`
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"12" maxLength:"1024"`
	}
}

func (s *Server) registerMemberRoutes() error {
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/members", authz.MembersManage, "list members and pending invites"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-members", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/members",
		Summary: "List members and pending invites", Tags: []string{"members"},
	}, func(ctx context.Context, in *orgPath) (*membersOutput, error) {
		actor, _ := actorFrom(ctx)
		members, err := s.svc.ListMembers(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		invites, err := s.svc.ListInvites(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &membersOutput{}
		for _, member := range members {
			out.Body.Members = append(out.Body.Members, memberBody{
				UserID: member.UserID, Email: member.Email, Name: member.Name,
				Role: member.RoleName, JoinedAt: member.CreatedAt,
			})
		}
		for _, invite := range invites {
			out.Body.Invites = append(out.Body.Invites, inviteBody{
				ID: invite.ID, Email: invite.Email, Role: invite.RoleName,
				ExpiresAt: invite.ExpiresAt, CreatedAt: invite.CreatedAt,
			})
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/invites", authz.MembersManage, "create invites"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-invite", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/invites",
		Summary: "Create an invite link", Tags: []string{"members"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createInviteInput) (*inviteOutput, error) {
		actor, _ := actorFrom(ctx)
		ttl := time.Duration(in.Body.ExpiresInDays) * 24 * time.Hour
		result, err := s.svc.CreateInvite(ctx, actor, in.Org, in.Body.Email, in.Body.Role, ttl, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		inviteURL := s.cfg.Server.BaseURL + "/join?token=" + url.QueryEscape(result.Token)
		if s.mailer != nil && result.Invite.Email != "" {
			if org, err := s.store.GetOrgBySlug(ctx, in.Org); err == nil {
				msg := email.InviteMessage(result.Invite.Email, org.Name, result.Invite.RoleName, inviteURL)
				if err := s.mailer.Send(ctx, msg); err != nil {
					s.logger.Error("send invite email failed", "invite_id", result.Invite.ID, "error", err)
				}
			}
		}
		return &inviteOutput{Body: inviteBody{
			ID: result.Invite.ID, Email: result.Invite.Email, Role: result.Invite.RoleName,
			ExpiresAt: result.Invite.ExpiresAt, CreatedAt: result.Invite.CreatedAt,
			URL: inviteURL,
		}}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/invites/{id}", authz.MembersManage, "revoke invites"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "revoke-invite", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}/invites/{id}",
		Summary: "Revoke an invite", Tags: []string{"members"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *invitePath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RevokeInvite(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, "/api/v1/orgs/{org}/members/{user}", authz.MembersManage, "change member roles"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-member", Method: http.MethodPatch, Path: "/api/v1/orgs/{org}/members/{user}",
		Summary: "Change a member's role", Tags: []string{"members"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *updateMemberInput) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.UpdateMemberRole(ctx, actor, in.Org, in.User, in.Body.Role, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/members/{user}", authz.MembersManage, "remove members"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "remove-member", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}/members/{user}",
		Summary: "Remove a member", Tags: []string{"members"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *memberPath) (*emptyOutput, error) {
		if err := s.requireReauth(ctx, in.Reauth); err != nil {
			return nil, err
		}
		actor, _ := actorFrom(ctx)
		if err := s.svc.RemoveMember(ctx, actor, in.Org, in.User, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.DeclarePublic(http.MethodGet, "/api/v1/invites/{token}", "the invite token is the capability for the join page"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "inspect-invite", Method: http.MethodGet, Path: "/api/v1/invites/{token}",
		Summary: "Inspect an invite link", Tags: []string{"members"},
	}, func(ctx context.Context, in *tokenPath) (*inviteInfoOutput, error) {
		invite, org, err := s.svc.InspectInvite(ctx, in.Token)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &inviteInfoOutput{}
		out.Body.Org = org.Name
		out.Body.OrgSlug = org.Slug
		out.Body.Role = invite.RoleName
		out.Body.Email = invite.Email
		out.Body.ExpiresAt = invite.ExpiresAt
		return out, nil
	})

	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/invites/{token}/accept", "the invite token authorizes creating the invited account"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "accept-invite", Method: http.MethodPost, Path: "/api/v1/invites/{token}/accept",
		Summary: "Accept an invite and create an account", Tags: []string{"members"},
	}, func(ctx context.Context, in *acceptInviteInput) (*sessionOutput, error) {
		invite, _, err := s.svc.InspectInvite(ctx, in.Token)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		email := in.Body.Email
		if invite.Email != "" {
			email = invite.Email
		}
		if err := s.svc.CheckPassword(ctx, in.Body.Password); err != nil {
			return nil, s.apiError(ctx, err)
		}
		user, err := s.auth.PrepareUser(email, in.Body.Name, in.Body.Password)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		user.EmailVerified = invite.Email != ""
		if _, err := s.svc.AcceptInviteNewUser(ctx, in.Token, user, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		session, err := s.auth.CreateSession(ctx, user, metaFrom(ctx).IP, metaFrom(ctx).UserAgent)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		body, err := s.sessionBody(ctx, session)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &sessionOutput{SetCookie: s.sessionCookies(session), Body: body}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/invites/{token}/accept-existing", "adds the signed-in user to the invited org"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "accept-invite-existing", Method: http.MethodPost, Path: "/api/v1/invites/{token}/accept-existing",
		Summary: "Join an org with an existing account", Tags: []string{"members"},
	}, func(ctx context.Context, in *tokenPath) (*orgOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(problem.Unauthorized("Sign in to continue."), ctx)
		}
		org, err := s.svc.AcceptInviteExisting(ctx, in.Token, principal.User, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		role := ""
		if m, err := s.store.GetMembership(ctx, org.ID, principal.User.ID); err == nil {
			role = m.RoleName
		}
		return &orgOutput{Body: orgFromStore(org, role)}, nil
	})

	return nil
}
