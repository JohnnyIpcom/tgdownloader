package key

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

func TestParsePublicKeySuccess(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	pubBytes := x509.MarshalPKCS1PublicKey(&priv.PublicKey)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: pubBytes})

	parsed, err := ParsePublicKey(pemData)
	if err != nil {
		t.Fatalf("ParsePublicKey() error = %v", err)
	}

	if parsed.N.Cmp(priv.PublicKey.N) != 0 {
		t.Fatal("parsed public key does not match source key")
	}
}

func TestParsePublicKeyInvalidPEMKindConfig(t *testing.T) {
	t.Parallel()

	_, err := ParsePublicKey([]byte("not-a-pem"))
	if err == nil {
		t.Fatal("expected PEM decode error")
	}

	if !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("expected KindConfig, got: %v", err)
	}
}

func TestParsePublicKeyInvalidTypeKindConfig(t *testing.T) {
	t.Parallel()

	pemData := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{1, 2, 3}})

	_, err := ParsePublicKey(pemData)
	if err == nil {
		t.Fatal("expected invalid type error")
	}

	if !apperr.IsKind(err, apperr.KindConfig) {
		t.Fatalf("expected KindConfig, got: %v", err)
	}
}
