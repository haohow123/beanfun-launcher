package alertsound

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestService_NoteDialogCancelled_Logs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := NewService(&fakeStore{prefs: Prefs{Selected: DefaultSound}})
	s.NoteDialogCancelled("cancelled by user")

	got := buf.String()
	if !strings.Contains(got, "file dialog closed without a selection") || !strings.Contains(got, `detail="cancelled by user"`) {
		t.Fatalf("log = %q, want the cancel message with its detail", got)
	}
}
