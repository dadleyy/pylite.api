package defs

const (
	// SecurityUserDeviceTokenSize is the size of user device tokens
	SecurityUserDeviceTokenSize = 20

	// SecurityUserDeviceNameMinLength is the size of user device tokens
	SecurityUserDeviceNameMinLength = 5

	// SecurityMinimumDeviceSharedSecretSize is the minimum size of shared secrets
	SecurityMinimumDeviceSharedSecretSize = 20
)

// DeviceTokenPermissions is a bitmask used to authorize device actions
type DeviceTokenPermissions uint

const (
	// SecurityDeviceTokenPermissionViewer - get state
	SecurityDeviceTokenPermissionViewer = 1 << iota

	// SecurityDeviceTokenPermissionController - control lights
	SecurityDeviceTokenPermissionController

	// SecurityDeviceTokenPermissionAdmin - control lights + tokens
	SecurityDeviceTokenPermissionAdmin
)

const (
	// SecurityDeviceTokenPermissionAll is all permissions
	SecurityDeviceTokenPermissionAll = SecurityDeviceTokenPermissionAdmin |
		SecurityDeviceTokenPermissionController |
		SecurityDeviceTokenPermissionViewer
)
