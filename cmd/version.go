package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/obegron/testtender/internal/config"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display testtender version details",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("-------------------------------------------------\n")
		fmt.Printf("testtender\n")
		fmt.Printf("-------------------------------------------------\n")
		fmt.Printf("version: %s\n", config.Version)
		fmt.Printf("date:    %s\n", config.Date)
		fmt.Printf("build:   %s\n", config.Build)
		fmt.Printf("-------------------------------------------------\n")
	},
}
