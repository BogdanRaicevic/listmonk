package models

import (
	"bytes"
	"html/template"
	"testing"
)

func TestAutoTrackLinks(t *testing.T) {
	f := func(in, want string) {
		t.Helper()
		if got := autoTrackLinks(in); got != want {
			t.Fatalf("autoTrackLinks(%q) = %q, want %q", in, got, want)
		}
	}

	f(`<a href="https://bs.rs/e/?a=1&amp;b=2">x</a>`, `<a href="{{ TrackLink "https://bs.rs/e/?a=1&amp;b=2" . }}">x</a>`)
	f(`<a class="b" href='http://bs.rs/x'>x</a>`, `<a class="b" href='{{ TrackLink "http://bs.rs/x" . }}'>x</a>`)
	f(`<a href = " https://bs.rs/x ">x</a>`, `<a href = "{{ TrackLink "https://bs.rs/x" . }}">x</a>`)
	f(`<a href='https://bs.rs/"q"\'>x</a>`, `<a href='{{ TrackLink "https://bs.rs/\"q\"\\" . }}'>x</a>`)
	f(`<a href="https://bs.rs/šđ">x</a>`, `<a href="{{ TrackLink "https://bs.rs/šđ" . }}">x</a>`)

	// Left alone.
	f(`<a href="https://bs.rs/x@TrackLink">x</a>`, `<a href="https://bs.rs/x@TrackLink">x</a>`)
	f(`<a href="https://bs.rs/?id={{ .Subscriber.UUID }}">x</a>`, `<a href="https://bs.rs/?id={{ .Subscriber.UUID }}">x</a>`)
	f(`<a href="{{ UnsubscribeURL }}">x</a>`, `<a href="{{ UnsubscribeURL }}">x</a>`)
	f(`<a href="mailto:a@b.rs">x</a><a href="tel:011">y</a>`, `<a href="mailto:a@b.rs">x</a><a href="tel:011">y</a>`)
	f(`<img src="https://bs.rs/a.png">`, `<img src="https://bs.rs/a.png">`)
}

// Compiled HTML campaigns track plain links once each; plain text campaigns don't.
func TestAutoTrackCompile(t *testing.T) {
	f := func(contentType string, want []string) {
		t.Helper()
		body := `<a href="https://bs.rs/a">a</a> <a href="https://bs.rs/b@TrackLink">b</a>`
		c := Campaign{ContentType: contentType, Body: body, TemplateBody: `{{ template "content" . }}`}

		var got []string
		if err := c.CompileTemplate(template.FuncMap{"TrackLink": func(url string, _ any) string {
			got = append(got, url)
			return "https://t.example/l"
		}}); err != nil {
			t.Fatal(err)
		}
		if err := c.Tpl.ExecuteTemplate(&bytes.Buffer{}, BaseTpl, nil); err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) || (len(want) == 2 && (got[0] != want[0] || got[1] != want[1])) {
			t.Fatalf("%s: tracked %q, want %q", contentType, got, want)
		}
	}

	f(CampaignContentTypeHTML, []string{"https://bs.rs/a", "https://bs.rs/b"})
	f(CampaignContentTypeRichtext, []string{"https://bs.rs/a", "https://bs.rs/b"})
	f(CampaignContentTypePlain, []string{"https://bs.rs/b"})
}
