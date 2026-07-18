package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// registerAdmin puts the fixture into the post-first-login state: admin@corp.com
// is the org admin AND holds a member registry row (zero group memberships),
// exactly what OwnerRegistrar creates on first login.
func (f *consoleAPIFixture) registerAdmin(t *testing.T) string {
	t.Helper()
	f.v.Groups().SetOrgAdmin("admin@corp.com")
	m, err := f.members.Add("admin@corp.com", "Admin")
	if err != nil {
		t.Fatalf("register admin member row: %v", err)
	}
	return m.ID
}

// TestConsoleAdminCanBeAddedToTeam pins that the CANNOT_INVITE_ADMIN guard is
// gone: the org admin holds a normal team membership like any member. admin-ness
// (IsOrgAdmin — grant authority) is a separate axis and does not bar membership,
// nor does it change how memory access is governed (purely the granted role).
func TestConsoleAdminCanBeAddedToTeam(t *testing.T) {
	f := newConsoleAPIFixture(t)
	f.registerAdmin(t)

	status, body := f.do(t, http.MethodPost, "/teams", `{"name":"Platform"}`)
	if status != http.StatusCreated {
		t.Fatalf("create team: %d %s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	teamID, _ := created["id"].(string)

	status, body = f.do(t, http.MethodPost, "/teams/"+teamID+"/members",
		`{"account":"admin@corp.com","role":"edit"}`)
	if status != http.StatusCreated {
		t.Fatalf("add admin to team: status=%d body=%s (guard must be gone)", status, body)
	}
	var added map[string]any
	_ = json.Unmarshal(body, &added)
	if added["role"] != "edit" {
		t.Errorf("added admin role=%v, want edit", added["role"])
	}
}
