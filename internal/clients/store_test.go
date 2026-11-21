package clients

import "testing"

func TestEscapeLike(t *testing.T) {
	t.Parallel()
	if got := escapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("got %q", got)
	}
}

func TestQueryFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in string
		prefix   string
		pattern  string
	}{
		{"empty", "", "", ""},
		{"cuit prefix", "20-12345", "2012345", ""},
		{"digits only", "123", "123", ""},
		{"too many digits", "123456789012", "", "%123456789012%"},
		{"name", "acme", "", "%acme%"},
		{"wildcards escaped", "5%_", "", `%5\%\_%`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, n := queryFilter(tt.in)
			if (p == nil) != (tt.prefix == "") || (p != nil && *p != tt.prefix) {
				t.Fatalf("prefix = %v", p)
			}
			if (n == nil) != (tt.pattern == "") || (n != nil && *n != tt.pattern) {
				t.Fatalf("pattern = %v", n)
			}
		})
	}
}
