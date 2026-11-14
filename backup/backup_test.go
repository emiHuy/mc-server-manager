package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/internal/fanout"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

// A realistic console line; waitFor only needs it to contain "Saved the game".
const flushDoneLine = "[12:00:00] [Server thread/INFO]: Saved the game"

var pauseAndResume = []string{"save-off", "save-all flush", "save-on"}

// fakeController stands in for minecraft.Instance.
// It records every command and, by default, "finishes" a flush by publishing
// the console line that waitFor is looking for.
type fakeController struct {
	hub   *fanout.Hub[string]
	state minecraft.State

	mu      sync.Mutex
	sent    []string
	sendErr map[string]error // command -> error to return
	onFlush func()           // runs when "save-all flush" is sent; nil publishes flushDoneLine
	events  []minecraft.Event
}

func newFakeController(state minecraft.State) *fakeController {
	return &fakeController{
		hub:     fanout.New[string]("console", 64, 0),
		state:   state,
		sendErr: make(map[string]error),
	}
}

func (f *fakeController) PublishEvent(e minecraft.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeController) Events() []minecraft.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.events)
}

// newTestManager returns a Manager with a fresh backup directory and no server.
// List, Delete and retention never touch the Controller, so nil is fine here.
func newTestManager(t *testing.T, keep int) *Manager {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	return New(nil, t.TempDir(), config.BackupConfig{Directory: dir, Keep: keep})
}

// writeBackup creates <id>.zip with the given number of bytes.
func writeBackup(t *testing.T, m *Manager, id string, size int) {
	t.Helper()

	err := os.WriteFile(filepath.Join(m.conf.Directory, id+".zip"), make([]byte, size), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func listIDs(t *testing.T, m *Manager) []string {
	t.Helper()

	infos, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	ids := make([]string, 0, len(infos))
	for _, info := range infos {
		ids = append(ids, info.ID)
	}
	return ids
}

func TestNewIDAndParseID(t *testing.T) {
	want := time.Date(2026, time.November, 12, 15, 30, 0, 0, time.UTC)

	id := newID(want)
	if id != "backup-20261112-153000" {
		t.Fatalf("newID = %q, want backup-20261112-153000", id)
	}
	if !idPattern.MatchString(id) {
		t.Fatalf("newID %q does not match idPattern", id)
	}

	got, err := parseID(id)
	if err != nil {
		t.Fatalf("parseID: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("parseID = %v, want %v", got, want)
	}
}

func TestNewIDUsesUTC(t *testing.T) {
	// 10:30 at UTC-5 is 15:30 UTC.
	local := time.Date(2026, time.November, 12, 10, 30, 0, 0, time.FixedZone("UTC-5", -5*60*60))

	if id := newID(local); id != "backup-20261112-153000" {
		t.Fatalf("newID = %q, want backup-20261112-153000", id)
	}
}

func TestListMissingDirectory(t *testing.T) {
	m := New(nil, t.TempDir(), config.BackupConfig{
		Directory: filepath.Join(t.TempDir(), "does-not-exist"),
		Keep:      3,
	})

	infos, err := m.List()
	if err != nil {
		t.Fatalf("List on a missing directory should not fail: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("got %d backups, want 0", len(infos))
	}
}

func TestListNewestFirstAndIgnoresStrays(t *testing.T) {
	m := newTestManager(t, 10)

	// Written out of order on purpose.
	writeBackup(t, m, "backup-20261112-120000", 10)
	writeBackup(t, m, "backup-20261114-090000", 30)
	writeBackup(t, m, "backup-20261113-180000", 20)

	// None of these should show up.
	strays := []string{
		"backup-20261115-000000.zip.tmp", // unfinished archive
		"notes.txt",
		"backup-bad.zip",
		"backup-2026111-120000.zip", // wrong digit count
	}
	for _, name := range strays {
		if err := os.WriteFile(filepath.Join(m.conf.Directory, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A directory that happens to look like a backup.
	if err := os.Mkdir(filepath.Join(m.conf.Directory, "backup-20260101-000000.zip"), 0o755); err != nil {
		t.Fatal(err)
	}

	infos, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	wantIDs := []string{
		"backup-20261114-090000",
		"backup-20261113-180000",
		"backup-20261112-120000",
	}
	var gotIDs []string
	for _, info := range infos {
		gotIDs = append(gotIDs, info.ID)
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("IDs = %v, want %v", gotIDs, wantIDs)
	}

	if infos[0].SizeBytes != 30 {
		t.Errorf("SizeBytes = %d, want 30", infos[0].SizeBytes)
	}
	wantTime := time.Date(2026, time.November, 14, 9, 0, 0, 0, time.UTC)
	if !infos[0].CreatedAt.Equal(wantTime) {
		t.Errorf("CreatedAt = %v, want %v", infos[0].CreatedAt, wantTime)
	}
}

func TestDelete(t *testing.T) {
	m := newTestManager(t, 10)
	const id = "backup-20261112-153000"
	writeBackup(t, m, id, 5)

	if err := m.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ids := listIDs(t, m); len(ids) != 0 {
		t.Fatalf("backup still listed after delete: %v", ids)
	}

	if err := m.Delete(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete error = %v, want ErrNotFound", err)
	}
}

func TestDeleteRejectsInvalidIDs(t *testing.T) {
	// Layout: root/x.zip is a file OUTSIDE the backup directory.
	// A traversal bug in Delete("../x") would remove it.
	root := t.TempDir()
	dir := filepath.Join(root, "backups")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "x.zip")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(nil, t.TempDir(), config.BackupConfig{Directory: dir, Keep: 3})

	ids := []string{
		"../x",
		"..",
		"",
		"backup-20261112-153000/../../x",
		"backup-20261112-153000.zip", // IDs have no extension
		`..\x`,
		"/etc/passwd",
		"backup-20261112-153000\x00",
	}
	for _, id := range ids {
		if err := m.Delete(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Delete(%q) error = %v, want ErrInvalidID", id, err)
		}
	}

	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the backup directory was touched: %v", err)
	}
}

func TestEnforceRetentionKeepsNewest(t *testing.T) {
	m := newTestManager(t, 2)

	for _, id := range []string{
		"backup-20261110-000000",
		"backup-20261111-000000",
		"backup-20261112-000000",
		"backup-20261113-000000",
		"backup-20261114-000000",
	} {
		writeBackup(t, m, id, 1)
	}
	// An unfinished archive must be left alone by retention.
	tmp := filepath.Join(m.conf.Directory, "backup-20261115-000000.zip.tmp")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.enforceRetention(); err != nil {
		t.Fatalf("enforceRetention: %v", err)
	}

	want := []string{"backup-20261114-000000", "backup-20261113-000000"}
	if got := listIDs(t, m); !slices.Equal(got, want) {
		t.Fatalf("after retention IDs = %v, want %v", got, want)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("retention removed the .tmp file: %v", err)
	}
}

func TestEnforceRetentionUnderLimit(t *testing.T) {
	m := newTestManager(t, 5)
	writeBackup(t, m, "backup-20261112-000000", 1)
	writeBackup(t, m, "backup-20261113-000000", 1)

	if err := m.enforceRetention(); err != nil {
		t.Fatalf("enforceRetention: %v", err)
	}
	if got := listIDs(t, m); len(got) != 2 {
		t.Fatalf("got %d backups, want 2 untouched", len(got))
	}
}

// emit puts a line on the fake console.
// NOTE: if your fanout.Hub publish method is not called Publish, this is the only line to change.
func (f *fakeController) emit(line string) {
	f.hub.Publish(line)
}

func (f *fakeController) Send(cmd string) error {
	f.mu.Lock()
	f.sent = append(f.sent, cmd)
	err := f.sendErr[cmd]
	onFlush := f.onFlush
	f.mu.Unlock()

	if err != nil {
		return err
	}
	if cmd == "save-all flush" {
		if onFlush != nil {
			onFlush()
		} else {
			f.emit(flushDoneLine)
		}
	}
	return nil
}

func (f *fakeController) State() minecraft.State {
	return f.state
}

func (f *fakeController) SubscribeConsole(n int) ([]string, *fanout.Subscription[string]) {
	return f.hub.Subscribe(n)
}

// Sent returns a copy of the commands received so far.
func (f *fakeController) Sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent)
}

// newCreateManager returns a Manager that reads from sourceDir and writes to a fresh temp directory.
func newCreateManager(t *testing.T, ctl Controller, sourceDir string, keep int) *Manager {
	t.Helper()

	return New(ctl, sourceDir, config.BackupConfig{
		Directory:     filepath.Join(t.TempDir(), "backups"),
		Keep:          keep,
		FoldersToSave: []string{"world"},
		FlushTimeout:  2 * time.Second,
	})
}

func TestCreateRunningServer(t *testing.T) {
	ctl := newFakeController(minecraft.StateRunning)
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, ctl, src, 3)

	info, err := m.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !idPattern.MatchString(info.ID) {
		t.Errorf("ID %q does not match idPattern", info.ID)
	}
	if got := ctl.Sent(); !slices.Equal(got, pauseAndResume) {
		t.Errorf("commands = %v, want %v", got, pauseAndResume)
	}
	if got := listIDs(t, m); !slices.Equal(got, []string{info.ID}) {
		t.Errorf("List = %v, want [%s]", got, info.ID)
	}
}

func TestCreateStoppedServerSendsNothing(t *testing.T) {
	ctl := newFakeController(minecraft.StateStopped)
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, ctl, src, 3)

	info, err := m.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := ctl.Sent(); len(got) != 0 {
		t.Errorf("a stopped server should receive no commands, got %v", got)
	}
	if got := listIDs(t, m); !slices.Equal(got, []string{info.ID}) {
		t.Errorf("List = %v, want [%s]", got, info.ID)
	}
}

func TestCreateRejectsConcurrentBackup(t *testing.T) {
	ctl := newFakeController(minecraft.StateRunning)
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, ctl, src, 3)

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock) // never leave the first Create stuck if the test fails early

	// The first backup stalls inside the flush until we release it.
	ctl.onFlush = func() {
		close(started)
		<-release
		ctl.emit(flushDoneLine)
	}

	done := make(chan error, 1)
	go func() {
		_, err := m.Create(context.Background())
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first backup never reached the flush")
	}

	if _, err := m.Create(context.Background()); !errors.Is(err, ErrInProgress) {
		t.Fatalf("second Create error = %v, want ErrInProgress", err)
	}
	// The rejected call must not have touched the server.
	if got := ctl.Sent(); !slices.Equal(got, []string{"save-off", "save-all flush"}) {
		t.Fatalf("commands while first backup runs = %v", got)
	}

	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first Create: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first backup never finished")
	}
}

func TestCreateSendsSaveOnAfterFailedArchive(t *testing.T) {
	ctl := newFakeController(minecraft.StateRunning)
	src := makeSource(t, map[string]string{"server.properties": "props"}) // no world folder, so archiving fails
	m := newCreateManager(t, ctl, src, 3)

	if _, err := m.Create(context.Background()); err == nil {
		t.Fatal("expected Create to fail when the world folder is missing")
	}

	if got := ctl.Sent(); !slices.Equal(got, pauseAndResume) {
		t.Errorf("commands = %v, want %v (save-on must still be sent)", got, pauseAndResume)
	}
	if names := dirEntryNames(t, m.conf.Directory); len(names) != 0 {
		t.Errorf("failed backup left files behind: %v", names)
	}
}

func TestCreateSendsSaveOnAfterFlushTimeout(t *testing.T) {
	ctl := newFakeController(minecraft.StateRunning)
	ctl.onFlush = func() {} // the server never says "Saved the game"
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, ctl, src, 3)
	m.conf.FlushTimeout = 50 * time.Millisecond

	_, err := m.Create(context.Background())
	if !errors.Is(err, ErrFlushTimeout) {
		t.Fatalf("Create error = %v, want ErrFlushTimeout", err)
	}

	if got := ctl.Sent(); !slices.Equal(got, pauseAndResume) {
		t.Errorf("commands = %v, want %v", got, pauseAndResume)
	}
	if got := listIDs(t, m); len(got) != 0 {
		t.Errorf("no backup should exist after a timeout, got %v", got)
	}
}

func TestCreateSendsSaveOnAfterContextCancelled(t *testing.T) {
	ctl := newFakeController(minecraft.StateRunning)
	ctl.onFlush = func() {}
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, ctl, src, 3)
	m.conf.FlushTimeout = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // e.g. the HTTP client hung up

	_, err := m.Create(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create error = %v, want context.Canceled", err)
	}
	if got := ctl.Sent(); !slices.Equal(got, pauseAndResume) {
		t.Errorf("commands = %v, want %v", got, pauseAndResume)
	}
}

func TestCreateSaveOffFailureSkipsSaveOn(t *testing.T) {
	ctl := newFakeController(minecraft.StateRunning)
	ctl.sendErr["save-off"] = errors.New("stdin closed")
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, ctl, src, 3)

	if _, err := m.Create(context.Background()); err == nil {
		t.Fatal("expected Create to fail when save-off fails")
	}

	// Autosave was never turned off, so there is nothing to turn back on.
	if got := ctl.Sent(); !slices.Equal(got, []string{"save-off"}) {
		t.Errorf("commands = %v, want [save-off]", got)
	}
}

func TestCreateRunsRetention(t *testing.T) {
	ctl := newFakeController(minecraft.StateStopped)
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, ctl, src, 1)

	if err := os.MkdirAll(m.conf.Directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBackup(t, m, "backup-20200101-000000", 1)

	info, err := m.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := listIDs(t, m); !slices.Equal(got, []string{info.ID}) {
		t.Fatalf("after Create with keep=1, List = %v, want only [%s]", got, info.ID)
	}
}
