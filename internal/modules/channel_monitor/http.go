package channel_monitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type testChannelMonitorRequest struct {
	ConfigYaml   string
	MessageCount int
	TestMessages []string
}

type testChannelMonitorReportData struct {
	Message         dto.SlackMessage
	Prompt          string
	ValidatedOutput string
	Error           string
	InvalidOutput   string
}

func HTTPHandler(
	llmClient llm.Client,
	slackIntegration slack_integration.Integration,
	prefix string,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, prefix+"/test", http.StatusSeeOther)
	})
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		nonce, errNonce := generateNonce()
		if errNonce != nil {
			http.Error(w, fmt.Sprintf("generating nonce: %v", errNonce), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Security-Policy", fmt.Sprintf("default-src 'self'; script-src 'self' https://unpkg.com 'nonce-%s'; style-src 'self' 'nonce-%s';", nonce, nonce))

		if r.Method == "POST" {
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			req := testChannelMonitorRequest{
				ConfigYaml:   r.FormValue("config_yaml"),
				MessageCount: 0, // Will parse below
				TestMessages: []string{},
			}
			if messageCount := r.FormValue("message_count"); messageCount != "" {
				if count, err := strconv.Atoi(messageCount); err == nil {
					req.MessageCount = count
				}
			}
			if messages := r.FormValue("test_messages"); messages != "" {
				req.TestMessages = strings.Split(messages, "---\n")
			}
			if req.MessageCount > 50 {
				http.Error(w, "Message count must be less than 50", http.StatusBadRequest)
				return
			}
			if len(req.TestMessages) > 50 {
				http.Error(w, "Must be less than 50 example messages", http.StatusBadRequest)
				return
			}
			if req.MessageCount == 0 && len(req.TestMessages) == 0 {
				http.Error(w, "Must provide either message count or example messages", http.StatusBadRequest)
				return
			}
			report, err := testChannelMonitorPrompt(r.Context(), llmClient, slackIntegration, req, nonce)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(report))
		}
		if r.Method == "GET" {
			renderPage(w, prefix, nonce)
		}
	})
	return mux
}

func testChannelMonitorPrompt(ctx context.Context,
	llmClient llm.Client,
	slackIntegration slack_integration.Integration,
	req testChannelMonitorRequest,
	nonce string,
) (string, error) {
	entry, err := getEntryForTest(req)
	if err != nil {
		return "", err
	}
	history, err := getMessagesForTest(ctx, slackIntegration, entry, req)
	if err != nil {
		return "", err
	}
	results := getTestResults(ctx, history, entry, llmClient) // Wait for all goroutines to complete
	return renderTestReport(results, nonce), nil
}

func getTestResults(ctx context.Context, history []dto.SlackMessage, entry *entry, llmClient llm.Client) []*testChannelMonitorReportData {
	results := make([]*testChannelMonitorReportData, len(history))
	var wg sync.WaitGroup
	resultsMutex := &sync.Mutex{}
	semaphore := make(chan struct{}, 5) // Limit concurrency to 5 parallel requests

	for i, msg := range history {
		wg.Add(1)
		semaphore <- struct{}{} // Acquire semaphore
		go func(idx int, message dto.SlackMessage) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release semaphore

			data := promptData{Message: message}
			var prompt bytes.Buffer
			err := entry.PromptTemplate.Execute(&prompt, data)

			reportData := &testChannelMonitorReportData{
				Message: message,
				Prompt:  prompt.String(),
			}

			if err != nil {
				reportData.Error = fmt.Sprintf("executing prompt template: %v", err)
			} else {
				validOutput, invalidOutput, err := llmClient.RunJSONModePrompt(ctx, prompt.String(), entry.ResultSchema)
				reportData.ValidatedOutput = validOutput
				reportData.InvalidOutput = invalidOutput
				if err != nil {
					reportData.Error = err.Error()
				}
			}

			resultsMutex.Lock()
			results[idx] = reportData
			resultsMutex.Unlock()
		}(i, msg)
	}
	wg.Wait()
	return results
}

func getEntryForTest(req testChannelMonitorRequest) (*entry, error) {
	parsedYaml := &map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(req.ConfigYaml), parsedYaml); err != nil {
		return nil, fmt.Errorf("unmarshalling yaml: %w", err)
	}
	marshaled, err := json.Marshal(parsedYaml)
	if err != nil {
		return nil, fmt.Errorf("marshalling json: %w", err)
	}
	entry := &entry{}
	if err := json.Unmarshal(marshaled, entry); err != nil {
		return nil, fmt.Errorf("unmarshalling json: %w", err)
	}
	tmpl, err := template.New("test").Parse(entry.Prompt)
	if err != nil {
		return nil, fmt.Errorf("parsing prompt template: %w", err)
	}
	entry.PromptTemplate = tmpl
	if entry.ChannelID == "" {
		return nil, fmt.Errorf("missing channel_id in config")
	}
	return entry, nil
}

func getMessagesForTest(ctx context.Context, slackIntegration slack_integration.Integration, entry *entry, req testChannelMonitorRequest) ([]dto.SlackMessage, error) {
	var history []dto.SlackMessage
	if req.MessageCount > 0 {
		slackMessages, err := slackIntegration.GetConversationHistory(ctx, entry.ChannelID, req.MessageCount)
		if err != nil {
			return nil, fmt.Errorf("getting conversation history: %w", err)
		}
		for _, msg := range slackMessages {
			history = append(history, dto.SlackMessage{
				SubType:     msg.SubType,
				Text:        msg.Text,
				User:        msg.User,
				BotID:       msg.BotID,
				BotUsername: msg.Username,
			})
		}
	}
	if len(req.TestMessages) > 0 {
		for _, msg := range req.TestMessages {
			history = append(history, dto.SlackMessage{
				SubType:     "",
				Text:        msg,
				User:        "",
				BotID:       "",
				BotUsername: "",
			})
		}
	}
	return history, nil
}

var pageTemplate = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html>
<head>
	<meta name="htmx-config" content='{"inlineScriptNonce":"{{.Nonce}}", "inlineStyleNonce":"{{.Nonce}}"}'>
	<title>Test Channel Monitor</title>
	<script src="https://unpkg.com/htmx.org@2.0.4"></script>
	<style nonce="{{.Nonce}}">
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.6; max-width: 800px; margin: 40px auto; padding: 0 20px; }
		textarea { width: 100%; height: 300px; margin: 10px 0; font-family: monospace; }
		.spinner { display: none; }
		.htmx-request .spinner { display: inline; }
		.htmx-request .submit { display: none; }
		h1 { color: #2c3e50; border-bottom: 2px solid #eee; padding-bottom: 10px; }
		h3 { color: #34495e; margin-top: 30px; }
		details { background: #f8f9fa; padding: 10px; border-radius: 4px; margin: 10px 0; }
		summary { cursor: pointer; color: #2980b9; }
		pre { background: #f8f9fa; padding: 15px; border-radius: 4px; overflow-x: auto; }
		.error { color: #e74c3c; }
		hr { border: none; border-top: 1px solid #eee; margin: 30px 0; }
		.download-btn { 
			background: #2980b9; 
			color: white; 
			padding: 10px 20px; 
			border: none; 
			border-radius: 4px; 
			cursor: pointer; 
			margin: 20px 0;
			text-decoration: none;
			display: inline-block;
		}
		.download-btn:hover { background: #2471a3; }
	</style>
</head>
<body>
	<div id="report-input">
		<h1>Test Channel Monitor</h1>
		<form hx-post="{{.Prefix}}/test" hx-target="#result" hx-swap="innerHTML">
			<div>
				<label for="config">Config YAML:</label><br>
				<textarea name="config_yaml" id="config"></textarea>
			</div>
			<div>
				<label for="count">Recent Messages To Fetch From Slack Channel:</label><br>
				<input type="number" name="message_count" id="count" value="3">
			</div>
			<div>
				<label for="messages">Additional Messages to Test (separate with --- line):</label><br>
				<textarea name="test_messages" id="messages"></textarea>
			</div>
			<button type="submit" class="submit">Test</button>
			<span class="spinner">Testing...</span>
		</form>
	</div>
	<div id="result"></div>
	<script nonce="{{.Nonce}}">
		document.body.addEventListener('htmx:responseError', function(evt) {
			evt.detail.target.innerHTML = '<div class="error">Error: ' + evt.detail.error + '<br>Response: ' + evt.detail.xhr.responseText + '</div>';
		});
	</script>
</body>
</html>`))

var reportTemplate = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html>
<head>
<meta name="htmx-config" content='{"inlineScriptNonce":"{{.Nonce}}", "inlineStyleNonce":"{{.Nonce}}"}'>
<style nonce="{{.Nonce}}">
	body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.6; max-width: 800px; margin: 40px auto; padding: 0 20px; }
	h1 { color: #2c3e50; border-bottom: 2px solid #eee; padding-bottom: 10px; }
	h3 { color: #34495e; margin-top: 30px; }
	details { background: #f8f9fa; padding: 10px; border-radius: 4px; margin: 10px 0; }
	summary { cursor: pointer; color: #2980b9; }
	pre { background: #f8f9fa; padding: 15px; border-radius: 4px; overflow-x: auto; }
	.error { color: #e74c3c; }
	hr { border: none; border-top: 1px solid #eee; margin: 30px 0; }
	.download-btn { 
		background: #2980b9; 
		color: white; 
		padding: 10px 20px; 
		border: none; 
		border-radius: 4px; 
		cursor: pointer; 
		margin: 20px 0;
		text-decoration: none;
		display: inline-block;
	}
	.download-btn:hover { background: #2471a3; }
</style>
</head>
<body>
<h1>Test Channel Monitor Report</h1>
<button id="download-btn" class="download-btn">Download Report</button>
<script nonce="{{.Nonce}}">
function downloadReport() {
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const filename = 'channel-report-' + timestamp + '.html';
	// Clone the document and remove the report input content and download button
    const clonedDoc = document.documentElement.cloneNode(true);
    const reportInput = clonedDoc.querySelector('#report-input');
    reportInput.remove();
	const downloadBtn = clonedDoc.querySelector('#download-btn');
	downloadBtn.remove();
    const htmlContent = clonedDoc.outerHTML;
    
    const blob = new Blob([htmlContent], { type: 'text/html' });
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);
}
document.getElementById('download-btn').addEventListener('click', downloadReport);
</script>`))

func renderPage(w http.ResponseWriter, prefix string, nonce string) {
	w.Header().Set("Content-Type", "text/html")

	data := struct {
		Prefix string
		Nonce  string
	}{
		Prefix: prefix,
		Nonce:  nonce,
	}

	if err := pageTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("executing template: %v", err), http.StatusInternalServerError)
		return
	}
}

func renderTestReport(results []*testChannelMonitorReportData, nonce string) string {
	var reportHTML bytes.Buffer
	data := struct {
		Nonce string
	}{
		Nonce: nonce,
	}

	if err := reportTemplate.Execute(&reportHTML, data); err != nil {
		return fmt.Sprintf("Error executing template: %v", err)
	}

	for _, data := range results {
		reportHTML.WriteString(fmt.Sprintf(`
<h3>Message</h3>
<p>%s</p>
<details>
<summary>Prompt</summary>
<pre>%s</pre>
</details>`, html.EscapeString(data.Message.Text), html.EscapeString(data.Prompt)))

		if data.ValidatedOutput != "" {
			reportHTML.WriteString(fmt.Sprintf(`
<h3>Output</h3>
<pre>%s</pre>`, html.EscapeString(data.ValidatedOutput)))
		}

		if data.InvalidOutput != "" {
			reportHTML.WriteString(fmt.Sprintf(`
<h3>Invalid Output</h3>
<pre style="background: #ffebee; border-radius: 4px; padding: 10px; margin: 5px 0; color: #d32f2f;">%s</pre>`, html.EscapeString(data.InvalidOutput)))
		}

		if data.Error != "" {
			reportHTML.WriteString(fmt.Sprintf(`
<pre style="background: #ffebee; border-radius: 4px; padding: 10px; margin: 5px 0; color: #d32f2f;">Error: %s</pre>`, html.EscapeString(data.Error)))
		}

		reportHTML.WriteString("<hr>")
	}
	reportHTML.WriteString("</body></html>")
	return reportHTML.String()
}

// generateNonce creates a cryptographically secure random base64 string for CSP.
func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
