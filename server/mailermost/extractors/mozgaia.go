package extractors

import (
	"io/ioutil"
	"mime/quotedprintable"
	"strings"
)

// MozGaiaExtractor is used for extracting emails from KaiOS mobile email client
type MozGaiaExtractor struct {
}

// ExtractMessage is implementation of IExtractor interface with method for extracting emails
// from KaiOS mobile email client
func (e MozGaiaExtractor) ExtractMessage(body string) string {
	bodyWithoutHeaders := body[60:]

	lastIdx := strings.Index(bodyWithoutHeaders, emailStartEnd)
	cleanBody := bodyWithoutHeaders
	if lastIdx != -1 {
		cleanBody = bodyWithoutHeaders[:lastIdx]
	}

	reader := strings.NewReader(cleanBody)
	quotedprintableReader := quotedprintable.NewReader(reader)
	message, _ := ioutil.ReadAll(quotedprintableReader)
	smessage := string(message)
	return smessage[:strings.Index(smessage, "<br/><br/>")]
}
