package extend

import (
	"context"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

var _ = g.Describe("[sig-hypershift] Hypershift", func() {
	var logger logr.Logger
	g.BeforeEach(func(ctx context.Context) {
		logger = ctrl.LoggerFrom(ctx)
		logger.Info("Prepare test environment...")
	})
	g.It("openshift-test-extension smoke test", func() {
		o.Expect(true).To(o.BeTrue())
	})
})
