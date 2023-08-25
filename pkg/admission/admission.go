/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package admission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	admission "k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"net/http"
	"kse/kse-rescheduler/pkg"
	"kse/kse-rescheduler/pkg/listfunc"
)

const (
	jsonContentType = `application/json`
)

var (
	k8sdecode       = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
	podResource     = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
)

type RequestsHandler struct {
	K8sClientSet                kubernetes.Interface
}

func NewRequestsHandler() RequestsHandler {
	return RequestsHandler{}
}



func (h *RequestsHandler) handleFunc(w http.ResponseWriter, r *http.Request) {
	review, header, err := h.readAdmissionReview(r)
	if err != nil {
		klog.Infof("failed to parse review: %v\n", err)
		http.Error(w, fmt.Sprintf("failed to parse admission review from request, error=%s", err.Error()), header)
		return
	}
	reviewResponse := admission.AdmissionReview{
		TypeMeta: review.TypeMeta,
		Response: &admission.AdmissionResponse{
			UID: review.Request.UID,
		},
	}

	//klog.Infof("incoming review request=%+v", *review.Request)

	patches, err := h.handleAdmissionReview(review)
	if err != nil {
		klog.Errorf("rejecting request: error=%v, review=%+v\n", err, review.Request.Resource.Resource)
		reviewResponse.Response.Allowed = false
		reviewResponse.Response.Result = &metav1.Status{
			Message: err.Error(),
		}
	} else {
		patchBytes, err := json.Marshal(patches)
		if err != nil {
			klog.Errorf("failed to marshal json patch: %+v, error=%v\n", patches, err)
			http.Error(w, fmt.Sprintf("could not marshal JSON patch: %s", err.Error()), http.StatusInternalServerError)
			return
		}

		reviewResponse.Response.Patch = patchBytes
		reviewResponse.Response.PatchType = new(admission.PatchType)
		*reviewResponse.Response.PatchType = admission.PatchTypeJSONPatch
		reviewResponse.Response.Allowed = true
	}

	//klog.Infof("sending response: allowed=%t, result=%+v, patches=%+v", reviewResponse.Response.Allowed, reviewResponse.Response.Result, patches)

	bytes, err := json.Marshal(&reviewResponse)
	if err != nil {
		klog.Errorf("failed to marshal response review: %+v, error=%v\n", reviewResponse, err)
		http.Error(w, fmt.Sprintf("failed to marshal response review: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	_, err = w.Write(bytes)
	if err != nil {
		klog.Errorf("failed to write response to output http stream: %v\n", err)
		http.Error(w, fmt.Sprintf("failed to write response: %s", err.Error()), http.StatusInternalServerError)
	}

}

func (h *RequestsHandler) readAdmissionReview(r *http.Request) (*admission.AdmissionReview, int, error) {
	if r.Method != http.MethodPost {
		return nil, http.StatusMethodNotAllowed, fmt.Errorf("invalid method %s, only POST requests are allowed", r.Method)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("could not read request body, error=%s", err.Error())
	}

	if contentType := r.Header.Get("Content-Type"); contentType != jsonContentType {
		return nil, http.StatusBadRequest, fmt.Errorf("unsupported content type %s, only %s is supported", contentType, jsonContentType)
	}

	review := &admission.AdmissionReview{}
	if _, _, err := k8sdecode.Decode(body, nil, review); err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("could not deserialize request to review object: %v", err)
	} else if review.Request == nil {
		return nil, http.StatusBadRequest, errors.New("review parsed but request is null")
	}

	return review, http.StatusOK, nil
}

func (h *RequestsHandler) handleAdmissionReview(review *admission.AdmissionReview) (pkg.Patches, error) {
	if review.Request.Operation == admission.Create && review.Request.Namespace != metav1.NamespaceSystem {
		if review.Request.Resource == podResource {
			raw := review.Request.Object.Raw
			pod := corev1.Pod{}
			if _, _, err := k8sdecode.Decode(raw, nil, &pod); err != nil {
				return nil, fmt.Errorf("could not deserialize pod object: %v", err)
			}
			if len(pod.OwnerReferences) > 0 {
				lf := listfunc.NewListFunc()
				lf.K8sClientSet = h.K8sClientSet
				podOwnerInfo, err := lf.GetPodOwnerInfo(&pod)
				if err != nil {
					return nil, nil
				}
				var namespace string
				if len(pod.Namespace) > 0 {
					namespace = pod.Namespace
				} else {
					namespace = "default"
				}
				switch podOwnerInfo.PodOwnerType {
				case "Deployment":
					deploy, err := h.K8sClientSet.AppsV1().Deployments(namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
					if err != nil {
						return nil, fmt.Errorf("get pod %s deployment error", pod.Name)
					}
					if _, ok := deploy.Annotations[pkg.SchedulingRetrieString]; ok {
						patches, err := h.createDeployPatches(&pod, deploy)
						if err != nil {
							return nil, err
						}
						return patches, nil
					}
				case "ReplicaSet":
					rs, err := h.K8sClientSet.AppsV1().ReplicaSets(namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
					if err != nil {
						return nil, fmt.Errorf("get pod %s replicasets error", pod.Name)
					}
					if _, ok := rs.Annotations[pkg.SchedulingRetrieString]; ok {
						patches, err := h.createRcPatches(&pod, rs)
						if err != nil {
							return nil, err
						}
						return patches, nil
					}
				case "CronJob":
					cj, err := h.K8sClientSet.BatchV1().CronJobs(namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
					if err != nil {
						return nil, fmt.Errorf("get pod %s cronjob error", pod.Name)
					}
					if _, ok := cj.Annotations[pkg.SchedulingRetrieString]; ok {
						patches, err := h.createCjPatches(&pod, cj)
						if err != nil {
							return nil, err
						}
						return patches, nil
					}
				case "Job":
					jb, err := h.K8sClientSet.BatchV1().Jobs(namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
					if err != nil {
						return nil, fmt.Errorf("get pod %s job error", pod.Name)
					}
					if _, ok := jb.Annotations[pkg.SchedulingRetrieString]; ok {
						patches, err := h.createJobPatches(&pod, jb)
						if err != nil {
							return nil, err
						}
						return patches, nil
					}
				case "StatefulSet":
					sts, err := h.K8sClientSet.AppsV1().StatefulSets(namespace).Get(context.TODO(), podOwnerInfo.PodOwnerName, metav1.GetOptions{})
					if err != nil {
						return nil, fmt.Errorf("get pod %s statefulset error", pod.Name)
					}

					if _, ok := sts.Annotations[pkg.SchedulingRetrieString]; ok {
						patches, err := h.createStsPatches(&pod, sts)
						if err != nil {
							return nil, err
						}
						return patches, nil
					}
				}
			}
		}
	}
	return nil, nil
}

func (h *RequestsHandler) createDeployPatches(pod *corev1.Pod, deploy *appsv1.Deployment) (pkg.Patches, error) {
	var patches pkg.Patches
	var deployInfo pkg.DeployInfo
	if _, ok := deploy.Annotations[pkg.DeployInfoString]; ok {
		if err := json.Unmarshal([]byte(deploy.Annotations[pkg.DeployInfoString]), &deployInfo); err != nil {
			return nil, fmt.Errorf("unmarshal %s kse.com/deploy err: %s", pod.Name, err.Error())
		}
		if len(deployInfo.DeployScheduledHosts) > 0 {
			byteScheduledHost, err := json.Marshal(deployInfo.DeployScheduledHosts)
			if err != nil {
				return nil, fmt.Errorf("marshal %s scheduled hosts from deployment %s err: %s", pod.Name, deploy.Name, err.Error())
			}
			patches = append(patches, pkg.Patch{
				Op: "add",
				Path: "/metadata/annotations",
				Value: map[string]string{pkg.SchedulinedHostString: string(byteScheduledHost)},
			})
			return patches, nil
		}
	}
	return nil, nil
}

func (h *RequestsHandler) createRcPatches(pod *corev1.Pod, rs *appsv1.ReplicaSet) (pkg.Patches, error) {
	var patches pkg.Patches
	var rsInfo pkg.RsInfo
	if _, ok := rs.Annotations[pkg.RsInfoString]; ok {
		if err := json.Unmarshal([]byte(rs.Annotations[pkg.RsInfoString]), &rsInfo); err != nil {
			return nil, fmt.Errorf("unmarshal %s kse.com/rs err: %s", pod.Name, err.Error())
		}
		if len(rsInfo.RsScheduledHosts) > 0 {
			byteScheduledHost, err := json.Marshal(rsInfo.RsScheduledHosts)
			if err != nil {
				return nil, fmt.Errorf("marshal %s scheduled hosts from replicaset %s err: %s", pod.Name, rs.Name, err.Error())
			}
			patches = append(patches, pkg.Patch{
				Op: "add",
				Path: "/metadata/annotations",
				Value: map[string]string{pkg.SchedulinedHostString: string(byteScheduledHost)},
			})
			return patches, nil
		}
	}
	return nil, nil
}

func (h *RequestsHandler) createCjPatches(pod *corev1.Pod, cj *batchv1.CronJob) (pkg.Patches, error) {
	var patches pkg.Patches
	var cjInfo pkg.CjInfo
	if _, ok := cj.Annotations[pkg.CjInfoString]; ok {
		if err := json.Unmarshal([]byte(cj.Annotations[pkg.CjInfoString]), &cjInfo); err != nil {
			return nil, fmt.Errorf("unmarshal %s kse.com/cj err: %s", pod.Name, err.Error())
		}
		if len(cjInfo.CjScheduledHosts) > 0 {
			byteScheduledHost, err := json.Marshal(cjInfo.CjScheduledHosts)
			if err != nil {
				return nil, fmt.Errorf("marshal %s scheduled hosts from cronjob %s err: %s", pod.Name, cj.Name, err.Error())
			}
			patches = append(patches, pkg.Patch{
				Op: "add",
				Path: "/metadata/annotations",
				Value: map[string]string{pkg.SchedulinedHostString: string(byteScheduledHost)},
			})
			return patches, nil
		}
	}
	return nil, nil
}

func (h *RequestsHandler) createJobPatches(pod *corev1.Pod, jb *batchv1.Job) (pkg.Patches, error) {
	var patches pkg.Patches
	var jbInfo pkg.JobInfo
	if _, ok := jb.Annotations[pkg.JobInfoString]; ok {
		if err := json.Unmarshal([]byte(jb.Annotations[pkg.JobInfoString]), &jbInfo); err != nil {
			return nil, fmt.Errorf("unmarshal %s kse.com/job err: %s", pod.Name, err.Error())
		}
		if len(jbInfo.JobScheduledHosts) > 0 {
			byteScheduledHost, err := json.Marshal(jbInfo.JobScheduledHosts)
			if err != nil {
				return nil, fmt.Errorf("marshal %s scheduled hosts from job %s err: %s", pod.Name, jb.Name, err.Error())
			}
			patches = append(patches, pkg.Patch{
				Op: "add",
				Path: "/metadata/annotations",
				Value: map[string]string{pkg.SchedulinedHostString: string(byteScheduledHost)},
			})
			return patches, nil
		}
	}
	return nil, nil
}

func (h *RequestsHandler) createStsPatches(pod *corev1.Pod, sts *appsv1.StatefulSet) (pkg.Patches, error) {
	var patches pkg.Patches
	var stsPodMap pkg.StsPodsMap
	podName := pod.Name
	if _, ok := sts.Annotations[pkg.StsPodMapString]; ok {
		if err := json.Unmarshal([]byte(sts.Annotations[pkg.StsPodMapString]), &stsPodMap); err != nil {
			return nil, fmt.Errorf("unmarshal %s kse.com/sts-pods-map err: %s", podName, err.Error())
		}
		if _, ok := stsPodMap[podName]; ok {
			if len(stsPodMap[podName].PodScheduledHosts) > 0 {
				byteScheduledHost, err := json.Marshal(stsPodMap[podName].PodScheduledHosts)
				if err != nil {
					return nil, fmt.Errorf("marshal %s scheduled hosts from statefulset %s err: %s", podName, sts.Name, err.Error())
				}
				patches = append(patches, pkg.Patch{
					Op: "add",
					Path: "/metadata/annotations",
					Value: map[string]string{pkg.SchedulinedHostString: string(byteScheduledHost)},
				})
				return patches, nil
			}
		}
	}
	return nil, nil
}