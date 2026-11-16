package inbox

import "testing"

// Real replies sent through privacy services (Nov 2026), address anonymised.
func TestClassifyPrivacyServiceReplies(t *testing.T) {
	cases := []struct {
		name string
		e    Email
		want ResponseType
	}{
		{"retention confirmation", Email{From: "optouts@retention.com", FromName: "Retention.com", FromDomain: "retention.com", Subject: "Opt-Out Confirmation",
			Body: "( https://retention.com )\n\nHi there,\n\nWe have processed your Opt-Out removal request.\n\nuser@example.com has been marked for removal in our vendor database.\n\nThanks,\nThe Retention.com Privacy Team."}, ResponseSuccess},
		{"retention received", Email{From: "optouts@retention.com", FromDomain: "retention.com", Subject: "CCPA Request Received",
			Body: "Hi there,\n\nWe have received your Opt-Out removal request. We will process your request within 7 business days.\n\n-The Retention.com Privacy Team"}, ResponsePending},
		{"6sense results", Email{From: "privacy@6sense.mail.datagrail.io", FromDomain: "6sense.mail.datagrail.io", Subject: "Privacy Request / 6sense.com Deletion Results",
			HTMLBody: "<div>Dear Requester,</div><div> </div><div>You are receiving this email because an individual using the email address user@example.com submitted a privacy Deletion request to 6sense (6sense.com).</div><div> </div><div>We have completed the processing of this request and all personal identifiable data has been <b><strong>permanently deleted from our systems in accordance with our data retention and deletion practices</strong></b>.</div><div> </div><div>If you submitted this request and have any questions regarding the outcome, or believe additional action is needed, please contact our Privacy Team at <b><strong>datasubjectrequests@6sense.com</strong></b>.</div><div> </div><div>Thank you,</div><div><b><strong>6sense Privacy Team</strong></b></div>"}, ResponseSuccess},
		{"6sense received", Email{From: "privacy@6sense.mail.datagrail.io", FromDomain: "6sense.mail.datagrail.io", Subject: "Privacy Request Confirmation",
			Body: "Dear Requester,\n\nWe have received your Deletion request. If you did not submit this request, please notify us immediately. Otherwise, no action is necessary. You will receive another email once your request has been processed."}, ResponsePending},
		{"trustsuperset erasure", Email{From: "noreply@trustsuperset.com", FromName: "Privacy Team at revoptimal.com", FromDomain: "trustsuperset.com", Subject: "Confirmation: Your request to exercise your Right to Erasure has been completed",
			Body: "Hi Giedrius,\n\nWe are writing to confirm that your request to exercise your Right to Erasure has been completed in accordance with our legal requirements and the practices outlined in our Privacy Policy."}, ResponseSuccess},
		{"trustsuperset data rights", Email{From: "noreply@trustsuperset.com", FromName: "Privacy Team at visualvisitor.com", FromDomain: "trustsuperset.com", Subject: "Confirmation: Your request has been completed",
			Body: "Dear Giedrius,\n\nThis message confirms that your request to exercise your data rights has been completed. To the extent that any personal data associated with you existed within our systems, it has been deleted."}, ResponseSuccess},
		{"trustsuperset opt-out", Email{From: "noreply@trustsuperset.com", FromName: "Privacy Team at brooksim.com", FromDomain: "trustsuperset.com", Subject: "Confirmation: Your opt-out request has been completed",
			Body: "Greetings,\n\nWe are writing to confirm that your request to exercise your right to opt-out has been completed. We are committed to protecting and respecting your right to privacy."}, ResponseSuccess},
	}
	for _, c := range cases {
		got := ClassifyResponse(&c.e)
		if got.Type != c.want {
			t.Errorf("%s: %s (%.2f, %s), want %s", c.name, got.Type, got.Confidence, got.Reason, c.want)
		}
		if c.want == ResponseSuccess && got.NeedsReview {
			t.Errorf("%s: success but NeedsReview (%.2f)", c.name, got.Confidence)
		}
	}
}

// The new success phrases must not turn a promise into a confirmation.
func TestClassifyServicePhrasesNotOverReaching(t *testing.T) {
	cases := []struct{ body string }{
		{"Your email will be marked for removal once we verify your identity. Please confirm your email by clicking the link below."},
		{"We will process your opt-out within 45 days. You will receive another email once it has been processed."},
		{"Your request to exercise your right to deletion has been received and will be completed within 30 days."},
	}
	for _, c := range cases {
		got := ClassifyResponse(&Email{From: "privacy@broker.example", FromDomain: "broker.example", Subject: "Your request", Body: c.body})
		if got.Type == ResponseSuccess && !got.NeedsReview {
			t.Errorf("%q: confident success (%.2f)", c.body, got.Confidence)
		}
	}
}
