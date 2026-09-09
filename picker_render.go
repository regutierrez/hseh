package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const pickerTabRows = 1
const pickerSearchRows = 3
const pickerFooterRows = 1

// Layout minimums. Side-by-side needs room for both panes; stacked needs a few list rows plus a short preview.
const (
	pickerMinListWidth       = 20
	pickerMinPreviewWidth    = 20
	pickerMinStackedListRows = 3
	pickerMinStackedPreview  = 3
	pickerMaxStackedPreview  = 8
	pickerMinFooterHeight    = 6
)

const pickerSearchTitle = " Search "
const pickerSearchPrompt = "❯"
const pickerSelectionRail = "┃"
const pickerRailWidth = 2

const pickerHelpText = "↑↓ move · tab view · enter open · esc close"

const (
	pickerCopyLoading        = "Loading…"
	pickerCopyLoadFailed     = "Could not load Herdr session"
	pickerCopyNoSpaces       = "No spaces open"
	pickerCopyNoAgents       = "No agents open"
	pickerCopyNoResults      = "No results"
	pickerCopyLoadingPreview = "Loading preview…"
	pickerCopyNoPreview      = "No preview"
	pickerCopyPreviewFailed  = "Preview unavailable"
)

var pickerSGRPattern = regexp.MustCompile(`\x1b\[[0-9:;]*m`)

type previewMode int

const (
	previewHidden previewMode = iota
	previewSide
	previewStacked
)

// pickerFrame is the resolved screen geometry for one size. Every renderer and the
// mouse hit-tester read from it so they cannot disagree.
type pickerFrame struct {
	mode                                   previewMode
	listY, listW, listH                    int
	searchY, searchW, searchH              int
	dividerX                               int
	stackDividerY                          int
	previewX, previewY, previewW, previewH int
	footerY                                int
}

func (m pickerModel) frame() pickerFrame {
	width := max(1, m.width)
	height := max(0, m.height)
	f := pickerFrame{mode: previewHidden, footerY: -1, listY: pickerTabRows, listW: width, searchW: width}
	if height <= pickerTabRows {
		return f
	}
	footer := 0
	if height >= pickerMinFooterHeight {
		footer = pickerFooterRows
		f.footerY = height - 1
	}
	rest := height - pickerTabRows - footer
	search := min(pickerSearchRows, rest-1)
	if search < 0 {
		search = 0
	}
	f.searchH = search
	f.listH = rest - search
	f.searchY = pickerTabRows + f.listH
	if m.widePreviewMinCols > 0 && width >= m.widePreviewMinCols && width >= pickerMinListWidth+1+pickerMinPreviewWidth {
		listW := m.splitListWidth
		if listW <= 0 {
			listW = width / 2
		}
		lo, hi := m.splitListBounds()
		listW = min(max(listW, lo), hi)
		f.mode = previewSide
		f.listW = listW
		f.searchW = listW
		f.dividerX = listW
		f.previewX = listW + 1
		f.previewW = width - listW - 1
		f.previewY = pickerTabRows
		f.previewH = rest
		return f
	}
	if width >= pickerMinListWidth {
		available := rest - search - 1
		previewH := min(pickerMaxStackedPreview, max(pickerMinStackedPreview, available/3))
		listH := available - previewH
		if listH >= pickerMinStackedListRows && previewH >= pickerMinStackedPreview {
			f.mode = previewStacked
			f.listH = listH
			f.searchY = pickerTabRows + listH
			f.stackDividerY = f.searchY + search
			f.previewX = 0
			f.previewY = f.stackDividerY + 1
			f.previewW = width
			f.previewH = previewH
		}
	}
	return f
}

// splitListBounds clamps the side-by-side list width so both panes keep a readable minimum.
func (m pickerModel) splitListBounds() (lo, hi int) {
	width := max(1, m.width)
	lo = pickerMinListWidth
	hi = width - 1 - pickerMinPreviewWidth
	if hi < lo {
		lo, hi = width/2, width/2
	}
	return lo, hi
}

func (m pickerModel) renderTabs() string {
	th := m.th()
	var tabs []string
	for _, tab := range []struct{ view, label string }{
		{pickerViewSpaces, "Spaces"}, {pickerViewAgents, "Agents"},
	} {
		label := " " + tab.label + " "
		if tab.view == m.view {
			label = th.TabActive + label + "\x1b[0m"
		} else {
			label = th.Muted + label + "\x1b[0m"
		}
		tabs = append(tabs, label)
	}
	return padDisplayWidth(strings.Join(tabs, " "), m.width) + "\x1b[0m"
}

// searchPaneHeight is the rows occupied by the rounded search box, clipped to the terminal.
func (m pickerModel) searchPaneHeight() int {
	return m.frame().searchH
}

func (m pickerModel) footerHeight() int {
	if m.frame().footerY < 0 {
		return 0
	}
	return pickerFooterRows
}

// renderSearch draws the rounded search box: Search title, prompt, caret, and matched/total count.
func (m pickerModel) renderSearch(width int) string {
	height := m.searchPaneHeight()
	if height < 1 {
		return ""
	}
	if width < 1 {
		width = 1
	}
	return strings.Join(m.searchBoxLines(width, height), "\n")
}

func (m pickerModel) searchBoxLines(width, height int) []string {
	th := m.th()
	minInput := ansi.StringWidth(pickerSearchPrompt) + 2 // prompt, space, caret
	if minInput > width {
		minInput = width
	}
	drawBox := height >= 2 && width >= 2+minInput
	borderInner := width
	sidePad := 0
	if drawBox {
		borderInner = width - 2
		if width >= 4+minInput {
			sidePad = 1
		}
	}
	inputInner := width
	if drawBox {
		inputInner = width - 2 - 2*sidePad
	}
	input := m.searchInputLine(inputInner)
	if drawBox {
		pad := strings.Repeat(" ", sidePad)
		input = padDisplayWidth(th.Accent+"│\x1b[0m"+pad+input+pad+th.Accent+"│\x1b[0m", width)
	}
	lines := make([]string, height)
	for i := range lines {
		lines[i] = padDisplayWidth("", width)
	}
	inputIndex := height - 1
	if drawBox && height >= 3 {
		inputIndex = height - 2
	}
	lines[inputIndex] = padDisplayWidth(input, width)
	if drawBox {
		title := ""
		if ansi.StringWidth(pickerSearchTitle) <= borderInner {
			title = pickerSearchTitle
		}
		lines[0] = searchHorizontalBorder(th.Accent, "╭", "╮", borderInner, title, width)
		if height >= 3 {
			lines[height-1] = searchHorizontalBorder(th.Accent, "╰", "╯", borderInner, "", width)
		}
	}
	return lines
}

func searchHorizontalBorder(color, left, right string, inner int, title string, width int) string {
	var b strings.Builder
	b.WriteString(left)
	if title != "" && inner >= ansi.StringWidth(title) {
		rest := inner - ansi.StringWidth(title)
		leftPad := rest / 2
		b.WriteString(strings.Repeat("─", leftPad))
		b.WriteString(title)
		b.WriteString(strings.Repeat("─", rest-leftPad))
	} else if inner > 0 {
		b.WriteString(strings.Repeat("─", inner))
	}
	b.WriteString(right)
	return padDisplayWidth(color+b.String()+"\x1b[0m", width)
}

// searchInputLine always shows the query; errors live in the footer, never here.
func (m pickerModel) searchInputLine(width int) string {
	if width < 1 {
		return ""
	}
	th := m.th()
	text := m.query
	count := strconv.Itoa(len(m.visible)) + " / " + strconv.Itoa(len(m.allItems))
	countWidth := ansi.StringWidth(count)
	promptWidth := ansi.StringWidth(pickerSearchPrompt) + 1
	wantPrompt, wantCaret, wantCount := true, true, true
	need := func() int {
		n := 0
		if wantPrompt {
			n += promptWidth
		}
		if wantCaret {
			n++
		}
		if wantCount {
			n += 1 + countWidth
		}
		return n
	}
	if need() > width {
		wantCount = false
	}
	if need() > width {
		wantPrompt = false
	}
	if need() > width {
		wantCaret = false
	}
	textBudget := max(0, width-need())
	visibleText := clipSearchText(text, textBudget, true)
	padW := max(0, width-need()-ansi.StringWidth(visibleText))
	var b strings.Builder
	if wantPrompt {
		b.WriteString(th.Mauve)
		b.WriteString(pickerSearchPrompt)
		b.WriteString("\x1b[0m ")
	}
	b.WriteString(visibleText)
	if wantCaret {
		b.WriteString(th.Mauve + "▏\x1b[0m")
	}
	if padW > 0 {
		b.WriteString(strings.Repeat(" ", padW))
	}
	if wantCount {
		b.WriteString(" ")
		b.WriteString(th.Overlay)
		b.WriteString(count)
		b.WriteString("\x1b[0m")
	}
	return padDisplayWidth(b.String(), width)
}

func clipSearchText(s string, width int, keepTail bool) string {
	if width < 1 || s == "" {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	if !keepTail {
		return ansi.Truncate(s, width, "")
	}
	runes := []rune(s)
	start := 0
	for start < len(runes) && ansi.StringWidth(string(runes[start:])) > width {
		start++
	}
	clipped := string(runes[start:])
	if ansi.StringWidth(clipped) > width {
		return ""
	}
	return clipped
}

// renderFooter shows key help, plus a status. Errors are primary and win over help when
// space is tight; a secondary informational status is dropped before key help.
func (m pickerModel) renderFooter(width int) string {
	if width < 1 {
		return ""
	}
	th := m.th()
	help := pickerHelpText
	helpW := ansi.StringWidth(help)
	if m.statusErr != "" {
		errText := strings.ReplaceAll(strings.ReplaceAll(m.statusErr, "\n", " "), "\t", " ")
		errW := ansi.StringWidth(errText)
		if errW+2+helpW <= width {
			gap := width - errW - helpW
			return padDisplayWidth(th.Red+errText+"\x1b[0m"+strings.Repeat(" ", gap)+th.Muted+help+"\x1b[0m", width)
		}
		return padDisplayWidth(th.Red+ansi.Truncate(errText, width, "…")+"\x1b[0m", width)
	}
	status := m.secondaryStatus()
	statusW := ansi.StringWidth(status)
	if status != "" && statusW+2+helpW <= width {
		gap := width - statusW - helpW
		return padDisplayWidth(th.Muted+status+"\x1b[0m"+strings.Repeat(" ", gap)+th.Muted+help+"\x1b[0m", width)
	}
	return padDisplayWidth(th.Muted+ansi.Truncate(help, width, "…")+"\x1b[0m", width)
}

func (m pickerModel) secondaryStatus() string {
	switch {
	case !m.snapshotReady:
		return "Loading Herdr session…"
	case !m.catalogReady:
		return "Loading definitions…"
	}
	return ""
}

func (m pickerModel) View() string {
	if m.width == 0 {
		return "hseh"
	}
	return m.renderView()
}

func (m pickerModel) renderView() (out string) {
	if traceEnabled() {
		span := traceSpan("view")
		defer func() { span("bytes", len(out)) }()
	}
	f := m.frame()
	tabs := m.renderTabs()
	if m.height > 0 && m.height <= pickerTabRows {
		return tabs
	}
	th := m.th()
	search := m.renderSearch(f.searchW)
	var lines []string
	lines = append(lines, tabs)
	switch f.mode {
	case previewSide:
		var listLines, searchLines, prevLines []string
		if f.listH > 0 {
			listLines = strings.Split(m.renderList(f.listW, f.listH), "\n")
		}
		if f.searchH > 0 {
			searchLines = strings.Split(search, "\n")
		}
		prevLines = strings.Split(m.renderPreview(f.previewW, f.previewH), "\n")
		for i := 0; i < f.previewH; i++ {
			left, right := "", ""
			if i < f.listH {
				if i < len(listLines) {
					left = listLines[i]
				}
			} else if si := i - f.listH; si >= 0 && si < len(searchLines) {
				left = searchLines[si]
			}
			if i < len(prevLines) {
				right = prevLines[i]
			}
			lines = append(lines, padDisplayWidth(left, f.listW)+th.Overlay+"│\x1b[0m"+padDisplayWidth(right, f.previewW))
		}
	default:
		if f.listH > 0 {
			lines = append(lines, strings.Split(m.renderList(f.listW, f.listH), "\n")...)
		}
		if f.searchH > 0 {
			lines = append(lines, strings.Split(search, "\n")...)
		}
		if f.mode == previewStacked {
			lines = append(lines, padDisplayWidth(th.Overlay+strings.Repeat("─", f.previewW)+"\x1b[0m", f.previewW))
			lines = append(lines, strings.Split(m.renderPreview(f.previewW, f.previewH), "\n")...)
		}
	}
	if f.footerY >= 0 {
		lines = append(lines, m.renderFooter(m.width))
	}
	return strings.Join(lines, "\n")
}

type pickerListLayout struct {
	Lines   []string
	ItemIDs []string
}

func (m pickerModel) listPaneWidth() int {
	return m.frame().listW
}

func (m pickerModel) bodyHeight() int {
	return max(1, m.frame().listH)
}

func (m pickerModel) previewBodyHeight() int {
	return max(1, m.frame().previewH)
}

func (m pickerModel) renderList(width, height int) string {
	layout := m.buildPickerListLayout(width, height)
	return strings.Join(layout.Lines, "\n")
}

func (m pickerModel) emptyListCopy() string {
	th := m.th()
	switch {
	case !m.snapshotReady && m.liveErr != "":
		return th.Red + pickerCopyLoadFailed + "\x1b[0m"
	case !m.snapshotReady:
		return th.Yellow + pickerCopyLoading + "\x1b[0m"
	case strings.TrimSpace(m.query) != "":
		return th.Yellow + pickerCopyNoResults + " for “" + m.query + "”\x1b[0m"
	case m.view == pickerViewAgents:
		return th.Yellow + pickerCopyNoAgents + "\x1b[0m"
	default:
		return th.Yellow + pickerCopyNoSpaces + "\x1b[0m"
	}
}

// wrapItemBlock wraps one item's display rows to the content width, highlighting query
// matches and, for the selected item, emboldening the primary row.
func (m pickerModel) wrapItemBlock(item PickerItem, selected bool, contentWidth int) []string {
	rows := highlightItemRows(item, m.query, m.th().Mauve+"\x1b[1m")
	if len(rows) == 0 {
		rows = []string{item.ID}
	}
	var lines []string
	for i, row := range rows {
		if selected && i == 0 {
			row = emboldenAfterResets(row)
		}
		lines = append(lines, wrapDisplayLine(row, contentWidth)...)
	}
	return lines
}

type listWindowResult struct {
	lines, ids     []string
	lowest         int // smallest visible item index (nearest the search box)
	highest        int // largest visible item index (nearest the top)
	partialLowest  bool
	partialHighest bool
}

// fillListWindow wraps only the blocks that can be visible: the anchor block first, then
// whole/partial blocks toward the bottom (lower rank index), then toward the top.
func (m pickerModel) fillListWindow(anchor, budget, contentWidth int) listWindowResult {
	n := len(m.visible)
	res := listWindowResult{lowest: anchor, highest: anchor}
	if budget < 1 || n == 0 {
		return res
	}
	block := m.wrapItemBlock(m.visible[anchor], m.visible[anchor].ID == m.selectedID, contentWidth)
	if len(block) > budget {
		block = block[:budget]
	}
	used := len(block)
	var below, belowIDs []string
	for i := anchor - 1; i >= 0 && used < budget; i-- {
		room := budget - used - 1
		if room < 1 {
			break
		}
		blk := m.wrapItemBlock(m.visible[i], m.visible[i].ID == m.selectedID, contentWidth)
		partial := len(blk) > room
		if partial {
			blk = blk[:room]
		}
		below = append(below, "")
		belowIDs = append(belowIDs, "")
		below = append(below, blk...)
		for range blk {
			belowIDs = append(belowIDs, m.visible[i].ID)
		}
		used += 1 + len(blk)
		res.lowest = i
		if partial {
			res.partialLowest = true
			break
		}
	}
	var aboveRev, aboveRevIDs [][]string
	for i := anchor + 1; i < n && used < budget; i++ {
		room := budget - used - 1
		if room < 1 {
			break
		}
		blk := m.wrapItemBlock(m.visible[i], m.visible[i].ID == m.selectedID, contentWidth)
		partial := len(blk) > room
		if partial {
			blk = blk[len(blk)-room:]
		}
		ids := make([]string, len(blk))
		for j := range ids {
			ids[j] = m.visible[i].ID
		}
		aboveRev = append(aboveRev, blk)
		aboveRevIDs = append(aboveRevIDs, ids)
		used += 1 + len(blk)
		res.highest = i
		if partial {
			res.partialHighest = true
			break
		}
	}
	for i := len(aboveRev) - 1; i >= 0; i-- {
		res.lines = append(res.lines, aboveRev[i]...)
		res.ids = append(res.ids, aboveRevIDs[i]...)
		res.lines = append(res.lines, "")
		res.ids = append(res.ids, "")
	}
	res.lines = append(res.lines, block...)
	for range block {
		res.ids = append(res.ids, m.visible[anchor].ID)
	}
	res.lines = append(res.lines, below...)
	res.ids = append(res.ids, belowIDs...)
	return res
}

// buildPickerListLayout places highest-ranked item blocks nearest the bottom search box.
// Rows inside each block stay top-to-bottom; only block order is reversed. Only rows that
// can be on screen are wrapped.
func (m pickerModel) buildPickerListLayout(width, height int) pickerListLayout {
	height = max(1, height)
	width = max(1, width)
	th := m.th()
	n := len(m.visible)
	if n == 0 {
		lines := padToHeight(nil, height)
		ids := make([]string, height)
		lines[height-1] = padDisplayWidth(strings.Repeat(" ", pickerRailWidth)+m.emptyListCopy(), width) + "\x1b[0m"
		for i := 0; i < height-1; i++ {
			lines[i] = padDisplayWidth("", width) + "\x1b[0m"
		}
		return pickerListLayout{Lines: lines, ItemIDs: ids}
	}
	anchor := 0
	for i, item := range m.visible {
		if item.ID == m.selectedID {
			anchor = i
			break
		}
	}
	contentWidth := max(1, width-pickerRailWidth)
	reserve := 0
	var res listWindowResult
	var moreAbove, moreBelow int
	for attempt := 0; attempt < 3; attempt++ {
		res = m.fillListWindow(anchor, height-reserve, contentWidth)
		moreAbove = n - 1 - res.highest
		moreBelow = res.lowest
		want := 0
		if moreAbove > 0 || res.partialHighest {
			want++
		}
		if moreBelow > 0 || res.partialLowest {
			want++
		}
		if want == reserve || height-want < 1 {
			break
		}
		reserve = want
	}
	var lines, ids []string
	if reserve > 0 && (moreAbove > 0 || res.partialHighest) {
		lines = append(lines, th.Overlay+"↑ "+strconv.Itoa(max(moreAbove, 1))+" more\x1b[0m")
		ids = append(ids, "")
	}
	lines = append(lines, res.lines...)
	ids = append(ids, res.ids...)
	if reserve > 0 && (moreBelow > 0 || res.partialLowest) {
		lines = append(lines, th.Overlay+"↓ "+strconv.Itoa(max(moreBelow, 1))+" more\x1b[0m")
		ids = append(ids, "")
	}
	if len(lines) < height {
		pad := height - len(lines)
		lines = append(make([]string, pad), lines...)
		ids = append(make([]string, pad), ids...)
	}
	lines = lines[:height]
	ids = ids[:height]
	rail := th.Accent + pickerSelectionRail + "\x1b[0m "
	blank := strings.Repeat(" ", pickerRailWidth)
	for i, line := range lines {
		if ids[i] != "" && ids[i] == m.selectedID {
			lines[i] = padDisplayWidth(rail+line, width) + "\x1b[0m"
		} else {
			lines[i] = padDisplayWidth(blank+line, width) + "\x1b[0m"
		}
	}
	return pickerListLayout{Lines: lines, ItemIDs: ids}
}

func (m pickerModel) itemIDAtMouse(x, y int) string {
	f := m.frame()
	if f.listH < 1 || y < f.listY || y >= f.listY+f.listH {
		return ""
	}
	if x < 0 || x >= f.listW {
		return ""
	}
	layout := m.buildPickerListLayout(f.listW, f.listH)
	row := y - f.listY
	if row < 0 || row >= len(layout.ItemIDs) {
		return ""
	}
	return layout.ItemIDs[row]
}

func (m pickerModel) renderPreview(width, height int) string {
	th := m.th()
	switch {
	case m.previewLoading:
		return clipBlock(th.Yellow+pickerCopyLoadingPreview+"\x1b[0m", width, height)
	case m.previewErr != "":
		return clipBlock(th.Red+pickerCopyPreviewFailed+"\x1b[0m\n"+th.Muted+m.previewErr+"\x1b[0m", width, height)
	case m.previewText == "":
		if !m.snapshotReady {
			return clipBlock(th.Yellow+pickerCopyLoading+"\x1b[0m", width, height)
		}
		return clipBlock(th.Muted+pickerCopyNoPreview+"\x1b[0m", width, height)
	case m.previewTextLive:
		return clipLivePreview(m.previewText, width, height)
	default:
		return clipBlock(m.previewText, width, height)
	}
}

// clipLivePreview preserves terminal rows and shows their bottom edge, rather than reflowing a terminal grid.
func clipLivePreview(text string, width, height int) string {
	width, height = max(1, width), max(1, height)
	lines := strings.Split(strings.TrimSuffix(AllowVisiblePreviewANSI(text), "\n"), "\n")
	start := max(0, len(lines)-height)
	var visible []string
	carry := ""
	for i, line := range lines {
		if i >= start {
			visible = append(visible, padDisplayWidth(carry+line, width)+"\x1b[0m")
		}
		// Replay source styles at each row boundary, including styles set above the crop.
		carry = continuePickerSGR(carry, line)
	}
	return joinPaddedRows(visible, width, height)
}

func continuePickerSGR(carry, line string) string {
	for _, sgr := range pickerSGRPattern.FindAllString(line, -1) {
		if sgr == "\x1b[m" || sgr == "\x1b[0m" {
			carry = ""
		} else {
			carry += sgr
		}
	}
	return carry
}

// emboldenAfterResets makes a styled row bold without erasing its own colors.
func emboldenAfterResets(line string) string {
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m\x1b[1m")
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m\x1b[1m")
	return "\x1b[1m" + line
}

// highlightItemRows returns display rows with query matches highlighted. Fuzzy match
// offsets map onto plain rows; when a display row's visible text diverges from its
// plain row, the whole query is highlighted as a case-insensitive substring instead.
func highlightItemRows(item PickerItem, query, style string) []string {
	rows := item.DisplayRows
	if len(rows) == 0 {
		rows = item.Rows
	}
	query = strings.TrimSpace(query)
	if query == "" || len(rows) == 0 {
		return rows
	}
	out := make([]string, len(rows))
	perRow := matchedRunesByRow(item, len(rows))
	for i, row := range rows {
		set := perRow[i]
		if set == nil || i >= len(item.Rows) || StripTerminalControls(row) != item.Rows[i] {
			set = substringRuneMatches(StripTerminalControls(row), query)
		}
		if len(set) == 0 {
			out[i] = row
			continue
		}
		out[i] = highlightVisibleRunes(row, set, style)
	}
	return out
}

// matchedRunesByRow converts SearchText byte offsets into per-row visible rune indexes.
func matchedRunesByRow(item PickerItem, rowCount int) []map[int]bool {
	perRow := make([]map[int]bool, rowCount)
	if len(item.Matches) == 0 || len(item.Rows) == 0 {
		return perRow
	}
	start := 0
	for r, plain := range item.Rows {
		if r >= rowCount {
			break
		}
		end := start + len(plain)
		for _, b := range item.Matches {
			if b >= start && b < end {
				if perRow[r] == nil {
					perRow[r] = map[int]bool{}
				}
				perRow[r][utf8.RuneCountInString(plain[:b-start])] = true
			}
		}
		start = end + 1 // joined with a single space
	}
	return perRow
}

func substringRuneMatches(plain, query string) map[int]bool {
	text := []rune(strings.ToLower(plain))
	q := []rune(strings.ToLower(query))
	if len(q) == 0 || len(q) > len(text) {
		return nil
	}
	var set map[int]bool
	for i := 0; i+len(q) <= len(text); {
		if string(text[i:i+len(q)]) == string(q) {
			if set == nil {
				set = map[int]bool{}
			}
			for j := 0; j < len(q); j++ {
				set[i+j] = true
			}
			i += len(q)
			continue
		}
		i++
	}
	return set
}

// highlightVisibleRunes styles the visible runes at the given indexes, keeping existing SGR
// state by replaying it after each highlighted rune.
func highlightVisibleRunes(line string, set map[int]bool, style string) string {
	var b strings.Builder
	b.Grow(len(line) + 16*len(set))
	carry := ""
	index := 0
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			if loc := pickerSGRPattern.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
				sgr := line[i : i+loc[1]]
				b.WriteString(sgr)
				carry = continuePickerSGR(carry, sgr)
				i += loc[1]
				continue
			}
			b.WriteByte(line[i])
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if set[index] {
			b.WriteString(style)
			b.WriteString(line[i : i+size])
			b.WriteString("\x1b[0m")
			b.WriteString(carry)
		} else {
			b.WriteString(line[i : i+size])
		}
		_ = r
		index++
		i += size
	}
	return b.String()
}

func wrapDisplayLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	wrapped := ansi.Hardwrap(line, width, false)
	if wrapped == "" {
		return []string{""}
	}
	lines := strings.Split(wrapped, "\n")
	carry := ""
	for i, line := range lines {
		lines[i] = carry + line
		carry = continuePickerSGR(carry, line)
	}
	return lines
}

func padDisplayWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = ansi.Truncate(line, width, "")
	return lipgloss.NewStyle().Width(width).MaxHeight(1).Render(line)
}

func padToHeight(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// joinPaddedRows pads every row, including empty filler, so stacked preview
// lines clear stale cells instead of emitting zero-width blanks.
func joinPaddedRows(lines []string, width, height int) string {
	lines = padToHeight(lines, height)
	for i, line := range lines {
		if ansi.StringWidth(line) == width {
			continue
		}
		lines[i] = padDisplayWidth(line, width)
	}
	return strings.Join(lines, "\n")
}

func clipBlock(text string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, wrapDisplayLine(line, width)...)
		if len(lines) >= height {
			break
		}
	}
	for i, line := range lines {
		if i >= height {
			break
		}
		lines[i] = padDisplayWidth(line, width)
	}
	return joinPaddedRows(lines, width, height)
}
