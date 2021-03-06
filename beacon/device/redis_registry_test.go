package device

import "log"
import "fmt"
import "bytes"
import "strconv"
import "testing"
import "strings"
import "github.com/franela/goblin"
import "github.com/golang/protobuf/proto"
import "github.com/garyburd/redigo/redis"
import "github.com/rafaeljusto/redigomock"
import "github.com/dadleyy/beacon.api/beacon/defs"
import "github.com/dadleyy/beacon.api/beacon/logging"
import "github.com/dadleyy/beacon.api/beacon/interchange"

const (
	permissionField = defs.RedisDeviceTokenPermissionField
)

func mask(strval string) uint {
	v, _ := strconv.ParseUint(strval, 2, 32)
	return uint(v)
}

func genFeedback() []byte {
	payload, _ := proto.Marshal(&interchange.FeedbackMessage{})
	return payload
}

type fakeTokenGenerator struct {
	t string
	e error
}

func (f *fakeTokenGenerator) GenerateToken() (string, error) {
	return f.t, f.e
}

var (
	generator fakeTokenGenerator
)

type redisMock struct {
	c *redigomock.Conn
}

func (r *redisMock) Close() error {
	return nil
}

func (r *redisMock) Send(string, ...interface{}) error {
	return nil
}

func (r *redisMock) Receive() (interface{}, error) {
	return nil, nil
}

func (r *redisMock) Flush() error {
	return nil
}

func (r *redisMock) Err() error {
	return nil
}

func (r *redisMock) Clear() {
	r.c.Clear()
}

func (r *redisMock) ExpectationsWereMet() error {
	return nil
}

func (r *redisMock) Do(name string, args ...interface{}) (interface{}, error) {
	return r.c.Do(name, args...)
}

func (r *redisMock) Command(name string, args ...interface{}) *redigomock.Cmd {
	return r.c.Command(name, args...)
}

func subject() (RedisRegistry, *redisMock) {
	out := bytes.NewBuffer([]byte{})
	logger := log.New(out, "", 0)
	logger.SetFlags(0)

	mock := &redisMock{
		c: redigomock.NewConn(),
	}

	pool := redis.Pool{
		Dial: func() (redis.Conn, error) {
			return mock, nil
		},
	}

	return RedisRegistry{
		Logger:         &logging.Logger{Logger: logger},
		Pool:           &pool,
		TokenGenerator: &generator,
	}, mock
}

func Test_RedisRegistry(t *testing.T) {
	g := goblin.Goblin(t)

	tokenFields := struct {
		id         string
		name       string
		device     string
		permission string
	}{
		defs.RedisDeviceTokenIDField,
		defs.RedisDeviceTokenNameField,
		defs.RedisDeviceTokenDeviceIDField,
		defs.RedisDeviceTokenPermissionField,
	}

	deviceFields := struct {
		id     string
		name   string
		secret string
	}{defs.RedisDeviceIDField, defs.RedisDeviceNameField, defs.RedisDeviceSecretField}

	g.Describe("ListRegistrations", func() {
		r, mock := subject()
		g.BeforeEach(mock.Clear)

		fields := struct {
			id     string
			name   string
			secret string
		}{defs.RedisDeviceIDField, defs.RedisDeviceNameField, defs.RedisDeviceSecretField}

		device := struct {
			name   string
			id     string
			secret string
		}{"device-name", "device-id", "device-secret"}

		g.AfterEach(func() {
			g.Assert(mock.ExpectationsWereMet()).Equal(nil)
		})

		g.It("returns an error if unable to perform the initial lrange", func() {
			mock.Command("LRANGE", defs.RedisDeviceIndexKey, 0, -1).ExpectError(fmt.Errorf("bad-range"))
			_, e := r.ListRegistrations()
			g.Assert(e.Error()).Equal("bad-range")
		})

		g.It("returns an error if unable to parse range as strings", func() {
			mock.Command("LRANGE", defs.RedisDeviceIndexKey, 0, -1).Expect(nil)
			_, e := r.ListRegistrations()
			g.Assert(e.Error()).Equal(defs.ErrBadRedisResponse)
		})

		g.Describe("having returned a registration key", func() {
			registration := []byte("some-registration")
			registryKey := r.genRegistryKey(string(registration))

			g.BeforeEach(func() {
				mock.Command("LRANGE", defs.RedisDeviceIndexKey, 0, -1).ExpectSlice(registration)
			})

			g.It("returns an error if unable to perform lookup on returned registrations", func() {
				mock.Command("HMGET", registryKey, fields.id, fields.name, fields.secret).ExpectError(fmt.Errorf("bad-get"))
				_, e := r.ListRegistrations()
				g.Assert(e.Error()).Equal("bad-get")
			})

			g.It("returns the details of the registration if successful", func() {
				mock.Command("HMGET", registryKey, fields.id, fields.name, fields.secret).ExpectSlice(
					[]byte(device.id),
					[]byte(device.name),
					[]byte(device.secret),
				)
				l, e := r.ListRegistrations()
				g.Assert(e).Equal(nil)
				g.Assert(len(l)).Equal(1)
				g.Assert(l[0].Name).Equal(device.name)
			})
		})
	})

	g.Describe("RemoveDevice", func() {
		r, mock := subject()
		g.BeforeEach(mock.Clear)

		device := struct {
			id    string
			token string
		}{"eeeeeeeeeeeeeeeeeeee", "some-token"}

		g.AfterEach(func() {
			g.Assert(mock.ExpectationsWereMet()).Equal(nil)
		})

		g.It("errors when unable to delete the main registry key", func() {
			mock.Command("DEL", r.genRegistryKey(device.id)).ExpectError(fmt.Errorf("invalid-delete"))
			e := r.RemoveDevice(device.id)
			g.Assert(e.Error()).Equal("invalid-delete")
		})

		g.It("errors when unable to delete the feedback key", func() {
			mock.Command("DEL", r.genRegistryKey(device.id)).Expect(nil)
			mock.Command("DEL", r.genFeedbackKey(device.id)).ExpectError(fmt.Errorf("invalid-delete"))
			e := r.RemoveDevice(device.id)
			g.Assert(e.Error()).Equal("invalid-delete")
		})

		g.It("errors when unable to remove the device from the index", func() {
			mock.Command("DEL", r.genRegistryKey(device.id)).Expect(nil)
			mock.Command("DEL", r.genFeedbackKey(device.id)).Expect(nil)
			mock.Command("LREM", defs.RedisDeviceIndexKey, 1, device.id).ExpectError(fmt.Errorf("invalid-lrem"))
			e := r.RemoveDevice(device.id)
			g.Assert(e.Error()).Equal("invalid-lrem")
		})

		g.It("errors when unable to get a list of tokens", func() {
			mock.Command("DEL", r.genRegistryKey(device.id)).Expect(nil)
			mock.Command("DEL", r.genFeedbackKey(device.id)).Expect(nil)
			mock.Command("LREM", defs.RedisDeviceIndexKey, 1, device.id).Expect(nil)
			mock.Command("LRANGE", r.genTokenListKey(device.id), 0, -1).ExpectError(fmt.Errorf("invalid-list"))
			e := r.RemoveDevice(device.id)
			g.Assert(e.Error()).Equal("invalid-list")
		})

		g.It("errors when unable to delete the token list", func() {
			mock.Command("DEL", r.genRegistryKey(device.id)).Expect(nil)
			mock.Command("DEL", r.genFeedbackKey(device.id)).Expect(nil)
			mock.Command("LREM", defs.RedisDeviceIndexKey, 1, device.id).Expect(nil)
			mock.Command("LRANGE", r.genTokenListKey(device.id), 0, -1).ExpectSlice(
				[]byte(device.token),
			)
			mock.Command("DEL", r.genTokenRegistrationKey(device.token)).ExpectError(fmt.Errorf("invalid-del"))
			mock.Command("DEL", r.genTokenListKey(device.id)).ExpectError(fmt.Errorf("invalid-del"))
			e := r.RemoveDevice(device.id)
			g.Assert(e.Error()).Equal("invalid-del")
		})

		g.It("does not error when unable to delete a single token", func() {
			mock.Command("DEL", r.genRegistryKey(device.id)).Expect(nil)
			mock.Command("DEL", r.genFeedbackKey(device.id)).Expect(nil)
			mock.Command("LREM", defs.RedisDeviceIndexKey, 1, device.id).Expect(nil)
			mock.Command("LRANGE", r.genTokenListKey(device.id), 0, -1).ExpectSlice(
				[]byte(device.token),
			)
			mock.Command("DEL", r.genTokenRegistrationKey(device.token)).ExpectError(fmt.Errorf("invalid-del"))
			mock.Command("DEL", r.genTokenListKey(device.id)).Expect(nil)
			e := r.RemoveDevice(device.id)
			g.Assert(e).Equal(nil)
		})
	})

	g.Describe("FindDevice", func() {
		r, mock := subject()
		device := RegistrationDetails{
			Name:         "some-device",
			DeviceID:     "1235",
			SharedSecret: "shared-secret",
		}
		registryKey := r.genRegistryKey(device.DeviceID)

		g.BeforeEach(mock.Clear)

		g.It("returns an error with no devies in the store", func() {
			_, e := r.FindDevice("garbage")
			g.Assert(e != nil).Equal(true)
		})

		g.Describe("when successfully able to check via exists", func() {
			g.BeforeEach(func() {
				mock.Command("EXISTS", registryKey).Expect([]byte("true"))
			})

			g.It("still returns an error if unable to load data", func() {
				_, e := r.FindDevice("garbage")
				g.Assert(e != nil).Equal(true)
			})

			g.Describe("when able to load all details via HMGET", func() {
				g.BeforeEach(func() {
					mock.Command("HMGET", registryKey, "device:uuid", "device:name", "device:secret").ExpectSlice(
						[]byte(device.DeviceID),
						[]byte(device.Name),
						[]byte(device.SharedSecret),
					)
				})

				g.It("successfully returns the device details", func() {
					result, e := r.FindDevice(device.DeviceID)

					g.Assert(e == nil).Equal(true)
					g.Assert(result.DeviceID).Equal(device.DeviceID)
				})
			})
		})

		g.Describe("when unable to find by fast id lookup", func() {
			g.BeforeEach(func() {
				mock.Command("EXISTS", r.genRegistryKey(device.Name)).Expect([]byte("false"))
			})

			g.It("returns an error when recevied an error during the loading from KEYS", func() {
				mock.Command("KEYS", fmt.Sprintf("%s*", defs.RedisDeviceRegistryKey)).ExpectError(fmt.Errorf("problems"))

				_, e := r.FindDevice(device.Name)

				g.Assert(e != nil).Equal(true)
			})

			g.It("returns an error when recevied an error during the parsing of strings from KEYS", func() {
				mock.Command("KEYS", fmt.Sprintf("%s*", defs.RedisDeviceRegistryKey)).Expect(nil)
				_, e := r.FindDevice(device.Name)
				g.Assert(e != nil).Equal(true)
			})

			g.Describe("having received a valid list of device registrations", func() {
				g.BeforeEach(func() {
					list := []byte(r.genRegistryKey(device.DeviceID))
					mock.Command("KEYS", fmt.Sprintf("%s*", defs.RedisDeviceRegistryKey)).ExpectSlice(list)
				})

				g.It("returns an error when recevied an error during the loading of keys via second HMGET", func() {
					mock.Command(
						"HMGET",
						r.genRegistryKey(device.DeviceID), "device:name", "device:uuid", "device:secret",
					).ExpectError(fmt.Errorf("problem"))

					_, e := r.FindDevice(device.Name)

					g.Assert(e != nil).Equal(true)
				})

				g.It("errors when recevied a mismatch during the loading from HMGET", func() {
					k := r.genRegistryKey(device.DeviceID)
					cmd := mock.Command("HMGET", k, "device:name", "device:uuid", "device:secret")
					cmd.ExpectSlice(
						[]byte("not-the-same"),
						[]byte("not-the-same"),
						[]byte("not-the-same"),
					)

					_, e := r.FindDevice(device.Name)
					g.Assert(e != nil).Equal(true)
				})

				g.It("succeeds with valid device details & searching by name", func() {
					k := r.genRegistryKey(device.DeviceID)
					cmd := mock.Command("HMGET", k, "device:name", "device:uuid", "device:secret")
					cmd.ExpectSlice(
						[]byte(device.Name),
						[]byte(device.DeviceID),
						[]byte(device.SharedSecret),
					)

					result, e := r.FindDevice(device.Name)

					g.Assert(e).Equal(nil)
					g.Assert(result.Name).Equal(device.Name)
					g.Assert(result.DeviceID).Equal(device.DeviceID)
				})
			})
		})
	})

	g.Describe("AllocateRegistration", func() {
		r, mock := subject()

		g.BeforeEach(mock.Clear)

		g.Describe("with invalid registration details", func() {
			registrations := []RegistrationRequest{
				RegistrationRequest{},
				RegistrationRequest{Name: "this is a valid name"},
				RegistrationRequest{SharedSecret: "iiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiii"},
			}

			for _, request := range registrations {
				g.It("errors with an invalid registration request", func() {
					e := r.AllocateRegistration(request)
					g.Assert(e.Error()).Equal(defs.ErrInvalidRegistrationRequest)
				})
			}
		})

		g.Describe("with a valid registration", func() {
			request := RegistrationRequest{
				Name:         "a valid device name",
				SharedSecret: "iiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiii",
			}

			g.It("errors when unable to set via hset", func() {
				mock.Command("HMSET").ExpectError(fmt.Errorf("some-error"))
				e := r.AllocateRegistration(request)
				g.Assert(e.Error()).Equal("some-error")
			})

			g.It("returns nil when successfully able to set via hset", func() {
				mock.Command("HMSET").Expect(nil)
				e := r.AllocateRegistration(request)
				g.Assert(e).Equal(nil)
			})
		})
	})

	g.Describe("FillRegistration", func() {
		r, mock := subject()
		g.BeforeEach(mock.Clear)

		fields := struct {
			secret string
			name   string
		}{defs.RedisRegistrationSecretField, defs.RedisRegistrationNameField}

		registration := struct {
			id     string
			name   string
			secret string
		}{"1212121212", "some request", "31313131313131313131"}

		g.It("returns error when initial keys lookup fails", func() {
			mock.Command("KEYS").ExpectError(fmt.Errorf("bad-keys"))
			e := r.FillRegistration("secret", "uuid")
			g.Assert(e.Error()).Equal("bad-keys")
		})

		g.It("returns error when initial keys lookup returns garbage", func() {
			mock.Command("KEYS").Expect(nil)
			e := r.FillRegistration("secret", "uuid")
			g.Assert(e.Error()).Equal(defs.ErrBadRedisResponse)
		})

		g.It("returns error when initial keys lookup returns empty array", func() {
			mock.Command("KEYS").ExpectSlice([]byte("one"))
			e := r.FillRegistration("secret", "uuid")
			g.Assert(e.Error()).Equal(defs.ErrNotFound)
		})

		g.It("returns error when received some keys but fails on string conv", func() {
			mock.Command("KEYS").ExpectSlice([]byte("hello"))
			mock.Command("HGET").Expect(nil)
			e := r.FillRegistration("secret", "uuid")
			g.Assert(e.Error()).Equal(defs.ErrNotFound)
		})

		g.Describe("when having received a valid lookup w/ a matching secret", func() {
			registrationKey := r.genAllocationKey(registration.id)

			g.BeforeEach(func() {
				mock.Command("KEYS").ExpectSlice([]byte(registrationKey))
				mock.Command("HGET", registrationKey, fields.secret).Expect([]byte(registration.secret))
			})

			g.It("returns error when unable to finalize the registration", func() {
				mock.Command("HMGET", registrationKey, fields.secret, fields.name).ExpectError(fmt.Errorf("some-error"))
				e := r.FillRegistration(registration.secret, registration.id)
				g.Assert(e.Error()).Equal("some-error")
			})

			g.It("returns error when unable to push into the index", func() {
				mock.Command("HMGET", registrationKey, fields.secret, fields.name).ExpectSlice(
					[]byte(registration.secret),
					[]byte(registration.name),
				)
				mock.Command("LPUSH", defs.RedisDeviceIndexKey, registration.id).ExpectError(fmt.Errorf("some-error"))
				e := r.FillRegistration(registration.secret, registration.id)
				g.Assert(e.Error()).Equal("some-error")
			})

			g.Describe("having succesfully loaded + pushed to the index", func() {
				g.BeforeEach(func() {
					mock.Command("HMGET", registrationKey, fields.secret, fields.name).ExpectSlice(
						[]byte(registration.secret),
						[]byte(registration.name),
					)
					mock.Command("LPUSH", defs.RedisDeviceIndexKey, registration.id).Expect(nil)
				})

				g.It("errors when failed on hmset", func() {
					mock.Command("HMSET").ExpectError(fmt.Errorf("bad-hmset"))
					e := r.FillRegistration(registration.secret, registration.id)
					g.Assert(e.Error()).Equal("bad-hmset")
				})

				g.It("succeeds after successful hmset", func() {
					mock.Command("HMSET").Expect(nil)
					e := r.FillRegistration(registration.secret, registration.id)
					g.Assert(e).Equal(nil)
				})
			})
		})
	})

	g.Describe("ListTokens", func() {
		r, mock := subject()
		g.BeforeEach(mock.Clear)

		fixtures := struct {
			deviceID            string
			deviceName          string
			deviceSecret        string
			testTokenValue      string
			testTokenName       string
			testTokenID         string
			testTokenPermission string
		}{
			deviceID:            "list-tokens-test-device-id",
			deviceName:          "some-device-name",
			deviceSecret:        "go-bills",
			testTokenValue:      "a-token",
			testTokenName:       "token-id",
			testTokenID:         "111",
			testTokenPermission: "111",
		}

		g.AfterEach(func() {
			g.Assert(mock.ExpectationsWereMet()).Equal(nil)
		})

		g.It("fails if it is unable to find the device requested", func() {
			registryKey := r.genRegistryKey(fixtures.deviceID)
			mock.Command("EXISTS", registryKey).ExpectError(fmt.Errorf("bad-exists"))
			_, e := r.ListTokens(fixtures.deviceID)
			g.Assert(e.Error()).Equal("bad-exists")
		})

		g.Describe("having found a device", func() {
			g.BeforeEach(func() {
				registryKey := r.genRegistryKey(fixtures.deviceID)
				mock.Command("EXISTS", registryKey).Expect([]byte("true"))
				mock.Command("HMGET", registryKey, deviceFields.id, deviceFields.name, deviceFields.secret).ExpectSlice(
					[]byte(fixtures.deviceID),
					[]byte(fixtures.deviceName),
					[]byte(fixtures.deviceSecret),
				)
			})

			g.It("errors if unable to range over the tokens", func() {
				tokensListKey := r.genTokenListKey(fixtures.deviceID)
				mock.Command("LRANGE", tokensListKey, 0, -1).ExpectError(fmt.Errorf("bad-range"))
				_, e := r.ListTokens(fixtures.deviceID)
				g.Assert(e.Error()).Equal("bad-range")
			})

			g.It("returns an empty range if no elements were returned", func() {
				tokensListKey := r.genTokenListKey(fixtures.deviceID)
				mock.Command("LRANGE", tokensListKey, 0, -1).ExpectSlice()
				tokens, e := r.ListTokens(fixtures.deviceID)
				g.Assert(e).Equal(nil)
				g.Assert(len(tokens)).Equal(0)
			})

			g.Describe("having returned some raw tokens from the range", func() {
				g.BeforeEach(func() {
					tokensListKey := r.genTokenListKey(fixtures.deviceID)
					mock.Command("LRANGE", tokensListKey, 0, -1).ExpectSlice(
						[]byte(fixtures.testTokenValue),
					)
				})

				g.It("returns no tokens even if errored during lookup", func() {
					tokenDetailKey := r.genTokenRegistrationKey(fixtures.testTokenValue)
					mock.Command(
						"HMGET",
						tokenDetailKey,
						tokenFields.id,
						tokenFields.name,
						tokenFields.device,
						tokenFields.permission,
					).ExpectError(fmt.Errorf("bad-get"))
					tokens, e := r.ListTokens(fixtures.deviceID)
					g.Assert(e).Equal(nil)
					g.Assert(len(tokens)).Equal(0)
				})

				g.It("skips tokens with invalid permission masks", func() {
					tokenDetailKey := r.genTokenRegistrationKey(fixtures.testTokenValue)
					mock.Command(
						"HMGET",
						tokenDetailKey,
						tokenFields.id,
						tokenFields.name,
						tokenFields.device,
						tokenFields.permission,
					).ExpectSlice(
						[]byte(fixtures.testTokenID),
						[]byte(fixtures.testTokenName),
						[]byte(fixtures.deviceID),
						[]byte("asdasdas"),
					)

					tokens, e := r.ListTokens(fixtures.deviceID)
					g.Assert(e).Equal(nil)
					g.Assert(len(tokens)).Equal(0)
				})

				g.It("returns the token details identified by the value returned from the range", func() {
					tokenDetailKey := r.genTokenRegistrationKey(fixtures.testTokenValue)
					mock.Command(
						"HMGET",
						tokenDetailKey,
						tokenFields.id,
						tokenFields.name,
						tokenFields.device,
						tokenFields.permission,
					).ExpectSlice(
						[]byte(fixtures.testTokenID),
						[]byte(fixtures.testTokenName),
						[]byte(fixtures.deviceID),
						[]byte(fixtures.testTokenPermission),
					)

					tokens, e := r.ListTokens(fixtures.deviceID)
					g.Assert(e).Equal(nil)
					g.Assert(len(tokens)).Equal(1)
				})
			})

		})
	})

	g.Describe("FindToken", func() {
		r, mock := subject()
		g.BeforeEach(mock.Clear)

		token := struct {
			name     string
			token    string
			id       string
			deviceID string
		}{"testing", "eeeeeeeeeeeeeeeeeeee", "token-id-1", "device-id-1"}

		tokenKey := r.genTokenRegistrationKey(token.token)

		fields := struct {
			permission string
		}{defs.RedisDeviceTokenPermissionField}

		g.It("fails fast when unable to get the permission mask", func() {
			mock.Command("HGET", tokenKey, fields.permission).ExpectError(fmt.Errorf("bad-hget"))
			_, e := r.FindToken(token.token)
			g.Assert(e.Error()).Equal("bad-hget")
		})

		g.It("fails fast when unable to parse the permission mask", func() {
			mock.Command("HGET", tokenKey, fields.permission).Expect([]byte("invalid-mask"))
			_, e := r.FindToken(token.token)
			g.Assert(strings.Contains(e.Error(), "invalid syntax")).Equal(true)
		})

		g.Describe("when successfully able to load token", func() {
			g.BeforeEach(func() {
				mock.Command("HGET", tokenKey, fields.permission).Expect([]byte("111"))
			})

			g.It("returns error when hmget lookup fails", func() {
				mock.Command("HMGET").ExpectError(fmt.Errorf("bad-hmget"))
				_, e := r.FindToken(token.token)
				g.Assert(e.Error()).Equal("bad-hmget")
			})

			g.It("successfully returns token details when hmget lookup passes", func() {
				mock.Command("HMGET").ExpectSlice(
					[]byte(token.id),
					[]byte(token.name),
					[]byte(token.deviceID),
				)
				_, e := r.FindToken(token.token)
				g.Assert(e).Equal(nil)
			})
		})
	})

	g.Describe("AuthorizeToken", func() {
		r, mock := subject()

		fields := struct {
			permission string
			deviceID   string
			id         string
			name       string
		}{
			defs.RedisDeviceTokenPermissionField,
			defs.RedisDeviceTokenDeviceIDField,
			defs.RedisDeviceTokenIDField,
			defs.RedisDeviceTokenNameField,
		}

		device := struct {
			name   string
			id     string
			token  string
			secret string
		}{"test-device", "id-123", "4242424242", "421421421421421421421421"}

		registryKey := r.genRegistryKey(device.id)

		g.BeforeEach(mock.Clear)

		g.AfterEach(func() {
			g.Assert(mock.ExpectationsWereMet()).Equal(nil)
		})

		g.It("returns false if unable to find device", func() {
			mock.Command("EXISTS", registryKey).ExpectError(fmt.Errorf("bad-exists"))
			b := r.AuthorizeToken(device.id, device.token, 1)
			g.Assert(b).Equal(false)
		})

		g.Describe("having found a device via EXISTS", func() {
			g.BeforeEach(func() {
				mock.Command("EXISTS", registryKey).Expect([]byte("true"))
			})

			g.It("should return true if token matches device secret", func() {
				mock.Command("HMGET", registryKey, "device:uuid", "device:name", "device:secret").ExpectSlice(
					[]byte(device.id),
					[]byte(device.name),
					[]byte(device.secret),
				)
				b := r.AuthorizeToken(device.id, device.secret, 1)
				g.Assert(b).Equal(true)
			})

			g.It("should not return true if unable to load in token details", func() {
				mock.Command("HMGET", registryKey, "device:uuid", "device:name", "device:secret").ExpectSlice(
					[]byte(device.id),
					[]byte(device.name),
					[]byte(device.secret),
				)
				mock.Command("HGET", r.genTokenRegistrationKey(device.token), fields.permission).ExpectError(fmt.Errorf(""))
				b := r.AuthorizeToken(device.id, device.token, 1)
				g.Assert(b).Equal(false)
			})

			g.Describe("with valid device + token information loaded", func() {
				tokenKey := r.genTokenRegistrationKey(device.token)

				g.BeforeEach(func() {
					mock.Command("HMGET", registryKey, "device:uuid", "device:name", "device:secret").ExpectSlice(
						[]byte(device.id),
						[]byte(device.name),
						[]byte(device.secret),
					)
					mock.Command("HMGET", tokenKey, fields.id, fields.name, fields.deviceID).ExpectSlice(
						[]byte(device.id),
						[]byte(device.name),
						[]byte(device.id),
					)
				})

				invalid := [][]string{
					[]string{"100", "011"},
					[]string{"100", "001"},
					[]string{"100", "010"},
					[]string{"100", "111"},
					[]string{"010", "101"},
					[]string{"010", "100"},
					[]string{"010", "001"},
					[]string{"010", "111"},
					[]string{"001", "110"},
					[]string{"001", "010"},
					[]string{"001", "100"},
					[]string{"001", "111"},
				}

				valid := [][]string{
					[]string{"1100", "100"},
					[]string{"1010", "010"},
					[]string{"1001", "001"},
				}

				for _, masks := range invalid {
					have, want := masks[0], masks[1]
					g.It(fmt.Sprintf("should not return true if the token mask is invalid (%s vs %s)", have, want), func() {
						mock.Command("HGET", tokenKey, fields.permission).Expect([]byte(have))
						b := r.AuthorizeToken(device.id, device.token, mask(want))
						g.Assert(b).Equal(false)
					})
				}

				for _, masks := range valid {
					have, want := masks[0], masks[1]
					g.It(fmt.Sprintf("should return true if the token mask is valid (%s vs %s)", have, want), func() {
						mock.Command("HGET", tokenKey, fields.permission).Expect([]byte(have))
						b := r.AuthorizeToken(device.id, device.token, mask(want))
						g.Assert(b).Equal(true)
					})
				}
			})
		})
	})

	g.Describe("CreateToken", func() {
		r, mock := subject()

		g.BeforeEach(mock.Clear)

		testFixtures := struct {
			deviceID        string
			deviceName      string
			deviceSecret    string
			tokenName       string
			tokenSecret     string
			tokenPermission uint
		}{"device-id", "a test device", "the device secret", "a test token", "token-secret", 7}

		g.BeforeEach(func() {
			generator.t = testFixtures.tokenSecret
		})

		g.AfterEach(func() {
			g.Assert(mock.ExpectationsWereMet()).Equal(nil)
		})

		g.It("errors when unable to push into token list", func() {
			mock.Command("EXISTS", r.genRegistryKey(testFixtures.deviceID)).ExpectError(fmt.Errorf("bad-exists"))
			_, e := r.CreateToken(testFixtures.deviceID, testFixtures.tokenName, testFixtures.tokenPermission)
			g.Assert(e.Error()).Equal("bad-exists")
		})

		g.Describe("having found the device", func() {
			g.BeforeEach(func() {
				key := r.genRegistryKey(testFixtures.deviceID)
				mock.Command("EXISTS", key).Expect([]byte("true"))
				mock.Command("HMGET", key, deviceFields.id, deviceFields.name, deviceFields.secret).ExpectSlice(
					[]byte(testFixtures.deviceID),
					[]byte(testFixtures.deviceName),
					[]byte(testFixtures.deviceSecret),
				)
			})

			g.It("returns an error if unable to push into the token list", func() {
				key := r.genTokenListKey(testFixtures.deviceID)
				mock.Command("LPUSH", key, testFixtures.tokenSecret).ExpectError(fmt.Errorf("bad-push"))
				_, e := r.CreateToken(testFixtures.deviceID, testFixtures.tokenName, testFixtures.tokenPermission)
				g.Assert(e.Error()).Equal("bad-push")
			})

			g.It("returns an error if unable to set token fields in the token registry", func() {
				listKey := r.genTokenListKey(testFixtures.deviceID)
				tokenRegistryKey := r.genTokenRegistrationKey(generator.t)
				mock.Command("LPUSH", listKey, testFixtures.tokenSecret).Expect(nil)
				mock.Command(
					"HMSET",
					tokenRegistryKey,
					tokenFields.name,
					testFixtures.tokenName,
					tokenFields.permission,
					redigomock.NewAnyData(),
					tokenFields.id,
					redigomock.NewAnyData(),
					tokenFields.device,
					testFixtures.deviceID,
				).ExpectError(fmt.Errorf("bad-set"))
				_, e := r.CreateToken(testFixtures.deviceID, testFixtures.tokenName, testFixtures.tokenPermission)
				g.Assert(e.Error()).Equal("bad-set")
			})

			g.It("returns successfully if able to set token fields in the token registry", func() {
				listKey := r.genTokenListKey(testFixtures.deviceID)
				tokenRegistryKey := r.genTokenRegistrationKey(generator.t)
				mock.Command("LPUSH", listKey, testFixtures.tokenSecret).Expect(nil)
				mock.Command(
					"HMSET",
					tokenRegistryKey,
					tokenFields.name,
					testFixtures.tokenName,
					tokenFields.permission,
					redigomock.NewAnyData(),
					tokenFields.id,
					redigomock.NewAnyData(),
					tokenFields.device,
					testFixtures.deviceID,
				).Expect(nil)
				_, e := r.CreateToken(testFixtures.deviceID, testFixtures.tokenName, testFixtures.tokenPermission)
				g.Assert(e).Equal(nil)
			})

		})
	})

	g.Describe("LogFeedback", func() {
		r, mock := subject()

		g.BeforeEach(mock.Clear)

		g.AfterEach(func() {
			g.Assert(mock.ExpectationsWereMet()).Equal(nil)
		})

		testFixtures := struct {
			deviceID string
		}{"12345"}

		g.It("errors if the message does not have any authentication information", func() {
			e := r.LogFeedback(interchange.FeedbackMessage{})
			g.Assert(e.Error()).Equal(defs.ErrBadInterchangeAuthentication)
		})

		g.Describe("with a valid feedbackMessage", func() {
			feedbackMessage := interchange.FeedbackMessage{
				Authentication: &interchange.DeviceMessageAuthentication{
					DeviceID: testFixtures.deviceID,
				},
			}

			g.It("errors if the message has a bad device id", func() {
				mock.Command("EXISTS", r.genRegistryKey(testFixtures.deviceID)).ExpectError(fmt.Errorf("bad-exists"))
				e := r.LogFeedback(feedbackMessage)
				g.Assert(e.Error()).Equal("bad-exists")
			})

			g.Describe("with a valid device", func() {
				g.BeforeEach(func() {
					key := r.genRegistryKey(testFixtures.deviceID)
					mock.Command("EXISTS", key).Expect([]byte("true"))
					mock.Command("HMGET", key, deviceFields.id, deviceFields.name, deviceFields.secret).ExpectSlice(
						[]byte(testFixtures.deviceID),
						[]byte("buffalo-bills"),
						[]byte("red-sox"),
					)
				})

				g.It("errors if the it is unable to get the length of messages currently in the list", func() {
					key := r.genFeedbackKey(testFixtures.deviceID)
					mock.Command("LLEN", key).ExpectError(fmt.Errorf("bad-llen"))
					e := r.LogFeedback(feedbackMessage)
					g.Assert(e.Error()).Equal("bad-llen")
				})

				g.It("errors if the it is unable to push into the registry", func() {
					key := r.genFeedbackKey(testFixtures.deviceID)
					mock.Command("LLEN", key).Expect([]byte("0"))
					mock.Command("LPUSH", key, redigomock.NewAnyData()).ExpectError(fmt.Errorf("bad-push"))
					e := r.LogFeedback(feedbackMessage)
					g.Assert(e.Error()).Equal("bad-push")
				})

				g.Describe("having more entries than the maximum amount", func() {
					g.BeforeEach(func() {
						key := r.genFeedbackKey(testFixtures.deviceID)
						mock.Command("LLEN", key).Expect([]byte(fmt.Sprintf("%d", defs.RedisMaxFeedbackEntries+1)))
					})

					g.It("attempts to trim the list down to size", func() {
						key := r.genFeedbackKey(testFixtures.deviceID)
						mock.Command("LTRIM", key, 0, defs.RedisMaxFeedbackEntries-2).ExpectError(fmt.Errorf("bad-trim"))
						e := r.LogFeedback(feedbackMessage)
						g.Assert(e.Error()).Equal("bad-trim")
					})
				})

				g.It("succeeds if the it is able to push into the registry", func() {
					key := r.genFeedbackKey(testFixtures.deviceID)
					mock.Command("LLEN", key).Expect([]byte("0"))
					mock.Command("LPUSH", key, redigomock.NewAnyData()).Expect(nil)
					e := r.LogFeedback(feedbackMessage)
					g.Assert(e).Equal(nil)
				})
			})
		})
	})

	g.Describe("ListFeedback", func() {
		r, mock := subject()

		device := struct {
			id     string
			name   string
			secret string
		}{"123123123123123123123123123123", "test-device", "some-secret"}

		g.BeforeEach(mock.Clear)

		g.AfterEach(func() {
			g.Assert(mock.ExpectationsWereMet()).Equal(nil)
		})

		g.It("errors if unable to find the device based on string provided", func() {
			mock.Command("EXISTS", r.genRegistryKey(device.id)).ExpectError(fmt.Errorf("bad-exists"))
			_, e := r.ListFeedback(device.id, 3)
			g.Assert(e.Error()).Equal("bad-exists")
		})

		g.Describe("having found the device", func() {
			g.BeforeEach(func() {
				key := r.genRegistryKey(device.id)
				mock.Command("EXISTS", key).Expect([]byte("true"))

				mock.Command("HMGET", key, deviceFields.id, deviceFields.name, deviceFields.secret).ExpectSlice(
					[]byte(device.id),
					[]byte(device.name),
					[]byte(device.secret),
				)
			})

			g.It("fails when error on LRANGE into feedback key", func() {
				key := r.genFeedbackKey(device.id)
				mock.Command("LRANGE", key, 0, 3).ExpectError(fmt.Errorf("bad-range"))
				_, e := r.ListFeedback(device.id, 3)
				g.Assert(e.Error()).Equal("bad-range")
			})

			g.It("fails when bad return on LRANGE command", func() {
				key := r.genFeedbackKey(device.id)
				mock.Command("LRANGE", key, 0, 3).Expect(nil)
				_, e := r.ListFeedback(device.id, 3)
				g.Assert(e.Error()).Equal(defs.ErrBadRedisResponse)
			})

			g.It("returns nil when LRANGE is empty", func() {
				key := r.genFeedbackKey(device.id)
				mock.Command("LRANGE", key, 0, 3).ExpectSlice()
				_, e := r.ListFeedback(device.id, 3)
				g.Assert(e).Equal(nil)
			})

			g.It("returns error when LRANGE returns unmarshallable responses", func() {
				key := r.genFeedbackKey(device.id)
				mock.Command("LRANGE", key, 0, 3).ExpectSlice(
					[]byte("invalid-interchange-format"),
				)
				_, e := r.ListFeedback(device.id, 3)
				g.Assert(e.Error()).Equal(defs.ErrBadInterchangeData)
			})

			g.It("returns proper list when lrange returns valid responses", func() {
				key := r.genFeedbackKey(device.id)
				mock.Command("LRANGE", key, 0, 3).ExpectSlice(
					genFeedback(),
					genFeedback(),
					genFeedback(),
				)
				results, e := r.ListFeedback(device.id, 3)
				g.Assert(e).Equal(nil)
				g.Assert(len(results)).Equal(3)
			})
		})
	})
}
