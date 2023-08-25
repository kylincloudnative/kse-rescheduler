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
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"reflect"
	"kse/kse-rescheduler/pkg"
	"testing"
	"time"
)


type fields struct {
	PodFile                        string
	ControllerFile                 string
	SubControllerFile              string
	BeforeOutOfTimeToRescheduling  bool
	Wanted                         map[string]string
}

func TestDoDeploy(t *testing.T) {
	tests := []struct{
		name string
		fields fields
	}{
		{
			name: "deploy empty scheduling-retries annotations",
			fields: fields{
				PodFile:            "testdata/deploy-pod.json",
				ControllerFile:     "testdata/deploy-empty-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				Wanted: map[string]string{pkg.DeployInfoString: ""},
			},
		},
		{
			name: "deploy empty scheduled hosts annotations before time",
			fields: fields{
				PodFile:            "testdata/deploy-pod.json",
				ControllerFile:     "testdata/deploy-empty-scheduled-hosts-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":1,"deployScheduledHosts":["master1"]}`))},
			},
		},
		{
			name: "deploy with annotations before time",
			fields: fields{
				PodFile:            "testdata/deploy-pod.json",
				ControllerFile:     "testdata/deploy-with-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":3,"deployScheduledHosts":["node1","node2","master1"]}`))},
			},
		},
		{
			name: "deploy with annotations but current scheduling retries than total scheduling retries before time",
			fields: fields{
				PodFile:            "testdata/deploy-pod.json",
				ControllerFile:     "testdata/deploy-than-scheduling-retries-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":10,"deployScheduledHosts":null}`))},
			},
		},
		{
			name: "deploy with annotations but pod unschedulable before time",
			fields: fields{
				PodFile:            "testdata/deploy-pod-unschedulable.json",
				ControllerFile:     "testdata/deploy-with-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":2,"deployScheduledHosts":null}`))},
			},
		},
		{
			name: "deploy empty scheduled hosts annotations after time",
			fields: fields{
				PodFile:            "testdata/deploy-pod.json",
				ControllerFile:     "testdata/deploy-empty-scheduled-hosts-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":1,"deployScheduledHosts":null}`))},
			},
		},
		{
			name: "deploy with annotations after time",
			fields: fields{
				PodFile:            "testdata/deploy-pod.json",
				ControllerFile:     "testdata/deploy-with-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":3,"deployScheduledHosts":null}`))},
			},
		},
		{
			name: "deploy with annotations but current scheduling retries than total scheduling retries after time",
			fields: fields{
				PodFile:            "testdata/deploy-pod.json",
				ControllerFile:     "testdata/deploy-than-scheduling-retries-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":10,"deployScheduledHosts":null}`))},
			},
		},
		{
			name: "deploy with annotations but pod unschedulable after time",
			fields: fields{
				PodFile:            "testdata/deploy-pod-unschedulable.json",
				ControllerFile:     "testdata/deploy-with-annotations.json",
				SubControllerFile:  "testdata/deploy-rs.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.DeployInfoString: string([]byte(`{"currentReschedulingTimes":2,"deployScheduledHosts":null}`))},
			},
		},
	}
	for _, tt := range tests{
		t.Run(tt.name, func(t *testing.T) {
			pod, err := unMarshalPods(tt.fields.PodFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			deploy, err := unMarshalDeploy(tt.fields.ControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			rs, err := unMarshalRs(tt.fields.SubControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			nowTime := time.Now()
			timeDura, _ := time.ParseDuration("-1h")
			if tt.fields.BeforeOutOfTimeToRescheduling {
				pod.CreationTimestamp = v1.Time{Time: nowTime}
			} else {
				pod.CreationTimestamp = v1.Time{Time: nowTime.Add(timeDura)}
			}
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, pod, deploy, rs}
			doDeployTest(fakeObjects, pod, tt.fields.Wanted, t)
		})
	}
}

func TestDoRs(t *testing.T) {
	tests := []struct{
		name string
		fields fields
	}{
		{
			name: "rs empty scheduling-retries annotations",
			fields: fields{
				PodFile:            "testdata/rs-pod.json",
				ControllerFile:     "testdata/rs-empty-annotations.json",
				Wanted: map[string]string{pkg.RsInfoString: ""},
			},
		},
		{
			name: "rs with annotations but pending pods before time",
			fields: fields{
				PodFile:            "testdata/rs-pod-pending.json",
				ControllerFile:     "testdata/rs-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":2,"rsScheduledHosts":["node7","node8"]}`))},
			},
		},
		{
			name: "rs empty scheduled hosts annotations before time",
			fields: fields{
				PodFile:            "testdata/rs-pod.json",
				ControllerFile:     "testdata/rs-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":1,"rsScheduledHosts":["master1"]}`))},
			},
		},
		{
			name: "rs with annotations before time",
			fields: fields{
				PodFile:            "testdata/rs-pod.json",
				ControllerFile:     "testdata/rs-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":3,"rsScheduledHosts":["node7","node8","master1"]}`))},
			},
		},
		{
			name: "rs with annotations but current scheduling retries than total scheduling retries before time",
			fields: fields{
				PodFile:            "testdata/rs-pod.json",
				ControllerFile:     "testdata/rs-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":10,"rsScheduledHosts":null}`))},
			},
		},
		{
			name: "rs with annotations but pod unschedulable before time",
			fields: fields{
				PodFile:            "testdata/rs-pod-unschedulable.json",
				ControllerFile:     "testdata/rs-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":2,"rsScheduledHosts":null}`))},
			},
		},
		{
			name: "rs empty scheduled hosts annotations after time",
			fields: fields{
				PodFile:            "testdata/rs-pod.json",
				ControllerFile:     "testdata/rs-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":1,"rsScheduledHosts":null}`))},
			},
		},
		{
			name: "rs with annotations after time",
			fields: fields{
				PodFile:            "testdata/rs-pod.json",
				ControllerFile:     "testdata/rs-with-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":3,"rsScheduledHosts":null}`))},
			},
		},
		{
			name: "rs with annotations but current scheduling retries than total scheduling retries after time",
			fields: fields{
				PodFile:            "testdata/rs-pod.json",
				ControllerFile:     "testdata/rs-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":10,"rsScheduledHosts":null}`))},
			},
		},
		{
			name: "rs with annotations but pod unschedulable after time",
			fields: fields{
				PodFile:            "testdata/rs-pod-unschedulable.json",
				ControllerFile:     "testdata/rs-with-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.RsInfoString: string([]byte(`{"currentReschedulingTimes":2,"rsScheduledHosts":null}`))},
			},
		},
	}
	for _, tt := range tests{
		t.Run(tt.name, func(t *testing.T) {
			pod, err := unMarshalPods(tt.fields.PodFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			rs, err := unMarshalRs(tt.fields.ControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			nowTime := time.Now()
			timeDura, _ := time.ParseDuration("-1h")
			if tt.fields.BeforeOutOfTimeToRescheduling {
				pod.CreationTimestamp = v1.Time{Time: nowTime}
			} else {
				pod.CreationTimestamp = v1.Time{Time: nowTime.Add(timeDura)}
			}
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, pod, rs}
			doRsTest(fakeObjects, pod, tt.fields.Wanted, t)
		})
	}
}

func TestDoPod(t *testing.T) {
	tests := []struct {
		name string
		fields fields
	}{
		{
			name: "pure pod empty scheduling-retries annotations",
			fields: fields{
				PodFile:            "testdata/pure-pod-empty-annotations.json",
				Wanted: map[string]string{pkg.PurePodInfoString: ""},
			},
		},
		{
			name: "pure pod empty scheduled hosts annotations before time",
			fields: fields{
				PodFile:            "testdata/pure-pod-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":1,"podScheduledHosts":["master1"]}`))},
			},
		},
		{
			name: "pure pod with annotations before time",
			fields: fields{
				PodFile:            "testdata/pure-pod-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":3,"podScheduledHosts":["node1","node2","master1"]}`))},
			},
		},
		{
			name: "pure pod with annotations but current scheduling retries than scheduling retries before time",
			fields: fields{
				PodFile:            "testdata/pure-pod-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":4,"podScheduledHosts":null}`))},
			},
		},
		{
			name: "pure pod with annotations but pod unschedulable before time",
			fields: fields{
				PodFile:            "testdata/pure-pod-unschedulable.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":2,"podScheduledHosts":null}`))},
			},
		},
		{
			name: "pure pod empty scheduled hosts annotations after time",
			fields: fields{
				PodFile:            "testdata/pure-pod-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":1,"podScheduledHosts":null}`))},
			},
		},
		{
			name: "pure pod with annotations after time",
			fields: fields{
				PodFile:            "testdata/pure-pod-with-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":3,"podScheduledHosts":null}`))},
			},
		},{
			name: "pure pod with annotations but current scheduling retries than scheduling retries after time",
			fields: fields{
				PodFile:            "testdata/pure-pod-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":4,"podScheduledHosts":null}`))},
			},
		},
		{
			name: "pure pod with annotations but pod unschedulable after time",
			fields: fields{
				PodFile:            "testdata/pure-pod-unschedulable.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.PurePodInfoString: string([]byte(`{"currentReschedulingTimes":2,"podScheduledHosts":null}`))},
			},
		},
	}
	for _, tt := range tests{
		t.Run(tt.name, func(t *testing.T) {
			pod, err := unMarshalPods(tt.fields.PodFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			nowTime := time.Now()
			timeDura, _ := time.ParseDuration("-1h")
			if tt.fields.BeforeOutOfTimeToRescheduling {
				pod.CreationTimestamp = v1.Time{Time: nowTime}
			} else {
				pod.CreationTimestamp = v1.Time{Time: nowTime.Add(timeDura)}
			}
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, pod}
			doPodTest(fakeObjects, pod, tt.fields.Wanted, t)
		})
	}
}

func TestDoSts(t *testing.T) {
	tests := []struct{
		name  string
		fields fields
	} {
		{
			name: "sts empty scheduling-retries annotations",
			fields: fields{
				PodFile:            "testdata/sts-pod-0.json",
				ControllerFile:     "testdata/sts-empty-annotations.json",
				Wanted: map[string]string{pkg.StsPodMapString: ""},
			},
		},
		{
			name: "sts empty scheduled hosts annotations before time",
			fields: fields{
				PodFile:            "testdata/sts-pod-0.json",
				ControllerFile:     "testdata/sts-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":1,"podScheduledHosts":["master1"]}}`))},
			},
		},
		{
			name: "sts only one pod scheduled hosts annotations before time",
			fields: fields{
				PodFile:            "testdata/sts-pod-1.json",
				ControllerFile:     "testdata/sts-only-one-pod-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":2,"podScheduledHosts":["node1","node2"]},"my-web-1":{"currentReschedulingTimes":1,"podScheduledHosts":["master4"]}}`))},
			},
		},
		{
			name: "sts with annotations before time",
			fields: fields{
				PodFile:            "testdata/sts-pod-1.json",
				ControllerFile:     "testdata/sts-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":2,"podScheduledHosts":["node1","node2"]},"my-web-1":{"currentReschedulingTimes":2,"podScheduledHosts":["node2","master4"]}}`))},
			},
		},
		{
			name: "sts with annotations but current scheduling retries than scheduling retries before time",
			fields: fields{
				PodFile:            "testdata/sts-pod-0.json",
				ControllerFile:     "testdata/sts-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":4,"podScheduledHosts":null},"my-web-1":{"currentReschedulingTimes":1,"podScheduledHosts":["node2"]}}`))},
			},
		},
		{
			name: "sts with annotations but one pod unschedulable before time",
			fields: fields{
				PodFile:            "testdata/sts-pod-0-unschedulable.json",
				ControllerFile:     "testdata/sts-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":2,"podScheduledHosts":null},"my-web-1":{"currentReschedulingTimes":1,"podScheduledHosts":["node2"]}}`))},
			},
		},
		{
			name: "sts empty scheduled hosts annotations after time",
			fields: fields{
				PodFile:            "testdata/sts-pod-0.json",
				ControllerFile:     "testdata/sts-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":1,"podScheduledHosts":null}}`))},
			},
		},
		{
			name: "sts only one pod scheduled hosts annotations after time",
			fields: fields{
				PodFile:            "testdata/sts-pod-1.json",
				ControllerFile:     "testdata/sts-only-one-pod-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":2,"podScheduledHosts":["node1","node2"]},"my-web-1":{"currentReschedulingTimes":1,"podScheduledHosts":null}}`))},
			},
		},
		{
			name: "sts with annotations after time",
			fields: fields{
				PodFile:            "testdata/sts-pod-1.json",
				ControllerFile:     "testdata/sts-with-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":2,"podScheduledHosts":["node1","node2"]},"my-web-1":{"currentReschedulingTimes":2,"podScheduledHosts":null}}`))},
			},
		},
		{
			name: "sts with annotations but current scheduling retries than scheduling retries after time",
			fields: fields{
				PodFile:            "testdata/sts-pod-0.json",
				ControllerFile:     "testdata/sts-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":4,"podScheduledHosts":null},"my-web-1":{"currentReschedulingTimes":1,"podScheduledHosts":["node2"]}}`))},
			},
		},
		{
			name: "sts with annotations but one pod unschedulable after time",
			fields: fields{
				PodFile:            "testdata/sts-pod-0-unschedulable.json",
				ControllerFile:     "testdata/sts-with-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.StsPodMapString: string([]byte(`{"my-web-0":{"currentReschedulingTimes":2,"podScheduledHosts":null},"my-web-1":{"currentReschedulingTimes":1,"podScheduledHosts":["node2"]}}`))},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod, err := unMarshalPods(tt.fields.PodFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			sts, err := unMarshalSts(tt.fields.ControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			nowTime := time.Now()
			timeDura, _ := time.ParseDuration("-1h")
			if tt.fields.BeforeOutOfTimeToRescheduling {
				pod.CreationTimestamp = v1.Time{Time: nowTime}
			} else {
				pod.CreationTimestamp = v1.Time{Time: nowTime.Add(timeDura)}
			}
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, pod, sts}
			doStsTest(fakeObjects, pod, tt.fields.Wanted, t)
		})
	}
}

func TestDoDs(t *testing.T) {
	tests := []struct{
		name string
		fields fields
	}{
		{
			name: "ds empty scheduling-retries annotations",
			fields: fields{
				PodFile:            "testdata/ds-pod.json",
				ControllerFile:     "testdata/ds-empty-annotations.json",
				Wanted: map[string]string{pkg.CurrentReschedulingTimeString: ""},
			},
		},
		{
			name: "ds empty current retries times annotations",
			fields: fields{
				PodFile:            "testdata/ds-pod.json",
				ControllerFile:     "testdata/ds-empty-current-retries-times-annotations.json",
				Wanted: map[string]string{pkg.CurrentReschedulingTimeString: string([]byte("1"))},
			},
		},
		{
			name: "ds with annotations",
			fields: fields{
				PodFile:            "testdata/ds-pod.json",
				ControllerFile:     "testdata/ds-with-annotations.json",
				Wanted: map[string]string{pkg.CurrentReschedulingTimeString: string([]byte("3"))},
			},
		},
		{
			name: "ds with annotations but current scheduling retries than scheduling retries",
			fields: fields{
				PodFile:            "testdata/ds-pod.json",
				ControllerFile:     "testdata/ds-than-scheduling-retries-annotations.json",
				Wanted: map[string]string{pkg.CurrentReschedulingTimeString: string([]byte("4"))},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod, err := unMarshalPods(tt.fields.PodFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			ds, err := unMarshalDs(tt.fields.ControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, pod, ds}
			doDsTest(fakeObjects, pod, tt.fields.Wanted, t)
		})
	}
}

func TestDoJob(t *testing.T) {
	tests := []struct{
		name string
		fields fields
	}{
		{
			name: "job empty scheduling-retries annotations",
			fields: fields{
				PodFile:            "testdata/job-pod.json",
				ControllerFile:     "testdata/job-empty-annotations.json",
				Wanted: map[string]string{pkg.JobInfoString: ""},
			},
		},
		{
			name: "job empty scheduled hosts annotations before time",
			fields: fields{
				PodFile:            "testdata/job-pod.json",
				ControllerFile:     "testdata/job-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":1,"jobScheduledHosts":["master2"]}`))},
			},
		},
		{
			name: "job with annotations before time",
			fields: fields{
				PodFile:            "testdata/job-pod.json",
				ControllerFile:     "testdata/job-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":2,"jobScheduledHosts":["node7","node8","master2"]}`))},
			},
		},
		{
			name: "job with annotations but current scheduling retries than scheduling retries before time",
			fields: fields{
				PodFile:            "testdata/job-pod.json",
				ControllerFile:     "testdata/job-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":4,"jobScheduledHosts":null}`))},
			},
		},
		{
			name: "job with annotations but pod unschedulable before time",
			fields: fields{
				PodFile:            "testdata/job-pod-unschedulable.json",
				ControllerFile:     "testdata/job-with-annotations.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":1,"jobScheduledHosts":null}`))},
			},
		},
		{
			name: "job empty scheduled hosts annotations after time",
			fields: fields{
				PodFile:            "testdata/job-pod.json",
				ControllerFile:     "testdata/job-empty-scheduled-hosts-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":1,"jobScheduledHosts":null}`))},
			},
		},
		{
			name: "job with annotations after time",
			fields: fields{
				PodFile:            "testdata/job-pod.json",
				ControllerFile:     "testdata/job-with-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":2,"jobScheduledHosts":null}`))},
			},
		},
		{
			name: "job with annotations but current scheduling retries than scheduling retries after time",
			fields: fields{
				PodFile:            "testdata/job-pod.json",
				ControllerFile:     "testdata/job-than-scheduling-retries-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":4,"jobScheduledHosts":null}`))},
			},
		},
		{
			name: "job with annotations but pod unschedulable after time",
			fields: fields{
				PodFile:            "testdata/job-pod-unschedulable.json",
				ControllerFile:     "testdata/job-with-annotations.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.JobInfoString: string([]byte(`{"currentReschedulingTimes":1,"jobScheduledHosts":null}`))},
			},
		},
	}
	for _, tt := range tests{
		t.Run(tt.name, func(t *testing.T) {
			pod, err := unMarshalPods(tt.fields.PodFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			jb, err := unMarshalJob(tt.fields.ControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			nowTime := time.Now()
			timeDura, _ := time.ParseDuration("-1h")
			if tt.fields.BeforeOutOfTimeToRescheduling {
				pod.CreationTimestamp = v1.Time{Time: nowTime}
			} else {
				pod.CreationTimestamp = v1.Time{Time: nowTime.Add(timeDura)}
			}
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, pod, jb}
			doJobTest(fakeObjects, pod, tt.fields.Wanted, t)
		})
	}
}

func TestDoCj(t *testing.T) {
	tests := []struct{
		name string
		fields fields
	}{
		{
			name: "cj empty scheduling-retries annotations",
			fields: fields{
				PodFile:            "testdata/cj-pod.json",
				ControllerFile:     "testdata/cj-empty-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				Wanted: map[string]string{pkg.CjInfoString: ""},
			},
		},
		{
			name: "cj empty scheduled hosts annotations before time",
			fields: fields{
				PodFile:            "testdata/cj-pod.json",
				ControllerFile:     "testdata/cj-empty-scheduled-hosts-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":1,"cjScheduledHosts":["master1"]}`))},
			},
		},
		{
			name: "cj with annotations before time",
			fields: fields{
				PodFile:            "testdata/cj-pod.json",
				ControllerFile:     "testdata/cj-with-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":2,"cjScheduledHosts":["node7","node8","master1"]}`))},
			},
		},
		{
			name: "cj with annotations but current scheduling retries than scheduling retries before time",
			fields: fields{
				PodFile:            "testdata/cj-pod.json",
				ControllerFile:     "testdata/cj-than-scheduling-retries-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":4,"cjScheduledHosts":null}`))},
			},
		},
		{
			name: "cj with annotations but pod unschedulable before time",
			fields: fields{
				PodFile:            "testdata/cj-pod-unschedulable.json",
				ControllerFile:     "testdata/cj-with-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: true,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":1,"cjScheduledHosts":null}`))},
			},
		},
		{
			name: "cj empty scheduled hosts annotations after time",
			fields: fields{
				PodFile:            "testdata/cj-pod.json",
				ControllerFile:     "testdata/cj-empty-scheduled-hosts-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":1,"cjScheduledHosts":null}`))},
			},
		},
		{
			name: "cj with annotations after time",
			fields: fields{
				PodFile:            "testdata/cj-pod.json",
				ControllerFile:     "testdata/cj-with-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":2,"cjScheduledHosts":null}`))},
			},
		},
		{
			name: "cj with annotations but current scheduling retries than  scheduling retries after time",
			fields: fields{
				PodFile:            "testdata/cj-pod.json",
				ControllerFile:     "testdata/cj-than-scheduling-retries-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":4,"cjScheduledHosts":null}`))},
			},
		},
		{
			name: "cj with annotations but pod unschedulable after time",
			fields: fields{
				PodFile:            "testdata/cj-pod-unschedulable.json",
				ControllerFile:     "testdata/cj-with-annotations.json",
				SubControllerFile:  "testdata/cj-job.json",
				BeforeOutOfTimeToRescheduling: false,
				Wanted: map[string]string{pkg.CjInfoString: string([]byte(`{"currentReschedulingTimes":1,"cjScheduledHosts":null}`))},
			},
		},
	}
	for _, tt := range tests{
		t.Run(tt.name, func(t *testing.T) {
			pod, err := unMarshalPods(tt.fields.PodFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			cj, err := unMarshalCj(tt.fields.ControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			jb, err := unMarshalJob(tt.fields.SubControllerFile)
			if err != nil {
				t.Errorf(err.Error())
				return
			}
			nowTime := time.Now()
			timeDura, _ := time.ParseDuration("-1h")
			if tt.fields.BeforeOutOfTimeToRescheduling {
				pod.CreationTimestamp = v1.Time{Time: nowTime}
			} else {
				pod.CreationTimestamp = v1.Time{Time: nowTime.Add(timeDura)}
			}
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, pod, cj, jb}
			doCjTest(fakeObjects, pod, tt.fields.Wanted, t)
		})
	}
}

func TestList(t *testing.T) {
	type watchFields struct {
		podFiles                         []string
		controllerFile                   string
		subControllerFile                string
		beforeOutOfTimeToRescheduling    bool
		wanted                           pkg.CjInfo
	}
	tests := []struct{
		name   string
		fields watchFields
	}{
		{
			name: "watch cj before time",
			fields: watchFields{
				podFiles:                      []string{"testdata/cj-pod.json", "testdata/cj-uncompleted-pod-1.json", "testdata/cj-uncompleted-pod-2.json"},
				controllerFile:                "testdata/cj-with-annotations.json",
				subControllerFile:             "testdata/cj-job.json",
				beforeOutOfTimeToRescheduling: true,
				wanted:                        pkg.CjInfo{CurrentReschedulingTimes: 3, CjScheduledHosts: []string{"node7","node8","node1","node2"}},
			},
		},
		{
			name: "watch cj after time",
			fields: watchFields{
				podFiles:                      []string{"testdata/cj-pod.json", "testdata/cj-uncompleted-pod-1.json", "testdata/cj-uncompleted-pod-2.json"},
				controllerFile:                "testdata/cj-with-annotations.json",
				subControllerFile:             "testdata/cj-job.json",
				beforeOutOfTimeToRescheduling: false,
				wanted:                        pkg.CjInfo{CurrentReschedulingTimes: 3, CjScheduledHosts: nil},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, err := unMarshalJob(tt.fields.subControllerFile)
			if err != nil {
				t.Fatal(err)
			}
			cj, err := unMarshalCj(tt.fields.controllerFile)
			fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, cj, job}
			if err != nil {
				t.Fatal(err)
			}
			for _, podFile := range tt.fields.podFiles {
				pod, err := unMarshalPods(podFile)
				if err != nil {
					t.Fatal(err)
				}
				nowTime := time.Now()
				timeDura, _ := time.ParseDuration("-1h")
				if tt.fields.beforeOutOfTimeToRescheduling {
					pod.CreationTimestamp = v1.Time{Time: nowTime}
				} else {
					pod.CreationTimestamp = v1.Time{Time: nowTime.Add(timeDura)}
				}
				fakeObjects = append(fakeObjects, pod)
			}
			lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
			lf.List()
			updateCj, err := lf.K8sClientSet.BatchV1().CronJobs("default").Get(context.TODO(), cj.Name, v1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var cjInfo pkg.CjInfo
			if err := json.Unmarshal([]byte(updateCj.Annotations[pkg.CjInfoString]), &cjInfo); err != nil {
				t.Fatal(err)
			}

			currentReschedulingTimesGot := cjInfo.CurrentReschedulingTimes
			cjScheduledHostsGot := cjInfo.CjScheduledHosts
			if !reflect.DeepEqual(tt.fields.wanted.CurrentReschedulingTimes, currentReschedulingTimesGot) {
				t.Errorf("test returned wrong current rescheduling times: got %v want %v", currentReschedulingTimesGot, tt.fields.wanted.CurrentReschedulingTimes)
				return
			}
			if !isSameElements(tt.fields.wanted.CjScheduledHosts, cjScheduledHostsGot) {
				t.Errorf("test returned wrong scheduled hosts: got %v want %v", cjScheduledHostsGot, tt.fields.wanted.CjScheduledHosts)
				return
			}
		})
	}
}

func doDsTest(fakeObjects []runtime.Object, pod *corev1.Pod, wanted map[string]string, t *testing.T){
	lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
	podOwnerInfo, err := lf.GetPodOwnerInfo(pod)
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.doDs(pod, *podOwnerInfo); err != nil {
		t.Fatal(err)
	}
	ds, err := lf.K8sClientSet.AppsV1().DaemonSets("default").Get(context.TODO(), podOwnerInfo.PodOwnerName, v1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{pkg.CurrentReschedulingTimeString: ds.Annotations[pkg.CurrentReschedulingTimeString]}
	if !reflect.DeepEqual(wanted, got) {
		t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
		return
	}
}

func doStsTest(fakeObjects []runtime.Object, pod *corev1.Pod, wanted map[string]string, t *testing.T){
	lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
	podOwnerInfo, err := lf.GetPodOwnerInfo(pod)
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.doSts(pod, *podOwnerInfo); err != nil {
		t.Fatal(err)
	}
	// get the  sts annotations after doSts
	gotSts, err := lf.K8sClientSet.AppsV1().StatefulSets("default").Get(context.TODO(), podOwnerInfo.PodOwnerName, v1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{pkg.StsPodMapString: gotSts.Annotations[pkg.StsPodMapString]}
	if wanted[pkg.StsPodMapString] == "" {
		if !reflect.DeepEqual(wanted, got) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	} else {
		var gotStsPodsMap pkg.StsPodsMap
		if err := json.Unmarshal([]byte(gotSts.Annotations[pkg.StsPodMapString]), &gotStsPodsMap); err != nil {
			t.Fatal(err)
		}

		var wantedStsPodsMap pkg.StsPodsMap
		if err := json.Unmarshal([]byte(wanted[pkg.StsPodMapString]), &wantedStsPodsMap); err != nil {
			t.Fatal(err)
		}

		if !isSameStsMap(wantedStsPodsMap, gotStsPodsMap) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	}
}

func doPodTest(fakeObjects []runtime.Object, pod *corev1.Pod, wanted map[string]string, t *testing.T){
	lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
	if err := lf.doPods(pod); err != nil {
		t.Fatal(err)
	}
	// get the  pod annotations after doPods
	gotPod, err := lf.K8sClientSet.CoreV1().Pods("default").Get(context.TODO(), pod.Name, v1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{pkg.PurePodInfoString: gotPod.Annotations[pkg.PurePodInfoString]}
	if wanted[pkg.PurePodInfoString] == "" {
		if !reflect.DeepEqual(wanted, got) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	} else {
		var gotPurePodInfo pkg.PurePodInfo
		if err := json.Unmarshal([]byte(gotPod.Annotations[pkg.PurePodInfoString]), &gotPurePodInfo); err != nil {
			t.Fatal(err)
		}

		var wantedPurePodInfo pkg.PurePodInfo
		if err := json.Unmarshal([]byte(wanted[pkg.PurePodInfoString]), &wantedPurePodInfo); err != nil {
			t.Fatal(err)
		}
		if gotPurePodInfo.CurrentReschedulingTimes != wantedPurePodInfo.CurrentReschedulingTimes {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
		if !isSameElements(gotPurePodInfo.PodScheduledHosts, wantedPurePodInfo.PodScheduledHosts) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	}
}

func doRsTest(fakeObjects []runtime.Object, pod *corev1.Pod, wanted map[string]string, t *testing.T){
	lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
	podOwnerInfo, err := lf.GetPodOwnerInfo(pod)
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.doRs(pod, *podOwnerInfo); err != nil {
		t.Fatal(err)
	}
	// get the  rs annotations after doRs
	gotRs, err := lf.K8sClientSet.AppsV1().ReplicaSets("default").Get(context.TODO(), podOwnerInfo.PodOwnerName, v1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{pkg.RsInfoString: gotRs.Annotations[pkg.RsInfoString]}
	if wanted[pkg.RsInfoString] == "" {
		if !reflect.DeepEqual(wanted, got) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	} else {
		var gotRsInfo pkg.RsInfo
		if err := json.Unmarshal([]byte(gotRs.Annotations[pkg.RsInfoString]), &gotRsInfo); err != nil {
			t.Fatal(err)
		}
		var wantedRsInfo pkg.RsInfo
		if err := json.Unmarshal([]byte(wanted[pkg.RsInfoString]), &wantedRsInfo); err != nil {
			t.Fatal(err)
		}
		if gotRsInfo.CurrentReschedulingTimes != wantedRsInfo.CurrentReschedulingTimes {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
		if !isSameElements(gotRsInfo.RsScheduledHosts, wantedRsInfo.RsScheduledHosts) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	}
}

func doDeployTest(fakeObjects []runtime.Object, pod *corev1.Pod, wanted map[string]string, t *testing.T){
	lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
	podOwnerInfo, err := lf.GetPodOwnerInfo(pod)
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.doDeploys(pod, *podOwnerInfo); err != nil {
		t.Fatal(err)
	}
	// get the deploy annotations after doDeploy
	gotDeploy, err := lf.K8sClientSet.AppsV1().Deployments("default").Get(context.TODO(), podOwnerInfo.PodOwnerName, v1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{pkg.DeployInfoString: gotDeploy.Annotations[pkg.DeployInfoString]}
	if wanted[pkg.DeployInfoString] == "" {
		if !reflect.DeepEqual(wanted, got) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	} else {
		var gotDeployInfo pkg.DeployInfo
		if err := json.Unmarshal([]byte(gotDeploy.Annotations[pkg.DeployInfoString]), &gotDeployInfo); err != nil {
			t.Fatal(err)
		}
		var wantedDeployInfo pkg.DeployInfo
		if err := json.Unmarshal([]byte(wanted[pkg.DeployInfoString]), &wantedDeployInfo); err != nil {
			t.Fatal(err)
		}
		if gotDeployInfo.CurrentReschedulingTimes != wantedDeployInfo.CurrentReschedulingTimes {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
		if !isSameElements(gotDeployInfo.DeployScheduledHosts, wantedDeployInfo.DeployScheduledHosts) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	}
}

func doJobTest(fakeObjects []runtime.Object, pod *corev1.Pod, wanted map[string]string, t *testing.T){
	lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
	podOwnerInfo, err := lf.GetPodOwnerInfo(pod)
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.doJobs(pod, *podOwnerInfo); err != nil {
		t.Fatal(err)
	}
	gotJb, err := lf.K8sClientSet.BatchV1().Jobs("default").Get(context.TODO(), podOwnerInfo.PodOwnerName, v1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{pkg.JobInfoString: gotJb.Annotations[pkg.JobInfoString]}
	if wanted[pkg.JobInfoString] == "" {
		if !reflect.DeepEqual(wanted, got) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	} else {
		var gotJbInfo pkg.JobInfo
		if err := json.Unmarshal([]byte(gotJb.Annotations[pkg.JobInfoString]), &gotJbInfo); err != nil {
			t.Fatal(err)
		}
		var wantedJbInfo pkg.JobInfo
		if err := json.Unmarshal([]byte(wanted[pkg.JobInfoString]), &wantedJbInfo); err != nil {
			t.Fatal(err)
		}
		if gotJbInfo.CurrentReschedulingTimes != wantedJbInfo.CurrentReschedulingTimes {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
		if !isSameElements(gotJbInfo.JobScheduledHosts, wantedJbInfo.JobScheduledHosts) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	}
}

func doCjTest(fakeObjects []runtime.Object, pod *corev1.Pod, wanted map[string]string, t *testing.T){
	lf := &ListFunc{K8sClientSet: fake.NewSimpleClientset(fakeObjects...)}
	podOwnerInfo, err := lf.GetPodOwnerInfo(pod)
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.doCjs(pod, *podOwnerInfo); err != nil {
		t.Fatal(err)
	}
	gotCj, err := lf.K8sClientSet.BatchV1().CronJobs("default").Get(context.TODO(), podOwnerInfo.PodOwnerName, v1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{pkg.CjInfoString: gotCj.Annotations[pkg.CjInfoString]}
	if wanted[pkg.CjInfoString] == "" {
		if !reflect.DeepEqual(wanted, got) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	} else {
		var gotCjInfo pkg.CjInfo
		if err := json.Unmarshal([]byte(gotCj.Annotations[pkg.CjInfoString]), &gotCjInfo); err != nil {
			t.Fatal(err)
		}
		var wantedCjInfo pkg.CjInfo
		if err := json.Unmarshal([]byte(wanted[pkg.CjInfoString]), &wantedCjInfo); err != nil {
			t.Fatal(err)
		}
		if gotCjInfo.CurrentReschedulingTimes != wantedCjInfo.CurrentReschedulingTimes {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
		if !isSameElements(gotCjInfo.CjScheduledHosts, wantedCjInfo.CjScheduledHosts) {
			t.Errorf("test returned wrong annotations: got %v want %v", got, wanted)
			return
		}
	}
}

func unMarshalPods(podFile string) (*corev1.Pod, error) {
	var pod corev1.Pod
	bytePod, err := ioutil.ReadFile(podFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(bytePod, &pod); err != nil {
		return nil, fmt.Errorf("unMarshal pod %s err: %s", podFile, err.Error())
	}
	return &pod, nil
}

func unMarshalDs(dsFile string) (*appsv1.DaemonSet, error) {
	var ds appsv1.DaemonSet
	byteDs, err := ioutil.ReadFile(dsFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(byteDs, &ds); err != nil {
		return nil, fmt.Errorf("unMarshal ds %s err: %s", dsFile, err.Error())
	}
	return &ds, nil
}

func unMarshalDeploy(deployFile string) (*appsv1.Deployment, error) {
	var deploy appsv1.Deployment
	byteDeploy, err := ioutil.ReadFile(deployFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(byteDeploy, &deploy); err != nil {
		return nil, fmt.Errorf("unMarshal deployment %s err: %s", deployFile, err.Error())
	}
	return &deploy, nil
}

func unMarshalSts(stsFile string) (*appsv1.StatefulSet, error) {
	var sts appsv1.StatefulSet
	byteSts, err := ioutil.ReadFile(stsFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(byteSts, &sts); err != nil {
		return nil, fmt.Errorf("unMarshal statefulset %s err: %s", stsFile, err.Error())
	}
	return &sts, nil
}

func unMarshalRs(rsFile string) (*appsv1.ReplicaSet, error) {
	var rs appsv1.ReplicaSet
	byteRs, err := ioutil.ReadFile(rsFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(byteRs, &rs); err != nil {
		return nil, fmt.Errorf("unMarshal replicaset %s err: %s", rsFile, err.Error())
	}
	return &rs, nil
}

func unMarshalJob(jbFile string) (*batchv1.Job, error) {
	var jb batchv1.Job
	byteJb, err := ioutil.ReadFile(jbFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(byteJb, &jb); err != nil {
		return nil, fmt.Errorf("unMarshal job %s err: %s", jbFile, err.Error())
	}
	return &jb, nil
}

func unMarshalCj(cjFile string) (*batchv1.CronJob, error) {
	var cj batchv1.CronJob
	byteCj, err := ioutil.ReadFile(cjFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(byteCj, &cj); err != nil {
		return nil, fmt.Errorf("unMarshal cronjob %s err: %s", cjFile, err.Error())
	}
	return &cj, nil
}

func isSameElements(slice1, slice2[]string) bool {
	// Create maps to count occurrences of elements in both slices
	count1 := make(map[string]int)
	count2 := make(map[string]int)

	for _, v := range slice1 {
		count1[v]++
	}

	for _, v := range slice2 {
		count2[v]++
	}

	// Compare the maps
	if len(count1) != len(count2) {
		// Slices don't have the same elements
		return false
	}
	for k, v := range count1 {
		if count2[k] != v {
			// Slices don't have the same elements
			return false
		}
	}
	return true
}

func isSameStsMap(map1, map2 pkg.StsPodsMap) bool {
	for podName1, podInfo1 := range map1 {
		if podInfo2, ok := map2[podName1]; !ok {
			return false
		} else {
			if podInfo1.CurrentReschedulingTimes != podInfo2.CurrentReschedulingTimes {
				return false
			}
			if !isSameElements(podInfo1.PodScheduledHosts, podInfo2.PodScheduledHosts) {
				return false
			}
		}
	}
	for podName2, podInfo2 := range map2 {
		if podInfo1, ok := map2[podName2]; !ok {
			return false
		} else {
			if podInfo2.CurrentReschedulingTimes != podInfo1.CurrentReschedulingTimes {
				return false
			}
			if !isSameElements(podInfo2.PodScheduledHosts, podInfo1.PodScheduledHosts) {
				return false
			}
		}
	}
	return true
}