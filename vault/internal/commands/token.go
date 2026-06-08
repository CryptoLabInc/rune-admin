package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage authentication tokens",
	}
	cmd.AddCommand(
		newTokenIssueCmd(),
		newTokenRevokeCmd(),
		newTokenRotateCmd(),
		newTokenListCmd(),
	)
	return cmd
}

type tokenResult struct {
	User     string `json:"user"`
	Token    string `json:"token"`
	Role     string `json:"role"`
	IssuedAt string `json:"issued_at"`
	Expires  string `json:"expires"`
}

func newTokenIssueCmd() *cobra.Command {
	var user, role, expires string
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Issue a new token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if user == "" || role == "" {
				return fmt.Errorf("--user and --role are required")
			}
			body := map[string]any{"user": user, "role": role}
			if expires != "" {
				days, err := parseDuration(expires)
				if err != nil {
					return err
				}
				body["expires_days"] = days
			}
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			var result tokenResult
			if err := ac.Do("POST", "/tokens", body, &result); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "\nToken issued for '%s':\n", result.User)
			fmt.Fprintf(out, "  Role:    %s\n", result.Role)
			fmt.Fprintf(out, "  Expires: %s\n", result.Expires)
			fmt.Fprintf(out, "\n  Token: %s\n", result.Token)
			fmt.Fprintln(out, "\n  WARNING: This token will NOT be shown again. Share it securely.")
			return nil
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "Username")
	cmd.Flags().StringVar(&role, "role", "", "Role name")
	cmd.Flags().StringVar(&expires, "expires", "", "Duration until expiry (e.g. 90d, 12w, 6m)")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("role")
	return cmd
}

func newTokenRevokeCmd() *cobra.Command {
	var user string
	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a user's token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			var result struct {
				Message string `json:"message"`
			}
			if err := ac.Do("DELETE", "/tokens/"+user, nil, &result); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Message)
			return nil
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "Username")
	_ = cmd.MarkFlagRequired("user")
	return cmd
}

func newTokenRotateCmd() *cobra.Command {
	var user string
	var rotateAll bool
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate one or all tokens",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (user == "") == (!rotateAll) {
				return fmt.Errorf("exactly one of --user or --all is required")
			}
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if rotateAll {
				var result struct {
					Rotated int `json:"rotated"`
					Tokens  []struct {
						User  string `json:"user"`
						Token string `json:"token"`
						Role  string `json:"role"`
					} `json:"tokens"`
				}
				if err := ac.Do("POST", "/tokens/_rotate_all", map[string]any{}, &result); err != nil {
					return err
				}
				if result.Rotated == 0 {
					fmt.Fprintln(out, "No tokens to rotate.")
					return nil
				}
				fmt.Fprintf(out, "Rotated %d token(s):\n\n", result.Rotated)
				for _, t := range result.Tokens {
					fmt.Fprintf(out, "  %s: %s\n", t.User, t.Token)
				}
				fmt.Fprintln(out, "\n  WARNING: These tokens will NOT be shown again. Share them securely.")
				return nil
			}
			var result tokenResult
			if err := ac.Do("POST", "/tokens/"+user+"/rotate", map[string]any{}, &result); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nToken rotated for '%s':\n", result.User)
			fmt.Fprintf(out, "  Role:    %s\n", result.Role)
			fmt.Fprintf(out, "  Expires: %s\n", result.Expires)
			fmt.Fprintf(out, "\n  Token: %s\n", result.Token)
			fmt.Fprintln(out, "\n  WARNING: This token will NOT be shown again. Share it securely.")
			return nil
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "Username to rotate")
	cmd.Flags().BoolVar(&rotateAll, "all", false, "Rotate all tokens")
	return cmd
}

func newTokenListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tokens",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			var result struct {
				Tokens []struct {
					User      string `json:"user"`
					Role      string `json:"role"`
					TopK      any    `json:"top_k"`
					RateLimit any    `json:"rate_limit"`
					Expires   string `json:"expires"`
				} `json:"tokens"`
			}
			if err := ac.Do("GET", "/tokens", nil, &result); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(result.Tokens) == 0 {
				fmt.Fprintln(out, "No tokens issued.")
				return nil
			}
			// "{:<16} {:<10} {:>6} {:>10}  {:<12}" — match vault_admin_cli.py
			fmt.Fprintf(out, "%-16s %-10s %6s %10s  %-12s\n", "USER", "ROLE", "TOP_K", "RATE", "EXPIRES")
			for _, t := range result.Tokens {
				fmt.Fprintf(out, "%-16s %-10s %6s %10s  %-12s\n",
					t.User, t.Role, fmt.Sprintf("%v", t.TopK), fmt.Sprintf("%v", t.RateLimit), t.Expires)
			}
			return nil
		},
	}
}

// formatScope is referenced from role.go; defined here to share with token output if needed.
func formatScope(scope []string) string { return strings.Join(scope, ",") }
