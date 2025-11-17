package qe_e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	o "github.com/onsi/gomega"
	exutil "github.com/openshift/origin/test/extended/util"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

type preStartJob struct {
	Name      string `json:"NAME"`
	Namespace string `json:"NAMESPACE"`
	CaseID    string `json:"CASEID"`
	Action    string `json:"ACTION"`
	TmpDir    string
}

func newPreStartJob(name string, namespace string, caseID string, action string, tmpDir string) *preStartJob {
	return &preStartJob{Name: name, Namespace: namespace, CaseID: caseID, Action: action, TmpDir: tmpDir}
}

func (p *preStartJob) create(oc *exutil.CLI) {
	out, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("secret", "generic", p.Name, "--from-file=KUBECONFIG="+os.Getenv("KUBECONFIG"), "-n", p.Namespace).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("create secret: %s, %s", p.Name, out)
	out, err = oc.AsAdmin().WithoutNamespace().Run("adm").Args("policy", "add-scc-to-user", "anyuid", "-z", "default", "-n", p.Namespace).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("oc adm policy: %s", out)
	defer compat_otp.RecoverNamespaceRestricted(oc, p.Namespace)
	compat_otp.SetNamespacePrivileged(oc, p.Namespace)

	preStartJobTemplate := filepath.Join(compat_otp.FixturePath("testdata", "hypershift"), "prestart-job.yaml")
	vars, err := parseTemplateVarParams(p)
	o.Expect(err).NotTo(o.HaveOccurred())

	var params = []string{"--ignore-unknown-parameters=true", "-f", preStartJobTemplate, "-p"}
	err = applyResourceFromTemplate(oc, "", p.Name+".yaml", append(params, vars...)...)
	o.Expect(err).NotTo(o.HaveOccurred())

	err = wait.Poll(LongTimeout/10, LongTimeout, func() (bool, error) {
		value, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("job", "-n", p.Namespace, p.Name, `-ojsonpath={.status.conditions[?(@.type=="Complete")].status}`).Output()
		return strings.Contains(value, "True"), nil
	})
	compat_otp.AssertWaitPollNoErr(err, "hyperShift operator PreStartJob error, log:"+p.getErrorLog(oc))
}

func (p *preStartJob) delete(oc *exutil.CLI) {
	oc.AsAdmin().WithoutNamespace().Run("delete").Args("secret", p.Name, "-n", p.Namespace).Output()
	oc.AsAdmin().WithoutNamespace().Run("delete").Args("job", p.Name, "-n", p.Namespace).Output()
}

func (p *preStartJob) getErrorLog(oc *exutil.CLI) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", p.Namespace, "-l", "job-name="+p.Name, `-ojsonpath={.items[0].metadata.name}`).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	logs, err := compat_otp.GetSpecificPodLogs(oc, p.Namespace, "prestart", podName, "\"Error\\|failed\\|error\"")
	if err != nil {
		return ""
	}
	return logs
}

func (p *preStartJob) preStartJobIP(oc *exutil.CLI) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", p.Namespace, "-l", "job-name="+p.Name, `-ojsonpath={.items[0].metadata.name}`).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	log, err := compat_otp.GetSpecificPodLogs(oc, p.Namespace, "prestart", podName, `"Your nodeport address is"`)
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("preStartJobIP,log: %s", log)

	// regex for ip
	numBlock := "(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])"
	regexPattern := numBlock + "\\." + numBlock + "\\." + numBlock + "\\." + numBlock

	regEx := regexp.MustCompile(regexPattern)
	return regEx.FindString(log)
}
