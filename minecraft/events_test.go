package minecraft

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantLevel   string
		wantMessage string
		wantOK      bool
	}{
		{"info", "[12:00:00] [Server thread/INFO]: hello", "INFO", "hello", true},
		{"warn", "[12:00:00] [Server thread/WARN]: careful", "WARN", "careful", true},
		{"error", "[12:00:00] [Server thread/ERROR]: boom", "ERROR", "boom", true},
		{"slash and bracket in message", "[12:00:00] [Server thread/INFO]: a/b]c", "INFO", "a/b]c", true},
		{"empty message", "[12:00:00] [Server thread/INFO]: ", "INFO", "", true},
		{"no leading bracket", "Steve joined the game", "", "", false},
		{"no slash", "[12:00:00] hello", "", "", false},
		{"no closing bracket", "[Server thread/INFO", "", "", false},
		{"missing colon and space", "[12:00:00] [Server thread/INFO] hello", "", "", false},
		{"empty line", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, message, ok := parseLine(tt.line)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantLevel, level)
			require.Equal(t, tt.wantMessage, message)
		})
	}
}

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantOK      bool
		wantType    EventType
		wantPlayer  string
		wantMessage string
	}{
		{
			name:        "server ready",
			line:        `[12:00:00] [Server thread/INFO]: Done (3.2s)! For help, type "help"`,
			wantOK:      true,
			wantType:    EventServerReady,
			wantMessage: `Done (3.2s)! For help, type "help"`,
		},
		{
			name:        "player joined",
			line:        "[12:00:01] [Server thread/INFO]: Steve joined the game",
			wantOK:      true,
			wantType:    EventPlayerJoined,
			wantPlayer:  "Steve",
			wantMessage: "Steve joined the game",
		},
		{
			name:        "player left",
			line:        "[12:00:02] [Server thread/INFO]: Steve left the game",
			wantOK:      true,
			wantType:    EventPlayerLeft,
			wantPlayer:  "Steve",
			wantMessage: "Steve left the game",
		},
		{
			name:        "player chat",
			line:        "[12:00:03] [Async Chat Thread - #0/INFO]: <Steve> hello world",
			wantOK:      true,
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: "hello world",
		},
		{
			name:        "warning",
			line:        "[12:00:04] [Server thread/WARN]: Can't keep up!",
			wantOK:      true,
			wantType:    EventServerWarning,
			wantMessage: "Can't keep up!",
		},
		{
			name:        "error",
			line:        "[12:00:05] [Server thread/ERROR]: Something broke",
			wantOK:      true,
			wantType:    EventServerError,
			wantMessage: "Something broke",
		},
		{
			name:        "warn level wins over join text",
			line:        "[12:00:06] [Server thread/WARN]: Steve joined the game",
			wantOK:      true,
			wantType:    EventServerWarning,
			wantMessage: "Steve joined the game",
		},
		{
			name:        "chat pretending to be a join is just chat",
			line:        "[12:00:07] [Async Chat Thread - #0/INFO]: <Steve> Alex joined the game",
			wantOK:      true,
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: "Alex joined the game",
		},
		{
			name:        "chat pretending to be ready is just chat",
			line:        `[12:00:08] [Async Chat Thread - #0/INFO]: <Steve> Done (1s)! For help, type "help"`,
			wantOK:      true,
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: `Done (1s)! For help, type "help"`,
		},
		{name: "ordinary info line", line: `[12:00:09] [Server thread/INFO]: Preparing level "world"`},
		{name: "not a log line", line: "Steve joined the game"},
		{name: "empty player name", line: "[12:00:10] [Server thread/INFO]:  joined the game"},
		{name: "player name with space", line: "[12:00:11] [Server thread/INFO]: Steve Smith joined the game"},
		{name: "chat with empty name", line: "[12:00:12] [Server thread/INFO]: <> hi"},
		{name: "chat without space after bracket", line: "[12:00:13] [Server thread/INFO]: <Steve>hi"},
		{name: "empty line", line: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ok := parseEvent(tt.line)
			require.Equal(t, tt.wantOK, ok)

			if !ok {
				return
			}

			require.Equal(t, tt.wantType, e.Type)
			require.Equal(t, tt.wantPlayer, e.Player)
			require.Equal(t, tt.wantMessage, e.Message)
			require.NotZero(t, e.Timestamp)
		})
	}
}

func TestEventTypeRoundTrip(t *testing.T) {
	for e := EventServerWarning; e <= EventBackupFailed; e++ {
		require.NotEqual(t, "unknown", e.String())

		text, err := e.MarshalText()
		require.NoError(t, err)

		var got EventType
		require.NoError(t, got.UnmarshalText(text))

		require.Equal(t, e, got)
	}
}

func TestEventTypeUnknown(t *testing.T) {
	require.Equal(t, "unknown", EventType(99).String())

	got := EventServerReady // make sure it gets overwritten
	err := got.UnmarshalText([]byte("nonsense"))
	require.NoError(t, err)
	require.Equal(t, EventUnknown, got)
}
