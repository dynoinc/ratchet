package dto

import (
	"encoding/json"
	"fmt"
	"time"
)

type action string
type priority string

const (
	ActionNone          action = "none"
	ActionOpenIncident  action = "open_incident"
	ActionCloseIncident action = "close_incident"

	PriorityHigh priority = "HIGH"
	PriorityLow  priority = "LOW"
)

func (a *action) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("failed to unmarshal action: %w", err)
	}

	switch s {
	case "none":
		*a = ActionNone
	case "open_incident":
		*a = ActionOpenIncident
	case "close_incident":
		*a = ActionCloseIncident
	default:
		return fmt.Errorf("unknown action: %s", s)
	}

	return nil
}

func (p *priority) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("failed to unmarshal priority: %w", err)
	}

	switch s {
	case "HIGH":
		*p = PriorityHigh
	case "LOW":
		*p = PriorityLow
	default:
		return fmt.Errorf("unknown priority: %s", s)
	}

	return nil
}

type IncidentAction struct {
	Action  action `json:"action"`
	Alert   string `json:"alert,omitzero"`
	Service string `json:"service,omitzero"`

	// Only used for open_incident.
	Priority priority `json:"priority,omitzero"`

	// Only used for close_incident.
	Duration durationWrapper `json:"duration,omitzero"`
}

type durationWrapper struct {
	time.Duration
}

func (d durationWrapper) IsZero() bool {
	return d.Duration == 0
}

func (d *durationWrapper) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		var err error
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("invalid duration type: %T", v)
	}
}

func (d durationWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}
