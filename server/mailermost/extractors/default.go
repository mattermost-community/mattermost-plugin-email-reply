package extractors

import (
	"io/ioutil"
	"mime/quotedprintable"
	"strings"
)

const (
	emailStartEnd string = "\r\n\r\n"
)

// DefaultExtractor is used for extracting emails all email clients which don't have custom implementation
type DefaultExtractor struct {
}

// ExtractMessage is implementation of IExtractor interface with method for extracting emails
// from all email clients which are not custom implemented - other clients
func (e DefaultExtractor) ExtractMessage(body string) string {
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
