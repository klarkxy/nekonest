package protocol

import core "github.com/klarkxy/nekonest/relaycore/protocol"

// This package is the standalone Server's compatibility import path. The
// canonical public wire contract lives in relaycore/protocol so other
// deployment shells do not depend on a Go internal package.
type (
	MessageType                = core.MessageType
	TransportMode              = core.TransportMode
	KeyScope                   = core.KeyScope
	ControlMode                = core.ControlMode
	AttachmentMode             = core.AttachmentMode
	SealedPayload              = core.SealedPayload
	NekoMessage                = core.NekoMessage
	Device                     = core.Device
	AgentStatus                = core.AgentStatus
	AgentType                  = core.AgentType
	AgentStartCapability       = core.AgentStartCapability
	SessionListPayload         = core.SessionListPayload
	SessionCapabilities        = core.SessionCapabilities
	AgentSession               = core.AgentSession
	ActiveTurnBinding          = core.ActiveTurnBinding
	PendingApproval            = core.PendingApproval
	UserInputOption            = core.UserInputOption
	UserInputQuestion          = core.UserInputQuestion
	PendingUserInput           = core.PendingUserInput
	AttachmentRef              = core.AttachmentRef
	StartThreadPayload         = core.StartThreadPayload
	ThreadStartState           = core.ThreadStartState
	ThreadStartResult          = core.ThreadStartResult
	SessionMessage             = core.SessionMessage
	PairCode                   = core.PairCode
	VersionParts               = core.VersionParts
	MessageBodyPolicy          = core.MessageBodyPolicy
	HandshakeResult            = core.HandshakeResult
	ServiceErrorCode           = core.ServiceErrorCode
	ServiceErrorPayload        = core.ServiceErrorPayload
	ConnectionState            = core.ConnectionState
	DeviceRegistrationResponse = core.DeviceRegistrationResponse
)

const (
	CurrentProtocolVersion = core.CurrentProtocolVersion
	CurrentProtocolMajor   = core.CurrentProtocolMajor
	CurrentProtocolMinor   = core.CurrentProtocolMinor

	MsgDeviceOnline        = core.MsgDeviceOnline
	MsgDeviceOffline       = core.MsgDeviceOffline
	MsgDeviceList          = core.MsgDeviceList
	MsgSessionList         = core.MsgSessionList
	MsgRefreshSessions     = core.MsgRefreshSessions
	MsgSessionUpdate       = core.MsgSessionUpdate
	MsgSessionMessage      = core.MsgSessionMessage
	MsgSendPrompt          = core.MsgSendPrompt
	MsgPromptStatusQuery   = core.MsgPromptStatusQuery
	MsgPromptNotSeen       = core.MsgPromptNotSeen
	MsgPromptQueued        = core.MsgPromptQueued
	MsgPromptAccepted      = core.MsgPromptAccepted
	MsgPromptCommitted     = core.MsgPromptCommitted
	MsgPromptFailed        = core.MsgPromptFailed
	MsgPromptSent          = core.MsgPromptSent
	MsgApprove             = core.MsgApprove
	MsgDeny                = core.MsgDeny
	MsgInterrupt           = core.MsgInterrupt
	MsgSteer               = core.MsgSteer
	MsgRespondUserInput    = core.MsgRespondUserInput
	MsgUserInputResult     = core.MsgUserInputResult
	MsgQueueUpdate         = core.MsgQueueUpdate
	MsgCancelPrompt        = core.MsgCancelPrompt
	MsgPromptCancelled     = core.MsgPromptCancelled
	MsgResumePromptQueue   = core.MsgResumePromptQueue
	MsgSkipPromptQueueItem = core.MsgSkipPromptQueueItem
	MsgStartThread         = core.MsgStartThread
	MsgThreadStarting      = core.MsgThreadStarting
	MsgThreadOwned         = core.MsgThreadOwned
	MsgThreadFailed        = core.MsgThreadFailed
	MsgThreadIndeterminate = core.MsgThreadIndeterminate
	MsgHeartbeat           = core.MsgHeartbeat
	MsgError               = core.MsgError
	MsgRegisterDevice      = core.MsgRegisterDevice
	MsgAuthResponse        = core.MsgAuthResponse
	MsgPairRequest         = core.MsgPairRequest
	MsgPairConfirm         = core.MsgPairConfirm
	MsgPairReady           = core.MsgPairReady
	MsgPairFailed          = core.MsgPairFailed
	MsgKeyPackage          = core.MsgKeyPackage
	MsgPhoneRevoked        = core.MsgPhoneRevoked
	MsgAttentionEvent      = core.MsgAttentionEvent
	MsgSubscribe           = core.MsgSubscribe
	MsgSubscribeAck        = core.MsgSubscribeAck
	MsgFetchHistory        = core.MsgFetchHistory
	MsgSessionHistory      = core.MsgSessionHistory

	ErrCodeVersionMismatch       = core.ErrCodeVersionMismatch
	ErrCodeTransportModeMismatch = core.ErrCodeTransportModeMismatch
	ErrCodeInvalidEnvelope       = core.ErrCodeInvalidEnvelope

	TransportSealed              = core.TransportSealed
	TransportOpen                = core.TransportOpen
	KeyScopeDeviceCatalog        = core.KeyScopeDeviceCatalog
	KeyScopeSession              = core.KeyScopeSession
	ControlAppServer             = core.ControlAppServer
	ControlExecResume            = core.ControlExecResume
	ControlCompatibility         = core.ControlCompatibility
	AttachmentNativeImageAndFile = core.AttachmentNativeImageAndFile
	AttachmentNativeImage        = core.AttachmentNativeImage
	AttachmentPathBestEffort     = core.AttachmentPathBestEffort
	AttachmentUnsupported        = core.AttachmentUnsupported

	AgentIdle            = core.AgentIdle
	AgentRunning         = core.AgentRunning
	AgentWaitingUser     = core.AgentWaitingUser
	AgentWaitingApproval = core.AgentWaitingApproval
	AgentError           = core.AgentError
	AgentClaudeCode      = core.AgentClaudeCode
	AgentCodex           = core.AgentCodex
	AgentKilo            = core.AgentKilo
	AgentKimiCLI         = core.AgentKimiCLI
	AgentGrokBuild       = core.AgentGrokBuild

	ThreadStartStarting      = core.ThreadStartStarting
	ThreadStartOwned         = core.ThreadStartOwned
	ThreadStartFailed        = core.ThreadStartFailed
	ThreadStartIndeterminate = core.ThreadStartIndeterminate

	MessageBodyUnknown     = core.MessageBodyUnknown
	MessageBodyRouting     = core.MessageBodyRouting
	MessageBodyApplication = core.MessageBodyApplication
	ConnectionReady        = core.ConnectionReady
	ConnectionProvisioning = core.ConnectionProvisioning
)

var (
	CapabilityBool            = core.CapabilityBool
	ParseProtocolVersion      = core.ParseProtocolVersion
	FormatProtocolVersion     = core.FormatProtocolVersion
	ParseTransportMode        = core.ParseTransportMode
	NegotiateProtocolVersion  = core.NegotiateProtocolVersion
	ValidateEnvelopeForm      = core.ValidateEnvelopeForm
	BodyPolicy                = core.BodyPolicy
	ValidateFrameForTransport = core.ValidateFrameForTransport
	NewMessage                = core.NewMessage
	NewMessageWithSession     = core.NewMessageWithSession
	NegotiateHandshake        = core.NegotiateHandshake
	HandshakeErrorPayload     = core.HandshakeErrorPayload
)
