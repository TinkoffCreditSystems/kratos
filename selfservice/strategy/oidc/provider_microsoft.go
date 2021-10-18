package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ory/kratos/x"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/gofrs/uuid"

	gooidc "github.com/coreos/go-oidc"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/ory/herodot"
)

type ProviderMicrosoft struct {
	*ProviderGenericOIDC
}

type providerJSON struct {
	Issuer      string   `json:"issuer"`
	AuthURL     string   `json:"authorization_endpoint"`
	TokenURL    string   `json:"token_endpoint"`
	JWKSURL     string   `json:"jwks_uri"`
	UserInfoURL string   `json:"userinfo_endpoint"`
	Algorithms  []string `json:"id_token_signing_alg_values_supported"`
}

const (
	RS256 = "RS256"
	RS384 = "RS384"
	RS512 = "RS512"
	ES256 = "ES256"
	ES384 = "ES384"
	ES512 = "ES512"
	PS256 = "PS256"
	PS384 = "PS384"
	PS512 = "PS512"
)

var supportedAlgorithms = map[string]bool{
	RS256: true,
	RS384: true,
	RS512: true,
	ES256: true,
	ES384: true,
	ES512: true,
	PS256: true,
	PS384: true,
	PS512: true,
}

func NewProviderMicrosoft(
	config *Configuration,
	loggingProvider x.LoggingProvider,
	public *url.URL,
) *ProviderMicrosoft {
	return &ProviderMicrosoft{
		ProviderGenericOIDC: &ProviderGenericOIDC{
			config: config,
			public: public,
			l:      loggingProvider.Logger(),
		},
	}
}

// ProviderMicrosoftOIDC represents an OpenID Connect server's configuration.
type ProviderMicrosoftOIDC struct {
	issuer      string
	authURL     string
	tokenURL    string
	userInfoURL string
	algorithms  []string

	// Raw claims returned by the server.
	rawClaims []byte

	remoteKeySet gooidc.KeySet
}

func (m *ProviderMicrosoft) OAuth2(_ context.Context) (*oauth2.Config, error) {
	if len(strings.TrimSpace(m.config.Tenant)) == 0 {
		return nil, errors.WithStack(herodot.ErrInternalServerError.WithReasonf("No Tenant specified for the `microsoft` oidc provider %s", m.config.ID))
	}

	endpointPrefix := "https://login.microsoftonline.com/" + m.config.Tenant
	endpoint := oauth2.Endpoint{
		AuthURL:  endpointPrefix + "/oauth2/v2.0/authorize",
		TokenURL: endpointPrefix + "/oauth2/v2.0/token",
	}

	m.l.Infof("Configuring Oauth 2.0 provider from endpoint %s", endpointPrefix)

	return m.oauth2ConfigFromEndpoint(endpoint), nil
}

func doRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	client := http.DefaultClient
	if c, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok {
		client = c
	}
	return client.Do(req.WithContext(ctx))
}

func (m *ProviderMicrosoft) newProvider(ctx context.Context, issuer string, appId string) (*ProviderMicrosoftOIDC, error) {
	wellKnown := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"

	if appId != "" {
		wellKnown = wellKnown + "?appid=" + appId
	}

	req, err := http.NewRequest("GET", wellKnown, nil)
	if err != nil {
		return nil, err
	}

	m.l.Infof("requesting openid configuration from %s", wellKnown)
	resp, err := doRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", resp.Status, body)
	}

	var p providerJSON
	err = unmarshalResp(resp, body, &p)
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to decode provider discovery object: %v", err)
	}

	if p.Issuer != issuer {
		return nil, fmt.Errorf("oidc: issuer did not match the issuer returned by provider, expected %q got %q", issuer, p.Issuer)
	}
	var algs []string
	for _, a := range p.Algorithms {
		if supportedAlgorithms[a] {
			algs = append(algs, a)
		}
	}

	m.l.Infof("oidc discovery configuration %v", p)

	pm := &ProviderMicrosoftOIDC{
		issuer:       p.Issuer,
		authURL:      p.AuthURL,
		tokenURL:     p.TokenURL,
		userInfoURL:  p.UserInfoURL,
		algorithms:   algs,
		rawClaims:    body,
		remoteKeySet: gooidc.NewRemoteKeySet(ctx, p.JWKSURL),
	}

	m.l.Infof("provider configuration %v", pm)

	return pm, nil
}

// Claims unmarshals raw fields returned by the server during discovery.
//
//    var claims struct {
//        ScopesSupported []string `json:"scopes_supported"`
//        ClaimsSupported []string `json:"claims_supported"`
//    }
//
//    if err := provider.Claims(&claims); err != nil {
//        // handle unmarshaling error
//    }
//
// For a list of fields defined by the OpenID Connect spec see:
// https://openid.net/specs/openid-connect-discovery-1_0.html#ProviderMetadata
func (p *ProviderMicrosoftOIDC) Claims(v interface{}) error {
	if p.rawClaims == nil {
		return errors.New("oidc: claims not set")
	}
	return json.Unmarshal(p.rawClaims, v)
}

// Verifier returns an IDTokenVerifier that uses the provider's key set to verify JWTs.
//
// The returned IDTokenVerifier is tied to the Provider's context and its behavior is
// undefined once the Provider's context is canceled.
func (p *ProviderMicrosoftOIDC) Verifier(config *gooidc.Config) *gooidc.IDTokenVerifier {
	if len(config.SupportedSigningAlgs) == 0 && len(p.algorithms) > 0 {
		// Make a copy so we don't modify the config values.
		cp := &gooidc.Config{}
		*cp = *config
		cp.SupportedSigningAlgs = p.algorithms
		config = cp
	}
	return gooidc.NewVerifier(p.issuer, p.remoteKeySet, config)
}

func (m *ProviderMicrosoft) Claims(ctx context.Context, exchange *oauth2.Token) (*Claims, error) {
	raw, ok := exchange.Extra("id_token").(string)

	if !ok || len(raw) == 0 {
		return nil, errors.WithStack(ErrIDTokenMissing)
	}

	m.l.Infof("received id token %s", raw)
	parser := new(jwt.Parser)
	unverifiedClaims := microsoftUnverifiedClaims{}

	if _, _, err := parser.ParseUnverified(raw, &unverifiedClaims); err != nil {
		return nil, err
	}

	if _, err := uuid.FromString(unverifiedClaims.TenantID); err != nil {
		return nil, errors.WithStack(herodot.ErrBadRequest.WithReasonf("TenantID claim is not a valid UUID: %s", err))
	}

	issuer := "https://login.microsoftonline.com/" + unverifiedClaims.TenantID + "/v2.0"
	p, err := m.newProvider(ctx, issuer, m.Config().AppID)

	if err != nil {
		return nil, errors.WithStack(herodot.ErrInternalServerError.WithReasonf("Unable to initialize OpenID Connect ProviderMicrosoftOIDC: %s", err))
	}

	return m.verifyAndDecodeClaimsWithProvider(ctx, p, raw)
}

type microsoftUnverifiedClaims struct {
	TenantID string `json:"tid,omitempty"`
}

func (c *microsoftUnverifiedClaims) Valid() error {
	return nil
}

func unmarshalResp(r *http.Response, body []byte, v interface{}) error {
	err := json.Unmarshal(body, &v)
	if err == nil {
		return nil
	}
	ct := r.Header.Get("Content-Type")
	mediaType, _, parseErr := mime.ParseMediaType(ct)
	if parseErr == nil && mediaType == "application/json" {
		return fmt.Errorf("got Content-Type = application/json, but could not unmarshal as JSON: %v", err)
	}
	return fmt.Errorf("expected Content-Type = application/json, got %q: %v", ct, err)
}
