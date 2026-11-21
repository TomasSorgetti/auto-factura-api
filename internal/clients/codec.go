package clients

import (
	"fmt"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

const (
	fieldNotes        = "clients.notes"
	fieldContactName  = "client_contacts.name"
	fieldContactEmail = "client_contacts.email"
)

// Codec encrypts personal fields with AES-GCM bound to field and row id, and computes the email blind index.
type Codec struct {
	cipher *secrets.Cipher
	index  *secrets.BlindIndex
}

// NewCodec derives the client data and blind index subkeys from master (32 bytes).
func NewCodec(master []byte) (*Codec, error) {
	dataKey, err := secrets.DeriveKey(master, secrets.PurposeClientData)
	if err != nil {
		return nil, fmt.Errorf("derive client data key: %w", err)
	}
	cipher, err := secrets.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("new client cipher: %w", err)
	}
	indexKey, err := secrets.DeriveKey(master, secrets.PurposeClientBlindIndex)
	if err != nil {
		return nil, fmt.Errorf("derive client blind index key: %w", err)
	}
	index, err := secrets.NewBlindIndex(indexKey)
	if err != nil {
		return nil, fmt.Errorf("new client blind index: %w", err)
	}
	return &Codec{cipher: cipher, index: index}, nil
}

func aad(field, id string) []byte { return []byte(field + ":" + id) }

func (c *Codec) seal(field, id, plain string) ([]byte, error) {
	sealed, err := c.cipher.Encrypt([]byte(plain), aad(field, id))
	if err != nil {
		return nil, fmt.Errorf("seal %s %s: %w", field, id, err)
	}
	return sealed, nil
}

func (c *Codec) open(field, id string, sealed []byte) (string, error) {
	plain, err := c.cipher.Decrypt(sealed, aad(field, id))
	if err != nil {
		return "", fmt.Errorf("open %s %s: %w", field, id, err)
	}
	return string(plain), nil
}

func (c *Codec) emailIndex(normalized string) []byte { return c.index.Sum(normalized) }
