package mailermost

import (
	"fmt"
	"io/ioutil"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost-server/model"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
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

func (s *Server) checkMailbox() {
	c, err := client.DialTLS(s.server, nil)
	if err != nil {

		s.api.LogError(err.Error())
	}

	if err := c.Login(s.email, s.password); err != nil {
		s.api.LogError(err.Error())
	}
	defer c.Logout()

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		s.api.LogError(err.Error())
	}

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
		deleteMessage := func() {
			item := imap.FormatFlagsOp(imap.AddFlags, true)
			flags := []interface{}{imap.DeletedFlag}
			err = c.Store(seqset, item, flags, nil)
			if err != nil {
				s.api.LogError(err.Error())
			}
		}

		postID := s.postIDFromEmailBody(string(body))
		if len(postID) != 26 {
			deleteMessage()
			continue
		}

		messageText := s.extractMessage(string(body))
		s.api.LogDebug(fmt.Sprintf("messageText: %s", messageText))

		var appErr *model.AppError
		var user *model.User
		user, appErr = s.api.GetUserByEmail(fromAddress)
		if appErr != nil {
			s.api.LogError(fmt.Sprintf("err: %#v", appErr))
			deleteMessage()
			continue
		}

		s.api.LogDebug(fmt.Sprintf("user: %+v", user))

		var post *model.Post
		post, appErr = s.api.GetPost(postID)
		if appErr != nil {
			s.api.LogError(fmt.Sprintf("err: %#v", appErr))
			deleteMessage()
			continue
		}

		s.api.LogDebug(fmt.Sprintf("post: %+v", post))

		// CHECK PERMISSIONS
		// POST MESSAGE
	}

	if err := <-done; err != nil {
		s.api.LogError(err.Error())
	}

	// TODO:
	// 1. Retrieve emails.
	// 2. Delete non-pertinent ones (don't have the subject, message-id header, and potentially the post hyperlink).
	// 3. Parse-out the potential post message from the email. If it's blank then return.
	// 4. Verify that the 'from' email address matches a MM user who is allowed to post to channel.
	// 5.
	// 		- If post id is not in a thread then create a thread and create the send message as reply.
	//		- If post id is in a thread and it's the last message in the thread then append a message to the thread.
	//		- If post id is in a thread but not the last message then quote the original message in the new post body.
	// 6. Delete email.
}

func (s *Server) StartPolling() {
	ticker := time.NewTicker(time.Duration(s.pollingInterval) * time.Second)
	for range ticker.C {
		s.api.LogInfo("poll")
		s.checkMailbox()
	}
}

func (s *Server) postIDFromEmailBody(emailBody string) string {
	var postID string
	re := regexp.MustCompile(`https?:\/\/.*\/pl\/[a-z0-9]{26}`)
	match := re.FindString(emailBody)
	if len(match) >= 26 {
		postID = match[len(match)-26:]
	}
	return postID
}

func (s *Server) extractMessage(body string) string {
	bodyWithoutHeaders := body
	firstIdx := strings.Index(body, "\r\n\r\n")
	if firstIdx != -1 {
		bodyWithoutHeaders = body[firstIdx+4:]
	}

	lastIdx := strings.Index(bodyWithoutHeaders, "\r\n\r\n")
	cleanBody := bodyWithoutHeaders
	if lastIdx != -1 {
		cleanBody = bodyWithoutHeaders[:lastIdx]
	}

	return cleanBody
}
