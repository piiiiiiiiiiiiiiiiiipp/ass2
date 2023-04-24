package main

import (
	"net/http"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	app := newTestApplication(t)
	ts := newTestServer(t, app.routes())
	defer ts.Close()

	code, _, body := ts.get(t, "/v1/healthcheck")

	if code != http.StatusOK {
		t.Errorf("want %d; got %d", http.StatusOK, code)
	}

	expResp := `{"status":"available","system_info":{"environment":"","user_name":"","version":"1.0.0"}}
`

	if string(body) != expResp {
		t.Errorf("want body to equal %q,\n but got %q", expResp, string(body))
	}
}
