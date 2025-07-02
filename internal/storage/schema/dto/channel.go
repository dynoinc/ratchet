package dto

type onboardingStatus string

const (
	OnboardingStatusStarted  onboardingStatus = "started"
	OnboardingStatusFinished onboardingStatus = "finished"
)

type ChannelAttrs struct {
	OnboardingStatus onboardingStatus `json:"onboarding_status,omitzero"`
	Name             string           `json:"name,omitzero"`
}
