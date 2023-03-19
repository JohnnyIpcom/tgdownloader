package ctxlogger

import (
	"context"

	"go.uber.org/zap"
)

type (
	loggerField struct{}
	fieldsField struct{}
)

func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerField{}, logger)
}

func FromContext(ctx context.Context) *zap.Logger {
	logger, ok := ctx.Value(loggerField{}).(*zap.Logger)
	if !ok {
		return zap.NewNop()
	}

	fields := getFields(ctx)
	return logger.With(fields...)
}

func getFields(ctx context.Context) []zap.Field {
	fields := ctx.Value(fieldsField{})
	if fields != nil {
		return fields.([]zap.Field)
	}

	return []zap.Field{}
}

func WithField(ctx context.Context, key string, value interface{}) context.Context {
	fields := getFields(ctx)
	fields = append(fields, zap.Any(key, value))
	return context.WithValue(ctx, fieldsField{}, fields)
}

func WithFields(ctx context.Context, f ...zap.Field) context.Context {
	fields := getFields(ctx)
	fields = append(fields, f...)
	return context.WithValue(ctx, fieldsField{}, fields)
}
