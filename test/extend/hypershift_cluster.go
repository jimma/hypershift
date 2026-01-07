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
func ValidHypershiftAndGetGuestKubeConf(ctx context.Context, client crcclient.Client) (string, string, string, error) {
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
	if len(hcList.Items) == 0 {
		g.Skip("no hosted clusters found, skip test run\n")
	}

	var clusterNames []string
	for _, hc := range hcList.Items {
		clusterNames = append(clusterNames, hc.Name)
	}
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
		fmt.Printf("failed to list operator pods: %v\n", err)
		return "", "", "", err
	}

	if len(podList.Items) == 0 {
		fmt.Printf("no operator pods found in %s\n", operatorNS)
		return "", "", "", nil
	}

	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning {
			return "", "", "", fmt.Errorf("hypershift operator pod is not in running state")
		}
	}
	clusterName := clusterNames[0]
	fmt.Printf("the hosted cluster names: %s, and will select the first: %s\n", strings.Join(clusterNames, " "), clusterName)
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
	return hostedclusterNS, clusterName, hostedClusterKubeconfigFile, nil
}
func GetHyperShiftOperatorNamespace(ctx context.Context, client crcclient.Client) (string, error) {
	podList := &corev1.PodList{}
	opts := &crcclient.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"hypershift.openshift.io/operator-component": "operator",
			"app": "operator",
		}),
	}
	err := client.List(ctx, podList, opts)
	if err != nil {
		return "", err
	}
	if len(podList.Items) == 0 {
		return "", nil
	}
	return podList.Items[0].Namespace, nil
}

func GetHyperShiftHostedClusterNamespace(ctx context.Context, client crcclient.Client) (string, error) {
	hcList := &hyperv1.HostedClusterList{}
	err := client.List(ctx, hcList)
	if err != nil {
		return "", err
	}
	if len(hcList.Items) == 0 {
		return "", nil
	}

	//TODO: check if we need return the namespace array
	var namespaces []string
	for _, hc := range hcList.Items {
		namespaces = append(namespaces, hc.Namespace)
	}
	return namespaces[0], nil
}
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

func GetNodePoolNamesByHostedClusterName(ctx context.Context, c crcclient.Client, hostedClusterNS, hostedClusterName string) ([]string, error) {
	nodePoolList := &hyperv1.NodePoolList{}
	err := c.List(
		ctx,
		nodePoolList,
		&crcclient.ListOptions{
			Namespace: hostedClusterNS,
		},
	)
	if err != nil {
		return nil, err
	}
	var nodePoolNames []string
	for _, np := range nodePoolList.Items {
		nodePoolNames = append(nodePoolNames, np.Name)
	}
	return nodePoolNames, nil
}

func GetHostedClusterVersion(ctx context.Context, guestClient crcclient.Client, hostedClusterNs, hostedClusterName string) (semver.Version, error) {
	cv := &configv1.ClusterVersion{}
	err := guestClient.Get(
		ctx,
		crcclient.ObjectKey{Name: "version"},
		cv,
	)
	if err != nil {
		return semver.Version{}, err
	}
	var versionStr string
	for _, h := range cv.Status.History {
		if h.State == configv1.CompletedUpdate {
			versionStr = h.Version
		}
	}
	hcVersion := semver.MustParse(versionStr)
	return hcVersion, nil
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
