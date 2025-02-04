package dto

type OnboardingStatus string

const (
	OnboardingStatusStarted  OnboardingStatus = "started"
	OnboardingStatusFinished OnboardingStatus = "finished"
)

type ChannelAttrs struct {
	OnboardingStatus OnboardingStatus `json:"onboarding_status,omitzero"`
	Name             string           `json:"name,omitzero"`
}
