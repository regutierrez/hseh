package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const pickerTabRows = 1
const pickerSearchRows = 3

// Search box colors match Catppuccin peach/mauve without recoloring the rest of the picker.
const pickerSearchBorderColor = "\x1b[38;2;250;179;135m" // #fab387
const pickerSearchPromptColor = "\x1b[38;2;203;166;247m" // #cba6f7
const pickerSearchCountColor = "\x1b[38;2;127;132;156m"  // #7f849c

const pickerSearchTitle = " Search "
const pickerSearchPrompt = "❯"

const pickerSelectedBackground = "\x1b[48;5;243m"
const pickerActiveTabBackground = "\x1b[48;5;74m"

var pickerSGRPattern = regexp.MustCompile(`\x1b\[[0-9:;]*m`)

func (m pickerModel) renderTabs() string {
	var tabs []string
	for _, tab := range []struct{ view, label string }{
		{pickerViewSpaces, "Spaces"}, {pickerViewAgents, "Agents"},
	} {
		label := " " + tab.label + " "
		if tab.view == m.view {
			label = pickerActiveTabBackground + "\x1b[30m" + label + "\x1b[0m"
		}
		tabs = append(tabs, label)
	}
	return padDisplayWidth(strings.Join(tabs, " "), m.width) + "\x1b[0m"
}

// searchPaneHeight is the footer rows occupied by the rounded search box, clipped to the terminal.
func (m pickerModel) searchPaneHeight() int {
	available := m.height - pickerTabRows
	if available < 1 {
		return 0
	}
	if available > pickerSearchRows {
		return pickerSearchRows
	}
	return available
}

// renderSearch draws the bottom-left rounded search box: Search title, purple prompt, caret, and matched/total count.
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
	minInput := ansi.StringWidth(pickerSearchPrompt) + 2 // prompt, space, caret
	if m.statusErr != "" {
		minInput = 1
	}
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
		input = padDisplayWidth(pickerSearchBorderColor+"│\x1b[0m"+pad+input+pad+pickerSearchBorderColor+"│\x1b[0m", width)
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
		lines[0] = searchHorizontalBorder("╭", "╮", borderInner, title, width)
		if height >= 3 {
			lines[height-1] = searchHorizontalBorder("╰", "╯", borderInner, "", width)
		}
	}
	return lines
}

func searchHorizontalBorder(left, right string, inner int, title string, width int) string {
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
	return padDisplayWidth(pickerSearchBorderColor+b.String()+"\x1b[0m", width)
}

func (m pickerModel) searchInputLine(width int) string {
	if width < 1 {
		return ""
	}
	text := m.query
	keepTail := true
	wantPrompt := true
	wantCaret := true
	if m.statusErr != "" {
		text = strings.ReplaceAll(strings.ReplaceAll(m.statusErr, "\n", " "), "\t", " ")
		keepTail = false
		wantPrompt = false
		wantCaret = false
	}
	count := strconv.Itoa(len(m.visible)) + " / " + strconv.Itoa(len(m.allItems))
	countWidth := ansi.StringWidth(count)
	promptWidth := ansi.StringWidth(pickerSearchPrompt) + 1
	wantCount := true
	fits := func() bool {
		need := 0
		if wantPrompt {
			need += promptWidth
		}
		if wantCaret {
			need++
		}
		if wantCount {
			need += 1 + countWidth
		}
		return need <= width
	}
	if !fits() {
		wantCount = false
	}
	if !fits() {
		wantPrompt = false
	}
	if !fits() {
		wantCaret = false
	}
	need := 0
	if wantPrompt {
		need += promptWidth
	}
	if wantCaret {
		need++
	}
	if wantCount {
		need += 1 + countWidth
	}
	textBudget := width - need
	if textBudget < 0 {
		textBudget = 0
	}
	visibleText := clipSearchText(text, textBudget, keepTail)
	padW := width - need - ansi.StringWidth(visibleText)
	if padW < 0 {
		padW = 0
	}
	var b strings.Builder
	if wantPrompt {
		b.WriteString(pickerSearchPromptColor)
		b.WriteString(pickerSearchPrompt)
		b.WriteString("\x1b[0m ")
	}
	b.WriteString(visibleText)
	if wantCaret {
		b.WriteString(pickerSearchPromptColor + "▏\x1b[0m")
	}
	if padW > 0 {
		b.WriteString(strings.Repeat(" ", padW))
	}
	if wantCount {
		b.WriteString(" ")
		b.WriteString(pickerSearchCountColor)
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

func (m pickerModel) View() string {
	if m.width == 0 {
		return "hseh"
	}
	tabs := m.renderTabs()
	if m.height > 0 && m.height <= pickerTabRows {
		return tabs
	}
	listWidth := m.listPaneWidth()
	showPreview := m.wideEnoughForPreview()
	searchRows := m.searchPaneHeight()
	listHeight := m.height - pickerTabRows - searchRows
	if listHeight < 0 {
		listHeight = 0
	}
	searchWidth := listWidth
	if !showPreview {
		searchWidth = m.width
		if searchWidth < 1 {
			searchWidth = 1
		}
	}
	search := m.renderSearch(searchWidth)
	if !showPreview {
		var parts []string
		parts = append(parts, tabs)
		if listHeight > 0 {
			parts = append(parts, m.renderList(listWidth, listHeight))
		}
		if m.height > pickerTabRows {
			parts = append(parts, search)
		}
		return strings.Join(parts, "\n")
	}
	previewWidth := m.width - listWidth - 1
	if previewWidth < 1 {
		previewWidth = 1
	}
	previewHeight := m.previewBodyHeight()
	preview := m.renderPreview(previewWidth, previewHeight)
	var listLines []string
	if listHeight > 0 {
		listLines = strings.Split(m.renderList(listWidth, listHeight), "\n")
	}
	prevLines := strings.Split(preview, "\n")
	searchLines := strings.Split(search, "\n")
	var lines []string
	for i := 0; i < previewHeight; i++ {
		left, right := "", ""
		if i < listHeight {
			if i < len(listLines) {
				left = listLines[i]
			}
		} else {
			si := i - listHeight
			if si >= 0 && si < len(searchLines) {
				left = searchLines[si]
			}
		}
		if i < len(prevLines) {
			right = prevLines[i]
		}
		lines = append(lines, padDisplayWidth(left, listWidth)+pickerMuted+"│\x1b[0m"+padDisplayWidth(right, previewWidth))
	}
	return tabs + "\n" + strings.Join(lines, "\n")
}

type pickerListLayout struct {
	Lines   []string
	ItemIDs []string
}

func (m pickerModel) listPaneWidth() int {
	if m.wideEnoughForPreview() && m.width > 1 {
		width := m.width / 2
		if width < 1 {
			return 1
		}
		return width
	}
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m pickerModel) bodyHeight() int {
	h := m.height - pickerTabRows - m.searchPaneHeight()
	if h < 1 {
		return 1
	}
	return h
}

func (m pickerModel) previewBodyHeight() int {
	h := m.height - pickerTabRows
	if h < 1 {
		return 1
	}
	return h
}

func (m pickerModel) renderList(width, height int) string {
	layout := m.buildPickerListLayout(width, height)
	return strings.Join(layout.Lines, "\n")
}

// buildPickerListLayout places highest-ranked item blocks nearest the bottom search.
// Rows inside each block stay top-to-bottom; only block order is reversed.
func (m pickerModel) buildPickerListLayout(width, height int) pickerListLayout {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	var blocks [][]string
	var blockIDs []string
	if len(m.visible) == 0 {
		blocks = [][]string{wrapDisplayLine("(no results)", width)}
		blockIDs = []string{""}
	}
	for _, item := range m.visible {
		block := strings.Join(item.DisplayRows, "\n")
		if block == "" {
			block = strings.Join(item.Rows, "\n")
		}
		if block == "" {
			block = item.ID
		}
		var lines []string
		for _, line := range strings.Split(block, "\n") {
			lines = append(lines, wrapDisplayLine("  "+line, width)...)
		}
		blocks = append(blocks, lines)
		blockIDs = append(blockIDs, item.ID)
	}
	var all []string
	var allIDs []string
	selectedStart, selectedEnd := 0, 0
	for i := len(blocks) - 1; i >= 0; i-- {
		if len(all) > 0 {
			all = append(all, "")
			allIDs = append(allIDs, "")
		}
		id := ""
		if i < len(blockIDs) {
			id = blockIDs[i]
		}
		if id != "" && id == m.selectedID {
			selectedStart = len(all)
		}
		for _, line := range blocks[i] {
			all = append(all, line)
			allIDs = append(allIDs, id)
		}
		if id != "" && id == m.selectedID {
			selectedEnd = len(all)
		}
	}
	var visible []string
	var visibleIDs []string
	if len(all) <= height {
		pad := height - len(all)
		for i := 0; i < pad; i++ {
			visible = append(visible, "")
			visibleIDs = append(visibleIDs, "")
		}
		visible = append(visible, all...)
		visibleIDs = append(visibleIDs, allIDs...)
	} else {
		start := windowAroundBottomStart(len(all), height, selectedStart, selectedEnd)
		end := start + height
		if end > len(all) {
			end = len(all)
		}
		if start > len(all) {
			start = len(all)
		}
		visible = append([]string{}, all[start:end]...)
		visibleIDs = append([]string{}, allIDs[start:end]...)
	}
	for i, line := range visible {
		if visibleIDs[i] != "" && visibleIDs[i] == m.selectedID {
			// Restore the row background after token resets without erasing status colors or bold text.
			base := pickerSelectedBackground + "\x1b[38;5;255m"
			styled := padDisplayWidth(line, width)
			// Secondary text needs a lighter gray on the selected gray background.
			styled = strings.ReplaceAll(styled, pickerMuted, "\x1b[38;5;252m")
			styled = strings.ReplaceAll(styled, "\x1b[0m", "\x1b[0m"+base)
			styled = strings.ReplaceAll(styled, "\x1b[m", "\x1b[m"+base)
			visible[i] = base + styled + "\x1b[0m"
		} else {
			visible[i] = padDisplayWidth(line, width) + "\x1b[0m"
		}
	}
	visible = padToHeight(visible, height)
	for len(visibleIDs) < height {
		visibleIDs = append(visibleIDs, "")
	}
	if len(visibleIDs) > height {
		visibleIDs = visibleIDs[:height]
	}
	return pickerListLayout{Lines: visible, ItemIDs: visibleIDs}
}

func (m pickerModel) itemIDAtMouse(x, y int) string {
	searchRows := m.searchPaneHeight()
	if y < pickerTabRows || y >= m.height-searchRows {
		return ""
	}
	listHeight := m.height - pickerTabRows - searchRows
	if listHeight < 1 {
		return ""
	}
	bodyY := y - pickerTabRows
	listWidth := m.listPaneWidth()
	if x < 0 || x >= listWidth {
		return ""
	}
	layout := m.buildPickerListLayout(listWidth, listHeight)
	if bodyY < 0 || bodyY >= len(layout.ItemIDs) {
		return ""
	}
	return layout.ItemIDs[bodyY]
}

func (m pickerModel) renderPreview(width, height int) string {
	text := m.previewText
	if m.previewErr != "" {
		text = m.previewErr
	}
	if text == "" {
		text = "(preview)"
	}
	if m.previewPane != "" && m.previewErr == "" {
		return clipLivePreview(text, width, height)
	}
	return clipBlock(text, width, height)
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
	return strings.Join(padToHeight(visible, height), "\n")
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

func windowAroundStart(n, height, selected int) int {
	if height < 1 || n <= height {
		return 0
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= n {
		selected = n - 1
	}
	start := selected
	if start+height > n {
		start = n - height
	}
	if start < 0 {
		start = 0
	}
	if selected >= start+height {
		start = selected - height + 1
	}
	if start < 0 {
		start = 0
	}
	return start
}

// windowAroundBottomStart keeps the selected block visible and prefers highest-ranked rows nearest the bottom.
func windowAroundBottomStart(n, height, selectedStart, selectedEnd int) int {
	if height < 1 || n <= height {
		return 0
	}
	if selectedEnd <= selectedStart {
		start := n - height
		if start < 0 {
			return 0
		}
		return start
	}
	if selectedStart < 0 {
		selectedStart = 0
	}
	if selectedEnd > n {
		selectedEnd = n
	}
	if selectedEnd-selectedStart >= height {
		start := selectedStart
		if start+height > n {
			start = n - height
		}
		if start < 0 {
			start = 0
		}
		return start
	}
	start := n - height
	if selectedStart < start {
		start = selectedStart
	}
	if selectedEnd > start+height {
		start = selectedEnd - height
	}
	if start < 0 {
		start = 0
	}
	if start+height > n {
		start = n - height
	}
	if start < 0 {
		start = 0
	}
	return start
}

func windowAround(lines []string, height, selected int) []string {
	start := windowAroundStart(len(lines), height, selected)
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	return lines[start:end]
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
	return strings.Join(padToHeight(lines, height), "\n")
}
