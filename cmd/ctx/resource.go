package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"ctx/internal/policy"
	"ctx/internal/resource"
	"ctx/internal/source"
)

func newResourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resource",
		Short: "Manage registered context resources",
	}
	cmd.AddCommand(newResourceAddCmd(), newResourceListCmd())
	return cmd
}

func newResourceAddCmd() *cobra.Command {
	var (
		sourceType string
		path       string
		ghOwner    string
		ghRepo     string
		ghRef      string
		policyName string
		maxAge     time.Duration
	)
	cmd := &cobra.Command{
		Use:   "add <uri>",
		Short: "Register a new context resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := args[0]
			parsed, err := resource.ParseURI(uri)
			if err != nil {
				return err
			}

			var cfg source.Config
			switch source.Kind(sourceType) {
			case source.KindFilesystem:
				if path == "" {
					return fmt.Errorf("--path is required for --source-type filesystem")
				}
				cfg = source.Config{Kind: source.KindFilesystem, Filesystem: &source.FilesystemConfig{Path: path}}
			case source.KindGitHub:
				if ghOwner == "" || ghRepo == "" || path == "" {
					return fmt.Errorf("--owner, --repo, and --path are required for --source-type github")
				}
				cfg = source.Config{Kind: source.KindGitHub, GitHub: &source.GitHubConfig{Owner: ghOwner, Repo: ghRepo, Path: path, Ref: ghRef}}
			default:
				return fmt.Errorf("unsupported --source-type %q (want filesystem or github)", sourceType)
			}

			var pol policy.Policy
			switch policy.Strategy(policyName) {
			case policy.StrategyRequireFresh:
				pol = policy.Policy{Strategy: policy.StrategyRequireFresh}
			case policy.StrategyAllowStale:
				pol = policy.Policy{Strategy: policy.StrategyAllowStale, MaxAge: maxAge}
			default:
				return fmt.Errorf("unsupported --policy %q (want require_fresh or allow_stale)", policyName)
			}

			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			res := resource.Resource{
				URI:          uri,
				Namespace:    parsed.Namespace,
				Path:         parsed.Path,
				SourceConfig: cfg,
				Policy:       pol,
			}
			if err := a.Resources.Create(context.Background(), res); err != nil {
				return err
			}

			fmt.Printf("Registered resource %s\n", uri)
			fmt.Printf("  source: %s\n", sourceType)
			fmt.Printf("  policy: %s\n", policyName)
			return nil
		},
	}
	cmd.Flags().StringVar(&sourceType, "source-type", "", "source type: filesystem | github (required)")
	cmd.Flags().StringVar(&path, "path", "", "path to the source content (file path for filesystem, path within repo for github)")
	cmd.Flags().StringVar(&ghOwner, "owner", "", "github: repository owner")
	cmd.Flags().StringVar(&ghRepo, "repo", "", "github: repository name")
	cmd.Flags().StringVar(&ghRef, "ref", "main", "github: branch or ref")
	cmd.Flags().StringVar(&policyName, "policy", string(policy.StrategyRequireFresh), "freshness policy: require_fresh | allow_stale")
	cmd.Flags().DurationVar(&maxAge, "max-age", 0, "allow_stale: maximum snapshot age before refresh")
	cmd.MarkFlagRequired("source-type")
	return cmd
}

func newResourceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered context resources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			resources, err := a.Resources.List(context.Background())
			if err != nil {
				return err
			}
			w := newTabWriter()
			fmt.Fprintln(w, "URI\tSOURCE\tPOLICY\tCURRENT SNAPSHOT")
			for _, r := range resources {
				snap := "-"
				if r.CurrentSnapshotID != "" {
					snap = r.CurrentSnapshotID
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.URI, r.SourceConfig.Kind, r.Policy.Strategy, snap)
			}
			return w.Flush()
		},
	}
}
