package extend

import (
	"context"
	"fmt"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/openshift/hypershift/test/extend/util"
	crcclient "sigs.k8s.io/controller-runtime/pkg/client"
	//"github.com/openshift/origin/test/extended/util/compat_otp"
	//e2e "k8s.io/kubernetes/test/e2e/framework"
	//compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	// "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-hypershift] Hypershift", func() {
	var (
		client                  crcclient.Client
		hostedClusterName       string
		hostedClusterNs         string
		hostedclusterKubeconfig string
	)
	g.BeforeEach(func(ctx context.Context) {
		managementClint, err := util.GetClient()
		if err != nil {
			fmt.Println("Error getting client")
		}
		client = managementClint
		hostedClusterNs, hostedClusterName, hostedclusterKubeconfig = ValidHypershiftAndGetGuestKubeConf(ctx, client)

		operators, err := GetOpeartors(ctx, client)
		if len(operators) <= 0 {
			g.Skip("hypershift operator not found, skip test run")
		}
		hostedclusterPlatform, _ := GetHostedClusterPlatform(ctx, client, hostedClusterNs, hostedClusterName)
		fmt.Printf("HostedCluster platform is: %s", hostedclusterPlatform)

	})
	// author: heli@redhat.com
	g.It("ROSA-OSD_CCS-HyperShiftMGMT-Author:heli-Critical-42855-Check Status Conditions for HostedControlPlane", func(ctx context.Context) {
		client, err := util.GetClient()
		o.Expect(err).NotTo(o.HaveOccurred())
		rc := CheckHCConditions(ctx, client, hostedClusterNs, hostedClusterName)
		o.Expect(rc).Should(o.BeTrue())

		operatorNS, _ := GetHyperShiftOperatorNamespace(ctx, client)
		//e2e.Logf("hosted cluster operator namespace %s", operatorNS)
		o.Expect(operatorNS).NotTo(o.BeEmpty())

		hostedclusterNS, err := GetHyperShiftHostedClusterNamespace(ctx, client)
		o.Expect(err).NotTo(o.HaveOccurred())
		//e2e.Logf("hosted cluster namespace %s", hostedclusterNS)
		o.Expect(hostedclusterNS).NotTo(o.BeEmpty())

		guestClient, err := util.GetClientWithConfig(hostedclusterKubeconfig)
		o.Expect(err).NotTo(o.HaveOccurred())

		cv := GetHostedClusterVersion(ctx, guestClient, hostedClusterNs, hostedClusterName)
		//e2e.Logf("hosted cluster clusterversion name %s", cv)
		fmt.Printf("hosted cluster clusterversion name %s", cv)

		/*
			guestClusterName, guestClusterKube, _ = ValidHypershiftAndGetGuestKubeConfWithNoSkip(oc)
			o.Expect(guestClusterName).NotTo(o.BeEmpty())
			o.Expect(guestClusterKube).NotTo(o.BeEmpty())
			cv, err = oc.AsAdmin().SetGuestKubeconf(guestClusterKube).AsGuestKubeconf().Run("get").Args("clusterversion").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
		*/
		//e2e.Logf("hosted cluster clusterversion with noskip api name %s", cv)
	})
})
