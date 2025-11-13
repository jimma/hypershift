package qe_e2e

import (
	exutil "github.com/openshift/origin/test/extended/util"
)

func getSharedIngressRouterExternalIp(oc *exutil.CLI) string {
	return doOcpReq(oc, OcpGet, true, "svc", "router", "-n", hypershiftSharedingressNamespace,
		"-o=jsonpath={.status.loadBalancer.ingress[0].ip}")
}
