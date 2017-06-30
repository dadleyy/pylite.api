package device

import "io"
import "bytes"
import "encoding/hex"
import "crypto/sha256"
import "github.com/satori/go.uuid"
import "github.com/golang/protobuf/proto"

import "github.com/dadleyy/beacon.api/beacon/defs"
import "github.com/dadleyy/beacon.api/beacon/logging"
import "github.com/dadleyy/beacon.api/beacon/interchange"

// NewStreamerConnection returns a device connection who's underlying IO is managed through a streamer interface
func NewStreamerConnection(stream defs.Streamer, sign defs.Signer, id uuid.UUID) *StreamerConnection {
	logger := logging.New(defs.DeviceConnectionLogPrefix, logging.Red)
	return &StreamerConnection{logger, stream, sign, id}
}

// StreamerConnection is an implementation of the device.Connection interface using a websocket
type StreamerConnection struct {
	*logging.Logger
	defs.Streamer
	defs.Signer
	id uuid.UUID
}

// Send writes the provided byte data to the next available writer from the underlying streamer interface
func (connection *StreamerConnection) Send(message interchange.DeviceMessage) error {
	// create the message's digest
	s := sha256.New()

	if _, e := s.Write(message.Payload); e != nil {
		return e
	}

	digestBuffer := bytes.NewBuffer([]byte{})

	if e := connection.Sign(digestBuffer, s.Sum(nil)); e != nil {
		return e
	}

	digestString := hex.EncodeToString(digestBuffer.Bytes())

	connection.Debugf("sending digest string: %s", digestString)

	// Set the authentication message digest
	message.Authentication.MessageDigest = digestString

	d, e := proto.Marshal(&message)

	if e != nil {
		return e
	}

	w, e := connection.NextWriter(defs.TextWriter)

	if e != nil {
		return e
	}

	defer w.Close()

	_, e = w.Write(d)

	return e
}

// Receive returns the next available reader from the underlying streamer interface
func (connection *StreamerConnection) Receive() (io.Reader, error) {
	_, r, e := connection.NextReader()
	return r, e
}

// GetID returns the unique identifier created for this connection as a string
func (connection *StreamerConnection) GetID() string {
	return connection.id.String()
}
