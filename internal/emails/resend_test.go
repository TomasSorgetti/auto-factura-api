package emails

import (
	"errors"
	"testing"
	"time"

	"github.com/resend/resend-go/v4"
)

func TestClassifyResendError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		permanent bool
		retry     time.Duration
	}{
		{"422", &resend.ResendError{StatusCode: 422}, true, 0},
		{"400", &resend.ResendError{StatusCode: 400}, true, 0},
		{"429 with header", &resend.RateLimitError{RetryAfter: "30"}, false, 30 * time.Second},
		{"429 without header", &resend.RateLimitError{}, false, defaultRetryAfter},
		{"500", &resend.ResendError{StatusCode: 500}, false, 0},
		{"409", &resend.ResendError{StatusCode: 409}, false, 0},
		{"network", errors.New("connection reset"), false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyResendError(tc.err)
			var perm *PermanentError
			var retry *RetryAfterError
			if errors.As(got, &perm) != tc.permanent {
				t.Fatalf("permanent = %v, want %v (%v)", !tc.permanent, tc.permanent, got)
			}
			if isRetry := errors.As(got, &retry); isRetry != (tc.retry != 0) || (isRetry && retry.After != tc.retry) {
				t.Fatalf("retry = %v, want %v (%v)", retry, tc.retry, got)
			}
		})
	}
}
