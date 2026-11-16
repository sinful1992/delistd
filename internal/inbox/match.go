package inbox

import (
	"regexp"
	"strings"

	"github.com/drumandbytes/delistd/internal/broker"
)

// privacyServices send replies on behalf of many brokers from their own
// domain (noreply@m.onetrust.com, privacy@6sense.mail.datagrail.io,
// noreply@trustsuperset.com). The sender domain alone can't name the broker,
// so the envelope's display name, subject and sender subdomain are read for
// a hint instead.
var privacyServices = []string{"onetrust.com", "datagrail.io", "trustsuperset.com"}

// relaySenders are domains that answer for exactly one broker in the catalog
// (GetEmails' opt-outs are run by Retention.com). Keyed by sender domain.
var relaySenders = map[string]string{
	"retention.com": "getemails",
}

// freeMail domains host many unrelated senders, so they never identify a
// broker by domain; a broker using one is matched by its exact address.
var freeMail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "yahoo.com": true, "ymail.com": true,
	"outlook.com": true, "hotmail.com": true, "live.com": true, "msn.com": true,
	"aol.com": true, "icloud.com": true, "me.com": true, "mac.com": true,
	"protonmail.com": true, "proton.me": true, "gmx.com": true, "gmx.net": true,
	"mail.com": true, "zoho.com": true, "yandex.com": true,
}

// genericLabels are sender subdomain labels that never name a broker.
var genericLabels = map[string]bool{
	"m": true, "mail": true, "email": true, "e": true, "my": true, "www": true,
	"privacy": true, "noreply": true, "no-reply": true, "support": true,
	"requests": true, "request": true, "portal": true, "privacyportal": true,
	"notify": true, "notifications": true, "send": true, "mg": true, "app": true,
}

// legalWords are dropped from broker names before name matching.
var legalWords = map[string]bool{
	"llc": true, "inc": true, "incorporated": true, "ltd": true, "limited": true,
	"corp": true, "corporation": true, "co": true, "company": true, "plc": true,
	"gmbh": true, "sa": true, "bv": true, "lp": true, "llp": true, "the": true,
}

// genericNames are normalized names too common to identify a broker.
var genericNames = map[string]bool{
	"data": true, "privacy": true, "team": true, "privacy team": true,
	"marketing": true, "media": true, "group": true, "solutions": true,
}

var (
	// A host ends in a letters-only TLD, so "11.16.2026" or "v2.3" is no host.
	hostRe    = regexp.MustCompile(`(?i)[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\.[a-z]{2,}\b`)
	nonWordRe = regexp.MustCompile(`[^a-z0-9]+`)
)

type namedBroker struct {
	name string // normalized, space separated
	b    broker.Broker
}

// brokerIndex holds the lookups used to match a reply to a broker.
type brokerIndex struct {
	byDomain  map[string]broker.Broker // email and website domain
	byAddress map[string]broker.Broker // full address, for brokers on free mail
	byID      map[string]broker.Broker
	byLabel   map[string][]broker.Broker // first label of a broker domain ("6sense")
	names     []namedBroker
}

func buildIndex(byDomain map[string]broker.Broker, list []broker.Broker) brokerIndex {
	idx := brokerIndex{
		byDomain:  byDomain,
		byAddress: make(map[string]broker.Broker),
		byID:      make(map[string]broker.Broker, len(list)),
		byLabel:   make(map[string][]broker.Broker),
	}
	for _, b := range list {
		idx.byID[b.ID] = b
		if addr := strings.ToLower(strings.TrimSpace(b.Email)); strings.Contains(addr, "@") {
			idx.byAddress[addr] = b
		}
		seen := map[string]bool{}
		for _, d := range ownDomains(b) {
			if freeMail[d] {
				continue
			}
			label := strings.SplitN(d, ".", 2)[0]
			if !seen[label] {
				seen[label] = true
				idx.byLabel[label] = append(idx.byLabel[label], b)
			}
		}
		if n := normalizeName(b.Name); len(n) >= 4 && !genericNames[n] {
			idx.names = append(idx.names, namedBroker{name: n, b: b})
		}
	}
	return idx
}

// ownDomains are the broker's email and website domains.
func ownDomains(b broker.Broker) []string {
	var out []string
	if at := strings.LastIndex(b.Email, "@"); at >= 0 {
		if d := strings.ToLower(strings.TrimSpace(b.Email[at+1:])); d != "" {
			out = append(out, d)
		}
	}
	if d := b.Domain(); d != "" {
		out = append(out, d)
	}
	return out
}

func normalizeName(s string) string {
	words := strings.Fields(nonWordRe.ReplaceAllString(strings.ToLower(s), " "))
	kept := words[:0]
	for _, w := range words {
		if !legalWords[w] {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}

// serviceBase returns the privacy service domain that domain belongs to.
func serviceBase(domain string) (string, bool) {
	for _, s := range privacyServices {
		if domain == s || strings.HasSuffix(domain, "."+s) {
			return s, true
		}
	}
	return "", false
}

// match finds the broker a reply came from, using only envelope fields:
// the sender's own domain, a known single-broker relay, or - for mail sent
// by a shared privacy service - a hint in the display name, subject or
// sender subdomain. A hint that fits more than one broker matches nothing:
// a wrong broker is worse than none.
func (idx brokerIndex) match(from, fromDomain, fromName, subject string) (broker.Broker, bool) {
	fromDomain = strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	if fromDomain == "" {
		return broker.Broker{}, false
	}
	if freeMail[fromDomain] {
		b, ok := idx.byAddress[strings.ToLower(from)]
		return b, ok
	}
	if b, ok := idx.byDomain[fromDomain]; ok {
		return b, true
	}
	if id, ok := relaySenders[fromDomain]; ok {
		b, found := idx.byID[id]
		return b, found
	}
	base, ok := serviceBase(fromDomain)
	if !ok {
		return broker.Broker{}, false
	}
	// Strongest hint first. A written host name decides on its own: if it
	// names no single broker, nothing matches - a lookalike such as
	// "revoptimal.com.evil.io" must not fall through to a name match. Below
	// that, the first stage that names any broker decides, so an ambiguous
	// strong hint never falls through to a weak one.
	if found, sawHost := idx.byDomainHint(fromName+" "+subject, base); sawHost {
		return only(found)
	}
	if found := idx.byLabelHint(strings.TrimSuffix(fromDomain, base)); len(found) > 0 {
		return only(found)
	}
	return only(idx.byNameHint(fromName))
}

// byDomainHint matches host names and email addresses written in text
// ("Privacy Team at visualvisitor.com", "privacy@lightcast.io").
// sawHost reports whether the text holds any host other than a privacy
// service's own.
func (idx brokerIndex) byDomainHint(text, service string) (found map[string]broker.Broker, sawHost bool) {
	found = map[string]broker.Broker{}
	for _, h := range hostRe.FindAllString(strings.ToLower(text), -1) {
		h = strings.TrimPrefix(h, "www.")
		if _, isService := serviceBase(h); isService || h == service {
			continue
		}
		sawHost = true
		// privacy.lightcast.io -> lightcast.io, never below two labels.
		for d := h; strings.Count(d, ".") >= 1; d = d[strings.Index(d, ".")+1:] {
			if b, ok := idx.byDomain[d]; ok {
				found[b.ID] = b
				break
			}
		}
	}
	return found, sawHost
}

// byLabelHint matches a sender subdomain label to a broker domain's first
// label (privacy@6sense.mail.datagrail.io -> 6sense.com).
func (idx brokerIndex) byLabelHint(prefix string) map[string]broker.Broker {
	found := map[string]broker.Broker{}
	for _, label := range strings.Split(strings.Trim(prefix, "."), ".") {
		if len(label) < 3 || genericLabels[label] {
			continue
		}
		for _, b := range idx.byLabel[label] {
			found[b.ID] = b
		}
	}
	return found
}

// byNameHint matches a broker name, as whole words, in the display name
// ("On behalf of AtData" -> "AtData, LLC"). A name inside a longer matched
// name ("Acme" in "Acme Data") yields to the longer one.
func (idx brokerIndex) byNameHint(fromName string) map[string]broker.Broker {
	text := " " + normalizeName(fromName) + " "
	var hits []namedBroker
	for _, nb := range idx.names {
		if strings.Contains(text, " "+nb.name+" ") {
			hits = append(hits, nb)
		}
	}
	found := map[string]broker.Broker{}
	for _, h := range hits {
		inner := false
		for _, o := range hits {
			if o.b.ID != h.b.ID && len(o.name) > len(h.name) && strings.Contains(" "+o.name+" ", " "+h.name+" ") {
				inner = true
				break
			}
		}
		if !inner {
			found[h.b.ID] = h.b
		}
	}
	return found
}

func only(found map[string]broker.Broker) (broker.Broker, bool) {
	if len(found) != 1 {
		return broker.Broker{}, false
	}
	for _, b := range found {
		return b, true
	}
	return broker.Broker{}, false
}
