package extend

import (
	"context"
	"fmt"
	configv1 "github.com/openshift/api/config/v1"
	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func CheckHCConditions(ctx context.Context, c crclient.Client, hcnamespace, hcname string) bool {
	iaasPlatform, _ := GetPlatformType(ctx, c)

	hc := &hypershiftv1beta1.HostedCluster{}
	key := types.NamespacedName{
		Name:      hcname,
		Namespace: hcnamespace,
	}

	if err := c.Get(ctx, key, hc); err != nil {
		return false
	}

	var strbuilder strings.Builder

	for _, cond := range hc.Status.Conditions {
		fmt.Fprintf(&strbuilder, "%s %s ", cond.Type, cond.Status)
	}

	var res = strbuilder.String()
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

func checkSubstringWithNoExit(src string, expect []string) bool {
	if expect == nil || len(expect) <= 0 {
		fmt.Printf("Warning expected sub string empty ? %+v", expect)
		return true
	}

	for i := 0; i < len(expect); i++ {
		if !strings.Contains(src, expect[i]) {
			fmt.Printf("expected sub string %s not in src %s", expect[i], src)
			return false
		}
	}

	return true
}

func GetPlatformType(ctx context.Context, client crclient.Client) (string, error) {
	infra := &configv1.Infrastructure{}

	err := client.Get(ctx, crclient.ObjectKey{Name: "cluster"}, infra)
	if err != nil {
		return "", err
	}

	platform := ""
	if infra.Status.PlatformStatus != nil {
		platform = string(infra.Status.PlatformStatus.Type)
	}

	return strings.ToLower(platform), nil
}

func GetHostedClusterPlatform(ctx context.Context, client crclient.Client, namespace, name string) (string, error) {
	hc := &hypershiftv1beta1.HostedCluster{}
	err := client.Get(
		ctx,
		crclient.ObjectKey{
			Namespace: namespace,
			Name:      name,
		},
		hc,
	)
	if err != nil {
		return "", err
	}
	return string(hc.Spec.Platform.Type), nil
}

func GetOpeartors(ctx context.Context, client crclient.Client) ([]string, error) {
	podList := &corev1.PodList{}

	err := client.List(
		ctx,
		podList,
		crclient.InNamespace("hypershift"),
	)
	if err != nil {
		return nil, err
	}

	operators := make([]string, 0, len(podList.Items))
	for _, pod := range podList.Items {
		operators = append(operators, pod.Name)
	}
	return operators, nil

}
