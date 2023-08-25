/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package admission

import (
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/apimachinery/pkg/util/uuid"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	"net/http"
	"os"
	"kse/kse-rescheduler/pkg/listfunc"
	"kse/kse-rescheduler/pkg/version"
	"kse/kse-rescheduler/pkg"
	"time"
	"fmt"
	"context"
)

type Server struct {
	TLSCertFile string
	TLSKeyFile  string
	Address     string
	ListFuncPeriod      time.Duration
	Handler     RequestsHandler
	ListFunc    listfunc.ListFunc
	KubeConfig  *restclient.Config
}

func NewKseReschedulerServer() *Server {
	return &Server{
		TLSCertFile:          "/run/secrets/tls/tls.crt",
		TLSKeyFile:            "/run/secrets/tls/tls.key",
		Address:               ":8443",
		ListFuncPeriod:        30 * time.Second,
		Handler:               NewRequestsHandler(),
		ListFunc:              listfunc.NewListFunc(),
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) InitializeK8sClientSet(kubeconfigPath string) error {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return err
	}
	k8sClientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	s.KubeConfig = config
	s.Handler.K8sClientSet = k8sClientSet
	s.ListFunc.K8sClientSet = k8sClientSet
	return nil
}

func (s *Server) Start(kubeconfigPath string) error {
	klog.Info(version.DisplayVersion())
	if err := s.InitializeK8sClientSet(kubeconfigPath); err != nil {
		return err
	}
	leaderElectionConfig, id, err := makeLeaderElectionConfig(s.KubeConfig)
	if err != nil {
		return err
	}
	leaderElectionConfig.Callbacks = leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			s.RunListFunc()
		},
		OnStoppedLeading: func() {
			klog.Info("no longer the leader, staying inactive")
		},
		OnNewLeader:      func(currentId string) {
			if currentId == id {
				klog.Info("still the leader!")
				return
			}
			klog.Info("new leader is: ", currentId)
		},
	}
	leaderElector, err := leaderelection.NewLeaderElector(*leaderElectionConfig)
	if err != nil {
		return fmt.Errorf("couldn't create leader elector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go leaderElector.Run(ctx)

	klog.Infof("Listening on %s\n", s.Address)
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.Handler.handleFunc)
	mux.HandleFunc("/health", s.health)

	server := &http.Server{
		Addr:    s.Address,
		Handler: mux,
	}

	return server.ListenAndServeTLS(s.TLSCertFile, s.TLSKeyFile)
}

func (s *Server) RunListFunc() {
	stopCh := make(chan struct{})
	defer close(stopCh)
	klog.Infof("Starting listFunc and it's list period is %v\n", s.ListFuncPeriod)
	wait.Until(s.ListFunc.List, s.ListFuncPeriod, stopCh)
}

func makeLeaderElectionConfig(kubeConfig *restclient.Config) (*leaderelection.LeaderElectionConfig, string, error) {
	var namespace, id string
	if namespace = os.Getenv("POD_NAMESPACE"); namespace == "" {
		namespace = pkg.NAMESPACE
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, id, fmt.Errorf("unable to get hostname: %v", err)
	}
	if id = os.Getenv("POD_NAME"); id == "" {
		// add a uniquifier so that two processes on the same host don't accidentally both become active
		id = hostname + "_" + string(uuid.NewUUID())
	}

	rl, err := resourcelock.NewFromKubeconfig(
		"leases",
		namespace,
		"kse-rescheduler",
		resourcelock.ResourceLockConfig{
			Identity: id,
		},
		kubeConfig, pkg.RenewDeadlineDuration)
	if err != nil {
		return nil, id, fmt.Errorf("couldn't create resource lock: %v", err)
	}
	return &leaderelection.LeaderElectionConfig{
		Lock:            rl,
		LeaseDuration:   pkg.LeaseDuration,
		RenewDeadline:   pkg.RenewDeadlineDuration,
		RetryPeriod:     pkg.RetryPeriod,
		WatchDog:        leaderelection.NewLeaderHealthzAdaptor(time.Second * 20),
		Name:            "kse-rescheduler",
		ReleaseOnCancel: true,
	}, id, nil
}
