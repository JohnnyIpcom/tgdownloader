package telegram

import (
	"os"
	"regexp"
	"strings"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/key"

	tgclient "github.com/gotd/td/telegram"
)

func findHashtags(input string) []string {
	hashtags := []string{}
	regex := regexp.MustCompile(`#[^\s#]+`)

	matches := regex.FindAllString(input, -1)
	for _, match := range matches {
		trimmedTag := strings.TrimPrefix(match, "#")
		hashtags = append(hashtags, trimmedTag)
	}

	return hashtags
}

func getPublicKeys(cfg config.Config) ([]tgclient.PublicKey, error) {
	if !cfg.IsSet("mtproto.public_keys") {
		return nil, nil
	}

	var keys []tgclient.PublicKey

	publicKeys := cfg.GetStringSlice("mtproto.public_keys")
	for _, publicKey := range publicKeys {
		publicKeyData, err := os.ReadFile(publicKey)
		if err != nil {
			return nil, err
		}

		key, err := key.ParsePublicKey(publicKeyData)
		if err != nil {
			return nil, err
		}

		keys = append(keys, tgclient.PublicKey{
			RSA: key,
		})
	}

	return keys, nil
}
