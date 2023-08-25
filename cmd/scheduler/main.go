/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package main

import (
	"k8s.io/component-base/cli"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
	"kse/kse-rescheduler/pkg/podrescheduling"
	"os"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(podrescheduling.Name, podrescheduling.New),
	)

	code := cli.Run(command)
	os.Exit(code)
}
