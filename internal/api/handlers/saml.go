package handlers

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"

	"github.com/crewjam/saml/samlsp"
)

// SAMLConfig holds configuration for SAML SP
type SAMLConfig struct {
	RootURL     string
	IdPMetadata string
	CertFile    string
	KeyFile     string
}

// NewSAMLMiddleware creates a new SAML Service Provider middleware
func NewSAMLMiddleware(cfg SAMLConfig) (*samlsp.Middleware, error) {
	keyPair, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load key pair: %w", err)
	}

	keyStore := keyPair.PrivateKey.(*rsa.PrivateKey)
	idpMetadataURL, err := url.Parse(cfg.IdPMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse IdP metadata URL: %w", err)
	}

	rootURL, err := url.Parse(cfg.RootURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse root URL: %w", err)
	}

	x509Cert, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	idpMetadata, err := samlsp.FetchMetadata(context.Background(), http.DefaultClient, *idpMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IdP metadata: %w", err)
	}

	samlSP, err := samlsp.New(samlsp.Options{
		URL:               *rootURL,
		Key:               keyStore,
		Certificate:       x509Cert,
		IDPMetadata:       idpMetadata,
		AllowIDPInitiated: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SAML SP: %w", err)
	}

	return samlSP, nil
}

// SAMLLoginHandler initiates the SAML login flow
func SAMLLoginHandler(sp *samlsp.Middleware) http.HandlerFunc {
	return sp.ServeHTTP
}

// GetUserFromSAMLContext extracts the user ID from the SAML session
func GetUserFromSAMLContext(ctx context.Context) string {
	s := samlsp.SessionFromContext(ctx)
	if s == nil {
		return ""
	}
	sa, ok := s.(samlsp.SessionWithAttributes)
	if !ok {
		return ""
	}
	// Assuming 'uid' or 'email' is the user ID. Adjust based on IdP attributes.
	return sa.GetAttributes().Get("uid")
}
