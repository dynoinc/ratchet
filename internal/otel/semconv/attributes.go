// Standardized attribute keys and values for use in all OpenTelemetry signals
// Before adding a new attribute, first check to see if an attribute is already defined
// in the OpenTelemetry spec (https://opentelemetry.io/docs/specs/semconv/)
package semconv

import "go.opentelemetry.io/otel/attribute"

const (
	// Slack-specific attributes
	SlackChannelIDKey = attribute.Key("slack.channel.id")
	SlackTimestampKey = attribute.Key("slack.timestamp")
	SlackUserKey      = attribute.Key("slack.user")

	// Application-specific attributes
	ForceTraceKey          = attribute.Key("force_trace")
	ResponseMessageSizeKey = attribute.Key("response.message.size")
)
