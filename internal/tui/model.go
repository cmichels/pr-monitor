package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
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
	ReviewerStatus   string // "pending", "approved", "commented", "changes_requested"
	FirstSeen        time.Time
	Status           string
	LastActivityType string
	LastActivityBy   string
	LastActivityAt   time.Time
}

// PRDetail contains on-demand detail for a selected PR.
type PRDetail struct {
	Body     string
	Files    []FileChange
	Checks   []Check
	Comments []Comment
}

// Comment represents a PR comment or review.
type Comment struct {
	Author      string
	Body        string
	CreatedAt   time.Time
	ReviewState string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED", or "" for regular comments
}

// FileChange represents a single changed file in a PR.
type FileChange struct {
	Path      string
	Additions int
	Deletions int
}

// Check represents a CI check or status context on a PR.
type Check struct {
	Name       string
	Status     string // "completed", "in_progress", "queued"
	Conclusion string // "success", "failure", "neutral", "cancelled", "timed_out", etc.
}

// DetailFetcher fetches on-demand PR detail (body, files, checks).
type DetailFetcher interface {
	FetchDetail(ctx context.Context, prNodeID string) (*PRDetail, error)
}

// Model is the top-level Bubble Tea model for pr-monitor.
type Model struct {
	prLoader     PRLoader
	repoResolver RepoResolver
	dismisser    Dismisser

	keys      keyMap
	tabs      []string
	activeTab int
	lists     []list.Model
	width     int
	height    int
	err       error

	showHelp   bool
	statusText string
	errorText  string // inline error banner (auto-dismisses after 10s)
	shame      ShameConfig

	// Detail panel state
	detailFetcher  DetailFetcher
	detailCache    map[string]*PRDetail
	activeDetail   *PRDetail
	activeDetailID string
	detailLoading  bool
	detailErr      error
	detailViewport viewport.Model
	detailReady      bool
	detailFocused    bool // true when detail panel has keyboard focus
	commentsExpanded bool // true when comment bodies are fully shown
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

// PollErrorMsg is sent by the poll goroutine to display an inline error in the TUI.
// The TUI auto-dismisses the error after 10 seconds or on any keypress.
type PollErrorMsg struct {
	Text string
}

// clearErrorMsg dismisses the inline error banner.
type clearErrorMsg struct{}

// Option configures optional dependencies on the Model.
type Option func(*Model)

// WithDismisser sets the Dismisser implementation.
func WithDismisser(d Dismisser) Option {
	return func(m *Model) {
		m.dismisser = d
	}
}

// WithDetailFetcher sets the DetailFetcher for on-demand PR detail loading.
func WithDetailFetcher(f DetailFetcher) Option {
	return func(m *Model) {
		m.detailFetcher = f
	}
}

// New creates a new TUI model wired to the given data sources.
func New(loader PRLoader, resolver RepoResolver, shame ShameConfig, opts ...Option) Model {
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

	m := Model{
		prLoader:     loader,
		repoResolver: resolver,
		keys:         defaultKeyMap(),
		tabs:         tabs,
		activeTab:    0,
		lists:        []list.Model{reviewList, authorList},
		shame:        shame,
		detailCache:  make(map[string]*PRDetail),
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// Init returns a command to load initial data.
func (m Model) Init() tea.Cmd {
	return m.loadData()
}

// Update handles messages and returns the updated model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		// Any key dismisses the help overlay.
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// Any keypress dismisses inline error banner.
		if m.errorText != "" {
			m.errorText = ""
		}

		// Don't intercept keys while filtering.
		if m.lists[m.activeTab].FilterState() == list.Filtering {
			break
		}

		// When the detail panel has focus, route navigation keys there.
		if m.detailFocused {
			switch {
			case key.Matches(msg, m.keys.FocusList), msg.String() == "esc":
				m.detailFocused = false
				return m, nil
			case msg.String() == "j":
				m.detailViewport.ScrollDown(1)
				return m, nil
			case msg.String() == "k":
				m.detailViewport.ScrollUp(1)
				return m, nil
			case key.Matches(msg, m.keys.DetailDown):
				m.detailViewport.HalfPageDown()
				return m, nil
			case key.Matches(msg, m.keys.DetailUp):
				m.detailViewport.HalfPageUp()
				return m, nil
			case msg.String() == "G":
				m.detailViewport.GotoBottom()
				return m, nil
			case msg.String() == "g":
				m.detailViewport.GotoTop()
				return m, nil
			case msg.String() == "e":
				m.commentsExpanded = !m.commentsExpanded
				m.updateDetailViewport()
				return m, nil
			case msg.String() == "r":
				if m.activeDetailID != "" && m.detailFetcher != nil {
					delete(m.detailCache, m.activeDetailID)
					m.detailLoading = true
					m.detailErr = nil
					m.activeDetail = nil
					m.updateDetailViewport()
					return m, fetchDetail(m.detailFetcher, m.activeDetailID)
				}
				return m, nil
			}
			// Fall through for global keys (quit, help, tab, etc.)
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.showHelp = true
			return m, nil

		case key.Matches(msg, m.keys.Refresh):
			m.statusText = "Refreshing..."
			return m, tea.Batch(m.loadData(), clearStatusAfter(3*time.Second))

		case msg.String() == "tab":
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			m.detailFocused = false
			detailCmd := m.maybeLoadDetail()
			return m, detailCmd

		case msg.String() == "shift+tab":
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			m.detailFocused = false
			detailCmd := m.maybeLoadDetail()
			return m, detailCmd

		case key.Matches(msg, m.keys.FocusDetail):
			if m.detailReady && m.detailFetcher != nil {
				m.detailFocused = true
			}
			return m, nil

		case key.Matches(msg, m.keys.Review):
			if pr, ok := m.SelectedItem(); ok {
				return m, m.launchReview(pr)
			}
			return m, nil

		case key.Matches(msg, m.keys.Dismiss):
			if pr, ok := m.SelectedItem(); ok {
				return m, m.dismissPR(pr)
			}
			return m, nil

		case key.Matches(msg, m.keys.OpenBrowser):
			if pr, ok := m.SelectedItem(); ok {
				return m, openBrowser(pr.pr.URL)
			}
			return m, nil

		case key.Matches(msg, m.keys.DetailDown):
			if m.detailReady {
				m.detailViewport.HalfPageDown()
			}
			return m, nil

		case key.Matches(msg, m.keys.DetailUp):
			if m.detailReady {
				m.detailViewport.HalfPageUp()
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Reserve space for header (3 lines), footer (1 line), and status (1 line).
		listHeight := m.height - 5
		if listHeight < 1 {
			listHeight = 1
		}

		if m.detailFetcher != nil && m.width >= 80 {
			listWidth := m.width * 2 / 5
			detailWidth := m.width - listWidth - 1
			for i := range m.lists {
				m.lists[i].SetSize(listWidth, listHeight)
			}
			m.detailViewport = viewport.New(detailWidth, listHeight)
			m.detailReady = true
		} else {
			for i := range m.lists {
				m.lists[i].SetSize(m.width, listHeight)
			}
		}
		detailCmd := m.maybeLoadDetail()
		return m, detailCmd

	case prsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.lists[0].SetItems(toListItems(msg.reviewPRs))
		m.lists[1].SetItems(toListItems(msg.authoredPRs))
		detailCmd := m.maybeLoadDetail()
		return m, tea.Batch(
			tea.SetWindowTitle(m.windowTitle(len(msg.reviewPRs), len(msg.authoredPRs))),
			detailCmd,
		)

	case detailLoadedMsg:
		if msg.prNodeID != m.activeDetailID {
			return m, nil
		}
		m.detailLoading = false
		if msg.err != nil {
			m.detailErr = msg.err
			m.activeDetail = nil
		} else {
			m.detailErr = nil
			m.activeDetail = msg.detail
			m.detailCache[msg.prNodeID] = msg.detail
		}
		m.updateDetailViewport()
		return m, nil

	case RefreshMsg:
		return m, m.loadData()

	case dismissMsg:
		m.statusText = fmt.Sprintf("Dismissed PR %s", msg.prID)
		return m, tea.Batch(m.loadData(), clearStatusAfter(3*time.Second))

	case dismissErrMsg:
		m.statusText = fmt.Sprintf("Dismiss failed: %v", msg.err)
		return m, clearStatusAfter(3*time.Second)

	case statusMsg:
		m.statusText = msg.text
		return m, clearStatusAfter(3*time.Second)

	case clearStatusMsg:
		m.statusText = ""
		return m, nil

	case PollErrorMsg:
		m.errorText = msg.Text
		return m, tea.Tick(10*time.Second, func(time.Time) tea.Msg {
			return clearErrorMsg{}
		})

	case clearErrorMsg:
		m.errorText = ""
		return m, nil
	}

	// Delegate to the active list for navigation, filtering, etc.
	prevIdx := m.lists[m.activeTab].Index()
	var cmd tea.Cmd
	m.lists[m.activeTab], cmd = m.lists[m.activeTab].Update(msg)

	// If selection changed, trigger detail loading for the new item.
	if m.lists[m.activeTab].Index() != prevIdx {
		detailCmd := m.maybeLoadDetail()
		return m, tea.Batch(cmd, detailCmd)
	}
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

// maybeLoadDetail checks if a detail fetch is needed for the current selection.
// If the detail is cached, it updates the viewport immediately and returns nil.
// If not cached, it sets loading state and returns a fetchDetail command.
func (m *Model) maybeLoadDetail() tea.Cmd {
	if m.detailFetcher == nil || !m.detailReady {
		return nil
	}

	pr, ok := m.SelectedItem()
	if !ok {
		m.activeDetail = nil
		m.activeDetailID = ""
		m.detailLoading = false
		m.detailErr = nil
		m.updateDetailViewport()
		return nil
	}

	prID := pr.pr.PRID
	if prID == m.activeDetailID && !m.detailLoading {
		return nil
	}

	m.activeDetailID = prID

	if cached, ok := m.detailCache[prID]; ok {
		m.activeDetail = cached
		m.detailLoading = false
		m.detailErr = nil
		m.updateDetailViewport()
		return nil
	}

	m.detailLoading = true
	m.detailErr = nil
	m.activeDetail = nil
	m.updateDetailViewport()
	return fetchDetail(m.detailFetcher, prID)
}

// loadData returns a tea.Cmd that queries the PRLoader for both roles.
func (m Model) loadData() tea.Cmd {
	loader := m.prLoader
	shame := m.shame
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
			rItems[i] = NewPRItem(pr, shame)
		}

		aItems := make([]PRItem, len(authoredPRs))
		for i, pr := range authoredPRs {
			aItems[i] = NewPRItem(pr, shame)
		}

		return prsLoadedMsg{
			reviewPRs:   rItems,
			authoredPRs: aItems,
		}
	}
}

// windowTitle builds the terminal title string shown in the wezterm tab bar.
func (m Model) windowTitle(reviewCount, authoredCount int) string {
	return fmt.Sprintf("PR(%d:%d)", reviewCount, authoredCount)
}

// toListItems converts a slice of PRItem to a slice of list.Item.
func toListItems(items []PRItem) []list.Item {
	result := make([]list.Item, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}
