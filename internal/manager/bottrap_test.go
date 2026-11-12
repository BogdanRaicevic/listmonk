package manager

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/knadh/listmonk/models"
)

type fakeLinkStore struct {
	Store
	urls []string
}

func (f *fakeLinkStore) CreateLink(url string) (string, error) {
	f.urls = append(f.urls, url)
	return "link-uuid", nil
}

// Fork: a rendered HTML campaign e-mail carries the hidden, tracked bot-trap link.
func TestBotTrapRendered(t *testing.T) {
	s := &fakeLinkStore{}
	m := &Manager{
		store: s, log: log.New(io.Discard, "", 0), links: map[string]string{},
		cfg: Config{RootURL: "https://lm.example", LinkTrackURL: "https://lm.example/link/%s/%s/%s", IndividualTracking: true},
	}

	c := &models.Campaign{UUID: "camp-uuid", ContentType: models.CampaignContentTypeHTML, Body: "<p>hi</p>", TemplateBody: `{{ template "content" . }}`}
	if err := c.CompileTemplate(m.TemplateFuncs(c)); err != nil {
		t.Fatal(err)
	}

	msg := &CampaignMessage{Campaign: c, Subscriber: models.Subscriber{UUID: "sub-uuid"}}
	if err := msg.render(); err != nil {
		t.Fatal(err)
	}

	want := `<a href="https://lm.example/link/link-uuid/camp-uuid/sub-uuid" aria-hidden="true" tabindex="-1" style="display:none;`
	if !strings.Contains(string(msg.body), want) {
		t.Fatalf("body %q has no bot trap", msg.body)
	}
	if len(s.urls) != 1 || s.urls[0] != "https://lm.example/?"+models.BotTrapMarker {
		t.Fatalf("registered links = %q", s.urls)
	}
}
