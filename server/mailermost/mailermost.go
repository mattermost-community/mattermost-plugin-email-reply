package mailermost

import (
	"fmt"
	"strconv"
	"time"

	"github.com/mattermost/mattermost-server/plugin"
)

type Server struct {
	api             plugin.API
	server          string
	security        string
	email           string
	password        string
	pollingInterval int
}

func NewServer(api plugin.API, server, security, email, password, pollingInterval string) (*Server, error) {

	s := &Server{
		api:      api,
		server:   server,
		security: security,
		email:    email,
		password: password,
	}

	var err error
	s.pollingInterval, err = strconv.Atoi(pollingInterval)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Server) StartPolling() {
	s.api.LogInfo("Starting Polling...")
	s.api.LogInfo(fmt.Sprintf("Server: %s, Security: %s, Email: %s, Password: %s", s.server, s.security, s.email, s.password))

	ticker := time.NewTicker(time.Duration(s.pollingInterval) * time.Second)
	for {
		select {
		case <-ticker.C:
			s.api.LogInfo("poll")
			// TODO:
			// 1. Retrieve emails.
			// 2. Delete non-pertinent ones (don't have the subject, message-id header, and potentially the post hyperlink).
			// 3. Parse-out the potential post message from the email. If it's blank then return.
			// 4. Verify that the 'from' email address matches a MM user who is allowed to post to channel.
			// 5.
			// 		- If post id is not in a thread then create a thread and create the send message as reply.
			//		- If post id is in a thread and it's the last message in the thread then append a message to the thread.
			//		- If post id is in a thread but not the last message then quote the original message in the new post body.
		}
	}
}
