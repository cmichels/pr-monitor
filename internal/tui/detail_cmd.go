package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// detailLoadedMsg is sent when an async detail fetch completes.
type detailLoadedMsg struct {
	prNodeID string
	detail   *PRDetail
	err      error
}

// fetchDetail returns a tea.Cmd that fetches detail for the given PR node ID.
func fetchDetail(fetcher DetailFetcher, prNodeID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		detail, err := fetcher.FetchDetail(ctx, prNodeID)
		return detailLoadedMsg{
			prNodeID: prNodeID,
			detail:   detail,
			err:      err,
		}
	}
}
