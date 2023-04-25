package main

import (
	"context"
	"errors"
	"expvar"
	"greenlight.bcc/internal/data"
	"greenlight.bcc/internal/jsonlog"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestRecoverPanic(t *testing.T) {
	testcase := []struct {
		name               string
		method             string
		handler            http.HandlerFunc
		expectedConnection string
	}{
		{
			name:   "default",
			method: "GET",
			handler: func(w http.ResponseWriter, r *http.Request) {
				panic("something went wrong")
			},
			expectedConnection: "",
		},
	}
	for _, test := range testcase {
		app := newTestApplication(t)

		handler := app.recoverPanic(test.handler)

		r := httptest.NewRequest(test.method, "/", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if w.Header().Get("Connection") == test.expectedConnection {
			t.Errorf("expected close, but got '%s'", w.Header().Get("Connection"))
		}

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected status code %d but got %d", http.StatusInternalServerError, w.Code)
		}
	}
}

func TestRequireActivatedUser(t *testing.T) {
	tests := []struct {
		name        string
		user        *data.User
		testHandler http.HandlerFunc
		expected    int
	}{
		{
			name: "inactive user",
			testHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}),
			user:     &data.User{Activated: false},
			expected: http.StatusForbidden,
		},
		{
			name: "active user",
			testHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			user:     &data.User{Activated: true},
			expected: http.StatusOK,
		},
	}

	var app application

	for _, test := range tests {
		handler := app.requireActivatedUser(test.testHandler)

		req := httptest.NewRequest("GET", "/test", nil)

		w := httptest.NewRecorder()

		r := app.contextSetUser(req, test.user)
		handler.ServeHTTP(w, r)

		if test.expected != w.Code {
			t.Errorf("expected status code %q, but got %q", test.expected, w.Code)
		}

	}
}

func TestRequireAuthenticatedUser(t *testing.T) {
	tests := []struct {
		name           string
		user           *data.User
		testHandler    http.HandlerFunc
		expectedStatus int
		expectedBool   bool
	}{
		{
			name: "user",
			testHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			user:           &data.User{Activated: true},
			expectedStatus: http.StatusOK,
			expectedBool:   true,
		},
		{
			name: "",
			testHandler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(http.StatusUnauthorized)
			}),
			user:           data.AnonymousUser,
			expectedStatus: http.StatusUnauthorized,
			expectedBool:   false,
		},
	}

	var app application

	for _, test := range tests {
		handler := app.requireAuthenticatedUser(test.testHandler)

		req := httptest.NewRequest("GET", "/test", nil)

		w := httptest.NewRecorder()

		r := app.contextSetUser(req, test.user)
		handler.ServeHTTP(w, r)

		if test.expectedStatus != w.Code {
			t.Errorf("expected status code %q, but got %q", test.expectedStatus, w.Code)
		}
	}
}

func TestMetricsHandler(t *testing.T) {
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	app := &application{}

	req, err := http.NewRequest("GET", "/test", nil)
	if err != nil {
		t.Fatalf("could not create request: %v", err)
	}

	w := httptest.NewRecorder()

	app.metrics(mockHandler).ServeHTTP(w, req)

	if got := expvar.Get("total_requests_received").String(); got != "1" {
		t.Errorf("total_requests_received = %v, want %v", got, 1)
	}
	if got := expvar.Get("total_responses_sent").String(); got != "1" {
		t.Errorf("total_responses_sent = %v, want %v", got, 1)
	}
	if got := expvar.Get("total_responses_sent_by_status").(*expvar.Map).Get("200").String(); got != "1" {
		t.Errorf("total_responses_sent_by_status[200] = %v, want %v", got, 1)
	}
}

func TestRequirePermission(t *testing.T) {

	testUser := &data.User{
		ID:        1,
		CreatedAt: time.Now(),
		Name:      "Test User",
		Email:     "test@example.com",
		Activated: true,
		Version:   1,
	}

	mockPermissions := data.MockPermissionModel{}
	mockPermissions.GetAllForUser(testUser.ID)

	app := newTestApplication(t)

	nextHandlerCalled := false
	mockNextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
	})

	handler := app.requirePermission("foo", mockNextHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, testUser))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if nextHandlerCalled {
		t.Error("Expected next handler to be called")
	}

	if rr.Code != http.StatusForbidden {
		t.Error("Expected response status code not to be forbidden")
	}
}

func TestEnableCORS(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	var tests = []struct {
		name         string
		method       string
		expectHeader bool
	}{
		{"preflight", "OPTIONS", true},
		{"get", "GET", false},
	}

	for _, test := range tests {
		app := newTestApplication(t)

		handler := app.enableCORS(nextHandler)

		r := httptest.NewRequest(test.method, "http://example.com", nil)
		r.Header.Set("Origin", "http://localhost:3000")
		r.Header.Set("Access-Control-Request-Method", "PUT")

		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if test.expectHeader && w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
			t.Errorf("Access-Control-Allow-Origin header not set correctly for %s request", test.name)
		}

		if !test.expectHeader && w.Header().Get("Access-Control-Allow-Origin") == "" {
			t.Errorf("Access-Control-Allow-Origin header set incorrectly for %s request", test.name)
		}
	}
}

type MockedUsersModel struct {
}

func (m *MockedUsersModel) Insert(user *data.User) error {
	return nil
}

func (m *MockedUsersModel) GetByEmail(email string) (*data.User, error) {
	return nil, nil
}
func (m *MockedUsersModel) Update(user *data.User) error {
	return nil
}

func (m *MockedUsersModel) GetForToken(tokenScope string, tokenPlaintext string) (*data.User, error) {
	switch tokenPlaintext {
	case "ValidTokenqwerrewwerewqqwe":
		return &data.User{ID: 1, Activated: true}, nil
	case "qInvalidTokenwqerqwerqwerq":
		return nil, data.ErrRecordNotFound
	case "qweqweqweqweqweqweqweqw321":
		return nil, errors.New("error")
	default:
		return nil, data.ErrRecordNotFound
	}
}

func TestAuthenticate(t *testing.T) {
	app := newTestApplication(t)

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	testCases := []struct {
		name            string
		authorization   string
		mockGetForToken func(string, string) (*data.User, error)
		expectedStatus  int
	}{
		{
			name:          "NoAuthorizationHeader",
			authorization: "",
			mockGetForToken: func(scope, token string) (*data.User, error) {
				return nil, data.ErrRecordNotFound
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:          "InvalidAuthorizationHeaderFormat",
			authorization: "Invalid Header",
			mockGetForToken: func(scope, token string) (*data.User, error) {
				return nil, data.ErrRecordNotFound
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:          "InvalidAuthorizationHeaderFormat",
			authorization: "Bearer TOKEN",
			mockGetForToken: func(scope, token string) (*data.User, error) {
				return nil, data.ErrRecordNotFound
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:          "InvalidAuthorizationHeaderFormat",
			authorization: "Bearer qweqweqweqweqweqweqweqw321",
			mockGetForToken: func(scope, token string) (*data.User, error) {
				return nil, data.ErrRecordNotFound
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:          "InvalidToken",
			authorization: "Bearer qInvalidTokenwqerqwerqwerq",
			mockGetForToken: func(scope, token string) (*data.User, error) {
				return nil, data.ErrRecordNotFound
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:          "ValidToken",
			authorization: "Bearer ValidTokenqwerrewwerewqqwe",
			mockGetForToken: func(scope, token string) (*data.User, error) {
				return &data.User{ID: 1, Activated: true}, nil
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			app.models.Users = &MockedUsersModel{}

			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tc.authorization)
			res := httptest.NewRecorder()

			middleware := app.authenticate(http.HandlerFunc(handler))
			middleware.ServeHTTP(res, req)

			if res.Code != tc.expectedStatus {
				t.Errorf("Expected status %d; got %d", tc.expectedStatus, res.Code)
			}
		})
	}
}

func newTestApplicationWithLimit(rps float64, burst int, enabled bool) *application {
	return &application{
		config: config{
			limiter: struct {
				rps     float64
				burst   int
				enabled bool
			}{rps: rps, burst: burst, enabled: enabled},
		},
	}
}

func TestRateLimit_Disabled(t *testing.T) {
	app := newTestApplicationWithLimit(1, 1, false)
	ts := httptest.NewServer(app.rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != "OK" {
		t.Errorf("expected body 'OK', got %q", string(body))
	}
}

func TestRateLimit_Enabled_Success(t *testing.T) {
	app := newTestApplicationWithLimit(10, 2, true)
	ts := httptest.NewServer(app.rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != "OK" {
		t.Errorf("expected body 'OK', got %q", string(body))
	}
}

func TestRateLimit_Enabled_Exceeded(t *testing.T) {
	app := newTestApplicationWithLimit(1, 1, true)
	ts := httptest.NewServer(app.rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})))
	defer ts.Close()

	// First request should be successful
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Second request should be rate-limited
	resp, err = http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", resp.StatusCode)
	}
}

func TestRateLimit_Enabled_BadRemoteAddr(t *testing.T) {
	app := newTestApplicationWithLimit(1, 1, true)
	app.logger = jsonlog.New(os.Stdout, jsonlog.LevelInfo)
	ts := httptest.NewServer(app.rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})))
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Set an invalid RemoteAddr
	req.RemoteAddr = "bad-address"

	resp := httptest.NewRecorder()
	app.rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.Code)
	}
}
