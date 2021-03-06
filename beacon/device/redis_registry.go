package device

import "fmt"
import "bytes"
import "strconv"
import "github.com/satori/go.uuid"
import "github.com/garyburd/redigo/redis"
import "github.com/golang/protobuf/proto"

import "github.com/dadleyy/beacon.api/beacon/defs"
import "github.com/dadleyy/beacon.api/beacon/logging"
import "github.com/dadleyy/beacon.api/beacon/interchange"

// RedisRegistry implements the `Registry` interface w/ a redis backend
type RedisRegistry struct {
	*logging.Logger
	*redis.Pool
	TokenGenerator
}

// FindDevice searches the registry based on a query string for the first matching device id
func (registry *RedisRegistry) FindDevice(query string) (RegistrationDetails, error) {
	registryKey := registry.genRegistryKey(query)

	exists, e := registry.exists(registryKey)

	if e != nil {
		return RegistrationDetails{}, e
	}

	if exists {
		return registry.loadDetails(registryKey)
	}

	response, e := registry.Do("KEYS", fmt.Sprintf("%s*", defs.RedisDeviceRegistryKey))

	if e != nil {
		return RegistrationDetails{}, e
	}

	registryKeys, e := redis.Strings(response, e)

	if e != nil {
		return RegistrationDetails{}, e
	}

	for _, k := range registryKeys {
		fields, e := registry.hmgetstr(k, defs.RedisDeviceNameField, defs.RedisDeviceIDField, defs.RedisDeviceSecretField)

		if e != nil {
			return RegistrationDetails{}, e
		}

		if fields[0] == query || fields[1] == query {
			d := RegistrationDetails{SharedSecret: fields[2], DeviceID: fields[1], Name: fields[0]}
			return d, nil
		}
	}

	registry.Warnf("did not find matching device: %s", query)
	return RegistrationDetails{}, fmt.Errorf(defs.ErrNotFound)
}

// ListFeedback retrieves the latest feedback for a given device id.
func (registry *RedisRegistry) ListFeedback(id string, count int) ([]interchange.FeedbackMessage, error) {
	details, e := registry.FindDevice(id)

	if e != nil {
		return nil, e
	}

	feedbackKey := registry.genFeedbackKey(details.DeviceID)

	list, e := registry.lrangestr(feedbackKey, 0, count)

	if e != nil {
		return nil, e
	}

	if filled := len(list) >= 1; filled == false {
		return nil, nil
	}

	results := make([]interchange.FeedbackMessage, 0, len(list))

	for _, entry := range list {
		message := interchange.FeedbackMessage{}

		if e := proto.UnmarshalText(entry, &message); e != nil {
			registry.Warnf("invalid feedback item device[%d]: %s", feedbackKey, e.Error())
			return nil, fmt.Errorf(defs.ErrBadInterchangeData)
		}

		results = append(results, message)
	}

	registry.Debugf("found %d entries for device key: %s (returning %d)", len(list), feedbackKey, len(results))
	return results, nil
}

// LogFeedback inserts a feedback item into the redis store.
func (registry *RedisRegistry) LogFeedback(message interchange.FeedbackMessage) error {
	auth := message.GetAuthentication()

	if auth == nil {
		return fmt.Errorf(defs.ErrBadInterchangeAuthentication)
	}

	details, e := registry.FindDevice(auth.DeviceID)

	if e != nil {
		return e
	}

	feedbackKey, textBuffer := registry.genFeedbackKey(details.DeviceID), bytes.NewBuffer([]byte{})

	count, e := registry.llen(feedbackKey)

	if e != nil {
		return e
	}

	if count >= defs.RedisMaxFeedbackEntries {
		registry.Warnf("feedback stack[%s] exceeds max[%d] entries, trimming", feedbackKey, defs.RedisMaxFeedbackEntries)

		if _, e := registry.Do("LTRIM", feedbackKey, 0, defs.RedisMaxFeedbackEntries-2); e != nil {
			registry.Errorf("unable to trim device feedback stack: %s", e.Error())
			return e
		}
	}

	if e := proto.MarshalText(textBuffer, &message); e != nil {
		return e
	}

	if _, e := registry.Do("LPUSH", feedbackKey, textBuffer.String()); e != nil {
		return e
	}

	registry.Debugf("logging state for device: %s", feedbackKey)

	return nil
}

// AllocateRegistration reserves a spot in the registry to be filled later
func (registry *RedisRegistry) AllocateRegistration(details RegistrationRequest) error {
	allocationID := uuid.NewV4().String()
	registryKey := registry.genAllocationKey(allocationID)

	if len(details.Name) < 4 || len(details.SharedSecret) < defs.SecurityMinimumDeviceSharedSecretSize {
		return fmt.Errorf(defs.ErrInvalidRegistrationRequest)
	}

	nameField, secretField := defs.RedisRegistrationNameField, defs.RedisRegistrationSecretField

	return registry.hmset(registryKey, nameField, details.Name, secretField, details.SharedSecret)
}

// FillRegistration searches the pending registrations and adds the new uuid to the index
func (registry *RedisRegistry) FillRegistration(secret, uuid string) error {
	response, e := registry.Do("KEYS", fmt.Sprintf("%s*", defs.RedisRegistrationRequestListKey))

	if e != nil {
		return e
	}

	requestKeys, e := redis.Strings(response, e)

	if e != nil {
		return fmt.Errorf(defs.ErrBadRedisResponse)
	}

	for _, k := range requestKeys {
		response, e := registry.Do("HGET", k, defs.RedisRegistrationSecretField)

		if e != nil {
			continue
		}

		s, e := redis.String(response, e)

		if e != nil {
			continue
		}

		if s == secret {
			registry.Debugf("found matching secret for device[%s], filling", uuid)
			return registry.fill(k, uuid)
		}
	}

	return fmt.Errorf(defs.ErrNotFound)
}

// ListTokens searches the token store for the token details given the token key.
func (registry *RedisRegistry) ListTokens(query string) ([]TokenDetails, error) {
	deviceInfo, e := registry.FindDevice(query)

	if e != nil {
		return nil, e
	}

	listKey := registry.genTokenListKey(deviceInfo.DeviceID)

	tokenEntries, e := registry.lrangestr(listKey, 0, -1)

	if e != nil {
		return nil, e
	}

	results := make([]TokenDetails, 0, len(tokenEntries))

	fields := struct {
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

	for _, tokenValue := range tokenEntries {
		registryKey := registry.genTokenRegistrationKey(tokenValue)
		details, e := registry.hmgetstr(registryKey, fields.id, fields.name, fields.device, fields.permission)

		if e != nil {
			continue
		}

		permission, e := strconv.ParseUint(details[3], 2, 32)

		if e != nil {
			continue
		}

		results = append(results, TokenDetails{
			TokenID:    details[0],
			Name:       details[1],
			DeviceID:   details[2],
			Permission: uint(permission),
		})
	}

	return results, nil
}

// FindToken searches the token store for the token details given the token key.
func (registry *RedisRegistry) FindToken(token string) (TokenDetails, error) {
	// Start w/ an attempt to look up by key directly>
	registryKey := registry.genTokenRegistrationKey(token)

	permissionMask, e := registry.hgetstr(registryKey, defs.RedisDeviceTokenPermissionField)

	if e != nil {
		registry.Errorf("unable to find token by registry key %s (token: %s)", registryKey, token)
		return TokenDetails{}, e
	}

	permission, e := strconv.ParseUint(permissionMask, 2, 32)

	if e != nil {
		registry.Errorf("invalid token permission mask %s (token: %s)", registryKey, token)
		return TokenDetails{}, e
	}

	fields := struct {
		id     string
		name   string
		device string
	}{defs.RedisDeviceTokenIDField, defs.RedisDeviceTokenNameField, defs.RedisDeviceTokenDeviceIDField}

	r, e := registry.hmgetstr(registryKey, fields.id, fields.name, fields.device)

	if e != nil {
		registry.Errorf("unable to find token details by registry key %s (token: %s)", registryKey, token)
		return TokenDetails{}, e
	}

	details := TokenDetails{
		Permission: uint(permission),
		TokenID:    r[0],
		Name:       r[1],
		DeviceID:   r[2],
	}

	return details, nil
}

// AuthorizeToken approves the token + permission for the given device id
func (registry *RedisRegistry) AuthorizeToken(deviceID, token string, permission uint) bool {
	registration, e := registry.FindDevice(deviceID)

	if e != nil {
		return false
	}

	if token == registration.SharedSecret {
		return true
	}

	requester, e := registry.FindToken(token)

	if e != nil {
		registry.Errorf("unable to find token: %s", e.Error())
		return false
	}

	registry.Infof("auth token: %s (token: %b, requested: %b)", requester.TokenID, requester.Permission, permission)

	return requester.Permission&permission == permission
}

// CreateToken creates a new auth token for a given device id
func (registry *RedisRegistry) CreateToken(deviceID, tokenName string, permission uint) (TokenDetails, error) {
	listKey := registry.genTokenListKey(deviceID)
	empty, permissionMask, tokenID := TokenDetails{}, fmt.Sprintf("%b", permission), uuid.NewV4().String()

	if _, e := registry.FindDevice(deviceID); e != nil {
		return empty, e
	}

	rawToken, e := registry.GenerateToken()

	if e != nil {
		return empty, e
	}

	if _, e := registry.Do("LPUSH", listKey, rawToken); e != nil {
		return empty, e
	}

	registryKey := registry.genTokenRegistrationKey(rawToken)

	fields := struct {
		name       string
		permission string
		id         string
		deviceID   string
	}{
		defs.RedisDeviceTokenNameField,
		defs.RedisDeviceTokenPermissionField,
		defs.RedisDeviceTokenIDField,
		defs.RedisDeviceTokenDeviceIDField,
	}

	details := TokenDetails{
		TokenID:    tokenID,
		DeviceID:   deviceID,
		Token:      rawToken,
		Name:       tokenName,
		Permission: permission,
	}

	return details, registry.hmset(
		registryKey,
		fields.name, tokenName,
		fields.permission, permissionMask,
		fields.id, tokenID,
		fields.deviceID, deviceID,
	)
}

// ListRegistrations prints out a list of all the registered devices
func (registry *RedisRegistry) ListRegistrations() ([]RegistrationDetails, error) {
	var results []RegistrationDetails

	ids, e := registry.lrangestr(defs.RedisDeviceIndexKey, 0, -1)

	if e != nil {
		return nil, e
	}

	for _, k := range ids {
		details, e := registry.loadDetails(registry.genRegistryKey(k))

		if e != nil {
			return nil, e
		}

		results = append(results, details)
	}

	return results, nil
}

// RemoveDevice executes the LREM command to the redis connection
func (registry *RedisRegistry) RemoveDevice(id string) error {
	regKey, feedKey := registry.genRegistryKey(id), registry.genFeedbackKey(id)

	if e := registry.del(regKey); e != nil {
		return e
	}

	if e := registry.del(feedKey); e != nil {
		return e
	}

	if _, e := registry.Do("LREM", defs.RedisDeviceIndexKey, 1, id); e != nil {
		return e
	}

	tokensListKey := registry.genTokenListKey(id)

	tokens, e := registry.lrangestr(tokensListKey, 0, -1)

	if e != nil {
		return e
	}

	for _, t := range tokens {
		registry.del(registry.genTokenRegistrationKey(t))
	}

	return registry.del(tokensListKey)
}

// exists extracts the full list of device keys and searches for the target id
func (registry *RedisRegistry) exists(key string) (bool, error) {
	response, e := registry.Do("EXISTS", key)

	if e != nil {
		return false, e
	}

	return redis.Bool(response, e)
}

// loadDetails returns the device registration details based on a provided device key
func (registry *RedisRegistry) loadDetails(deviceKey string) (RegistrationDetails, error) {
	f := struct {
		id   string
		name string
		key  string
	}{defs.RedisDeviceIDField, defs.RedisDeviceNameField, defs.RedisDeviceSecretField}
	values, e := registry.hmgetstr(deviceKey, f.id, f.name, f.key)

	if e != nil {
		return RegistrationDetails{}, e
	}

	for _, v := range values {
		if filled := len(v) > 1; !filled {
			return RegistrationDetails{}, fmt.Errorf("invalid-device")
		}
	}

	return RegistrationDetails{
		DeviceID:     values[0],
		Name:         values[1],
		SharedSecret: values[2],
	}, nil
}

// loadRequest loads the registration request associated w/ a given key
func (registry *RedisRegistry) loadRequest(requestKey string) (RegistrationRequest, error) {
	f := struct {
		secret string
		name   string
	}{defs.RedisRegistrationSecretField, defs.RedisRegistrationNameField}
	values, e := registry.hmgetstr(requestKey, f.secret, f.name)

	if e != nil {
		return RegistrationRequest{}, e
	}

	for _, v := range values {
		if filled := len(v) > 1; !filled {
			return RegistrationRequest{}, fmt.Errorf("invalid-request")
		}
	}

	return RegistrationRequest{SharedSecret: values[0], Name: values[1]}, nil
}
func (registry *RedisRegistry) genAllocationKey(id string) string {
	return fmt.Sprintf("%s:%s", defs.RedisRegistrationRequestListKey, id)
}

func (registry *RedisRegistry) genTokenRegistrationKey(token string) string {
	return fmt.Sprintf("%s:%s", defs.RedisDeviceTokenRegistrationKey, token)
}

func (registry *RedisRegistry) genRegistryKey(id string) string {
	return fmt.Sprintf("%s:%s", defs.RedisDeviceRegistryKey, id)
}

func (registry *RedisRegistry) genFeedbackKey(id string) string {
	return fmt.Sprintf("%s:%s", defs.RedisDeviceFeedbackKey, id)
}

func (registry *RedisRegistry) genTokenListKey(id string) string {
	return fmt.Sprintf("%s:%s", defs.RedisDeviceTokenListKey, id)
}

// hmgetstr is a wrapper around the redis HMGET command where all fields are expected to be strings
func (registry *RedisRegistry) hmgetstr(key string, fields ...string) ([]string, error) {
	args := []interface{}{key}

	for _, f := range fields {
		args = append(args, f)
	}

	response, e := registry.Do("HMGET", args...)

	if e != nil {
		return nil, e
	}

	list, e := redis.Strings(response, e)

	if e != nil {
		return nil, e
	}

	for i, s := range list {
		if empty := len(s) == 0; empty {
			return nil, fmt.Errorf("invalid-entry[%s:%s]", fields[i], s)
		}
	}

	return list, nil
}

// del is a wrapper around DEL that casts to a string
func (registry *RedisRegistry) del(key string) error {
	_, e := registry.Do("DEL", key)
	return e
}

// llen is a wrapper around HGET that casts to a string
func (registry *RedisRegistry) llen(key string) (int, error) {
	response, e := registry.Do("LLEN", key)

	if e != nil {
		return -1, e
	}

	return redis.Int(response, e)
}

// lrangestr is a wrapper around HGET that casts to a string
func (registry *RedisRegistry) lrangestr(key string, start, end int) ([]string, error) {
	response, e := registry.Do("LRANGE", key, start, end)

	if e != nil {
		return nil, e
	}

	result, e := redis.Strings(response, e)

	if e != nil {
		return nil, fmt.Errorf(defs.ErrBadRedisResponse)
	}

	return result, nil
}

// hmset is a wrapper around hset
func (registry *RedisRegistry) hmset(key string, pairs ...string) error {
	args := []interface{}{key}

	for _, i := range pairs {
		args = append(args, i)
	}

	_, e := registry.Do("HMSET", args...)

	return e
}

// hset is a wrapper around hset
func (registry *RedisRegistry) hset(key, field, value string) error {
	_, e := registry.Do("HSET", key, field, value)
	return e
}

// hgetstr is a wrapper around HGET that casts to a string
func (registry *RedisRegistry) hgetstr(key, field string) (string, error) {
	response, e := registry.Do("HGET", key, field)

	if e != nil {
		return "", e
	}

	return redis.String(response, e)
}

// fill is responsible for loading the information stored during the registration request and creating records in both
// the device registry index as well as the device registry (keys w/ device hash information)
func (registry *RedisRegistry) fill(requestKey, deviceID string) error {
	request, e := registry.loadRequest(requestKey)

	if e != nil {
		return e
	}

	if _, e := registry.Do("LPUSH", defs.RedisDeviceIndexKey, deviceID); e != nil {
		return e
	}

	registryKey := registry.genRegistryKey(deviceID)

	f := struct {
		id   string
		name string
		key  string
	}{defs.RedisDeviceIDField, defs.RedisDeviceNameField, defs.RedisDeviceSecretField}

	_, e = registry.Do("HMSET", registryKey, f.id, deviceID, f.name, request.Name, f.key, request.SharedSecret)

	if e != nil {
		return e
	}

	registry.Infof("filling device registry w/ name[%s] id[%s]", request.Name, deviceID)

	defer registry.Do("DEL", requestKey)

	return nil
}

// Do attempts to get an available connection from the pool and execute a command against it.
func (registry *RedisRegistry) Do(commandName string, args ...interface{}) (reply interface{}, err error) {
	conn := registry.Pool.Get()
	defer conn.Close()
	return conn.Do(commandName, args...)
}
