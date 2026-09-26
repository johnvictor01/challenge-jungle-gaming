package application

import (
	"crypto/rand"
	"encoding/hex"
)

// UUIDGenerator cria UUIDs v4 para transações, ledger e eventos persistidos.
// A carteira já gera seu próprio UUID no domínio.
type UUIDGenerator struct{}

func (UUIDGenerator) NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
