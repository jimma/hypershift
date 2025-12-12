package extend

import (
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/openshift/hypershift/test/extend/util"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	"strings"
)

func doOcpReq(oc *util.CLI, verb OcpClientVerb, notEmpty bool, args ...string) string {
	g.GinkgoHelper()
	res, err := oc.AsAdmin().WithoutNamespace().Run(verb).Args(args...).Output()
	o.Expect(err).ShouldNot(o.HaveOccurred())
	if notEmpty {
		o.Expect(res).ShouldNot(o.BeEmpty())
	}
	return res
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
