package extend

import (
	"context"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/origin/test/extended/util"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-hypershift] Hypershift", func() {
	var (
		oc *exutil.CLI
		//iaasPlatform          string
		hostedcluster         *hostedCluster
		hostedclusterPlatform PlatformType
	)
	if err := compat_otp.InitTest(true); err != nil {
		panic(err)
	}
	e2e.AfterReadingAllFlags(compat_otp.TestContext)
	g.BeforeEach(func(ctx context.Context) {
		oc = exutil.NewCLIForMonitorTest("hypershift")
		//This is a workaround to set the testStarted=true, then it can allow invoking compat_otp.ValidHypershiftAndGetGuestKubeConf(oc)
		exutil.WithCleanup(func() {
			e2e.Logf("This is called by the workaround to set the test to be started")
		})

		hostedClusterName, hostedclusterKubeconfig, hostedClusterNs := compat_otp.ValidHypershiftAndGetGuestKubeConf(oc)
		oc.SetGuestKubeconf(hostedclusterKubeconfig)
		hostedcluster = newHostedCluster(oc, hostedClusterNs, hostedClusterName)
		hostedcluster.setHostedClusterKubeconfigFile(hostedclusterKubeconfig)

		operator := doOcpReq(oc, OcpGet, false, "pods", "-n", "hypershift", "-ojsonpath={.items[*].metadata.name}")
		if len(operator) <= 0 {
			g.Skip("hypershift operator not found, skip test run")
		}

		// get IaaS platform
		//iaasPlatform = compat_otp.ExtendedCheckPlatform(ctx, oc)
		//hypershiftTeamBaseDir = compat_otp.FixturePath("testdata", "hypershift")
		// hosted cluster infra ID
		//hcInfraID = doOcpReq(oc, OcpGet, true, "hc", hostedClusterName, "-n", hostedClusterNs, `-ojsonpath={.spec.infraID}`)

		hostedclusterPlatform = doOcpReq(oc, OcpGet, true, "hostedcluster", "-n", hostedcluster.namespace, hostedcluster.name, "-ojsonpath={.spec.platform.type}")
		e2e.Logf("HostedCluster platform is: %s", hostedclusterPlatform)

		if compat_otp.IsROSA() {
			compat_otp.ROSALogin()
		}

	})
	// author: heli@redhat.com
	g.It("ROSA-OSD_CCS-HyperShiftMGMT-Author:heli-Critical-42855-Check Status Conditions for HostedControlPlane", func() {
		rc := hostedcluster.checkHCConditions()
		o.Expect(rc).Should(o.BeTrue())

		// add more test here to check hypershift util
		operatorNS := compat_otp.GetHyperShiftOperatorNameSpace(oc)
		e2e.Logf("hosted cluster operator namespace %s", operatorNS)
		o.Expect(operatorNS).NotTo(o.BeEmpty())

		hostedclusterNS := compat_otp.GetHyperShiftHostedClusterNameSpace(oc)
		e2e.Logf("hosted cluster namespace %s", hostedclusterNS)
		o.Expect(hostedclusterNS).NotTo(o.BeEmpty())

		guestClusterName, guestClusterKube, _ := compat_otp.ValidHypershiftAndGetGuestKubeConf(oc)
		e2e.Logf("hostedclustercluster name %s", guestClusterName)
		cv, err := oc.AsAdmin().SetGuestKubeconf(guestClusterKube).AsGuestKubeconf().Run("get").Args("clusterversion").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("hosted cluster clusterversion name %s", cv)

		guestClusterName, guestClusterKube, _ = compat_otp.ValidHypershiftAndGetGuestKubeConfWithNoSkip(oc)
		o.Expect(guestClusterName).NotTo(o.BeEmpty())
		o.Expect(guestClusterKube).NotTo(o.BeEmpty())
		cv, err = oc.AsAdmin().SetGuestKubeconf(guestClusterKube).AsGuestKubeconf().Run("get").Args("clusterversion").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("hosted cluster clusterversion with noskip api name %s", cv)

	})
})
