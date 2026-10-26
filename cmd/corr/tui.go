package main

import (
	"context"
	"fmt"
	"image/color"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arenzana/correspondent/internal/client"
	"github.com/arenzana/correspondent/internal/textify"
)

// The reader: sources on the left (views, folders, feeds), the articles of
// the chosen one on a time spine, and an article full screen. Keys follow
// the web app's: j/k move, enter opens, s stars, m toggles read, n/p next
// and previous, o opens the original, / searches, ? shows them all.

func (a *app) tui() error {
	if a.cfg.Token == "" {
		return usagef("sign in first: corr auth login")
	}
	// The web app's theme: the reader's choice there, or --theme, or the
	// terminal's background (asked once, before the reader owns the keyboard).
	webTheme := ""
	if me, err := a.api.Me(a.ctx); err == nil {
		webTheme = me.Theme()
	}
	chooseTheme(a.theme, webTheme).apply()
	m := newReader(a)
	_, err := tea.NewProgram(m, tea.WithContext(a.ctx)).Run()
	return err
}

type pane int

const (
	paneSources pane = iota
	paneList
	paneArticle
)

type source struct {
	label    string
	view     string
	feedID   int64
	folderID int64
	unread   int
	indent   bool
	header   bool // a non-selectable group label
}

type reader struct {
	app     *app
	ctx     context.Context
	w, h    int
	focus   pane
	tree    client.Tree
	titles  map[int64]string
	sources []source
	src     int

	entries []client.Entry
	cursor  string
	sel     int
	offset  int // first visible row of the list
	loading bool

	article  *client.Entry
	vp       viewport.Model
	search   textinput.Model
	query    string
	searchOn bool

	help    bool
	status  string
	confirm string // a second press confirms (mark all read)
	err     error
}

// Messages.
type (
	treeMsg    client.Tree
	entriesMsg struct {
		page   client.Page
		append bool
	}
	articleMsg struct {
		e    client.Entry
		body string
	}
	statusMsg string
	errMsg    struct{ err error }
)

// Styles from the current palette (theme.go), set before the reader starts.
var (
	accent, slate, seal, danger               color.Color
	sDim, sAccent, sSeal, sBold, sErr, sTitle lipgloss.Style
)

func newReader(a *app) *reader {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "Search titles, excerpts, authors"
	return &reader{app: a, ctx: a.ctx, focus: paneList, search: ti, vp: viewport.New(), loading: true}
}

func (m *reader) Init() tea.Cmd { return tea.Batch(m.loadTree(), m.loadEntries(false)) }

// --- commands -----------------------------------------------------------------

func (m *reader) loadTree() tea.Cmd {
	return func() tea.Msg {
		t, err := m.app.api.Tree(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return treeMsg(t)
	}
}

func (m *reader) query_() client.Query {
	q := client.Query{View: "unread", Limit: 60, Search: m.query}
	if len(m.sources) > 0 {
		s := m.sources[m.src]
		q.FeedID, q.FolderID = s.feedID, s.folderID
		if s.view != "" {
			q.View = s.view
		}
	}
	return q
}

func (m *reader) loadEntries(more bool) tea.Cmd {
	q := m.query_()
	if more {
		q.Cursor = m.cursor
	}
	return func() tea.Msg {
		p, err := m.app.api.Entries(m.ctx, q)
		if err != nil {
			return errMsg{err}
		}
		return entriesMsg{p, more}
	}
}

func (m *reader) openArticle(e client.Entry, full bool) tea.Cmd {
	width := m.articleWidth()
	return func() tea.Msg {
		var got client.Entry
		var err error
		if full {
			got, err = m.app.api.FullArticle(m.ctx, e.ID)
		} else {
			got, err = m.app.api.Entry(m.ctx, e.ID)
		}
		if err != nil {
			return errMsg{err}
		}
		if !got.Read {
			if _, err := m.app.api.SetState(m.ctx, "read", []string{got.ID}); err == nil {
				got.Read = true
			}
		}
		feed := m.titles[got.FeedID]
		doc := "# " + got.Title + "\n\n" + feed
		if got.Author != "" {
			doc += " · " + got.Author
		}
		doc += " · " + time.Unix(got.PublishedAt, 0).Format("Monday, January 2, 2006") + "\n\n---\n\n" +
			textify.Numbered(got.Content, m.app.cfg.Server+"/")
		return articleMsg{got, renderMarkdown(doc, width)}
	}
}

func (m *reader) setState(action string, ids ...string) tea.Cmd {
	return func() tea.Msg {
		if _, err := m.app.api.SetState(m.ctx, action, ids); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func openURL(u string) tea.Cmd {
	return func() tea.Msg {
		name := "xdg-open"
		if runtime.GOOS == "darwin" {
			name = "open"
		}
		if err := exec.Command(name, u).Start(); err != nil {
			return statusMsg("Couldn't open a browser: " + u)
		}
		return statusMsg("Opened in your browser.")
	}
}

// --- update -------------------------------------------------------------------

func (m *reader) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.vp.SetWidth(m.w)
		m.vp.SetHeight(m.h - 3)
		return m, nil
	case treeMsg:
		m.tree = client.Tree(msg)
		m.titles = feedTitles(m.tree)
		m.buildSources()
		return m, nil
	case entriesMsg:
		m.loading = false
		m.err = nil
		if msg.append {
			m.entries = append(m.entries, msg.page.Entries...)
		} else {
			m.entries, m.sel, m.offset = msg.page.Entries, 0, 0
		}
		m.cursor = msg.page.Cursor
		return m, nil
	case articleMsg:
		e := msg.e
		m.article = &e
		m.patch(e.ID, func(x *client.Entry) { x.Read, x.Starred, x.FullText = e.Read, e.Starred, e.FullText })
		m.vp.SetContent(msg.body)
		m.vp.GotoTop()
		m.focus = paneArticle
		m.status = ""
		return m, nil
	case statusMsg:
		m.status = string(msg)
		return m, nil
	case errMsg:
		m.loading = false
		m.err = msg.err
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *reader) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.searchOn {
		switch key {
		case "enter":
			m.searchOn = false
			m.query = strings.TrimSpace(m.search.Value())
			m.search.Blur()
			m.loading = true
			return m, m.loadEntries(false)
		case "esc":
			m.searchOn = false
			m.search.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(k)
		return m, cmd
	}
	if m.help {
		m.help = false
		return m, nil
	}
	m.status = ""
	if key != "A" {
		m.confirm = ""
	}
	switch key {
	case "?":
		m.help = true
		return m, nil
	case "r":
		m.loading = true
		return m, tea.Batch(m.loadTree(), m.loadEntries(false))
	}
	switch m.focus {
	case paneArticle:
		return m.articleKey(key)
	case paneSources:
		return m.sourcesKey(key)
	}
	return m.listKey(key)
}

func (m *reader) listKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "j", "down":
		return m, m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.sel = 0
	case "G", "end":
		m.sel = max(0, len(m.entries)-1)
		return m, m.more()
	case "enter", "l", "right":
		if e := m.current(); e != nil {
			return m, m.openArticle(*e, false)
		}
	case "tab", "h", "left":
		m.focus = paneSources
	case "s":
		if e := m.current(); e != nil {
			e.Starred = !e.Starred
			return m, m.setState(map[bool]string{true: "star", false: "unstar"}[e.Starred], e.ID)
		}
	case "m":
		if e := m.current(); e != nil {
			e.Read = !e.Read
			return m, m.setState(map[bool]string{true: "read", false: "unread"}[e.Read], e.ID)
		}
	case "o", "v":
		if e := m.current(); e != nil && e.URL != "" {
			return m, openURL(e.URL)
		}
	case "/":
		m.searchOn = true
		m.search.SetValue(m.query)
		return m, m.search.Focus()
	case "esc":
		if m.query != "" {
			m.query = ""
			m.loading = true
			return m, m.loadEntries(false)
		}
	case "A":
		if m.confirm != "A" {
			m.confirm = "A"
			m.status = "Press A again to mark everything here read."
			return m, nil
		}
		m.confirm = ""
		s := m.sources[m.src]
		return m, func() tea.Msg {
			n, err := m.app.api.MarkAllRead(m.ctx, s.feedID, s.folderID, map[bool]string{true: "today", false: ""}[s.view == "today"])
			if err != nil {
				return errMsg{err}
			}
			return tea.Batch(func() tea.Msg { return statusMsg(fmt.Sprintf("Marked %d read.", n)) }, m.loadTree(), m.loadEntries(false))()
		}
	}
	return m, nil
}

func (m *reader) sourcesKey(key string) (tea.Model, tea.Cmd) {
	step := func(d int) {
		for i := m.src + d; i >= 0 && i < len(m.sources); i += d {
			if !m.sources[i].header {
				m.src = i
				return
			}
		}
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "j", "down":
		step(1)
	case "k", "up":
		step(-1)
	case "enter", "l", "right", "tab":
		m.focus = paneList
		m.loading = true
		m.query = ""
		return m, m.loadEntries(false)
	}
	return m, nil
}

func (m *reader) articleKey(key string) (tea.Model, tea.Cmd) {
	e := m.article
	switch key {
	case "q", "esc", "h", "left", "backspace":
		m.focus = paneList
		m.article = nil
	case "j", "down":
		m.vp.ScrollDown(1)
	case "k", "up":
		m.vp.ScrollUp(1)
	case "space", " ", "pgdown", "ctrl+d":
		if m.vp.AtBottom() {
			return m, m.step(1)
		}
		m.vp.HalfPageDown()
	case "pgup", "ctrl+u", "b":
		m.vp.HalfPageUp()
	case "g", "home":
		m.vp.GotoTop()
	case "G", "end":
		m.vp.GotoBottom()
	case "n":
		return m, m.step(1)
	case "p":
		return m, m.step(-1)
	case "s":
		e.Starred = !e.Starred
		m.patch(e.ID, func(x *client.Entry) { x.Starred = e.Starred })
		m.status = map[bool]string{true: "Starred.", false: "Unstarred."}[e.Starred]
		return m, m.setState(map[bool]string{true: "star", false: "unstar"}[e.Starred], e.ID)
	case "m":
		e.Read = false
		m.patch(e.ID, func(x *client.Entry) { x.Read = false })
		m.status = "Kept unread."
		return m, m.setState("unread", e.ID)
	case "f":
		if !e.FullText {
			m.status = "Fetching the full article…"
			return m, m.openArticle(*e, true)
		}
	case "o", "v":
		if e.URL != "" {
			return m, openURL(e.URL)
		}
	}
	return m, nil
}

// move changes the selection; near the end it loads the next page.
func (m *reader) move(d int) tea.Cmd {
	m.sel = min(max(m.sel+d, 0), max(len(m.entries)-1, 0))
	return m.more()
}

func (m *reader) more() tea.Cmd {
	if m.cursor != "" && !m.loading && m.sel >= len(m.entries)-5 {
		m.loading = true
		return m.loadEntries(true)
	}
	return nil
}

// step opens the next or previous article from the article view.
func (m *reader) step(d int) tea.Cmd {
	i := m.sel + d
	if i < 0 || i >= len(m.entries) {
		m.status = map[int]string{1: "That was the last article.", -1: "That was the first article."}[d]
		return nil
	}
	m.sel = i
	return tea.Batch(m.openArticle(m.entries[i], false), m.more())
}

func (m *reader) current() *client.Entry {
	if m.sel < len(m.entries) {
		return &m.entries[m.sel]
	}
	return nil
}

func (m *reader) patch(id string, f func(*client.Entry)) {
	for i := range m.entries {
		if m.entries[i].ID == id {
			f(&m.entries[i])
		}
	}
}

func (m *reader) buildSources() {
	t := m.tree
	cur := ""
	if len(m.sources) > 0 {
		cur = m.sources[m.src].label
	}
	src := []source{
		{label: "Unread", view: "unread", unread: t.Counts.Unread},
		{label: "Today", view: "today", unread: t.Counts.Today},
		{label: "Starred", view: "starred", unread: t.Counts.Starred},
		{label: "All articles", view: "all"},
	}
	byFolder := map[int64][]client.Feed{}
	var loose []client.Feed
	for _, f := range t.Feeds {
		if f.FolderID != nil {
			byFolder[*f.FolderID] = append(byFolder[*f.FolderID], f)
		} else {
			loose = append(loose, f)
		}
	}
	name := func(f client.Feed) string { return strings.ToLower(f.Title) }
	folders := append([]client.Folder(nil), t.Folders...)
	sort.Slice(folders, func(i, j int) bool { return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name) })
	for _, fo := range folders {
		src = append(src, source{label: fo.Name, view: "all", folderID: fo.ID, unread: fo.Unread})
		feeds := byFolder[fo.ID]
		sort.Slice(feeds, func(i, j int) bool { return name(feeds[i]) < name(feeds[j]) })
		for _, f := range feeds {
			src = append(src, source{label: f.Title, view: "all", feedID: f.ID, unread: f.Unread, indent: true})
		}
	}
	sort.Slice(loose, func(i, j int) bool { return name(loose[i]) < name(loose[j]) })
	for _, f := range loose {
		src = append(src, source{label: f.Title, view: "all", feedID: f.ID, unread: f.Unread})
	}
	m.sources = src
	m.src = 0
	for i, s := range src {
		if s.label == cur {
			m.src = i
		}
	}
}

// --- view ---------------------------------------------------------------------

const sourcesWidth = 28

func (m *reader) articleWidth() int { return max(m.w, 40) }

func (m *reader) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "Correspondent"
	// The whole terminal takes the theme's sheet and ink while reading.
	v.BackgroundColor = current.color(current.sheet)
	v.ForegroundColor = current.color(current.ink)
	return v
}

func (m *reader) render() string {
	if m.w == 0 {
		return ""
	}
	if m.help {
		return m.helpView()
	}
	header := m.header()
	footer := m.footer()
	bodyH := max(m.h-lipgloss.Height(header)-lipgloss.Height(footer), 1)
	var body string
	switch m.focus {
	case paneArticle:
		m.vp.SetHeight(bodyH)
		body = m.vp.View()
	default:
		left := m.sourcesView(bodyH)
		right := m.listView(m.w-sourcesWidth-1, bodyH)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, lipgloss.NewStyle().Foreground(current.color(current.rule)).Render(strings.Repeat("│\n", bodyH-1)+"│"), right)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *reader) header() string {
	title := "Unread"
	if len(m.sources) > 0 {
		title = m.sources[m.src].label
	}
	if m.focus == paneArticle && m.article != nil {
		title = m.titles[m.article.FeedID]
	}
	left := sTitle.Render(" Correspondent") + sDim.Render("  ·  ") + sBold.Render(title)
	if m.query != "" {
		left += sDim.Render("  matching “" + m.query + "”")
	}
	right := ""
	if m.focus == paneArticle {
		right = sDim.Render(fmt.Sprintf("%d of %d  %3.0f%% ", m.sel+1, len(m.entries), m.vp.ScrollPercent()*100))
	} else {
		right = sDim.Render(fmt.Sprintf("%d unread ", m.tree.Counts.Unread))
	}
	gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right + "\n" + lipgloss.NewStyle().Foreground(current.color(current.rule)).Render(strings.Repeat("─", m.w))
}

func (m *reader) footer() string {
	if m.searchOn {
		return m.search.View()
	}
	msg := ""
	switch {
	case m.err != nil:
		msg = sErr.Render(" " + m.err.Error())
	case m.status != "":
		msg = sAccent.Render(" " + m.status)
	case m.loading:
		msg = sDim.Render(" Loading…")
	}
	keys := " j/k move · enter open · s star · m read · / search · tab feeds · ? help · q quit"
	if m.focus == paneArticle {
		keys = " space page · n/p next/prev · s star · m keep unread · f full article · o open · esc back"
	}
	if msg != "" {
		return msg
	}
	return sDim.Render(ansi.Truncate(keys, m.w, "…"))
}

func (m *reader) sourcesView(h int) string {
	var b strings.Builder
	start := max(0, min(m.src-h/2, len(m.sources)-h))
	for i := start; i < len(m.sources) && i < start+h; i++ {
		s := m.sources[i]
		label := s.label
		if s.indent {
			label = "  " + label
		}
		count := ""
		if s.unread > 0 {
			count = fmt.Sprint(s.unread)
		}
		width := sourcesWidth - 3
		label = ansi.Truncate(label, width-len(count)-1, "…")
		row := " " + label + strings.Repeat(" ", max(width-lipgloss.Width(label)-len(count), 1)) + count + " "
		switch {
		case i == m.src && m.focus == paneSources:
			row = lipgloss.NewStyle().Background(accent).Foreground(current.color(current.sheet)).Bold(true).Render(row)
		case i == m.src:
			row = sAccent.Bold(true).Render(row)
		case s.indent:
			row = sDim.Render(row)
		}
		b.WriteString(row)
		if i < start+h-1 {
			b.WriteString("\n")
		}
	}
	return lipgloss.NewStyle().Width(sourcesWidth).Height(h).Render(b.String())
}

// listView draws the articles on a time spine: a day crossbar, then for
// each article the time, a filled dot (unread) or ring (read), the title,
// and the feed and excerpt beneath. The selection is a bar on the spine.
func (m *reader) listView(width, h int) string {
	if len(m.entries) == 0 {
		msg := "Nothing here."
		if m.loading {
			msg = "Loading…"
		} else if m.query != "" {
			msg = "No articles match."
		} else if len(m.sources) == 0 || m.sources[m.src].view == "unread" {
			msg = "All caught up."
		}
		return lipgloss.NewStyle().Width(width).Height(h).Padding(1, 2).Render(sDim.Render(msg))
	}
	type row struct {
		text  string
		entry int // -1 for day labels
	}
	var rows []row
	day := ""
	for i, e := range m.entries {
		t := time.Unix(e.PublishedAt, 0)
		if d := dayLabel(t); d != day {
			day = d
			rows = append(rows, row{"       " + sBold.Render("— "+d), -1})
		}
		sel := i == m.sel
		spine := sDim.Render("│")
		if sel {
			spine = sAccent.Render("┃")
		}
		dot := sAccent.Render("●")
		if e.Read {
			dot = sDim.Render("○")
		}
		star := " "
		if e.Starred {
			star = sSeal.Render("★")
		}
		// Both lines start 13 columns in (time, spine, dot, star).
		title := ansi.Truncate(e.Title, width-14, "…")
		if !e.Read {
			title = sBold.Render(title)
		} else {
			title = sDim.Render(title)
		}
		meta := m.titles[e.FeedID]
		if e.Excerpt != "" {
			meta += " · " + e.Excerpt
		}
		meta = sDim.Render(ansi.Truncate(meta, width-14, "…"))
		first := fmt.Sprintf(" %s %s %s %s %s", sDim.Render(t.Format("15:04")), spine, dot, star, title)
		second := fmt.Sprintf("       %s     %s", spine, meta)
		if sel && m.focus == paneList {
			// Styled segments reset the background, so the tint is laid
			// under each line and its padding separately.
			tint := lipgloss.NewStyle().Background(current.wash())
			fill := func(line string) string {
				bg := ansiBackground(current.wash())
				line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[m")
				line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+bg)
				return tint.Render(line + strings.Repeat(" ", max(width-lipgloss.Width(line), 0)))
			}
			first, second = fill(first), fill(second)
		}
		rows = append(rows, row{first, i}, row{second, i})
	}
	// Keep the selection in view.
	selRow := 0
	for i, r := range rows {
		if r.entry == m.sel {
			selRow = i
			break
		}
	}
	if selRow < m.offset {
		m.offset = max(selRow-1, 0)
	}
	if selRow+1 >= m.offset+h {
		m.offset = selRow + 2 - h
	}
	m.offset = max(0, min(m.offset, max(len(rows)-h, 0)))
	var b strings.Builder
	for i := m.offset; i < len(rows) && i < m.offset+h; i++ {
		b.WriteString(rows[i].text)
		if i < m.offset+h-1 {
			b.WriteString("\n")
		}
	}
	return lipgloss.NewStyle().Width(width).Height(h).Render(b.String())
}

// ansiBackground is the escape sequence that sets c as the background.
func ansiBackground(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

func dayLabel(t time.Time) string {
	now := time.Now()
	y, m, d := now.Date()
	ty, tm, td := t.Date()
	switch {
	case y == ty && m == tm && d == td:
		return "Today"
	case now.AddDate(0, 0, -1).Format("2006-01-02") == t.Format("2006-01-02"):
		return "Yesterday"
	case now.Sub(t) < 6*24*time.Hour:
		return t.Format("Monday")
	}
	return t.Format("Monday, January 2")
}

func (m *reader) helpView() string {
	rows := [][2]string{
		{"j / k, ↓ / ↑", "next / previous article"},
		{"enter, l", "open the article"},
		{"space", "page down; at the end, the next article"},
		{"n / p", "next / previous article while reading"},
		{"s", "star or unstar"},
		{"m", "mark read or unread"},
		{"f", "fetch the full article from its page"},
		{"o", "open the original in your browser"},
		{"/", "search; esc clears it"},
		{"tab, h", "feeds and folders"},
		{"A A", "mark everything here read"},
		{"r", "refresh"},
		{"esc", "back"},
		{"q", "quit"},
	}
	var b strings.Builder
	b.WriteString(sTitle.Render("Keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %s  %s\n", sAccent.Render(fmt.Sprintf("%-14s", r[0])), r[1]))
	}
	b.WriteString("\n" + sDim.Render("Press any key to go back."))
	return lipgloss.NewStyle().Padding(1, 3).Render(b.String())
}
