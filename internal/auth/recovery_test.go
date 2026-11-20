package auth

import (
	"regexp"
	"testing"
)

func TestRecoveryCodes(t *testing.T) {
	t.Parallel()
	codes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCount {
		t.Fatalf("len = %d", len(codes))
	}
	re := regexp.MustCompile(`^[A-Z2-7]{5}-[A-Z2-7]{5}$`)
	seen := map[string]bool{}
	for _, c := range codes {
		if !re.MatchString(c) {
			t.Errorf("bad format %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate %q", c)
		}
		seen[c] = true
	}
	if got := normalizeRecoveryCode(" abcde-fghij "); got != "ABCDEFGHIJ" {
		t.Fatalf("normalize = %q", got)
	}
}
