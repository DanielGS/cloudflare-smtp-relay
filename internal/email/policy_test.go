package email

import (
	"errors"
	"testing"
)

func TestSenderPolicy_Allows(t *testing.T) {
	tests := []struct {
		name    string
		domains []string
		from    Address
		want    bool
	}{
		{
			name:    "nil allowlist allows any sender",
			domains: nil,
			from:    Address{Address: "user@anything.example"},
			want:    true,
		},
		{
			name:    "empty allowlist allows any sender",
			domains: []string{},
			from:    Address{Address: "user@anything.example"},
			want:    true,
		},
		{
			name:    "exact domain match is allowed",
			domains: []string{"mydomainexample.com"},
			from:    Address{Address: "user@mydomainexample.com"},
			want:    true,
		},
		{
			name:    "matching is case-insensitive on the domain",
			domains: []string{"MyDomainExample.COM"},
			from:    Address{Address: "user@mydomainexample.com"},
			want:    true,
		},
		{
			name:    "matching is case-insensitive on the sender address too",
			domains: []string{"mydomainexample.com"},
			from:    Address{Address: "user@MYDOMAINEXAMPLE.COM"},
			want:    true,
		},
		{
			name:    "a subdomain does not inherit its parent domain's allowance",
			domains: []string{"subdomain.mydomainexample.com"},
			from:    Address{Address: "user@mydomainexample.com"},
			want:    false,
		},
		{
			name:    "an unrelated address sharing a domain suffix is rejected",
			domains: []string{"subdomain.mydomainexample.com"},
			from:    Address{Address: "user@evil-subdomain.mydomainexample.com"},
			want:    false,
		},
		{
			name:    "a domain outside the allowlist is rejected",
			domains: []string{"mydomainexample.com"},
			from:    Address{Address: "user@otherdomain.com"},
			want:    false,
		},
		{
			name:    "an address with no domain part is rejected when an allowlist is configured",
			domains: []string{"mydomainexample.com"},
			from:    Address{Address: "no-domain-here"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSenderPolicy(tt.domains)
			if got := p.Allows(tt.from); got != tt.want {
				t.Errorf("Allows(%q) = %v, want %v", tt.from.Address, got, tt.want)
			}
		})
	}
}

func TestSenderPolicy_Check_Allowed(t *testing.T) {
	p := NewSenderPolicy([]string{"mydomainexample.com"})
	if err := p.Check(Address{Address: "user@mydomainexample.com"}); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

func TestSenderPolicy_Check_Rejected(t *testing.T) {
	p := NewSenderPolicy([]string{"mydomainexample.com"})
	from := Address{Address: "user@evil.com"}

	err := p.Check(from)
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}
	if !errors.Is(err, ErrSenderNotAllowed) {
		t.Errorf("Check() error does not wrap ErrSenderNotAllowed: %v", err)
	}
	if got := err.Error(); got == "" {
		t.Fatal("Check() error message is empty")
	}
}

func TestSenderPolicy_Check_NilAllowlistAllowsAny(t *testing.T) {
	p := NewSenderPolicy(nil)
	if err := p.Check(Address{Address: "anyone@anywhere.example"}); err != nil {
		t.Fatalf("Check() = %v, want nil for an unrestricted policy", err)
	}
}
