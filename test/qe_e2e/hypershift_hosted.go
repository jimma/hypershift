package qe_e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-hypershift] Hypershift", func() {
	defer g.GinkgoRecover()

	var (
		oc = compat_otp.NewCLI("hypershift-hosted", compat_otp.KubeConfigPath())
	)

	g.BeforeEach(func() {
		if !compat_otp.IsHypershiftHostedCluster(oc) {
			g.Skip("not a hosted cluster, skip the test case")
		}
	})

	// author: heli@redhat.com
	g.It("NonPreRelease-PreChkUpgrade-PstChkUpgrade-Author:heli-Critical-66831-HCP to support mgmt on 4.13 non-OVNIC and hosted on 4.14 OVN-IC and mgmt to upgrade to 4.14", func() {
		version := doOcpReq(oc, OcpGet, true, "clusterversion", "version", `-ojsonpath={.status.desired.version}`)
		g.By(fmt.Sprintf("check hosted cluster version: %s", version))

		ovnLeaseHolder := doOcpReq(oc, OcpGet, true, "lease", "ovn-kubernetes-master", "-n", "openshift-ovn-kubernetes", `-ojsonpath={.spec.holderIdentity}`)
		g.By(fmt.Sprintf("check hosted cluster ovn lease holder: %s", ovnLeaseHolder))

		if strings.Contains(version, "4.13") {
			// currently we only check aws 4.13
			if strings.ToLower(compat_otp.CheckPlatform(oc)) == "aws" {
				o.Expect(ovnLeaseHolder).Should(o.ContainSubstring("compute.internal"))
			}
		}

		if strings.Contains(version, "4.14") {
			o.Expect(ovnLeaseHolder).Should(o.ContainSubstring("ovnkube-control-plane"))
		}

	})

	//author: wewang@redhat.com
	g.It("Author:wewang-High-83958-Make sure internalJoinSubnet and internalTransitSwitchSubnet works", func() {
		var (
			buildPruningBaseDir    = compat_otp.FixturePath("testdata", "networking")
			pingPodNodeTemplate    = filepath.Join(buildPruningBaseDir, "ping-for-pod-specific-node-template.yaml")
			genericServiceTemplate = filepath.Join(buildPruningBaseDir, "service-generic-template.yaml")
		)
		currentinternalJoinSubnetIPv4Value, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("Network.operator.openshift.io/cluster", "-o=jsonpath={.spec.defaultNetwork.ovnKubernetesConfig.ipv4.internalJoinSubnet}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if currentinternalJoinSubnetIPv4Value != "100.99.0.0/16" {
			g.Skip("no custom internalJoinSubnet configured, skip it!")
		}
		currentinternalTransitSwSubnetIPv4Value, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("Network.operator.openshift.io/cluster", "-o=jsonpath={.spec.defaultNetwork.ovnKubernetesConfig.ipv4.internalTransitSwitchSubnet}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if currentinternalTransitSwSubnetIPv4Value != "100.69.0.0/16" {
			g.Skip("no custom internalTransitSwitchSubnet configured, skip it!")
		}
		ipStackType := checkIPStackType(oc)
		o.Expect(ipStackType).NotTo(o.BeEmpty())

		nodeList, err := e2enode.GetReadySchedulableNodes(context.TODO(), oc.KubeFramework().ClientSet)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(nodeList.Items) < 2 {
			g.Skip("This case requires 2 nodes, but the cluster has less than two nodes")
		}
		compat_otp.By("create a hello pod1 in namespace")
		pod1ns := pingPodResourceNode{
			name:      "hello-pod1",
			namespace: oc.Namespace(),
			nodename:  nodeList.Items[0].Name,
			template:  pingPodNodeTemplate,
		}
		pod1ns.createPingPodNode(oc)
		waitPodReady(oc, oc.Namespace(), pod1ns.name)

		compat_otp.By("create a hello-pod2 in namespace")
		pod2ns := pingPodResourceNode{
			name:      "hello-pod2",
			namespace: oc.Namespace(),
			nodename:  nodeList.Items[1].Name,
			template:  pingPodNodeTemplate,
		}
		pod2ns.createPingPodNode(oc)
		waitPodReady(oc, oc.Namespace(), pod2ns.name)

		g.By("Create a test service backing up both the above pods")
		svc := genericServiceResource{
			servicename:           "test-service-83958",
			namespace:             oc.Namespace(),
			protocol:              "TCP",
			selector:              "hello-pod",
			serviceType:           "ClusterIP",
			ipFamilyPolicy:        "",
			internalTrafficPolicy: "Cluster",
			externalTrafficPolicy: "",
			template:              genericServiceTemplate,
		}

		if ipStackType == "ipv4single" {
			svc.ipFamilyPolicy = "SingleStack"
		} else {
			svc.ipFamilyPolicy = "PreferDualStack"
		}
		svc.createServiceFromParams(oc)
		curlPod2PodPass(oc, oc.Namespace(), pod1ns.name, oc.Namespace(), pod2ns.name)
		curlPod2SvcPass(oc, oc.Namespace(), oc.Namespace(), pod1ns.name, "test-service-83958")
	})
})
