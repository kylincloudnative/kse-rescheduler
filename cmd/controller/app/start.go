/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package app

import (
	"github.com/spf13/cobra"
	"kse/kse-rescheduler/pkg/admission"
)

var kseRescheduler = admission.NewKseReschedulerServer()

var kseReschedulerCmd = &cobra.Command{
	Use:                        "start",
	Short:  "Starts kse rescheduler server",
	Long: `Starts kse-rescheduler's Kubernetes mutating admission controller webhook server and listFunc.
The webhook will listen to requests from Kubernetes and reply
with list of json patches for kubernetes to perform in order
to have the scheduling-retries defined in annotations injected to the pods.

The listFunc will periodically list terminated and crashloopback pods, then delete them.

TLS certificate and private key is required to receive requests
from kubernetes controllers. The certificate should have SAN
and DNS that reflects the webhooks service FQDN, e.g:
webhook.kserescheduler.svc.
.`,
   Run: func(cmd *cobra.Command, args []string) {
	   //klog.V(3).Info("It is debug message")
	   cobra.CheckErr(kseRescheduler.Start(kubeConfigFile))
   },
}

func init()  {
	rootCmd.AddCommand(kseReschedulerCmd)
	kseReschedulerCmd.Flags().StringVar(&kseRescheduler.TLSCertFile, "tls-crt", kseRescheduler.TLSCertFile, "TLS Certificate file")
	kseReschedulerCmd.Flags().StringVar(&kseRescheduler.TLSKeyFile, "tls-key", kseRescheduler.TLSKeyFile, "TLS Key file")
	kseReschedulerCmd.Flags().StringVar(&kseRescheduler.Address, "addr", kseRescheduler.Address, "Webhook bind address")
	kseReschedulerCmd.Flags().DurationVar(&kseRescheduler.ListFuncPeriod, "list-func-period", kseRescheduler.ListFuncPeriod, "kse-rescheduler's execution period to reschedule terminated or crashloopback pods")
	//klog.InitFlags(flag.CommandLine)
	//webhookCmd.Flags().AddGoFlagSet(flag.CommandLine)
}
