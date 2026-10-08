package minecraft

import "testing"

const readyLine = `[20:26:34] [Server thread/INFO]: Done (0.439s)! For help, type "help"`

func TestParseLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantLevel   string
		wantMessage string
		wantOK      bool
	}{
		{
			name:        "ready line",
			line:        readyLine,
			wantLevel:   "INFO",
			wantMessage: `Done (0.439s)! For help, type "help"`,
			wantOK:      true,
		},
		{
			name:        "ordinary info line",
			line:        "[20:26:33] [Server thread/INFO]: Preparing spawn area: 100%",
			wantLevel:   "INFO",
			wantMessage: "Preparing spawn area: 100%",
			wantOK:      true,
		},
		{
			name:        "warn level",
			line:        "[20:26:33] [Server thread/WARN]: Can't keep up!",
			wantLevel:   "WARN",
			wantMessage: "Can't keep up!",
			wantOK:      true,
		},
		{
			name:        "different thread name",
			line:        "[20:26:33] [Worker-Main-1/INFO]: Loading chunks",
			wantLevel:   "INFO",
			wantMessage: "Loading chunks",
			wantOK:      true,
		},
		{
			name:        "chat line keeps player prefix in message",
			line:        "[20:26:34] [Server thread/INFO]: <Steve> hello",
			wantLevel:   "INFO",
			wantMessage: "<Steve> hello",
			wantOK:      true,
		},
		{
			name:   "no leading bracket",
			line:   `Exception in thread "main" java.lang.Error`,
			wantOK: false,
		},
		{
			name:   "stack trace line",
			line:   "\tat java.base/java.lang.Thread.run(Thread.java:1583)",
			wantOK: false,
		},
		{
			name:   "no slash",
			line:   "[12:00:00] no slash here: x",
			wantOK: false,
		},
		{
			name:   "no closing bracket after slash",
			line:   "[12:00:00] [main/INFO",
			wantOK: false,
		},
		{
			name:   "no colon after bracket",
			line:   "[12:00:00] [main/INFO] no colon",
			wantOK: false,
		},
		{
			name:   "empty string",
			line:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, message, ok := parseLine(tt.line)

			if ok != tt.wantOK {
				t.Fatalf("parseLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if level != tt.wantLevel {
				t.Errorf("parseLine(%q) level = %q, want %q", tt.line, level, tt.wantLevel)
			}
			if message != tt.wantMessage {
				t.Errorf("parseLine(%q) message = %q, want %q", tt.line, message, tt.wantMessage)
			}
		})
	}
}

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantType    EventType
		wantPlayer  string
		wantMessage string
		wantOK      bool
	}{
		// Server ready
		{
			name:        "ready line",
			line:        readyLine,
			wantType:    EventServerReady,
			wantMessage: `Done (0.439s)! For help, type "help"`,
			wantOK:      true,
		},
		{
			name:     "starts with Done but wrong ending",
			line:     "[20:26:34] [Server thread/INFO]: Done (0.439s)! something else",
			wantType: EventUnknown,
		},

		// Warnings and errors come from the level
		{
			name:        "warning",
			line:        "[20:26:33] [Server thread/WARN]: Can't keep up! Is the server overloaded?",
			wantType:    EventServerWarning,
			wantMessage: "Can't keep up! Is the server overloaded?",
			wantOK:      true,
		},
		{
			name:        "error",
			line:        "[20:26:33] [Server thread/ERROR]: Failed to save level",
			wantType:    EventServerError,
			wantMessage: "Failed to save level",
			wantOK:      true,
		},
		{
			name:        "ready wording at warn level is a warning",
			line:        `[20:26:34] [Server thread/WARN]: Done (0.439s)! For help, type "help"`,
			wantType:    EventServerWarning,
			wantMessage: `Done (0.439s)! For help, type "help"`,
			wantOK:      true,
		},

		// Joins and leaves
		{
			name:        "player joined",
			line:        "[20:30:01] [Server thread/INFO]: Steve joined the game",
			wantType:    EventPlayerJoined,
			wantPlayer:  "Steve",
			wantMessage: "Steve joined the game",
			wantOK:      true,
		},
		{
			name:        "player left",
			line:        "[20:31:12] [Server thread/INFO]: Steve left the game",
			wantType:    EventPlayerLeft,
			wantPlayer:  "Steve",
			wantMessage: "Steve left the game",
			wantOK:      true,
		},
		{
			name:     "join wording with a space in the name",
			line:     "[20:30:01] [Server thread/INFO]: Some One joined the game",
			wantType: EventUnknown,
		},

		// Chat
		{
			name:        "player chat",
			line:        "[20:30:05] [Server thread/INFO]: <Steve> hello",
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: "hello",
			wantOK:      true,
		},
		{
			name:        "chat containing a greater-than sign",
			line:        "[20:30:05] [Server thread/INFO]: <Steve> a > b",
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: "a > b",
			wantOK:      true,
		},
		{
			name:        "chat imitating ready is still chat",
			line:        `[20:26:34] [Server thread/INFO]: <Steve> Done (0.439s)! For help, type "help"`,
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: `Done (0.439s)! For help, type "help"`,
			wantOK:      true,
		},
		{
			name:        "chat imitating a join is chat, not a join",
			line:        "[20:30:05] [Server thread/INFO]: <Steve> Alex joined the game",
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: "Alex joined the game",
			wantOK:      true,
		},
		{
			name:        "chat imitating a leave is chat, not a leave",
			line:        "[20:30:05] [Server thread/INFO]: <Steve> Alex left the game",
			wantType:    EventPlayerChat,
			wantPlayer:  "Steve",
			wantMessage: "Alex left the game",
			wantOK:      true,
		},
		{
			name:     "name only, no chat text",
			line:     "[20:30:05] [Server thread/INFO]: <Steve>",
			wantType: EventUnknown,
		},
		{
			name:     "empty player name",
			line:     "[20:30:05] [Server thread/INFO]: <> hello",
			wantType: EventUnknown,
		},
		{
			name:     "space inside player name",
			line:     "[20:30:05] [Server thread/INFO]: <a b> hello",
			wantType: EventUnknown,
		},

		// Nothing to report
		{
			name:     "ordinary info line",
			line:     "[20:26:33] [Server thread/INFO]: Preparing spawn area: 100%",
			wantType: EventUnknown,
		},
		{
			name:     "no prefix",
			line:     `Exception in thread "main" java.lang.Error`,
			wantType: EventUnknown,
		},
		{
			name:     "empty string",
			line:     "",
			wantType: EventUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseEvent(tt.line)

			if ok != tt.wantOK {
				t.Fatalf("parseEvent(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if got.Type != tt.wantType {
				t.Errorf("parseEvent(%q) type = %v, want %v", tt.line, got.Type, tt.wantType)
			}
			if got.Player != tt.wantPlayer {
				t.Errorf("parseEvent(%q) player = %q, want %q", tt.line, got.Player, tt.wantPlayer)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("parseEvent(%q) message = %q, want %q", tt.line, got.Message, tt.wantMessage)
			}
			if tt.wantOK && got.Timestamp.IsZero() {
				t.Errorf("parseEvent(%q) timestamp is zero, want it set", tt.line)
			}
		})
	}
}
