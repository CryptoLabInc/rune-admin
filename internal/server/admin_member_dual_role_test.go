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

// TestConsoleAdminMemberRowCannotBeDeleted pins the one guard on the one path
// that can remove a pre-existing member row (DELETE /users). The admin's row is
// their identity: memberships are keyed by its UUID, only the admin can grant,
// and a re-created row gets a fresh UUID — so a delete silently destroys access
// nobody else can restore. Revoking the admin's team access stays allowed.
func TestConsoleAdminMemberRowCannotBeDeleted(t *testing.T) {
	f := newConsoleAPIFixture(t)
	adminID := f.registerAdmin(t)

	// A plain member deleted in the SAME batch must still succeed: the refusal
	// is per-item, not a whole-request rejection.
	plain, err := f.members.Add("dev@corp.com", "Dev")
	if err != nil {
		t.Fatal(err)
	}

	status, body := f.do(t, http.MethodDelete, "/users?userIds="+adminID+","+plain.ID, "")
	if status != http.StatusOK {
		t.Fatalf("delete batch: status=%d body=%s", status, body)
	}
	var res struct {
		Succeeded []string `json:"succeeded"`
		Failed    []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"failed"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode batch result: %v (%s)", err, body)
	}
	if len(res.Failed) != 1 || res.Failed[0].ID != adminID || res.Failed[0].Code != "CANNOT_DELETE_ADMIN" {
		t.Errorf("failed = %+v, want the admin refused with CANNOT_DELETE_ADMIN", res.Failed)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != plain.ID {
		t.Errorf("succeeded = %v, want the plain member %s", res.Succeeded, plain.ID)
	}

	// The refusal must leave the admin's row untouched — a half-applied cascade
	// (token revoked, memberships dropped) would be worse than either outcome.
	if _, err := f.members.Get(adminID); err != nil {
		t.Errorf("admin member row was removed: %v", err)
	}
	if _, err := f.members.Get(plain.ID); err == nil {
		t.Error("plain member survived the batch")
	}
}

// TestConsoleAdminTeamAccessCanBeRevoked is the other half of the contract: the
// guard protects the identity row, NOT the admin's grants. An admin's memory
// reach is pure membership like anyone's, so an operator must be able to strip
// every team from them — the row simply survives it.
func TestConsoleAdminTeamAccessCanBeRevoked(t *testing.T) {
	f := newConsoleAPIFixture(t)
	adminID := f.registerAdmin(t)

	status, body := f.do(t, http.MethodPost, "/teams", `{"name":"Platform"}`)
	if status != http.StatusCreated {
		t.Fatalf("create team: %d %s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	teamID, _ := created["id"].(string)

	if status, body = f.do(t, http.MethodPost, "/teams/"+teamID+"/members",
		`{"account":"admin@corp.com","role":"edit"}`); status != http.StatusCreated {
		t.Fatalf("grant admin: %d %s", status, body)
	}

	status, body = f.do(t, http.MethodDelete, "/users/"+adminID+"/members/roles?teamIds="+teamID, "")
	if status != http.StatusOK {
		t.Fatalf("revoke admin team access: status=%d body=%s", status, body)
	}
	if _, err := f.members.Get(adminID); err != nil {
		t.Errorf("revoking access removed the admin's row: %v", err)
	}
	for _, m := range f.v.Groups().ListMemberships() {
		if m.User == adminID {
			t.Errorf("admin membership survived the revoke: %+v", m)
		}
	}
}
