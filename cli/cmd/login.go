package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kyenel64/invosit/cli/internal/apiclient"
	"github.com/kyenel64/invosit/cli/internal/credstore"
	"github.com/kyenel64/invosit/cli/internal/kratos"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	defaultKratosURL = "http://localhost:4433"
	defaultAPIURL    = "http://localhost:8080"
	defaultUIURL     = "http://127.0.0.1:5173"
)

var (
	loginFlagPassword bool
	loginFlagWeb      bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to invosit",
	RunE: func(cmd *cobra.Command, args []string) error {
		fileStore, err := credstore.NewFileStore("")
		if err != nil {
			return fmt.Errorf("failed to create new filestore: %w", err)
		}

		kratosURL := defaultKratosURL
		apiURL := defaultAPIURL

		var (
			email string
			token string
		)
		if loginFlagPassword {
			email, token, err = runPasswordLogin(cmd.Context(), kratosURL, cmd.InOrStdin(), cmd.ErrOrStderr())
		} else {
			email, token, err = runBrowserLogin(cmd.Context(), kratosURL, cmd.ErrOrStderr())
		}
		if err != nil {
			return err
		}

		apiClient := apiclient.NewClient(apiURL)
		user, err := apiClient.Me(cmd.Context(), token)
		if err != nil {
			if errors.Is(err, apiclient.ErrUnauthorized) {
				return errors.New("login succeeded but server doesn't recognize this user. Check the registration webhook")
			}
			return err
		}

		// If the password flow was used, the user typed their email already
		// and we have it directly; the browser flow doesn't know the email
		// up front, so we use whatever /auth/me returned. Use the API
		// response as authoritative either way.
		_ = email

		err = fileStore.Save(credstore.Credentials{
			Version:      credstore.SchemaVersion,
			Email:        user.Email,
			UserID:       user.ID,
			SessionToken: token,
			KratosURL:    kratosURL,
			APIURL:       apiURL,
			SavedAt:      time.Now(),
		})
		if err != nil {
			return fmt.Errorf("failed to save credentials: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "logged in as %s\n", user.Email)
		return nil
	},
}

func init() {
	loginCmd.Flags().BoolVar(&loginFlagPassword, "password", false, "Sign in by entering email and password in the terminal")
	loginCmd.Flags().BoolVar(&loginFlagWeb, "web", false, "Sign in via the browser (default)")
	loginCmd.MarkFlagsMutuallyExclusive("password", "web")
	rootCmd.AddCommand(loginCmd)
}

// runPasswordLogin runs the legacy email+password flow against the Kratos
// API endpoint. Returns the email the user typed (used downstream for
// /auth/me) and the session token.
func runPasswordLogin(ctx context.Context, kratosURL string, stdin io.Reader, stderr io.Writer) (string, string, error) {
	reader := bufio.NewReader(stdin)

	_, _ = fmt.Fprint(stderr, "Email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("failed to read email input: %w", err)
	}
	email = strings.TrimSpace(email)

	_, _ = fmt.Fprint(stderr, "Password: ")
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(stderr)
	if err != nil {
		return "", "", fmt.Errorf("failed to read password: %w", err)
	}
	password := string(passwordBytes)

	client := kratos.NewClient(kratosURL)
	token, err := client.Login(ctx, email, password)
	if err != nil {
		if errors.Is(err, kratos.ErrInvalidCredentials) {
			return "", "", errors.New("invalid email or password")
		}
		return "", "", err
	}
	return email, token, nil
}

// runBrowserLogin runs the browser-based login flow: opens the invosit
// web UI in a browser, waits on a loopback listener for the frontend to
// hand back a session token.
func runBrowserLogin(ctx context.Context, kratosURL string, stderr io.Writer) (string, string, error) {
	client := kratos.NewClient(kratosURL)
	token, err := client.BrowserLogin(ctx, kratos.BrowserLoginOpts{
		UIBaseURL: defaultUIURL,
		Timeout:   5 * time.Minute,
		Stderr:    stderr,
	})
	if err != nil {
		if errors.Is(err, kratos.ErrBrowserLoginTimeout) {
			return "", "", errors.New("browser sign-in timed out — try again, or use --password")
		}
		return "", "", err
	}
	return "", token, nil
}
