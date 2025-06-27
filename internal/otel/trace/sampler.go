package trace

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
)

// forceBasedSampler is a custom OpenTelemetry trace sampler that samples 100% of traces with force=true attribute
// and uses default sampling rate for all other traces
type forceBasedSampler struct {
	defaultSampler sdkTrace.Sampler
}

func NewForceBasedSampler(defaultSampleRate float64) sdkTrace.Sampler {
	return &forceBasedSampler{
		defaultSampler: sdkTrace.ParentBased(sdkTrace.TraceIDRatioBased(defaultSampleRate)),
	}
}

func (s *forceBasedSampler) ShouldSample(parameters sdkTrace.SamplingParameters) sdkTrace.SamplingResult {
	for _, attr := range parameters.Attributes {
		if attr.Key == "force" && attr.Value.AsBool() {
			return sdkTrace.SamplingResult{
				Decision:   sdkTrace.RecordAndSample,
				Attributes: []attribute.KeyValue{},
			}
		}
	}

	return s.defaultSampler.ShouldSample(parameters)
}


func (s *forceBasedSampler) Description() string {
	return fmt.Sprintf("ForceBasedSampler{default=%s}", s.defaultSampler.Description())
}