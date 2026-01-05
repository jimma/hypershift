package util

import (
	"fmt"
	cr "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

func GetClient() (crclient.Client, error) {
	config, err := cr.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("Unable to get kuberentes config: %w", err)
	}
	config.QPS = 200
	config.Burst = 300
	config.Timeout = 5 * time.Minute
	client, err := crclient.New(config, crclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("Unable to create kubernetes client: %w", err)
	}
	return client, nil
}
