package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <uri>",
		Short: "Resolve a context resource and print its content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := args[0]
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()

			result, err := a.Resolver.Resolve(context.Background(), uri)
			if err != nil {
				return err
			}

			fmt.Printf("uri:          %s\n", uri)
			fmt.Printf("snapshot:     %s\n", result.Snapshot.SnapshotID)
			fmt.Printf("content_hash: %s\n", result.Snapshot.ContentHash)
			fmt.Printf("freshness:    %s\n", freshnessLabel(result))
			fmt.Printf("provenance:   %s\n", formatProvenance(result.Snapshot.Provenance))
			fmt.Printf("bytes:        %d\n", result.Snapshot.Bytes)
			fmt.Printf("content_type: %s\n", result.Snapshot.ContentType)
			fmt.Println()
			fmt.Println("--- content ---")
			fmt.Println(string(result.Content))
			return nil
		},
	}
}
