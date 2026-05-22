package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	jwtauth "github.com/vks4/vks4/services/control-plane/internal/adapters/auth/jwt"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/repo/postgres"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
	apperr "github.com/vks4/vks4/services/control-plane/internal/pkg/errors"
)

type Auth struct {
	users    *postgres.Users
	refresh  *postgres.RefreshTokens
	audit    *postgres.Audit
	jwt      *jwtauth.Issuer
	bootPass string
}

func NewAuth(u *postgres.Users, r *postgres.RefreshTokens, a *postgres.Audit, j *jwtauth.Issuer, bootPass string) *Auth {
	return &Auth{users: u, refresh: r, audit: a, jwt: j, bootPass: bootPass}
}

type Tokens struct {
	Access    string
	Refresh   string
	ExpiresIn int
}

func (a *Auth) Login(ctx context.Context, email, password string) (*Tokens, *domain.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := a.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if u == nil || u.Disabled {
		return nil, nil, apperr.New(apperr.CodeUnauthorized, "invalid credentials")
	}
	// Bootstrap: первая авторизация admin@local при пустом хеше.
	if u.PasswordHash == "$bootstrap$" && password == a.bootPass {
		h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err := a.users.UpdatePassword(ctx, u.ID, string(h)); err != nil {
			return nil, nil, apperr.Wrap(apperr.CodeInternal, "set bootstrap pw", err)
		}
		u.PasswordHash = string(h)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil, apperr.New(apperr.CodeUnauthorized, "invalid credentials")
	}
	t, err := a.issue(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	d := u.Domain()
	_ = a.audit.Write(ctx, postgres.AuditWrite{ActorID: &u.ID, Action: "auth.login", Target: u.ID.String()})
	return t, &d, nil
}

func (a *Auth) Refresh(ctx context.Context, refresh string) (*Tokens, error) {
	claims, err := a.jwt.ParseRefresh(refresh)
	if err != nil {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid refresh")
	}
	jtiStr, _ := claims["jti"].(string)
	subStr, _ := claims["sub"].(string)
	jti, err := uuid.Parse(jtiStr)
	if err != nil {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid jti")
	}
	uid, err := uuid.Parse(subStr)
	if err != nil {
		return nil, apperr.New(apperr.CodeUnauthorized, "invalid sub")
	}
	ok, err := a.refresh.IsValid(ctx, jti)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if !ok {
		return nil, apperr.New(apperr.CodeUnauthorized, "revoked or expired")
	}
	u, err := a.users.GetByID(ctx, uid)
	if err != nil || u == nil {
		return nil, apperr.New(apperr.CodeUnauthorized, "user gone")
	}
	// Rotate refresh.
	_ = a.refresh.Revoke(ctx, jti)
	t, err := a.issue(ctx, u)
	return t, err
}

func (a *Auth) Logout(ctx context.Context, jti uuid.UUID) error {
	return a.refresh.Revoke(ctx, jti)
}

func (a *Auth) issue(ctx context.Context, u *postgres.UserRow) (*Tokens, error) {
	access, exp, err := a.jwt.AccessToken(u.ID, u.Role, u.Email)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "sign access", err)
	}
	refresh, jti, rexp, err := a.jwt.RefreshToken(u.ID)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "sign refresh", err)
	}
	if err := a.refresh.Store(ctx, jti, u.ID, rexp); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "store refresh", err)
	}
	return &Tokens{
		Access:    access,
		Refresh:   refresh,
		ExpiresIn: int(time.Until(exp).Seconds()),
	}, nil
}
