/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package admission

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

type fields struct {
	ContentType    string
	Method         string
	ReviewFile     string
	GoldenFile     string
	ControllerFile string
	ControllerType string
	WantCode       int
}

func TestAdmissionHandleFunc(t *testing.T) {
	tests := []struct{
		name string
		fields fields
	}{
		{
			name: "empty deploy annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-deploy-pod.json",
				GoldenFile:               "testdata/review-deploy-pod-empty-annotations-golden.json",
				ControllerFile:           "testdata/deploy-empty-annotations.json",
				ControllerType:           "Deployment",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "with deploy annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-deploy-pod.json",
				GoldenFile:               "testdata/review-deploy-pod-with-annotations-golden.json",
				ControllerFile:           "testdata/deploy-with-annotations.json",
				ControllerType:           "Deployment",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "empty rs annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-rs-pod.json",
				GoldenFile:               "testdata/review-rs-pod-empty-annotations-golden.json",
				ControllerFile:           "testdata/rs-empty-annotations.json",
				ControllerType:           "ReplicaSet",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "with rs annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-rs-pod.json",
				GoldenFile:               "testdata/review-rs-pod-with-annotations-golden.json",
				ControllerFile:           "testdata/rs-with-annotations.json",
				ControllerType:           "ReplicaSet",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "empty cj annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-cj-pod.json",
				GoldenFile:               "testdata/review-cj-pod-empty-annotations-golden.json",
				ControllerFile:           "testdata/cj-empty-annotations.json",
				ControllerType:           "CronJob",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "with cj annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-cj-pod.json",
				GoldenFile:               "testdata/review-cj-pod-with-annotations-golden.json",
				ControllerFile:           "testdata/cj-with-annotations.json",
				ControllerType:           "CronJob",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "empty job annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-job-pod.json",
				GoldenFile:               "testdata/review-job-pod-empty-annotations-golden.json",
				ControllerFile:           "testdata/job-empty-annotations.json",
				ControllerType:           "Job",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "with job annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-job-pod.json",
				GoldenFile:               "testdata/review-job-pod-with-annotations-golden.json",
				ControllerFile:           "testdata/job-with-annotations.json",
				ControllerType:           "Job",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "empty sts annotations post request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-sts-pod.json",
				GoldenFile:               "testdata/review-sts-pod-empty-annotations-golden.json",
				ControllerFile:           "testdata/sts-empty-annotations.json",
				ControllerType:           "StatefulSet",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "get request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "GET",
				ReviewFile:               "testdata/review-sts-pod.json",
				GoldenFile:               "",
				ControllerFile:           "testdata/sts-empty-annotations.json",
				ControllerType:           "StatefulSet",
				WantCode:                  http.StatusMethodNotAllowed,
			},
		},
		{
			name: "with sts annotations request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-sts-pod.json",
				GoldenFile:               "testdata/review-sts-pod-with-annotations-golden.json",
				ControllerFile:           "testdata/sts-with-annotations.json",
				ControllerType:           "StatefulSet",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "with sts annotations but sts pod name don't belongs to sts",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-sts-different-name-pod.json",
				GoldenFile:               "testdata/review-sts-different-name-pod-with-annotations-golden.json",
				ControllerFile:           "testdata/sts-with-annotations.json",
				ControllerType:           "StatefulSet",
				WantCode:                 http.StatusOK,
			},
		},
		{
			name: "with sts annotations but empty scheduled-hosts request",
			fields: fields{
				ContentType:              "application/json",
				Method:                   "POST",
				ReviewFile:               "testdata/review-sts-pod.json",
				GoldenFile:               "testdata/review-sts-pod-with-empty-scheduled-hosts-annotations-golden.json",
				ControllerFile:           "testdata/sts-empty-scheduled-host-annotations.json",
				ControllerType:           "StatefulSet",
				WantCode:                 http.StatusOK,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.fields.ControllerType {
			case "StatefulSet":
				sts, err := unMarshalSts(tt.fields.ControllerFile)
				if err != nil {
					t.Errorf(err.Error())
					return
				}
				fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, sts}
				doTest(fakeObjects, tt.fields, t)
			case "Deployment":
				deploy, err := unMarshalDeploy(tt.fields.ControllerFile)
				if err != nil {
					t.Errorf(err.Error())
					return
				}
				rs, err := unMarshalRs("testdata/deploy-rs.json")
				if err != nil {
					t.Errorf(err.Error())
				}
				fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, deploy, rs}
				doTest(fakeObjects, tt.fields, t)
			case "ReplicaSet":
				rs, err := unMarshalRs(tt.fields.ControllerFile)
				if err != nil {
					t.Errorf(err.Error())
					return
				}
				fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, rs}
				doTest(fakeObjects, tt.fields, t)
			case "CronJob":
				cj, err := unMarshalCj(tt.fields.ControllerFile)
				if err != nil {
					t.Errorf(err.Error())
					return
				}
				job, err := unMarshalJob("testdata/cj-job.json")
				if err != nil {
					t.Errorf(err.Error())
					return
				}
				fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, cj, job}
				doTest(fakeObjects, tt.fields, t)
			case "Job":
				job, err := unMarshalJob(tt.fields.ControllerFile)
				if err != nil {
					t.Errorf(err.Error())
					return
				}
				fakeObjects := []runtime.Object{&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "default"}}, job}
				doTest(fakeObjects, tt.fields, t)
			}
		})
	}
}

func doTest(fakeObjects []runtime.Object, fields fields, t *testing.T) {
	h := &RequestsHandler{
		K8sClientSet:                fake.NewSimpleClientset(fakeObjects...),
	}

	inputFile, err := os.Open(fields.ReviewFile)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(fields.Method, "/", inputFile)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Add("Content-Type", fields.ContentType)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(h.handleFunc)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != fields.WantCode {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, fields.WantCode)
		return
	}

	if fields.GoldenFile != "" {
		if err := compareResponse(rr.Body, fields.GoldenFile); err != nil {
			t.Errorf("TestAdmissionHandleFunc: %v", err)
		}
	}
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

func unMarshalJob(jobFile string) (*batchv1.Job, error) {
	var job batchv1.Job
	byteJob, err := ioutil.ReadFile(jobFile)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(byteJob, &job); err != nil {
		return nil, fmt.Errorf("unMarshal job %s err: %s", jobFile, err.Error())
	}
	return &job, nil
}

func compareResponse(got *bytes.Buffer, goldenFile string) error {
	golden, exists, err := readGolden(goldenFile)
	if err != nil {
		return fmt.Errorf("golden file: %s, exists: %t, err: %v", goldenFile, exists, err)
	}

	if !exists {
		f, err := os.Create(goldenFile)
		if err != nil {
			return fmt.Errorf("failed to create golden file: %s, error: %v", goldenFile, err)
		}

		defer f.Close()

		_, err = f.Write(got.Bytes())
		if err != nil {
			return fmt.Errorf("failed to write golden file: %s, error: %v", goldenFile, err)
		}
	} else {
		hyps := got.String()
		refs := *golden
		if refs != hyps {
			return fmt.Errorf("actual: %v, want: %v", hyps, refs)
		}
	}

	return nil
}

func readGolden(file string) (*string, bool, error) {
	if _, err := os.Stat(file); err == nil {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, true, err
		}

		content := string(data)
		return &content, true, nil
	} else if os.IsNotExist(err) {
		return nil, false, nil
	} else {
		return nil, false, err
	}
}

