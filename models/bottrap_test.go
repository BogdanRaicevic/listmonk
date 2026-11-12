package models

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

// Fork: the bot trap is added to HTML e-mails only, and only when the BotTrap
// func is provided.
func TestBotTrapCompile(t *testing.T) {
	f := func(contentType string, funcs template.FuncMap, want bool) {
		t.Helper()
		c := Campaign{ContentType: contentType, Body: "hello", TemplateBody: `{{ template "content" . }}`}
		if err := c.CompileTemplate(funcs); err != nil {
			t.Fatalf("%s: %v", contentType, err)
		}

		var out bytes.Buffer
		if err := c.Tpl.ExecuteTemplate(&out, BaseTpl, nil); err != nil {
			t.Fatalf("%s: %v", contentType, err)
		}
		if got := strings.Contains(out.String(), "TRAP"); got != want {
			t.Fatalf("%s: trap in %q = %v, want %v", contentType, out.String(), got, want)
		}
	}

	trap := template.FuncMap{"BotTrap": func(any) template.HTML { return "TRAP" }}
	for _, ct := range []string{CampaignContentTypeHTML, CampaignContentTypeRichtext, CampaignContentTypeMarkdown, CampaignContentTypeVisual} {
		f(ct, trap, true)
	}
	f(CampaignContentTypePlain, trap, false)
	f(CampaignContentTypeHTML, template.FuncMap{}, false)
}
