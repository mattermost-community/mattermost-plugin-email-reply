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
	emailStartEnd        string = "\r\n\r\n"
	postIDUrlRe          string = `https?:\/\/.*\/pl\/[a-z0-9]{26}`
	emailLineEndingRe    string = `=\r\n`
	mailboxName          string = "INBOX"
	ellipsisLen          int    = 50
	maxEmailsPerInterval        = 1000
)

type Poller struct {
	api             plugin.API
	server          string
	security        string
	email           string
	password        string
	pollingInterval int
}

func NewPoller(api plugin.API, server, security, password, pollingInterval string) (*Poller, error) {
	p := &Poller{
		api:      api,
		server:   server,
		security: security,
		email:    *api.GetConfig().EmailSettings.ReplyToAddress,
		password: password,
	}

	var err error
	p.pollingInterval, err = strconv.Atoi(pollingInterval)
	if err != nil {
		return nil, err
	}

	return p, nil
}

// Poll starts checking the configured email mailbox on the configured interval.
func (p *Poller) Poll() {
	ticker := time.NewTicker(time.Duration(p.pollingInterval) * time.Second)
	for range ticker.C {
		p.checkMailbox()
	}
}

func (p *Poller) checkMailbox() {
	c, err := client.DialTLS(p.server, nil)
	if err != nil {
		p.api.LogError(fmt.Sprintf("failure dialing TLS: %s", err.Error()))
	}

	if err := c.Login(p.email, p.password); err != nil {
		p.api.LogError(fmt.Sprintf("failure loging into email for user %s: %s", p.email, err.Error()))
	}
	defer c.Logout()

	mbox, err := c.Select(mailboxName, false)
	if err != nil {
		p.api.LogError(fmt.Sprintf("failed to get %s: %s", mailboxName, err.Error()))
	}

	from := uint32(1)
	to := mbox.Messages

	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	messages := make(chan *imap.Message, maxEmailsPerInterval)
	done := make(chan error, 1)
	section := &imap.BodySectionName{}
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}, messages)
	}()

	for msg := range messages {
		p.processEmail(msg, section, seqset, c)
	}

	if err := <-done; err != nil {
		p.api.LogError(err.Error())
	}
}

func (p *Poller) processEmail(msg *imap.Message, section *imap.BodySectionName, seqset *imap.SeqSet, c *client.Client) {
	messageID := msg.Envelope.MessageId

	r := msg.GetBody(section)
	if r == nil {
		p.api.LogError(fmt.Sprintf("failed to get message body of email %s", messageID))
		return
	}

	m, err := mail.ReadMessage(r)
	if err != nil {
		p.api.LogError(fmt.Sprintf("failure reading email %s: %s", messageID, err.Error()))
		return
	}

	body, err := ioutil.ReadAll(m.Body)
	if err != nil {
		p.api.LogError(fmt.Sprintf("failed to read message body of email %s: %s", messageID, err.Error()))
		return
	}

	fromAddress := msg.Envelope.From[0].MailboxName + "@" + msg.Envelope.From[0].HostName

	postID := p.postIDFromEmailBody(string(body))
	if !model.IsValidId(postID) {
		p.api.LogInfo(fmt.Sprintf("email %s contains invalid post id %s", messageID, postID))
		p.deleteMessage(c, seqset, messageID)
		return
	}

	messageText := p.extractMessage(string(body))
	if len(messageText) == 0 {
		p.api.LogError(fmt.Sprintf("email %s has no message text", messageID))
		p.deleteMessage(c, seqset, messageID)
		return
	}

	var appErr *model.AppError

	var user *model.User
	user, appErr = p.api.GetUserByEmail(fromAddress)
	if appErr != nil {
		p.api.LogError(fmt.Sprintf("failed to get user with email address %s: %s", fromAddress, appErr.Error()))
		p.deleteMessage(c, seqset, messageID)
		return
	}

	var post *model.Post
	post, appErr = p.api.GetPost(postID)
	if appErr != nil {
		p.api.LogError(fmt.Sprintf("failed to get post with id %s: %s", postID, appErr.Error()))
		p.deleteMessage(c, seqset, messageID)
		return
	}

	_, appErr = p.api.GetChannelMember(post.ChannelId, user.Id)
	if appErr != nil {
		p.api.LogError(fmt.Sprintf("failed to get channel member %s in channel %s: %s", user.Id, post.ChannelId, appErr.Error()))
		p.deleteMessage(c, seqset, messageID)
		return
	}

	postList, appErr := p.api.GetPostThread(postID)
	if appErr != nil {
		p.api.LogError(fmt.Sprintf("failed to get post thread for post id %s: %s", postID, appErr.Error()))
		p.deleteMessage(c, seqset, messageID)
		return
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
		channel, appErr = p.api.GetChannel(post.ChannelId)
		if appErr != nil {
			p.api.LogError(fmt.Sprintf("failed to get channel with id %s: %s", post.ChannelId, appErr.Error()))
			p.deleteMessage(c, seqset, messageID)
			return
		}

		var team *model.Team
		team, appErr = p.api.GetTeam(channel.TeamId)
		if appErr != nil {
			p.api.LogError(fmt.Sprintf("failed to get team with id %s: %s", channel.TeamId, appErr.Error()))
			p.deleteMessage(c, seqset, messageID)
			return
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

	_, appErr = p.api.CreatePost(newPost)
	if appErr != nil {
		p.api.LogError(fmt.Sprintf("failed to create post %+v: %s", newPost, appErr.Error()))
		// Do not delete the inbound email in this failure case because everything about the inbound email has been valid so far.
		return
	}

	p.deleteMessage(c, seqset, messageID)
}

func (p *Poller) deleteMessage(c *client.Client, seqset *imap.SeqSet, messageID string) {
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.DeletedFlag}
	err := c.Store(seqset, item, flags, nil)
	if err != nil {
		p.api.LogError(fmt.Sprintf("failed to set deleted flag on email %s: %s", messageID, err.Error()))
	}
}

func (p *Poller) postIDFromEmailBody(emailBody string) string {
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

func (p *Poller) extractMessage(body string) string {
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
