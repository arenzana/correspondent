// Package secretbox encrypts small secrets that readers give the app (an AI
// API key) before they are stored in the database.
//
// The encryption key lives in its own file beside the database and is kept
// out of backups, so a copy of the database or a backup never holds a usable
// secret. Losing the key file (a restore onto a new host) only means the
// secrets must be entered again: Open reports ErrUnreadable.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
)

const keySize = 32

var ErrUnreadable = errors.New("the secret can't be decrypted here; enter it again")

type Box struct {
	aead cipher.AEAD
}

// Load reads the key file, creating it (mode 0600) on first use.
func Load(path string) (*Box, error) {
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, keySize)
		rand.Read(key)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return Load(path) // created concurrently
		}
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("%s: key must be %d bytes", path, keySize)
	}
	return New(key)
}

func New(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext bound to context (for example "user:12:ai"), so a
// sealed value can't be moved to another account or purpose.
func (b *Box) Seal(plaintext []byte, context string) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	rand.Read(nonce)
	return b.aead.Seal(nonce, nonce, plaintext, []byte(context))
}

func (b *Box) Open(sealed []byte, context string) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrUnreadable
	}
	out, err := b.aead.Open(nil, sealed[:n], sealed[n:], []byte(context))
	if err != nil {
		return nil, ErrUnreadable
	}
	return out, nil
}
