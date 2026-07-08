package key

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

func ParsePublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, apperr.New("key.parse_public_key.decode_pem", apperr.KindConfig, fmt.Errorf("failed to decode PEM block"))
	}

	if block.Type != "RSA PUBLIC KEY" {
		return nil, apperr.New("key.parse_public_key.type", apperr.KindConfig, fmt.Errorf("invalid key type %q", block.Type))
	}

	k, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, apperr.New("key.parse_public_key.parse_pkcs1", apperr.KindConfig, fmt.Errorf("failed to parse public key: %w", err))
	}

	return k, nil
}
