// Package emailstest provides an in-memory emails.Sender for tests.
package emailstest

import (
	"context"
	"sync"

	"github.com/TomasSorgetti/auto-factura-api/internal/emails"
)

// Fake records sent messages and returns Err from Send when it is set.
type Fake struct {
	mu   sync.Mutex
	sent []emails.Message
	err  error
}

func (f *Fake) Send(_ context.Context, m emails.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *Fake) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *Fake) Sent() []emails.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]emails.Message(nil), f.sent...)
}
