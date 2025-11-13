package hypershift

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/blang/semver"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/origin/test/extended/util"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	"github.com/openshift/origin/test/extended/util/compat_otp/clusterinfra"
	"github.com/tidwall/gjson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	e2eoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
	netutils "k8s.io/utils/net"
)

func doOcpReq(oc *exutil.CLI, verb OcpClientVerb, notEmpty bool, args ...string) string {
	g.GinkgoHelper()
	res, err := oc.AsAdmin().WithoutNamespace().Run(verb).Args(args...).Output()
	o.Expect(err).ShouldNot(o.HaveOccurred())
	if notEmpty {
		o.Expect(res).ShouldNot(o.BeEmpty())
	}
	return res
}

func checkSubstring(src string, expect []string) {
	if expect == nil || len(expect) <= 0 {
		o.Expect(expect).ShouldNot(o.BeEmpty())
	}

	for i := 0; i < len(expect); i++ {
		o.Expect(src).To(o.ContainSubstring(expect[i]))
	}
}

func checkSubstringWithNoExit(src string, expect []string) bool {
	if expect == nil || len(expect) <= 0 {
		e2e.Logf("Warning expected sub string empty ? %+v", expect)
		return true
	}

	for i := 0; i < len(expect); i++ {
		if !strings.Contains(src, expect[i]) {
			e2e.Logf("expected sub string %s not in src %s", expect[i], src)
			return false
		}
	}

	return true
}

type workload struct {
	name      string
	namespace string
	template  string
}

func (wl *workload) create(oc *exutil.CLI, kubeconfig, parsedTemplate string, extraParams ...string) {
	params := []string{
		"--ignore-unknown-parameters=true", "-f", wl.template, "-p", "NAME=" + wl.name, "NAMESPACE=" + wl.namespace,
	}
	params = append(params, extraParams...)
	err := wl.applyResourceFromTemplate(oc, kubeconfig, parsedTemplate, params...)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (wl *workload) delete(oc *exutil.CLI, kubeconfig, parsedTemplate string) {
	defer func() {
		path := filepath.Join(e2e.TestContext.OutputDir, oc.Namespace()+"-"+parsedTemplate)
		os.Remove(path)
	}()
	args := []string{"job", wl.name, "-n", wl.namespace}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig="+kubeconfig)
	}
	err := oc.AsAdmin().WithoutNamespace().Run("delete").Args(args...).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (wl *workload) applyResourceFromTemplate(oc *exutil.CLI, kubeconfig, parsedTemplate string, parameters ...string) error {
	return applyResourceFromTemplate(oc, kubeconfig, parsedTemplate, parameters...)
}

// parse a struct for a Template variables to generate params like "NAME=myname", "NAMESPACE=clusters" ...
// currently only support int, string, bool, *int, *string, *bool. A pointer is used to check whether it is set explicitly.
// use json tag as the true variable Name in the struct e.g. < Name string `json:"NAME"`>
func parseTemplateVarParams(obj interface{}) ([]string, error) {
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return []string{}, errors.New("params must be a pointer pointed to a struct")
	}

	var params []string
	t := v.Elem().Type()
	for i := 0; i < t.NumField(); i++ {
		if !v.Elem().Field(i).CanInterface() {
			continue
		}
		varName := t.Field(i).Name
		varType := t.Field(i).Type
		varValue := v.Elem().Field(i).Interface()
		tagName := t.Field(i).Tag.Get("json")

		if tagName == "" {
			continue
		}

		//handle non nil pointer that set the params explicitly
		if varType.Kind() == reflect.Ptr {
			if reflect.ValueOf(varValue).IsNil() {
				continue
			}

			switch reflect.ValueOf(varValue).Elem().Type().Kind() {
			case reflect.Int:
				p := fmt.Sprintf("%s=%d", tagName, reflect.ValueOf(varValue).Elem().Interface().(int))
				params = append(params, p)
			case reflect.String:
				params = append(params, tagName+"="+reflect.ValueOf(varValue).Elem().Interface().(string))
			case reflect.Bool:
				v, _ := reflect.ValueOf(varValue).Elem().Interface().(bool)
				params = append(params, tagName+"="+strconv.FormatBool(v))
			default:
				e2e.Logf("parseTemplateVarParams params %v invalid, ignore it", varName)
			}
			continue
		}

		//non-pointer
		switch varType.Kind() {
		case reflect.String:
			if varValue.(string) != "" {
				params = append(params, tagName+"="+varValue.(string))
			}
		case reflect.Int:
			params = append(params, tagName+"="+strconv.Itoa(varValue.(int)))
		case reflect.Bool:
			params = append(params, tagName+"="+strconv.FormatBool(varValue.(bool)))
		default:
			e2e.Logf("parseTemplateVarParams params %v not support, ignore it", varValue)
		}
	}

	return params, nil
}

func applyResourceFromTemplate(oc *exutil.CLI, kubeconfig, parsedTemplate string, parameters ...string) error {
	var configFile string
	defer func() {
		if len(configFile) > 0 {
			_ = os.Remove(configFile)
		}
	}()
	err := wait.PollUntilContextTimeout(context.Background(), 3*time.Second, 15*time.Second, true, func(_ context.Context) (bool, error) {
		output, err := oc.AsAdmin().Run("process").Args(parameters...).OutputToFile(parsedTemplate)
		if err != nil {
			e2e.Logf("Error processing template: %v, keep polling", err)
			return false, nil
		}
		configFile = output
		return true, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred())

	var args = []string{"-f", configFile}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig="+kubeconfig)
	}
	return oc.AsAdmin().WithoutNamespace().Run("apply").Args(args...).Execute()
}

func getClusterRegion(oc *exutil.CLI) (string, error) {
	return oc.AsAdmin().WithoutNamespace().Run("get").Args("node", `-ojsonpath={.items[].metadata.labels.topology\.kubernetes\.io/region}`).Output()
}

func getBaseDomain(oc *exutil.CLI) (string, error) {
	str, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns/cluster", `-ojsonpath={.spec.baseDomain}`).Output()
	if err != nil {
		return "", err
	}
	index := strings.Index(str, ".")
	if index == -1 {
		return "", fmt.Errorf("can not parse baseDomain because not finding '.'")
	}
	return str[index+1:], nil
}

func getAWSKey(oc *exutil.CLI) (string, string, error) {
	accessKeyID, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("secret/aws-creds", "-n", "kube-system", "-o", `template={{index .data "aws_access_key_id"|base64decode}}`).Output()
	if err != nil {
		return "", "", err
	}
	secureKey, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("secret/aws-creds", "-n", "kube-system", "-o", `template={{index .data "aws_secret_access_key"|base64decode}}`).Output()
	if err != nil {
		return "", "", err
	}
	return accessKeyID, secureKey, nil
}

func getAzureKey(oc *exutil.CLI) (string, string, string, string, error) {
	clientID, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("secret/azure-credentials", "-n", "kube-system", "-o", `template={{index .data "azure_client_id"|base64decode}}`).Output()
	if err != nil {
		return "", "", "", "", err
	}
	clientSecret, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("secret/azure-credentials", "-n", "kube-system", "-o", `template={{index .data "azure_client_secret"|base64decode}}`).Output()
	if err != nil {
		return "", "", "", "", err
	}
	subscriptionID, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("secret/azure-credentials", "-n", "kube-system", "-o", `template={{index .data "azure_subscription_id"|base64decode}}`).Output()
	if err != nil {
		return "", "", "", "", err
	}
	tenantID, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("secret/azure-credentials", "-n", "kube-system", "-o", `template={{index .data "azure_tenant_id"|base64decode}}`).Output()
	if err != nil {
		return "", "", "", "", err
	}
	return clientID, clientSecret, subscriptionID, tenantID, nil
}

/*
	parse a structure's tag 'param' and output cli command parameters like --params=$var, support embedded struct

e.g.
Input:

	  type example struct {
		Name string `param:"name"`
	    PullSecret string `param:"pull_secret"`
	  } {
	  	Name:"hypershift",
	    PullSecret:"pullsecret.txt",
	  }

Output:

	--name="hypershift" --pull_secret="pullsecret.txt"
*/
func parse(obj interface{}) ([]string, error) {
	var params []string
	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	k := v.Kind()
	if k == reflect.Struct {
		return parseStruct(v.Interface(), params)
	}
	return []string{}, fmt.Errorf("unsupported type: %s (supported types: struct, pointer to struct)", k)
}

func parseStruct(obj interface{}, params []string) ([]string, error) {
	v := reflect.ValueOf(obj)
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		varType := t.Field(i).Type
		varValueV := v.Field(i)

		if !t.Field(i).IsExported() {
			continue
		}

		if varType.Kind() == reflect.Ptr && varValueV.IsNil() {
			continue
		}

		for varType.Kind() == reflect.Ptr {
			varType = varType.Elem()
			varValueV = varValueV.Elem()
		}

		varValue := varValueV.Interface()
		varKind := varType.Kind()

		var err error
		if varKind == reflect.Struct {
			params, err = parseStruct(varValue, params)
			if err != nil {
				return []string{}, err
			}
			continue
		}

		tagName := t.Field(i).Tag.Get("param")
		if tagName == "" {
			continue
		}

		switch {
		case varKind == reflect.Map && isStringMap(varValueV):
			params = append(params, stringMapToParams(varValue.(map[string]string), tagName)...)
		case varKind == reflect.String:
			if varValue.(string) != "" {
				params = append(params, "--"+tagName+"="+varValue.(string))
			}
		case varKind == reflect.Int:
			params = append(params, "--"+tagName+"="+strconv.Itoa(varValue.(int)))
		case varKind == reflect.Int64:
			params = append(params, "--"+tagName+"="+strconv.FormatInt(varValue.(int64), 10))
		case varKind == reflect.Bool:
			params = append(params, "--"+tagName+"="+strconv.FormatBool(varValue.(bool)))
		default:
			e2e.Logf("parseTemplateVarParams params %s %v not support, ignore it", varType.Kind(), varValue)
		}
	}
	return params, nil
}

func isStringMap(v reflect.Value) bool {
	t := v.Type()
	return t.Kind() == reflect.Map &&
		t.Key().Kind() == reflect.String &&
		t.Elem().Kind() == reflect.String
}

func stringMapToParams(m map[string]string, flagName string) []string {
	params := make([]string, 0, len(m))
	for k, v := range m {
		params = append(params, fmt.Sprintf("--%s=%s=%s", flagName, k, v))
	}
	return params
}

func getSha256ByFile(file string) string {
	ha := sha256.New()
	f, err := os.Open(file)
	o.Expect(err).ShouldNot(o.HaveOccurred())
	defer f.Close()
	_, err = io.Copy(ha, f)
	o.Expect(err).ShouldNot(o.HaveOccurred())
	return fmt.Sprintf("%X", ha.Sum(nil))
}

func getJSONByFile(filePath string, path string) gjson.Result {
	file, err := os.Open(filePath)
	o.Expect(err).ShouldNot(o.HaveOccurred())
	defer file.Close()
	con, err := ioutil.ReadAll(file)
	o.Expect(err).ShouldNot(o.HaveOccurred())
	return gjson.Get(string(con), path)
}

func replaceInFile(file string, old string, new string) error {
	input, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	output := bytes.Replace(input, []byte(old), []byte(new), -1)
	err = ioutil.WriteFile(file, output, 0666)
	return err
}

func execCMDOnWorkNodeByBastion(showInfo bool, nodeIP, bastionIP, exec string) string {
	var bashClient = NewCmdClient().WithShowInfo(showInfo)
	privateKey, err := compat_otp.GetPrivateKey()
	o.Expect(err).NotTo(o.HaveOccurred())
	cmd := `chmod 600 ` + privateKey + `; ssh -i ` + privateKey + ` -o StrictHostKeyChecking=no -o ProxyCommand="ssh -i ` + privateKey + " -o StrictHostKeyChecking=no -W %h:%p ec2-user@" + bastionIP + `" core@` + nodeIP + ` '` + exec + `'`
	log, err := bashClient.Run(cmd).Output()
	o.Expect(err).ShouldNot(o.HaveOccurred())
	return log
}

func getAllByFile(filePath string) string {
	con, err := ioutil.ReadFile(filePath)
	o.Expect(err).ShouldNot(o.HaveOccurred())
	return string(con)
}

func getAWSPrivateCredentials(defaultCredPaths ...string) string {
	g.GinkgoHelper()

	// Always prefer environment variable override
	if envOverride := os.Getenv(AWSHyperShiftPrivateSecretFile); envOverride != "" {
		return envOverride
	}

	// Running in Prow
	if compat_otp.GetTestEnv().IsRunningInProw() {
		return DefaultAWSHyperShiftPrivateSecretFile
	}

	// Try default paths
	var res string
	for _, credPath := range defaultCredPaths {
		info, err := os.Stat(credPath)
		if err != nil {
			e2e.Logf("Error inspecting path %s: %v, skipping", credPath, err)
			continue
		}
		if mode := info.Mode(); !mode.IsRegular() {
			e2e.Logf("Path %s does not point to a regular file but a(n) %v, skipping", credPath, mode)
			continue
		}
		res = credPath
		break
	}
	o.Expect(res).NotTo(o.BeEmpty())
	return res
}

func subtractMinor(version *semver.Version, count uint64) *semver.Version {
	result := *version
	result.Minor = maxInt64(0, result.Minor-count)
	return &result
}

func maxInt64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func getHyperShiftOperatorLatestSupportOCPVersion() string {
	var bashClient = NewCmdClient().WithShowInfo(true)
	res, err := bashClient.Run(fmt.Sprintf("oc logs -n hypershift -lapp=operator --tail=-1 | head -1")).Output()
	o.Expect(err).ShouldNot(o.HaveOccurred())

	re := regexp.MustCompile(`Latest supported OCP: (\d+\.\d+\.\d+)`)
	match := re.FindStringSubmatch(res)
	o.Expect(len(match) > 1).Should(o.BeTrue())
	return match[1]
}

func getHyperShiftSupportedOCPVersion() (semver.Version, semver.Version) {
	v := getHyperShiftOperatorLatestSupportOCPVersion()
	latestSupportedVersion := semver.MustParse(v)
	minSupportedVersion := semver.MustParse(subtractMinor(&latestSupportedVersion, uint64(SupportedPreviousMinorVersions)).String())
	return latestSupportedVersion, minSupportedVersion
}

func getMinSupportedOCPVersion() string {
	_, minVersion := getHyperShiftSupportedOCPVersion()
	return minVersion.String()
}

// getAWSMgmtClusterAvailableZones returns available zones based on mgmt cluster's oc client and region
func getAWSMgmtClusterRegionAvailableZones(oc *exutil.CLI) []string {
	region, err := compat_otp.GetAWSClusterRegion(oc)
	o.Expect(err).ShouldNot(o.HaveOccurred())
	clusterinfra.GetAwsCredentialFromCluster(oc)
	awsClient := compat_otp.InitAwsSessionWithRegion(region)
	availableZones, err := awsClient.GetAvailabilityZoneNames()
	o.Expect(err).ShouldNot(o.HaveOccurred())
	return availableZones
}

// removeNodesTaint removes the node taint by taintKey if the node exists
func removeNodesTaint(oc *exutil.CLI, nodes []string, taintKey string) {
	for _, no := range nodes {
		nodeInfo := doOcpReq(oc, OcpGet, false, "no", no, "--ignore-not-found")
		if nodeInfo != "" {
			doOcpReq(oc, OcpAdm, false, "taint", "node", no, taintKey+"-")
		}
	}
}

// removeNodesLabel removes the node label by labelKey if the node exists
func removeNodesLabel(oc *exutil.CLI, nodes []string, labelKey string) {
	for _, no := range nodes {
		nodeInfo := doOcpReq(oc, OcpGet, false, "no", no, "--ignore-not-found")
		if nodeInfo != "" {
			doOcpReq(oc, OcpLabel, false, "node", no, labelKey+"-")
		}
	}
}

func getLatestUnsupportedOCPVersion() string {
	min := semver.MustParse(getMinSupportedOCPVersion())
	return semver.MustParse(subtractMinor(&min, uint64(1)).String()).String()
}

// remove z stream suffix 4.12.0 --> 4.12
func getVersionWithMajorAndMinor(version string) (string, error) {
	v := strings.Split(version, ".")
	if len(v) == 0 || len(v) > 3 {
		return "", fmt.Errorf("invalid version")
	}
	if len(v) < 3 {
		return version, nil
	} else {
		return strings.Join(v[:2], "."), nil
	}
}

// isRequestServingComponent determines if a deployment, replicaset or pod belongs to a serving component
func isRequestServingComponent(name string) bool {
	servingComponentRegex := regexp.MustCompile("^(kube-apiserver|ignition-server-proxy|oauth-openshift|router).*")
	return servingComponentRegex.MatchString(name)
}

// getTestCaseIDs extracts test case IDs from the Ginkgo nodes. Should be called within g.It.
func getTestCaseIDs() (testCaseIDs []string) {
	pattern := `-(\d{5,})-`
	re := regexp.MustCompile(pattern)
	for _, match := range re.FindAllStringSubmatch(g.CurrentSpecReport().FullText(), -1) {
		// Should be fulfilled all the time but just in case
		o.Expect(match).To(o.HaveLen(2))
		testCaseIDs = append(testCaseIDs, match[1])
	}
	o.Expect(testCaseIDs).NotTo(o.BeEmpty())
	return testCaseIDs
}

// getResourceNamePrefix generates a cloud resource name prefix by concatenating the first test case ID
// with a random string. The resulting string is safe to use as a prefix for cloud resource names.
func getResourceNamePrefix() string {
	return fmt.Sprintf("ocp%s-%s", getTestCaseIDs()[0], strings.ToLower(compat_otp.RandStr(4)))
}

func createTempDir(dir string) {
	g.DeferCleanup(func() {
		e2e.Logf("Removing temporary directory %s", dir)
		o.Expect(os.RemoveAll(dir)).NotTo(o.HaveOccurred(), "failed to remove temporary directory")
	})
	e2e.Logf("Creating temporary directory %s", dir)
	o.Expect(os.MkdirAll(dir, 0755)).NotTo(o.HaveOccurred(), "failed to create temporary directory")
}

func logHypershiftCLIVersion(c *CLI) {
	version, err := c.WithShowInfo(true).Run("hypershift version").Output()
	if err != nil {
		e2e.Logf("Failed to get hypershift CLI version: %v", err)
	}
	e2e.Logf("Found hypershift CLI version:\n%s", version)
}

func getNsCount(ctx context.Context, c kubernetes.Interface) (int, error) {
	nsList, err := c.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("error listing namespaces: %w", err)
	}
	return len(nsList.Items), nil
}

func createAndCheckNs(ctx context.Context, c kubernetes.Interface, logger *slog.Logger, numNsToCreate int, nsNamePrefix string) func() error {
	return func() error {
		g.GinkgoRecover()
		logger = logger.With("id", ctx.Value(ctxKeyId))

		nsCount, err := getNsCount(ctx, c)
		if err != nil {
			return fmt.Errorf("error counting namespaces: %v", err)
		}
		expectedNsCount := nsCount
		logger.Info("Got initial namespace count", "nsCount", nsCount)

		for i := 0; i < numNsToCreate; i++ {
			nsName := fmt.Sprintf("%s-%d", nsNamePrefix, i)
			logger.Info("Creating namespace", "nsName", nsName)

			if _, err = c.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("error creating namespace %v: %v", nsName, err)
			}
			expectedNsCount++

			switch nsCount, err = getNsCount(ctx, c); {
			case err != nil:
				return fmt.Errorf("error counting namespaces: %v", err)
			case nsCount != expectedNsCount:
				return fmt.Errorf("expect %v namespaces but found %v", expectedNsCount, nsCount)
			}
			time.Sleep(1 * time.Second)
		}
		return nil
	}
}

// Creates OCP resource by a yaml string representation.
// args can be specified like using a different kubeconfig before the -f arguments
func createOCPResourceByStr(oc *exutil.CLI, res string, args ...string) string {
	f, err := os.CreateTemp("", "hypershift-test-resources")
	o.Expect(err).NotTo(o.HaveOccurred())
	defer func() {
		if err = f.Close(); err != nil {
			e2e.Logf("Error closing file %s: %v", f.Name(), err)
		}
		if err = os.Remove(f.Name()); err != nil {
			e2e.Logf("Error removing file %s: %v", f.Name(), err)
		}
	}()

	// Write resources to file.
	_, err = f.WriteString(res)
	o.Expect(err).NotTo(o.HaveOccurred())
	err = f.Sync()
	o.Expect(err).NotTo(o.HaveOccurred())
	_args := []string{}
	_args = append(_args, args...)
	_args = append(_args, "-f", f.Name())

	return doOcpReq(oc, OcpCreate, false, _args...)
}

type pingPodResourceNode struct {
	name      string
	namespace string
	nodename  string
	template  string
}
type genericServiceResource struct {
	servicename           string
	namespace             string
	protocol              string
	selector              string
	serviceType           string
	ipFamilyPolicy        string
	externalTrafficPolicy string
	internalTrafficPolicy string
	template              string
}

func checkIPStackType(oc *exutil.CLI) string {
	svcNetwork, err := oc.WithoutNamespace().AsAdmin().Run("get").Args("network.operator", "cluster", "-o=jsonpath={.spec.serviceNetwork}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if strings.Count(svcNetwork, ":") >= 2 && strings.Count(svcNetwork, ".") >= 2 {
		return "dualstack"
	} else if strings.Count(svcNetwork, ":") >= 2 {
		return "ipv6single"
	} else if strings.Count(svcNetwork, ".") >= 2 {
		return "ipv4single"
	}
	return ""
}

func checkPodReady(oc *exutil.CLI, namespace string, podName string) (bool, error) {
	podOutPut, err := getPodStatus(oc, namespace, podName)
	status := []string{"Running", "Ready", "Complete", "Succeeded"}
	return contains(status, podOutPut), err
}

func (pod *pingPodResourceNode) createPingPodNode(oc *exutil.CLI) {
	err := wait.Poll(3*time.Second, 20*time.Second, func() (bool, error) {
		err1 := applyResourceFromTemplateByAdmin(oc, "--ignore-unknown-parameters=true", "-f", pod.template, "-p", "NAME="+pod.name, "NAMESPACE="+pod.namespace, "NODENAME="+pod.nodename)
		if err1 != nil {
			e2e.Logf("the err:%v, and try next round", err1)
			return false, nil
		}
		return true, nil
	})
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("fail to create pod %v", pod.name))
}

func applyResourceFromTemplateByAdmin(oc *exutil.CLI, parameters ...string) error {
	var configFile string
	err := wait.Poll(3*time.Second, 15*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().Run("process").Args(parameters...).OutputToFile(getRandomString() + "resource.json")
		if err != nil {
			e2e.Logf("the err:%v, and try next round", err)
			return false, nil
		}
		configFile = output
		return true, nil
	})
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("as admin fail to process %v", parameters))

	e2e.Logf("the file of resource is %s", configFile)
	return oc.WithoutNamespace().AsAdmin().Run("apply").Args("-f", configFile).Execute()
}

func getRandomString() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	buffer := make([]byte, 8)
	for index := range buffer {
		buffer[index] = chars[seed.Intn(len(chars))]
	}
	return string(buffer)
}

func (service *genericServiceResource) createServiceFromParams(oc *exutil.CLI) {
	err := wait.Poll(3*time.Second, 20*time.Second, func() (bool, error) {
		err1 := applyResourceFromTemplateByAdmin(oc, "--ignore-unknown-parameters=true", "-f", service.template, "-p", "SERVICENAME="+service.servicename, "NAMESPACE="+service.namespace, "PROTOCOL="+service.protocol, "SELECTOR="+service.selector, "serviceType="+service.serviceType, "ipFamilyPolicy="+service.ipFamilyPolicy, "internalTrafficPolicy="+service.internalTrafficPolicy, "externalTrafficPolicy="+service.externalTrafficPolicy)
		if err1 != nil {
			e2e.Logf("the err:%v, and try next round", err1)
			return false, nil
		}
		return true, nil
	})
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("fail to create svc %v", service.servicename))
}

func waitPodReady(oc *exutil.CLI, namespace string, podName string) {
	err := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		status, err1 := checkPodReady(oc, namespace, podName)
		if err1 != nil {
			e2e.Logf("the err:%v, wait for pod %v to become ready.", err1, podName)
			return status, err1
		}
		if !status {
			return status, nil
		}
		return status, nil
	})

	if err != nil {
		podDescribe := describePod(oc, namespace, podName)
		e2e.Logf("oc describe pod %v.", podName)
		e2e.Logf(podDescribe)
	}
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("pod %v is not ready", podName))
}

func describePod(oc *exutil.CLI, namespace string, podName string) string {
	podDescribe, err := oc.WithoutNamespace().Run("describe").Args("pod", "-n", namespace, podName).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("The pod  %s status is %q", podName, podDescribe)
	return podDescribe
}

func getPodStatus(oc *exutil.CLI, namespace string, podName string) (string, error) {
	podStatus, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, podName, "-o=jsonpath={.status.phase}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("The pod  %s status in namespace %s is %q", podName, namespace, podStatus)
	return podStatus, err
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func getPodIP(oc *exutil.CLI, namespace string, podName string) (string, string) {
	ipStack := checkIPStackType(oc)
	if (ipStack == "ipv6single") || (ipStack == "ipv4single") {
		podIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, podName, "-o=jsonpath={.status.podIPs[0].ip}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The pod  %s IP in namespace %s is %q", podName, namespace, podIP)
		return podIP, ""
	} else if ipStack == "dualstack" {
		podIP1, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, podName, "-o=jsonpath={.status.podIPs[1].ip}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The pod's %s 1st IP in namespace %s is %q", podName, namespace, podIP1)
		podIP2, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, podName, "-o=jsonpath={.status.podIPs[0].ip}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The pod's %s 2nd IP in namespace %s is %q", podName, namespace, podIP2)
		if netutils.IsIPv6String(podIP1) {
			e2e.Logf("This is IPv4 primary dual stack cluster")
			return podIP1, podIP2
		}
		e2e.Logf("This is IPv6 primary dual stack cluster")
		return podIP2, podIP1
	}
	return "", ""
}

func CurlPod2PodPass(oc *exutil.CLI, namespaceSrc string, podNameSrc string, namespaceDst string, podNameDst string) {
	podIP1, podIP2 := getPodIP(oc, namespaceDst, podNameDst)
	if podIP2 != "" {
		_, err := e2eoutput.RunHostCmd(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(podIP1, "8080"))
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = e2eoutput.RunHostCmd(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(podIP2, "8080"))
		o.Expect(err).NotTo(o.HaveOccurred())
	} else {
		_, err := e2eoutput.RunHostCmd(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(podIP1, "8080"))
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

func curlPod2PodPass(oc *exutil.CLI, namespaceSrc string, podNameSrc string, namespaceDst string, podNameDst string) {
	podIP1, podIP2 := getPodIP(oc, namespaceDst, podNameDst)
	if podIP2 != "" {
		_, err := e2eoutput.RunHostCmd(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(podIP1, "8080"))
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = e2eoutput.RunHostCmd(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(podIP2, "8080"))
		o.Expect(err).NotTo(o.HaveOccurred())
	} else {
		_, err := e2eoutput.RunHostCmd(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(podIP1, "8080"))
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

func curlPod2SvcPass(oc *exutil.CLI, namespaceSrc string, namespaceSvc string, podNameSrc string, svcName string) {
	svcIP1, svcIP2 := getSvcIP(oc, namespaceSvc, svcName)
	if svcIP2 != "" {
		_, err := e2eoutput.RunHostCmdWithRetries(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(svcIP1, "27017"), 3*time.Second, 15*time.Second)
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = e2eoutput.RunHostCmdWithRetries(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(svcIP2, "27017"), 3*time.Second, 15*time.Second)
		o.Expect(err).NotTo(o.HaveOccurred())
	} else {
		_, err := e2eoutput.RunHostCmdWithRetries(namespaceSrc, podNameSrc, "curl --connect-timeout 5 -s "+net.JoinHostPort(svcIP1, "27017"), 3*time.Second, 15*time.Second)
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

func getSvcIP(oc *exutil.CLI, namespace string, svcName string) (string, string) {
	ipStack := checkIPStackType(oc)
	svctype, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.type}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	ipFamilyType, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.ipFamilyPolicy}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if (svctype == "ClusterIP") || (svctype == "NodePort") {
		if (ipStack == "ipv6single") || (ipStack == "ipv4single") {
			svcIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.clusterIPs[0]}").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			if svctype == "ClusterIP" {
				e2e.Logf("The service %s IP in namespace %s is %q", svcName, namespace, svcIP)
				return svcIP, ""
			}
			nodePort, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.ports[*].nodePort}").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			e2e.Logf("The NodePort service %s IP and NodePort in namespace %s is %s %s", svcName, namespace, svcIP, nodePort)
			return svcIP, nodePort

		} else if (ipStack == "dualstack" && ipFamilyType == "PreferDualStack") || (ipStack == "dualstack" && ipFamilyType == "RequireDualStack") {
			ipFamilyPrecedence, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.ipFamilies[0]}").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			//if IPv4 is listed first in ipFamilies then clustrIPs allocation will take order as Ipv4 first and then Ipv6 else reverse
			svcIPv4, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.clusterIPs[0]}").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			e2e.Logf("The service %s IP in namespace %s is %q", svcName, namespace, svcIPv4)
			svcIPv6, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.clusterIPs[1]}").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			e2e.Logf("The service %s IP in namespace %s is %q", svcName, namespace, svcIPv6)
			/*As stated Nodeport type svc will return node port value in 2nd var. We don't care about what svc address is coming in 1st var as we evetually going to get
			node IPs later and use that in curl operation to node_ip:nodeport*/
			if ipFamilyPrecedence == "IPv4" {
				e2e.Logf("The ipFamilyPrecedence is Ipv4, Ipv6")
				switch svctype {
				case "NodePort":
					nodePort, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.ports[*].nodePort}").Output()
					o.Expect(err).NotTo(o.HaveOccurred())
					e2e.Logf("The Dual Stack NodePort service %s IP and NodePort in namespace %s is %s %s", svcName, namespace, svcIPv4, nodePort)
					return svcIPv4, nodePort
				default:
					return svcIPv6, svcIPv4
				}
			} else {
				e2e.Logf("The ipFamilyPrecedence is Ipv6, Ipv4")
				switch svctype {
				case "NodePort":
					nodePort, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.ports[*].nodePort}").Output()
					o.Expect(err).NotTo(o.HaveOccurred())
					e2e.Logf("The Dual Stack NodePort service %s IP and NodePort in namespace %s is %s %s", svcName, namespace, svcIPv6, nodePort)
					return svcIPv6, nodePort
				default:
					svcIPv4, svcIPv6 = svcIPv6, svcIPv4
					return svcIPv6, svcIPv4
				}
			}
		} else {
			//Its a Dual Stack Cluster with SingleStack ipFamilyPolicy
			svcIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, "-o=jsonpath={.spec.clusterIPs[0]}").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			e2e.Logf("The service %s IP in namespace %s is %q", svcName, namespace, svcIP)
			return svcIP, ""
		}
	} else {
		//Loadbalancer will be supported for single stack Ipv4 here for mostly GCP,Azure. We can take further enhancements wrt Metal platforms in Metallb utils later
		e2e.Logf("The serviceType is LoadBalancer")
		platform := compat_otp.CheckPlatform(oc)
		var jsonString string
		if platform == "aws" {
			jsonString = "-o=jsonpath={.status.loadBalancer.ingress[0].hostname}"
		} else {
			jsonString = "-o=jsonpath={.status.loadBalancer.ingress[0].ip}"
		}

		err := wait.Poll(30*time.Second, 300*time.Second, func() (bool, error) {
			svcIP, er := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, jsonString).Output()
			o.Expect(er).NotTo(o.HaveOccurred())
			if svcIP == "" {
				e2e.Logf("Waiting for lb service IP assignment. Trying again...")
				return false, nil
			}
			return true, nil
		})
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("fail to assign lb svc IP to %v", svcName))
		lbSvcIP, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace, svcName, jsonString).Output()
		e2e.Logf("The %s lb service Ingress VIP in namespace %s is %q", svcName, namespace, lbSvcIP)
		return lbSvcIP, ""
	}
}

// DistinctStrings removes duplicates from a slice of strings
func DistinctStrings(input []string) []string {
	seen := make(map[string]struct{})
	result := []string{}

	for _, val := range input {
		if _, ok := seen[val]; !ok {
			seen[val] = struct{}{}
			result = append(result, val)
		}
	}
	return result
}

// FindBySuffix tries to find the item with suffix, it returns "" if not found
func FindBySuffix(arr []string, suffix string) string {
	for _, s := range arr {
		if strings.HasSuffix(s, suffix) {
			return s
		}
	}
	return ""
}
