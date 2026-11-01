// Package secretbox seals small secrets kept in the database (§11.4): an
// organization's LLM API key (QĐ-15) is stored so a database dump alone
// cannot use it. AES-256-GCM under a key derived from TOKEN_PEPPER and a
// purpose label — so two purposes never share a key — with associated data
// binding a sealed value to its row: copied onto another row it does not
// open. Rotating the pepper makes every sealed value unreadable.
//
// Platform code: it knows nothing about what the secrets are.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// ErrOpen: the sealed value is damaged, belongs to another row, or was
// sealed under another pepper.
var ErrOpen = errors.New("secretbox: sealed value does not open")

type Box struct{ aead cipher.AEAD }

// New derives the box for one purpose ("atp llm key v1", …) from the pepper.
func New(pepper []byte, purpose string) *Box {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte(purpose))
	block, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		panic("secretbox: aes: " + err.Error())
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic("secretbox: gcm: " + err.Error())
	}
	return &Box{aead: aead}
}

// Seal encrypts secret; ad (a row id) must be given again to Open.
func (b *Box) Seal(ad string, secret []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("secretbox: crypto/rand: " + err.Error())
	}
	return b.aead.Seal(nonce, nonce, secret, []byte(ad))
}

func (b *Box) Open(ad string, sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, ErrOpen
	}
	out, err := b.aead.Open(nil, sealed[:n], sealed[n:], []byte(ad))
	if err != nil {
		return nil, ErrOpen
	}
	return out, nil
}
