# 📊 Weekly Operations Report
Channel: #{{.ChannelName}}
📅 Week: {{.WeekRange}}

## 📈 Summary
🚨 Total Incidents: {{.TotalIncidents}}
🔴 Critical Incidents: {{.CriticalIncidents}}
🟠 Major Incidents: {{.MajorIncidents}}
🟡 Minor Incidents: {{.MinorIncidents}}

## 🔍 Incident Breakdown
{{range .Incidents}}
### {{.Title}}
- 🔥 Severity: {{.Severity}}
- ⏱️ Duration: {{.Duration}}
- 📌 Status: {{.Status}}
- 💥 Impact: {{.Impact}}
{{end}}

## 📊 Key Metrics
- ⚡ Average Response Time: {{.AvgResponseTime}}
- ✅ Resolution Rate: {{.ResolutionRate}}%
- 🎯 Team Performance Score: {{.TeamScore}}/10

## 📝 Action Items
{{range .ActionItems}}
- {{.}}
{{end}}

🕒 Generated on: {{.GeneratedAt}} 