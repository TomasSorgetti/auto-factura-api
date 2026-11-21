package secrets

import (
	"regexp"
	"testing"
)

func TestNewID(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		if !re.MatchString(id) {
			t.Fatalf("bad format: %s", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id: %s", id)
		}
		seen[id] = struct{}{}
	}
}
