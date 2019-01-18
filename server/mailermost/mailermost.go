package mailermost

import (
	"fmt"
	"github.com/mattermost/mattermost-server/plugin"
	"strconv"
)

type Server struct {
	api plugin.API
	server string
	security string
	email string
	password string
	pollingInterval int
}

func NewServer (api plugin.API, server, security, email, password, pollingInterval string) (*Server, error){
	s := &Server{
		api: api,
		server: server,
		security: security,
		email: email,
		password: password,
	}

	s.pollingInterval, _ = strconv.Atoi(pollingInterval)

	return s, nil
}

func (s *Server) StartPolling() {
	s.api.LogInfo("Starting Polling...")
	s.api.LogInfo(fmt.Sprintf("Server: %s, Security: %s, Email: %s, Password: %s", s.server, s.security, s.email, s.password))

}