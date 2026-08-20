package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <uri>[@<tag>]",
		Short: "Resolve a context resource and print its content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := args[0]
			c, err := openClient()
			if err != nil {
				return err
			}
			defer c.Close()

			result, err := c.Resolve(context.Background(), uri)
			if err != nil {
				return err
			}

			fmt.Printf("uri:          %s\n", uri)
			if result.Ref != "" {
				fmt.Printf("ref:          %s\n", result.Ref)
			}
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
