package auth

import (
	"crypto/rsa"
	"errors"
	"os"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	UserID uuid.UUID `json:"uid"`
	Role   string    `json:"role"`
	Email  string    `json:"email"`
}

type Verifier struct {
	pub      *rsa.PublicKey
	issuer   string
	audience string
}

func NewVerifier(pubPath, issuer, audience string) (*Verifier, error) {
	b, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, err
	}
	pub, err := jwt.ParseRSAPublicKeyFromPEM(b)
	if err != nil {
		return nil, err
	}
	return &Verifier{pub: pub, issuer: issuer, audience: audience}, nil
}

func (v *Verifier) Parse(token string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("bad alg")
		}
		return v.pub, nil
	}, jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience))
	if err != nil {
		return nil, err
	}
	return &c, nil
}
