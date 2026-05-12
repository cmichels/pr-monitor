package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockDriver records calls to TerminalDriver methods for test verification.
type mockDriver struct {
	spawnCalls          []mockSpawnCall
	spawnSessionCalls   []mockSpawnSessionCall
	setTitleCalls       []mockSetTitleCall
	sendTextCalls       []mockSendTextCall
	sendLineCalls       []mockSendLineCall
	spawnErr            error
	spawnSessionErr     error
	returnWindowID      string
}

type mockSpawnCall struct{ cwd string }
type mockSpawnSessionCall struct{ session, cwd string }
type mockSetTitleCall struct{ targetID, title string }
type mockSendTextCall struct{ targetID, text string }
type mockSendLineCall struct{ targetID, text string }

func (d *mockDriver) SpawnWindow(cwd string) (string, error) {
	d.spawnCalls = append(d.spawnCalls, mockSpawnCall{cwd: cwd})
	if d.spawnErr != nil {
		return "", d.spawnErr
	}
	id := d.returnWindowID
	if id == "" {
		id = "@1"
	}
	return id, nil
}

func (d *mockDriver) SpawnWindowInSession(session, cwd string) (string, error) {
	d.spawnSessionCalls = append(d.spawnSessionCalls, mockSpawnSessionCall{session: session, cwd: cwd})
	if d.spawnSessionErr != nil {
		return "", d.spawnSessionErr
	}
	id := d.returnWindowID
	if id == "" {
		id = "@1"
	}
	return id, nil
}

func (d *mockDriver) SetTitle(targetID, title string) error {
	d.setTitleCalls = append(d.setTitleCalls, mockSetTitleCall{targetID: targetID, title: title})
	return nil
}

func (d *mockDriver) SendText(targetID, text string) error {
	d.sendTextCalls = append(d.sendTextCalls, mockSendTextCall{targetID: targetID, text: text})
	return nil
}

func (d *mockDriver) SendLine(targetID, text string) error {
	d.sendLineCalls = append(d.sendLineCalls, mockSendLineCall{targetID: targetID, text: text})
	return nil
}

func TestMockDriver_ImplementsInterface(t *testing.T) {
	// Compile-time check that mockDriver satisfies TerminalDriver.
	var _ TerminalDriver = (*mockDriver)(nil)
}

func TestTmuxDriver_ImplementsInterface(t *testing.T) {
	// Compile-time check that TmuxDriver satisfies TerminalDriver.
	var _ TerminalDriver = (*TmuxDriver)(nil)
}

func TestSpawnWindowInSession_InterfaceContract(t *testing.T) {
	tests := []struct {
		name    string
		session string
		cwd     string
	}{
		{"creates session with cwd", "pr-review", "/home/user/repo"},
		{"creates session without cwd", "pr-review", ""},
		{"custom session name", "my-reviews", "/tmp/code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &mockDriver{returnWindowID: "@42"}
			id, err := d.SpawnWindowInSession(tt.session, tt.cwd)

			assert.NoError(t, err)
			assert.Equal(t, "@42", id)
			assert.Len(t, d.spawnSessionCalls, 1)
			assert.Equal(t, tt.session, d.spawnSessionCalls[0].session)
			assert.Equal(t, tt.cwd, d.spawnSessionCalls[0].cwd)
			// SpawnWindow should NOT have been called.
			assert.Empty(t, d.spawnCalls)
		})
	}
}

func TestSpawnWindow_UnaffectedBySessionMethod(t *testing.T) {
	d := &mockDriver{returnWindowID: "@10"}

	id, err := d.SpawnWindow("/some/path")

	assert.NoError(t, err)
	assert.Equal(t, "@10", id)
	assert.Len(t, d.spawnCalls, 1)
	assert.Equal(t, "/some/path", d.spawnCalls[0].cwd)
	// Session method should NOT have been called.
	assert.Empty(t, d.spawnSessionCalls)
}
