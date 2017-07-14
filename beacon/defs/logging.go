package defs

import "log"

const (
	// DebugLogLevelTag is used for debugf logger calls
	DebugLogLevelTag = "debug"

	// InfoLogLevelTag is used for infof logger calls
	InfoLogLevelTag = "info"

	// WarnLogLevelTag is used for errorf logger calls
	WarnLogLevelTag = "warn"

	// ErrorLogLevelTag is used for errorf logger calls
	ErrorLogLevelTag = "error"

	// MainLogPrefix is the log prefix for the main go routine
	MainLogPrefix = "[beacon api] "

	// ServerKeyLogPrefix log prefix used by server key
	ServerKeyLogPrefix = "[server key] "

	// RegistryLogPrefix is the log prefix for the device registry
	RegistryLogPrefix = "[device registry] "

	// ServerRuntimeLogPrefix is the log prefix for the http server runtime
	ServerRuntimeLogPrefix = "[server runtime] "

	// DeviceConnectionLogPrefix is the log prefix for the device connections
	DeviceConnectionLogPrefix = "[device connection] "

	// DeviceControlLogPrefix is the log prefix for the device control processor
	DeviceControlLogPrefix = "[device control] "

	// DeviceFeedbackLogPrefix is the log prefix for the device feeback processor
	DeviceFeedbackLogPrefix = "[device feedback] "

	// DefaultLoggerFlags is the bitmask used to create default logging
	DefaultLoggerFlags = log.Ldate | log.Ltime
)
