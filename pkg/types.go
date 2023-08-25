/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package pkg

import "time"
//annotations name
const (
	SchedulingRetrieString        = "scheduling-retries"
	StsPodMapString               = "kse.com/sts-pods-map"
	PurePodInfoString             = "kse.com/pod"
	DeployInfoString              = "kse.com/deploy"
	RsInfoString                  = "kse.com/rs"
	CjInfoString                  = "kse.com/cj"
	JobInfoString                 = "kse.com/job"
	SchedulinedHostString         = "kse.com/scheduled-hosts"
	CurrentReschedulingTimeString = "kse.com/current-retries-times"
	NAMESPACE                     = "kube-system"
	RenewDeadlineDuration         = 10 * time.Second
	LeaseDuration                 = 15 * time.Second
	RetryPeriod                   = 2 * time.Second
)

// if a pod's createTime max than OutOfTimeToRescheduling, we just need to delete it, we don't have to rescheduling this pod
// because of the k8s cluster environment may be changed
const OutOfTimeToRescheduling  = "30m"

type Patches []Patch

type Patch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type PodOwnerInfo struct {
	PodOwnerName string
	PodOwnerType string
}

type PurePodInfo struct {
	CurrentReschedulingTimes int `json:"currentReschedulingTimes, omitempty"`
	PodScheduledHosts []string `json:"podScheduledHosts, omitempty"`
}

type DeployInfo struct {
	CurrentReschedulingTimes int `json:"currentReschedulingTimes, omitempty"`
	DeployScheduledHosts []string `json:"deployScheduledHosts, omitempty"`
}

type RsInfo struct {
	CurrentReschedulingTimes int `json:"currentReschedulingTimes, omitempty"`
	RsScheduledHosts []string `json:"rsScheduledHosts, omitempty"`
}

type CjInfo struct {
	CurrentReschedulingTimes int `json:"currentReschedulingTimes, omitempty"`
	CjScheduledHosts []string `json:"cjScheduledHosts, omitempty"`
}

type JobInfo struct {
	CurrentReschedulingTimes int `json:"currentReschedulingTimes, omitempty"`
	JobScheduledHosts []string `json:"jobScheduledHosts, omitempty"`
}

type StsPodsMap map[string]PurePodInfo
