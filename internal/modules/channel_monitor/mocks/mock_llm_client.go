// Code generated by MockGen. DO NOT EDIT.
// Source: ../../llm/client.go
//
// Generated by this command:
//
//	mockgen -destination=mocks/mock_llm_client.go -package=mocks -source=../../llm/client.go Client
//

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	llm "github.com/dynoinc/ratchet/internal/llm"
	schema "github.com/dynoinc/ratchet/internal/storage/schema"
	jsonschema "github.com/qri-io/jsonschema"
	gomock "go.uber.org/mock/gomock"
)

// MockClient is a mock of Client interface.
type MockClient struct {
	ctrl     *gomock.Controller
	recorder *MockClientMockRecorder
	isgomock struct{}
}

// MockClientMockRecorder is the mock recorder for MockClient.
type MockClientMockRecorder struct {
	mock *MockClient
}

// NewMockClient creates a new mock instance.
func NewMockClient(ctrl *gomock.Controller) *MockClient {
	mock := &MockClient{ctrl: ctrl}
	mock.recorder = &MockClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockClient) EXPECT() *MockClientMockRecorder {
	return m.recorder
}

// Config mocks base method.
func (m *MockClient) Config() llm.Config {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Config")
	ret0, _ := ret[0].(llm.Config)
	return ret0
}

// Config indicates an expected call of Config.
func (mr *MockClientMockRecorder) Config() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Config", reflect.TypeOf((*MockClient)(nil).Config))
}

// CreateRunbook mocks base method.
func (m *MockClient) CreateRunbook(ctx context.Context, service, alert string, msgs []schema.ThreadMessagesV2) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateRunbook", ctx, service, alert, msgs)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateRunbook indicates an expected call of CreateRunbook.
func (mr *MockClientMockRecorder) CreateRunbook(ctx, service, alert, msgs any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateRunbook", reflect.TypeOf((*MockClient)(nil).CreateRunbook), ctx, service, alert, msgs)
}

// GenerateChannelSuggestions mocks base method.
func (m *MockClient) GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GenerateChannelSuggestions", ctx, messages)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GenerateChannelSuggestions indicates an expected call of GenerateChannelSuggestions.
func (mr *MockClientMockRecorder) GenerateChannelSuggestions(ctx, messages any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GenerateChannelSuggestions", reflect.TypeOf((*MockClient)(nil).GenerateChannelSuggestions), ctx, messages)
}

// GenerateEmbedding mocks base method.
func (m *MockClient) GenerateEmbedding(ctx context.Context, task, text string) ([]float32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GenerateEmbedding", ctx, task, text)
	ret0, _ := ret[0].([]float32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GenerateEmbedding indicates an expected call of GenerateEmbedding.
func (mr *MockClientMockRecorder) GenerateEmbedding(ctx, task, text any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GenerateEmbedding", reflect.TypeOf((*MockClient)(nil).GenerateEmbedding), ctx, task, text)
}

// RunJSONModePrompt mocks base method.
func (m *MockClient) RunJSONModePrompt(ctx context.Context, prompt string, schema *jsonschema.Schema) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RunJSONModePrompt", ctx, prompt, schema)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RunJSONModePrompt indicates an expected call of RunJSONModePrompt.
func (mr *MockClientMockRecorder) RunJSONModePrompt(ctx, prompt, schema any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RunJSONModePrompt", reflect.TypeOf((*MockClient)(nil).RunJSONModePrompt), ctx, prompt, schema)
}
