package jwtauth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	UserID uuid.UUID `json:"uid"`
	Role   string    `json:"role"`
	Email  string    `json:"email"`
}

type Issuer struct {
	priv       *rsa.PrivateKey
	pub        *rsa.PublicKey
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func New(privPath, pubPath, issuer, audience string, access, refresh time.Duration) (*Issuer, error) {
	priv, err := loadPriv(privPath)
	if err != nil {
		return nil, err
	}
	pub, err := loadPub(pubPath)
	if err != nil {
		return nil, err
	}
	return &Issuer{priv: priv, pub: pub, issuer: issuer, audience: audience, accessTTL: access, refreshTTL: refresh}, nil
}

func loadPriv(path string) (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read priv key %s: %w", path, err)
	}
	return jwt.ParseRSAPrivateKeyFromPEM(b)
}

func loadPub(path string) (*rsa.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pub key %s: %w", path, err)
	}
	return jwt.ParseRSAPublicKeyFromPEM(b)
}

func (i *Issuer) AccessToken(userID uuid.UUID, role, email string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(i.accessTTL)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Audience:  jwt.ClaimStrings{i.audience},
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
		UserID: userID,
		Role:   role,
		Email:  email,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(i.priv)
	return s, exp, err
}

func (i *Issuer) RefreshToken(userID uuid.UUID) (string, uuid.UUID, time.Time, error) {
	jti := uuid.New()
	now := time.Now()
	exp := now.Add(i.refreshTTL)
	claims := jwt.RegisteredClaims{
		Issuer:    i.issuer,
		Audience:  jwt.ClaimStrings{i.audience + ":refresh"},
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
		ID:        jti.String(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(i.priv)
	return s, jti, exp, err
}

func (i *Issuer) Parse(token string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return i.pub, nil
	}, jwt.WithIssuer(i.issuer), jwt.WithAudience(i.audience))
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (i *Issuer) ParseRefresh(token string) (jwt.MapClaims, error) {
	c := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		return i.pub, nil
	}, jwt.WithIssuer(i.issuer), jwt.WithAudience(i.audience+":refresh"))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (i *Issuer) AccessTTL() time.Duration { return i.accessTTL }
