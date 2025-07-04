package classifier

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestClassifierWorker(t *testing.T) {
	outputs := map[string]dto.IncidentAction{
		"OPEN_HIGH": {
			Action:   dto.ActionOpenIncident,
			Alert:    "fake-alert",
			Service:  "fake-service",
			Priority: dto.PriorityHigh,
		},
		"OPEN_LOW": {
			Action:   dto.ActionOpenIncident,
			Alert:    "fake-alert",
			Service:  "fake-service",
			Priority: dto.PriorityLow,
		},
		"CLOSE": {
			Action:  dto.ActionCloseIncident,
			Alert:   "fake-alert",
			Service: "fake-service",
		},
		"NONE": {
			Action: dto.ActionNone,
		},
	}

	if a, ok := outputs[os.Getenv("INCIDENT_BINARY_ACTION")]; ok {
		if err := json.NewEncoder(os.Stdout).Encode(&a); err != nil {
			slog.Error("failed to encode incident action", "error", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("Failed to get executable path: %v", err)
	}

	for testCase, expected := range outputs {
		t.Run(testCase, func(t *testing.T) {
			t.Setenv("INCIDENT_BINARY_ACTION", testCase)
			got, err := RunIncidentBinary(executable, "username", "text")
			require.NoError(t, err)
			require.Equal(t, expected, got)
		})
	}
}
