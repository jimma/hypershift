package extend

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	exutil "github.com/openshift/origin/test/extended/util"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	"github.com/openshift/origin/test/extended/util/compat_otp/clusterinfra"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-hypershift] Hypershift", func() {
	defer g.GinkgoRecover()

	var (
		oc                    = exutil.NewCLIWithoutNamespace("hypershift")
		iaasPlatform          string
		hostedcluster         *hostedCluster
		hostedclusterPlatform PlatformType
	)

	if err := compat_otp.InitTest(false); err != nil {
		panic(err)
	}
	e2e.AfterReadingAllFlags(compat_otp.TestContext)
	g.BeforeEach(func(ctx context.Context) {
		exutil.WithCleanup(func() {
			fmt.Println("cleanup after each test")
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
		iaasPlatform = compat_otp.ExtendedCheckPlatform(ctx, oc)
		//hypershiftTeamBaseDir = compat_otp.FixturePath("testdata", "hypershift")
		// hosted cluster infra ID
		//hcInfraID = doOcpReq(oc, OcpGet, true, "hc", hostedClusterName, "-n", hostedClusterNs, `-ojsonpath={.spec.infraID}`)

		hostedclusterPlatform = doOcpReq(oc, OcpGet, true, "hostedcluster", "-n", hostedcluster.namespace, hostedcluster.name, "-ojsonpath={.spec.platform.type}")
		e2e.Logf("HostedCluster platform is: %s", hostedclusterPlatform)

		if compat_otp.IsROSA() {
			compat_otp.ROSALogin()
		}

	})

	// author: ema@redhat.com
	g.It("Author:ema-HyperShiftMGMT-Critical-83131-Setting AWS tags on existing Hosted Clusters and their Nodepools happens without roll out", func() {
		if iaasPlatform != "aws" {
			g.Skip("Day2 tag test on HostedCluster and NodePool is only on AWS and skip this test on:" + iaasPlatform)
		}
		hcVersion := compat_otp.GetHostedClusterVersion(oc, hostedcluster.name, hostedcluster.namespace)
		e2e.Logf("Found hosted cluster version = %q", hcVersion)
		hcVersion.Pre = nil
		minHcVersion := semver.MustParse("4.19.0")
		if hcVersion.LT(minHcVersion) {
			g.Skip(fmt.Sprintf("Skip the test on HostedCluster version lower than 4.19. The current HostedCluster version: %s", hcVersion))
		}
		rc := hostedcluster.checkHCConditions()
		o.Expect(rc).Should(o.BeTrue())

		operatorNS := compat_otp.GetHyperShiftOperatorNameSpace(oc)
		e2e.Logf("hosted cluster operator namespace %s", operatorNS)
		o.Expect(operatorNS).NotTo(o.BeEmpty())

		//get guest cluster worker node creation time
		guestName, guestKube, _ := compat_otp.ValidHypershiftAndGetGuestKubeConfWithNoSkip(oc)
		o.Expect(guestName).NotTo(o.BeEmpty())
		o.Expect(guestKube).NotTo(o.BeEmpty())

		resourceTagPatch := `[
			              {
			                "op": "add",
			                "path": "/spec/platform/aws/resourceTags/0",
			                "value": {
			                  "key": "hypershift.io/hostedcluster/qe-test",
			                  "value": "owned"
			                 }
			              }
			         ]`
		e2e.Logf("Add resource tag: %s", resourceTagPatch)

		doOcpReq(oc, OcpPatch, true, "hostedcluster", hostedcluster.name, "-n", hostedcluster.namespace, "--type=json", "-p="+resourceTagPatch)

		//revert the resource tag change
		resourceTagRevertPatch := `[
			              {
			                "op": "remove",
			                "path": "/spec/platform/aws/resourceTags/0"
			              }
			         ]`
		defer doOcpReq(oc, OcpPatch, true, "hostedcluster", hostedcluster.name, "-n", hostedcluster.namespace, "--type=json", "-p="+resourceTagRevertPatch)

		//get the first nodepool
		nodePool := doOcpReq(oc, OcpGet, true, "nodepool", "-n", hostedcluster.namespace, "-ojsonpath={.items[0].metadata.name}")
		e2e.Logf("Get the node pool name: %s", nodePool)

		//check if the hostedcluster tag is added to aws instance
		awsInstanceIDs := hostedcluster.getAWSInstanceIDs(nodePool)
		//get the youngest age aws instance
		awsInstanceID := awsInstanceIDs[len(awsInstanceIDs)-1]
		parts := strings.Split(awsInstanceID, "/")
		awsInstance := parts[len(parts)-1]
		//check aws tags
		clusterinfra.GetAwsCredentialFromCluster(oc)
		region := doOcpReq(oc, OcpGet, true, "hostedcluster", hostedcluster.name, "-n", hostedcluster.namespace, "-ojsonpath={.spec.platform.aws.region}")
		e2e.Logf("The aws region of the hosted cluster is %s", region)
		awsClient := compat_otp.InitAwsSessionWithRegion(region)
		instanceTags, err := awsClient.DescribeTags("instance", awsInstance)
		o.Expect(err).NotTo(o.HaveOccurred())
		awsInstanceTags := instanceTags.String()
		e2e.Logf("Get aws insance: %s tags : %s", awsInstanceID, awsInstanceTags)
		o.Expect(strings.Contains(awsInstanceTags, "hypershift.io/hostedcluster/qe-test")).To(o.BeTrue())

		//Check security group tag
		securityGroups, err := awsClient.GetInstanceSecurityGroupIDs(awsInstance)
		o.Expect(err).NotTo(o.HaveOccurred())
		if len(securityGroups) != 0 {
			exists := false
			for _, securityGroup := range securityGroups {
				securityGroupTags, err := awsClient.DescribeTags("security-group", securityGroup)
				o.Expect(err).NotTo(o.HaveOccurred())
				securityGroupStringTags := securityGroupTags.String()
				e2e.Logf("Get security group tags : %s", securityGroupStringTags)
				if strings.Contains(securityGroupStringTags, "hypershift.io/hostedcluster/qe-test") {
					exists = true
					break
				}
			}
			o.Expect(exists).To(o.BeTrue(), "expected the security group tag 'hypershift.io/hostedcluster/qe-test' to exist but it does not")
		}
		//check if the node will be rolled out after HC is tagged
		o.Consistently(hostedcluster.pollCheckHostedClustersNodePoolReady(nodePool), time.Minute*6, time.Second*10).Should(o.BeTrue())
		nodePoolTagPatch := `{
			                  "spec": {
			                     "platform": {
				                    "aws": {
 										"resourceTags": [
                                           {
                                            "key": "hypershift.io/nodepool/qe-test",
                                            "value": "owned"
                                           }
		                                ]
		                            }
		                         }
		                      }
		                    }`

		nodePoolReplics := hostedcluster.getNodepoolReadyReplicas(nodePool)
		doOcpReq(oc, OcpPatch, true, "nodepool", nodePool, "-n", hostedcluster.namespace, "--type=merge", "-p="+nodePoolTagPatch)

		nodePoolTagRevertPatch := `[
			              {
			                "op": "remove",
			                "path": "/spec/platform/aws/resourceTags/0"
			              }
			         ]`
		defer doOcpReq(oc, OcpPatch, true, "nodepool", nodePool, "-n", hostedcluster.namespace, "--type=json", "-p="+nodePoolTagRevertPatch)

		//The existing node should not be replaced/recreated
		o.Consistently(hostedcluster.pollCheckHostedClustersNodePoolReady(nodePool), time.Minute*6, time.Second*10).Should(o.BeTrue())

		//check if this tag is added aws instance
		instanceTags, err = awsClient.DescribeTags("instance", awsInstance)
		o.Expect(err).NotTo(o.HaveOccurred())
		awsInstanceTags = instanceTags.String()
		e2e.Logf("Get aws insance: %s tags : %s", awsInstanceID, awsInstanceTags)
		o.Expect(strings.Contains(awsInstanceTags, "hypershift.io/nodepool/qe-test")).To(o.BeTrue())

		//increase nodepool replicas
		nodePoolReplicaPatch := fmt.Sprintf("{\"spec\":{\"replicas\": %d}}", nodePoolReplics+1)
		doOcpReq(oc, OcpPatch, true, "nodepool", nodePool, "-n", hostedcluster.namespace, "--type=merge", "-p="+nodePoolReplicaPatch)

		revertNodePoolReplicaPatch := fmt.Sprintf("{\"spec\":{\"replicas\": %d}}", nodePoolReplics)
		defer doOcpReq(oc, OcpPatch, true, "nodepool", nodePool, "-n", hostedcluster.namespace, "--type=merge", "-p="+revertNodePoolReplicaPatch)

		o.Eventually(hostedcluster.pollCheckHostedClustersNodePoolReady(nodePool), time.Minute*10, time.Second*10).Should(o.BeTrue())

		awsInstanceIDs = hostedcluster.getAWSInstanceIDs(nodePool)
		//get the youngest age aws instance
		awsInstanceID = awsInstanceIDs[len(awsInstanceIDs)-1]
		parts = strings.Split(awsInstanceID, "/")
		awsInstance = parts[len(parts)-1]
		e2e.Logf("Get aws insance: %s tags", awsInstance)
		//check aws tags
		clusterinfra.GetAwsCredentialFromCluster(oc)
		instanceTags, err = awsClient.DescribeTags("instance", awsInstance)
		o.Expect(err).NotTo(o.HaveOccurred())
		awsInstanceTags = instanceTags.String()
		e2e.Logf("Get aws insance: tags : %s", awsInstanceTags)
		o.Expect(strings.Contains(awsInstanceTags, "hypershift.io/nodepool/qe-test")).To(o.BeTrue())
		o.Expect(strings.Contains(awsInstanceTags, "hypershift.io/hostedcluster/qe-test")).To(o.BeTrue())
	})
})
