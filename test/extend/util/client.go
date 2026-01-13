package util

import (
	"fmt"
	configv1 "github.com/openshift/api/config/v1"
	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cr "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

func buildRestConfig(kubeconfigPath string) (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}

func buildScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core scheme: %w", err)
	}
	if err := configv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add configv1 scheme: %w", err)
	}
	if err := hypershiftv1beta1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add hypershift scheme: %w", err)
	}
	return scheme, nil
}

func buildClient(config *rest.Config) (crclient.Client, error) {
	scheme, err := buildScheme()
	if err != nil {
		return nil, err
	}
	config.QPS = 200
	config.Burst = 300
	config.Timeout = 5 * time.Minute

	k8sClient, err := crclient.New(config, crclient.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}

func GetClient() (crclient.Client, error) {
	config, err := cr.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to get kubernetes config: %w", err)
	}
	return buildClient(config)
}

func GetClientWithConfig(guestKubeconfigFile string) (crclient.Client, error) {
	config, err := buildRestConfig(guestKubeconfigFile)
	if err != nil {
		return nil, fmt.Errorf("unable to get kubernetes config: %w", err)
	}
	return buildClient(config)
}
