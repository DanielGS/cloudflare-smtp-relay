package email

import "testing"

func TestAddress_Domain(t *testing.T) {
	tests := []struct {
		name string
		addr Address
		want string
	}{
		{
			name: "lowercase domain",
			addr: Address{Address: "user@mydomainexample.com"},
			want: "mydomainexample.com",
		},
		{
			name: "uppercase domain is lowered",
			addr: Address{Address: "user@MyDomainExample.COM"},
			want: "mydomainexample.com",
		},
		{
			name: "no @ yields empty domain",
			addr: Address{Address: "not-an-address"},
			want: "",
		},
		{
			name: "empty address yields empty domain",
			addr: Address{Address: ""},
			want: "",
		},
		{
			name: "quoted local part containing @ still resolves the last @ as the domain separator",
			addr: Address{Address: `"a@b"@mydomainexample.com`},
			want: "mydomainexample.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addr.Domain(); got != tt.want {
				t.Errorf("Domain() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddress_String(t *testing.T) {
	tests := []struct {
		name string
		addr Address
		want string
	}{
		{
			name: "no display name falls back to the bare address",
			addr: Address{Address: "user@mydomainexample.com"},
			want: "user@mydomainexample.com",
		},
		{
			name: "display name is rendered before the bracketed address",
			addr: Address{Name: "Alice Smith", Address: "alice@mydomainexample.com"},
			want: "Alice Smith <alice@mydomainexample.com>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.addr.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessage_RecipientCount(t *testing.T) {
	msg := &Message{
		To:  []Address{{Address: "to1@example.com"}, {Address: "to2@example.com"}},
		Cc:  []Address{{Address: "cc1@example.com"}},
		Bcc: []Address{{Address: "bcc1@example.com"}, {Address: "bcc2@example.com"}, {Address: "bcc3@example.com"}},
	}

	if got, want := msg.RecipientCount(), 6; got != want {
		t.Errorf("RecipientCount() = %d, want %d", got, want)
	}
}

func TestMessage_RecipientCount_Empty(t *testing.T) {
	msg := &Message{}
	if got, want := msg.RecipientCount(), 0; got != want {
		t.Errorf("RecipientCount() = %d, want %d", got, want)
	}
}

func TestMessage_AllRecipients_Ordering(t *testing.T) {
	msg := &Message{
		To:  []Address{{Address: "to1@example.com"}, {Address: "to2@example.com"}},
		Cc:  []Address{{Address: "cc1@example.com"}},
		Bcc: []Address{{Address: "bcc1@example.com"}},
	}

	want := []string{"to1@example.com", "to2@example.com", "cc1@example.com", "bcc1@example.com"}
	got := msg.AllRecipients()

	if len(got) != len(want) {
		t.Fatalf("AllRecipients() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllRecipients()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMessage_AllRecipients_Empty(t *testing.T) {
	msg := &Message{}
	got := msg.AllRecipients()
	if len(got) != 0 {
		t.Errorf("AllRecipients() = %v, want empty", got)
	}
}
