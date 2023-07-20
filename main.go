package main

import (
	"context"
	"fmt"
	"k8s.io/client-go/rest"
	"os"
	"os/signal"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
)

type SideCar struct {
	ctx       context.Context
	client    *clientset.Clientset
	lock      *resourcelock.LeaseLock
	namespace string
	name      string
	key       string
	active    bool
}

func (s *SideCar) markActive() {
	pod, err := s.client.CoreV1().Pods(s.namespace).Get(s.ctx, s.name, metav1.GetOptions{})
	if err == nil {
		if _, exists := pod.GetLabels()[s.key]; !exists {
			labels := pod.GetLabels()
			labels[s.key] = "active"
			pod.SetLabels(labels)
			if _, err := s.client.CoreV1().Pods(s.namespace).Update(s.ctx, pod, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("error updating pod labels: %s", err)
			} else {
				s.active = true
			}
		}
	} else {
		klog.Fatalf("failed to list pods: %s", err)
	}
}

func (s *SideCar) markPassive() {
	if s.active {
		pod, err := s.client.CoreV1().Pods(s.namespace).Get(s.ctx, s.name, metav1.GetOptions{})
		if err == nil {
			if _, exists := pod.GetLabels()[s.key]; exists {
				labels := pod.GetLabels()
				delete(labels, s.key)
				pod.SetLabels(labels)
				if _, err := s.client.CoreV1().Pods(s.namespace).Update(s.ctx, pod, metav1.UpdateOptions{}); err != nil {
					klog.Errorf("error updating pod labels: %s", err)
				} else {
					s.active = false
				}
			}
		} else {
			klog.Fatalf("failed to list pods: %s", err)
		}
	}
}

func (s *SideCar) runLeaderElection() {
	if s.lock == nil {
		klog.Fatal("lock not initialized")
	}
	leaderelection.RunOrDie(s.ctx, leaderelection.LeaderElectionConfig{
		Lock:            s.lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				klog.Info("we are the leader, marking active")
				s.markActive()
			},
			OnStoppedLeading: func() {
				klog.Info("no longer the leader, marking inactive.")
				s.markPassive()
			},
		},
	})
}

func (s *SideCar) generateLock() {
	pod, err := s.client.CoreV1().Pods(s.namespace).Get(s.ctx, s.name, metav1.GetOptions{})
	if err != nil {
		klog.Fatalf("failed to list pods: %s", err)
	}

	s.lock = &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s",
				pod.GetObjectMeta().GetLabels()["app.kubernetes.io/name"],
				pod.GetObjectMeta().GetLabels()["app.kubernetes.io/component"]),
			Namespace: s.namespace,
		},
		Client: s.client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: s.name,
		},
	}
}

func newSideCar(ctx context.Context, client *clientset.Clientset) *SideCar {
	namespace := os.Getenv("POD_NAMESPACE")
	name := os.Getenv("POD_NAME")
	if namespace == "" {
		klog.Fatal("missing POD_NAMESPACE env var")
	}
	if name == "" {
		klog.Fatal("missing POD_NAME env var")
	}
	key := getEnvOrDefault("LABEL_KEY", "apsc.knoppiks.de/state")
	return &SideCar{
		namespace: namespace,
		name:      name,
		key:       key,
		client:    client,
		ctx:       ctx,
		active:    false,
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func main() {
	var client *clientset.Clientset
	if kubecfg, err := rest.InClusterConfig(); err == nil {
		client = clientset.NewForConfigOrDie(kubecfg)
	} else {
		klog.Fatalf("failed to get kubecfg: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sideCar := newSideCar(ctx, client)

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL)
	defer func() {
		signal.Stop(c)
		cancel()
	}()

	go func() {
		<-c
		klog.Info("shutdown after signal")
		sideCar.markPassive()
		cancel()
	}()

	sideCar.generateLock()
	sideCar.runLeaderElection()
}
