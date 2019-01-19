package mailermost

import (
	"fmt"
	"strconv"
	"time"
	"io/ioutil"
	"net/mail"

	"github.com/mattermost/mattermost-server/plugin"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap"
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

func (s *Server) checkMailbox() {
	s.api.LogInfo("============================")
	s.api.LogInfo("Start poll")

	c, err := client.DialTLS(s.server, nil)
	if err != nil {
		s.api.LogError(err.Error())
	}
	s.api.LogDebug("Connected")

	if err := c.Login(s.email, s.password); err != nil {
		s.api.LogError(err.Error())
	}
	s.api.LogInfo("Logged in")
	defer c.Logout()

	s.api.LogInfo("Selecting mailbox")
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		s.api.LogError(err.Error())
	}
	s.api.LogInfo("Mailbox selected")

	from := uint32(1)
	to := mbox.Messages

	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	section := &imap.BodySectionName{}
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}, messages)
	}()

	s.api.LogInfo("Last messages:")
	for msg := range messages {
		r := msg.GetBody(section)
		if r == nil {
			s.api.LogError(fmt.Sprintf("Server didn't returned message body for subject %v", msg.Envelope.Subject))
			continue
		}

		m, err := mail.ReadMessage(r)
		if err != nil {
			s.api.LogError(err.Error())
			continue
		}

		body, err := ioutil.ReadAll(m.Body)
		if err != nil {
			s.api.LogError("Couldn't read message's body")
			continue
		}

		fromAddress := msg.Envelope.From[0].MailboxName + "@" + msg.Envelope.From[0].HostName

		s.api.LogInfo("------------ START MESSAGE ------------")
		s.api.LogInfo("- Subject: " + msg.Envelope.Subject)
		s.api.LogInfo("- From: " + fromAddress)
		s.api.LogInfo("- InReplyTo: " + msg.Envelope.InReplyTo)
		s.api.LogInfo("- MessageId: " + msg.Envelope.MessageId)
		s.api.LogInfo(fmt.Sprintf("- Message Body: %+v", string(body)[:10]))
		s.api.LogInfo("------------- END MESSAGE -------------")
	}

	if err := <-done; err != nil {
		s.api.LogError(err.Error())
	}

	s.api.LogInfo("End poll")
	s.api.LogInfo("============================")
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

func (s *Server) StartPolling() {
	s.api.LogInfo("Starting Polling...")
	s.api.LogInfo(fmt.Sprintf("Server: %s, Security: %s, Email: %s, Password: %s", s.server, s.security, s.email, s.password))

	ticker := time.NewTicker(time.Duration(s.pollingInterval) * time.Second)
	for {
		select {
		case <-ticker.C:
			s.checkMailbox()
		}
	}
}
