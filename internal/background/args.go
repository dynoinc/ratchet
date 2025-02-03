package background

type ClassifierArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (c ClassifierArgs) Kind() string {
	return "classifier"
}

type ChannelOnboardWorkerArgs struct {
	ChannelID string `json:"channel_id"`
}

func (c ChannelOnboardWorkerArgs) Kind() string {
	return "channel_board"
}
