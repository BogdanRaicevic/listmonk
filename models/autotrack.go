package models

import (
	"regexp"
	"strconv"
	"strings"
)

// Fork: every http(s) link in a campaign's HTML is tracked, without the
// @TrackLink suffix. autoTrackRe matches href attributes in double or single quotes.
var autoTrackRe = regexp.MustCompile(`(href\s*=\s*)(?:"\s*(https?://[^"]+)"|'\s*(https?://[^']+)')`)

// autoTrackLinks wraps every http(s) href in body in {{ TrackLink "url" . }}.
// Links already marked with @TrackLink are left for the regular shorthand, and
// links with template expressions in them are left untouched.
func autoTrackLinks(body string) string {
	return autoTrackRe.ReplaceAllStringFunc(body, func(s string) string {
		m := autoTrackRe.FindStringSubmatch(s)
		url, q := m[2], `"`
		if m[3] != "" {
			url, q = m[3], `'`
		}

		url = strings.TrimSpace(url)
		if strings.HasSuffix(url, "@TrackLink") || strings.Contains(url, "{{") {
			return s
		}

		return m[1] + q + `{{ TrackLink ` + strconv.Quote(url) + ` . }}` + q
	})
}
