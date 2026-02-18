package notify

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNotifyNewReview(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifier(&buf, true)

	pr := PR{
		Repo:   "org/my-repo",
		Number: 42,
		Title:  "Add widget support",
		Author: "alice",
	}

	err := n.NotifyNewReview(pr)
	assert.NoError(t, err)
	assert.Equal(t, "\033]9;PR Review: org/my-repo #42 — Add widget support (by @alice)\033\\", buf.String())
}

func TestNotifyActivity(t *testing.T) {
	tests := []struct {
		name         string
		activityType string
		wantPrefix   string
	}{
		{
			name:         "approved",
			activityType: "approved",
			wantPrefix:   "PR Approved",
		},
		{
			name:         "commented",
			activityType: "commented",
			wantPrefix:   "PR Comment",
		},
		{
			name:         "changes_requested",
			activityType: "changes_requested",
			wantPrefix:   "Changes Requested",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			n := NewNotifier(&buf, true)

			pr := PR{
				Repo:             "org/my-repo",
				Number:           99,
				Title:            "Fix login bug",
				LastActivityType: tt.activityType,
				LastActivityBy:   "bob",
			}

			err := n.NotifyActivity(pr)
			assert.NoError(t, err)

			want := "\033]9;" + tt.wantPrefix + ": org/my-repo #99 — Fix login bug (by @bob)\033\\"
			assert.Equal(t, want, buf.String())
		})
	}
}

func TestNotifyActivity_UnknownType(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifier(&buf, true)

	pr := PR{
		Repo:             "org/my-repo",
		Number:           7,
		Title:            "Some PR",
		LastActivityType: "unknown_type",
		LastActivityBy:   "charlie",
	}

	err := n.NotifyActivity(pr)
	assert.NoError(t, err)
	assert.Equal(t, "\033]9;PR Activity: org/my-repo #7 — Some PR (by @charlie)\033\\", buf.String())
}

func TestDisabledNotifier(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifier(&buf, false)

	pr := PR{
		Repo:   "org/my-repo",
		Number: 1,
		Title:  "Test PR",
		Author: "dev",
	}

	err := n.NotifyNewReview(pr)
	assert.NoError(t, err)
	assert.Empty(t, buf.String(), "disabled notifier should not emit anything")

	err = n.NotifyActivity(PR{
		Repo:             "org/my-repo",
		Number:           2,
		Title:            "Another PR",
		LastActivityType: "approved",
		LastActivityBy:   "reviewer",
	})
	assert.NoError(t, err)
	assert.Empty(t, buf.String(), "disabled notifier should not emit anything")
}
