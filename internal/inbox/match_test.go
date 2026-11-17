package inbox

import (
	"testing"

	"github.com/emersion/go-imap"

	"github.com/drumandbytes/delistd/data"
	"github.com/drumandbytes/delistd/internal/broker"
	"github.com/drumandbytes/delistd/internal/config"
)

// catalogMonitor builds a Monitor over the broker list shipped in the binary.
func catalogMonitor(t *testing.T) *Monitor {
	t.Helper()
	db, err := broker.Parse(data.BrokersYAML)
	if err != nil {
		t.Fatal(err)
	}
	return NewMonitor(config.InboxConfig{}, db.Brokers)
}

func envelope(mailbox, host, name, subject string) *imap.Message {
	return &imap.Message{Envelope: &imap.Envelope{
		Subject: subject,
		From:    []*imap.Address{{PersonalName: name, MailboxName: mailbox, HostName: host}},
	}}
}

// Real envelopes of replies sent through privacy services (Nov 2026).
func TestMatchPrivacyServiceReplies(t *testing.T) {
	m := catalogMonitor(t)
	cases := []struct {
		mailbox, host, name, subject, want string
	}{
		// Retention.com runs GetEmails' opt-outs.
		{"optouts", "retention.com", "Retention.com", "Opt-Out Confirmation", "getemails"},
		{"optouts", "retention.com", "Retention.com", "CCPA Request Received", "getemails"},
		{"optouts", "Retention.com", "", "", "getemails"},
		// DataGrail: broker in the subdomain, and in the subject.
		{"privacy", "6sense.mail.datagrail.io", "", "Privacy Request / 6sense.com Deletion Results", "6sense-insights"},
		{"privacy", "6sense.mail.datagrail.io", "", "Privacy Request Confirmation", "6sense-insights"},
		// TrustSuperset: broker domain in the display name.
		{"noreply", "trustsuperset.com", "Privacy Team at revoptimal.com", "Confirmation: Your request to exercise your Right to Erasure has been completed", "revoptimal"},
		// OneTrust: broker name in the display name.
		{"noreply", "m.onetrust.com", "On behalf of AtData", "(Request ID: J3XKBAJHXH) Your Privacy Request", "atdata"},
		{"noreply", "m.onetrust.com", "ActivImpact Privacy Team", "(Request ID: WSSCW78WD5) Your Privacy Request", "activimpact"},
		{"noreply", "m.onetrust.com", "On behalf of Cybba", "(Request ID: ANLYMKR5LB) Your Privacy Request", "cybba"},
		// The broker's own domain still wins as before.
		{"privacy", "atdata.com", "AtData", "Re: request", "atdata"},
	}
	for _, c := range cases {
		e, err := m.parseMessage(envelope(c.mailbox, c.host, c.name, c.subject), &imap.BodySectionName{})
		if err != nil {
			t.Fatal(err)
		}
		if e.BrokerID != c.want {
			t.Errorf("%s@%s %q %q: matched %q, want %q", c.mailbox, c.host, c.name, c.subject, e.BrokerID, c.want)
		}
		if c.want != "" && e.BrokerName == "" {
			t.Errorf("%s: BrokerName empty", c.want)
		}
	}
}

// Mail from a privacy service, or from any big shared platform, must not be
// pinned on a broker without a clear, single hint.
func TestMatchNoGuessing(t *testing.T) {
	m := catalogMonitor(t)
	cases := []struct{ mailbox, host, name, subject string }{
		// Service mail naming no broker in the catalog.
		{"noreply", "m.onetrust.com", "GBG Privacy and Data Compliance Team", "(Request ID: 83W6YA6MMX) Your Privacy Request Needs Attention"},
		{"noreply", "m.onetrust.com", "On Behalf Of Ziff Davis' GDPR Team", "(Request ID: KRBQHX7ATD) Request logged successfully"},
		{"noreply", "m.onetrust.com", "", "Your Privacy Request"},
		{"noreply", "onetrust.com", "OneTrust", "Your OneTrust account"},
		{"privacy", "mail.datagrail.io", "", "Privacy Request Confirmation"},
		{"noreply", "trustsuperset.com", "Privacy Team at trustsuperset.com", "Confirmation"},
		// Two brokers named: ambiguous, so no match.
		{"noreply", "m.onetrust.com", "AtData and Cybba", "Your Privacy Request"},
		{"noreply", "trustsuperset.com", "Privacy Team at revoptimal.com", "about atdata.com"},
		// Hosts that are opt-out platforms for some brokers, never a reply.
		{"no-reply", "google.com", "Google", "Security alert"},
		{"forms-receipts-noreply", "google.com", "Google Forms", "Your response"},
		{"noreply", "hubspot.com", "HubSpot", "Your request"},
		{"x", "gmail.com", "Someone", "Hi"},
		{"x", "app.retention.com", "", ""}, // only the exact relay domain counts
		{"x", "notretention.com", "", ""},
		// A name hint is only read from a privacy service.
		{"news", "example.com", "On behalf of AtData", "AtData news"},
		// Empty envelope.
		{"", "", "", ""},
	}
	for _, c := range cases {
		e, err := m.parseMessage(envelope(c.mailbox, c.host, c.name, c.subject), &imap.BodySectionName{})
		if err != nil {
			t.Fatal(err)
		}
		if e.BrokerID != "" {
			t.Errorf("%s@%s %q %q: matched %q, want none", c.mailbox, c.host, c.name, c.subject, e.BrokerID)
		}
	}
}

// Hint rules on a small synthetic catalog.
func TestMatchHintRules(t *testing.T) {
	list := []broker.Broker{
		{ID: "acme", Name: "Acme, Inc.", Email: "privacy@acme.com", Website: "https://acme.com"},
		{ID: "acme-data", Name: "Acme Data LLC", Email: "dpo@acmedata.io"},
		{ID: "data-co", Name: "Data Co", Email: "x@dataco.net"},
		{ID: "twin-a", Name: "Twin", Email: "a@twin.com"},
		{ID: "twin-b", Name: "Twin", Email: "b@twin.org"},
	}
	m := NewMonitor(config.InboxConfig{}, list)
	cases := []struct{ host, name, subject, want string }{
		// Longer name wins over a name inside it.
		{"m.onetrust.com", "On behalf of Acme Data", "", "acme-data"},
		{"m.onetrust.com", "On behalf of ACME", "", "acme"},
		// Partial words never match.
		{"m.onetrust.com", "Acmeville Privacy", "", ""},
		// Generic names are not names ("Data Co" -> "data").
		{"m.onetrust.com", "Data Privacy Team", "", ""},
		// Two brokers with one name: ambiguous.
		{"m.onetrust.com", "Twin Privacy", "", ""},
		// Subdomain on a broker domain host resolves to the parent domain.
		{"trustsuperset.com", "Privacy Team at privacy.acme.com", "", "acme"},
		// Subdomain label: shared label is ambiguous; a unique one matches.
		{"twin.mail.datagrail.io", "", "", ""},
		{"acmedata.mail.datagrail.io", "", "", "acme-data"},
		// A domain hint beats a name hint.
		{"m.onetrust.com", "Acme Data", "about acme.com", "acme"},
		// Unknown broker in catalog for the relay: no match, no panic.
		{"retention.com", "", "", ""},
		// A lookalike host decides alone: no fall-through to the name.
		{"trustsuperset.com", "Privacy Team at acme.com.evil.io", "", ""},
		{"m.onetrust.com", "On behalf of Acme", "see evil.io", ""},
		// Dates and versions are not hosts, so the name still matches.
		{"m.onetrust.com", "On behalf of Acme", "Request 11.16.2026 v2.3", "acme"},
		// Trailing dot and case in the sender host.
		{"M.OneTrust.com.", "On behalf of Acme", "", "acme"},
	}
	for _, c := range cases {
		b, ok := m.index.match("x@"+c.host, c.host, c.name, c.subject)
		if (c.want == "") == ok || b.ID != c.want {
			t.Errorf("%q %q %q: got %q ok=%v, want %q", c.host, c.name, c.subject, b.ID, ok, c.want)
		}
	}
}

// The broker list must keep the relay targets the matcher names.
func TestRelaySendersExistInCatalog(t *testing.T) {
	m := catalogMonitor(t)
	for domain, id := range relaySenders {
		if _, ok := m.index.byID[id]; !ok {
			t.Errorf("relay %s -> %s: broker not in data/brokers.yaml", domain, id)
		}
	}
}

// A broker on a free mailbox owns its address, not gmail.com: other Gmail
// senders (the user's own test mail) were filed under it before.
func TestMatchFreeMailBrokerByAddress(t *testing.T) {
	m := NewMonitor(config.InboxConfig{}, []broker.Broker{
		{ID: "tiny", Name: "Tiny Lists", Email: "Tiny.Lists@gmail.com", Website: "https://tinylists.com"},
	})
	cases := []struct{ mailbox, host, want string }{
		{"tiny.lists", "gmail.com", "tiny"},
		{"TINY.LISTS", "GMAIL.COM", "tiny"},
		{"someone.else", "gmail.com", ""},
		{"privacy", "tinylists.com", "tiny"},
		{"tiny.lists", "googlemail.com", ""},
	}
	for _, c := range cases {
		e, err := m.parseMessage(envelope(c.mailbox, c.host, "", "Re: request"), &imap.BodySectionName{})
		if err != nil {
			t.Fatal(err)
		}
		if e.BrokerID != c.want {
			t.Errorf("%s@%s: matched %q, want %q", c.mailbox, c.host, e.BrokerID, c.want)
		}
	}
}

// The requester's own address or a file name in the subject is no broker
// hint and must not block a name match (review N1).
func TestMatchIgnoresRequesterAndFileHosts(t *testing.T) {
	list := []broker.Broker{{ID: "acme", Name: "Acme, Inc.", Email: "privacy@acme.com"}}
	m := NewMonitor(config.InboxConfig{Email: "me@Home-Domain.uk"}, list)
	cases := []struct{ name, subject, want string }{
		{"On behalf of Acme", "Your Privacy Request for jane@gmail.com", "acme"},
		{"On behalf of Acme", "Request for me@home-domain.uk", "acme"},
		{"On behalf of Acme", "report.PDF attached", "acme"},
		{"On behalf of Acme", "Your data export: data.csv and photo.jpeg", "acme"},
		// A real unknown host still blocks.
		{"On behalf of Acme", "see other-broker.com", ""},
	}
	for _, c := range cases {
		b, ok := m.index.match("noreply@m.onetrust.com", "m.onetrust.com", c.name, c.subject)
		if (c.want == "") == ok || b.ID != c.want {
			t.Errorf("%q %q: got %q ok=%v, want %q", c.name, c.subject, b.ID, ok, c.want)
		}
	}
}
