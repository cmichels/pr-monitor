package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PRLoader abstracts the store for loading PRs.
type PRLoader interface {
	GetPendingByRole(ctx context.Context, role string) ([]PR, error)
	GetDismissedByRole(ctx context.Context, role string) ([]PR, error)
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
	IsDraft          bool
	FirstSeen        time.Time
	Status           string
	LastActivityType string
	LastActivityBy   string
	LastActivityAt   time.Time
}

// ReviewStatus represents the latest review state for a single reviewer.
type ReviewStatus struct {
	Author string
	State  string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "PENDING"
}

// PRDetail contains on-demand detail for a selected PR.
type PRDetail struct {
	Body     string
	Files    []FileChange
	Checks   []Check
	Comments []Comment
	Reviews  []ReviewStatus
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
	undismisser  Undismisser

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
	spinner    spinner.Model

	// Per-source loading state — true from Init/Refresh until data arrives.
	prsLoading    bool
	jiraLoading   bool
	epicLoading   bool
	sprintLoading bool

	// Stacked section state (tab 0: To Review)
	reviewSection              int  // 0=pending, 1=reviewed, 2=dismissed
	pendingCollapsed           bool
	reviewedCollapsed          bool
	dismissedReviewerCollapsed bool

	// Stacked section state (tab 1: My PRs)
	myPRsSection              int  // 0=active, 1=drafts, 2=dismissed
	activeCollapsed           bool
	draftsCollapsed           bool
	dismissedAuthoredCollapsed bool

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

	// Stats tab state (tab 2)
	statsLoader      StatsLoader
	statsReady       bool
	statsProgress    StatsProgressMsg
	statsViewMode    statsViewMode
	statsUsers       []string // alphabetically sorted logins
	statsUserIdx     int
	statsGranularity statsGranularity
	statsViewport    viewport.Model
	statsData        *StatsData

	// Jira tab state (tab 3)
	jiraLoader        JiraLoader
	jiraDetailFetcher JiraDetailFetcher
	jiraClaimer       JiraClaimer
	jiraSection       int       // 0=in_progress, 1=submissions, 2=knowledge, 3=my_tasks
	jiraCollapsed     [4]bool
	jiraDetail        *JiraDetail
	jiraDetailKey     string
	jiraDetailLoading bool
	jiraDetailErr     error
	jiraDetailCache   map[string]*JiraDetail
	jiraBaseURL       string
	jiraSrcKeys       [3]string

	// Current Jira user (inferred from my_tasks assignee).
	currentUser string

	// Epics tab state (tab 4) — dynamic N sections, separate from lists[]
	epicLoader     EpicLoader
	epicManager    EpicManager
	epicSection    int
	epicSections   []EpicSection
	epicLists      []list.Model
	epicCollapsed  []bool
	epicStats      []EpicStats
	epicPartitions []epicPartition // per-section partitioned items
	epicShowOthers []bool          // per-section: true = show items assigned to others

	// Epic add prompt state
	epicAddInput  textinput.Model
	epicAddActive bool // true when the text input prompt is visible

	// Sprint tab state (tab 5) — single section with assignee partitioning
	sprintLoader     SprintLoader
	sprintList       list.Model
	sprintPartition  epicPartition // reuses the same mine/unassigned/others partition
	sprintShowOthers bool
	sprintName       string
	sprintStats      SprintStats
}

// prsLoadedMsg is returned by the data loading Cmd.
type prsLoadedMsg struct {
	reviewPRs          []PRItem
	authoredPRs        []PRItem
	dismissedReviewer  []PRItem
	dismissedAuthored  []PRItem
	err                error
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

// WithUndismisser sets the Undismisser implementation.
func WithUndismisser(u Undismisser) Option {
	return func(m *Model) {
		m.undismisser = u
	}
}

// WithDetailFetcher sets the DetailFetcher for on-demand PR detail loading.
func WithDetailFetcher(f DetailFetcher) Option {
	return func(m *Model) {
		m.detailFetcher = f
	}
}

// WithJiraLoader sets the JiraLoader implementation for the Jira tab.
func WithJiraLoader(l JiraLoader) Option {
	return func(m *Model) {
		m.jiraLoader = l
	}
}

// WithJiraDetailFetcher sets the JiraDetailFetcher for on-demand Jira detail loading.
func WithJiraDetailFetcher(f JiraDetailFetcher) Option {
	return func(m *Model) {
		m.jiraDetailFetcher = f
	}
}

// WithJiraClaimer sets the JiraClaimer for claiming Jira issues.
func WithJiraClaimer(c JiraClaimer) Option {
	return func(m *Model) {
		m.jiraClaimer = c
	}
}

// WithJiraBaseURL sets the Jira base URL for building browse URLs.
func WithJiraBaseURL(url string) Option {
	return func(m *Model) {
		m.jiraBaseURL = url
	}
}

// WithJiraSourceKeys sets the store source keys for the 3 Jira sections.
// Keys should be derived from config (e.g. "filter:13066", "filter:12562", "my_tasks").
func WithJiraSourceKeys(keys [3]string) Option {
	return func(m *Model) {
		m.jiraSrcKeys = keys
	}
}

// WithEpicLoader sets the EpicLoader implementation for the Epics tab.
func WithEpicLoader(l EpicLoader) Option {
	return func(m *Model) {
		m.epicLoader = l
	}
}

// WithEpicManager sets the EpicManager for interactive epic management (add/toggle/remove).
func WithEpicManager(mgr EpicManager) Option {
	return func(m *Model) {
		m.epicManager = mgr
	}
}

// WithSprintLoader sets the SprintLoader implementation for the Sprint tab.
func WithSprintLoader(l SprintLoader) Option {
	return func(m *Model) {
		m.sprintLoader = l
	}
}

// New creates a new TUI model wired to the given data sources.
func New(loader PRLoader, resolver RepoResolver, shame ShameConfig, opts ...Option) Model {
	tabs := []string{"To Review", "My PRs", "Stats", "Jira", "Epics", "Sprint"}

	delegate := list.NewDefaultDelegate()

	jiraDelegate := list.NewDefaultDelegate()
	jiraDelegate.SetSpacing(0)

	newList := func(d list.DefaultDelegate) list.Model {
		l := list.New(nil, d, 0, 0)
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(true)
		l.SetShowHelp(false)
		return l
	}

	// lists[0]=pending, [1]=reviewed, [2]=active, [3]=drafts,
	// [4]=jira in-progress, [5]=jira submissions, [6]=jira knowledge, [7]=jira my tasks,
	// [8]=dismissed reviewer, [9]=dismissed authored
	lists := make([]list.Model, 10)
	for i := 0; i < 4; i++ {
		lists[i] = newList(delegate)
	}
	for i := 4; i < 8; i++ {
		lists[i] = newList(jiraDelegate)
	}
	for i := 8; i < 10; i++ {
		lists[i] = newList(delegate)
	}

	// Initialize loading spinner.
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Initialize sprint list with epic delegate.
	sprintDelegate := newEpicDelegate()
	sprintList := list.New(nil, sprintDelegate, 0, 0)
	sprintList.SetShowTitle(false)
	sprintList.SetShowStatusBar(false)
	sprintList.SetFilteringEnabled(true)
	sprintList.SetShowHelp(false)

	ti := textinput.New()
	ti.Placeholder = "PROJ-1234"
	ti.CharLimit = 30
	ti.Width = 30

	m := Model{
		prLoader:                   loader,
		repoResolver:               resolver,
		keys:                       defaultKeyMap(),
		tabs:                       tabs,
		activeTab:                  0,
		lists:                      lists,
		shame:                      shame,
		detailCache:                make(map[string]*PRDetail),
		jiraDetailCache:            make(map[string]*JiraDetail),
		dismissedReviewerCollapsed: true,
		dismissedAuthoredCollapsed: true,
		epicAddInput:               ti,
		sprintList:                 sprintList,
		spinner:                    s,
		prsLoading:                 true,
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// activeListIndex returns the index into m.lists for the currently focused list.
// Tab 0: 0=pending, 1=reviewed, 2=dismissed reviewer (lists[8])
// Tab 1: 0=active, 1=drafts, 2=dismissed authored (lists[9])
// Tab 2 (stats) doesn't use lists but falls back to lists[2] for safety.
func (m Model) activeListIndex() int {
	switch m.activeTab {
	case 0:
		if m.reviewSection == 2 {
			return 8
		}
		return m.reviewSection
	case 1:
		if m.myPRsSection == 2 {
			return 9
		}
		return 2 + m.myPRsSection
	case 3:
		return 4 + m.jiraSection
	default:
		return 2
	}
}

// Init returns a command to load initial data.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadData(), m.spinner.Tick}
	if m.jiraLoader != nil {
		m.jiraLoading = true
		cmds = append(cmds, m.loadJiraData())
	}
	if m.epicLoader != nil {
		m.epicLoading = true
		cmds = append(cmds, m.loadEpicData())
	}
	if m.sprintLoader != nil {
		m.sprintLoading = true
		cmds = append(cmds, m.loadSprintData())
	}
	return tea.Batch(cmds...)
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

		// Epic add input mode: route all keys to the text input.
		if m.epicAddActive {
			switch msg.Type {
			case tea.KeyEsc:
				m.epicAddActive = false
				m.epicAddInput.Reset()
				return m, nil
			case tea.KeyEnter:
				epicKey := strings.TrimSpace(m.epicAddInput.Value())
				m.epicAddActive = false
				m.epicAddInput.Reset()
				if epicKey == "" {
					return m, nil
				}
				if m.epicManager == nil {
					m.statusText = "Epic manager not configured"
					return m, clearStatusAfter(3 * time.Second)
				}
				m.statusText = fmt.Sprintf("Adding %s...", epicKey)
				return m, addEpic(m.epicManager, epicKey, "")
			default:
				var cmd tea.Cmd
				m.epicAddInput, cmd = m.epicAddInput.Update(msg)
				return m, cmd
			}
		}

		// Any keypress dismisses inline error banner.
		if m.errorText != "" {
			m.errorText = ""
		}

		// Don't intercept keys while filtering (stats/epics/sprint tabs use their own lists).
		if m.activeTab != 2 && m.activeTab != 4 && m.activeTab != 5 && m.lists[m.activeListIndex()].FilterState() == list.Filtering {
			break
		}
		if m.activeTab == 4 && len(m.epicLists) > 0 && m.epicSection < len(m.epicLists) &&
			m.epicLists[m.epicSection].FilterState() == list.Filtering {
			break
		}
		if m.activeTab == 5 && m.sprintList.FilterState() == list.Filtering {
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
				if (m.activeTab == 3 || m.activeTab == 4 || m.activeTab == 5) && m.jiraDetailKey != "" && m.jiraDetailFetcher != nil {
					delete(m.jiraDetailCache, m.jiraDetailKey)
					m.jiraDetailLoading = true
					m.jiraDetailErr = nil
					m.jiraDetail = nil
					m.updateDetailViewport()
					return m, fetchJiraDetail(m.jiraDetailFetcher, m.jiraDetailKey)
				}
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

		// Jira tab keybindings (when on tab 3).
		if m.activeTab == 3 {
			switch {
			case key.Matches(msg, m.keys.SectionSwitch):
				m.jiraSection = (m.jiraSection + 1) % 4
				detailCmd := m.maybeLoadJiraDetail()
				return m, detailCmd

			case key.Matches(msg, m.keys.CollapseToggle):
				m.jiraCollapsed[m.jiraSection] = !m.jiraCollapsed[m.jiraSection]
				m.resizeJiraSections()
				return m, nil

			case key.Matches(msg, m.keys.Review):
				if item, ok := m.SelectedJiraItem(); ok {
					return m, launchWorktree(item)
				}
				return m, nil

			case key.Matches(msg, m.keys.OpenBrowser):
				if item, ok := m.SelectedJiraItem(); ok {
					url := item.issue.BrowseURL
					if url == "" && m.jiraBaseURL != "" {
						url = m.jiraBaseURL + "/browse/" + item.issue.Key
					}
					if url != "" {
						return m, openBrowser(url)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.CopyURL):
				if item, ok := m.SelectedJiraItem(); ok {
					url := item.issue.BrowseURL
					if url == "" && m.jiraBaseURL != "" {
						url = m.jiraBaseURL + "/browse/" + item.issue.Key
					}
					if url != "" {
						return m, copyURL(url)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.Claim):
				if m.jiraClaimer != nil {
					if item, ok := m.SelectedJiraItem(); ok {
						m.statusText = fmt.Sprintf("Claiming %s...", item.issue.Key)
						return m, claimJiraIssue(m.jiraClaimer, item)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.FocusDetail):
				if m.detailReady && m.jiraDetailFetcher != nil {
					m.detailFocused = true
				}
				return m, nil

			// Cross-boundary j/down: bottom of section N → section N+1
			case msg.String() == "j" || msg.String() == "down":
				for from := 0; from < 3; from++ {
					if m.jiraSection == from && !m.jiraCollapsed[from+1] && len(m.lists[4+from+1].Items()) > 0 {
						items := m.lists[4+from].Items()
						cursor := m.lists[4+from].Index()
						if len(items) == 0 || cursor >= len(items)-1 {
							m.jiraSection = from + 1
							m.lists[4+from+1].Select(0)
							detailCmd := m.maybeLoadJiraDetail()
							return m, detailCmd
						}
					}
				}

			// Cross-boundary k/up: top of section N → section N-1
			case msg.String() == "k" || msg.String() == "up":
				for to := 2; to >= 0; to-- {
					from := to + 1
					if m.jiraSection == from && !m.jiraCollapsed[to] && len(m.lists[4+to].Items()) > 0 {
						cursor := m.lists[4+from].Index()
						if cursor <= 0 {
							m.jiraSection = to
							lastIdx := len(m.lists[4+to].Items()) - 1
							m.lists[4+to].Select(lastIdx)
							detailCmd := m.maybeLoadJiraDetail()
							return m, detailCmd
						}
					}
				}
			}
		}

		// Epics tab keybindings (when on tab 4).
		// The 'a' key works even when there are no epic lists.
		if m.activeTab == 4 && msg.String() == "a" && m.epicManager != nil && !m.epicAddActive {
			m.epicAddActive = true
			return m, m.epicAddInput.Focus()
		}
		if m.activeTab == 4 && len(m.epicLists) > 0 {
			switch {
			case msg.String() == "t" && m.epicManager != nil:
				// Toggle active/inactive on the focused epic section.
				if m.epicSection < len(m.epicSections) {
					sec := m.epicSections[m.epicSection]
					m.statusText = fmt.Sprintf("Hiding %s...", sec.EpicKey)
					return m, toggleEpic(m.epicManager, sec.EpicKey, false)
				}
				return m, nil

			case msg.String() == "D" && m.epicManager != nil:
				// Remove the focused epic section entirely.
				if m.epicSection < len(m.epicSections) {
					sec := m.epicSections[m.epicSection]
					m.statusText = fmt.Sprintf("Removing %s...", sec.EpicKey)
					return m, removeEpic(m.epicManager, sec.EpicKey)
				}
				return m, nil

			case msg.String() == "e":
				// Toggle showing items assigned to others.
				if m.epicSection < len(m.epicShowOthers) {
					m.epicShowOthers[m.epicSection] = !m.epicShowOthers[m.epicSection]
					m.epicLists[m.epicSection].SetItems(m.epicListItems(m.epicSection))
					m.resizeEpicSections()
				}
				return m, nil

			case key.Matches(msg, m.keys.SectionSwitch):
				if len(m.epicSections) > 0 {
					m.epicSection = (m.epicSection + 1) % len(m.epicSections)
					detailCmd := m.maybeLoadJiraDetailForEpic()
					return m, detailCmd
				}
				return m, nil

			case key.Matches(msg, m.keys.CollapseToggle):
				if m.epicSection < len(m.epicCollapsed) {
					m.epicCollapsed[m.epicSection] = !m.epicCollapsed[m.epicSection]
					m.resizeEpicSections()
				}
				return m, nil

			case key.Matches(msg, m.keys.Review):
				if item, ok := m.SelectedEpicItem(); ok {
					return m, launchWorktree(item)
				}
				return m, nil

			case key.Matches(msg, m.keys.OpenBrowser):
				if item, ok := m.SelectedEpicItem(); ok {
					url := item.issue.BrowseURL
					if url == "" && m.jiraBaseURL != "" {
						url = m.jiraBaseURL + "/browse/" + item.issue.Key
					}
					if url != "" {
						return m, openBrowser(url)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.CopyURL):
				if item, ok := m.SelectedEpicItem(); ok {
					url := item.issue.BrowseURL
					if url == "" && m.jiraBaseURL != "" {
						url = m.jiraBaseURL + "/browse/" + item.issue.Key
					}
					if url != "" {
						return m, copyURL(url)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.Claim):
				if m.jiraClaimer != nil {
					if item, ok := m.SelectedEpicItem(); ok {
						m.statusText = fmt.Sprintf("Claiming %s...", item.issue.Key)
						return m, claimJiraIssue(m.jiraClaimer, item)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.FocusDetail):
				if m.detailReady && m.jiraDetailFetcher != nil {
					m.detailFocused = true
				}
				return m, nil

			// Cross-boundary j/down: bottom of section N -> section N+1
			case msg.String() == "j" || msg.String() == "down":
				for from := 0; from < len(m.epicSections)-1; from++ {
					if m.epicSection == from && !m.epicCollapsed[from+1] && len(m.epicLists[from+1].Items()) > 0 {
						items := m.epicLists[from].Items()
						cursor := m.epicLists[from].Index()
						if len(items) == 0 || cursor >= len(items)-1 {
							m.epicSection = from + 1
							m.epicLists[from+1].Select(0)
							detailCmd := m.maybeLoadJiraDetailForEpic()
							return m, detailCmd
						}
					}
				}

			// Cross-boundary k/up: top of section N -> section N-1
			case msg.String() == "k" || msg.String() == "up":
				for to := len(m.epicSections) - 2; to >= 0; to-- {
					from := to + 1
					if m.epicSection == from && !m.epicCollapsed[to] && len(m.epicLists[to].Items()) > 0 {
						cursor := m.epicLists[from].Index()
						if cursor <= 0 {
							m.epicSection = to
							lastIdx := len(m.epicLists[to].Items()) - 1
							m.epicLists[to].Select(lastIdx)
							detailCmd := m.maybeLoadJiraDetailForEpic()
							return m, detailCmd
						}
					}
				}
			}
		}

		// Sprint tab keybindings (when on tab 5).
		if m.activeTab == 5 {
			switch {
			case msg.String() == "e":
				m.sprintShowOthers = !m.sprintShowOthers
				m.sprintList.SetItems(m.sprintListItems())
				m.resizeSprintSection()
				return m, nil

			case key.Matches(msg, m.keys.Review):
				if item, ok := m.SelectedSprintItem(); ok {
					return m, launchWorktree(item)
				}
				return m, nil

			case key.Matches(msg, m.keys.OpenBrowser):
				if item, ok := m.SelectedSprintItem(); ok {
					url := item.issue.BrowseURL
					if url == "" && m.jiraBaseURL != "" {
						url = m.jiraBaseURL + "/browse/" + item.issue.Key
					}
					if url != "" {
						return m, openBrowser(url)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.CopyURL):
				if item, ok := m.SelectedSprintItem(); ok {
					url := item.issue.BrowseURL
					if url == "" && m.jiraBaseURL != "" {
						url = m.jiraBaseURL + "/browse/" + item.issue.Key
					}
					if url != "" {
						return m, copyURL(url)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.Claim):
				if m.jiraClaimer != nil {
					if item, ok := m.SelectedSprintItem(); ok {
						m.statusText = fmt.Sprintf("Claiming %s...", item.issue.Key)
						return m, claimJiraIssue(m.jiraClaimer, item)
					}
				}
				return m, nil

			case key.Matches(msg, m.keys.FocusDetail):
				if m.detailReady && m.jiraDetailFetcher != nil {
					m.detailFocused = true
				}
				return m, nil
			}
		}

		// Stats tab keybindings (when on tab 2).
		if m.activeTab == 2 {
			switch msg.String() {
			case "u":
				if m.statsViewMode == statsViewTeam {
					m.statsViewMode = statsViewUser
					m.statsUserIdx = 0
				} else {
					m.statsViewMode = statsViewTeam
				}
				m.updateStatsViewport()
				return m, nil
			case "j":
				if m.statsViewMode == statsViewUser && len(m.statsUsers) > 0 {
					m.statsUserIdx = (m.statsUserIdx + 1) % len(m.statsUsers)
					m.updateStatsViewport()
				} else {
					m.statsViewport.ScrollDown(1)
				}
				return m, nil
			case "k":
				if m.statsViewMode == statsViewUser && len(m.statsUsers) > 0 {
					m.statsUserIdx = (m.statsUserIdx - 1 + len(m.statsUsers)) % len(m.statsUsers)
					m.updateStatsViewport()
				} else {
					m.statsViewport.ScrollUp(1)
				}
				return m, nil
			case "w":
				m.statsGranularity = statsGranWeekly
				m.updateStatsViewport()
				return m, nil
			case "m":
				m.statsGranularity = statsGranMonthly
				m.updateStatsViewport()
				return m, nil
			case "G":
				m.statsViewport.GotoBottom()
				return m, nil
			case "g":
				m.statsViewport.GotoTop()
				return m, nil
			}
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
			prevTab := m.activeTab
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			m.reviewSection = 0
			m.myPRsSection = 0
			m.jiraSection = 0
			m.epicSection = 0
			m.detailFocused = false
			cmd := m.handleTabSwitch(prevTab)
			return m, cmd

		case msg.String() == "shift+tab":
			prevTab := m.activeTab
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			m.reviewSection = 0
			m.myPRsSection = 0
			m.jiraSection = 0
			m.epicSection = 0
			m.detailFocused = false
			cmd := m.handleTabSwitch(prevTab)
			return m, cmd

		case key.Matches(msg, m.keys.SectionSwitch):
			if m.activeTab == 0 {
				m.reviewSection = (m.reviewSection + 1) % 3
				detailCmd := m.maybeLoadDetail()
				return m, detailCmd
			} else if m.activeTab == 1 {
				m.myPRsSection = (m.myPRsSection + 1) % 3
				detailCmd := m.maybeLoadDetail()
				return m, detailCmd
			}
			return m, nil

		case key.Matches(msg, m.keys.CollapseToggle):
			if m.activeTab == 0 {
				switch m.reviewSection {
				case 0:
					m.pendingCollapsed = !m.pendingCollapsed
				case 1:
					m.reviewedCollapsed = !m.reviewedCollapsed
				case 2:
					m.dismissedReviewerCollapsed = !m.dismissedReviewerCollapsed
				}
				m.resizeStackedLists()
			} else if m.activeTab == 1 {
				switch m.myPRsSection {
				case 0:
					m.activeCollapsed = !m.activeCollapsed
				case 1:
					m.draftsCollapsed = !m.draftsCollapsed
				case 2:
					m.dismissedAuthoredCollapsed = !m.dismissedAuthoredCollapsed
				}
				m.resizeMyPRsSections()
			}
			return m, nil

		// Cross-section boundary: j/down at bottom of current section jumps to next section.
		case msg.String() == "j" || msg.String() == "down":
			if m.activeTab == 0 && m.reviewSection == 0 && !m.reviewedCollapsed && len(m.lists[1].Items()) > 0 {
				items := m.lists[0].Items()
				cursor := m.lists[0].Index()
				if len(items) == 0 || cursor >= len(items)-1 {
					m.reviewSection = 1
					m.lists[1].Select(0)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}
			if m.activeTab == 0 && m.reviewSection == 1 && !m.dismissedReviewerCollapsed && len(m.lists[8].Items()) > 0 {
				items := m.lists[1].Items()
				cursor := m.lists[1].Index()
				if len(items) == 0 || cursor >= len(items)-1 {
					m.reviewSection = 2
					m.lists[8].Select(0)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}
			if m.activeTab == 1 && m.myPRsSection == 0 && !m.draftsCollapsed && len(m.lists[3].Items()) > 0 {
				items := m.lists[2].Items()
				cursor := m.lists[2].Index()
				if len(items) == 0 || cursor >= len(items)-1 {
					m.myPRsSection = 1
					m.lists[3].Select(0)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}
			if m.activeTab == 1 && m.myPRsSection == 1 && !m.dismissedAuthoredCollapsed && len(m.lists[9].Items()) > 0 {
				items := m.lists[3].Items()
				cursor := m.lists[3].Index()
				if len(items) == 0 || cursor >= len(items)-1 {
					m.myPRsSection = 2
					m.lists[9].Select(0)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}

		// Cross-section boundary: k/up at top of current section jumps to previous section.
		case msg.String() == "k" || msg.String() == "up":
			if m.activeTab == 0 && m.reviewSection == 2 && !m.reviewedCollapsed && len(m.lists[1].Items()) > 0 {
				cursor := m.lists[8].Index()
				if cursor <= 0 {
					m.reviewSection = 1
					lastIdx := len(m.lists[1].Items()) - 1
					m.lists[1].Select(lastIdx)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}
			if m.activeTab == 0 && m.reviewSection == 1 && !m.pendingCollapsed && len(m.lists[0].Items()) > 0 {
				cursor := m.lists[1].Index()
				if cursor <= 0 {
					m.reviewSection = 0
					lastIdx := len(m.lists[0].Items()) - 1
					m.lists[0].Select(lastIdx)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}
			if m.activeTab == 1 && m.myPRsSection == 2 && !m.draftsCollapsed && len(m.lists[3].Items()) > 0 {
				cursor := m.lists[9].Index()
				if cursor <= 0 {
					m.myPRsSection = 1
					lastIdx := len(m.lists[3].Items()) - 1
					m.lists[3].Select(lastIdx)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}
			if m.activeTab == 1 && m.myPRsSection == 1 && !m.activeCollapsed && len(m.lists[2].Items()) > 0 {
				cursor := m.lists[3].Index()
				if cursor <= 0 {
					m.myPRsSection = 0
					lastIdx := len(m.lists[2].Items()) - 1
					m.lists[2].Select(lastIdx)
					detailCmd := m.maybeLoadDetail()
					return m, detailCmd
				}
			}

		case key.Matches(msg, m.keys.FocusDetail):
			if m.detailReady && m.detailFetcher != nil {
				m.detailFocused = true
			}
			return m, nil

		case key.Matches(msg, m.keys.Review):
			if pr, ok := m.SelectedItem(); ok {
				if m.activeTab == 1 {
					return m, m.addressComments(pr)
				}
				return m, m.launchReview(pr)
			}
			return m, nil

		case key.Matches(msg, m.keys.Dismiss):
			if pr, ok := m.SelectedItem(); ok {
				return m, m.dismissPR(pr)
			}
			return m, nil

		case key.Matches(msg, m.keys.Undismiss):
			if m.activeTab == 0 && m.reviewSection == 2 {
				if pr, ok := m.SelectedItem(); ok {
					return m, m.undismissPR(pr)
				}
			} else if m.activeTab == 1 && m.myPRsSection == 2 {
				if pr, ok := m.SelectedItem(); ok {
					return m, m.undismissPR(pr)
				}
			}
			return m, nil

		case key.Matches(msg, m.keys.OpenBrowser):
			if pr, ok := m.SelectedItem(); ok {
				return m, openBrowser(pr.pr.URL)
			}
			return m, nil

		case key.Matches(msg, m.keys.CopyURL):
			if pr, ok := m.SelectedItem(); ok {
				return m, copyURL(pr.pr.URL)
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

		listWidth := m.width
		if m.detailFetcher != nil && m.width >= 80 {
			listWidth = m.width * 2 / 5
			detailWidth := m.width - listWidth - 1
			m.detailViewport = viewport.New(detailWidth, listHeight)
			m.detailReady = true
		}

		// Stats viewport uses full width (no detail panel split).
		m.statsViewport = viewport.New(m.width, listHeight)
		m.updateStatsViewport()

		// Stacked sections for tab 0 (lists[0], lists[1]) share height.
		m.resizeStackedLists()

		// Stacked sections for tab 1 (lists[2], lists[3]) share height.
		m.resizeMyPRsSections()

		// Stacked sections for tab 3 (lists[4..7]) share height.
		m.resizeJiraSections()

		// Dynamic epic sections (tab 4).
		m.resizeEpicSections()

		// Sprint section (tab 5).
		m.resizeSprintSection()

		detailCmd := m.maybeLoadDetail()
		return m, detailCmd

	case prsLoadedMsg:
		m.prsLoading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil

		// Split review PRs into pending vs reviewed sections.
		var pendingItems, reviewedItems []PRItem
		for _, item := range msg.reviewPRs {
			switch item.pr.ReviewerStatus {
			case "approved", "commented", "changes_requested":
				reviewedItems = append(reviewedItems, item)
			default:
				pendingItems = append(pendingItems, item)
			}
		}

		// Split authored PRs into active vs draft sections.
		var activeItems, draftItems []PRItem
		for _, item := range msg.authoredPRs {
			if item.pr.IsDraft {
				draftItems = append(draftItems, item)
			} else {
				activeItems = append(activeItems, item)
			}
		}

		m.lists[0].SetItems(toListItems(pendingItems))
		m.lists[1].SetItems(toListItems(reviewedItems))
		m.lists[2].SetItems(toListItems(activeItems))
		m.lists[3].SetItems(toListItems(draftItems))
		m.lists[8].SetItems(toListItems(msg.dismissedReviewer))
		m.lists[9].SetItems(toListItems(msg.dismissedAuthored))
		m.resizeStackedLists()
		m.resizeMyPRsSections()
		detailCmd := m.maybeLoadDetail()
		return m, tea.Batch(
			tea.SetWindowTitle(m.windowTitle(len(pendingItems), len(reviewedItems), len(activeItems), len(draftItems))),
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

	case StatsProgressMsg:
		m.statsProgress = msg
		m.updateStatsViewport()
		return m, nil

	case StatsReadyMsg:
		m.statsReady = true
		return m, m.loadStatsData()

	case statsDataLoadedMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Stats error: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		m.statsData = msg.data
		m.statsUsers = msg.users
		m.updateStatsViewport()
		return m, nil

	case RefreshMsg:
		m.prsLoading = true
		return m, tea.Batch(m.loadData(), m.spinner.Tick)

	case JiraRefreshMsg:
		m.jiraLoading = true
		cmds := []tea.Cmd{m.loadJiraData(), m.spinner.Tick}
		if m.statsReady {
			cmds = append(cmds, m.loadStatsData())
		}
		return m, tea.Batch(cmds...)

	case EpicRefreshMsg:
		m.epicLoading = true
		return m, tea.Batch(m.loadEpicData(), m.spinner.Tick)

	case epicDataLoadedMsg:
		m.epicLoading = false
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Epics: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		m.epicSections = msg.sections
		m.rebuildEpicLists(msg.items)
		// Compute per-section stats from the loaded items.
		m.epicStats = make([]EpicStats, len(msg.sections))
		for i, items := range msg.items {
			m.epicStats[i] = computeEpicStats(items, m.currentUser)
		}
		m.resizeEpicSections()
		detailCmd := m.maybeLoadJiraDetailForEpic()
		return m, detailCmd

	case epicAddedMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Add failed: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		m.statusText = fmt.Sprintf("Added %s", msg.key)
		return m, tea.Batch(m.loadEpicData(), clearStatusAfter(3*time.Second))

	case epicToggledMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Toggle failed: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		state := "hidden"
		if msg.active {
			state = "visible"
		}
		m.statusText = fmt.Sprintf("%s now %s", msg.key, state)
		return m, tea.Batch(m.loadEpicData(), clearStatusAfter(3*time.Second))

	case epicRemovedMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Remove failed: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		m.statusText = fmt.Sprintf("Removed %s", msg.key)
		if m.epicSection >= len(m.epicSections)-1 && m.epicSection > 0 {
			m.epicSection--
		}
		return m, tea.Batch(m.loadEpicData(), clearStatusAfter(3*time.Second))

	case SprintRefreshMsg:
		m.sprintLoading = true
		return m, tea.Batch(m.loadSprintData(), m.spinner.Tick)

	case sprintDataLoadedMsg:
		m.sprintLoading = false
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Sprint: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		m.sprintName = msg.sprintName
		m.sprintStats = computeSprintStats(msg.items, m.currentUser)
		m.rebuildSprintList(msg.items)
		m.resizeSprintSection()
		detailCmd := m.maybeLoadJiraDetailForSprint()
		return m, detailCmd

	case jiraDataLoadedMsg:
		m.jiraLoading = false
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Jira: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		if msg.currentUser != "" {
			m.currentUser = msg.currentUser
		}
		m.lists[4].SetItems(toJiraListItems(msg.inProgress))
		m.lists[5].SetItems(toJiraListItems(msg.submissions))
		m.lists[6].SetItems(toJiraListItems(msg.knowledge))
		m.lists[7].SetItems(toJiraListItems(msg.myTasks))
		m.resizeJiraSections()
		detailCmd := m.maybeLoadJiraDetail()
		return m, detailCmd

	case jiraDetailLoadedMsg:
		if msg.key != m.jiraDetailKey {
			return m, nil
		}
		m.jiraDetailLoading = false
		if msg.err != nil {
			m.jiraDetailErr = msg.err
			m.jiraDetail = nil
		} else {
			m.jiraDetailErr = nil
			m.jiraDetail = msg.detail
			m.jiraDetailCache[msg.key] = msg.detail
		}
		m.updateDetailViewport()
		return m, nil

	case jiraClaimedMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Claim failed: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
		}
		m.statusText = fmt.Sprintf("Claimed %s", msg.key)
		return m, tea.Batch(m.loadJiraData(), clearStatusAfter(3*time.Second))

	case dismissMsg:
		m.statusText = fmt.Sprintf("Dismissed PR %s", msg.prID)
		return m, tea.Batch(m.loadData(), clearStatusAfter(3*time.Second))

	case dismissErrMsg:
		m.statusText = fmt.Sprintf("Dismiss failed: %v", msg.err)
		return m, clearStatusAfter(3*time.Second)

	case undismissMsg:
		m.statusText = fmt.Sprintf("Restored PR %s", msg.prID)
		return m, tea.Batch(m.loadData(), clearStatusAfter(3*time.Second))

	case undismissErrMsg:
		m.statusText = fmt.Sprintf("Restore failed: %v", msg.err)
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

	case spinner.TickMsg:
		// Only tick the spinner while something is loading.
		if m.prsLoading || m.jiraLoading || m.epicLoading || m.sprintLoading || m.detailLoading || m.jiraDetailLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Stats tab doesn't use list.Model — handle viewport updates only.
	if m.activeTab == 2 {
		var cmd tea.Cmd
		m.statsViewport, cmd = m.statsViewport.Update(msg)
		return m, cmd
	}

	// Epics tab delegates to epicLists instead of lists[].
	if m.activeTab == 4 && len(m.epicLists) > 0 && m.epicSection < len(m.epicLists) {
		prevIdx := m.epicLists[m.epicSection].Index()
		var cmd tea.Cmd
		m.epicLists[m.epicSection], cmd = m.epicLists[m.epicSection].Update(msg)
		if m.epicLists[m.epicSection].Index() != prevIdx {
			detailCmd := m.maybeLoadJiraDetailForEpic()
			return m, tea.Batch(cmd, detailCmd)
		}
		return m, cmd
	}

	// Sprint tab delegates to sprintList.
	if m.activeTab == 5 {
		prevIdx := m.sprintList.Index()
		var cmd tea.Cmd
		m.sprintList, cmd = m.sprintList.Update(msg)
		if m.sprintList.Index() != prevIdx {
			detailCmd := m.maybeLoadJiraDetailForSprint()
			return m, tea.Batch(cmd, detailCmd)
		}
		return m, cmd
	}

	// Delegate to the active list for navigation, filtering, etc.
	idx := m.activeListIndex()
	prevIdx := m.lists[idx].Index()
	var cmd tea.Cmd
	m.lists[idx], cmd = m.lists[idx].Update(msg)

	// If selection changed, trigger detail loading for the new item.
	if m.lists[idx].Index() != prevIdx {
		var detailCmd tea.Cmd
		if m.activeTab == 3 {
			detailCmd = m.maybeLoadJiraDetail()
		} else {
			detailCmd = m.maybeLoadDetail()
		}
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
	item := m.lists[m.activeListIndex()].SelectedItem()
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

		dismissedReviewer, err := loader.GetDismissedByRole(ctx, "reviewer")
		if err != nil {
			return prsLoadedMsg{err: err}
		}

		dismissedAuthored, err := loader.GetDismissedByRole(ctx, "author")
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

		drItems := make([]PRItem, len(dismissedReviewer))
		for i, pr := range dismissedReviewer {
			drItems[i] = NewPRItem(pr, shame)
		}

		daItems := make([]PRItem, len(dismissedAuthored))
		for i, pr := range dismissedAuthored {
			daItems[i] = NewPRItem(pr, shame)
		}

		return prsLoadedMsg{
			reviewPRs:         rItems,
			authoredPRs:       aItems,
			dismissedReviewer: drItems,
			dismissedAuthored: daItems,
		}
	}
}

// windowTitle builds the terminal title string shown in the wezterm tab bar.
func (m Model) windowTitle(pendingCount, reviewedCount, activeCount, draftCount int) string {
	return fmt.Sprintf("PR(%d:%d:%d:%d)", pendingCount, reviewedCount, activeCount, draftCount)
}

// resizeStackedLists recalculates the height of lists[0] (pending), lists[1] (reviewed),
// and lists[8] (dismissed reviewer) based on collapse state.
// Each section header takes 1 line of overhead (3 total).
func (m *Model) resizeStackedLists() {
	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	listWidth := m.width
	if m.detailFetcher != nil && m.width >= 80 {
		listWidth = m.width * 2 / 5
	}

	// 3 section headers = 3 lines overhead.
	available := listHeight - 3
	if available < 0 {
		available = 0
	}

	expanded := 0
	if !m.pendingCollapsed {
		expanded++
	}
	if !m.reviewedCollapsed {
		expanded++
	}
	if !m.dismissedReviewerCollapsed {
		expanded++
	}

	var pendingH, reviewedH, dismissedH int
	if expanded > 0 {
		each := available / expanded
		rem := available - each*expanded
		if !m.pendingCollapsed {
			pendingH = each + rem
			rem = 0
		}
		if !m.reviewedCollapsed {
			reviewedH = each + rem
			rem = 0
		}
		if !m.dismissedReviewerCollapsed {
			dismissedH = each
		}
	}

	m.lists[0].SetSize(listWidth, pendingH)
	m.lists[1].SetSize(listWidth, reviewedH)
	m.lists[8].SetSize(listWidth, dismissedH)
}

// resizeMyPRsSections recalculates the height of lists[2] (active), lists[3] (drafts),
// and lists[9] (dismissed authored) based on collapse state.
// Each section header takes 1 line of overhead (3 total).
func (m *Model) resizeMyPRsSections() {
	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	listWidth := m.width
	if m.detailFetcher != nil && m.width >= 80 {
		listWidth = m.width * 2 / 5
	}

	// 3 section headers = 3 lines overhead.
	available := listHeight - 3
	if available < 0 {
		available = 0
	}

	expanded := 0
	if !m.activeCollapsed {
		expanded++
	}
	if !m.draftsCollapsed {
		expanded++
	}
	if !m.dismissedAuthoredCollapsed {
		expanded++
	}

	var activeH, draftsH, dismissedH int
	if expanded > 0 {
		each := available / expanded
		rem := available - each*expanded
		if !m.activeCollapsed {
			activeH = each + rem
			rem = 0
		}
		if !m.draftsCollapsed {
			draftsH = each + rem
			rem = 0
		}
		if !m.dismissedAuthoredCollapsed {
			dismissedH = each
		}
	}

	m.lists[2].SetSize(listWidth, activeH)
	m.lists[3].SetSize(listWidth, draftsH)
	m.lists[9].SetSize(listWidth, dismissedH)
}

// toListItems converts a slice of PRItem to a slice of list.Item.
func toListItems(items []PRItem) []list.Item {
	result := make([]list.Item, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}

// toJiraListItems converts a slice of JiraItem to a slice of list.Item.
func toJiraListItems(items []JiraItem) []list.Item {
	result := make([]list.Item, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}

// SelectedJiraItem returns the currently selected JiraItem, if any (for tab 3).
func (m Model) SelectedJiraItem() (JiraItem, bool) {
	if m.activeTab != 3 {
		return JiraItem{}, false
	}
	item := m.lists[m.activeListIndex()].SelectedItem()
	if item == nil {
		return JiraItem{}, false
	}
	ji, ok := item.(JiraItem)
	return ji, ok
}

// maybeLoadJiraDetail checks if a Jira detail fetch is needed for the current selection.
func (m *Model) maybeLoadJiraDetail() tea.Cmd {
	if m.jiraDetailFetcher == nil || !m.detailReady {
		return nil
	}

	ji, ok := m.SelectedJiraItem()
	if !ok {
		m.jiraDetail = nil
		m.jiraDetailKey = ""
		m.jiraDetailLoading = false
		m.jiraDetailErr = nil
		m.updateDetailViewport()
		return nil
	}

	issueKey := ji.issue.Key
	if issueKey == m.jiraDetailKey && !m.jiraDetailLoading {
		return nil
	}

	m.jiraDetailKey = issueKey

	if cached, ok := m.jiraDetailCache[issueKey]; ok {
		m.jiraDetail = cached
		m.jiraDetailLoading = false
		m.jiraDetailErr = nil
		m.updateDetailViewport()
		return nil
	}

	m.jiraDetailLoading = true
	m.jiraDetailErr = nil
	m.jiraDetail = nil
	m.updateDetailViewport()
	return fetchJiraDetail(m.jiraDetailFetcher, issueKey)
}

// handleTabSwitch returns a Cmd appropriate for the new active tab.
// For tabs 0/1 it triggers detail loading; for tab 2 it loads stats data if ready.
func (m *Model) handleTabSwitch(_ int) tea.Cmd {
	switch m.activeTab {
	case 2:
		// Arriving at stats tab: load data if ready and not yet loaded.
		if m.statsReady && m.statsData == nil {
			return m.loadStatsData()
		}
		m.updateStatsViewport()
		return nil
	case 3:
		// Arriving at Jira tab: load detail for current selection.
		return m.maybeLoadJiraDetail()
	case 4:
		// Arriving at Epics tab: load detail for current selection.
		return m.maybeLoadJiraDetailForEpic()
	case 5:
		// Arriving at Sprint tab: load detail for current selection.
		return m.maybeLoadJiraDetailForSprint()
	default:
		return m.maybeLoadDetail()
	}
}

// updateStatsViewport renders the current stats state into the stats viewport.
func (m *Model) updateStatsViewport() {
	if m.width == 0 {
		return
	}
	content := renderStatsTab(*m)
	m.statsViewport.SetContent(content)
}
