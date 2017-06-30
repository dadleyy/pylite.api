package routes

import "crypto/rsa"
import "crypto/x509"
import "encoding/hex"
import "github.com/garyburd/redigo/redis"

import "github.com/satori/go.uuid"
import "github.com/dadleyy/beacon.api/beacon/net"
import "github.com/dadleyy/beacon.api/beacon/defs"
import "github.com/dadleyy/beacon.api/beacon/device"
import "github.com/dadleyy/beacon.api/beacon/security"

// NewRegistrationAPI returns a constructed registration api
func NewRegistrationAPI(stream device.RegistrationStream, registry device.Registry, store redis.Conn) *Registration {
	return &Registration{stream, registry, store}
}

// Registration route engine handles receiving http reqests, upgrading and sending along to the registation stream
type Registration struct {
	stream   device.RegistrationStream
	registry device.Registry
	store    redis.Conn
}

// Preregister is used to submit a new registation request for a device
func (registrations *Registration) Preregister(runtime *net.RequestRuntime) net.HandlerResult {
	request := struct {
		SharedSecret string `json:"shared_secret"`
		Name         string `json:"name"`
	}{}

	if e := runtime.ReadBody(&request); e != nil {
		runtime.Warnf("invalid request: %s", e.Error())
		return runtime.LogicError("bad-request")
	}

	if valid := len(request.Name) > 1 && len(request.SharedSecret) > 1; !valid {
		runtime.Warnf("invalid registration request: %v", request)
		return runtime.LogicError("bad-request")
	}

	if _, e := registrations.registry.Find(request.Name); e == nil {
		runtime.Warnf("duplicate device name registration: %v", request)
		return runtime.LogicError("duplicate-name")
	}

	block, e := hex.DecodeString(request.SharedSecret)

	if e != nil {
		runtime.Warnf("invalid shared secret: %s", e.Error())
		return runtime.LogicError("invalid-key")
	}

	pub, e := x509.ParsePKIXPublicKey(block)

	if e != nil {
		runtime.Warnf("invalid shared secret: %s", e.Error())
		return runtime.LogicError("invalid-key")
	}

	if _, ok := pub.(*rsa.PublicKey); ok != true {
		runtime.Warnf("incorrect shared secret key, not rsa format: %s", request.SharedSecret)
		return runtime.LogicError("bad-key-format")
	}

	details := device.RegistrationRequest(request)

	if e := registrations.registry.Allocate(details); e != nil {
		runtime.Errorf("unable to allocate registration: %s", e.Error())
		return runtime.ServerError()
	}

	runtime.Infof("successfully pre-registered device: %s", details.Name)

	return net.HandlerResult{}
}

// Register is the route handler responsible for upgrating + registering connections
func (registrations *Registration) Register(runtime *net.RequestRuntime) net.HandlerResult {
	connection, e := runtime.Websocket()

	if e != nil {
		runtime.Warnf("unable to upgrade websocket: %s", e.Error())
		return net.HandlerResult{Errors: []error{e}}
	}

	encodedSecret, uuid := runtime.Header.Get(defs.APIAuthorizationHeader), uuid.NewV4()

	deviceKey, e := security.ParseDeviceKey(encodedSecret)

	if e != nil {
		runtime.Warnf("invalid hex shared secret: %s", e.Error())
		connection.Close()
		return net.HandlerResult{NoRender: true}
	}

	if e := registrations.registry.Fill(encodedSecret, uuid.String()); e != nil {
		runtime.Warnf("unable to push device id into store: %s", e.Error())
		connection.Close()
		return net.HandlerResult{NoRender: true}
	}

	registrations.stream <- device.NewStreamerConnection(connection, uuid, deviceKey)
	return net.HandlerResult{NoRender: true}
}
