package cmd

import (
	"fmt"

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
		creds, err := loadCredentials()
		if err != nil {
			return err
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
