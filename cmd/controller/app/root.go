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
	"os"
	"strings"
)

var kubeConfigFile = ""

var rootCmd = &cobra.Command{
	Use:    "kse-rescheduler",
	Short:  "Rescheduling terminated or crashloopbackoff pods",
	Long:   `Rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined in annotations. 

Some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
the scheduling-retries defined in annotations.`,
    Run:     runCommand,
    SilenceUsage: true,
	SilenceErrors: true,
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func runCommand(cmd *cobra.Command, args []string) {
	cmd.Help()
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		e := err.Error()
		fmt.Println(strings.ToUpper(e[:1]) + e[1:])
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize()
	rootCmd.PersistentFlags().StringVar(&kubeConfigFile, "kube-config", kubeConfigFile, "Path to kubeconfig file")
}


