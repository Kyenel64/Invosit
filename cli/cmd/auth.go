package cmd

import (
	"errors"
	"fmt"

	"github.com/kyenel64/invosit/cli/internal/credstore"
)

// loadCredentials reads the stored session from the default filestore.
func loadCredentials() (credstore.Credentials, error) {
	fileStore, err := credstore.NewFileStore("")
	if err != nil {
		return credstore.Credentials{}, fmt.Errorf("failed to create new filestore: %w", err)
	}

	creds, err := fileStore.Load()
	if err != nil {
		if errors.Is(err, credstore.ErrNotFound) {
			return credstore.Credentials{}, errors.New("not logged in. Run 'invosit login'")
		}
		return credstore.Credentials{}, fmt.Errorf("failed to load credentials: %w", err)
	}

	return creds, nil
}
