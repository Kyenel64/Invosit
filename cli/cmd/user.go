package cmd

import (
	"errors"
	"fmt"

	"github.com/kyenel64/invosit/cli/internal/credstore"
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage user credentials",
}

var userGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Display current user information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fileStore, err := credstore.NewFileStore("")
		if err != nil {
			return fmt.Errorf("failed to create new filestore: %w", err)
		}

		creds, err := fileStore.Load()
		if err != nil {
			if errors.Is(err, credstore.ErrNotFound) {
				return errors.New("not logged in. Run 'invosit login'")
			}
			return fmt.Errorf("failed to load credentials: %w", err)
		}

		fmt.Printf("ID:    %s\n", creds.UserID)
		fmt.Printf("Email: %s\n", creds.Email)
		return nil
	},
}

func init() {
	userCmd.AddCommand(userGetCmd)
	rootCmd.AddCommand(userCmd)
}
