package main

import (
	"errors"

	"github.com/zalando/go-keyring"

	"github.com/lsj5031/joplin-lite/internal/assistant"
)

const (
	askKeychainService = "Joplin Lite Ask"
	askKeychainAccount = "model-api-key"
)

// askKeychain keeps the optional model credential out of assistant.sqlite and
// out of the launch config. It is deliberately a small adapter: all policy
// (whether to remember, clearing on endpoint changes) stays in Manager.
type askKeychain struct{}

func newAskKeychain() assistant.SecretStore { return askKeychain{} }

func (askKeychain) Load() (string, error) {
	value, err := keyring.Get(askKeychainService, askKeychainAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", assistant.ErrSecretNotFound
	}
	return value, err
}

func (askKeychain) Save(value string) error {
	if value == "" {
		return askKeychain{}.Delete()
	}
	return keyring.Set(askKeychainService, askKeychainAccount, value)
}

func (askKeychain) Delete() error {
	err := keyring.Delete(askKeychainService, askKeychainAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return assistant.ErrSecretNotFound
	}
	return err
}
