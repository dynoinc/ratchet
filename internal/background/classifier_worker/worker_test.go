package classifier_worker

import (
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestClassifierWorker(t *testing.T) {
	expected := IncidentAction{
		Action:   ActionOpenIncident,
		Alert:    "fake-alert",
		Service:  "fake-service",
		Priority: PriorityHigh,
	}

	if os.Getenv("INCIDENT_BINARY_ACTION") == "OPEN" {
		if err := json.NewEncoder(os.Stdout).Encode(&expected); err != nil {
			log.Printf("Failed to encode incident action: %v", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("Failed to get executable path: %v", err)
	}

	t.Setenv("INCIDENT_BINARY_ACTION", "OPEN")
	got, err := runIncidentBinary(executable, dto.MessageAttrs{})
	require.NoError(t, err)
	require.Equal(t, expected, *got)
}
