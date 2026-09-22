package email

import "fmt"

// SenderPolicy decides whether an envelope sender may use the relay.
//
// The allowlist is opt-in: a policy built from an empty or nil domain list
// allows every sender. Matching is exact and case-insensitive on the domain
// part only. A configured domain never matches its subdomains or its parent
// domain, so "tasks.midominio.com" does not permit "midominio.com" and does
// not permit "evil-tasks.midominio.com" either.
type SenderPolicy struct {
	allowed map[string]struct{}
}

// NewSenderPolicy builds a SenderPolicy that only allows senders whose domain
// exactly matches one of domains. An empty or nil domains list produces a
// policy that allows any sender.
func NewSenderPolicy(domains []string) *SenderPolicy {
	if len(domains) == 0 {
		return &SenderPolicy{}
	}

	allowed := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		allowed[lowerASCII(d)] = struct{}{}
	}
	return &SenderPolicy{allowed: allowed}
}

// Allows reports whether from may use the relay under this policy.
func (p *SenderPolicy) Allows(from Address) bool {
	if len(p.allowed) == 0 {
		return true
	}

	domain := from.Domain()
	if domain == "" {
		return false
	}

	_, ok := p.allowed[domain]
	return ok
}

// Check returns nil when from is allowed to use the relay, or an error
// wrapping ErrSenderNotAllowed naming the rejected domain when it is not.
func (p *SenderPolicy) Check(from Address) error {
	if p.Allows(from) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrSenderNotAllowed, from.Domain())
}
