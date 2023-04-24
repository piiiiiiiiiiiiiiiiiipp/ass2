package main

import (
	"bytes"
	"encoding/json"
	_ "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"greenlight.bcc/internal/data"
)

func TestRegisterUserHandler(t *testing.T) {
	// Initialize a new instance of the application struct
	app := newTestApplication(t)

	// Create a new HTTP POST request to the /v1/users endpoint
	jsonPayload := `{"name": "test user", "email": "test@example.com","password": "testpass123"}`
	req, err := http.NewRequest("POST", "/v1/users", strings.NewReader(jsonPayload))
	if err != nil {
		t.Fatal(err)
	}

	// Set the request header content-type to JSON
	req.Header.Set("Content-Type", "application/json")

	// Create a new ResponseRecorder to record the response from the handler
	rr := httptest.NewRecorder()

	// Call the registerUserHandler method, passing in the ResponseRecorder and Request
	app.registerUserHandler(rr, req)

	// Check the status code is as expected
	if rr.Code != http.StatusCreated {
		t.Errorf("expected status 201 but got %d", rr.Code)
	}

	// Check the response body is as expected
	expected := `{"user":{"id":0,"created_at":"0001-01-01T00:00:00Z","name":"test user","email":"test@example.com","activated":false}}`
	if strings.Compare(rr.Body.String(), expected) != 1 {
		t.Errorf("unexpected response body: %s", rr.Body.String())
	}
}

func TestActivateUserHandler(t *testing.T) {
	// Initialize a new instance of the application struct
	app := newTestApplication(t)

	// Create a new user to activate
	user := &data.User{
		Name:      "test user",
		Email:     "test@example.com",
		Activated: false,
	}
	err := user.Password.Set("testpass123")
	if err != nil {
		t.Fatal(err)
	}
	err = app.models.Users.Insert(user)
	if err != nil {
		t.Fatal(err)
	}

	// Create a new activation token for the user
	token, err := app.models.Tokens.New(user.ID, 3*24*time.Hour, data.ScopeActivation)
	if err != nil {
		t.Fatal(err)
	}

	// Create a JSON payload containing the activation token
	jsonPayload := map[string]string{"token": token.Plaintext}
	payloadBytes, err := json.Marshal(jsonPayload)
	if err != nil {
		t.Fatal(err)
	}

	// Create a new HTTP POST request to the /v1/users/activate endpoint
	req, err := http.NewRequest("POST", "/v1/users/activate", bytes.NewBuffer(payloadBytes))
	if err != nil {
		t.Fatal(err)
	}

	// Set the request header content-type to JSON
	req.Header.Set("Content-Type", "application/json")

	// Create a new ResponseRecorder to record the response from the handler
	rr := httptest.NewRecorder()

	// Call the activateUserHandler method, passing in the ResponseRecorder and Request
	app.activateUserHandler(rr, req)

	// Check the status code is as expected
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 but got %d", rr.Code)
	}

	// Check the response body is as expected
	expected := `{"user":{"id":1,"name":"test user","email":"test@example.com","activated":true}}`
	if rr.Body.String() != expected {
		t.Errorf("unexpected response body: %s", rr.Body.String())
	}

}
