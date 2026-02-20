package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "belowdeck",
	Short: "Stream Deck Plus daemon",
	Long:  "A modular Stream Deck Plus application combining media controls, calendar, home automation, weather, and more.",
	RunE:  runDaemon,
}

func init() {
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(statusCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
