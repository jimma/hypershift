package extend

import (
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/origin/test/extended/util"
	"github.com/openshift/origin/test/extended/util/compat_otp"
)

type hostedCluster struct {
	oc                           *exutil.CLI
	namespace                    string
	name                         string
	hostedClustersKubeconfigFile string
}

func newHostedCluster(oc *exutil.CLI, namespace string, name string) *hostedCluster {
	return &hostedCluster{oc: oc, namespace: namespace, name: name}
}

func (h *hostedCluster) setHostedClusterKubeconfigFile(kubeconfig string) {
	h.hostedClustersKubeconfigFile = kubeconfig
}

func (h *hostedCluster) checkHCConditions() bool {
	iaasPlatform := compat_otp.CheckPlatform(h.oc)
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
