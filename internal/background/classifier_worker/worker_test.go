package classifier_worker

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifierWorker(t *testing.T) {
	outputs := map[string]incidentAction{
		"OPEN_HIGH": {
			Action:   actionOpenIncident,
			Alert:    "fake-alert",
			Service:  "fake-service",
			Priority: priorityHigh,
		},
		"OPEN_LOW": {
			Action:   actionOpenIncident,
			Alert:    "fake-alert",
			Service:  "fake-service",
			Priority: priorityLow,
		},
		"CLOSE": {
			Action:  actionCloseIncident,
			Alert:   "fake-alert",
			Service: "fake-service",
		},
		"NONE": {
			Action: actionNone,
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
			got, err := runIncidentBinary(executable, "username", "text")
			require.NoError(t, err)
			require.Equal(t, expected, *got)
		})
	}
}
