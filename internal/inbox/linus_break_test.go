package inbox

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap"
)

// linus review of 2aca0c0 (task 174): edge cases aimed at breaking the matcher.
func TestLinusBreakMatch(t *testing.T) {
	m := catalogMonitor(t)
	cases := []struct {
		name, mailbox, host, fromName, subject, want string
	}{
		{"upper-case service host", "privacy", "6SENSE.MAIL.DATAGRAIL.IO", "", "Privacy Request Confirmation", "6sense-insights"},
		{"trailing dot host", "optouts", "retention.com.", "", "", "getemails"},
		{"retention subdomain not in relay list", "noreply", "app.retention.com", "", "", ""},
		{"two broker domains in subject", "noreply", "trustsuperset.com", "", "revoptimal.com and brooksim.com", ""},
		{"domain hint beats name hint", "noreply", "m.onetrust.com", "On behalf of AtData", "brooksim.com request", "brooks-integrated-marketing"},
		{"only service names", "noreply", "m.onetrust.com", "OneTrust Privacy", "trustsuperset.com onetrust.com", ""},
		{"generic display name", "noreply", "m.onetrust.com", "Privacy Team", "Your Privacy Request", ""},
		{"only legal words", "noreply", "m.onetrust.com", "LLC Inc Ltd", "", ""},
		{"empty everything on service", "", "trustsuperset.com", "", "", ""},
		{"version numbers in subject", "noreply", "m.onetrust.com", "", "v2.3.4 ref 1.2 a.b", ""},
		{"user gmail in name + broker domain", "noreply", "trustsuperset.com", "user@gmail.com via revoptimal.com", "", "revoptimal"},
		{"subdomain of broker in hint", "noreply", "trustsuperset.com", "privacy.revoptimal.com", "", "revoptimal"},
		{"lookalike domain no match", "noreply", "trustsuperset.com", "Privacy Team at revoptimal.com.evil.io", "", ""},
		{"broker name inside a word", "noreply", "m.onetrust.com", "On behalf of AtDataX", "", ""},
		{"gmail stranger", "someone", "gmail.com", "Skipmasher", "removal", ""},
		{"service-like lookalike domain", "noreply", "notonetrust.com", "On behalf of AtData", "", ""},
		{"own broker domain unchanged", "privacy", "revoptimal.com", "", "", "revoptimal"},
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

func TestLinusBreakHugeSubject(t *testing.T) {
	m := catalogMonitor(t)
	subj := strings.Repeat("a.b ", 50000) + "revoptimal.com"
	e, err := m.parseMessage(envelope("noreply", "trustsuperset.com", strings.Repeat("Data ", 20000), subj), &imap.BodySectionName{})
	if err != nil {
		t.Fatal(err)
	}
	if e.BrokerID != "revoptimal" {
		t.Errorf("huge subject: %q", e.BrokerID)
	}
}

// A promise ("will be ... deleted") next to an ack must never be a confident
// success, and the verdict must be the same on every run (map order).
func TestLinusBreakClassifyPromises(t *testing.T) {
	bodies := []string{
		"We have received your request. Your data will be permanently deleted from our systems within 45 days.",
		"We received your deletion request. Once verified, your email will be marked for removal.",
		"Thank you for your request. Once we have processed your opt-out you will receive another email.",
		"Your request to exercise your right to erasure has been received. It has been completed for marketing data only after verification.",
	}
	for _, b := range bodies {
		first := ""
		for i := 0; i < 50; i++ {
			got := ClassifyResponse(&Email{From: "noreply@trustsuperset.com", FromDomain: "trustsuperset.com", Subject: "Your request", Body: b})
			if got.Type == ResponseSuccess && !got.NeedsReview {
				t.Errorf("%q: confident success (%.2f)", b, got.Confidence)
				break
			}
			v := string(got.Type)
			if first == "" {
				first = v
			} else if v != first {
				t.Errorf("%q: flaky verdict %s vs %s", b, first, v)
				break
			}
		}
	}
}
