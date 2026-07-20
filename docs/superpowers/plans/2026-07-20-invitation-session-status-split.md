# Invitation/Session Status 2-Axis Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single `status` member field (5 values) with two independent fields — `invitationStatus` (invite_pending / invite_expired / invite_redeemed) and `sessionStatus` (online / offline) — across the mock server, the frontend, and the Go backend.

**Architecture:** The wire contract gains two fields and drops `status`. `sessionStatus` reflects session-token liveness + agent activation; `invitationStatus` reflects the state of the member's latest invite code. The old `session_expired` value disappears — it becomes `{invitationStatus: invite_redeemed, sessionStatus: offline}`. Work order: mock server + frontend first (to lock the contract), backend last (re-shape the already-existing derived status).

**Tech Stack:** TypeScript + React + Vite + Vitest (frontend), a small Node/TS mock server with a bespoke test harness (`frontend/mock-server`), Go 1.x with the standard `testing` package (`internal/`).

## Global Constraints

- Two wire fields everywhere a member/user is serialized: `invitationStatus: "invite_pending" | "invite_expired" | "invite_redeemed"` and `sessionStatus: "online" | "offline"`. The old `status` field is REMOVED from every DTO.
- `session_expired` is not a value in either axis. Any code, seed, test, or comment referencing it must be migrated to `{invitationStatus: "invite_redeemed", sessionStatus: "offline"}`.
- Korean UI copy is fixed: 온라인 / 오프라인 (session), 초대 수락 대기 / 초대 코드 만료 / 초대 코드 사용됨 (invitation).
- List views (UsersPage, TreeDetailView) show ONLY the session status. The invitation status is shown only in MemberDetailDrawer.
- The member-list status filter offers exactly: 전체 (all) / 온라인 (online) / 오프라인 (offline).
- Button enable rules: [초대 취소] enabled iff `invitationStatus === "invite_pending"`; [세션 비활성화] enabled iff `sessionStatus === "online"`; [재전송] always enabled.
- Resend always drives `invitationStatus` to `invite_pending` (session unchanged).
- Field ordering in commits: each task ends with tests passing before commit. Never commit with a red build.
- Commit message trailer (every commit):
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  ```

---

## File Structure

**Mock server** (`frontend/mock-server/`)
- `types.ts` — replace `MemberStatus` with `TInvitationStatus` + `TSessionStatus`; `User`/`InvitationRow` gain the two fields.
- `store.ts` — seed `USER_SEED` and `seedUsersAndMemberships` produce two fields.
- `routes/invitations.ts` — invite / resend / cancel transitions on two fields.
- `routes/users.ts` — `listUsers` status filter (session axis), `deactivateSession` transition, `userView` passthrough.
- `routes/teamMembers.ts` — `addTeamMember` codeSent logic, `memberItem`/`inheritedMemberItems` defaults.
- `__tests__/mockContract.test.ts` — assertions migrated to two fields.

**Frontend** (`frontend/src/`)
- `types/teamTypes.ts` — `TInvitationStatus`, `TSessionStatus`; `TTeamMember` two fields; remove `TTeamMemberStatus`.
- `types/userTypes.ts` — `TUserListItem`, `TInvitationResponse` two fields.
- `types/commonTypes.ts` — repurpose `TMemberStatus` → session chip states.
- `constants/styleConstants.ts` — `MEMBER_STATUS_VAR` (session) + new `INVITATION_STATUS_VAR`.
- `components/users/memberStatusMap.ts` — session chip map + invitation label map.
- `components/teams/TreeDetailView.tsx` — inline `CHIP_STATUS` → session; render session.
- `pages/UsersPage.tsx` — filter options (3), render session.
- `components/users/MemberDetailDrawer.tsx` — invitation display, subtitle, button rules.
- `pages/teamsDummyData.ts`, `pages/UITestPage.tsx` — dummy data + gallery to two fields.
- Test files under `__tests__/` — assertions migrated.

**Backend** (`internal/server/`)
- `console_api_users.go` — `status()` → `statuses()` returning both; `userDTO`/`userWire`, `validUserStatus`, `userDTO`/`memberDTO` builders, `userStats`, `userSessionDeactivate`, `createInvitation`, `resendInvitation`, `cancelInvitation` responses.
- `console_api.go` — `memberDTO` struct two fields.
- `console_api_test.go` — status assertions migrated.

---

## Task 1: Baseline — isolate the in-progress work and branch

The working tree already contains an unrelated in-progress `username` feature (37 modified files). It must not be entangled with this change.

**Files:** none created; git only.

- [ ] **Step 1: Inspect the working tree**

Run: `git -C /Users/geuna/Desktop/work/rune-console status --short`
Expected: the modified `username`-related files listed in the session's git status.

- [ ] **Step 2: Commit the in-progress username work as its own commit (only if the user confirms it is complete)**

If the user confirms the WIP is a finished, separable change:
```bash
cd /Users/geuna/Desktop/work/rune-console
git add -A
git commit -m "feat(console): add username display field to members

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
If the user does NOT confirm, `git stash push -u -m "wip-username"` instead and restore it later. Do not silently fold it into this plan's commits.

- [ ] **Step 3: Create the feature branch**

```bash
cd /Users/geuna/Desktop/work/rune-console
git checkout -b feat/invitation-session-status-split
```
Expected: `Switched to a new branch 'feat/invitation-session-status-split'`.

- [ ] **Step 4: Confirm the frontend test runner and Go build both work from a clean baseline**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run 2>&1 | tail -20`
Expected: existing suite passes (this is the green baseline).
Run: `cd /Users/geuna/Desktop/work/rune-console && go build ./... 2>&1 | tail -20`
Expected: no output (builds clean).

No commit for this task beyond Steps 2–3.

---

## Task 2: Mock server — types + seed data

**Files:**
- Modify: `frontend/mock-server/types.ts:6-11,36-53`
- Modify: `frontend/mock-server/store.ts:48-53,58-154,156-189`

**Interfaces:**
- Produces: `TInvitationStatus = "invite_pending" | "invite_expired" | "invite_redeemed"`, `TSessionStatus = "online" | "offline"`; `User` and `InvitationRow` each carry `invitationStatus` and `sessionStatus`. Consumed by Tasks 3.

- [ ] **Step 1: Replace the status union in types.ts**

In `frontend/mock-server/types.ts`, replace lines 6-11:
```typescript
export type MemberStatus =
  | "online"
  | "invite_redeemed"
  | "invite_pending"
  | "invite_expired"
  | "session_expired";
```
with:
```typescript
export type TInvitationStatus =
  | "invite_pending"
  | "invite_expired"
  | "invite_redeemed";

export type TSessionStatus = "online" | "offline";
```

- [ ] **Step 2: Update the `User` type (types.ts lines 36-45)**

Replace the `User` type's `status` field:
```typescript
export type User = {
  id: string;
  account: string;
  // Display name (not an identifier — account stays unique, API 2026-07-20).
  username: string;
  invitationStatus: TInvitationStatus;
  sessionStatus: TSessionStatus;
  lastAccessAt: string | null;
  lastInvitedAt: string | null;
  sessionExpiredAt: string | null;
};
```
(`sessionExpiredAt` stays: the backend still emits it; the drawer no longer branches on it.)

- [ ] **Step 3: Update the `Seed` type in store.ts (lines 48-53)**

Replace:
```typescript
type Seed = {
  account: string;
  username: string;
  status: User["status"];
  teams: Array<[string, Membership["role"]]>;
};
```
with:
```typescript
type Seed = {
  account: string;
  username: string;
  invitationStatus: User["invitationStatus"];
  sessionStatus: User["sessionStatus"];
  teams: Array<[string, Membership["role"]]>;
};
```

- [ ] **Step 4: Rewrite `USER_SEED` (store.ts lines 58-154) to the two-field form**

Apply this mapping for each of the 14 seed users (old `status` → two fields):
- `online` → `invitationStatus: "invite_redeemed", sessionStatus: "online"`
- `invite_pending` → `invitationStatus: "invite_pending", sessionStatus: "offline"`
- `invite_expired` → `invitationStatus: "invite_expired", sessionStatus: "offline"`
- `invite_redeemed` → `invitationStatus: "invite_redeemed", sessionStatus: "offline"`
- `session_expired` → `invitationStatus: "invite_redeemed", sessionStatus: "offline"`

Concretely, replace each seed object's `status: "..."` line. The full rewritten array:
```typescript
const USER_SEED: Seed[] = [
  { account: "kim@corp.com", username: "김철수", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_1", "edit"], ["t_2", "write"]] },
  { account: "lee@corp.com", username: "lee young hee", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_1", "read"]] },
  { account: "park@corp.com", username: "박민준", invitationStatus: "invite_pending", sessionStatus: "offline", teams: [["t_2", "read"]] },
  { account: "choi@corp.com", username: "최지우", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_3", "edit"]] },
  { account: "jung@corp.com", username: "정다은", invitationStatus: "invite_redeemed", sessionStatus: "offline", teams: [["t_1", "write"], ["t_3", "read"]] },
  { account: "kang@corp.com", username: "강호진", invitationStatus: "invite_expired", sessionStatus: "offline", teams: [["t_4", "read"]] },
  { account: "cho@corp.com", username: "cho min soo", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_4", "edit"], ["t_5", "edit"]] },
  { account: "yoon@corp.com", username: "윤아름", invitationStatus: "invite_pending", sessionStatus: "offline", teams: [["t_5", "read"]] },
  { account: "jang@corp.com", username: "장원석", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_2", "read"]] },
  { account: "lim@corp.com", username: "임재현", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_3", "write"]] },
  { account: "han@corp.com", username: "한지민", invitationStatus: "invite_redeemed", sessionStatus: "offline", teams: [["t_1", "read"]] },
  { account: "oh@corp.com", username: "오세영", invitationStatus: "invite_pending", sessionStatus: "offline", teams: [["t_4", "write"]] },
  // invite_redeemed + offline: code used (token released) but the agent has
  // never authenticated — the "초대 코드 사용됨 · 연결 대기 중" state.
  { account: "seo@corp.com", username: "서준호", invitationStatus: "invite_redeemed", sessionStatus: "offline", teams: [["t_5", "read"]] },
  { account: "shin@corp.com", username: "신동혁", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [] },
];
```

- [ ] **Step 5: Rewrite `seedUsersAndMemberships` (store.ts lines 156-189)**

Replace the `users.push({...})` block so it populates both fields and derives timestamps from the new axes:
```typescript
    users.push({
      id,
      account: s.account,
      username: s.username,
      invitationStatus: s.invitationStatus,
      sessionStatus: s.sessionStatus,
      lastAccessAt: s.sessionStatus === "online" ? daysAgo(i % 5) : null,
      // Any offline member that once redeemed but is not currently online may
      // carry an invited stamp; pending/expired always do.
      lastInvitedAt:
        s.invitationStatus === "invite_pending" ||
        s.invitationStatus === "invite_expired" ||
        s.invitationStatus === "invite_redeemed"
          ? daysAgo((i % 4) + 1)
          : null,
      // A redeemed member that is now offline models a destroyed session.
      sessionExpiredAt:
        s.invitationStatus === "invite_redeemed" && s.sessionStatus === "offline"
          ? daysAgo(i % 3)
          : null,
    });
```

- [ ] **Step 6: Type-check the mock server**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npx tsc --noEmit -p mock-server/tsconfig.json 2>&1 | tail -30`
(If `mock-server/tsconfig.json` does not exist, run `npx tsc --noEmit 2>&1 | grep mock-server | head -30`.)
Expected: errors ONLY in `routes/*.ts` (which still reference `.status` — fixed in Task 3). `types.ts` and `store.ts` themselves report no errors.

- [ ] **Step 7: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/mock-server/types.ts frontend/mock-server/store.ts
git commit -m "feat(mock): split member status into invitation + session axes (types + seed)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 3: Mock server — route transitions

**Files:**
- Modify: `frontend/mock-server/routes/invitations.ts:63-119,128-144,146-158`
- Modify: `frontend/mock-server/routes/users.ts:56,103-118`
- Modify: `frontend/mock-server/routes/teamMembers.ts:39-49,67-88,141-175`

**Interfaces:**
- Consumes: `User.invitationStatus`, `User.sessionStatus` (Task 2).
- Produces: invite/resend/cancel/deactivate/addTeamMember responses carrying `invitationStatus` + `sessionStatus`; `listUsers` filters on `sessionStatus`.

- [ ] **Step 1: `invitations.ts` — new-user creation (around line 63-72)**

In `invite`, the new-user object literal currently sets `status: "invite_pending"`. Replace with:
```typescript
    user = {
      id: nextId("u"),
      account,
      username,
      invitationStatus: "invite_pending",
      sessionStatus: "offline",
      lastAccessAt: null,
      lastInvitedAt: new Date().toISOString(),
      sessionExpiredAt: null,
    };
```

- [ ] **Step 2: `invitations.ts` — codeSent judgment + response (lines 100-119)**

Replace the block starting `// Per-status judgment` through the `sendJson` call:
```typescript
  // Send a fresh code when the member cannot currently get in: no live pending
  // code AND not online. (invite_pending has a live code; online is already in.)
  const codeSent =
    isNew ||
    (user.invitationStatus !== "invite_pending" &&
      user.sessionStatus !== "online");
  if (codeSent) {
    user.invitationStatus = "invite_pending";
    user.lastInvitedAt = new Date().toISOString();
    state.invitations.push({
      account: user.account,
      username: user.username,
      issuedAt: new Date().toISOString(),
      lastAccessAt: null,
    });
  }
  sendJson(ctx.res, 201, {
    userId: user.id,
    account: user.account,
    username: user.username,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
    codeSent,
  });
```

- [ ] **Step 2b: `invitations.ts` — InvitationRow push if it references status**

The `state.invitations.push` objects use `account/username/issuedAt/lastAccessAt` only — no status field. Confirm `InvitationRow` (types.ts) is unchanged. No edit needed if it has no status.

- [ ] **Step 3: `invitations.ts` — resend (lines 128-144)**

Replace the body:
```typescript
export const resend = (ctx: Ctx): void => {
  const body = (ctx.body ?? {}) as { userId?: unknown };
  const user = requireUserById(String(body.userId ?? ""));
  // Resend = issue a new code. The latest code is now a fresh pending one,
  // so invitationStatus always becomes invite_pending; sessionStatus is
  // untouched. A new history row is always added.
  user.invitationStatus = "invite_pending";
  user.lastInvitedAt = new Date().toISOString();
  state.invitations.push({
    account: user.account,
    username: user.username,
    issuedAt: new Date().toISOString(),
    lastAccessAt: null,
  });
  sendJson(ctx.res, 200, {
    userId: user.id,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
  });
};
```

- [ ] **Step 4: `invitations.ts` — cancel (lines 146-158)**

Replace the body:
```typescript
export const cancel = (ctx: Ctx): void => {
  const body = (ctx.body ?? {}) as { userId?: unknown };
  const user = requireUserById(String(body.userId ?? ""));
  if (user.invitationStatus !== "invite_pending") {
    throw new HttpError(
      409,
      "INVITATION_NOT_PENDING",
      "user is not in invite_pending",
    );
  }
  user.invitationStatus = "invite_expired";
  sendJson(ctx.res, 200, {
    userId: user.id,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
  });
};
```

- [ ] **Step 5: `users.ts` — listUsers status filter (line 56)**

Replace:
```typescript
  if (status) rows = rows.filter((u) => u.status === status);
```
with (filter on the session axis; values "online"/"offline"):
```typescript
  if (status) rows = rows.filter((u) => u.sessionStatus === status);
```

- [ ] **Step 6: `users.ts` — deactivateSession (lines 103-118)**

Replace the body:
```typescript
export const deactivateSession = (ctx: Ctx): void => {
  const user = state.users.find((u) => u.id === ctx.params.userId);
  if (!user) throw new HttpError(404, "USER_NOT_FOUND", "user not found");
  // Deactivation destroys the live session token — only meaningful while
  // online (the visible session state maps 1:1 to the button).
  if (user.sessionStatus !== "online") {
    throw new HttpError(
      409,
      "SESSION_NOT_ACTIVE",
      "no active session to destroy",
    );
  }
  user.sessionStatus = "offline";
  user.sessionExpiredAt = new Date().toISOString();
  sendJson(ctx.res, 200, {
    userId: user.id,
    invitationStatus: user.invitationStatus,
    sessionStatus: user.sessionStatus,
  });
};
```

- [ ] **Step 7: `teamMembers.ts` — memberItem + inheritedMemberItems defaults (lines 39-49, 67-88)**

In `memberItem`, replace `status: user?.status ?? "session_expired",` with:
```typescript
    invitationStatus: user?.invitationStatus ?? "invite_redeemed",
    sessionStatus: user?.sessionStatus ?? "offline",
```
Apply the identical replacement to the object literal in `inheritedMemberItems` (line 84).

- [ ] **Step 8: `teamMembers.ts` — addTeamMember new-user + codeSent (lines 141-175)**

Replace the new-user literal's `status: "invite_pending",` with:
```typescript
      invitationStatus: "invite_pending",
      sessionStatus: "offline",
```
Then replace the codeSent block:
```typescript
  const codeSent =
    isNew ||
    (user.invitationStatus !== "invite_pending" &&
      user.sessionStatus !== "online");
  if (codeSent) {
    user.invitationStatus = "invite_pending";
    user.lastInvitedAt = new Date().toISOString();
    state.invitations.push({
      account: user.account,
      username: user.username,
      issuedAt: new Date().toISOString(),
      lastAccessAt: null,
    });
  }
```

- [ ] **Step 9: Type-check the mock server**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npx tsc --noEmit -p mock-server/tsconfig.json 2>&1 | tail -30`
Expected: no errors in `frontend/mock-server` (routes now consistent; test file fixed in Step 10).

- [ ] **Step 10: Migrate `__tests__/mockContract.test.ts` (lines 46,61,66,71)**

Open the file and update every status assertion:
- Line 71 `"session_expired"` → the test's intent is "after deactivate, session is offline". Change the asserted response field to `sessionStatus` equalling `"offline"`.
- Lines 46,61,66 `"invite_redeemed"` → assert on `invitationStatus === "invite_redeemed"`, and add a companion assert on `sessionStatus` where the test previously distinguished online vs redeemed. Read the surrounding assertion to preserve intent (e.g. the deactivate test should assert `invitationStatus === "invite_redeemed"` AND `sessionStatus === "offline"`).

- [ ] **Step 11: Run the mock server test suite**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run mock-server 2>&1 | tail -30`
Expected: all mock-server tests pass. If a test still references `.status`, fix it to the appropriate axis and re-run.

- [ ] **Step 12: Smoke the running mock server**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend/mock-server && bash smoke.sh 2>&1 | tail -30`
Expected: smoke script exits 0. If `smoke.sh` greps for `"status"` or `session_expired`, update those greps to the new field names.

- [ ] **Step 13: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/mock-server/routes frontend/mock-server/__tests__ frontend/mock-server/smoke.sh
git commit -m "feat(mock): route transitions on invitation + session axes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 4: Frontend — shared types

**Files:**
- Modify: `frontend/src/types/teamTypes.ts:15-17,29-43`
- Modify: `frontend/src/types/userTypes.ts:4-14,70-77`
- Modify: `frontend/src/types/commonTypes.ts:15-17`

**Interfaces:**
- Produces: `TInvitationStatus`, `TSessionStatus` (teamTypes); `TUserListItem` and `TTeamMember` carry both fields; `TInvitationResponse` carries both; `TMemberStatus` (commonTypes) = `"online" | "offline"`. `TTeamMemberStatus` is removed. Consumed by Tasks 5–9.

- [ ] **Step 1: teamTypes.ts — replace the status union (lines 15-17)**

Replace:
```typescript
/** Member status on the wire (common contract). */
export type TTeamMemberStatus =
  "online" | "invite_redeemed" | "invite_pending" | "invite_expired" | "session_expired";
```
with:
```typescript
/** Invitation-code lifecycle status on the wire (common contract). */
export type TInvitationStatus =
  "invite_pending" | "invite_expired" | "invite_redeemed";

/** Session-token liveness on the wire (common contract). */
export type TSessionStatus = "online" | "offline";
```

- [ ] **Step 2: teamTypes.ts — `TTeamMember` (lines 30-43)**

Replace the `status: TTeamMemberStatus;` field with:
```typescript
  invitationStatus: TInvitationStatus;
  sessionStatus: TSessionStatus;
```

- [ ] **Step 3: userTypes.ts — `TUserListItem` (lines 4-14) and imports (line 1)**

Change the import on line 1 to:
```typescript
import type {
  TInvitationStatus,
  TSessionStatus,
  TTeamMemberRole,
} from "@/types/teamTypes";
```
Replace the `status: TTeamMemberStatus;` field in `TUserListItem` with:
```typescript
  invitationStatus: TInvitationStatus;
  sessionStatus: TSessionStatus;
```

- [ ] **Step 4: userTypes.ts — `TInvitationResponse` (lines 70-77)**

Replace `status: TTeamMemberStatus;` with:
```typescript
  invitationStatus: TInvitationStatus;
  sessionStatus: TSessionStatus;
```

- [ ] **Step 5: commonTypes.ts — repurpose `TMemberStatus` (lines 15-17)**

Replace:
```typescript
/** Member connection status (wireframe v0.4 token model, 5 badges). */
export type TMemberStatus =
  "online" | "redeemed" | "pending" | "invite-expired" | "session-expired";
```
with:
```typescript
/** Session chip state — the only status a list view renders. */
export type TMemberStatus = "online" | "offline";
```

- [ ] **Step 6: Type-check (expect cascade errors in consumers)**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npx tsc --noEmit 2>&1 | grep -E "src/(components|pages|constants)" | head -40`
Expected: errors in `styleConstants.ts`, `memberStatusMap.ts`, `UsersPage.tsx`, `TreeDetailView.tsx`, `MemberDetailDrawer.tsx`, dummy/test files — these are fixed in Tasks 5–9. No errors in the three type files themselves.

- [ ] **Step 7: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/src/types/teamTypes.ts frontend/src/types/userTypes.ts frontend/src/types/commonTypes.ts
git commit -m "feat(console): split member status types into invitation + session axes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 5: Frontend — status chip + label maps

**Files:**
- Modify: `frontend/src/constants/styleConstants.ts:82-89`
- Modify: `frontend/src/components/users/memberStatusMap.ts:1-12`

**Interfaces:**
- Consumes: `TMemberStatus` (= session), `TInvitationStatus`, `TSessionStatus` (Task 4).
- Produces: `MEMBER_STATUS_VAR` keyed by session state and `INVITATION_STATUS_VAR` keyed by invitation state (both `{label, color}`, in `styleConstants.ts`); `CHIP_STATUS: Record<TSessionStatus, TMemberStatus>` (in `memberStatusMap.ts`). Consumed by Tasks 6–9.

- [ ] **Step 1: styleConstants.ts — session chip vars + invitation label vars (lines 82-89)**

Replace `MEMBER_STATUS_VAR`:
```typescript
/* Session chips — the only status a list view shows. */
export const MEMBER_STATUS_VAR = {
  online: { label: "온라인", color: "text-mint" },
  offline: { label: "오프라인", color: "text-faint" },
} as const;

/* Invitation-status labels — shown only in the member detail drawer. */
export const INVITATION_STATUS_VAR = {
  invite_pending: { label: "초대 수락 대기", color: "text-warning" },
  invite_expired: { label: "초대 코드 만료", color: "text-faint" },
  invite_redeemed: { label: "초대 코드 사용됨", color: "text-accent-blue" },
} as const;
```

- [ ] **Step 2: memberStatusMap.ts — session chip map + invitation label helper (lines 1-12)**

Replace the whole file:
```typescript
import type { TMemberStatus } from "@/types/commonTypes";
import type { TSessionStatus } from "@/types/teamTypes";

/** API session status → MemberStatus chip state. Identity today, but kept as a
    seam so the chip vocabulary can diverge from the wire later. */
export const CHIP_STATUS: Record<TSessionStatus, TMemberStatus> = {
  online: "online",
  offline: "offline",
};
```

- [ ] **Step 3: Type-check the two files**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npx tsc --noEmit 2>&1 | grep -E "styleConstants|memberStatusMap" | head`
Expected: no errors referencing these two files. (Consumers still error — next tasks.)

- [ ] **Step 4: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/src/constants/styleConstants.ts frontend/src/components/users/memberStatusMap.ts
git commit -m "feat(console): session chip + invitation label maps

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 6: Frontend — UsersPage (list shows session; filter is 전체/온라인/오프라인)

**Files:**
- Modify: `frontend/src/pages/UsersPage.tsx:59-67,491-493`
- Test: `frontend/src/pages/__tests__/UsersPage.test.tsx`

**Interfaces:**
- Consumes: `CHIP_STATUS` (Task 5), `user.sessionStatus` (Task 4).

- [ ] **Step 1: Update the failing test first — filter options + row rendering**

In `frontend/src/pages/__tests__/UsersPage.test.tsx`, update the mock user fixtures to use `invitationStatus`/`sessionStatus` (search for `status:` at lines ~45,119,225,290,368) and change any filter-option assertions to expect exactly 전체/온라인/오프라인. Add/adjust a test asserting a row renders 온라인 for `sessionStatus: "online"` and 오프라인 for `"offline"`.

- [ ] **Step 2: Run the test to confirm it fails**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run UsersPage 2>&1 | tail -30`
Expected: FAIL (component still references old options / `user.status`).

- [ ] **Step 3: Replace STATUS_OPTIONS (lines 59-67)**

```typescript
/* Filter/sort option sets (SC-11 no.2–3). "all" stands in for 전체. The list
   shows only the session axis, so the filter matches it. */
const STATUS_OPTIONS: TDropdownOption[] = [
  { value: "all", label: "전체" },
  { value: "online", label: "온라인" },
  { value: "offline", label: "오프라인" },
];
```

- [ ] **Step 4: Update the table cell render (line ~493)**

Replace `<MemberStatus status={CHIP_STATUS[user.status]} />` with:
```typescript
                  <MemberStatus status={CHIP_STATUS[user.sessionStatus]} />
```

- [ ] **Step 5: Run the test to confirm it passes**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run UsersPage 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/src/pages/UsersPage.tsx frontend/src/pages/__tests__/UsersPage.test.tsx
git commit -m "feat(console): UsersPage list + filter on session status

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 7: Frontend — TreeDetailView (team member list shows session)

**Files:**
- Modify: `frontend/src/components/teams/TreeDetailView.tsx:67-74,640-642`
- Test: `frontend/src/components/teams/__tests__/TreeDetailView.test.tsx`

**Interfaces:**
- Consumes: `CHIP_STATUS` from `memberStatusMap` (Task 5), `member.sessionStatus` (Task 4).

- [ ] **Step 1: Remove the inline CHIP_STATUS, import the shared one (lines 67-74)**

Delete the local `const CHIP_STATUS: Record<TTeamMemberStatus, TMemberStatus> = {...}` block (lines 67-74) and add to the imports:
```typescript
import { CHIP_STATUS } from "@/components/users/memberStatusMap";
```
Remove now-unused `TTeamMemberStatus`/`TMemberStatus` imports if they were only used by the deleted block.

- [ ] **Step 2: Update the member row render (line ~642)**

Replace `<MemberStatus status={CHIP_STATUS[member.status]} />` with:
```typescript
                    <MemberStatus status={CHIP_STATUS[member.sessionStatus]} />
```

- [ ] **Step 3: Update the test fixtures**

In `frontend/src/components/teams/__tests__/TreeDetailView.test.tsx`, change member fixtures from `status:` to `invitationStatus`/`sessionStatus`, and any status-chip assertion to expect 온라인/오프라인.

- [ ] **Step 4: Run the test**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run TreeDetailView 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/src/components/teams/TreeDetailView.tsx frontend/src/components/teams/__tests__/TreeDetailView.test.tsx
git commit -m "feat(console): team member list on session status

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 8: Frontend — MemberDetailDrawer (invitation status + button rules)

**Files:**
- Modify: `frontend/src/components/users/MemberDetailDrawer.tsx:44-57,258-262,281,444`
- Test: `frontend/src/components/users/__tests__/MemberDetailDrawer.test.tsx`

**Interfaces:**
- Consumes: `INVITATION_STATUS_VAR` (Task 5), `CHIP_STATUS` (session, Task 5), `user.invitationStatus`, `user.sessionStatus` (Task 4).

- [ ] **Step 1: Update tests first — invitation display + button enable rules**

In `frontend/src/components/users/__tests__/MemberDetailDrawer.test.tsx`:
- Change fixtures to `invitationStatus`/`sessionStatus` (lines ~36,45).
- Assert the drawer shows the invitation label (e.g. 초대 코드 사용됨) AND the session chip (온라인/오프라인).
- Assert [초대 취소] is enabled only when `invitationStatus === "invite_pending"`.
- Assert [세션 비활성화] is enabled only when `sessionStatus === "online"`.

- [ ] **Step 2: Run tests to confirm failure**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run MemberDetailDrawer 2>&1 | tail -30`
Expected: FAIL.

- [ ] **Step 3: Rewrite `subtitleFor` (lines 44-57)**

```typescript
/** Per-status header timestamp (SC-13 no.1 — D13). Session takes priority:
    an online member shows last access; otherwise the invitation axis drives it. */
const subtitleFor = (user: TUserListItem): string => {
  if (user.sessionStatus === "online") {
    return `최근 접속 ${formatDate(user.lastAccessAt)}`;
  }
  switch (user.invitationStatus) {
    case "invite_redeemed":
      return "초대 코드 사용됨 · 연결 대기 중";
    case "invite_pending":
    case "invite_expired":
      return `최근 초대 코드 발송 ${formatDateTime(user.lastInvitedAt)}`;
  }
};
```

- [ ] **Step 4: Update the status row (lines 258-262) to show session chip + invitation label**

Add the import:
```typescript
import { INVITATION_STATUS_VAR } from "@/constants/styleConstants";
```
Replace the status row:
```typescript
          <div className={styles.statusRow}>
            <MemberStatus status={CHIP_STATUS[user.sessionStatus]} />
            <span className={INVITATION_STATUS_VAR[user.invitationStatus].color}>
              {INVITATION_STATUS_VAR[user.invitationStatus].label}
            </span>
            <span className={styles.accessTime}>{subtitleFor(user)}</span>
          </div>
```

- [ ] **Step 5: Update the cancel-invitation button disable (line 281)**

Replace `disabled={user.status !== "invite_pending"}` with:
```typescript
                disabled={user.invitationStatus !== "invite_pending"}
```

- [ ] **Step 6: Update the deactivate-session button disable (line 444)**

Replace `disabled={user.status === "session_expired"}` with (enabled only while online):
```typescript
              disabled={user.sessionStatus !== "online"}
```

- [ ] **Step 7: Run the tests**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run MemberDetailDrawer 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/src/components/users/MemberDetailDrawer.tsx frontend/src/components/users/__tests__/MemberDetailDrawer.test.tsx
git commit -m "feat(console): drawer shows invitation status; button rules on two axes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 9: Frontend — dummy data, gallery, and remaining consumers

**Files:**
- Modify: `frontend/src/pages/teamsDummyData.ts` (type at :16 + all data rows)
- Modify: `frontend/src/pages/UITestPage.tsx:82,96,817`
- Modify: `frontend/src/components/elements/__tests__/StatusChips.test.tsx:14`
- Modify: `frontend/src/components/users/InviteMemberModal.tsx:241`, `frontend/src/components/teams/AddMemberModal.tsx:86` (only if they read/emit `status`)
- Modify: `frontend/src/hooks/queries/__tests__/useTeamMembersQuery.test.tsx:34`, `frontend/src/hooks/mutations/__tests__/useInvitationMutations.test.tsx:18`

**Interfaces:**
- Consumes: `TInvitationStatus`, `TSessionStatus` (Task 4).

- [ ] **Step 1: teamsDummyData.ts — migrate the local status type (line 16) and every data row**

If line 16 declares a local status union, replace it with the two fields on the row type, then convert every data row using the Task 2 mapping table (online → redeemed+online, session_expired → redeemed+offline, etc.). The occurrences to convert (verbatim list): lines 133(comment),146,159,168,177,186,198,207,219,228,237,246,258,267,279,290,299,314,323,332,341,353,362,371,395,402,409,416,423,430. Each `status: "X"` becomes `invitationStatus: "...", sessionStatus: "..."`.

- [ ] **Step 2: UITestPage.tsx — status gallery (lines 82,96,817)**

This is a component gallery. Update it to exercise the new chip vocabulary: render `MemberStatus` with `online`/`offline`, and (if it demonstrates member states) show the three invitation labels via `INVITATION_STATUS_VAR`. Remove references to `redeemed`/`pending`/`invite-expired`/`session-expired` as `MemberStatus` inputs (those are no longer `TMemberStatus`).

- [ ] **Step 3: StatusChips.test.tsx (line 14)**

Update the chip test to the two session states (온라인/오프라인). If it previously enumerated all five, split: session chips here, invitation labels asserted where they now live.

- [ ] **Step 4: InviteMemberModal.tsx (241) and AddMemberModal.tsx (86)**

Inspect both. If line references `"online"` only as an unrelated string, leave it. If it reads `response.status` from an invite response, change to read `response.invitationStatus`/`response.sessionStatus`. Only edit if it touches the member status contract.

- [ ] **Step 5: Hook test fixtures (useTeamMembersQuery.test.tsx:34, useInvitationMutations.test.tsx:18)**

Update mock member/invitation-response fixtures from `status:` to the two fields.

- [ ] **Step 6: Full type-check — must be clean**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npx tsc --noEmit 2>&1 | tail -30`
Expected: NO errors anywhere. Any remaining `status` reference is a missed consumer — fix it.

- [ ] **Step 7: Full frontend test suite**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npm run test -- --run 2>&1 | tail -30`
Expected: all suites pass.

- [ ] **Step 8: Confirm no stale literals remain**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && grep -rn "session_expired\|session-expired\|TTeamMemberStatus\|\"redeemed\"\|\"pending\"\|\"invite-expired\"" src mock-server | grep -v node_modules`
Expected: no output (all migrated).

- [ ] **Step 9: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add frontend/src
git commit -m "feat(console): migrate dummy data, gallery, and remaining status consumers

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 10: Backend — derivation function + DTOs

**Files:**
- Modify: `internal/server/console_api_users.go:37-96,209-236,255-335,437-443`
- Modify: `internal/server/console_api.go:364-374`

**Interfaces:**
- Produces: `func (idx *userIndex) statuses(m members.Member) memberStatuses` returning `{invitation, session string}`; `userDTO`/`userWire`/`memberDTO` carry `InvitationStatus`+`SessionStatus` json fields; `validInvitationStatus`/`validSessionStatus`. Consumed by Task 11 handlers/tests.

- [ ] **Step 1: VERIFY the redemption→invite-status relationship (blocking, read-only)**

Before writing the derivation, confirm what happens to a member's latest invite AFTER the code is redeemed (Unwrap). Read `internal/invites/` and `internal/tokens/store.go` and `internal/server/grpc.go:469`.
Run: `cd /Users/geuna/Desktop/work/rune-console && grep -rn "StatusCompleted\|StatusConsumed\|StatusRedeemed\|MarkCompleted\|Consume\|Redeem" internal/invites internal/tokens | head -30`
Determine: after redemption, is the used invite still `StatusPending`, or does it move to a completed/consumed status?
- If it becomes non-pending on redemption → Step 3's primary logic (latest-pending ⇒ invite_pending) is correct as written.
- If it STAYS pending after redemption → use the Step 3 ALTERNATE that gates invite_pending on "a live pending invite issued after the member last activated" (compare `latestInvite.CreatedAt` to activation time). Record the finding in the commit message.

- [ ] **Step 2: Add the `memberStatuses` type near `tokenLive` (console_api_users.go ~line 122)**

```go
// memberStatuses is the two-axis console status: the invitation-code lifecycle
// and the session-token liveness, derived independently from the member, its
// latest invite, and its token. session_expired no longer exists — a redeemed
// member whose token is gone is {invite_redeemed, offline}.
type memberStatuses struct {
	invitation string // invite_pending | invite_expired | invite_redeemed
	session    string // online | offline
}
```

- [ ] **Step 3: Replace `status()` with `statuses()` (lines 209-236)**

Primary version (use when redemption marks the invite non-pending):
```go
// statuses derives the two console status axes (SC-11 defs, 2026-07-20 split):
//   session:    online  = active member, live token, agent self-reported
//                         activation (ReportActivation); offline otherwise.
//   invitation: invite_pending  = latest code is live (unused, not expired);
//               invite_expired  = latest code expired/revoked, never redeemed;
//               invite_redeemed = the latest code was used (no newer live code).
// Resend issues a fresh live code, so a redeemed/expired member flips back to
// invite_pending automatically — no special case here.
func (idx *userIndex) statuses(m members.Member) memberStatuses {
	session := "offline"
	redeemed := false
	switch m.Status {
	case members.StatusActive:
		redeemed = true
		if tl, ok := idx.tokenByEmail[m.Email]; ok && !tl.expired && tl.activatedAt != "" {
			session = "online"
		}
	case members.StatusDisabled:
		redeemed = true
	}

	inv, ok := idx.latestInvite[m.ID]
	switch {
	case ok && !idx.inviteExpired(inv):
		// A live pending code is the most recent invitation event, even for a
		// member who previously redeemed (this is the resend case).
		return memberStatuses{invitation: "invite_pending", session: session}
	case redeemed:
		return memberStatuses{invitation: "invite_redeemed", session: session}
	case ok && idx.inviteExpired(inv):
		return memberStatuses{invitation: "invite_expired", session: session}
	default:
		// Invited with no invite record yet — treat as pending.
		return memberStatuses{invitation: "invite_pending", session: session}
	}
}
```
ALTERNATE first `case` (use only if Step 1 found redemption leaves the invite pending) — replace the first `case` with a check that the live invite post-dates activation:
```go
	case ok && !idx.inviteExpired(inv) && (!redeemed || idx.inviteAfterActivation(inv, m)):
		return memberStatuses{invitation: "invite_pending", session: session}
```
and add the helper (compare `inv.CreatedAt` to the member's activation/last-active timestamp; return true when the code was issued after the member last became active — i.e. a genuine resend). Ground its correctness in `TestConsoleUserRedeemedNotOnline` staying green.

- [ ] **Step 4: Keep `inviteExpired()` unchanged (lines 238-253)**

No edit — `statuses()` reuses it.

- [ ] **Step 5: Update `userDTO` + `userWire` structs (lines 37-96)**

In `userDTO` (line ~48) replace `Status  string \`json:"status"\`` with:
```go
	InvitationStatus string `json:"invitationStatus"`
	SessionStatus    string `json:"sessionStatus"`
```
Apply the identical replacement in `userWire` (line ~89). If `MarshalJSON` copies `Status` between the two, update it to copy both new fields.

- [ ] **Step 6: Update the `userDTO` builder (lines 255-299)**

Replace the `Status: idx.status(m),` line in the returned struct with:
```go
		InvitationStatus:     st.invitation,
		SessionStatus:        st.session,
```
and compute `st` at the top of the return block (before the `return userDTO{`):
```go
	st := idx.statuses(m)
```

- [ ] **Step 7: Update `memberDTO` struct (console_api.go lines 364-374)**

Replace `Status  string \`json:"status"\`` with:
```go
	InvitationStatus string `json:"invitationStatus"`
	SessionStatus    string `json:"sessionStatus"`
```

- [ ] **Step 8: Update `memberDTO` + `inheritedMemberDTO` builders (lines 302-335)**

In `memberDTO` (line 302): replace `account, status := "", "invite_pending"` and the populate with:
```go
	account := ""
	st := memberStatuses{invitation: "invite_pending", session: "offline"}
	if mem, ok := idx.memberByID[m.User]; ok {
		account, st = mem.Email, idx.statuses(mem)
	}
	return memberDTO{
		UserID:           m.User,
		Account:          account,
		Role:             string(m.Role),
		InvitationStatus: st.invitation,
		SessionStatus:    st.session,
		JoinedAt:         wireTimePtr(m.GrantedAt),
	}
```
Apply the analogous change to `inheritedMemberDTO` (lines 323-335), keeping `JoinedAt: nil` and `Role: string(groups.RoleRead)`.

- [ ] **Step 9: Replace `validUserStatus` (lines 437-443)**

```go
// validInvitationStatus / validSessionStatus report whether s is a value the
// derivation can emit. Kept in lockstep with statuses().
func validInvitationStatus(s string) bool {
	switch s {
	case "invite_pending", "invite_expired", "invite_redeemed":
		return true
	}
	return false
}

func validSessionStatus(s string) bool {
	return s == "online" || s == "offline"
}
```
If `validUserStatus` is referenced elsewhere (e.g. a status query-param validator), update that caller: the list filter now validates against `validSessionStatus`.
Run: `cd /Users/geuna/Desktop/work/rune-console && grep -rn "validUserStatus" internal`
and fix each hit.

- [ ] **Step 10: Build**

Run: `cd /Users/geuna/Desktop/work/rune-console && go build ./... 2>&1 | tail -30`
Expected: errors only in the handlers still calling `idx.status(...)` / setting `"status"` (fixed in Task 11).

- [ ] **Step 11: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add internal/server/console_api_users.go internal/server/console_api.go
git commit -m "feat(console): derive invitation + session status as two axes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 11: Backend — handlers, responses, and tests

**Files:**
- Modify: `internal/server/console_api_users.go:530-576,815-902`
- Modify: `internal/server/console_api_test.go` (assertions at 133-136,630-683,690-724,750-788,790-846,848-868)

**Interfaces:**
- Consumes: `statuses()`, `validSessionStatus` (Task 10).

- [ ] **Step 1: Migrate the lifecycle tests FIRST (they encode the real member lifecycle — the correctness gate)**

In `console_api_test.go`, rewrite each status assertion to the two-axis contract, per the mapping table. Specifics:
- Line ~133 fresh member: `status == "invite_pending"` → `invitationStatus == "invite_pending"` AND `sessionStatus == "offline"`.
- `TestConsoleUserLiveness` (~630-683): online case → `sessionStatus == "online"` (and `invitationStatus == "invite_redeemed"`); after deactivate → `sessionStatus == "offline"` (and `invitationStatus == "invite_redeemed"`). Replace `"session_expired"` expectations accordingly.
- `TestConsoleUserRedeemedNotOnline` (~690-724): expect `invitationStatus == "invite_redeemed"` AND `sessionStatus == "offline"`.
- `TestConsoleRevokedInviteRendersInviteExpired` (~750-788): before → `invitationStatus == "invite_pending"`; after cancel → `invitationStatus == "invite_expired"`.
- `TestConsoleNeedsCode` (~790-846): unaffected by field names (asserts `needsCode`), but update any comments referencing `session_expired`.
- `TestConsoleResendSessionExpiredBecomesPending` (~848-868): resend response → `invitationStatus == "invite_pending"`.
- Rename this test to `TestConsoleResendRedeemedBecomesPending` to match the new vocabulary.

- [ ] **Step 2: Run the tests to confirm they fail against the old handlers**

Run: `cd /Users/geuna/Desktop/work/rune-console && go test ./internal/server/ -run 'Console' 2>&1 | tail -40`
Expected: FAIL (handlers still emit `status`).

- [ ] **Step 3: Fix `userSessionDeactivate` (lines 530-565)**

Replace the status gate (line 542) so deactivation requires an online session:
```go
	if st := h.newIndex().statuses(*m); st.session != "online" {
		apiErr(w, http.StatusConflict, "SESSION_NOT_ACTIVE", "user has no active session")
		return
	}
```
Replace the success response (line 564):
```go
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":           m.ID,
		"invitationStatus": h.newIndex().statuses(*m).invitation,
		"sessionStatus":    "offline",
	})
```

- [ ] **Step 4: Fix `userStats` (lines 567-576)**

Replace the pending count predicate (line 571):
```go
		if idx.statuses(m).invitation == "invite_pending" {
```

- [ ] **Step 5: Fix `createInvitation` response (lines 815-824)**

```go
	st := h.newIndex().statuses(*m)
	writeJSON(w, http.StatusCreated, map[string]any{
		"userId":           m.ID,
		"account":          m.Email,
		"invitationStatus": st.invitation,
		"sessionStatus":    st.session,
		"codeSent":         codeSent,
	})
```

- [ ] **Step 6: Fix `resendInvitation` response (line 868)**

```go
	st := h.newIndex().statuses(*m)
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":           m.ID,
		"invitationStatus": st.invitation,
		"sessionStatus":    st.session,
	})
```

- [ ] **Step 7: Fix `cancelInvitation` response (line 901)**

```go
	st := h.newIndex().statuses(*m)
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":           m.ID,
		"invitationStatus": st.invitation, // invite_expired after voiding
		"sessionStatus":    st.session,
	})
```

- [ ] **Step 8: Fix the list-users status filter validation**

Find where the `status` query param is validated/applied for GET /users.
Run: `cd /Users/geuna/Desktop/work/rune-console && grep -n "status" internal/server/console_api_users.go | grep -iE "query|filter|param|valid"`
Update it to validate against `validSessionStatus` and to filter rows by `idx.statuses(m).session`.

- [ ] **Step 9: Sweep remaining comments/literals**

Run: `cd /Users/geuna/Desktop/work/rune-console && grep -rn "session_expired\|idx.status(" internal/server/*.go | grep -v _test.go`
Expected: no functional `idx.status(` calls remain; migrate any lingering `session_expired` comments to the two-axis vocabulary.

- [ ] **Step 10: Run the backend tests**

Run: `cd /Users/geuna/Desktop/work/rune-console && go test ./internal/... 2>&1 | tail -40`
Expected: PASS. If `TestConsoleUserRedeemedNotOnline` fails, revisit Task 10 Step 3 (the redeemed-vs-live-code branch) — this test is the ground truth for that decision.

- [ ] **Step 11: Vet + build**

Run: `cd /Users/geuna/Desktop/work/rune-console && go vet ./... && go build ./... 2>&1 | tail -20`
Expected: clean.

- [ ] **Step 12: Commit**

```bash
cd /Users/geuna/Desktop/work/rune-console
git add internal/server/console_api_users.go internal/server/console_api_test.go
git commit -m "feat(console): handlers + tests on invitation + session status axes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Task 12: End-to-end verification

**Files:** none (verification only).

- [ ] **Step 1: Full frontend suite + type-check**

Run: `cd /Users/geuna/Desktop/work/rune-console/frontend && npx tsc --noEmit && npm run test -- --run 2>&1 | tail -20`
Expected: type-check clean, all suites pass.

- [ ] **Step 2: Full backend suite**

Run: `cd /Users/geuna/Desktop/work/rune-console && go test ./... 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 3: Contract cross-check — mock and backend emit identical field names**

Run: `cd /Users/geuna/Desktop/work/rune-console && grep -rn "invitationStatus\|sessionStatus" frontend/mock-server internal/server/console_api_users.go internal/server/console_api.go | grep -c invitationStatus`
Manually confirm the mock invite/resend/cancel/deactivate responses and the Go `createInvitation`/`resendInvitation`/`cancelInvitation`/`userSessionDeactivate` responses carry the SAME two field names.

- [ ] **Step 4: Run the app against the mock and eyeball the three views**

Start the mock + frontend per the repo's dev command (check `frontend/package.json` scripts, e.g. `npm run dev:mock`). Verify:
- Members list shows 온라인/오프라인 only; filter offers 전체/온라인/오프라인.
- Team detail member list shows 온라인/오프라인.
- Member drawer shows the invitation label (초대 수락 대기 / 초대 코드 만료 / 초대 코드 사용됨) + session chip; [초대 취소] enabled only for 초대 수락 대기; [세션 비활성화] enabled only when 온라인; [재전송] always enabled and flips a redeemed/expired member to 초대 수락 대기.

- [ ] **Step 5: No stale literals anywhere**

Run: `cd /Users/geuna/Desktop/work/rune-console && grep -rn "session_expired\|session-expired\|TTeamMemberStatus" frontend/src frontend/mock-server internal --include=*.ts --include=*.tsx --include=*.go | grep -v _test`
Expected: no output.

- [ ] **Step 6: Finalize**

Invoke `superpowers:finishing-a-development-branch` to decide merge/PR. Do not merge without the user's go-ahead.
