package auth

import (
	"net/http"

	"github.com/antihax/goesi"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

// AuthRepository is interface for accessing token data.
type AuthRepository interface {
	Read() (oauth2.Token, error)
	Write(oauth2.Token) error
}

type authService struct {
	sso            *goesi.SSOAuthenticator
	authRepository AuthRepository
}

// NewService returns new token source from storage.
func NewService(
	log *zap.Logger,
	client *http.Client,
	authRepository AuthRepository,
	secretKey []byte,
	clientID, ssoSecret, callbackURL string,
	scopes []string,
) *authService {
	sso := goesi.NewSSOAuthenticator(client, clientID, ssoSecret, callbackURL, scopes)
	return &authService{
		authRepository: authRepository,
		sso:            sso,
	}
}

func (s *authService) Token() (*oauth2.Token, error) {
	ts, err := s.TokenSource()
	if err != nil {
		return nil, errors.Wrap(err, "unable to read token")
	}
	newToken, err := ts.Token()
	if err != nil {
		return nil, errors.Errorf("error getting token")
	}

	// Save token.
	err = s.authRepository.Write(*newToken)
	if err != nil {
		return nil, errors.Wrap(err, "unable to save refreshed token")
	}

	return newToken, nil
}

func (s *authService) TokenSource() (oauth2.TokenSource, error) {
	token, err := s.authRepository.Read()
	if err != nil {
		return nil, errors.Wrap(err, "unable to read token")
	}
	return s.sso.TokenSource(&token), nil
}

func (s *authService) Verify() (*goesi.VerifyResponse, error) {
	ts, err := s.TokenSource()
	if err != nil {
		return nil, errors.Wrap(err, "unable to create token source")
	}
	return s.sso.Verify(ts)
}

func (s *authService) SaveToken(token oauth2.Token) error {
	return s.authRepository.Write(token)
}
