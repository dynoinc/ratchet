# Project TODO: Refactor Message Processing Architecture

This document outlines the plan to refactor the bot's core message processing logic to be more robust, stateful, and extensible using a Finite State Machine (FSM) based workflow engine.

## 1. High-Level Goal

The primary goal is to transition from a simple command-response model to a stateful workflow-driven architecture. This will allow the bot to handle complex, multi-step interactions, including those that require user approval, while maintaining a clear and maintainable codebase.

## 2. New Architecture Overview

The new message processing pipeline will consist of three main stages:

```
Incoming Message -> [1. Intent Engine] -> [2. Workflow Engine] -> Outgoing Response
                      (Classify Intent,   (Execute FSM,
                       Select Workflow)    Use Tools)
```

1.  **Intent Engine:** Receives a raw message and determines the appropriate workflow based on message type:
    - Bot messages: Uses classifier to detect alerts and triggers runbook workflow
    - Human messages with @bot: Uses LLM to determine intent and select appropriate workflow
    - Channel monitor messages: Triggers channel-specific workflows based on configuration
2.  **Workflow Engine:** Receives the selected workflow from the Intent Engine. It is responsible for executing the workflow, which is defined as a Finite State Machine (FSM). The engine manages the state transitions, executes associated business logic (often by calling "tools"), and handles pausing and resuming the workflow for events like user approvals.
3.  **Tools:** These are self-contained units of functionality (e.g., `docsearch`, `docupdate`, `upstream_search`) that workflows can execute to perform actions. They are stateless and receive all necessary data as input.

## 3. Tracing Strategy

End-to-end tracing will cover the entire message processing lifecycle:

```
[Root Trace: Message Processing]
├── [Span: Intent Classification]
│   ├── [Span: Message Source Detection]
│   ├── [Span: Alert Detection] (if bot message)
│   ├── [Span: LLM Intent Classification] (if @bot mention)
│   └── [Span: Channel Pattern Match] (if monitored channel)
├── [Span: Workflow Execution]
│   ├── [Span: FSM State: Start]
│   ├── [Span: FSM State: Processing]
│   │   └── [Span: Tool Execution]
│   │       ├── [Span: Tool Input Preparation]
│   │       ├── [Span: Tool Call]
│   │       └── [Span: Tool Response Processing]
│   └── [Span: FSM State: Complete]
└── [Span: Response Delivery]
```

Key Trace Attributes:
- message_id: Unique identifier for the message
- channel_id: Slack channel identifier
- workflow_type: Type of workflow selected
- tool_name: Name of tool being executed
- state_name: Current FSM state
- error_type: Type of error if any occurred

## 4. Component Separation Strategy

Before implementing the new architecture, we need to break down existing monolithic components into separate intent classification and execution logic:

### Channel Monitor Separation
Current: `channel_monitor` handles both message matching and action execution in one component.
```
[Channel Monitor]
├── Configuration Loading
├── Message Pattern Matching
└── Action Execution (DMs, Replies)
```

After separation:
```
[Intent Engine]                    [Workflow Engine]
├── Channel Config Loading    ->   [Channel Action Workflow]
└── Message Pattern Matching  ->   ├── Execute Actions
                                  └── Send Responses
```

### Classifier Separation
Current: `classifier` combines alert detection and immediate action.
```
[Classifier]
├── Alert Detection
└── Immediate Response
```

After separation:
```
[Intent Engine]              [Workflow Engine]
├── Alert Detection    ->   [Alert Workflow]
└── Intent Mapping     ->   └── Response Actions
```

## 5. Detailed Implementation Plan

### Phase 1: Break Down Monolithic Components

-   [ ] **Separate Channel Monitor Logic:**
    -   Create `internal/modules_worker/intent/channel_patterns.go`
        - Move configuration loading
        - Move message pattern matching
        - Define pattern match result types
    -   Create `internal/modules_worker/workflow/channel_actions.go`
        - Move action execution logic
        - Define action types and parameters
        - Create FSM states for actions
    -   Update tests to verify separation

-   [ ] **Separate Classifier Logic:**
    -   Create `internal/modules_worker/intent/alert_detection.go`
        - Move alert classification logic
        - Define alert detection result types
    -   Create `internal/modules_worker/workflow/alert_response.go`
        - Move response generation logic
        - Define response types
        - Create FSM states for responses
    -   Extract embedding generation into `internal/modules_worker/intent/embedding_generator.go` and **persist pgvector embeddings** for each message so documentation similarity search continues to work

-   [ ] **Move Runbook to Tools:**
    -   Move `internal/modules/runbook` to `internal/inbuilt_tools/runbook`
    -   Convert to stateless tool interface
    -   Preserve runbook generation and formatting
    -   Update to use tool response format

### Phase 2: Intent Engine

-   [ ] **Create Intent Engine Core:**
    -   Create `internal/modules_worker/intent/engine.go`
    -   Implement message source detection (bot vs human vs channel monitor)
    -   Define workflow selection logic
    -   Add message validation and preprocessing

-   [ ] **Implement Bot Message Processing:**
    -   Use separated alert detection logic
    -   Create AlertRunbookWorkflow FSM definition:
        ```
        [Start] -> [DetectAlert] -> [GenerateRunbook] -> [PostToSlack] -> [End]
        ```
    -   Add alert context extraction

-   [ ] **Implement Human Message Processing:**
    -   Add @bot mention detection
    -   Create LLM prompt for intent classification
    -   Define available workflows and mapping
    -   Handle conversation context and history

-   [ ] **Implement Channel Monitor Processing:**
    -   Use separated pattern matching logic
    -   Create ChannelMonitorWorkflow FSM definition:
        ```
        [Start] -> [ValidatePattern] -> [ExecuteActions] -> [HandleResponse] -> [End]
        ```
    -   Support dynamic workflow configuration
    -   **Implement Thread Message Processing:** FSMs must be able to receive and act on thread replies. Support context continuation and state updates when a message arrives with `parent_ts`.
    -   **Handle Backfill Messages:**
        - Detect `IsBackfill` flag early and set `context.backfill=true` in Intent Engine.
        - **ONLY** run embedding generation and alert detection steps; skip channel pattern matching, LLM intent classification, and all Workflow/FSM execution.
        - Persist embeddings and alert‐classification results to the database.
        - Backfill messages don't need any more processing or workflow execution.

### Phase 3: Workflow Engine

-   [ ] **Create Workflow Engine:**
    -   Create `internal/modules_worker/workflow/engine.go`
    -   Implement FSM state management
    -   Handle workflow execution and state transitions
    -   Integrate with River queue for job scheduling/execution so workflows can be processed asynchronously with existing infrastructure.

-   [ ] **Define Core Workflows:**
    -   AlertRunbookWorkflow (using separated alert response logic)
    -   ChannelMonitorWorkflow (using separated action execution)
    -   UserCommandWorkflow (for @bot interactions)

-   [ ] **Implement Tool Orchestration:**
    -   Create `internal/modules_worker/workflow/tool_orchestrator.go`
    -   Integrate with inbuilt_tools
    -   Handle tool execution and results
    -   Allow for up to 5 iterations of LLM tool-use loops for complex queries to preserve existing command behavior.
    -   Record each LLM call in `llmusage` table via a middleware on the shared LLM client to preserve usage analytics.

### Phase 4: Tool Integration

-   [ ] **Tool Categories to Support:**
    -   Documentation tools (docsearch, docread, docupdate)
    -   Runbook generation
    -   Usage reporting tools
    -   Upstream search tools

-   [ ] **Tool Integration:**
    -   Ensure all tools follow consistent interface
    -   Implement tool result handling in FSM states
    -   Add error handling and retry logic
    -   Record each LLM call in `llmusage` table via a middleware on the shared LLM client to preserve usage analytics.

-   [ ] **Tool Testing:**
    -   Add unit tests for each tool
    -   Test tool integration with FSM
    -   Test error handling and retries

### Phase 5: Testing and Documentation

-   [ ] **Add Unit Tests:**
    -   Test separated components independently:
        - Pattern matching without action execution
        - Alert detection without response generation
    -   Test workflow integrations:
        - Bot message + alert -> runbook workflow
        - @bot mention -> LLM intent -> specific workflow
        - Channel monitor -> configured workflow
    -   Test FSM state transitions
    -   Test tool orchestration

-   [ ] **Add Integration Tests:**
    -   Test end-to-end workflows
    -   Test state persistence
    -   Test tool execution

-   [ ] **Documentation:**
    -   Update architecture documentation
    -   Document new workflow creation process
    -   Document tool integration process

### Phase 6: Cleanup and Deployment

-   [ ] **Remove Old Code:**
    -   Remove `internal/modules` package
    -   Clean up deprecated interfaces
    -   Remove unused code paths

-   [ ] **Deployment Strategy:**
    -   Plan gradual rollout
    -   Monitor performance metrics
    -   Track error rates

-   [ ] **Performance Optimization:**
    -   Add caching where appropriate
    -   Optimize database queries
    -   Profile and optimize FSM transitions

### Phase 7: Observability Implementation

-   [ ] **Create Trace Manager:**
    -   Create `internal/otel/trace/manager.go`
    -   Implement trace context propagation
    -   Define standard attribute keys
    -   Add helper functions for common operations

-   [ ] **Add Intent Engine Tracing:**
    -   Add root trace for message processing
    -   Add spans for message source detection
    -   Add spans for each type of intent classification
    -   Include relevant attributes (message_id, channel_id, etc.)

-   [ ] **Add Workflow Engine Tracing:**
    -   Add spans for workflow execution
    -   Add spans for FSM state transitions
    -   Track workflow type and state information
    -   Measure state transition durations

-   [ ] **Add Tool Execution Tracing:**
    -   Add spans for tool preparation
    -   Add spans for tool execution
    -   Track tool name and parameters
    -   Measure tool execution duration

-   [ ] **Add Error Tracking:**
    -   Add error attributes to spans
    -   Track error types and frequencies
    -   Add stack traces for debugging
    -   Implement error recovery tracking

-   [ ] **Add Metrics:**
    -   Track message processing latency
    -   Track workflow execution times
    -   Track tool execution times
    -   Track error rates by component
    -   Track embedding generation durations and success rates
    -   Track backfill throughput, backlog size, and skipped workflow counts

-   [ ] **Add Trace Testing:**
    -   Test trace context propagation
    -   Verify span hierarchy
    -   Test error tracking
    -   Test metric collection

-   [ ] **Add Monitoring:**
    -   Set up trace visualization
    -   Create performance dashboards
    -   Set up alerting on error rates
    -   Monitor trace completeness 

## 6. Final Directory Structure

This is the target directory structure after the refactor is complete.

```
internal/
├── bot/
│   └── bot.go              # Core bot logic, message ingestion, channel management
├── workers/
│   ├── intent/             # Intent classification and workflow selection
│   │   ├── engine.go
│   │   ├── alert_detection.go
│   │   ├── channel_patterns.go
│   │   └── embedding_generator.go
│   ├── workflow/           # FSM-based workflow execution
│   │   ├── engine.go
│   │   ├── tool_orchestrator.go
│   │   ├── alert_response.go
│   │   └── channel_actions.go
├── tools/                  # All stateless, executable tools
│   ├── docsearch/
│   ├── docread/
│   ├── docupdate/
│   ├── runbook/
│   ├── ...
├── slack_integration/      # Slack API interaction
├── llm/                    # Language model client and utilities
├── storage/                # Database schemas, queries, and migrations
└── ... (other existing packages)
```

**Purpose of Key Directories:**

*   **`internal/bot`**: Contains the core `Bot` struct and functions for interacting with the database, handling initial message receipt from Slack, and managing channel state. It is the main entry point for incoming data.
*   **`internal/workers`**: This is the heart of the new architecture, containing all the logic for processing messages asynchronously.
    *   **`intent/`**: The Intent Engine. Its responsibility is to analyze an incoming message and decide which workflow should be triggered. It does not execute any business logic itself.
    *   **`workflow/`**: The Workflow Engine. It takes the intent from the `intent` package and executes the corresponding Finite State Machine (FSM). It manages state, orchestrates tools, and handles the core business logic of a given task.
*   **`internal/tools`**: Replaces `inbuilt_tools`. This directory will house all the stateless, single-purpose functions (tools) that can be called by workflows. For example, `docsearch`, `runbook` generation, etc.
*   **`internal/slack_integration`**: Remains the dedicated layer for all communication with the Slack API.
*   **`internal/llm`**: Remains the client for interacting with Large Language Models.
*   **`internal/storage`**: No changes. Continues to manage all database interactions, schemas, and migrations.
*   **`internal/otel`**: A centralized place for managing OpenTelemetry tracing setup, context propagation, and standardized attributes to ensure consistent observability across the system. 