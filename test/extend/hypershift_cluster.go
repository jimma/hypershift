package extend

import (
	"context"
	"fmt"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"math/rand"
	"os"
	"os/exec"
	crcclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
)

type HostedClusterPlatformType = string

// ValidHypershiftAndGetGuestKubeConf check if it is hypershift env and get kubeconf of the hosted cluster
// the first return is hosted cluster name
// the second return is the file of kubeconfig of the hosted cluster
// the third return is the hostedcluster namespace in mgmt cluster which contains the generated resources
// if it is not hypershift env, it will skip test.
func ValidHypershiftAndGetGuestKubeConf(ctx context.Context, client crcclient.Client) (string, string, string) {
	/*if IsROSA() {
		e2e.Logf("there is a ROSA env")
		hostedClusterName, hostedclusterKubeconfig, hostedClusterNs := ROSAValidHypershiftAndGetGuestKubeConf(oc)
		if len(hostedClusterName) == 0 || len(hostedclusterKubeconfig) == 0 || len(hostedClusterNs) == 0 {
			g.Skip("there is a ROSA env, but the env is problematic, skip test run")
		}
		return hostedClusterName, hostedclusterKubeconfig, hostedClusterNs
	}*/
	operatorNS, err := GetHyperShiftOperatorNamespace(ctx, client)
	if len(operatorNS) <= 0 {
		g.Skip("there is no hypershift operator on host cluster, skip test run")
	}

	hostedclusterNS, _ := GetHyperShiftHostedClusterNamespace(ctx, client)
	if len(hostedclusterNS) <= 0 {
		g.Skip("there is no hosted cluster NS in mgmt cluster, skip test run")
	}

	hcList := &hyperv1.HostedClusterList{}

	err = client.List(ctx, hcList, &crcclient.ListOptions{
		Namespace: hostedclusterNS,
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	if len(hcList.Items) == 0 {
		fmt.Errorf("no hosted clusters found, skip test run")
		return "", "", ""
	}

	// collect names
	var clusterNames []string
	for _, hc := range hcList.Items {
		clusterNames = append(clusterNames, hc.Name)
	}

	// Verify operator pod running ===
	// labels :
	//  - hypershift.openshift.io/operator-component=operator
	//  - app=operator
	operatorSelector := labels.SelectorFromSet(labels.Set{
		"hypershift.openshift.io/operator-component": "operator",
		"app": "operator",
	})

	podList := &corev1.PodList{}
	err = client.List(ctx, podList,
		&crcclient.ListOptions{
			Namespace: operatorNS,
		},
		crcclient.MatchingLabelsSelector{Selector: operatorSelector},
	)
	if err != nil {
		fmt.Errorf("failed to list operator pods: %v", err)
		return "", "", ""
	}

	if len(podList.Items) == 0 {
		fmt.Errorf("no operator pods found in %s", operatorNS)
		return "", "", ""
	}

	running := false
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			running = true
			break
		}
	}
	if !running {
		fmt.Errorf("operator pod not in Running state")
		return "", "", ""
	}

	// === 3) Pick the first hostedcluster ===
	// matches your strings.Split(clusterNames, " ")[0] logic
	clusterName := clusterNames[0]

	fmt.Printf("the hosted cluster names: %s, and will select the first: %s\n",
		strings.Join(clusterNames, " "), clusterName)

	var hostedClusterKubeconfigFile string
	if os.Getenv("GUEST_KUBECONFIG") != "" {
		fmt.Printf("the kubeconfig you set GUEST_KUBECONFIG must be that of the hosted cluster %s in namespace %s", clusterName, hostedclusterNS)
		hostedClusterKubeconfigFile = os.Getenv("GUEST_KUBECONFIG")
		fmt.Printf("use a known hosted cluster kubeconfig: %v", hostedClusterKubeconfigFile)
	} else {
		hostedClusterKubeconfigFile = "/tmp/guestcluster-kubeconfig-" + clusterName + "-" + GetRandomString()
		output, err := exec.Command("bash", "-c", fmt.Sprintf("hypershift create kubeconfig --name %s --namespace %s > %s",
			clusterName, hostedclusterNS, hostedClusterKubeconfigFile)).Output()
		fmt.Printf("the cmd output: %s", string(output))
		o.Expect(err).NotTo(o.HaveOccurred())
		fmt.Printf("create a new hosted cluster kubeconfig: %v", hostedClusterKubeconfigFile)
	}
	fmt.Printf("if you want hostedcluster controlplane namespace, you could get it by combining %s and %s with -", hostedclusterNS, clusterName)
	return hostedclusterNS, clusterName, hostedClusterKubeconfigFile
}

// ValidHypershiftAndGetGuestKubeConfWithNoSkip check if it is hypershift env and get kubeconf of the hosted cluster
// the first return is hosted cluster name
// the second return is the file of kubeconfig of the hosted cluster
// the third return is the hostedcluster namespace in mgmt cluster which contains the generated resources
// if it is not hypershift env, it will not skip the testcase and return null string.
/*
func ValidHypershiftAndGetGuestKubeConfWithNoSkip(ctx context.Context, client crcclient.Client) (string, string, string) {
	if IsROSA() {
		e2e.Logf("there is a ROSA env")
		return ROSAValidHypershiftAndGetGuestKubeConf(oc)
	}
	operatorNS, err := GetHyperShiftOperatorNameSpace(ctx, client)
	if len(operatorNS) <= 0 {
		return "", "", ""
	}

	hostedclusterNS := GetHyperShiftHostedClusterNameSpace(ctx,client)
	if len(hostedclusterNS) <= 0 {
		return "", "", ""
	}

	clusterNames, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(
		"-n", hostedclusterNS, "hostedclusters", "-o=jsonpath={.items[*].metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if len(clusterNames) <= 0 {
		return "", "", ""
	}

	hypersfhitPodStatus, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(
		"-n", operatorNS, "pod", "-l", "hypershift.openshift.io/operator-component=operator", "-l", "app=operator", "-o=jsonpath={.items[*].status.phase}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(hypersfhitPodStatus).To(o.ContainSubstring("Running"))

	//get first hosted cluster to run test
	e2e.Logf("the hosted cluster names: %s, and will select the first", clusterNames)
	clusterName := strings.Split(clusterNames, " ")[0]

	var hostedClusterKubeconfigFile string
	if os.Getenv("GUEST_KUBECONFIG") != "" {
		e2e.Logf("the kubeconfig you set GUEST_KUBECONFIG must be that of the guestcluster %s in namespace %s", clusterName, hostedclusterNS)
		hostedClusterKubeconfigFile = os.Getenv("GUEST_KUBECONFIG")
		e2e.Logf("use a known hosted cluster kubeconfig: %v", hostedClusterKubeconfigFile)
	} else {
		hostedClusterKubeconfigFile = "/tmp/guestcluster-kubeconfig-" + clusterName + "-" + GetRandomString()
		output, err := exec.Command("bash", "-c", fmt.Sprintf("hypershift create kubeconfig --name %s --namespace %s > %s",
			clusterName, hostedclusterNS, hostedClusterKubeconfigFile)).Output()
		e2e.Logf("the cmd output: %s", string(output))
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("create a new hosted cluster kubeconfig: %v", hostedClusterKubeconfigFile)
	}
	e2e.Logf("if you want hostedcluster controlplane namespace, you could get it by combining %s and %s with -", hostedclusterNS, clusterName)
	return clusterName, hostedClusterKubeconfigFile, hostedclusterNS
}*/

func GetHyperShiftOperatorNamespace(ctx context.Context, client crcclient.Client) (string, error) {

	podList := &corev1.PodList{}

	opts := &crcclient.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"hypershift.openshift.io/operator-component": "operator",
			"app": "operator",
		}),
	}

	err := client.List(
		ctx,
		podList,
		opts,
	)
	if err != nil {
		return "", err
	}

	// Equivalent to --ignore-not-found + jsonpath items[0]
	if len(podList.Items) == 0 {
		return "", nil
	}

	return strings.TrimSpace(
		podList.Items[0].Namespace,
	), nil
}

func GetHyperShiftHostedClusterNamespace(ctx context.Context, client crcclient.Client) (string, error) {

	hcList := &hyperv1.HostedClusterList{}

	err := client.List(ctx, hcList)
	if err != nil {
		if strings.Contains(err.Error(), "no matches for kind") ||
			strings.Contains(err.Error(), "could not find the requested resource") {
			return "", nil
		}
		return "", err
	}

	if len(hcList.Items) == 0 {
		return "", nil
	}

	// Collect namespaces (equivalent to jsonpath items[*].metadata.namespace)
	var namespaces []string
	for _, hc := range hcList.Items {
		namespaces = append(namespaces, hc.Namespace)
	}

	if len(namespaces) == 1 {
		return namespaces[0], nil
	}

	// Match original logic: skip "clusters"
	for _, ns := range namespaces {
		if ns != "clusters" {
			return ns, nil
		}
	}

	// Fallback (same behavior as original)
	return namespaces[0], nil
}

// ROSAValidHypershiftAndGetGuestKubeConf check if it is ROSA-hypershift env and get kubeconf of the hosted cluster, only support prow
// the first return is hosted cluster name
// the second return is the file of kubeconfig of the hosted cluster
// the third return is the hostedcluster namespace in mgmt cluster which contains the generated resources
// if it is not hypershift env, it will skip test.
/*
func ROSAValidHypershiftAndGetGuestKubeConf(oc *util.CLI) (string, string, string) {
	operatorNS := GetHyperShiftOperatorNameSpace(oc)
	if len(operatorNS) <= 0 {
		e2e.Logf("there is no hypershift operator on host cluster")
		return "", "", ""
	}

	data, err := ioutil.ReadFile(os.Getenv("SHARED_DIR") + "/cluster-name")
	if err != nil {
		e2e.Logf("can't get hostedcluster name %s SHARE_DIR: %s", err.Error(), os.Getenv("SHARED_DIR"))
		return "", "", ""
	}
	clusterName := strings.TrimSpace(string(data))
	hostedclusterNS, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-A", "hostedclusters", `-o=jsonpath={.items[?(@.metadata.name=="`+clusterName+`")].metadata.namespace}`).Output()
	if len(hostedclusterNS) <= 0 {
		e2e.Logf("there is no hosted cluster NS in mgmt cluster")
	}

	hostedClusterKubeconfigFile := os.Getenv("SHARED_DIR") + "/nested_kubeconfig"
	return clusterName, hostedClusterKubeconfigFile, hostedclusterNS
}*/

// GetHostedClusterPlatformType returns a hosted cluster platform type
// oc is the management cluster client to query the hosted cluster platform type based on hostedcluster CR obj
func GetHostedClusterPlatformType(ctx context.Context, c crcclient.Client, clusterName, clusterNamespace string) (hyperv1.PlatformType, error) {

	/*if IsHypershiftHostedClusterClient(c) {
		return "", fmt.Errorf(
			"this is a hosted cluster env. You should use a management cluster client",
		)
	}*/

	hc := &hyperv1.HostedCluster{}

	err := c.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterName,
			Namespace: clusterNamespace,
		},
		hc,
	)
	if err != nil {
		return "", err
	}

	return hc.Spec.Platform.Type, nil
}

// GetNodePoolNamesbyHostedClusterName gets the nodepools names of the hosted cluster
func GetNodePoolNamesByHostedClusterName(ctx context.Context, c crcclient.Client, hostedClusterName, hostedClusterNS string) []string {

	nodePoolList := &hyperv1.NodePoolList{}

	err := c.List(
		ctx,
		nodePoolList,
		&crcclient.ListOptions{
			Namespace: hostedClusterNS,
		},
	)
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(nodePoolList.Items).NotTo(o.BeEmpty())

	var nodePoolNames []string
	for _, np := range nodePoolList.Items {
		nodePoolNames = append(nodePoolNames, np.Name)
	}

	fmt.Printf(
		"\n\nGot nodepool(s) for the hosted cluster %s: %v\n",
		hostedClusterName,
		nodePoolNames,
	)

	return nodePoolNames
}

// GetHostedClusterVersion gets a HostedCluster's version from the management cluster.
func GetHostedClusterVersion(ctx context.Context, c crcclient.Client, hostedClusterName, hostedClusterNs string) semver.Version {

	hc := &hyperv1.HostedCluster{}

	err := c.Get(
		ctx,
		types.NamespacedName{
			Name:      hostedClusterName,
			Namespace: hostedClusterNs,
		},
		hc,
	)
	o.Expect(err).NotTo(o.HaveOccurred())

	var versionStr string
	for _, h := range hc.Status.Version.History {
		if h.State != "" {
			versionStr = h.Version
			break
		}
	}
	o.Expect(versionStr).NotTo(o.BeEmpty())
	hcVersion := semver.MustParse(versionStr)
	return hcVersion
}

func CheckHypershiftOperatorExistence(ctx context.Context, c crcclient.Client) (bool, error) {
	podList := &corev1.PodList{}
	err := c.List(
		ctx,
		podList,
		&crcclient.ListOptions{
			Namespace: "hypershift",
		},
	)
	if err != nil {
		return false, fmt.Errorf("failed to get HO Pods: %w", err)
	}

	return len(podList.Items) > 0, nil
}
func GetRandomString() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	buffer := make([]byte, 8)
	for index := range buffer {
		buffer[index] = chars[seed.Intn(len(chars))]
	}
	return string(buffer)
}
func IsExternalControlPlane(ctx context.Context, c crcclient.Client) bool {
	infra := &configv1.Infrastructure{}

	err := c.Get(
		ctx,
		types.NamespacedName{
			Name: "cluster",
		},
		infra,
	)
	o.Expect(err).NotTo(o.HaveOccurred())

	topology := string(infra.Status.ControlPlaneTopology)
	fmt.Printf("topology is %s", topology)

	if topology == "" {
		fmt.Printf("cluster status %+v", infra.Status)
		fmt.Printf("failure: controlPlaneTopology returned empty")
	}

	return strings.Compare(topology, "External") == 0
}

func GetHostedCluster(ctx context.Context, client crcclient.Client, name, namespace string) (*hyperv1.HostedCluster, error) {

	hc := &hyperv1.HostedCluster{}

	err := client.Get(ctx,
		crcclient.ObjectKey{
			Name:      name,
			Namespace: namespace,
		}, hc)
	if err != nil {
		return nil, err
	}
	return hc, nil
}
