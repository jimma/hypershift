package extend

import (
	o "github.com/onsi/gomega"
	"github.com/openshift/hypershift/test/extend/util"
	"strings"
)

type hostedCluster struct {
	oc                           *util.CLI
	namespace                    string
	name                         string
	hostedClustersKubeconfigFile string
}

func newHostedCluster(oc *util.CLI, namespace string, name string) *hostedCluster {
	return &hostedCluster{oc: oc, namespace: namespace, name: name}
}

func (h *hostedCluster) setHostedClusterKubeconfigFile(kubeconfig string) {
	h.hostedClustersKubeconfigFile = kubeconfig
}

func (h *hostedCluster) checkHCConditions() bool {
	iaasPlatform := CheckPlatform(h.oc)
	res, err := h.oc.AsAdmin().WithoutNamespace().Run(OcpGet).Args("hostedcluster", h.name, "-n", h.namespace,
		`-ojsonpath={range .status.conditions[*]}{@.type}{" "}{@.status}{" "}{end}`).Output()
	o.Expect(err).ShouldNot(o.HaveOccurred())

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

func CheckPlatform(oc *util.CLI) string {
	output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("infrastructure", "cluster", "-o=jsonpath={.status.platformStatus.type}").Output()
	return strings.ToLower(output)
}
