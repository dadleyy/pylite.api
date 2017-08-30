package routes

import "fmt"
import "log"
import "bytes"
import "github.com/dadleyy/beacon.api/beacon/device"
import "github.com/dadleyy/beacon.api/beacon/logging"
import "github.com/dadleyy/beacon.api/beacon/interchange"

func newTestRouteLogger() *logging.Logger {
	out := bytes.NewBuffer([]byte{})
	logger := log.New(out, "", 0)
	logger.SetFlags(0)
	return &logging.Logger{Logger: logger}
}

type feedbackStoreListParams struct {
	deviceID      string
	feedbackCount int
}

type testFeedbackStore struct {
	testErrorStore
	listResults []interchange.FeedbackMessage
	listErrors  []error
	logErrors   []error
	listCalls   []feedbackStoreListParams
}

func (t *testFeedbackStore) LogFeedback(interchange.FeedbackMessage) error {
	return t.latestError(t.logErrors)
}

func (t *testFeedbackStore) ListFeedback(d string, c int) ([]interchange.FeedbackMessage, error) {
	t.listCalls = append(t.listCalls, feedbackStoreListParams{d, c})

	if e := t.latestError(t.listErrors); e != nil {
		return nil, e
	}

	return t.listResults, nil
}

type testDeviceRegistry struct {
	testErrorStore
	allocationErrors       []error
	findErrors             []error
	fillErrors             []error
	listRegistrationErrors []error
	removalErrors          []error
	activeRegistrations    []device.RegistrationDetails
}

func (t *testDeviceRegistry) AllocateRegistration(device.RegistrationRequest) error {
	return t.latestError(t.allocationErrors)
}

func (t *testDeviceRegistry) FindDevice(string) (device.RegistrationDetails, error) {
	if e := t.latestError(t.findErrors); e != nil {
		return device.RegistrationDetails{}, e
	}

	if len(t.activeRegistrations) >= 1 {
		return t.activeRegistrations[0], nil
	}

	return device.RegistrationDetails{}, fmt.Errorf("not-found")
}

func (t *testDeviceRegistry) FillRegistration(string, string) error {
	return t.latestError(t.fillErrors)
}

func (t *testDeviceRegistry) RemoveDevice(string) error {
	return t.latestError(t.removalErrors)
}

func (t *testDeviceRegistry) ListRegistrations() ([]device.RegistrationDetails, error) {
	if e := t.latestError(t.listRegistrationErrors); e != nil {
		return nil, e
	}

	return t.activeRegistrations, nil
}

type testErrorStore struct {
}

func (t *testErrorStore) latestError(errList []error) error {
	if len(errList) >= 1 {
		return errList[0]
	}

	return nil
}

type testDeviceTokenStore struct {
	authorized     bool
	createdTokens  []device.TokenDetails
	creationErrors []error
	listedTokens   []device.TokenDetails
	listedErrors   []error
}

func (t *testDeviceTokenStore) AuthorizeToken(string, string, uint) bool {
	return t.authorized
}

func (t *testDeviceTokenStore) ListTokens(string) ([]device.TokenDetails, error) {
	if len(t.listedErrors) >= 1 {
		return nil, t.listedErrors[0]
	}

	return t.listedTokens, nil
}

func (t *testDeviceTokenStore) CreateToken(string, string, uint) (device.TokenDetails, error) {
	if len(t.createdTokens) >= 1 {
		return t.createdTokens[0], nil
	}

	if len(t.creationErrors) >= 1 {
		return device.TokenDetails{}, t.creationErrors[0]
	}

	return device.TokenDetails{}, fmt.Errorf("not-found")
}

type testDeviceIndex struct {
	testErrorStore
	foundDevices  []device.RegistrationDetails
	findErrors    []error
	removalErrors []error
}

func (t *testDeviceIndex) RemoveDevice(string) error {
	return t.latestError(t.removalErrors)
}

func (t *testDeviceIndex) FindDevice(string) (device.RegistrationDetails, error) {
	if e := t.latestError(t.findErrors); e != nil {
		return device.RegistrationDetails{}, e
	}

	return t.foundDevices[0], nil
}
