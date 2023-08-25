/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package listfunc

import (
	"context"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"kse/kse-rescheduler/pkg"
	"time"
)

type ListFunc struct {
	K8sClientSet                kubernetes.Interface
}

func NewListFunc() ListFunc {
	return ListFunc{}
}

func (lf *ListFunc) List() {
	podList, err := lf.K8sClientSet.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("list pods in all namespaces err: %s\n", err.Error())
		return
	}
	allNameSpacePods := podList.Items

	//get the abnormalPods in the k8s cluster
	var abnormalPods []corev1.Pod
	for _, pod := range allNameSpacePods {
		for _, condition := range pod.Status.Conditions {
			if condition.Status == "False" {
				abnormalPods = append(abnormalPods, pod)
				break
			}
		}
	}

	// beginning to rescheduling the abnormalPods
	for _, pod := range abnormalPods {
		// Avoid pod has been deleted at this list pods period
		_, err := lf.K8sClientSet.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("get pod: %s err: %s\n", pod.Name, err.Error())
			continue
		}
		if len(pod.OwnerReferences) >0 {
			podOwnerInfo, err := lf.GetPodOwnerInfo(&pod)
			if err != nil {
				klog.Error(err.Error())
				continue
			}
			switch podOwnerInfo.PodOwnerType {
			case "Deployment":
				if err := lf.doDeploys(&pod, *podOwnerInfo); err != nil {
					klog.Error(err.Error())
					continue
				}
			case "ReplicaSet":
				if err := lf.doRs(&pod, *podOwnerInfo); err != nil {
					klog.Error(err.Error())
					continue
				}
			case "CronJob":
				if !podCompleted(&pod) {
					if err := lf.doCjs(&pod, *podOwnerInfo); err != nil {
						klog.Error(err.Error())
						continue
					}
				}
			case "Job":
				if !podCompleted(&pod) {
					if err := lf.doJobs(&pod, *podOwnerInfo); err != nil {
						klog.Error(err.Error())
						continue
					}
				}
			case "DaemonSet":
				if err := lf.doDs(&pod, *podOwnerInfo); err != nil {
					klog.Error(err.Error())
					continue
				}
			case "StatefulSet":
				if err := lf.doSts(&pod, *podOwnerInfo); err != nil {
					klog.Error(err.Error())
					continue
				}
			}

		} else {
			//pure pod
			if err := lf.doPods(&pod); err != nil {
				klog.Error(err.Error())
				continue
			}
		}
	}
}

func (lf *ListFunc) GetPodOwnerInfo(pod *corev1.Pod) (*pkg.PodOwnerInfo, error) {
	var podOwnerInfo pkg.PodOwnerInfo
	if pod.OwnerReferences[0].Kind == "ReplicaSet" {
		rsName := pod.OwnerReferences[0].Name
		rs, err := lf.K8sClientSet.AppsV1().ReplicaSets(pod.Namespace).Get(context.TODO(), rsName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get pod %s owner ReplicaSets err: %s\n", pod.Name, err.Error())
		}
		if len(rs.OwnerReferences) > 0 && rs.OwnerReferences[0].Kind == "Deployment" {
			podOwnerInfo.PodOwnerName = rs.OwnerReferences[0].Name
			podOwnerInfo.PodOwnerType = "Deployment"
		} else {
			podOwnerInfo.PodOwnerName = rsName
			podOwnerInfo.PodOwnerType = "ReplicaSet"
		}
	}

	if pod.OwnerReferences[0].Kind == "StatefulSet" {
		podOwnerInfo.PodOwnerName = pod.OwnerReferences[0].Name
		podOwnerInfo.PodOwnerType = "StatefulSet"
	}

	if pod.OwnerReferences[0].Kind == "DaemonSet" {
		podOwnerInfo.PodOwnerName = pod.OwnerReferences[0].Name
		podOwnerInfo.PodOwnerType = "DaemonSet"
	}
	if pod.OwnerReferences[0].Kind == "Job" {
		jbName := pod.OwnerReferences[0].Name
		jb, err := lf.K8sClientSet.BatchV1().Jobs(pod.Namespace).Get(context.TODO(), jbName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get pod %s onwer Job err: %s\n", pod.Name, err.Error())
		}
		if len(jb.OwnerReferences) > 0 && jb.OwnerReferences[0].Kind == "CronJob" {
			podOwnerInfo.PodOwnerName = jb.OwnerReferences[0].Name
			podOwnerInfo.PodOwnerType = "CronJob"
		} else {
			podOwnerInfo.PodOwnerName = jbName
			podOwnerInfo.PodOwnerType = "Job"
		}
	}

	return &podOwnerInfo, nil
}

func podHasScheduled(pod *corev1.Pod) bool {
	var podHasScheduled bool
	podHasScheduled = false
	// avoid delete pod while pod is pulling images
	if pod.Status.Phase == "Pending" {
		return false
	}
	// avoid delete pod while pod is in readiness probing, and the pod may run normally after readiness probing
	if len(pod.Status.ContainerStatuses) >0 {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Running != nil {
				return false
			}
		}
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == "PodScheduled" && condition.Status == "True" {
			podHasScheduled = true
		}
	}
	return podHasScheduled
}

func podUnschedulable(pod *corev1.Pod) bool {
	var podUnschedulable bool
	podUnschedulable = false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == "PodScheduled" && condition.Status == "False" {
			if _, ok := pod.Annotations[pkg.SchedulinedHostString]; ok {
				podUnschedulable = true
			}
		}
	}
	return podUnschedulable
}

func podCompleted(pod *corev1.Pod) bool {
	// avoid pod unschedulable and it's phase is Pending
	if pod.Status.Phase == "Pending" {
		return false
	}
	podCompleted := true
	for _, condition := range pod.Status.Conditions {
		if condition.Type == "Ready" && condition.Reason != "PodCompleted" {
			podCompleted = false
		}
		if condition.Type == "ContainersReady" && condition.Reason != "PodCompleted" {
			podCompleted = false
		}
	}
	return podCompleted
}

func (lf *ListFunc) delPod(pod *corev1.Pod) error{
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		backgroundDeletion := metav1.DeletePropagationBackground
		gracePeriod := int64(0)
		if err := lf.K8sClientSet.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod, PropagationPolicy: &backgroundDeletion}); err != nil {
			return fmt.Errorf("delete pod %s err: %s", pod.Name, err.Error())
		}
		return nil
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) delCj(cj *batchv1.CronJob) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		gracePeriod := int64(0)
		backgroundDeletion := metav1.DeletePropagationBackground
		if err := lf.K8sClientSet.BatchV1().CronJobs(cj.Namespace).Delete(context.TODO(), cj.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod, PropagationPolicy: &backgroundDeletion}); err != nil {
			return fmt.Errorf("delete cronjob %s err: %s", cj.Name, err.Error())
		}
		return nil
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) delJob(job *batchv1.Job) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		gracePeriod := int64(0)
		backgroundDeletion := metav1.DeletePropagationBackground
		if err := lf.K8sClientSet.BatchV1().Jobs(job.Namespace).Delete(context.TODO(), job.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod, PropagationPolicy: &backgroundDeletion}); err != nil {
			return fmt.Errorf("delete job %s err: %s", job.Name, err.Error())
		}
		return nil
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) doDeploys(pod *corev1.Pod, podOwnerInfo pkg.PodOwnerInfo) error {
	deploy, err := lf.K8sClientSet.AppsV1().Deployments(pod.Namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod %s owner Deployment err: %s\n", pod.Name, err.Error())
	}
	if _, ok := deploy.Annotations[pkg.SchedulingRetrieString]; ok {
		var schedulingRetries int
		var deployInfo pkg.DeployInfo
		var deployScheduledHosts []string
		if err := json.Unmarshal([]byte(deploy.Annotations[pkg.SchedulingRetrieString]), &schedulingRetries); err != nil {
			return fmt.Errorf("unmarshal %s deployment %s scheduling-retries err: %s\n", pod.Name, deploy.Name, err.Error())
		}
		totalSchedulingRetries := schedulingRetries * int(*deploy.Spec.Replicas)
		nowTime := time.Now()
		timeDura, _ := time.ParseDuration(pkg.OutOfTimeToRescheduling)
		if nowTime.Before(pod.CreationTimestamp.Add(timeDura)) {
			if _, ok := deploy.Annotations[pkg.DeployInfoString]; ok {
				if err := json.Unmarshal([]byte(deploy.Annotations[pkg.DeployInfoString]), &deployInfo); err != nil {
					return fmt.Errorf("unmarshal deployment %s kse.com/deploy err: %s\n", deploy.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if deployInfo.CurrentReschedulingTimes >= 1 && deployInfo.CurrentReschedulingTimes <= totalSchedulingRetries {
						deployInfo := pkg.DeployInfo{
							CurrentReschedulingTimes: deployInfo.CurrentReschedulingTimes + 1,
							DeployScheduledHosts: append(deployInfo.DeployScheduledHosts, pod.Spec.NodeName)}
						if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
							return err
						}
						if err := lf.delPod(pod); err != nil {
							return err
						}
					}
					if deployInfo.CurrentReschedulingTimes > totalSchedulingRetries {
						deployInfo := pkg.DeployInfo{
							CurrentReschedulingTimes: deployInfo.CurrentReschedulingTimes,
							DeployScheduledHosts: nil}
						if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					deployInfo := pkg.DeployInfo{
						CurrentReschedulingTimes: deployInfo.CurrentReschedulingTimes,
						DeployScheduledHosts: nil}
					if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}

			} else {
				// first time rescheduling pods, so the deploy.Annotations[pkg.DeployInfoString] is empty, add it
				if podHasScheduled(pod) {
					deployScheduledHosts = append(deployScheduledHosts, pod.Spec.NodeName)
					deployInfo := pkg.DeployInfo{CurrentReschedulingTimes: 1, DeployScheduledHosts: deployScheduledHosts}
					if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			}
			//if a pod's createTime max than OutOfTimeToRescheduling，just need to delete it, and don't have to
			// keep scheduled-hosts, we don't need kube-scheduler to interfere the scheduling in the priFilter phase
		} else {
			if _, ok := deploy.Annotations[pkg.DeployInfoString]; ok {
				if err := json.Unmarshal([]byte(deploy.Annotations[pkg.DeployInfoString]), &deployInfo); err != nil {
					return fmt.Errorf("unmarshal deployment %s kse.com/deploy err: %s\n", deploy.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if deployInfo.CurrentReschedulingTimes >= 1 && deployInfo.CurrentReschedulingTimes <= totalSchedulingRetries {
						deployInfo := pkg.DeployInfo{
							CurrentReschedulingTimes: deployInfo.CurrentReschedulingTimes + 1,
							DeployScheduledHosts: nil}
						if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
							return err
						}
						if err := lf.delPod(pod); err != nil {
							return err
						}
					}
					if deployInfo.CurrentReschedulingTimes > totalSchedulingRetries {
						deployInfo := pkg.DeployInfo{
							CurrentReschedulingTimes: deployInfo.CurrentReschedulingTimes,
							DeployScheduledHosts: nil}
						if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					deployInfo := pkg.DeployInfo{
						CurrentReschedulingTimes: deployInfo.CurrentReschedulingTimes,
						DeployScheduledHosts: nil}
					if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			} else {
				// first time rescheduling pods, so the deploy.Annotations[pkg.DeployInfoString] is empty, add it, and set
				// DeployScheduledHosts nil
				if podHasScheduled(pod) {
					deployInfo := pkg.DeployInfo{CurrentReschedulingTimes: 1, DeployScheduledHosts: nil}
					if err := lf.updateDeploy(deploy, &deployInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (lf *ListFunc) doRs(pod *corev1.Pod, podOwnerInfo pkg.PodOwnerInfo) error {
	rs, err := lf.K8sClientSet.AppsV1().ReplicaSets(pod.Namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod %s owner ReplicaSets err: %s\n", pod.Name, err.Error())
	}
	if _, ok := rs.Annotations[pkg.SchedulingRetrieString]; ok {
		var schedulingRetries int
		var rsInfo pkg.RsInfo
		var rsScheduledHosts []string
		if err := json.Unmarshal([]byte(rs.Annotations[pkg.SchedulingRetrieString]), &schedulingRetries); err != nil {
			return fmt.Errorf("unmarshal %s ReplicaSets %s scheduling-retries err: %s\n", pod.Name, rs.Name, err.Error())
		}
		totalSchedulingRetries := schedulingRetries * int(*rs.Spec.Replicas)
		nowTime := time.Now()
		timeDura, _ := time.ParseDuration(pkg.OutOfTimeToRescheduling)
		if nowTime.Before(pod.CreationTimestamp.Add(timeDura)) {
			if _, ok := rs.Annotations[pkg.RsInfoString]; ok {
				if err := json.Unmarshal([]byte(rs.Annotations[pkg.RsInfoString]), &rsInfo); err != nil {
					return fmt.Errorf("unmarshal deployment %s kse.com/rs err: %s\n", rs.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if rsInfo.CurrentReschedulingTimes >= 1 && rsInfo.CurrentReschedulingTimes <= totalSchedulingRetries {
						rsInfo := pkg.RsInfo{
							CurrentReschedulingTimes: rsInfo.CurrentReschedulingTimes + 1,
							RsScheduledHosts: append(rsInfo.RsScheduledHosts, pod.Spec.NodeName)}
						if err := lf.updateRs(rs, &rsInfo); err != nil {
							return err
						}
						if err := lf.delPod(pod); err != nil {
							return err
						}
					}
					if rsInfo.CurrentReschedulingTimes > totalSchedulingRetries {
						rsInfo := pkg.RsInfo{
							CurrentReschedulingTimes: rsInfo.CurrentReschedulingTimes,
							RsScheduledHosts: nil}
						if err := lf.updateRs(rs, &rsInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					rsInfo := pkg.RsInfo{
						CurrentReschedulingTimes: rsInfo.CurrentReschedulingTimes,
						RsScheduledHosts: nil}
					if err := lf.updateRs(rs, &rsInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}

			} else {
				// first time rescheduling pods, so the rs.Annotations[pkg.RsInfoString] is empty, add it
				if podHasScheduled(pod) {
					rsScheduledHosts = append(rsScheduledHosts, pod.Spec.NodeName)
					rsInfo := pkg.RsInfo{CurrentReschedulingTimes: 1, RsScheduledHosts: rsScheduledHosts}
					if err := lf.updateRs(rs, &rsInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			}
			//if a pod's createTime max than OutOfTimeToRescheduling，just need to delete it, and don't have to
			// keep scheduled-hosts, we don't need kube-scheduler to interfere the scheduling in the priFilter phase
		} else {
			if _, ok := rs.Annotations[pkg.RsInfoString]; ok {
				if err := json.Unmarshal([]byte(rs.Annotations[pkg.RsInfoString]), &rsInfo); err != nil {
					return fmt.Errorf("unmarshal replicasets %s kse.com/rs err: %s\n", rs.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if rsInfo.CurrentReschedulingTimes >= 1 && rsInfo.CurrentReschedulingTimes <= totalSchedulingRetries {
						rsInfo := pkg.RsInfo{
							CurrentReschedulingTimes: rsInfo.CurrentReschedulingTimes + 1,
							RsScheduledHosts: nil}
						if err := lf.updateRs(rs, &rsInfo); err != nil {
							return err
						}
						if err := lf.delPod(pod); err != nil {
							return err
						}
					}
					if rsInfo.CurrentReschedulingTimes > totalSchedulingRetries {
						rsInfo := pkg.RsInfo{
							CurrentReschedulingTimes: rsInfo.CurrentReschedulingTimes,
							RsScheduledHosts: nil}
						if err := lf.updateRs(rs, &rsInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					rsInfo := pkg.RsInfo{
						CurrentReschedulingTimes: rsInfo.CurrentReschedulingTimes,
						RsScheduledHosts: nil}
					if err := lf.updateRs(rs, &rsInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			} else {
				// first time rescheduling pods, so the rs.Annotations[pkg.RsInfoString] is empty, add it, and set
				// RsScheduledHosts nil
				if podHasScheduled(pod) {
					rsInfo := pkg.RsInfo{CurrentReschedulingTimes: 1, RsScheduledHosts: nil}
					if err := lf.updateRs(rs, &rsInfo); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (lf *ListFunc) doCjs(pod *corev1.Pod, podOwnerInfo pkg.PodOwnerInfo) error {
	cj, err := lf.K8sClientSet.BatchV1().CronJobs(pod.Namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod %s owner CronJob err: %s\n", pod.Name, err.Error())
	}
	if _, ok := cj.Annotations[pkg.SchedulingRetrieString]; ok {
		var schedulingRetries int
		var cjInfo pkg.CjInfo
		var cjScheduledHosts []string
		if err := json.Unmarshal([]byte(cj.Annotations[pkg.SchedulingRetrieString]), &schedulingRetries); err != nil {
			return fmt.Errorf("unmarshal %s cronjob %s scheduling-retries err: %s\n", pod.Name, cj.Name, err.Error())
		}
		nowTime := time.Now()
		timeDura, _ := time.ParseDuration(pkg.OutOfTimeToRescheduling)
		if nowTime.Before(pod.CreationTimestamp.Add(timeDura)) {
			if _, ok := cj.Annotations[pkg.CjInfoString]; ok {
				if err := json.Unmarshal([]byte(cj.Annotations[pkg.CjInfoString]), &cjInfo); err != nil {
					return fmt.Errorf("unmarshal cronjob %s kse.com/cj err: %s\n", cj.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if cjInfo.CurrentReschedulingTimes >= 1 && cjInfo.CurrentReschedulingTimes <= schedulingRetries {
						cjInfo := pkg.CjInfo{
							CurrentReschedulingTimes: cjInfo.CurrentReschedulingTimes + 1,
							CjScheduledHosts: append(cjInfo.CjScheduledHosts, pod.Spec.NodeName)}
						// we just need to delete cronjob, and it's pods will be deleted
						if err := lf.delCj(cj); err != nil {
							return err
						}
						if err := lf.createCj(cj, &cjInfo); err != nil {
							return err
						}
					}
					if cjInfo.CurrentReschedulingTimes > schedulingRetries {
						cjInfo := pkg.CjInfo{
							CurrentReschedulingTimes: cjInfo.CurrentReschedulingTimes,
							CjScheduledHosts: nil}
						if err := lf.updateCj(cj, &cjInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					cjInfo := pkg.CjInfo{
						CurrentReschedulingTimes: cjInfo.CurrentReschedulingTimes,
						CjScheduledHosts: nil}
					// we just need to delete cronjob, and it's pods will be deleted
					if err := lf.delCj(cj); err != nil {
						return err
					}
					if err := lf.createCj(cj, &cjInfo); err != nil {
						return err
					}
				}

			} else {
				// first time rescheduling pods, so the cj.Annotations[pkg.CjInfoString] is empty, add it
				if podHasScheduled(pod) {
					cjScheduledHosts = append(cjScheduledHosts, pod.Spec.NodeName)
					cjInfo := pkg.CjInfo{CurrentReschedulingTimes: 1, CjScheduledHosts: cjScheduledHosts}
					// we just need to delete cronjob, and it's pods will be deleted
					if err := lf.delCj(cj); err != nil {
						return err
					}
					if err := lf.createCj(cj, &cjInfo); err != nil {
						return err
					}
				}
			}
			//if a pod's createTime max than OutOfTimeToRescheduling，just need to delete it, and don't have to
			// keep scheduled-hosts, we don't need kube-scheduler to interfere the scheduling in the priFilter phase
		} else {
			if _, ok := cj.Annotations[pkg.CjInfoString]; ok {
				if err := json.Unmarshal([]byte(cj.Annotations[pkg.CjInfoString]), &cjInfo); err != nil {
					return fmt.Errorf("unmarshal cronjob %s kse.com/cj err: %s\n", cj.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if cjInfo.CurrentReschedulingTimes >= 1 && cjInfo.CurrentReschedulingTimes <= schedulingRetries {
						cjInfo := pkg.CjInfo{
							CurrentReschedulingTimes: cjInfo.CurrentReschedulingTimes + 1,
							CjScheduledHosts: nil}
						// we just need to delete cronjob, and it's pods will be deleted
						if err := lf.delCj(cj); err != nil {
							return err
						}
						if err := lf.createCj(cj, &cjInfo); err != nil {
							return err
						}
					}
					if cjInfo.CurrentReschedulingTimes > schedulingRetries {
						cjInfo := pkg.CjInfo{
							CurrentReschedulingTimes: cjInfo.CurrentReschedulingTimes,
							CjScheduledHosts: nil}
						if err := lf.updateCj(cj, &cjInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					cjInfo := pkg.CjInfo{
						CurrentReschedulingTimes: cjInfo.CurrentReschedulingTimes,
						CjScheduledHosts: nil}
					// we just need to delete cronjob, and it's pods will be deleted
					if err := lf.delCj(cj); err != nil {
						return err
					}
					if err := lf.createCj(cj, &cjInfo); err != nil {
						return err
					}
				}
			} else {
				// first time rescheduling pods, so the cj.Annotations[pkg.CjInfoString] is empty, add it, and set
				// CjScheduledHosts nil
				if podHasScheduled(pod) {
					cjInfo := pkg.CjInfo{CurrentReschedulingTimes: 1, CjScheduledHosts: nil}
					// we just need to delete cronjob, and it's pods will be deleted
					if err := lf.delCj(cj); err != nil {
						return err
					}
					if err := lf.createCj(cj, &cjInfo); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (lf *ListFunc) doJobs(pod *corev1.Pod, podOwnerInfo pkg.PodOwnerInfo) error {
	jb, err := lf.K8sClientSet.BatchV1().Jobs(pod.Namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod %s owner Job err: %s\n", pod.Name, err.Error())
	}
	if _, ok := jb.Annotations[pkg.SchedulingRetrieString]; ok {
		var schedulingRetries int
		var jbInfo pkg.JobInfo
		var jbScheduledHosts []string
		if err := json.Unmarshal([]byte(jb.Annotations[pkg.SchedulingRetrieString]), &schedulingRetries); err != nil {
			return fmt.Errorf("unmarshal %s job %s scheduling-retries err: %s\n", pod.Name, jb.Name, err.Error())
		}
		nowTime := time.Now()
		timeDura, _ := time.ParseDuration(pkg.OutOfTimeToRescheduling)
		if nowTime.Before(pod.CreationTimestamp.Add(timeDura)) {
			if _, ok := jb.Annotations[pkg.JobInfoString]; ok {
				if err := json.Unmarshal([]byte(jb.Annotations[pkg.JobInfoString]), &jbInfo); err != nil {
					return fmt.Errorf("unmarshal job %s kse.com/job err: %s\n", jb.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if jbInfo.CurrentReschedulingTimes >= 1 && jbInfo.CurrentReschedulingTimes <= schedulingRetries {
						jbInfo := pkg.JobInfo{
							CurrentReschedulingTimes: jbInfo.CurrentReschedulingTimes + 1,
							JobScheduledHosts: append(jbInfo.JobScheduledHosts, pod.Spec.NodeName)}
						// we just need to delete job, and it's pods will be deleted
						if err := lf.delJob(jb); err != nil {
							return err
						}
						if err := lf.createJob(jb, &jbInfo); err != nil {
							return err
						}
					}
					if jbInfo.CurrentReschedulingTimes > schedulingRetries {
						jbInfo := pkg.JobInfo{
							CurrentReschedulingTimes: jbInfo.CurrentReschedulingTimes,
							JobScheduledHosts: nil}
						if err := lf.updateJob(jb, &jbInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					jbInfo := pkg.JobInfo{
						CurrentReschedulingTimes: jbInfo.CurrentReschedulingTimes,
						JobScheduledHosts: nil}
					// we just need to delete job, and it's pods will be deleted
					if err := lf.delJob(jb); err != nil {
						return err
					}
					if err := lf.createJob(jb, &jbInfo); err != nil {
						return err
					}
				}

			} else {
				// first time rescheduling pods, so the jb.Annotations[pkg.JobInfoString] is empty, add it
				if podHasScheduled(pod) {
					jbScheduledHosts = append(jbScheduledHosts, pod.Spec.NodeName)
					jbInfo := pkg.JobInfo{CurrentReschedulingTimes: 1, JobScheduledHosts: jbScheduledHosts}
					// we just need to delete job, and it's pods will be deleted
					if err := lf.delJob(jb); err != nil {
						return err
					}
					if err := lf.createJob(jb, &jbInfo); err != nil {
						return err
					}
				}
			}
			//if a pod's createTime max than OutOfTimeToRescheduling，just need to delete it, and don't have to
			// keep scheduled-hosts, we don't need kube-scheduler to interfere the scheduling in the priFilter phase
		} else {
			if _, ok := jb.Annotations[pkg.JobInfoString]; ok {
				if err := json.Unmarshal([]byte(jb.Annotations[pkg.JobInfoString]), &jbInfo); err != nil {
					return fmt.Errorf("unmarshal job %s kse.com/jb err: %s\n", jb.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if jbInfo.CurrentReschedulingTimes >= 1 && jbInfo.CurrentReschedulingTimes <= schedulingRetries {
						jbInfo := pkg.JobInfo{
							CurrentReschedulingTimes: jbInfo.CurrentReschedulingTimes + 1,
							JobScheduledHosts: nil}
						// we just need to delete job, and it's pods will be deleted
						if err := lf.delJob(jb); err != nil {
							return err
						}
						if err := lf.createJob(jb, &jbInfo); err != nil {
							return err
						}
					}
					if jbInfo.CurrentReschedulingTimes > schedulingRetries {
						jbInfo := pkg.JobInfo{
							CurrentReschedulingTimes: jbInfo.CurrentReschedulingTimes,
							JobScheduledHosts: nil}
						if err := lf.updateJob(jb, &jbInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					jbInfo := pkg.JobInfo{
						CurrentReschedulingTimes: jbInfo.CurrentReschedulingTimes,
						JobScheduledHosts: nil}
					// we just need to delete job, and it's pods will be deleted
					if err := lf.delJob(jb); err != nil {
						return err
					}
					if err := lf.createJob(jb, &jbInfo); err != nil {
						return err
					}
				}
			} else {
				// first time rescheduling pods, so the jb.Annotations[pkg.JobInfoString] is empty, add it, and set
				// JobScheduledHosts nil
				if podHasScheduled(pod) {
					jbInfo := pkg.JobInfo{CurrentReschedulingTimes: 1, JobScheduledHosts: nil}
					// we just need to delete job, and it's pods will be deleted
					if err := lf.delJob(jb); err != nil {
						return err
					}
					if err := lf.createJob(jb, &jbInfo); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (lf *ListFunc) doDs(pod *corev1.Pod, podOwnerInfo pkg.PodOwnerInfo) error {
	ds, err := lf.K8sClientSet.AppsV1().DaemonSets(pod.Namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod %s owner DaemonSet err: %s\n", pod.Name, err.Error())
	}
	if _, ok := ds.Annotations[pkg.SchedulingRetrieString]; ok {
		var schedulingRetries int
		if err := json.Unmarshal([]byte(ds.Annotations[pkg.SchedulingRetrieString]), &schedulingRetries); err != nil {
			return fmt.Errorf("unmarshal %s daemonsets %s scheduling-retries err: %s\n", pod.Name, ds.Name, err.Error())
		}

		var dsCurrentReschedulingTimes int
		if _, ok := ds.Annotations[pkg.CurrentReschedulingTimeString]; ok {
			if err := json.Unmarshal([]byte(ds.Annotations[pkg.CurrentReschedulingTimeString]), &dsCurrentReschedulingTimes); err != nil {
				return fmt.Errorf("unmarshal %s daemonsets %s kse.com/current-retries-times err: %s\n", pod.Name, ds.Name, err.Error())
			}
			if dsCurrentReschedulingTimes >= 1 && dsCurrentReschedulingTimes <= schedulingRetries {
				dsCurrentReschedulingTimes = dsCurrentReschedulingTimes +1
				byteDsCurrentReschedulingTimes, err := json.Marshal(dsCurrentReschedulingTimes)
				if err != nil {
					return fmt.Errorf("marshal %s daemonsets %s kse.com/current-retries-times err: %s\n", pod.Name, ds.Name, err.Error())
				}
				if err := lf.updateDs(ds, string(byteDsCurrentReschedulingTimes)); err != nil {
					return err
				}
				if err := lf.delPod(pod); err != nil {
					return err
				}
			}
		} else {
			dsCurrentReschedulingTimes = 1
			byteDsCurrentReschedulingTimes, err := json.Marshal(dsCurrentReschedulingTimes)
			if err != nil {
				return fmt.Errorf("marshal %s daemonsets %s kse.com/current-retries-times err: %s\n", pod.Name, ds.Name, err.Error())
			}
			if err := lf.updateDs(ds, string(byteDsCurrentReschedulingTimes)); err != nil {
				return err
			}
			if err := lf.delPod(pod); err != nil {
				return err
			}
		}
	}
	return nil
}

func (lf *ListFunc) doSts(pod *corev1.Pod, podOwnerInfo pkg.PodOwnerInfo) error {
	sts, err := lf.K8sClientSet.AppsV1().StatefulSets(pod.Namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod %s owner Statefulset err: %s\n", pod.Name, err.Error())
	}
	if _, ok := sts.Annotations[pkg.SchedulingRetrieString]; ok {
		var schedulingRetries int
		var podScheduledHosts []string
		var currentReschedulingTime int
		var stsPodsMap pkg.StsPodsMap
		if err := json.Unmarshal([]byte(sts.Annotations[pkg.SchedulingRetrieString]), &schedulingRetries); err != nil {
			return fmt.Errorf("unmarshal %s statefulset %s scheduling-retries err: %s\n", pod.Name, sts.Name, err.Error())
		}
		nowTime := time.Now()
		timeDura, _ := time.ParseDuration(pkg.OutOfTimeToRescheduling)
		if nowTime.Before(pod.CreationTimestamp.Add(timeDura)) {
			if _, ok := sts.Annotations[pkg.StsPodMapString]; ok {
				if err := json.Unmarshal([]byte(sts.Annotations[pkg.StsPodMapString]), &stsPodsMap); err != nil {
					return fmt.Errorf("unmarshal %s statefulset %s kse.com/sts-pods-map err: %s\n", pod.Name, sts.Name, err.Error())
				}
				if podHasScheduled(pod) {
					if _, ok := stsPodsMap[pod.Name]; ok {
						if stsPodsMap[pod.Name].CurrentReschedulingTimes >= 1 && stsPodsMap[pod.Name].CurrentReschedulingTimes <= schedulingRetries {
							currentReschedulingTime = stsPodsMap[pod.Name].CurrentReschedulingTimes + 1
							stsPodInfo := pkg.PurePodInfo{
								CurrentReschedulingTimes: currentReschedulingTime,
								PodScheduledHosts: append(stsPodsMap[pod.Name].PodScheduledHosts, pod.Spec.NodeName)}
							stsPodsMap[pod.Name] = stsPodInfo
							if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
								return err
							}
							if err := lf.delPod(pod); err != nil {
								return err
							}
						}
						if stsPodsMap[pod.Name].CurrentReschedulingTimes > schedulingRetries {
							stsPodInfo := pkg.PurePodInfo{
								CurrentReschedulingTimes: stsPodsMap[pod.Name].CurrentReschedulingTimes,
								PodScheduledHosts: nil}
							stsPodsMap[pod.Name] = stsPodInfo
							if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
								return err
							}
						}
					} else {
						// if stsPodsMap[pod.Name] is empty, this pod is the first time rescheduling, so we need to add it
						podScheduledHosts = append(podScheduledHosts, pod.Spec.NodeName)
						podInfo := pkg.PurePodInfo{CurrentReschedulingTimes: 1, PodScheduledHosts: podScheduledHosts}
						stsPodsMap[pod.Name] = podInfo
						if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
							return err
						}
						if err := lf.delPod(pod); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					if _, ok := stsPodsMap[pod.Name]; !ok {
						return fmt.Errorf("this pod %s dont't add in the statefulset's StsPodMap\n", pod.Name)
					}
					stsPodInfo := pkg.PurePodInfo{
						CurrentReschedulingTimes: stsPodsMap[pod.Name].CurrentReschedulingTimes,
						PodScheduledHosts: nil}
					stsPodsMap[pod.Name] = stsPodInfo
					if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			} else {
				// first time rescheduling pods, so the sts.Annotations[pkg.StsPodMapString] is empty, add it
				if podHasScheduled(pod) {
					podScheduledHosts = append(podScheduledHosts, pod.Spec.NodeName)
					stsPodsMap = map[string]pkg.PurePodInfo{pod.Name: {1, podScheduledHosts}}
					if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			}
		} else {
			//if a pod's createTime max than OutOfTimeToRescheduling，just need to delete it, and don't have to
			// keep podName -> PurePodInfo, we don't need kube-scheduler to interfere the scheduling in the priFilter phase
			if _, ok := sts.Annotations[pkg.StsPodMapString]; ok {
				if err := json.Unmarshal([]byte(sts.Annotations[pkg.StsPodMapString]), &stsPodsMap); err != nil {
					return fmt.Errorf("unmarshal %s statefulset %s kse.com/sts-pods-map err: %s\n", pod.Name, sts.Name, err.Error())
				}
				if podHasScheduled(pod) {
					if _, ok := stsPodsMap[pod.Name]; ok {
						if stsPodsMap[pod.Name].CurrentReschedulingTimes >= 1 && stsPodsMap[pod.Name].CurrentReschedulingTimes <= schedulingRetries {
							currentReschedulingTime = stsPodsMap[pod.Name].CurrentReschedulingTimes + 1
							stsPodInfo := pkg.PurePodInfo{
								CurrentReschedulingTimes: currentReschedulingTime,
								PodScheduledHosts: nil}
							stsPodsMap[pod.Name] = stsPodInfo
							if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
								return err
							}
							if err := lf.delPod(pod); err != nil {
								return err
							}
						}
						if stsPodsMap[pod.Name].CurrentReschedulingTimes > schedulingRetries {
							stsPodInfo := pkg.PurePodInfo{
								CurrentReschedulingTimes: stsPodsMap[pod.Name].CurrentReschedulingTimes,
								PodScheduledHosts: nil}
							stsPodsMap[pod.Name] = stsPodInfo
							if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
								return err
							}
						}
					} else {
						// if stsPodsMap[pod.Name] is empty, this pod is the first time rescheduling, so we need to add it
						// and set PodScheduledHosts nil
						podInfo := pkg.PurePodInfo{CurrentReschedulingTimes: 1, PodScheduledHosts: nil}
						stsPodsMap[pod.Name] = podInfo
						if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
							return err
						}
						if err := lf.delPod(pod); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					if _, ok := stsPodsMap[pod.Name]; !ok {
						return fmt.Errorf("this pod %s dont't add in the statefulset's StsPodMap\n", pod.Name)
					}
					stsPodInfo := pkg.PurePodInfo{
						CurrentReschedulingTimes: stsPodsMap[pod.Name].CurrentReschedulingTimes,
						PodScheduledHosts: nil}
					stsPodsMap[pod.Name] = stsPodInfo
					if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			} else {
				// first time rescheduling pods, so the sts.Annotations[pkg.StsPodMapString] is empty, add it, and set
				// podScheduledHosts nil
				if podHasScheduled(pod) {
					stsPodsMap = map[string]pkg.PurePodInfo{pod.Name: {1, nil}}
					if err := lf.updateSts(sts, pod, &stsPodsMap); err != nil {
						return err
					}
					if err := lf.delPod(pod); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (lf *ListFunc) doPods(pod *corev1.Pod) error {
	if _, ok := pod.Annotations[pkg.SchedulingRetrieString]; ok {
		var schedulingRetries int
		var purePodInfo pkg.PurePodInfo
		var podScheduledHosts []string
		if err := json.Unmarshal([]byte(pod.Annotations[pkg.SchedulingRetrieString]), &schedulingRetries); err != nil {
			return fmt.Errorf("unmarshal pod %s scheduling-retries err: %s\n", pod.Name, err.Error())
		}

		nowTime := time.Now()
		timeDura, _ := time.ParseDuration(pkg.OutOfTimeToRescheduling)
		if nowTime.Before(pod.CreationTimestamp.Add(timeDura)) {
			if _, ok := pod.Annotations[pkg.PurePodInfoString]; ok {
				if err := json.Unmarshal([]byte(pod.Annotations[pkg.PurePodInfoString]), &purePodInfo); err != nil {
					return fmt.Errorf("unmarshal pod %s kse.com/pure-pods err: %s\n", pod.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if purePodInfo.CurrentReschedulingTimes >= 1 && purePodInfo.CurrentReschedulingTimes <= schedulingRetries {
						if err := lf.delPod(pod); err != nil {
							return err
						}
						purePodInfo := pkg.PurePodInfo{
							CurrentReschedulingTimes: purePodInfo.CurrentReschedulingTimes + 1,
							PodScheduledHosts: append(purePodInfo.PodScheduledHosts, pod.Spec.NodeName)}
						if err := lf.createPod(pod, &purePodInfo); err != nil {
							return err
						}
					}
					if purePodInfo.CurrentReschedulingTimes > schedulingRetries {
						purePodInfo := pkg.PurePodInfo{
							CurrentReschedulingTimes: purePodInfo.CurrentReschedulingTimes,
							PodScheduledHosts: nil}
						if err := lf.updatePod(pod, &purePodInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					if err := lf.delPod(pod); err != nil {
						return err
					}
					purePodInfo := pkg.PurePodInfo{
						CurrentReschedulingTimes: purePodInfo.CurrentReschedulingTimes,
						PodScheduledHosts: nil}
					if err := lf.createPod(pod, &purePodInfo); err != nil {
						return err
					}
				}

			} else {
				// first time rescheduling pods, so the pod.Annotations[pkg.PurePodInfoString] is empty, add it
				if podHasScheduled(pod) {
					if err := lf.delPod(pod); err != nil {
						return err
					}
					podScheduledHosts = append(podScheduledHosts, pod.Spec.NodeName)
					purePodInfo := pkg.PurePodInfo{CurrentReschedulingTimes: 1, PodScheduledHosts: podScheduledHosts}
					if err := lf.createPod(pod, &purePodInfo); err != nil {
						return err
					}
				}
			}
			//if a pod's createTime max than OutOfTimeToRescheduling，just need to delete it, and don't have to
			// keep scheduled-hosts, we don't need kube-scheduler to interfere the scheduling in the priFilter phase
		} else {
			if _, ok := pod.Annotations[pkg.PurePodInfoString]; ok {
				if err := json.Unmarshal([]byte(pod.Annotations[pkg.PurePodInfoString]), &purePodInfo); err != nil {
					return fmt.Errorf("unmarshal pod %s kse.com/pure-pods err: %s\n", pod.Name, err.Error())
				}
				// only for the successful scheduled pods
				if podHasScheduled(pod) {
					if purePodInfo.CurrentReschedulingTimes >= 1 && purePodInfo.CurrentReschedulingTimes <= schedulingRetries {
						if err := lf.delPod(pod); err != nil {
							return err
						}
						purePodInfo := pkg.PurePodInfo{
							CurrentReschedulingTimes: purePodInfo.CurrentReschedulingTimes + 1,
							PodScheduledHosts: nil}
						if err := lf.createPod(pod, &purePodInfo); err != nil {
							return err
						}
					}
					if purePodInfo.CurrentReschedulingTimes > schedulingRetries {
						purePodInfo := pkg.PurePodInfo{
							CurrentReschedulingTimes: purePodInfo.CurrentReschedulingTimes,
							PodScheduledHosts: nil}
						if err := lf.updatePod(pod, &purePodInfo); err != nil {
							return err
						}
					}
					return nil
				}
				// our Podrescheduling preFilter plugin  caused pod unschedulable, just delete pod and it's scheduled-hosts
				if podUnschedulable(pod) {
					if err := lf.delPod(pod); err != nil {
						return err
					}
					purePodInfo := pkg.PurePodInfo{
						CurrentReschedulingTimes: purePodInfo.CurrentReschedulingTimes,
						PodScheduledHosts: nil}
					if err := lf.createPod(pod, &purePodInfo); err != nil {
						return err
					}
				}
			} else {
				// first time rescheduling pods, so the pod.Annotations[pkg.PurePodInfoString] is empty, add it, and
				// the PodScheduledHosts set nil
				if podHasScheduled(pod) {
					if err := lf.delPod(pod); err != nil {
						return err
					}
					purePodInfo := pkg.PurePodInfo{CurrentReschedulingTimes: 1, PodScheduledHosts: nil}
					if err := lf.createPod(pod, &purePodInfo); err != nil {
						return err
					}
				}
			}
		}

	}
	return nil
}

func (lf *ListFunc) updateDeploy(deploy *appsv1.Deployment, deployInfo *pkg.DeployInfo) error {
	//exclude the same elements in slice
	if deployInfo.DeployScheduledHosts != nil {
		newStr := sets.NewString(deployInfo.DeployScheduledHosts...)
		deployInfo.DeployScheduledHosts = newStr.List()
	}
	byteDeployInfo, err := json.Marshal(deployInfo)
	if err != nil {
		return  fmt.Errorf("marshal deploy %s kse.com/deploy err: %s\n", deploy.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		deploy.Annotations[pkg.DeployInfoString] = string(byteDeployInfo)
		_, updateErr := lf.K8sClientSet.AppsV1().Deployments(deploy.Namespace).Update(context.TODO(), deploy, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) updateRs(rs *appsv1.ReplicaSet, rsInfo *pkg.RsInfo) error {
	//exclude the same elements in slice
	if rsInfo.RsScheduledHosts != nil {
		newStr := sets.NewString(rsInfo.RsScheduledHosts...)
		rsInfo.RsScheduledHosts = newStr.List()
	}
	byteRsInfo, err := json.Marshal(rsInfo)
	if err != nil {
		return  fmt.Errorf("marshal replicasets %s kse.com/rs err: %s\n", rs.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		rs.Annotations[pkg.RsInfoString] = string(byteRsInfo)
		_, updateErr := lf.K8sClientSet.AppsV1().ReplicaSets(rs.Namespace).Update(context.TODO(), rs, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) createCj(cj *batchv1.CronJob, cjInfo *pkg.CjInfo) error {
	//exclude the same elements in slice
	if cjInfo.CjScheduledHosts != nil {
		newStr := sets.NewString(cjInfo.CjScheduledHosts...)
		cjInfo.CjScheduledHosts = newStr.List()
	}
	byteCjInfo, err := json.Marshal(cjInfo)
	if err != nil {
		return  fmt.Errorf("marshal cronjob %s kse.com/cj err: %s\n", cj.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		newCj := cj.DeepCopy()
		newCj.UID = ""
		newCj.ResourceVersion = ""
		newCj.Status = batchv1.CronJobStatus{}
		newCj.Annotations[pkg.CjInfoString] = string(byteCjInfo)
		_, createErr := lf.K8sClientSet.BatchV1().CronJobs(cj.Namespace).Create(context.TODO(), newCj, metav1.CreateOptions{})
		return createErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) updateCj(cj *batchv1.CronJob, cjInfo *pkg.CjInfo) error {
	//exclude the same elements in slice
	if cjInfo.CjScheduledHosts != nil {
		newStr := sets.NewString(cjInfo.CjScheduledHosts...)
		cjInfo.CjScheduledHosts = newStr.List()
	}
	byteCjInfo, err := json.Marshal(cjInfo)
	if err != nil {
		return  fmt.Errorf("marshal cronjob %s kse.com/cj err: %s\n", cj.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		cj.Annotations[pkg.CjInfoString] = string(byteCjInfo)
		_, updateErr := lf.K8sClientSet.BatchV1().CronJobs(cj.Namespace).Update(context.TODO(), cj, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) createJob(job *batchv1.Job, jobInfo *pkg.JobInfo) error {
	//exclude the same elements in slice
	if jobInfo.JobScheduledHosts != nil {
		newStr := sets.NewString(jobInfo.JobScheduledHosts...)
		jobInfo.JobScheduledHosts = newStr.List()
	}
	byteJobInfo, err := json.Marshal(jobInfo)
	if err != nil {
		return  fmt.Errorf("marshal job %s kse.com/job err: %s\n", job.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		newJob := job.DeepCopy()
		newJob.Labels = nil
		newJob.UID = ""
		newJob.Spec.Selector = nil
		newJob.Spec.Template.Labels = nil
		newJob.ResourceVersion = ""
		newJob.Status = batchv1.JobStatus{}
		newJob.Annotations[pkg.JobInfoString] = string(byteJobInfo)
		_, createErr := lf.K8sClientSet.BatchV1().Jobs(job.Namespace).Create(context.TODO(), newJob, metav1.CreateOptions{})
		return createErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) updateJob(job *batchv1.Job, jobInfo *pkg.JobInfo) error {
	//exclude the same elements in slice
	if jobInfo.JobScheduledHosts != nil {
		newStr := sets.NewString(jobInfo.JobScheduledHosts...)
		jobInfo.JobScheduledHosts = newStr.List()
	}
	byteJobInfo, err := json.Marshal(jobInfo)
	if err != nil {
		return  fmt.Errorf("marshal job %s kse.com/job err: %s\n", job.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		job.Annotations[pkg.JobInfoString] = string(byteJobInfo)
		_, updateErr := lf.K8sClientSet.BatchV1().Jobs(job.Namespace).Update(context.TODO(), job, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) updateDs(ds *appsv1.DaemonSet, annotationValue string) error {

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		ds.Annotations[pkg.CurrentReschedulingTimeString] = annotationValue
		_, updateErr := lf.K8sClientSet.AppsV1().DaemonSets(ds.Namespace).Update(context.TODO(), ds, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	}
	return nil
}

func (lf *ListFunc) updateSts(sts *appsv1.StatefulSet, pod *corev1.Pod, stsPodsMap *pkg.StsPodsMap) error {
	//exclude the same elements in slice
	newStsPodsMap := make(pkg.StsPodsMap)
	for podName, podInfo := range *stsPodsMap {
		if podInfo.PodScheduledHosts != nil {
			newStr := sets.NewString(podInfo.PodScheduledHosts...)
			podInfo.PodScheduledHosts = newStr.List()
		}
		newStsPodsMap[podName] = podInfo
	}
	byteStsPodsMap, err := json.Marshal(newStsPodsMap)
	if err != nil {
		return fmt.Errorf("marshal %s statefulset %s kse.com/sts-pods-map err: %s\n", pod.Name, sts.Name, err.Error())
	}
	annotations := map[string]string{pkg.StsPodMapString: string(byteStsPodsMap)}
	for k, v := range annotations {
		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
			sts.Annotations[k] = v
			_, updateErr := lf.K8sClientSet.AppsV1().StatefulSets(sts.Namespace).Update(context.TODO(), sts, metav1.UpdateOptions{})
			return updateErr
		})
		if retryErr != nil {
			return retryErr
		}
	}
	return nil
}

func (lf *ListFunc) createPod(pod *corev1.Pod, purePodInfo *pkg.PurePodInfo) error {
	//exclude the same elements in slice
	if purePodInfo.PodScheduledHosts != nil {
		newStr := sets.NewString(purePodInfo.PodScheduledHosts...)
		purePodInfo.PodScheduledHosts = newStr.List()
	}
	bytePurePodInfo, err := json.Marshal(purePodInfo)
	if err != nil {
		return fmt.Errorf("marshal pod %s kse.com/pod err: %s\n", pod.Name, err.Error())
	}
	byteScheduledHosts, err := json.Marshal(purePodInfo.PodScheduledHosts)
	if err != nil {
		return fmt.Errorf("marshal pod %s kse.com/pod err: %s\n", pod.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		newPod := pod.DeepCopy()
		if purePodInfo.PodScheduledHosts != nil {
			newPod.Annotations[pkg.SchedulinedHostString] = string(byteScheduledHosts)
		} else {
			if _, ok := newPod.Annotations[pkg.SchedulinedHostString]; ok {
				delete(newPod.Annotations, pkg.SchedulinedHostString)
			}
		}
		newPod.ResourceVersion = ""
		newPod.UID = ""
		newPod.Spec.NodeName = ""
		newPod.Status = corev1.PodStatus{}
		newPod.Annotations[pkg.PurePodInfoString] = string(bytePurePodInfo)
		_, createErr := lf.K8sClientSet.CoreV1().Pods(pod.Namespace).Create(context.TODO(), newPod, metav1.CreateOptions{})
		return createErr
	})
	if retryErr != nil {
		return retryErr
	}

	return nil
}

func (lf *ListFunc) updatePod(pod *corev1.Pod, purePodInfo *pkg.PurePodInfo) error {
	//exclude the same elements in slice
	if purePodInfo.PodScheduledHosts != nil {
		newStr := sets.NewString(purePodInfo.PodScheduledHosts...)
		purePodInfo.PodScheduledHosts = newStr.List()
	}
	bytePurePodInfo, err := json.Marshal(purePodInfo)
	if err != nil {
		return fmt.Errorf("marshal pod %s kse.com/pod err: %s\n", pod.Name, err.Error())
	}
	byteScheduledHosts, err := json.Marshal(purePodInfo.PodScheduledHosts)
	if err != nil {
		return fmt.Errorf("marshal pod %s kse.com/pod err: %s\n", pod.Name, err.Error())
	}
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
		if purePodInfo.PodScheduledHosts != nil {
			pod.Annotations[pkg.SchedulinedHostString] = string(byteScheduledHosts)
		} else {
			if _, ok := pod.Annotations[pkg.SchedulinedHostString]; ok {
				delete(pod.Annotations, pkg.SchedulinedHostString)
			}
		}
		pod.Annotations[pkg.PurePodInfoString] = string(bytePurePodInfo)
		_, updateErr := lf.K8sClientSet.CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metav1.UpdateOptions{})
		return updateErr
	})
	if retryErr != nil {
		return retryErr
	}

	return nil
}
