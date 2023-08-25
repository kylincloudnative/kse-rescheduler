/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package app

import (
	"fmt"
	"github.com/spf13/cobra"
	"kse/kse-rescheduler/pkg/version"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:     "version",
	Aliases: []string{"ver"},
	Short:   "Print the application version",
	Long:    `Print the application version`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.DisplayVersion())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
