/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package podrescheduling

import (
	"context"
	"encoding/json"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"kse/kse-rescheduler/pkg"
)

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name                          = "Podrescheduling"
)

type ScheduledHosts []string

var _ framework.PreFilterPlugin = &Podrescheduling{}

type Podrescheduling struct {
	frameworkHandler framework.Handle
}


func (pr *Podrescheduling) Name() string {
	return Name
}

func New(obj runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	plugin := &Podrescheduling{frameworkHandler: handle}
	return plugin, nil
}

func (pr *Podrescheduling) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

func (pr *Podrescheduling) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	if _, ok := pod.Annotations[pkg.SchedulinedHostString]; ok {
		var scheduledHosts ScheduledHosts
		if err := json.Unmarshal([]byte(pod.Annotations[pkg.SchedulinedHostString]), &scheduledHosts); err != nil {
			klog.ErrorS(err, "PreFilter failed", "pod", klog.KObj(pod))
			return nil, nil
		}
		nodeInfos, err := pr.frameworkHandler.SnapshotSharedLister().NodeInfos().List()
		if err != nil {
			return nil, framework.AsStatus(err)
		}
		var resultNodes []string
		for _, nodeInfo := range nodeInfos {
			found := false
			for _, scheduledHost := range scheduledHosts {
				if nodeInfo.Node().Name == scheduledHost {
					found = true
					break
				}
			}
			if !found {
				resultNodes = append(resultNodes, nodeInfo.Node().Name)
			}
		}
		if len(resultNodes) == 0 {
			return nil, nil
		}
		nodeNames := sets.NewString(resultNodes...)
		return &framework.PreFilterResult{NodeNames: nodeNames}, nil
	}
	return nil, nil
}