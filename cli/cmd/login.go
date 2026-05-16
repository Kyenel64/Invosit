package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kyenel64/invosit/cli/internal/kratos"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to your invosit account",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Print("Email: ")
		email, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read email: %w", err)
		}
		email = strings.TrimSpace(email)

		fmt.Print("Password: ")
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password := string(passwordBytes)

		client := kratos.NewClient("http://localhost:4433") // TODO: config kratosURL
		token, err := client.Login(cmd.Context(), email, password)
		if err != nil {
			if errors.Is(err, kratos.ErrInvalidCredentials) {
				return errors.New("invalid email or password")
			}
			return err
		}

		fmt.Println("logged in")
		fmt.Println(token)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
