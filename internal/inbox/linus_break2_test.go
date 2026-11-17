package inbox

import (
	"testing"

	"github.com/emersion/go-imap"
)

// linus review round 2 of c6fccec.
func TestLinusBreak2Match(t *testing.T) {
	m := catalogMonitor(t)
	cases := []struct{ name, mailbox, host, fromName, subject, want string }{
		{"user address in subject, name hint", "noreply", "m.onetrust.com", "On behalf of AtData", "Your Privacy Request for user@gmail.com", "atdata"},
		{"date in subject", "noreply", "m.onetrust.com", "On behalf of AtData", "Request 11.16.2026", "atdata"},
		{"service host in subject", "noreply", "m.onetrust.com", "On behalf of AtData", "privacyportal.onetrust.com request", "atdata"},
		{"label hint plus unknown host", "privacy", "6sense.mail.datagrail.io", "", "Request from example.org", ""},
		{"filename in subject", "noreply", "m.onetrust.com", "On behalf of AtData", "report.pdf attached", "atdata"},
		{"uppercase host in name", "noreply", "trustsuperset.com", "Privacy Team at REVOPTIMAL.COM", "", "revoptimal"},
	}
	for _, c := range cases {
		e, err := m.parseMessage(envelope(c.mailbox, c.host, c.fromName, c.subject), &imap.BodySectionName{})
		if err != nil {
			t.Fatal(err)
		}
		if e.BrokerID != c.want {
			t.Errorf("%s: matched %q, want %q", c.name, e.BrokerID, c.want)
		}
	}
}

func TestLinusBreak2Classify(t *testing.T) {
	cases := []struct {
		body string
		bad  ResponseType
	}{
		{"Your data will be permanently deleted once verified.", ResponseSuccess},
		{"Your email address has been marked for removal and will be removed within 7 days.", ""},
		{"We have processed your opt-out request. You will not receive another email from us.", ""},
	}
	for _, c := range cases {
		got := ClassifyResponse(&Email{From: "x@broker.example", FromDomain: "broker.example", Subject: "Re", Body: c.body})
		t.Logf("%q -> %s %.2f review=%v", c.body, got.Type, got.Confidence, got.NeedsReview)
		if c.bad != "" && got.Type == c.bad {
			t.Errorf("%q: %s", c.body, got.Type)
		}
	}
}
