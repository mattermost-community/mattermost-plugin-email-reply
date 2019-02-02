package mailermost

import (
	"fmt"
	"io/ioutil"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost-server/model"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/mattermost/mattermost-server/plugin"
)

const (
	emailStartEnd     string = "\r\n\r\n"
	postIDUrlRe       string = `https?:\/\/.*\/pl\/[a-z0-9]{26}`
	emailLineEndingRe string = `=\r\n`
	mailboxName       string = "INBOX"
	ellipsisLen       int    = 50
)

type Client struct {
	api             plugin.API
	server          string
	security        string
	email           string
	password        string
	pollingInterval int
}

func NewClient(api plugin.API, server, security, password, pollingInterval string) (*Client, error) {
	s := &Client{
		api:      api,
		server:   server,
		security: security,
		email:    *api.GetConfig().EmailSettings.ReplyToAddress,
		password: password,
	}

	var err error
	s.pollingInterval, err = strconv.Atoi(pollingInterval)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Client) checkMailbox() {
	c, err := client.DialTLS(s.server, nil)
	if err != nil {
		s.api.LogError(fmt.Sprintf("failure dialing TLS: %s", err.Error()))
	}

	if err := c.Login(s.email, s.password); err != nil {
		s.api.LogError(fmt.Sprintf("failure loging into email for user %s: %s", s.email, err.Error()))
	}
	defer c.Logout()

	mbox, err := c.Select(mailboxName, false)
	if err != nil {
		s.api.LogError(fmt.Sprintf("failed to get %s: %s", mailboxName, err.Error()))
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
		messageID := msg.Envelope.MessageId

		r := msg.GetBody(section)
		if r == nil {
			s.api.LogError(fmt.Sprintf("failed to get message body of email %s", messageID))
			continue
		}

		m, err := mail.ReadMessage(r)
		if err != nil {
			s.api.LogError(fmt.Sprintf("failure reading email %s: %s", messageID, err.Error()))
			continue
		}

		body, err := ioutil.ReadAll(m.Body)
		if err != nil {
			s.api.LogError(fmt.Sprintf("failed to read message body of email %s: %s", messageID, err.Error()))
			continue
		}

		fromAddress := msg.Envelope.From[0].MailboxName + "@" + msg.Envelope.From[0].HostName

		postID := s.postIDFromEmailBody(string(body))
		if !model.IsValidId(postID) {
			s.api.LogInfo(fmt.Sprintf("email %s contains invalid post id %s", messageID, postID))
			s.deleteMessage(c, seqset, messageID)
			continue
		}

		messageText := s.extractMessage(string(body))
		if len(messageText) == 0 {
			s.api.LogError(fmt.Sprintf("email %s has no message text", messageID))
			s.deleteMessage(c, seqset, messageID)
			continue
		}

		var appErr *model.AppError

		var user *model.User
		user, appErr = s.api.GetUserByEmail(fromAddress)
		if appErr != nil {
			s.api.LogError(fmt.Sprintf("failed to get user with email address %s: %s", fromAddress, appErr.Error()))
			s.deleteMessage(c, seqset, messageID)
			continue
		}

		var post *model.Post
		post, appErr = s.api.GetPost(postID)
		if appErr != nil {
			s.api.LogError(fmt.Sprintf("failed to get post with id %s: %s", postID, appErr.Error()))
			s.deleteMessage(c, seqset, messageID)
			continue
		}

		_, appErr = s.api.GetChannelMember(post.ChannelId, user.Id)
		if appErr != nil {
			s.api.LogError(fmt.Sprintf("failed to get channel member %s in channel %s: %s", user.Id, post.ChannelId, appErr.Error()))
			s.deleteMessage(c, seqset, messageID)
			continue
		}

		postList, appErr := s.api.GetPostThread(postID)
		if appErr != nil {
			s.api.LogError(fmt.Sprintf("failed to get post thread for post id %s: %s", postID, appErr.Error()))
			s.deleteMessage(c, seqset, messageID)
			continue
		}

		threadPosts := make([]*model.Post, 0)
		for _, v := range postList.Posts {
			threadPosts = append(threadPosts, v)
		}
		sort.Slice(threadPosts, func(i, j int) bool {
			return threadPosts[i].CreateAt > threadPosts[j].CreateAt
		})

		rootPost := threadPosts[len(threadPosts)-1]
		lastPost := threadPosts[0]

		if len(postList.Posts) > 1 && lastPost.Id != post.Id {
			var channel *model.Channel
			channel, appErr = s.api.GetChannel(post.ChannelId)
			if appErr != nil {
				s.api.LogError(fmt.Sprintf("failed to get channel with id %s: %s", post.ChannelId, appErr.Error()))
				s.deleteMessage(c, seqset, messageID)
				continue
			}

			var team *model.Team
			team, appErr = s.api.GetTeam(channel.TeamId)
			if appErr != nil {
				s.api.LogError(fmt.Sprintf("failed to get team with id %s: %s", channel.TeamId, appErr.Error()))
				s.deleteMessage(c, seqset, messageID)
				continue
			}

			postPl := "/" + team.Name + "/pl/" + post.Id

			if len(post.Message) > ellipsisLen {
				messageText = fmt.Sprintf("> [%s](%s)...\n\n%s", post.Message[:ellipsisLen], postPl, messageText)
			} else {
				messageText = fmt.Sprintf("> [%s](%s)\n\n%s", post.Message, postPl, messageText)
			}
		}

		newPost := &model.Post{
			UserId:    user.Id,
			ChannelId: post.ChannelId,
			Message:   messageText,
			ParentId:  rootPost.Id,
			RootId:    rootPost.Id,
		}

		_, appErr = s.api.CreatePost(newPost)
		if appErr != nil {
			s.api.LogError(fmt.Sprintf("failed to create post %+v: %s", newPost, appErr.Error()))
			// Do not delete the inbound email in this failure case because everything about the inbound email has been valid so far.
			continue
		}

		s.deleteMessage(c, seqset, messageID)
	}

	if err := <-done; err != nil {
		s.api.LogError(err.Error())
	}
}

func (s *Client) deleteMessage(c *client.Client, seqset *imap.SeqSet, messageID string) {
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.DeletedFlag}
	err := c.Store(seqset, item, flags, nil)
	if err != nil {
		s.api.LogError(fmt.Sprintf("failed to set deleted flag on email %s: %s", messageID, err.Error()))
	}
}

// StartPolling starts checking the configured email mailbox on the configured interval.
func (s *Client) StartPolling() {
	ticker := time.NewTicker(time.Duration(s.pollingInterval) * time.Second)
	for range ticker.C {
		s.checkMailbox()
	}
}

func (s *Client) postIDFromEmailBody(emailBody string) string {
	var postID string

	postIDRe := regexp.MustCompile(postIDUrlRe)
	lineEndingRe := regexp.MustCompile(emailLineEndingRe)
	emailBody = lineEndingRe.ReplaceAllString(emailBody, "")
	match := postIDRe.FindString(emailBody)
	if len(match) >= 26 {
		postID = match[len(match)-26:]
	}

	return postID
}

func (s *Client) extractMessage(body string) string {
	bodyWithoutHeaders := body

	firstIdx := strings.Index(body, emailStartEnd)
	if firstIdx != -1 {
		bodyWithoutHeaders = body[firstIdx+4:]
	}

	lastIdx := strings.Index(bodyWithoutHeaders, emailStartEnd)
	cleanBody := bodyWithoutHeaders
	if lastIdx != -1 {
		cleanBody = bodyWithoutHeaders[:lastIdx]
	}

	reader := strings.NewReader(cleanBody)
	quotedprintableReader := quotedprintable.NewReader(reader)
	message, _ := ioutil.ReadAll(quotedprintableReader)

	return strings.TrimSpace(string(message))
}
