package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeUpdateManager struct {
	status     UpdateStatus
	checkErr   error
	requestErr error
	requested  string
}

func (f *fakeUpdateManager) Check(context.Context) (UpdateStatus, error) {
	return f.status, f.checkErr
}

func (f *fakeUpdateManager) Request(_ context.Context, version string) error {
	f.requested = version
	return f.requestErr
}

func updateTestHandler(t *testing.T, sessionEmail, ownerEmail string, updates UpdateManager) (http.Handler, *http.Cookie) {
	t.Helper()
	database := openTestDB(t)
	owners, err := newOwnerStore(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ownerEmail != "" {
		if _, err := owners.bindIfAbsent(ownerEmail, json.RawMessage(`{"email":"`+ownerEmail+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	sessions, err := newSessionStore(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := sessions.create("token", "cloud", json.RawMessage(`{"email":"`+sessionEmail+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	handler, _, err := NewHandler(Deps{
		Port:       8787,
		APIBaseURL: "http://cloud.invalid",
		WebBaseURL: "http://web.invalid",
		DB:         database,
		Updates:    updates,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, &http.Cookie{Name: cookieName, Value: session.ID}
}

func TestUpdateStatusRequiresOwnerSessionAndSameOrigin(t *testing.T) {
	manager := &fakeUpdateManager{status: UpdateStatus{
		CurrentVersion: "v1.0.0", TargetVersion: "v1.1.0",
		UpdateAvailable: true, Capable: true, State: UpdateStateIdle,
	}}
	handler, cookie := updateTestHandler(t, "owner@example.com", "owner@example.com", manager)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("without session status = %d, want 401", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil)
	request.AddCookie(cookie)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-site status = %d, want 403", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var got UpdateStatus
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.UpdateAvailable || !got.Capable || got.TargetVersion != "v1.1.0" {
		t.Fatalf("unexpected status: %+v", got)
	}
}

func TestUpdateStatusRejectsNonOwnerSession(t *testing.T) {
	handler, cookie := updateTestHandler(t, "other@example.com", "owner@example.com", &fakeUpdateManager{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/system/update", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestUpdateRequestValidatesBodyAndQueuesPinnedVersion(t *testing.T) {
	manager := &fakeUpdateManager{}
	handler, cookie := updateTestHandler(t, "owner@example.com", "owner@example.com", manager)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/system/update", strings.NewReader(`{"version":"v1.1.0","url":"https://evil.invalid"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown-field status = %d, want 400", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/system/update", strings.NewReader(`{"version":"v1.1.0"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", response.Code, response.Body.String())
	}
	if manager.requested != "v1.1.0" {
		t.Fatalf("requested = %q", manager.requested)
	}
}

func TestUpdateRequestMapsSafeErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "pending", err: ErrUpdatePending, want: http.StatusConflict, code: "UPDATE_ALREADY_PENDING"},
		{name: "stale", err: ErrUpdateStale, want: http.StatusConflict, code: "UPDATE_NOT_AVAILABLE"},
		{name: "invalid", err: ErrUpdateInvalid, want: http.StatusBadRequest, code: "UPDATE_REQUEST_INVALID"},
		{name: "internal", err: errors.New("secret /root/path"), want: http.StatusServiceUnavailable, code: "UPDATE_REQUEST_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager := &fakeUpdateManager{requestErr: tc.err}
			handler, cookie := updateTestHandler(t, "owner@example.com", "owner@example.com", manager)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/system/update", strings.NewReader(`{"version":"v1.1.0"}`))
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want || !strings.Contains(response.Body.String(), tc.code) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "/root/path") {
				t.Fatal("internal error details leaked")
			}
		})
	}
}
