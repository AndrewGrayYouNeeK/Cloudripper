package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "cli",
		Short: "A simple CLI application",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Hello from Cloudripper CLI!")
		},
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
	}
}