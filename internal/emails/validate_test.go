package emails

import (
	"errors"
	"strings"
	"testing"
)

func TestMessageValidate(t *testing.T) {
	t.Parallel()
	many := make([]string, 51)
	for i := range many {
		many[i] = "a@example.com"
	}
	tests := []struct {
		name   string
		mutate func(*Message)
		valid  bool
	}{
		{"valid", func(*Message) {}, true},
		{"no recipients", func(m *Message) { m.To = nil }, false},
		{"51 recipients", func(m *Message) { m.To = many }, false},
		{"address with name", func(m *Message) { m.To = []string{"Admin <admin@example.com>"} }, false},
		{"subject with newline", func(m *Message) { m.Subject = "a\nb" }, false},
		{"empty subject", func(m *Message) { m.Subject = "" }, false},
		{"body too big", func(m *Message) { m.HTML = strings.Repeat("a", 512*1024+1) }, false},
		{"body at limit", func(m *Message) { m.HTML = strings.Repeat("a", 512*1024) }, true},
		{"unknown kind", func(m *Message) { m.Kind = "other" }, false},
		{"idempotency key set", func(m *Message) { m.IdempotencyKey = "x" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := Message{Kind: KindDeviceAlert, To: []string{"admin@example.com"}, Subject: "Hola", HTML: "<p>hola</p>", Text: "hola"}
			tc.mutate(&m)
			err := m.validate()
			if tc.valid {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("err = %v, want ErrInvalidMessage", err)
			}
		})
	}
}
