/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package podrescheduling

import (
	"context"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"kse/kse-rescheduler/pkg"
	testutil "kse/kse-rescheduler/test/util"
	"testing"
)
func TestPodRescheduling(t *testing.T) {
	tests := []struct {
		name                string
		pod                 *corev1.Pod
		nodes               []*corev1.Node
		wantStatus          *framework.Status
		wantPreFilterResult *framework.PreFilterResult
	}{
		{
			name: "pod with scheduled hosts",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{pkg.SchedulinedHostString: string([]byte(`["node1","node2","master1"]`))},
				},
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master3"}},
			},
			wantPreFilterResult: &framework.PreFilterResult{NodeNames: sets.NewString("master2", "master3")},
		},
		{
			name: "pod without scheduled hosts",
			pod: &corev1.Pod{},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master3"}},
			},
		},
		{
			name: "pod with all the k8s cluster nodes",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{pkg.SchedulinedHostString: string([]byte(`["master2","master3","node1","node2","master1"]`))},
				},
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "master3"}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := framework.NewCycleState()
			fh, _ := runtime.NewFramework(nil, nil, runtime.WithSnapshotSharedLister(testutil.NewFakeSharedLister(nil, test.nodes)))
			p, err := New(nil, fh)
			if err != nil {
				t.Fatalf("Creating plugin: %v", err)
			}
			gotPreFilterResult, gotStatus := p.(framework.PreFilterPlugin).PreFilter(context.Background(), state, test.pod)
			if diff := cmp.Diff(test.wantStatus, gotStatus); diff != "" {
				t.Errorf("unexpected PreFilter Status (-want,+got):\n%s", diff)
				return
			}
			if diff := cmp.Diff(test.wantPreFilterResult, gotPreFilterResult); diff != "" {
				t.Errorf("unexpected PreFilterResult (-want,+got):\n%s", diff)
				return
			}
		})
	}
}
