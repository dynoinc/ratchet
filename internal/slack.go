package internal

import (
	"fmt"

	"github.com/slack-go/slack"
)

func TestSlackAPIConnectivity(token string) error {
	api := slack.New(token)
	_, err := api.AuthTest()
	if err != nil {
		return fmt.Errorf("Slack API test failed: %v", err)
	}
	return nil
}
