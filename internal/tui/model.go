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

	// Stacked section state (tab 0: To Review)
	reviewSection     int  // 0=pending focused, 1=reviewed focused
	pendingCollapsed  bool
	reviewedCollapsed bool

	// Stacked section state (tab 1: My PRs)
	myPRsSection     int  // 0=active focused, 1=drafts focused
	activeCollapsed  bool
	draftsCollapsed  bool

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

// New creates a new TUI model wired to the given data sources.
func New(loader PRLoader, resolver RepoResolver, shame ShameConfig, opts ...Option) Model {
	tabs := []string{"To Review", "My PRs", "Stats", "Jira"}

	delegate := list.NewDefaultDelegate()

	newList := func() list.Model {
		l := list.New(nil, delegate, 0, 0)
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(true)
		l.SetShowHelp(false)
		return l
	}

	// lists[0]=pending, [1]=reviewed, [2]=active, [3]=drafts,
	// [4]=jira in-progress, [5]=jira submissions, [6]=jira knowledge, [7]=jira my tasks
	lists := make([]list.Model, 8)
	for i := range lists {
		lists[i] = newList()
	}

	m := Model{
		prLoader:        loader,
		repoResolver:    resolver,
		keys:            defaultKeyMap(),
		tabs:            tabs,
		activeTab:       0,
		lists:           lists,
		shame:           shame,
		detailCache:     make(map[string]*PRDetail),
		jiraDetailCache: make(map[string]*JiraDetail),
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// activeListIndex returns the index into m.lists for the currently focused list.
// Tab 0 uses reviewSection (0=pending, 1=reviewed); tab 1 uses myPRsSection (0=active, 1=drafts).
// Tab 2 (stats) doesn't use lists but falls back to lists[2] for safety.
func (m Model) activeListIndex() int {
	switch m.activeTab {
	case 0:
		return m.reviewSection
	case 1:
		return 2 + m.myPRsSection
	case 3:
		return 4 + m.jiraSection
	default:
		return 2
	}
}

// Init returns a command to load initial data.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadData()}
	if m.jiraLoader != nil {
		cmds = append(cmds, m.loadJiraData())
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

		// Any keypress dismisses inline error banner.
		if m.errorText != "" {
			m.errorText = ""
		}

		// Don't intercept keys while filtering (stats tab has no list).
		if m.activeTab != 2 && m.lists[m.activeListIndex()].FilterState() == list.Filtering {
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
				if m.activeTab == 3 && m.jiraDetailKey != "" && m.jiraDetailFetcher != nil {
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
			m.detailFocused = false
			cmd := m.handleTabSwitch(prevTab)
			return m, cmd

		case msg.String() == "shift+tab":
			prevTab := m.activeTab
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			m.reviewSection = 0
			m.myPRsSection = 0
			m.jiraSection = 0
			m.detailFocused = false
			cmd := m.handleTabSwitch(prevTab)
			return m, cmd

		case key.Matches(msg, m.keys.SectionSwitch):
			if m.activeTab == 0 {
				m.reviewSection = 1 - m.reviewSection
				detailCmd := m.maybeLoadDetail()
				return m, detailCmd
			} else if m.activeTab == 1 {
				m.myPRsSection = 1 - m.myPRsSection
				detailCmd := m.maybeLoadDetail()
				return m, detailCmd
			}
			return m, nil

		case key.Matches(msg, m.keys.CollapseToggle):
			if m.activeTab == 0 {
				if m.reviewSection == 0 {
					m.pendingCollapsed = !m.pendingCollapsed
				} else {
					m.reviewedCollapsed = !m.reviewedCollapsed
				}
				m.resizeStackedLists()
			} else if m.activeTab == 1 {
				if m.myPRsSection == 0 {
					m.activeCollapsed = !m.activeCollapsed
				} else {
					m.draftsCollapsed = !m.draftsCollapsed
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

		// Cross-section boundary: k/up at top of current section jumps to previous section.
		case msg.String() == "k" || msg.String() == "up":
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

		detailCmd := m.maybeLoadDetail()
		return m, detailCmd

	case prsLoadedMsg:
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
		return m, m.loadData()

	case JiraRefreshMsg:
		cmds := []tea.Cmd{m.loadJiraData()}
		if m.statsReady {
			cmds = append(cmds, m.loadStatsData())
		}
		return m, tea.Batch(cmds...)

	case jiraDataLoadedMsg:
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Jira: %v", msg.err)
			return m, clearStatusAfter(5 * time.Second)
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

	// Stats tab doesn't use list.Model — handle viewport updates only.
	if m.activeTab == 2 {
		var cmd tea.Cmd
		m.statsViewport, cmd = m.statsViewport.Update(msg)
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
func (m Model) windowTitle(pendingCount, reviewedCount, activeCount, draftCount int) string {
	return fmt.Sprintf("PR(%d:%d:%d:%d)", pendingCount, reviewedCount, activeCount, draftCount)
}

// resizeStackedLists recalculates the height of lists[0] (pending) and lists[1] (reviewed)
// based on collapse state. Each section header takes 1 line of overhead.
func (m *Model) resizeStackedLists() {
	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	listWidth := m.width
	if m.detailFetcher != nil && m.width >= 80 {
		listWidth = m.width * 2 / 5
	}

	// 2 section headers = 2 lines overhead.
	available := listHeight - 2
	if available < 0 {
		available = 0
	}

	var pendingH, reviewedH int
	switch {
	case m.pendingCollapsed && m.reviewedCollapsed:
		pendingH = 0
		reviewedH = 0
	case m.pendingCollapsed:
		pendingH = 0
		reviewedH = available
	case m.reviewedCollapsed:
		pendingH = available
		reviewedH = 0
	default:
		pendingH = available / 2
		reviewedH = available - pendingH
	}

	m.lists[0].SetSize(listWidth, pendingH)
	m.lists[1].SetSize(listWidth, reviewedH)
}

// resizeMyPRsSections recalculates the height of lists[2] (active) and lists[3] (drafts)
// based on collapse state. Each section header takes 1 line of overhead.
func (m *Model) resizeMyPRsSections() {
	listHeight := m.height - 5
	if listHeight < 1 {
		listHeight = 1
	}

	listWidth := m.width
	if m.detailFetcher != nil && m.width >= 80 {
		listWidth = m.width * 2 / 5
	}

	// 2 section headers = 2 lines overhead.
	available := listHeight - 2
	if available < 0 {
		available = 0
	}

	var activeH, draftsH int
	switch {
	case m.activeCollapsed && m.draftsCollapsed:
		activeH = 0
		draftsH = 0
	case m.activeCollapsed:
		activeH = 0
		draftsH = available
	case m.draftsCollapsed:
		activeH = available
		draftsH = 0
	default:
		activeH = available / 2
		draftsH = available - activeH
	}

	m.lists[2].SetSize(listWidth, activeH)
	m.lists[3].SetSize(listWidth, draftsH)
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
func (m *Model) handleTabSwitch(prevTab int) tea.Cmd {
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
