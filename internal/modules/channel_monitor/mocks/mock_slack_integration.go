// Code generated by MockGen. DO NOT EDIT.
// Source: ../../slack_integration/slack.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	slack "github.com/slack-go/slack"
)

// MockIntegration is a mock of Integration interface.
type MockIntegration struct {
	ctrl     *gomock.Controller
	recorder *MockIntegrationMockRecorder
}

// MockIntegrationMockRecorder is the mock recorder for MockIntegration.
type MockIntegrationMockRecorder struct {
	mock *MockIntegration
}

// NewMockIntegration creates a new mock instance.
func NewMockIntegration(ctrl *gomock.Controller) *MockIntegration {
	mock := &MockIntegration{ctrl: ctrl}
	mock.recorder = &MockIntegrationMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockIntegration) EXPECT() *MockIntegrationMockRecorder {
	return m.recorder
}

// BotUserID mocks base method.
func (m *MockIntegration) BotUserID() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BotUserID")
	ret0, _ := ret[0].(string)
	return ret0
}

// BotUserID indicates an expected call of BotUserID.
func (mr *MockIntegrationMockRecorder) BotUserID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BotUserID", reflect.TypeOf((*MockIntegration)(nil).BotUserID))
}

// Client mocks base method.
func (m *MockIntegration) Client() *slack.Client {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Client")
	ret0, _ := ret[0].(*slack.Client)
	return ret0
}

// Client indicates an expected call of Client.
func (mr *MockIntegrationMockRecorder) Client() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Client", reflect.TypeOf((*MockIntegration)(nil).Client))
}

// GetBotChannels mocks base method.
func (m *MockIntegration) GetBotChannels() ([]slack.Channel, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetBotChannels")
	ret0, _ := ret[0].([]slack.Channel)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetBotChannels indicates an expected call of GetBotChannels.
func (mr *MockIntegrationMockRecorder) GetBotChannels() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetBotChannels", reflect.TypeOf((*MockIntegration)(nil).GetBotChannels))
}

// GetConversationHistory mocks base method.
func (m *MockIntegration) GetConversationHistory(ctx context.Context, channelID string, lastNMsgs int) ([]slack.Message, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetConversationHistory", ctx, channelID, lastNMsgs)
	ret0, _ := ret[0].([]slack.Message)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetConversationHistory indicates an expected call of GetConversationHistory.
func (mr *MockIntegrationMockRecorder) GetConversationHistory(ctx, channelID, lastNMsgs interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetConversationHistory", reflect.TypeOf((*MockIntegration)(nil).GetConversationHistory), ctx, channelID, lastNMsgs)
}

// GetConversationInfo mocks base method.
func (m *MockIntegration) GetConversationInfo(ctx context.Context, channelID string) (*slack.Channel, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetConversationInfo", ctx, channelID)
	ret0, _ := ret[0].(*slack.Channel)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetConversationInfo indicates an expected call of GetConversationInfo.
func (mr *MockIntegrationMockRecorder) GetConversationInfo(ctx, channelID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetConversationInfo", reflect.TypeOf((*MockIntegration)(nil).GetConversationInfo), ctx, channelID)
}

// GetConversationReplies mocks base method.
func (m *MockIntegration) GetConversationReplies(ctx context.Context, channelID, ts string) ([]slack.Message, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetConversationReplies", ctx, channelID, ts)
	ret0, _ := ret[0].([]slack.Message)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetConversationReplies indicates an expected call of GetConversationReplies.
func (mr *MockIntegrationMockRecorder) GetConversationReplies(ctx, channelID, ts interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetConversationReplies", reflect.TypeOf((*MockIntegration)(nil).GetConversationReplies), ctx, channelID, ts)
}

// GetUserIDByEmail mocks base method.
func (m *MockIntegration) GetUserIDByEmail(ctx context.Context, email string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetUserIDByEmail", ctx, email)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetUserIDByEmail indicates an expected call of GetUserIDByEmail.
func (mr *MockIntegrationMockRecorder) GetUserIDByEmail(ctx, email interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetUserIDByEmail", reflect.TypeOf((*MockIntegration)(nil).GetUserIDByEmail), ctx, email)
}

// LeaveChannel mocks base method.
func (m *MockIntegration) LeaveChannel(ctx context.Context, channelID string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LeaveChannel", ctx, channelID)
	ret0, _ := ret[0].(error)
	return ret0
}

// LeaveChannel indicates an expected call of LeaveChannel.
func (mr *MockIntegrationMockRecorder) LeaveChannel(ctx, channelID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LeaveChannel", reflect.TypeOf((*MockIntegration)(nil).LeaveChannel), ctx, channelID)
}

// PostMessage mocks base method.
func (m *MockIntegration) PostMessage(ctx context.Context, channelID string, messageBlocks ...slack.Block) error {
	m.ctrl.T.Helper()
	varargs := []interface{}{ctx, channelID}
	for _, a := range messageBlocks {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "PostMessage", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// PostMessage indicates an expected call of PostMessage.
func (mr *MockIntegrationMockRecorder) PostMessage(ctx, channelID interface{}, messageBlocks ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{ctx, channelID}, messageBlocks...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PostMessage", reflect.TypeOf((*MockIntegration)(nil).PostMessage), varargs...)
}

// PostThreadReply mocks base method.
func (m *MockIntegration) PostThreadReply(ctx context.Context, channelID, ts string, messageBlocks ...slack.Block) error {
	m.ctrl.T.Helper()
	varargs := []interface{}{ctx, channelID, ts}
	for _, a := range messageBlocks {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "PostThreadReply", varargs...)
	ret0, _ := ret[0].(error)
	return ret0
}

// PostThreadReply indicates an expected call of PostThreadReply.
func (mr *MockIntegrationMockRecorder) PostThreadReply(ctx, channelID, ts interface{}, messageBlocks ...interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]interface{}{ctx, channelID, ts}, messageBlocks...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PostThreadReply", reflect.TypeOf((*MockIntegration)(nil).PostThreadReply), varargs...)
}

// Run mocks base method.
func (m *MockIntegration) Run(ctx context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

// Run indicates an expected call of Run.
func (mr *MockIntegrationMockRecorder) Run(ctx interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockIntegration)(nil).Run), ctx)
}
