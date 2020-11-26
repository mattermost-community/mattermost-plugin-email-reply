package extractors

// IExtractor is interface which needs to be implemented by all email extractoris in fhis directory
type IExtractor interface {
	ExtractMessage(body string) string
}
