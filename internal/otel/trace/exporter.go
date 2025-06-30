package trace

import (
	"context"

	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
)

// noOpSpanExporter is a minimal no-op implementation of sdkTrace.SpanExporter
// that does nothing and returns nil for all methods.
type noOpSpanExporter struct{}

func NewNoOpSpanExporter() sdkTrace.SpanExporter {
	return &noOpSpanExporter{}
}

func (noOpSpanExporter) ExportSpans(ctx context.Context, spans []sdkTrace.ReadOnlySpan) error {
	return nil
}

func (noOpSpanExporter) Shutdown(ctx context.Context) error {
	return nil
}
