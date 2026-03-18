package processor

import (
	"regexp"
)

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// stripHTML removes HTML tags from a string
func stripHTML(html string) string {
	return htmlTagRE.ReplaceAllString(html, "")
}
