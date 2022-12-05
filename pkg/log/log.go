package log

import (
	"context"

	"github.com/samber/lo"
	"go.uber.org/zap"
	"knative.dev/pkg/logging"
)

func With(ctx context.Context) context.Context {
	return logging.WithLogger(ctx, lo.Must(zap.NewDevelopment(zap.AddStacktrace(zap.PanicLevel))).Sugar())
}

func From(ctx context.Context) *zap.SugaredLogger {
	return logging.FromContext(ctx)
}
