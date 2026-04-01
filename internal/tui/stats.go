package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tslc "github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

// section constants: 0=Chart, 1=ByModel, 2=ByAgent
const (
	statsSectionChart = 0
	statsSectionModel = 1
	statsSectionAgent = 2
	statsSectionCount = 3
)

type StatsView struct {
	period           model.StatsPeriod
	modelStats       []model.ModelStat
	agentStats       []model.AgentStat
	filteredModels   []model.ModelStat
	filteredAgents   []model.AgentStat
	filterQuery      string
	dailyPoints      []model.DailyPoint
	modelDailyPoints []model.ModelDailyPoint
	userDailyPoints  []model.UserDailyPoint
	userRequests     int
	humanRequests    int
	totalSessions    int
	loading          bool
	section          int
	modelCursor      int
	agentCursor      int
	modelOffset      int
	agentOffset      int
	modelSortCol     int
	agentSortCol     int
	chartTokens      tslc.Model
	chartCachePct    tslc.Model
	chartReqs        tslc.Model
	chartCursor      int
	sortedPoints     []model.DailyPoint
	width            int
	height           int
	theme            panes.Theme
}

func newStatsView(theme panes.Theme) StatsView {
	return StatsView{
		period:  model.PeriodAll,
		loading: true,
		theme:   theme,
	}
}

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func sinceMs(period model.StatsPeriod) int64 {
	now := time.Now()
	switch period {
	case model.PeriodToday:
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).UnixMilli()
	case model.Period7d:
		return now.AddDate(0, 0, -7).UnixMilli()
	case model.Period30d:
		return now.AddDate(0, 0, -30).UnixMilli()
	default:
		return 0
	}
}

func (v *StatsView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

func (v *StatsView) SetData(period model.StatsPeriod, modelStats []model.ModelStat, agentStats []model.AgentStat, daily []model.DailyPoint, modelDaily []model.ModelDailyPoint, userDaily []model.UserDailyPoint, userRequests int, humanRequests int, totalSessions int) {
	v.period = period
	v.modelStats = modelStats
	v.agentStats = agentStats
	v.dailyPoints = daily
	v.modelDailyPoints = modelDaily
	v.userDailyPoints = userDaily
	v.userRequests = userRequests
	v.humanRequests = humanRequests
	v.totalSessions = totalSessions
	v.loading = false

	v.applyFilter()
}

func (v *StatsView) SetFilter(query string) {
	v.filterQuery = query
	v.applyFilter()
}

func (v *StatsView) applyFilter() {
	q := strings.ToLower(v.filterQuery)
	if q == "" {
		v.filteredModels = make([]model.ModelStat, len(v.modelStats))
		copy(v.filteredModels, v.modelStats)
		v.filteredAgents = make([]model.AgentStat, len(v.agentStats))
		copy(v.filteredAgents, v.agentStats)
		v.buildCharts(v.dailyPoints)
	} else {
		v.filteredModels = nil
		for _, s := range v.modelStats {
			if strings.Contains(strings.ToLower(s.ModelID), q) || strings.Contains(strings.ToLower(s.ProviderID), q) {
				v.filteredModels = append(v.filteredModels, s)
			}
		}
		v.filteredAgents = nil
		for _, s := range v.agentStats {
			if strings.Contains(strings.ToLower(s.Agent), q) {
				v.filteredAgents = append(v.filteredAgents, s)
			}
		}
		switch v.section {
		case statsSectionAgent:
			v.buildCharts(v.dailyPoints)
		default:
			v.buildCharts(v.filteredDailyPoints(q))
		}
	}
	if v.modelCursor >= len(v.filteredModels) {
		v.modelCursor = 0
		v.modelOffset = 0
	}
	if v.agentCursor >= len(v.filteredAgents) {
		v.agentCursor = 0
		v.agentOffset = 0
	}
	v.resortModelStats()
}

func (v *StatsView) filteredDailyPoints(q string) []model.DailyPoint {
	userByDay := make(map[time.Time]map[string]model.UserDailyPoint)
	for _, udp := range v.userDailyPoints {
		if _, ok := userByDay[udp.Date]; !ok {
			userByDay[udp.Date] = make(map[string]model.UserDailyPoint)
		}
		userByDay[udp.Date][strings.ToLower(udp.ProviderID)] = udp
	}

	byDay := make(map[time.Time]*model.DailyPoint)
	for _, mdp := range v.modelDailyPoints {
		if !strings.Contains(strings.ToLower(mdp.ModelID), q) && !strings.Contains(strings.ToLower(mdp.ProviderID), q) {
			continue
		}
		dp, ok := byDay[mdp.Date]
		if !ok {
			byDay[mdp.Date] = &model.DailyPoint{Date: mdp.Date}
			dp = byDay[mdp.Date]
		}
		dp.Turns += mdp.Turns
		dp.InputTokens += mdp.InputTokens
		dp.OutputTokens += mdp.OutputTokens
		dp.CacheRead += mdp.CacheRead
		dp.CacheWrite += mdp.CacheWrite
		if provMap, ok := userByDay[mdp.Date]; ok {
			if udp, ok := provMap[strings.ToLower(mdp.ProviderID)]; ok {
				dp.UserRequests = udp.UserRequests
				dp.HumanRequests = udp.HumanRequests
			}
		}
	}
	result := make([]model.DailyPoint, 0, len(byDay))
	for _, dp := range byDay {
		result = append(result, *dp)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date.Before(result[j].Date) })
	return result
}

func (v *StatsView) buildCharts(daily []model.DailyPoint) {
	if len(daily) == 0 {
		v.sortedPoints = nil
		v.chartCursor = 0
		v.chartTokens = tslc.Model{}
		v.chartCachePct = tslc.Model{}
		v.chartReqs = tslc.Model{}
		return
	}

	points := make([]model.DailyPoint, len(daily))
	copy(points, daily)
	sort.Slice(points, func(i, j int) bool {
		return points[i].Date.Before(points[j].Date)
	})
	v.sortedPoints = points
	v.chartCursor = len(points) - 1

	chartWidth := v.width - 4
	if chartWidth < 10 {
		chartWidth = 10
	}

	available := v.height - 11
	eachH := available / 3
	if eachH < 4 {
		eachH = 4
	}

	v.chartTokens = tslc.New(chartWidth, eachH)
	v.chartCachePct = tslc.New(chartWidth, eachH)
	v.chartReqs = tslc.New(chartWidth, eachH)

	tokenFmt := func(_ int, val float64) string { return formatTokens(int64(math.Round(val))) }
	pctFmt := func(_ int, val float64) string { return fmt.Sprintf("%.0f%%", val) }
	intFmt := func(_ int, val float64) string { return fmt.Sprintf("%d", int64(math.Round(val))) }

	v.chartTokens.YLabelFormatter = tokenFmt
	v.chartCachePct.YLabelFormatter = pctFmt
	v.chartReqs.YLabelFormatter = intFmt

	v.chartTokens.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("33")))
	v.chartCachePct.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("10")))
	v.chartReqs.SetDataSetStyle("human", lipgloss.NewStyle().Foreground(lipgloss.Color("73")))
	v.chartReqs.SetDataSetStyle("subagent", lipgloss.NewStyle().Foreground(lipgloss.Color("141")))

	for _, dp := range points {
		t := dp.Date
		total := dp.InputTokens + dp.CacheRead + dp.CacheWrite
		v.chartTokens.Push(tslc.TimePoint{Time: t, Value: float64(total)})
		var cachePct float64
		if total > 0 {
			cachePct = float64(dp.CacheRead) / float64(total) * 100
		}
		v.chartCachePct.Push(tslc.TimePoint{Time: t, Value: cachePct})
		v.chartReqs.PushDataSet("human", tslc.TimePoint{Time: t, Value: float64(dp.HumanRequests)})
		v.chartReqs.PushDataSet("subagent", tslc.TimePoint{Time: t, Value: float64(dp.UserRequests - dp.HumanRequests)})
	}

	// Align Y-axis label widths so graph areas line up.
	maxLabelW := v.chartTokens.Origin().X
	if w := v.chartCachePct.Origin().X; w > maxLabelW {
		maxLabelW = w
	}
	if w := v.chartReqs.Origin().X; w > maxLabelW {
		maxLabelW = w
	}
	padFmt := func(inner func(int, float64) string) func(int, float64) string {
		return func(i int, val float64) string {
			s := inner(i, val)
			if pad := maxLabelW - len(s); pad > 0 {
				return strings.Repeat(" ", pad) + s
			}
			return s
		}
	}
	v.chartTokens.YLabelFormatter = padFmt(tokenFmt)
	v.chartCachePct.YLabelFormatter = padFmt(pctFmt)
	v.chartReqs.YLabelFormatter = padFmt(intFmt)
	v.chartTokens.Resize(chartWidth, eachH)
	v.chartCachePct.Resize(chartWidth, eachH)
	v.chartReqs.Resize(chartWidth, eachH)
}

func (v *StatsView) resortModelStats() {
	sort.SliceStable(v.filteredModels, func(i, j int) bool {
		a, b := v.filteredModels[i], v.filteredModels[j]
		switch v.modelSortCol {
		case 1:
			return a.OutputTokens > b.OutputTokens
		case 2:
			return a.CachePercent > b.CachePercent
		case 3:
			return a.UserRequests > b.UserRequests
		case 4:
			return a.Sessions > b.Sessions
		default:
			return (a.InputTokens + a.CacheRead + a.CacheWrite) > (b.InputTokens + b.CacheRead + b.CacheWrite)
		}
	})
}

func (v *StatsView) resortAgentStats() {
	sort.SliceStable(v.filteredAgents, func(i, j int) bool {
		a, b := v.filteredAgents[i], v.filteredAgents[j]
		switch v.agentSortCol {
		case 1:
			return a.OutputTokens > b.OutputTokens
		case 2:
			return a.CachePercent > b.CachePercent
		case 3:
			return a.UserRequests > b.UserRequests
		case 4:
			return a.Sessions > b.Sessions
		default:
			return (a.InputTokens + a.CacheRead + a.CacheWrite) > (b.InputTokens + b.CacheRead + b.CacheWrite)
		}
	})
}

func (v *StatsView) listPageHeight() int {
	h := v.height - 8
	if h < 1 {
		h = 1
	}
	return h
}

func (v *StatsView) clampOffsets() {
	pageH := v.listPageHeight()
	if v.modelCursor < v.modelOffset {
		v.modelOffset = v.modelCursor
	}
	if v.modelCursor >= v.modelOffset+pageH {
		v.modelOffset = v.modelCursor - pageH + 1
	}
	if v.agentCursor < v.agentOffset {
		v.agentOffset = v.agentCursor
	}
	if v.agentCursor >= v.agentOffset+pageH {
		v.agentOffset = v.agentCursor - pageH + 1
	}
}

func (v StatsView) handleKey(msg tea.KeyMsg) (StatsView, tea.Cmd) {
	switch msg.String() {
	case "tab":
		v.section = (v.section + 1) % statsSectionCount
	case "shift+tab":
		v.section = (v.section - 1 + statsSectionCount) % statsSectionCount
	case "j", "down":
		if v.section == statsSectionModel && v.modelCursor < len(v.filteredModels)-1 {
			v.modelCursor++
		} else if v.section == statsSectionAgent && v.agentCursor < len(v.filteredAgents)-1 {
			v.agentCursor++
		}
		v.clampOffsets()
	case "k", "up":
		if v.section == statsSectionModel && v.modelCursor > 0 {
			v.modelCursor--
		} else if v.section == statsSectionAgent && v.agentCursor > 0 {
			v.agentCursor--
		}
		v.clampOffsets()
	case "h", "left":
		if v.section == statsSectionChart && v.chartCursor > 0 {
			v.chartCursor--
		}
	case "l", "right":
		if v.section == statsSectionChart && v.chartCursor < len(v.sortedPoints)-1 {
			v.chartCursor++
		}
	case "ctrl+f", "ctrl+d":
		pageH := v.listPageHeight()
		if v.section == statsSectionModel {
			v.modelCursor = min(v.modelCursor+pageH, len(v.filteredModels)-1)
		} else if v.section == statsSectionAgent {
			v.agentCursor = min(v.agentCursor+pageH, len(v.filteredAgents)-1)
		} else if v.section == statsSectionChart && len(v.sortedPoints) > 0 {
			v.chartCursor = min(v.chartCursor+7, len(v.sortedPoints)-1)
		}
		v.clampOffsets()
	case "ctrl+b", "ctrl+u":
		pageH := v.listPageHeight()
		if v.section == statsSectionModel {
			v.modelCursor = max(v.modelCursor-pageH, 0)
		} else if v.section == statsSectionAgent {
			v.agentCursor = max(v.agentCursor-pageH, 0)
		} else if v.section == statsSectionChart && len(v.sortedPoints) > 0 {
			v.chartCursor = max(v.chartCursor-7, 0)
		}
		v.clampOffsets()
	case "0":
		if v.section == statsSectionChart && len(v.sortedPoints) > 0 {
			v.chartCursor = 0
		}
	case "$":
		if v.section == statsSectionChart && len(v.sortedPoints) > 0 {
			v.chartCursor = len(v.sortedPoints) - 1
		}
	case "s":
		if v.section == statsSectionModel {
			v.modelSortCol = (v.modelSortCol + 1) % 5
			v.resortModelStats()
		} else if v.section == statsSectionAgent {
			v.agentSortCol = (v.agentSortCol + 1) % 5
			v.resortAgentStats()
		}
	}
	return v, nil
}

func (v StatsView) renderSummary(mutedStyle, accentStyle, normalStyle lipgloss.Style) string {
	var input, output, cacheRead, cacheWrite int64
	topModel := ""

	for i, s := range v.filteredModels {
		input += s.InputTokens
		output += s.OutputTokens
		cacheRead += s.CacheRead
		cacheWrite += s.CacheWrite
		if i == 0 {
			topModel = s.ModelID
		}
	}

	total := input + cacheRead + cacheWrite
	if total == 0 && v.totalSessions == 0 && v.filterQuery == "" {
		return ""
	}

	sep := mutedStyle.Render("  │  ")
	filterStyle := mutedStyle.Italic(true)

	if total == 0 && v.totalSessions == 0 {
		return filterStyle.Render(`"` + v.filterQuery + `"`)
	}

	var cachePercent float64
	if total > 0 {
		cachePercent = float64(cacheRead) / float64(total) * 100
	}

	parts := []string{
		normalStyle.Render("Sess ") + accentStyle.Render(fmt.Sprintf("%d", v.totalSessions)),
		normalStyle.Render("Requests ") + accentStyle.Render(fmt.Sprintf("%d", v.userRequests)) +
			mutedStyle.Render(fmt.Sprintf(" (Human %d  SubAgent %d)", v.humanRequests, v.userRequests-v.humanRequests)),
		normalStyle.Render("In ") + accentStyle.Render(formatTokens(total)),
		normalStyle.Render("Out ") + accentStyle.Render(formatTokens(output)),
		normalStyle.Render("Cache ") + accentStyle.Render(fmt.Sprintf("%.0f%%", cachePercent)),
	}
	if topModel != "" {
		runes := []rune(topModel)
		if len(runes) > 28 {
			topModel = string(runes[:27]) + "…"
		}
		parts = append(parts, normalStyle.Render("Top ")+accentStyle.Render(topModel))
	}
	if v.filterQuery != "" {
		parts = append(parts, filterStyle.Render(`"`+v.filterQuery+`"`))
	}
	return strings.Join(parts, sep)
}

func (v StatsView) View() string {
	if v.width == 0 || v.height == 0 {
		return ""
	}

	accentStyle := lipgloss.NewStyle().Foreground(v.theme.Accent).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(v.theme.TextMuted)
	normalStyle := lipgloss.NewStyle().Foreground(v.theme.TextNormal)
	activeSecStyle := lipgloss.NewStyle().Foreground(v.theme.Accent).Bold(true).Underline(true)
	highlightStyle := lipgloss.NewStyle().
		Background(v.theme.AccentBg).
		Foreground(v.theme.AccentFg)

	var sb strings.Builder

	// Unified header: section tabs + period selector on the same line
	sectionLabels := []string{"Chart", "By Model", "By Agent"}
	var secParts []string
	for i, lbl := range sectionLabels {
		if i == v.section {
			secParts = append(secParts, activeSecStyle.Render(lbl))
		} else {
			secParts = append(secParts, normalStyle.Render(lbl))
		}
	}
	header := strings.Join(secParts, "   ")

	if v.section != statsSectionChart {
		periods := []struct {
			label string
			val   model.StatsPeriod
		}{
			{"Today", model.PeriodToday},
			{"7d", model.Period7d},
			{"30d", model.Period30d},
			{"All", model.PeriodAll},
		}
		var periodParts []string
		for _, p := range periods {
			lbl := fmt.Sprintf("[%s]", p.label)
			if p.val == v.period {
				periodParts = append(periodParts, accentStyle.Render(lbl))
			} else {
				periodParts = append(periodParts, mutedStyle.Render(lbl))
			}
		}
		header += "    " + strings.Join(periodParts, " ")
	}

	sb.WriteString(header)
	sb.WriteString("\n")

	if !v.loading {
		summary := v.renderSummary(mutedStyle, accentStyle, normalStyle)
		if summary != "" {
			sb.WriteString(summary)
			sb.WriteString("\n")
		}
	}

	if v.loading {
		loadingMsg := lipgloss.NewStyle().
			Foreground(v.theme.TextMuted).
			Render("⠸ Loading...")
		sb.WriteString(lipgloss.Place(v.width, v.height-3, lipgloss.Center, lipgloss.Center, loadingMsg))
		return sb.String()
	}

	contentWidth := v.width - 2
	if contentWidth < 10 {
		contentWidth = 10
	}

	switch v.section {
	case statsSectionChart:
		if len(v.sortedPoints) == 0 {
			var msg string
			if v.filterQuery != "" {
				msg = fmt.Sprintf("No chart data for %q", v.filterQuery)
			} else {
				msg = "No data available."
			}
			noDataStyle := lipgloss.NewStyle().Foreground(v.theme.TextMuted).Italic(true)
			chartAreaH := v.height - 6
			if chartAreaH < 1 {
				chartAreaH = 1
			}
			sb.WriteString(lipgloss.Place(contentWidth, chartAreaH, lipgloss.Center, lipgloss.Center, noDataStyle.Render(msg)))
			sb.WriteString("\n")
		} else {
			tokensStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DAEA3")).Bold(true)
			cachePctStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A9B665")).Bold(true)
			reqsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E78A4E")).Bold(true)

			v.chartTokens.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#7DAEA3")))
			v.chartCachePct.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#A9B665")))
			v.chartReqs.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#E78A4E")))

			colHighlight := lipgloss.NewStyle().Background(v.theme.AccentBg)

			var cursorDate, cursorTokens, cursorOut, cursorCachePct, cursorReqs string
			var cursorHuman, cursorSubAgent int
			if len(v.sortedPoints) > 0 && v.chartCursor >= 0 && v.chartCursor < len(v.sortedPoints) {
				dp := v.sortedPoints[v.chartCursor]
				total := dp.InputTokens + dp.CacheRead + dp.CacheWrite
				cursorDate = dp.Date.Format("2006-01-02")
				cursorTokens = formatTokens(total)
				cursorOut = formatTokens(dp.OutputTokens)
				cursorReqs = fmt.Sprintf("%d", dp.UserRequests)
				cursorHuman = dp.HumanRequests
				cursorSubAgent = dp.UserRequests - dp.HumanRequests
				var cachePct float64
				if total > 0 {
					cachePct = float64(dp.CacheRead) / float64(total) * 100
				}
				cursorCachePct = fmt.Sprintf("%.0f%%", cachePct)
			}

			title := tokensStyle.Render("── Input ──")
			if cursorDate != "" {
				title += mutedStyle.Render("  "+cursorDate+": ") + tokensStyle.Render(cursorTokens)
				title += mutedStyle.Render("  (Output: ") + tokensStyle.Render(cursorOut) + mutedStyle.Render(")")
			}
			sb.WriteString(title)
			sb.WriteString("\n")
			v.chartTokens.DrawBrailleAll()
			if len(v.sortedPoints) > 0 {
				v.chartTokens.SetColumnBackgroundStyle(v.sortedPoints[v.chartCursor].Date, colHighlight)
			}
			sb.WriteString(v.chartTokens.View())
			sb.WriteString("\n")

			title = cachePctStyle.Render("── Cache% ──")
			if cursorDate != "" {
				title += mutedStyle.Render("  "+cursorDate+": ") + cachePctStyle.Render(cursorCachePct)
			}
			sb.WriteString(title)
			sb.WriteString("\n")
			v.chartCachePct.DrawBrailleAll()
			if len(v.sortedPoints) > 0 {
				v.chartCachePct.SetColumnBackgroundStyle(v.sortedPoints[v.chartCursor].Date, colHighlight)
			}
			sb.WriteString(v.chartCachePct.View())
			sb.WriteString("\n")

			humanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("73")).Bold(true)
			subagentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true)

			title = reqsStyle.Render("── Reqs ──")
			title += "  " + humanStyle.Render("●") + mutedStyle.Render(" Human")
			title += "  " + subagentStyle.Render("●") + mutedStyle.Render(" SubAgent")
			if cursorDate != "" {
				title += mutedStyle.Render("  "+cursorDate+": ") + reqsStyle.Render(cursorReqs)
				title += mutedStyle.Render(fmt.Sprintf("  (Human %d  SubAgent %d)", cursorHuman, cursorSubAgent))
			}
			sb.WriteString(title)
			sb.WriteString("\n")
			v.chartReqs.DrawBrailleAll()
			if len(v.sortedPoints) > 0 {
				v.chartReqs.SetColumnBackgroundStyle(v.sortedPoints[v.chartCursor].Date, colHighlight)
			}
			sb.WriteString(v.chartReqs.View())
		}

	case statsSectionModel:
		sb.WriteString("\n")
		if len(v.filteredModels) == 0 {
			sb.WriteString(mutedStyle.Render("No data for this period."))
			sb.WriteString("\n")
		} else {
			sortIndicator := func(col int) string {
				if col == v.modelSortCol {
					return " ▼"
				}
				return ""
			}

			colSess := 6
			colTurns := 7
			colInput := 10
			colOutput := 10
			colCache := 8
			colProvider := 14
			colModel := contentWidth - colInput - colOutput - colCache - colSess - colTurns - colProvider - 13

			if colModel < 12 {
				colModel = 12
			}

			headerStyle := lipgloss.NewStyle().Foreground(v.theme.Accent).Bold(true)

			hModel := fmt.Sprintf("%-*s", colModel, "Model")
			hProvider := fmt.Sprintf("%-*s", colProvider, "Provider")
			hInput := fmt.Sprintf("%*s%s", colInput-len(sortIndicator(0)), "Input", sortIndicator(0))
			hOutput := fmt.Sprintf("%*s%s", colOutput-len(sortIndicator(1)), "Output", sortIndicator(1))
			hCache := fmt.Sprintf("%*s%s", colCache-len(sortIndicator(2)), "Cache%", sortIndicator(2))
			hTurns := fmt.Sprintf("%*s%s", colTurns-len(sortIndicator(3)), "Reqs", sortIndicator(3))
			hSess := fmt.Sprintf("%*s%s", colSess-len(sortIndicator(4)), "Sess", sortIndicator(4))

			headerLine := hModel + "  " + hProvider + "  " + hInput + "  " + hOutput + "  " + hCache + "  " + hTurns + "  " + hSess
			sb.WriteString(headerStyle.Render(headerLine))
			sb.WriteString("\n")

			for i, stat := range v.filteredModels {
				if i < v.modelOffset {
					continue
				}
				if i >= v.modelOffset+v.listPageHeight() {
					break
				}
				modelName := stat.ModelID
				if len([]rune(modelName)) > colModel {
					runes := []rune(modelName)
					modelName = string(runes[:colModel-1]) + "…"
				}
				providerName := stat.ProviderID
				if len([]rune(providerName)) > colProvider {
					runes := []rune(providerName)
					providerName = string(runes[:colProvider-1]) + "…"
				}

				line := fmt.Sprintf("%-*s  %-*s  %*s  %*s  %*.1f%%  %*d  %*d",
					colModel, modelName,
					colProvider, providerName,
					colInput, formatTokens(stat.InputTokens+stat.CacheRead+stat.CacheWrite),
					colOutput, formatTokens(stat.OutputTokens),
					colCache-1, stat.CachePercent,
					colTurns, stat.UserRequests,
					colSess, stat.Sessions,
				)

				if i == v.modelCursor {
					sb.WriteString(highlightStyle.Render(line))
				} else {
					sb.WriteString(normalStyle.Render(line))
				}
				sb.WriteString("\n")
			}
		}

	case statsSectionAgent:
		sb.WriteString("\n")
		if len(v.filteredAgents) == 0 {
			sb.WriteString(mutedStyle.Render("No data for this period."))
			sb.WriteString("\n")
		} else {
			sortIndicator := func(col int) string {
				if col == v.agentSortCol {
					return " ▼"
				}
				return ""
			}

			colSess := 6
			colTurns := 7
			colInput := 10
			colOutput := 10
			colCache := 8
			colAgent := contentWidth - colInput - colOutput - colCache - colTurns - colSess - 13

			if colAgent < 12 {
				colAgent = 12
			}

			headerStyle := lipgloss.NewStyle().Foreground(v.theme.Accent).Bold(true)

			hAgent := fmt.Sprintf("%-*s", colAgent, "Agent")
			hInput := fmt.Sprintf("%*s%s", colInput-len(sortIndicator(0)), "Input", sortIndicator(0))
			hOutput := fmt.Sprintf("%*s%s", colOutput-len(sortIndicator(1)), "Output", sortIndicator(1))
			hCache := fmt.Sprintf("%*s%s", colCache-len(sortIndicator(2)), "Cache%", sortIndicator(2))
			hTurns := fmt.Sprintf("%*s%s", colTurns-len(sortIndicator(3)), "Reqs", sortIndicator(3))
			hSess := fmt.Sprintf("%*s%s", colSess-len(sortIndicator(4)), "Sess", sortIndicator(4))

			headerLine := hAgent + "  " + hInput + "  " + hOutput + "  " + hCache + "  " + hTurns + "  " + hSess
			sb.WriteString(headerStyle.Render(headerLine))
			sb.WriteString("\n")

			for i, stat := range v.filteredAgents {
				if i < v.agentOffset {
					continue
				}
				if i >= v.agentOffset+v.listPageHeight() {
					break
				}
				agentName := stat.Agent
				if len([]rune(agentName)) > colAgent {
					runes := []rune(agentName)
					agentName = string(runes[:colAgent-1]) + "…"
				}

				line := fmt.Sprintf("%-*s  %*s  %*s  %*.1f%%  %*d  %*d",
					colAgent, agentName,
					colInput, formatTokens(stat.InputTokens+stat.CacheRead+stat.CacheWrite),
					colOutput, formatTokens(stat.OutputTokens),
					colCache-1, stat.CachePercent,
					colTurns, stat.UserRequests,
					colSess, stat.Sessions,
				)

				if i == v.agentCursor {
					sb.WriteString(highlightStyle.Render(line))
				} else {
					sb.WriteString(normalStyle.Render(line))
				}
				sb.WriteString("\n")
			}
		}
	}

	content := sb.String()
	if v.width > 4 {
		content = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(v.theme.BorderFocused).
			Width(v.width - 2).
			Height(v.height - 2). // fill allocated height (minus border)
			Render(content)
	}
	return content
}
