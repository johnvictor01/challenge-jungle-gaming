package http

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

var ErrUnauthorized = errors.New("unauthorized")

type Principal struct {
	Subject    string
	ClientID   string
	ProviderID string
	Roles      map[string]struct{}
}

func (p Principal) HasRole(role string) bool {
	_, ok := p.Roles[role]
	return ok
}

type TokenAuthenticator interface {
	Authenticate(ctx context.Context, token string) (Principal, error)
}

// OIDCAuthenticator valida assinatura, emissor, audiência, validade e claims
// usando o discovery e o JWKS publicados pelo Keycloak.
type OIDCAuthenticator struct {
	verifier *oidc.IDTokenVerifier
}

func NewOIDCAuthenticator(ctx context.Context, issuerURL, audience string) (*OIDCAuthenticator, error) {
	if strings.TrimSpace(issuerURL) == "" || strings.TrimSpace(audience) == "" {
		return nil, errors.New("OIDC issuer and audience are required")
	}
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	return &OIDCAuthenticator{verifier: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

func (a *OIDCAuthenticator) Authenticate(ctx context.Context, token string) (Principal, error) {
	if a == nil || a.verifier == nil || token == "" {
		return Principal{}, ErrUnauthorized
	}
	verified, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	var claims struct {
		Subject    string `json:"sub"`
		Authorized string `json:"azp"`
		ProviderID string `json:"provider_id"`
		Realm      struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err := verified.Claims(&claims); err != nil || claims.Subject == "" || claims.Authorized == "" {
		return Principal{}, ErrUnauthorized
	}
	roles := make(map[string]struct{}, len(claims.Realm.Roles))
	for _, role := range claims.Realm.Roles {
		roles[role] = struct{}{}
	}
	return Principal{Subject: claims.Subject, ClientID: claims.Authorized, ProviderID: claims.ProviderID, Roles: roles}, nil
}
