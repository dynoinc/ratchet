package report

import (
	"bytes"
	"text/template"
	"time"
)

type ReportData struct {
	ChannelName      string
	WeekRange        string
	TotalIncidents   int
	CriticalIncidents int
	MajorIncidents   int
	MinorIncidents   int
	Incidents        []IncidentSummary
	AvgResponseTime  string
	ResolutionRate   float64
	TeamScore       float64
	ActionItems     []string
	GeneratedAt     string
}

type IncidentSummary struct {
	Title    string
	Severity string
	Duration string
	Status   string
	Impact   string
}

type Generator struct {
	template *template.Template
}

func NewGenerator() (*Generator, error) {
	tmpl, err := template.ParseFiles("internal/templates/weekly_report.md")
	if err != nil {
		return nil, err
	}
	
	return &Generator{
		template: tmpl,
	}, nil
}

func (g *Generator) GenerateReport(channelName string, startDate time.Time) (string, error) {
	// Generate mock data with more realistic and visually appealing content
	data := ReportData{
		ChannelName:      channelName,
		WeekRange:        formatWeekRange(startDate),
		TotalIncidents:   5,
		CriticalIncidents: 1,
		MajorIncidents:   2,
		MinorIncidents:   2,
		Incidents: []IncidentSummary{
			{
				Title:    "Database Connection Pool Exhaustion",
				Severity: "🔴 Critical",
				Duration: "2h 15m",
				Status:   "✅ Resolved",
				Impact:   "Authentication system affected for 15% of users",
			},
			{
				Title:    "API Latency Spike",
				Severity: "🟠 Major",
				Duration: "45m",
				Status:   "✅ Resolved",
				Impact:   "Response times increased by 300%",
			},
			{
				Title:    "CDN Cache Miss Rate Increase",
				Severity: "🟡 Minor",
				Duration: "1h 30m",
				Status:   "✅ Resolved",
				Impact:   "Slight performance degradation for static assets",
			},
		},
		AvgResponseTime: "12m",
		ResolutionRate:  95.5,
		TeamScore:      9.0,
		ActionItems: []string{
			"📚 Update incident response documentation for database issues",
			"🔄 Implement automated failover for critical services",
			"📈 Review and adjust monitoring thresholds",
			"👥 Schedule incident response training for new team members",
		},
		GeneratedAt: time.Now().Format(time.RFC1123),
	}

	var buf bytes.Buffer
	if err := g.template.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func formatWeekRange(startDate time.Time) string {
	endDate := startDate.AddDate(0, 0, 6)
	return startDate.Format("Jan 2") + " - " + endDate.Format("Jan 2, 2006")
} 