package main

import (
	"context"
	"expvar"
	"greenlight.bcc/internal/data"
	"net/http"
	"net/http/httptest"
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
