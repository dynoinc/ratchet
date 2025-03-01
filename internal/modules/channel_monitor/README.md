# Channel Monitor Module

The channel monitor module allows you to monitor slack channels and send slack messages based on the message, a prompt, and an executable.

## Configuration
To enable channel monitor, set the environment variable RATCHET_CHANNEL_MONITOR_CONFIG_FILE to the path of the configuration file. 

The configuration file contains a set of channel monitor configurations. Each configuration contains the following fields:

- channel_id: the id of the slack channel to monitor, e.g. C0123ABC, the app needs to be added to the channel
- prompt: The prompt to send to the llm to process. The prompt uses golang's text template to render.
- result_schema: The schema of the response you expect from the llm. The schema is a json object, but in yaml format.
- executable: The executable to run to process the message. The executable takes input via stdin and outputs to stdout.
- executable_args: List of arguments to pass to the executable.

### Prompt Template Variables
The prompt template has access to the following variables:
 - `{{.Message.Text}}` - The text of the message
 - `{{.Message.User}}` - The slack ID of the user who sent the message

### Executable Input JSON 
The executable reads a json object from stdin with the following fields:
- slug: The key of the channel monitor configuration entry
- channel_id: The id of the slack channel being monitored
- slack_ts: The timestamp of the slack message
- llm_output: The output of the llm as a string of escaped json
- message: Information about the original slack message
  - text: The text of the message
  - user: The slack ID of the user who sent the message

### Executable Output JSON
The executable should output a json object with the following fields:
- direct_messages: A list direct messages to send
  - email: The email of the user to send the message to
  - text: The text of the message
- channel_messages: A list of channel messages to send
  - channel_id: The id of the channel to send the message to
  - text: The text of the message
  - slack_ts: The timestamp of the slack message to reply to (optional)

## Example Configuration

In this example, we monitor a channel for questions about kubernetes. 
The monitor will send a DM to a specified user if the llm 
determines the message is a question about kubernetes and user is asking for help.
```yaml
channel_monitor_1:
    channel_id: C0123ABC
    prompt: >
      If the message below the horizontal line is asking a question about the kubernetes cluster, respond with {"help": true}. Otherwise, respond with {"help": false}.
      
      ---
      {{.Message.Text}}
    result_schema: 
        type: object
        properties:
            help:
                type: boolean
    executable: jq
    executable_args: 
        - '(.llm_output | fromjson | .help) as $help | if $help then {"direct_messages": [{"email": "mike@example.com", "text": "User posted message in channel asking about k8s: \(.message.text)"}]} else {} end'
```

## Testing a Channel Prompt

To test a prompt, navigate to the `/channel-monitor/test` route on the ratchet http server and enter the yaml for the channel, prompt, and result_schema you want to test.

From there you can run the prompt on recent messages in the channel or test messages entered in the web page.

Example yaml input
```yaml
channel_id: C0123ABC
prompt: >
  If the message below the horizontal line is asking a question about the kubernetes cluster, respond with {"help": true}. Otherwise, respond with {"help": false}.
  
  ---
  {{.Message.Text}}
result_schema: 
  type: object
  properties:
      help:
          type: boolean
  required:
  - text
```