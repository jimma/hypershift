package util

import (
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func NewLogger() logr.Logger {
	log := crzap.New(crzap.WriteTo(GinkgoWriter), crzap.UseDevMode(true), crzap.JSONEncoder(func(o *zapcore.EncoderConfig) {
		o.EncodeTime = zapcore.RFC3339TimeEncoder
	}))
	return log
}
