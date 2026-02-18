package tui

import (
	"context"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// PRLoader abstracts the store for loading PRs.
type PRLoader interface {
	GetPendingByRole(ctx context.Context, role string) ([]PR, error)
}

// RepoResolver abstracts discover for resolving local repo paths.
type RepoResolver interface {
	Resolve(repo string) (string, bool)
}

// PR is the TUI's view of a pull request (maps from store.PR).
type PR struct {
	PRID             string
	Repo             string
	Number           int
	Title            string
	Author           string
	URL              string
	FilesChanged     int
	CIStatus         string
	FirstSeen        time.Time
	Status           string
	LastActivityType string
	LastActivityBy   string
	LastActivityAt   time.Time
}

// Model is the top-level Bubble Tea model for pr-monitor.
type Model struct {
	prLoader     PRLoader
	repoResolver RepoResolver

	tabs      []string
	activeTab int
	lists     []list.Model
	width     int
	height    int
	err       error
}

// prsLoadedMsg is returned by the data loading Cmd.
type prsLoadedMsg struct {
	reviewPRs   []PRItem
	authoredPRs []PRItem
	err         error
}

// RefreshMsg is sent by the poll goroutine (via program.Send) to tell the
// TUI that new data is available in the store.
type RefreshMsg struct{}

// New creates a new TUI model wired to the given data sources.
func New(loader PRLoader, resolver RepoResolver) Model {
	tabs := []string{"To Review", "My PRs"}

	delegate := list.NewDefaultDelegate()

	reviewList := list.New(nil, delegate, 0, 0)
	reviewList.Title = "To Review"
	reviewList.SetShowStatusBar(false)
	reviewList.SetFilteringEnabled(true)
	reviewList.SetShowHelp(false)

	authorList := list.New(nil, delegate, 0, 0)
	authorList.Title = "My PRs"
	authorList.SetShowStatusBar(false)
	authorList.SetFilteringEnabled(true)
	authorList.SetShowHelp(false)

	return Model{
		prLoader:     loader,
		repoResolver: resolver,
		tabs:         tabs,
		activeTab:    0,
		lists:        []list.Model{reviewList, authorList},
	}
}

// Init returns a command to load initial data.
func (m Model) Init() tea.Cmd {
	return m.loadData()
}

// Update handles messages and returns the updated model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		// Don't intercept keys while filtering.
		if m.lists[m.activeTab].FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			return m, nil
		case "shift+tab":
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve space for header (3 lines) and footer (2 lines).
		listHeight := m.height - 5
		if listHeight < 1 {
			listHeight = 1
		}
		for i := range m.lists {
			m.lists[i].SetSize(m.width, listHeight)
		}
		return m, nil

	case prsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.lists[0].SetItems(toListItems(msg.reviewPRs))
		m.lists[1].SetItems(toListItems(msg.authoredPRs))
		return m, nil

	case RefreshMsg:
		return m, m.loadData()
	}

	// Delegate to the active list for navigation, filtering, etc.
	var cmd tea.Cmd
	m.lists[m.activeTab], cmd = m.lists[m.activeTab].Update(msg)
	return m, cmd
}

// View renders the full TUI. Delegated to view.go's renderView.
func (m Model) View() string {
	return renderView(m)
}

// SelectedItem returns the currently selected PRItem, if any.
func (m Model) SelectedItem() (PRItem, bool) {
	item := m.lists[m.activeTab].SelectedItem()
	if item == nil {
		return PRItem{}, false
	}
	pr, ok := item.(PRItem)
	return pr, ok
}

// loadData returns a tea.Cmd that queries the PRLoader for both roles.
func (m Model) loadData() tea.Cmd {
	loader := m.prLoader
	return func() tea.Msg {
		ctx := context.Background()

		reviewPRs, err := loader.GetPendingByRole(ctx, "reviewer")
		if err != nil {
			return prsLoadedMsg{err: err}
		}

		authoredPRs, err := loader.GetPendingByRole(ctx, "author")
		if err != nil {
			return prsLoadedMsg{err: err}
		}

		// Sort authored PRs by most recent activity descending.
		sort.Slice(authoredPRs, func(i, j int) bool {
			return authoredPRs[i].LastActivityAt.After(authoredPRs[j].LastActivityAt)
		})

		// Review PRs come pre-sorted from store (oldest first via ORDER BY first_seen ASC).

		rItems := make([]PRItem, len(reviewPRs))
		for i, pr := range reviewPRs {
			rItems[i] = NewPRItem(pr)
		}

		aItems := make([]PRItem, len(authoredPRs))
		for i, pr := range authoredPRs {
			aItems[i] = NewPRItem(pr)
		}

		return prsLoadedMsg{
			reviewPRs:   rItems,
			authoredPRs: aItems,
		}
	}
}

// toListItems converts a slice of PRItem to a slice of list.Item.
func toListItems(items []PRItem) []list.Item {
	result := make([]list.Item, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}
