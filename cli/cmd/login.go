package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kyenel64/invosit/cli/internal/apiclient"
	"github.com/kyenel64/invosit/cli/internal/credstore"
	"github.com/kyenel64/invosit/cli/internal/kratos"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to your invosit account",
	RunE: func(cmd *cobra.Command, args []string) error {

		// --- Build filestore ---
		fileStore, err := credstore.NewFileStore("")
		if err != nil {
			return fmt.Errorf("failed to create new filestore: %w", err)
		}

		// --- Prompt email + password ---
		reader := bufio.NewReader(os.Stdin)

		fmt.Print("Email: ")
		email, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read email input: %w", err)
		}
		email = strings.TrimSpace(email)

		fmt.Print("Password: ")
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		password := string(passwordBytes)

		// --- Call login ---
		kratosClient := kratos.NewClient("http://localhost:4433") // TODO: config kratosURL
		token, err := kratosClient.Login(cmd.Context(), email, password)
		if err != nil {
			if errors.Is(err, kratos.ErrInvalidCredentials) {
				return errors.New("invalid email or password")
			}
			return err
		}

		// --- Retrieve user id ---
		apiClient := apiclient.NewClient("http://localhost:8080")
		user, err := apiClient.Me(cmd.Context(), token)
		if err != nil {
			if errors.Is(err, apiclient.ErrUnauthorized) {
				return errors.New("login succeeded but server doesn't recognize this user. Check the registration webhook")
			}
			return err
		}

		// --- Save credentials ---
		err = fileStore.Save(credstore.Credentials{
			Version:      credstore.SchemaVersion,
			Email:        email,
			UserID:       user.ID,
			SessionToken: token,
			KratosURL:    "http://localhost:4433",
			APIURL:       "http://localhost:8080",
			SavedAt:      time.Now(),
		})
		if err != nil {
			return fmt.Errorf("failed to save credentials: %w", err)
		}

		fmt.Printf("logged in as %s\n", user.Email)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
