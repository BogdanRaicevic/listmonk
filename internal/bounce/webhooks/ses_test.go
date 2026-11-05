package webhooks

import (
	"encoding/json"
	"testing"
)

func TestSESRecipient(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "bounce with display name in destination",
			msg: `{"notificationType":"Bounce",
				"bounce":{"bounceType":"Permanent","bouncedRecipients":[{"emailAddress":"jane.doe@example.com","status":"5.1.1"}]},
				"mail":{"destination":["\"Jane Doe\" <jane.doe@example.com>"]}}`,
			want: "jane.doe@example.com",
		},
		{
			name: "complaint with display name in destination",
			msg: `{"notificationType":"Complaint",
				"complaint":{"complainedRecipients":[{"emailAddress":"jane.doe@example.com"}]},
				"mail":{"destination":["\"Jane Doe\" <jane.doe@example.com>"]}}`,
			want: "jane.doe@example.com",
		},
		{
			name: "no recipient lists, display name with non-ASCII chars in destination",
			msg:  `{"eventType":"Bounce","mail":{"destination":["\"Đurđa Jovanović\" <Djurdja.Jovanovic@Example.rs>"]}}`,
			want: "djurdja.jovanovic@example.rs",
		},
		{
			name: "plain address",
			msg:  `{"notificationType":"Bounce","bounce":{"bounceType":"Transient"},"mail":{"destination":["Jane@Example.com"]}}`,
			want: "jane@example.com",
		},
		{
			name: "recipient with display name",
			msg: `{"notificationType":"Bounce",
				"bounce":{"bouncedRecipients":[{"emailAddress":"Jane Doe <jane@example.com>"}]},
				"mail":{"destination":["jane@example.com"]}}`,
			want: "jane@example.com",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m sesMail
			if err := json.Unmarshal([]byte(c.msg), &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := sesRecipient(m); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
