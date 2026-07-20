package server

import (
	"log/slog"
	"net/http"
)

// deleteTeam handles DELETE /teams/{id}?memoryAction={transfer|purge}&targetTeamId={id}.
//
// The team's encrypted memory is handled first (so a failure rolls back cleanly
// with nothing deleted), then the team and its memberships are removed:
//   - transfer: reassign every record tagged with this team to targetTeamId
//     (runespace RetagAll), then delete.
//   - purge: strip this team's tag from every record (runespace RemoveTag) —
//     the memory itself is not destroyed, and records shared with other teams
//     stay reachable there — then delete.
//
// Child teams block deletion (TEAM_HAS_CHILDREN); the wireframe routes that to
// an alert. Members do NOT block: the design doc allows deleting a team with
// members, so their memberships are removed as part of the deletion. The
// sole-tag delete guard is intentionally NOT applied to team deletion.
func (h *consoleAPI) deleteTeam(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("id")
	detail, err := h.v.Groups().TeamDetail(ref)
	if err != nil {
		writeGroupAPIErr(w, err) // TEAM_NOT_FOUND
		return
	}
	if len(detail.Children) > 0 {
		apiErr(w, http.StatusConflict, "TEAM_HAS_CHILDREN", "team has child teams; delete them first")
		return
	}

	action := r.URL.Query().Get("memoryAction")
	actor := actorFromContext(r.Context())
	switch action {
	case "transfer":
		target := r.URL.Query().Get("targetTeamId")
		if target == "" {
			apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "targetTeamId is required for memoryAction=transfer")
			return
		}
		targetDetail, terr := h.v.Groups().TeamDetail(target)
		if terr != nil {
			apiErr(w, http.StatusNotFound, "TEAM_NOT_FOUND", "target team not found")
			return
		}
		if targetDetail.ID == detail.ID {
			apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "cannot transfer a team's memory to itself")
			return
		}
		if _, terr := h.v.ReassignGroupTag(r.Context(), detail.ID, targetDetail.ID, actor); terr != nil {
			// The client gets the doc's generic INTERNAL_ERROR; the specific
			// runespace/tag cause goes to the server log for ops + debugging.
			slog.Error("console: team memory transfer failed",
				"team", detail.ID, "target", targetDetail.ID, "actor", actor, "err", terr)
			apiErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to transfer team memory")
			return
		}
	case "purge":
		if _, perr := h.v.RemoveGroupTag(r.Context(), detail.ID, actor); perr != nil {
			slog.Error("console: team memory purge failed",
				"team", detail.ID, "actor", actor, "err", perr)
			apiErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to purge team memory")
			return
		}
	default:
		apiErr(w, http.StatusBadRequest, "VALIDATION_ERROR", "memoryAction must be transfer or purge")
		return
	}

	// Memory handled; now remove the team and its memberships atomically.
	g, _, derr := h.v.Groups().DeleteGroupWithMembers(ref)
	if derr != nil {
		// A child team raced in after the pre-check, or the team vanished.
		// The memory op ALREADY ran, so this leaves a partial state (memory
		// retagged/purged but the team not deleted) — log it loudly for ops.
		slog.Error("console: team delete failed AFTER memory handling (partial state)",
			"team", detail.ID, "memoryAction", action, "actor", actor, "err", derr)
		writeGroupAPIErr(w, derr)
		return
	}
	auditAdmin(h.v, "admin.group.delete", actor, g.Name+" ("+g.ID+") memory="+action)
	w.WriteHeader(http.StatusNoContent)
}
