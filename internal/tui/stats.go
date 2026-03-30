package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tslc "github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

type StatsView struct {
	period       model.StatsPeriod
	modelStats   []model.ModelStat
	agentStats   []model.AgentStat
	dailyPoints  []model.DailyPoint
	loading      bool
	section      int
	modelCursor  int
	agentCursor  int
	modelSortCol int
	chartContext tslc.Model
	chartOutput  tslc.Model
	chartTurns   tslc.Model
	chartMetric  int
	width        int
	height       int
	theme        panes.Theme
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

func (v *StatsView) SetData(period model.StatsPeriod, modelStats []model.ModelStat, agentStats []model.AgentStat, daily []model.DailyPoint) {
	v.period = period
	v.modelStats = modelStats
	v.agentStats = agentStats
	v.dailyPoints = daily
	v.loading = false

	if v.modelCursor >= len(v.modelStats) {
		v.modelCursor = 0
	}
	if v.agentCursor >= len(v.agentStats) {
		v.agentCursor = 0
	}

	v.resortModelStats()
	v.buildCharts(daily)
}

func (v *StatsView) buildCharts(daily []model.DailyPoint) {
	if len(daily) == 0 {
		return
	}

	type weekKey struct{ year, week int }

	var points []model.DailyPoint
	if v.period == model.PeriodAll {
		weekData := map[weekKey]*model.DailyPoint{}
		for _, dp := range daily {
			year, week := dp.Date.ISOWeek()
			k := weekKey{year, week}
			if weekData[k] == nil {
				weekday := int(dp.Date.Weekday())
				if weekday == 0 {
					weekday = 7
				}
				monday := dp.Date.AddDate(0, 0, -(weekday - 1))
				weekData[k] = &model.DailyPoint{Date: monday}
			}
			weekData[k].Turns += dp.Turns
			weekData[k].InputTokens += dp.InputTokens
			weekData[k].OutputTokens += dp.OutputTokens
			weekData[k].CacheRead += dp.CacheRead
		}
		for _, wp := range weekData {
			points = append(points, *wp)
		}
		sort.Slice(points, func(i, j int) bool {
			return points[i].Date.Before(points[j].Date)
		})
	} else {
		points = daily
	}

	chartWidth := v.width - 4
	if chartWidth < 10 {
		chartWidth = 10
	}
	chartHeight := (v.height - 8) / 2
	if chartHeight < 6 {
		chartHeight = 6
	}

	v.chartContext = tslc.New(chartWidth, chartHeight)
	v.chartOutput = tslc.New(chartWidth, chartHeight)
	v.chartTurns = tslc.New(chartWidth, chartHeight)

	v.chartContext.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("33")))
	v.chartOutput.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("10")))
	v.chartTurns.SetStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("214")))

	for _, dp := range points {
		t := dp.Date
		v.chartContext.Push(tslc.TimePoint{Time: t, Value: float64(dp.InputTokens + dp.CacheRead)})
		v.chartOutput.Push(tslc.TimePoint{Time: t, Value: float64(dp.OutputTokens)})
		v.chartTurns.Push(tslc.TimePoint{Time: t, Value: float64(dp.Turns)})
	}
}

func (v *StatsView) resortModelStats() {
	sort.SliceStable(v.modelStats, func(i, j int) bool {
		a, b := v.modelStats[i], v.modelStats[j]
		switch v.modelSortCol {
		case 1:
			return a.OutputTokens > b.OutputTokens
		case 2:
			return a.Turns > b.Turns
		case 3:
			return a.CachePercent > b.CachePercent
		default:
			return a.InputTokens > b.InputTokens
		}
	})
}

func (v StatsView) handleKey(msg tea.KeyMsg) (StatsView, tea.Cmd) {
	switch msg.String() {
	case "tab":
		v.section = (v.section + 1) % 3
	case "1":
		v.section = 0
	case "2":
		v.section = 1
	case "3":
		v.section = 2
	case "j", "down":
		if v.section == 0 && v.modelCursor < len(v.modelStats)-1 {
			v.modelCursor++
		} else if v.section == 1 && v.agentCursor < len(v.agentStats)-1 {
			v.agentCursor++
		}
	case "k", "up":
		if v.section == 0 && v.modelCursor > 0 {
			v.modelCursor--
		} else if v.section == 1 && v.agentCursor > 0 {
			v.agentCursor--
		}
	case "s":
		if v.section == 0 {
			v.modelSortCol = (v.modelSortCol + 1) % 4
			v.resortModelStats()
		}
	case "m":
		if v.section == 2 {
			v.chartMetric = (v.chartMetric + 1) % 2
		}
	case "left":
		if v.section == 2 {
			var cmd tea.Cmd
			v.chartContext, cmd = v.chartContext.Update(msg)
			v.chartOutput, _ = v.chartOutput.Update(msg)
			v.chartTurns, _ = v.chartTurns.Update(msg)
			return v, cmd
		}
	case "right":
		if v.section == 2 {
			var cmd tea.Cmd
			v.chartContext, cmd = v.chartContext.Update(msg)
			v.chartOutput, _ = v.chartOutput.Update(msg)
			v.chartTurns, _ = v.chartTurns.Update(msg)
			return v, cmd
		}
	}
	return v, nil
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
	sb.WriteString(strings.Join(periodParts, "  "))
	sb.WriteString("\n")

	sectionLabels := []string{"[1] By Model", "[2] By Agent", "[3] Chart"}
	var secParts []string
	for i, lbl := range sectionLabels {
		if i == v.section {
			secParts = append(secParts, activeSecStyle.Render(lbl))
		} else {
			secParts = append(secParts, normalStyle.Render(lbl))
		}
	}
	sb.WriteString(strings.Join(secParts, "   "))
	sb.WriteString("\n\n")

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
	case 0:
		if len(v.modelStats) == 0 {
			sb.WriteString(mutedStyle.Render("No data for this period."))
			sb.WriteString("\n")
		} else {
			sortIndicator := func(col int) string {
				if col == v.modelSortCol {
					return " ▼"
				}
				return ""
			}

			colTurns := 7
			colInput := 10
			colOutput := 10
			colCache := 8
			colProvider := 14
			colModel := contentWidth - colTurns - colInput - colOutput - colCache - colProvider - 11

			if colModel < 12 {
				colModel = 12
			}

			headerStyle := lipgloss.NewStyle().Foreground(v.theme.Accent).Bold(true)

			hModel := fmt.Sprintf("%-*s", colModel, "Model")
			hProvider := fmt.Sprintf("%-*s", colProvider, "Provider")
			hTurns := fmt.Sprintf("%*s%s", colTurns, "Turns", sortIndicator(2))
			hInput := fmt.Sprintf("%*s%s", colInput-len(sortIndicator(0)), "Input", sortIndicator(0))
			hOutput := fmt.Sprintf("%*s%s", colOutput-len(sortIndicator(1)), "Output", sortIndicator(1))
			hCache := fmt.Sprintf("%*s%s", colCache-len(sortIndicator(3)), "Cache%", sortIndicator(3))

			headerLine := hModel + "  " + hProvider + "  " + hTurns + "  " + hInput + "  " + hOutput + "  " + hCache
			sb.WriteString(headerStyle.Render(headerLine))
			sb.WriteString("\n")

			for i, stat := range v.modelStats {
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

				line := fmt.Sprintf("%-*s  %-*s  %*d  %*s  %*s  %*.1f%%",
					colModel, modelName,
					colProvider, providerName,
					colTurns, stat.Turns,
					colInput, formatTokens(stat.InputTokens),
					colOutput, formatTokens(stat.OutputTokens),
					colCache-1, stat.CachePercent,
				)

				if i == v.modelCursor {
					sb.WriteString(highlightStyle.Render(line))
				} else {
					sb.WriteString(normalStyle.Render(line))
				}
				sb.WriteString("\n")
			}
		}

	case 1:
		if len(v.agentStats) == 0 {
			sb.WriteString(mutedStyle.Render("No data for this period."))
			sb.WriteString("\n")
		} else {
			colTurns := 7
			colInput := 10
			colOutput := 10
			colAgent := contentWidth - colTurns - colInput - colOutput - 6

			if colAgent < 12 {
				colAgent = 12
			}

			headerStyle := lipgloss.NewStyle().Foreground(v.theme.Accent).Bold(true)

			headerLine := fmt.Sprintf("%-*s  %*s  %*s  %*s",
				colAgent, "Agent",
				colTurns, "Turns",
				colInput, "Input",
				colOutput, "Output",
			)
			sb.WriteString(headerStyle.Render(headerLine))
			sb.WriteString("\n")

			for i, stat := range v.agentStats {
				agentName := stat.Agent
				if len([]rune(agentName)) > colAgent {
					runes := []rune(agentName)
					agentName = string(runes[:colAgent-1]) + "…"
				}

				line := fmt.Sprintf("%-*s  %*d  %*s  %*s",
					colAgent, agentName,
					colTurns, stat.Turns,
					colInput, formatTokens(stat.InputTokens),
					colOutput, formatTokens(stat.OutputTokens),
				)

				if i == v.agentCursor {
					sb.WriteString(highlightStyle.Render(line))
				} else {
					sb.WriteString(normalStyle.Render(line))
				}
				sb.WriteString("\n")
			}
		}

	case 2:
		metricNames := []string{"Context+Output", "Turns"}
		metricLabel := mutedStyle.Render(fmt.Sprintf("[m: %s]  ←/→ scroll", metricNames[v.chartMetric]))
		sb.WriteString(metricLabel)
		sb.WriteString("\n")

		if len(v.dailyPoints) == 0 {
			sb.WriteString(mutedStyle.Render("No data for this period."))
			sb.WriteString("\n")
		} else if v.chartMetric == 1 {
			v.chartTurns.DrawBrailleAll()
			sb.WriteString(v.chartTurns.View())
		} else {
			v.chartContext.DrawBrailleAll()
			v.chartOutput.DrawBrailleAll()
			sb.WriteString(mutedStyle.Render("── Context+Cache ──"))
			sb.WriteString("\n")
			sb.WriteString(v.chartContext.View())
			sb.WriteString("\n")
			sb.WriteString(mutedStyle.Render("── Output ──"))
			sb.WriteString("\n")
			sb.WriteString(v.chartOutput.View())
		}
	}

	footer := mutedStyle.Render("Tab: section  j/k: scroll  s: sort  m: metric  1/7/3/0: period")
	sb.WriteString("\n")
	sb.WriteString(footer)

	content := sb.String()
	if v.width > 4 {
		content = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(v.theme.BorderFocused).
			Width(v.width - 2).
			Render(content)
	}
	return content
}
