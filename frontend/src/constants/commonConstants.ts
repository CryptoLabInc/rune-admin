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
