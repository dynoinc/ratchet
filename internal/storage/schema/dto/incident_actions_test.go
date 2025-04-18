package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDurationWrapperJSONRoundtrip(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedDur    time.Duration
		expectedOutput string
		wantErr        bool
	}{
		{
			name:           "numeric value",
			input:          "1000000000",
			expectedDur:    time.Second,
			expectedOutput: `"1s"`,
		},
		{
			name:           "string value",
			input:          `"1h30m"`,
			expectedDur:    90 * time.Minute,
			expectedOutput: `"1h30m0s"`,
		},
		{
			name:           "zero string",
			input:          `"0s"`,
			expectedDur:    0,
			expectedOutput: `"0s"`,
		},
		{
			name:           "zero numeric",
			input:          "0",
			expectedDur:    0,
			expectedOutput: `"0s"`,
		},
		{
			name:    "invalid type",
			input:   "{}",
			wantErr: true,
		},
		{
			name:    "invalid string format",
			input:   `"not-a-duration"`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Test unmarshaling
			var d durationWrapper
			err := json.Unmarshal([]byte(tc.input), &d)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.expectedDur, d.Duration)

			// Test marshaling (roundtrip)
			bytes, err := json.Marshal(d)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedOutput, string(bytes))
		})
	}
}
