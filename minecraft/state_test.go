package minecraft

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateStopped, "stopped"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{StateCrashed, "crashed"},
		{State(99), "unknown"},
		{State(-1), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestStateNamesAreComplete(t *testing.T) {
	seen := make(map[string]State)

	for s := StateStopped; s <= StateCrashed; s++ {
		name := s.String()
		require.NotEqual(t, "unknown", name)

		_, dup := seen[name]
		require.False(t, dup)

		seen[name] = s
	}
}

func TestStateLive(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{StateStarting, true},
		{StateRunning, true},
		{StateStopping, false},
		{StateStopped, false},
		{StateCrashed, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.state.live())
		})
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	for _, want := range []State{StateStopped, StateStarting, StateRunning, StateStopping, StateCrashed} {
		data, err := json.Marshal(want)
		require.NoError(t, err)
		require.Equal(t, `"`+want.String()+`"`, string(data))

		var got State
		err = json.Unmarshal(data, &got)
		require.NoError(t, err)

		require.Equal(t, want, got)
	}
}

func TestStateUnmarshalUnknown(t *testing.T) {
	var s State
	err := json.Unmarshal([]byte(`"bogus"`), &s)
	require.Error(t, err)
}
