package authserver

import (
	"code.cloudfoundry.org/lager"
	"github.com/concourse/atc/auth"
	"github.com/concourse/atc/db"
)

type Server struct {
	logger          lager.Logger
	externalURL     string
	oAuthBaseURL    string
	tokenGenerator  auth.TokenGenerator
	providerFactory auth.ProviderFactory
	teamDBFactory   db.TeamDBFactory
}

func NewServer(
	logger lager.Logger,
	externalURL string,
	oAuthBaseURL string,
	tokenGenerator auth.TokenGenerator,
	providerFactory auth.ProviderFactory,
	teamDBFactory db.TeamDBFactory,
) *Server {
	return &Server{
		logger:          logger,
		externalURL:     externalURL,
		oAuthBaseURL:    oAuthBaseURL,
		tokenGenerator:  tokenGenerator,
		providerFactory: providerFactory,
		teamDBFactory:   teamDBFactory,
	}
}
