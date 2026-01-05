package extend

import (
	"context"
	"fmt"
	o "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"github.com/openshift/hypershift/test/extend/util"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func checkHCConditions(ctx context.Context, c crclient.Client, hcname, hcnamespace string) bool {
	iaasPlatform, _ := GetPlatformType(ctx, c)

	hc := &hypershiftv1beta1.HostedCluster{}
	key := types.NamespacedName{
		Name:      hcname,
		Namespace: hcnamespace,
	}

	if err := c.Get(ctx, key, hc); err != nil {
		return false
	}

	var res []string
	for _, cond := range hc.Status.Conditions {
		res = append(res, fmt.Sprintf("%s %s", cond.Type, cond.Status))
	}

	if iaasPlatform == "azure" {
		return checkSubstringWithNoExit(res,
			[]string{"ValidHostedControlPlaneConfiguration True", "ClusterVersionSucceeding True",
				"Degraded False", "EtcdAvailable True", "KubeAPIServerAvailable True", "InfrastructureReady True",
				"Available True", "ValidConfiguration True", "SupportedHostedCluster True",
				"ValidHostedControlPlaneConfiguration True", "IgnitionEndpointAvailable True", "ReconciliationActive True",
				"ValidReleaseImage True", "ReconciliationSucceeded True"})
	} else {
		return checkSubstringWithNoExit(res,
			[]string{"ValidHostedControlPlaneConfiguration True", "ClusterVersionSucceeding True",
				"Degraded False", "EtcdAvailable True", "KubeAPIServerAvailable True", "InfrastructureReady True",
				"Available True", "ValidConfiguration True", "SupportedHostedCluster True",
				"ValidHostedControlPlaneConfiguration True", "IgnitionEndpointAvailable True", "ReconciliationActive True",
				"ValidReleaseImage True", "ReconciliationSucceeded True"})
	}
}

func GetPlatformType(ctx context.Context, client crclient.Client) (string, error) {
	infra := &configv1.Infrastructure{}

	// "cluster" is the fixed name of the Infrastructure resource on OpenShift
	err := client.Get(ctx, &client.ListOption{Name: "cluster"}, infra)
	if err != nil {
		return "", err
	}

	platform := ""
	if infra.Status.PlatformStatus != nil {
		platform = string(infra.Status.PlatformStatus.Type)
	}

	return strings.ToLower(platform), nil
}
