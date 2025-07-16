package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	projectclient "github.com/openshift/client-go/project/clientset/versioned"
	userclient "github.com/openshift/client-go/user/clientset/versioned"
	"github.com/redhat-ai-dev/rosa-namespace-provisioner/pkg/controller"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)

	// Build the Kubernetes client configuration
	var config *rest.Config
	var err error

	// Try to get in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// If not in cluster, try to get kubeconfig
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("HOME") + "/.kube/config"
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			klog.Fatalf("Failed to build config: %v", err)
		}
	}

	// Create the OpenShift user client
	userClient, err := userclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create OpenShift user client: %v", err)
	}

	// Create the OpenShift project client
	projectClient, err := projectclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create OpenShift project client: %v", err)
	}

	// Create and start the controller
	ctrl := controller.NewController(userClient, projectClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		klog.Info("Received shutdown signal")
		cancel()
	}()

	if err := ctrl.Run(ctx); err != nil {
		klog.Fatalf("Controller failed: %v", err)
	}

	klog.Info("Controller shut down gracefully")
}
