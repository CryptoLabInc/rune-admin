package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newRoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "Manage authorization roles",
	}
	cmd.AddCommand(
		newRoleListCmd(),
		newRoleCreateCmd(),
		newRoleUpdateCmd(),
		newRoleDeleteCmd(),
	)
	return cmd
}

type roleResult struct {
	Name      string   `json:"name"`
	Scope     []string `json:"scope"`
	TopK      int      `json:"top_k"`
	RateLimit string   `json:"rate_limit"`
}

func newRoleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all roles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			var result struct {
				Roles []roleResult `json:"roles"`
			}
			if err := ac.Do("GET", "/roles", nil, &result); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(result.Roles) == 0 {
				fmt.Fprintln(out, "No roles defined.")
				return nil
			}
			// "{:<12} {:<50} {:>6} {:>10}" — match vault_admin_cli.py
			fmt.Fprintf(out, "%-12s %-50s %6s %10s\n", "ROLE", "SCOPE", "TOP_K", "RATE")
			for _, r := range result.Roles {
				fmt.Fprintf(out, "%-12s %-50s %6d %10s\n",
					r.Name, formatScope(r.Scope), r.TopK, r.RateLimit)
			}
			return nil
		},
	}
}

func newRoleCreateCmd() *cobra.Command {
	var name, scope, rateLimit string
	var topK int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopeList := splitCSV(scope)
			body := map[string]any{
				"name":       name,
				"scope":      scopeList,
				"top_k":      topK,
				"rate_limit": rateLimit,
			}
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			if err := ac.Do("POST", "/roles", body, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Role '%s' created.\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Role name")
	cmd.Flags().StringVar(&scope, "scope", "", "Comma-separated scope list")
	cmd.Flags().IntVar(&topK, "top-k", 0, "Max top_k")
	cmd.Flags().StringVar(&rateLimit, "rate-limit", "", "Rate limit (e.g. 30/60s)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("scope")
	_ = cmd.MarkFlagRequired("top-k")
	_ = cmd.MarkFlagRequired("rate-limit")
	return cmd
}

func newRoleUpdateCmd() *cobra.Command {
	var name, scope, rateLimit string
	var topK int
	var topKSet bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update an existing role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			if scope != "" {
				body["scope"] = splitCSV(scope)
			}
			if topKSet {
				body["top_k"] = topK
			}
			if rateLimit != "" {
				body["rate_limit"] = rateLimit
			}
			if len(body) == 0 {
				return fmt.Errorf("No fields to update.")
			}
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			if err := ac.Do("PUT", "/roles/"+name, body, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Role '%s' updated. Changes take effect immediately for all tokens with this role.\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Role name")
	cmd.Flags().StringVar(&scope, "scope", "", "Comma-separated scope list")
	cmd.Flags().IntVar(&topK, "top-k", 0, "Max top_k")
	cmd.Flags().StringVar(&rateLimit, "rate-limit", "", "Rate limit (e.g. 30/60s)")
	_ = cmd.MarkFlagRequired("name")
	cmd.PreRun = func(c *cobra.Command, _ []string) {
		topKSet = c.Flags().Changed("top-k")
	}
	return cmd
}

func newRoleDeleteCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ac, err := resolveAdminClient()
			if err != nil {
				return err
			}
			if err := ac.Do("DELETE", "/roles/"+name, nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Role '%s' deleted.\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Role name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func splitCSV(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
