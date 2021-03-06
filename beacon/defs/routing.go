package defs

import "regexp"

var shorthandColors = "red|blue|green|off|rand|[0-9a-f]{6}"

var (
	// DeviceListRoute is the regular expression used for the device list route
	DeviceListRoute = regexp.MustCompile("^/devices$")

	// DeviceShorthandRoute is the regular expression used for the device shorthand route
	DeviceShorthandRoute = regexp.MustCompile("^/devices/(?P<uuid>[\\d\\w\\-]+)/(?P<color>" + shorthandColors + ")$")

	// DeviceRegistrationRoute is used by devices to register with the server
	DeviceRegistrationRoute = regexp.MustCompile("^/register$")

	// DeviceTokensRoute is used to create device tokens for a given device.
	DeviceTokensRoute = regexp.MustCompile("^/device-tokens$")

	// DeviceFeedbackRoute is used to receive device feedback from clients.
	DeviceFeedbackRoute = regexp.MustCompile("^/device-feedback$")

	// DeviceMessagesRoute is used to create device messages.
	DeviceMessagesRoute = regexp.MustCompile("^/device-messages$")

	// SystemRoute prints out system information
	SystemRoute = regexp.MustCompile("^/system$")
)
