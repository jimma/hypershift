package qe_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"

	"k8s.io/apimachinery/pkg/api/resource"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-hypershift] Hypershift", func() {
	defer g.GinkgoRecover()
	if err := compat_otp.InitTest(false); err != nil {
		panic(err)
	}
	e2e.AfterReadingAllFlags(compat_otp.TestContext)
	var (
		oc                                  = compat_otp.NewCLIForKubeOpenShift("hypershift")
		iaasPlatform, hypershiftTeamBaseDir string
		hostedcluster                       *hostedCluster
		hostedclusterPlatform               PlatformType
	)

	g.BeforeEach(func(ctx context.Context) {
		hostedClusterName, hostedclusterKubeconfig, hostedClusterNs := compat_otp.ValidHypershiftAndGetGuestKubeConf(oc)
		oc.SetGuestKubeconf(hostedclusterKubeconfig)
		hostedcluster = newHostedCluster(oc, hostedClusterNs, hostedClusterName)
		hostedcluster.setHostedClusterKubeconfigFile(hostedclusterKubeconfig)

		operator := doOcpReq(oc, OcpGet, false, "pods", "-n", "hypershift", "-ojsonpath={.items[*].metadata.name}")
		if len(operator) <= 0 {
			g.Skip("hypershift operator not found, skip test run")
		}

		// get IaaS platform
		iaasPlatform = compat_otp.ExtendedCheckPlatform(ctx, oc)
		hypershiftTeamBaseDir = compat_otp.FixturePath("testdata", "hypershift")

		hostedclusterPlatform = doOcpReq(oc, OcpGet, true, "hostedcluster", "-n", hostedcluster.namespace, hostedcluster.name, "-ojsonpath={.spec.platform.type}")
		e2e.Logf("HostedCluster platform is: %s", hostedclusterPlatform)

	})

	// author: lgao@redhat.com
	// Test run duration: about 25 mins
	g.FIt("Hypershift-HyperShiftMGMT-Longduration-NonPreRelease-Author:lgao-Critical-82716-Specifying cluster-autoscaler flags to the hosted cluster with scale down [Serial] [Disruptive]", func() {
		if iaasPlatform != "aws" && iaasPlatform != "azure" {
			g.Skip("Skip due to incompatible platform")
		}
		var (
			npCount            = 1
			npName             = "lgao-82716-test-01"
			autoScalingMax     = 3
			autoScalingMin     = 1
			workloadTemplate   = filepath.Join(hypershiftTeamBaseDir, "workload.yaml")
			parsedWorkloadFile = "ocp-82716-workload-template.config"
		)

		compat_otp.By("Step 1: Create a nodepool for the test")
		defer func() {
			hostedcluster.deleteNodePool(npName)
			o.Eventually(hostedcluster.pollCheckAllNodepoolReady(), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "in defer check all nodes ready error")
		}()
		switch iaasPlatform {
		case "aws":
			hostedcluster.createAwsNodePool(npName, npCount)
		case "azure":
			hostedcluster.createAdditionalAzureNodePool(npName, npCount)
		}
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName)).Should(o.BeFalse())

		compat_otp.By(fmt.Sprintf("Step 2: Enable ClusterAutoScaling for hosted cluster %s with SacleDown enabled", hostedcluster.name))
		defer hostedcluster.disableClusterAutoScale(hostedcluster.name)
		// wait for 60 seconds before scaling down
		autoScalingJson := `{
		"scaling": "ScaleUpAndScaleDown",
			"maxPodGracePeriod": 60,
			"scaleDown": {
				"delayAfterAddSeconds": 60,
				"delayAfterDeleteSeconds": 60,
				"delayAfterFailureSeconds": 60,
				"unneededDurationSeconds": 60,
				"utilizationThresholdPercent": 50
			}
		}`
		hostedcluster.setClusterAutoScale(hostedcluster.name, autoScalingJson)

		compat_otp.By("Step 3: Enable the nodepool autoscaling")
		defer hostedcluster.disableNodePoolAutoScale(npName)
		hostedcluster.setNodepoolAutoScale(npName, strconv.Itoa(autoScalingMax), strconv.Itoa(autoScalingMin))
		doOcpReq(oc, OcpPatch, true, "np", npName, "-n", hostedcluster.namespace, "--type", "merge", "-p", fmt.Sprintf(`{"spec":{"nodeDrainTimeout": "%s"}}`, "5m"))
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready after setting autoscaling error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName)).Should(o.BeTrue())

		hostedcluster.validateClusterAutoScallerDeploymentRunning()

		// NOTE: what I observe: All nodes in the nodepool will be deleted when enabling node autoscaling

		compat_otp.By("Step 4: Create workload to trigger scaling up")
		workLoad := workload{
			name:      "workload",
			namespace: "default",
			template:  workloadTemplate,
		}
		workLoad.create(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile, "--local")

		// check the nodes count
		compat_otp.By(fmt.Sprintf("Step 5: Check that the nodes in the hosted cluster is auto-scaled to %d", autoScalingMax))
		o.Eventually(hostedcluster.pollGetHostedClusterReadyNodeCount(npName), DoubleLongTimeout, DoubleLongTimeout/10).Should(o.BeEquivalentTo(autoScalingMax), "nodepool autoscaling max error")

		// delete job workload
		compat_otp.By("Step 6: Delete the workload to trigger the scaling down")
		workLoad.delete(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile)

		// check nodes count, it should scale down to the 1 specified in the autoScalingMin
		compat_otp.By("Step 7: Check that the nodes in the hosted cluster is auto-scaled-down to 1")
		o.Eventually(hostedcluster.pollGetHostedClusterReadyNodeCount(npName), DoubleLongTimeout, DoubleLongTimeout/10).Should(o.BeEquivalentTo(autoScalingMin), "nodepool scaling down 1 error")

		compat_otp.By("Step 8: Well Done, Feature with scaling up and down gets passed!")
	})

	// author: lgao@redhat.com
	// Test run duration: about 25 mins
	g.FIt("Hypershift-HyperShiftMGMT-Longduration-NonPreRelease-Author:lgao-Critical-83127-Specifying cluster-autoscaler flags to the hosted cluster without scale down [Serial] [Disruptive]", func() {
		if iaasPlatform != "aws" && iaasPlatform != "azure" {
			g.Skip("Skip due to incompatible platform")
		}
		var (
			npCount            = 1
			npName             = "lgao-83127-test-01"
			autoScalingMax     = 3
			autoScalingMin     = 1
			workloadTemplate   = filepath.Join(hypershiftTeamBaseDir, "workload.yaml")
			parsedWorkloadFile = "ocp-83127-workload-template.config"
		)

		compat_otp.By("Step 1: Create a nodepool for the test")
		defer func() {
			hostedcluster.deleteNodePool(npName)
			o.Eventually(hostedcluster.pollCheckAllNodepoolReady(), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "in defer check all nodes ready error")
		}()
		switch iaasPlatform {
		case "aws":
			hostedcluster.createAwsNodePool(npName, npCount)
		case "azure":
			hostedcluster.createAdditionalAzureNodePool(npName, npCount)
		}
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName)).Should(o.BeFalse())

		compat_otp.By(fmt.Sprintf("Step 2: Enable ClusterAutoScaling for hosted cluster %s with SacleDown Disabled", hostedcluster.name))
		defer hostedcluster.disableClusterAutoScale(hostedcluster.name)
		// Sets ScaleUpOnly but also set maxPodGracePeriod to 60 seconds which would trigger the scaling down if enabled.
		autoScalingJson := `{
			"scaling": "ScaleUpOnly",
			"maxPodGracePeriod": 60
		}`
		hostedcluster.setClusterAutoScale(hostedcluster.name, autoScalingJson)

		compat_otp.By("Step 3: Enable the nodepool autoscaling")
		defer hostedcluster.disableNodePoolAutoScale(npName)
		hostedcluster.setNodepoolAutoScale(npName, strconv.Itoa(autoScalingMax), strconv.Itoa(autoScalingMin))
		doOcpReq(oc, OcpPatch, true, "np", npName, "-n", hostedcluster.namespace, "--type", "merge", "-p", fmt.Sprintf(`{"spec":{"nodeDrainTimeout": "%s"}}`, "5m"))
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready after setting autoscaling error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName)).Should(o.BeTrue())

		hostedcluster.validateClusterAutoScallerDeploymentRunning()

		// NOTE: what I observe: All nodes in the nodepool will be deleted when enabling node autoscaling

		compat_otp.By("Step 4: Create workload to trigger scaling up")
		workLoad := workload{
			name:      "workload",
			namespace: "default",
			template:  workloadTemplate,
		}
		workLoad.create(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile, "--local")

		// check the nodes count
		compat_otp.By(fmt.Sprintf("Step 5: Check that the nodes in the hosted cluster is auto-scaled to %d", autoScalingMax))
		o.Eventually(hostedcluster.pollGetHostedClusterReadyNodeCount(npName), DoubleLongTimeout, DoubleLongTimeout/10).Should(o.BeEquivalentTo(autoScalingMax), "nodepool autoscaling max error")

		// delete job workload
		compat_otp.By("Step 6: Delete the workload, but it shouldn't lead to scaling down because it is disabled")
		workLoad.delete(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile)

		// check nodes count, it should keeps the same as the autoScalingMax after scaling up
		// we choose to wait for at most 5 minutes that the nodes do not have '.spec.unschedulable'
		compat_otp.By("Step7: Check that the nodes in the hosted cluster which don't have .spec.unschedulale within 5 minutes")
		// check for 5 minutes on each 10 seconds, fail if any find
		unschedulable := func() int {
			// list number of nodes with '.spec.unschedulable: true'
			cnt, err := hostedcluster.getHostedClusterUnschedulableNodeCount(npName)
			o.Expect(err).NotTo(o.HaveOccurred())
			return cnt
		}
		o.Consistently(unschedulable, 5*time.Minute, 10*time.Second).Should(o.BeEquivalentTo(0))

		compat_otp.By(fmt.Sprintf("Step7.1: The nodes keep be %d", autoScalingMax))
		o.Expect(hostedcluster.getHostedClusterReadyNodeCount(npName)).Should(o.BeEquivalentTo(autoScalingMax))

		compat_otp.By("Step 8: Well Done, feature with only scaling up without scaling down gets passed!")
	})

	// author: lgao@redhat.com
	// Test run duration: about 25 mins
	g.FIt("Hypershift-HyperShiftMGMT-Longduration-NonPreRelease-Author:lgao-Critical-83145-Specifying cluster-autoscaler flags to the hosted cluster with priority expander [Serial] [Disruptive]", func() {
		if iaasPlatform != "aws" && iaasPlatform != "azure" {
			g.Skip("Skip due to incompatible platform")
		}
		var (
			npName1            = "lgao-83145-test-01"
			npName2            = "lgao-83145-test-02"
			autoScalingMax     = 3
			autoScalingMin     = 1
			workloadTemplate   = filepath.Join(hypershiftTeamBaseDir, "workload.yaml")
			parsedWorkloadFile = "ocp-83145-workload-template.config"
		)
		compat_otp.By("Step 1: Create 2 NodePools for the test")
		defer func() {
			hostedcluster.deleteNodePool(npName1)
			hostedcluster.deleteNodePool(npName2)
			o.Eventually(hostedcluster.pollCheckAllNodepoolReady(), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "in defer check all nodes ready error")
		}()
		switch iaasPlatform {
		case "aws":
			hostedcluster.createAwsNodePool(npName1, 1)
			hostedcluster.createAwsNodePool(npName2, 1)
		case "azure":
			hostedcluster.createAdditionalAzureNodePool(npName1, 1)
			hostedcluster.createAdditionalAzureNodePool(npName2, 1)
		}

		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName1), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName1)).Should(o.BeFalse())

		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName2), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName2)).Should(o.BeFalse())
		controlplaneNS := hostedcluster.namespace + "-" + hostedcluster.name

		// node pool 2 has higher priority to expand on the autoscaling up
		compat_otp.By("Step 2:  Create a configmap called cluster-autoscaler-priority-expander in the hosted cluster")
		cmName := "cluster-autoscaler-priority-expander"
		cmNameSpace := "kube-system"
		cmData := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  priorities: |-
    10:
      - ".*%s.*"
    100:
      - ".*%s.*"`,
			cmName, cmNameSpace, npName1, npName2)
		defer doOcpReq(oc, OcpDelete, true, "ConfigMap", cmName, "-n", cmNameSpace, "--kubeconfig", hostedcluster.hostedClustersKubeconfigFile)
		createOCPResourceByStr(oc, cmData, "--kubeconfig", hostedcluster.hostedClustersKubeconfigFile)

		compat_otp.By(fmt.Sprintf("Step 3: Enable ClusterAutoScaling for hosted cluster %s with Priority expander specified", hostedcluster.name))
		defer hostedcluster.disableClusterAutoScale(hostedcluster.name)
		autoScalingJson := `{
			"scaling": "ScaleUpOnly",
			"expanders": ["Priority"]
		}`
		hostedcluster.setClusterAutoScale(hostedcluster.name, autoScalingJson)

		compat_otp.By("Step 4: Enable autoscaling for the new created NodePools")
		defer hostedcluster.disableNodePoolAutoScale(npName1)
		hostedcluster.setNodepoolAutoScale(npName1, strconv.Itoa(autoScalingMax), strconv.Itoa(autoScalingMin))
		doOcpReq(oc, OcpPatch, true, "np", npName1, "-n", hostedcluster.namespace, "--type", "merge", "-p", fmt.Sprintf(`{"spec":{"nodeDrainTimeout": "%s"}}`, "5m"))
		defer hostedcluster.disableNodePoolAutoScale(npName2)
		hostedcluster.setNodepoolAutoScale(npName2, strconv.Itoa(autoScalingMax), strconv.Itoa(autoScalingMin))
		doOcpReq(oc, OcpPatch, true, "np", npName2, "-n", hostedcluster.namespace, "--type", "merge", "-p", fmt.Sprintf(`{"spec":{"nodeDrainTimeout": "%s"}}`, "5m"))

		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName1), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready after setting autoscaling error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName1)).Should(o.BeTrue())

		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName2), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready after setting autoscaling error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName2)).Should(o.BeTrue())
		hostedcluster.validateClusterAutoScallerDeploymentRunning()

		compat_otp.By("Step 5: Create workload to the hosted cluster to trigger scaling up")
		workLoad := workload{
			name:      "workload",
			namespace: "default",
			template:  workloadTemplate,
		}
		defer workLoad.delete(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile)
		workLoad.create(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile, "--local")

		compat_otp.By(fmt.Sprintf("Step 6: Check that the nodes in the hosted cluster are auto-scaled to %d", autoScalingMax))
		o.Eventually(hostedcluster.pollGetHostedClusterReadyNodeCount(npName1), DoubleLongTimeout, DoubleLongTimeout/10).Should(o.BeEquivalentTo(autoScalingMax), "nodepool autoscaling max error")
		o.Eventually(hostedcluster.pollGetHostedClusterReadyNodeCount(npName2), DoubleLongTimeout, DoubleLongTimeout/10).Should(o.BeEquivalentTo(autoScalingMax), "nodepool autoscaling max error")

		compat_otp.By("Step 7: All machines created in node pool 2 should be earlier before ndoes created in node pool 1")
		// The min is at least 1, so there is one node created for each nodepool before any workload
		// So we check the second machine in nodepool 1 and the second to last machine in nodepool 2
		firstMachineCreationTimeInNodePool1 := doOcpReq(oc, OcpGet, true, "machines", "-l", "cluster.x-k8s.io/deployment-name="+npName1, "-n", controlplaneNS, "--sort-by=.metadata.creationTimestamp", "-o=jsonpath={.items[1].metadata.creationTimestamp}")
		lastMachineCreationTimeInNodePool2 := doOcpReq(oc, OcpGet, true, "machines", "-l", "cluster.x-k8s.io/deployment-name="+npName2, "-n", controlplaneNS, "--sort-by=.metadata.creationTimestamp", "-o=jsonpath={.items[-1].metadata.creationTimestamp}")
		t1, err := time.Parse(time.RFC3339, firstMachineCreationTimeInNodePool1)
		o.Expect(err).NotTo(o.HaveOccurred())
		t2, err := time.Parse(time.RFC3339, lastMachineCreationTimeInNodePool2)
		o.Expect(err).NotTo(o.HaveOccurred())
		compat_otp.By(fmt.Sprintf(`Create time of the Second  machine in nodepool 1: %s: %s`, npName1, firstMachineCreationTimeInNodePool1))
		compat_otp.By(fmt.Sprintf(`Create time of the last machine in nodepool 2: %s: %s`, npName2, lastMachineCreationTimeInNodePool2))
		o.Expect(t1.After(t2) || t1.Equal(t2)).Should(o.BeTrue())

		compat_otp.By("Step 8: Great, expander with priority works well!")
	})

	// author: lgao@redhat.com
	// Test run duration: about 22 mins
	g.FIt("Hypershift-HyperShiftMGMT-Longduration-NonPreRelease-Author:lgao-Critical-82718-Hosted Clusters expose CAO's BalancingIgnoredLabels [Serial] [Disruptive]", func() {
		if iaasPlatform != "aws" && iaasPlatform != "azure" {
			g.Skip("Skip due to incompatible platform")
		}
		var (
			npName1            = "lgao-82718-test-01"
			npName2            = "lgao-82718-test-02"
			npName3            = "lgao-82718-test-03"
			autoScalingMax     = 4
			autoScalingMin     = 1
			workloadTemplate   = filepath.Join(hypershiftTeamBaseDir, "workload.yaml")
			parsedWorkloadFile = "ocp-82718-workload-template.config"
		)
		compat_otp.By("Step 1: Create 3 NodePools for the test")
		defer func() {
			hostedcluster.deleteNodePool(npName1)
			hostedcluster.deleteNodePool(npName2)
			hostedcluster.deleteNodePool(npName3)
			o.Eventually(hostedcluster.pollCheckAllNodepoolReady(), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "in defer check all nodes ready error")
		}()
		switch iaasPlatform {
		case "aws":
			hostedcluster.createAwsNodePool(npName1, 1)
			hostedcluster.createAwsNodePool(npName2, 1)
			hostedcluster.createAwsNodePool(npName3, 1)
		case "azure":
			hostedcluster.createAdditionalAzureNodePool(npName1, 1)
			hostedcluster.createAdditionalAzureNodePool(npName2, 1)
			hostedcluster.createAdditionalAzureNodePool(npName3, 1)
		}

		// update nodelabels in the node pools
		compat_otp.By("Step 2: Update NodeLabels for the 3 node pools to have a common label which will be ignored on the node groups balancing")
		// np1 has label a, np2 and np3 have label b
		nodeBalancingIgnoredLabel := "node.group.balancing.ignored"
		hostedcluster.patchNodeLabelToNodePool(npName1, fmt.Sprintf(`{"%s": "test-01"}`, nodeBalancingIgnoredLabel))
		hostedcluster.patchNodeLabelToNodePool(npName2, fmt.Sprintf(`{"%s": "test-01"}`, nodeBalancingIgnoredLabel))
		hostedcluster.patchNodeLabelToNodePool(npName3, fmt.Sprintf(`{"%s": "test-03"}`, nodeBalancingIgnoredLabel))

		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName1), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName1)).Should(o.BeFalse())
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName2), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName2)).Should(o.BeFalse())
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName3), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName3)).Should(o.BeFalse())

		nodeNames := hostedcluster.getNodepoolHostedClusterNodes(npName1)
		o.Expect(len(nodeNames)).Should(o.BeEquivalentTo(1))
		nodeName := nodeNames[0]
		nodesInfoArgs := []string{"--kubeconfig=" + hostedcluster.hostedClustersKubeconfigFile, "node", nodeName, "-o", "jsonpath='{.status.allocatable}'"}
		allocatable := doOcpReq(oc, OcpGet, true, nodesInfoArgs...)
		o.Expect(allocatable).ShouldNot(o.BeEmpty())
		if strings.HasPrefix(allocatable, "'") && strings.HasSuffix(allocatable, "'") {
			allocatable = strings.Trim(allocatable, "'")
		}
		var allocatableRes map[string]string
		err := json.Unmarshal([]byte(allocatable), &allocatableRes)
		o.Expect(err).NotTo(o.HaveOccurred())
		allocatableMem := allocatableRes["memory"]
		o.Expect(allocatableMem).ShouldNot(o.BeEmpty())

		compat_otp.By(fmt.Sprintf("Step 3: Enable ClusterAutoScaling for hosted cluster %s with Random expander specified and balancingIgnoredLabels set.", hostedcluster.name))
		defer hostedcluster.disableClusterAutoScale(hostedcluster.name)
		// Sets ScaleUpOnly but also set maxPodGracePeriod to 60 seconds which would trigger the scaling down if enabled.
		autoScalingJson := fmt.Sprintf(`{
			"scaling": "ScaleUpOnly",
			"balancingIgnoredLabels": ["%s"],
			"maxFreeDifferenceRatioPercent": 50,
			"expanders": ["Random"]
		}`, nodeBalancingIgnoredLabel)
		hostedcluster.setClusterAutoScale(hostedcluster.name, autoScalingJson)

		compat_otp.By("Step 4: Enable autoscaling for the new created NodePools")
		defer hostedcluster.disableNodePoolAutoScale(npName1)
		hostedcluster.setNodepoolAutoScale(npName1, strconv.Itoa(autoScalingMax), strconv.Itoa(autoScalingMin))
		doOcpReq(oc, OcpPatch, true, "np", npName1, "-n", hostedcluster.namespace, "--type", "merge", "-p", fmt.Sprintf(`{"spec":{"nodeDrainTimeout": "%s"}}`, "5m"))
		defer hostedcluster.disableNodePoolAutoScale(npName2)
		hostedcluster.setNodepoolAutoScale(npName2, strconv.Itoa(autoScalingMax), strconv.Itoa(autoScalingMin))
		doOcpReq(oc, OcpPatch, true, "np", npName2, "-n", hostedcluster.namespace, "--type", "merge", "-p", fmt.Sprintf(`{"spec":{"nodeDrainTimeout": "%s"}}`, "5m"))
		defer hostedcluster.disableNodePoolAutoScale(npName3)
		hostedcluster.setNodepoolAutoScale(npName3, strconv.Itoa(autoScalingMax), strconv.Itoa(autoScalingMin))
		doOcpReq(oc, OcpPatch, true, "np", npName3, "-n", hostedcluster.namespace, "--type", "merge", "-p", fmt.Sprintf(`{"spec":{"nodeDrainTimeout": "%s"}}`, "5m"))

		// now the node status becomes Ready,SchedulingDisabled, then get deleted
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName1), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready after setting autoscaling error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName1)).Should(o.BeTrue())
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName2), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready after setting autoscaling error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName2)).Should(o.BeTrue())
		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(npName3), LongTimeout, LongTimeout/10).Should(o.BeTrue(), "nodepool ready after setting autoscaling error")
		o.Expect(hostedcluster.isNodepoolAutosaclingEnabled(npName3)).Should(o.BeTrue())
		hostedcluster.validateClusterAutoScallerDeploymentRunning()

		compat_otp.By("Step 5: Create workload to the hosted cluster to trigger scaling up")
		nodeMem := resource.MustParse(allocatableMem)
		nodeMemBytes, ok := nodeMem.AsInt64()
		if !ok {
			nodeMemBytes = int64(nodeMem.AsApproximateFloat64())
		}
		podMemReq := nodeMemBytes / 2
		targetNodeCount := 6
		workloadMemRequest := resource.NewQuantity(podMemReq, resource.BinarySI).String()
		e2e.Logf("Pod request memory: %s", workloadMemRequest)

		workLoad := workload{
			name:      "workload",
			namespace: "default",
			template:  workloadTemplate,
		}
		defer workLoad.delete(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile)
		workLoad.create(oc, hostedcluster.getHostedClusterKubeconfigFile(), parsedWorkloadFile, "CPU=100m", "MEMORY="+workloadMemRequest, "PARALLELISM="+strconv.Itoa(targetNodeCount), "--local")

		// check the nodes count of the 2 node pools
		compat_otp.By(fmt.Sprintf("Step 6: Check that the nodes in the hosted cluster is auto-scaled to %d", targetNodeCount))
		nodeCount := func() int {
			// list number of nodes with '.spec.unschedulable: true'
			cnt1, err := hostedcluster.getHostedClusterReadyNodeCount(npName1)
			o.Expect(err).NotTo(o.HaveOccurred())
			cnt2, err := hostedcluster.getHostedClusterReadyNodeCount(npName2)
			o.Expect(err).NotTo(o.HaveOccurred())
			cnt3, err := hostedcluster.getHostedClusterReadyNodeCount(npName3)
			o.Expect(err).NotTo(o.HaveOccurred())
			e2e.Logf("Node Count of: %s : %d", npName1, cnt1)
			e2e.Logf("Node Count of: %s : %d", npName2, cnt2)
			e2e.Logf("Node Count of: %s : %d", npName3, cnt3)
			return cnt1 + cnt2 + cnt3
		}
		o.Eventually(nodeCount, DoubleLongTimeout, DoubleLongTimeout/10).Should(o.BeNumerically(">=", targetNodeCount))

		compat_otp.By("Step 7: Check that the nodes in 2 node pools should be roughtly even")
		cnt1, err := hostedcluster.getHostedClusterReadyNodeCount(npName1)
		o.Expect(err).NotTo(o.HaveOccurred())
		cnt2, err := hostedcluster.getHostedClusterReadyNodeCount(npName2)
		o.Expect(err).NotTo(o.HaveOccurred())
		cnt3, err := hostedcluster.getHostedClusterReadyNodeCount(npName3)
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Final Node Count of: %s : %d", npName1, cnt1)
		e2e.Logf("Final Node Count of: %s : %d", npName2, cnt2)
		e2e.Logf("Final Node Count of: %s : %d", npName3, cnt3)
		o.Expect(func() int {
			diff := cnt1 - cnt2
			if diff < 0 {
				return -diff
			}
			return diff
		}()).Should(o.BeNumerically("<=", 1))
		o.Expect(func() int {
			diff := cnt1 - cnt3
			if diff < 0 {
				return -diff
			}
			return diff
		}()).Should(o.BeNumerically("<=", 1))
		o.Expect(func() int {
			diff := cnt2 - cnt3
			if diff < 0 {
				return -diff
			}
			return diff
		}()).Should(o.BeNumerically("<=", 1))

		compat_otp.By("Step 8: Great, balancing ingore labels works well!")
	})

})
