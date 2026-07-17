/** BRAND_WORDMARK is the displayed product wordmark, shared by the navbars
 * (SC-01/SC-03) and the login card heading (SC-01) so the brand string has a
 * single source of truth. */
export const BRAND_WORDMARK = "RUNE CONSOLE";

/** BTN_TEXT is the single source of truth for visible action-button labels
 * (Button `btnText` / TextButton) across the console screens, so a wording
 * change lands in one place. Icon-button aria-labels are intentionally out of
 * scope — those are accessibility strings, not button captions. */
export const BTN_TEXT = {
  // Generic actions
  close: "닫기",
  cancel: "취소",
  confirm: "확인",
  save: "저장",
  delete: "삭제하기",
  remove: "제거하기",
  add: "추가",
  create: "생성",
  change: "변경하기",
  refresh: "새로고침",
  // Auth / navigation
  signOut: "로그아웃",
  getStarted: "시작하기",
  login: "로그인하기",
  home: "홈으로",
  // Workspace
  restart: "재실행",
  stop: "중지",
  deactivate: "비활성화",
  // Teams
  createTeam: "새 팀 만들기",
  createGroup: "팀 생성하기",
  rename: "이름 변경",
  deleteTeam: "팀 삭제하기",
  addMember: "멤버 추가하기",
  addTeam: "팀 추가하기",
  // Members / invitations
  invite: "초대하기",
  inviteMember: "멤버 초대하기",
  sendInvitation: "초대 전송",
  resendInvitationCode: "초대 코드 재전송",
  cancelInvitation: "초대 취소",
  cancelAction: "취소하기",
  addTeamRole: "+ 팀/권한 추가",
  removeRow: "✕",
  updateChanges: "변경사항 업데이트",
  deactivateSession: "세션 비활성화",
  deleteMember: "멤버 삭제",
} as const;

/** MODAL_TITLES is the single source of truth for ModalLayout titles across
 * the console modals, mirroring BTN_TEXT so a wording change lands in one
 * place. Titles that embed a name or count are functions; the rest are plain
 * strings. */
export const MODAL_TITLES = {
  // Workspace
  workspaceManage: "워크스페이스 관리",
  workspaceDelete: "워크스페이스 삭제",
  // Teams
  createTeam: "새 팀 만들기",
  renameTeam: "팀 이름 변경",
  deleteTeam: (teamName: string) => `팀 삭제 — ${teamName}`,
  addMember: (teamName: string) => `멤버 추가 — ${teamName}`,
  batchFailure: "일부 항목을 처리하지 못했습니다",
  // Members / roles / invitations
  roleChange: "권한 변경",
  removeMembership: "멤버십 제거",
  inviteMember: "멤버 초대",
  cancelInvitation: "초대 취소",
  deactivateSession: "세션 비활성화",
  deleteMemberSingle: (account: string) => `멤버 삭제 — ${account}`,
  deleteMemberBulk: (count: number) => `멤버 삭제 (${count}명)`,
} as const;

export const PATH_LIST = {
  home: "/",
  login: "/login",
  workspace: "/workspace",
  teams: "/teams",
  users: "/users",
  sessions: "/sessions",
  uiTest: "/ui-test",
} as const;

export const NAV_LIST = [
  { title: "팀 관리", url: PATH_LIST.teams },
  { title: "멤버 관리", url: PATH_LIST.users },
  { title: "세션 관리", url: PATH_LIST.sessions },
] as const;

export const QUERY_KEYS = {
  teamsTree: "teamsTree",
  users: "users",
  usersStats: "usersStats",
  session: "session",
  team: "team",
  teamMembers: "teamMembers",
  workspace: "workspace",
  user: "user",
  invitations: "invitations",
} as const;
