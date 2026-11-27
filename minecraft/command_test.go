package minecraft

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCommand(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "list", "list", false},
		{"surrounding whitespace", "  list  ", "list", false},
		{"trailing newline is trimmed", "list\n", "list", false},
		{"leading slash", "/list", "list", false},
		{"slash then space", "/ list", "list", false},
		{"whitespace before slash", "  /list", "list", false},
		{"only one slash is stripped", "//list", "/list", false},
		{"slash inside is untouched", "say a/b", "say a/b", false},
		{"non-ascii", "say héllo", "say héllo", false},
		{"at the length limit", strings.Repeat("a", maxCommandLength), strings.Repeat("a", maxCommandLength), false},
		{"slash does not count toward the limit", "/" + strings.Repeat("a", maxCommandLength), strings.Repeat("a", maxCommandLength), false},
		{"limit is in bytes, at the limit", strings.Repeat("é", maxCommandLength/2), strings.Repeat("é", maxCommandLength/2), false},

		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"bare slash", "/", "", true},
		{"slash and spaces", " /  ", "", true},
		{"over the limit", strings.Repeat("a", maxCommandLength+1), "", true},
		{"limit is in bytes, over the limit", strings.Repeat("é", maxCommandLength/2+1), "", true},
		{"embedded newline", "say hi\nstop", "", true},
		{"embedded carriage return", "say hi\rstop", "", true},
		{"embedded tab", "say\thi", "", true},
		{"bell character", "say \x07", "", true},
		{"nul byte", "say \x00", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCommand(tt.in)

			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidCommand)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
