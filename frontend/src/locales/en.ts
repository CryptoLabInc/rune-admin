/**
 * English UI copy. This module is the locale SHAPE's source of truth: ko.ts
 * must satisfy TLocale, so a key added or removed here is a compile error
 * there — the two languages cannot drift apart silently.
 *
 * Strings that embed a value are plain functions, so each language controls
 * its own word order (Korean particles and counters don't map 1:1 to English).
 */
export const en = {
  btn: {
    // Generic actions
    close: "Close",
    cancel: "Cancel",
    confirm: "Confirm",
    save: "Save",
    delete: "Delete",
    remove: "Remove",
    add: "Add",
    create: "Create",
    change: "Change",
    refresh: "Refresh",
    retry: "Retry",
    update: "Update",
    later: "Later",
    // Auth / navigation
    signOut: "Sign Out",
    getStarted: "Get Started",
    login: "Log In",
    home: "Go to Home",
    // Workspace
    restart: "Restart",
    stop: "Stop",
    deactivate: "Deactivate",
    recreate: "Delete and Recreate",
    reconnect: "Reconnect",
    // Teams
    createTeam: "Create New Team",
    createGroup: "Create Team",
    rename: "Rename",
    deleteTeam: "Delete Team",
    addMember: "Add Member",
    addTeam: "Add Team",
    // Members / invitations
    invite: "Invite",
    inviteMember: "Invite Member",
    sendInvitation: "Send Invitation",
    resendInvitationCode: "Resend Invitation Code",
    cancelInvitation: "Cancel Invitation",
    cancelAction: "Cancel",
    addTeamRole: "+ Add Team/Role",
    removeRow: "✕",
    updateChanges: "Update Changes",
    resetChanges: "Reset Changes",
    deactivateSession: "Deactivate Session",
    deleteMember: "Delete Member",
  },
  modal: {
    // Workspace
    workspaceManage: "Manage Workspace",
    workspaceDelete: "Delete Workspace",
    workspaceOrphaned: "Workspace Recreation Required",
    workspaceReconnect: "Workspace Reconnection Required",
    // Teams
    createTeam: "Create New Team",
    renameTeam: "Rename Team",
    deleteTeam: (teamName: string) => `Delete Team — ${teamName}`,
    addMember: (teamName: string) => `Add Member — ${teamName}`,
    batchFailure: "Some items could not be processed",
    // Members / roles / invitations
    roleChange: "Change Role",
    removeMembership: "Remove Membership",
    inviteMember: "Invite Member",
    cancelInvitation: "Cancel Invitation",
    deactivateSession: "Deactivate Session",
    deleteMemberSingle: (account: string) => `Delete Member — ${account}`,
    deleteMemberBulk: (count: number) => `Delete Members (${count})`,
  },
  nav: {
    teams: "Team Management",
    users: "Member Management",
    sessions: "Session History",
  },
  login: {
    failed1: "There was a problem signing in.",
    failed2: "Please try again.",
    accountRequired: "A RUNE account is required to use the console.",
  },
  status: {
    member: {
      online: "Online",
      offline: "Offline",
    },
    invitation: {
      pending: "Invitation Pending",
      expired: "Invitation Code Expired",
      redeemed: "Invitation Code Used",
    },
    workspace: {
      provisioning: "Creating",
      running: "Running",
      stopping: "Stopping",
      stopped: "Stopped",
      starting: "Restarting",
      deleting: "Deleting",
      error: "Unavailable",
    },
  },
  profile: {
    menu: "Profile menu",
    image: "Profile image",
    plan: "Plan: Free",
  },
  common: {
    none: "None",
    loading: "Loading…",
    team: "Team",
    role: "Role",
    memberName: "Member Name",
    memberStatus: "Member Status",
    selectAll: "Select All",
    select: "Select",
    all: "All",
    users: "Users",
    sort: "Sort",
    sortBy: "Sort by",
    noResults: "No results found.",
    refreshRetry: "Refresh and try again.",
    tryAgainLater: "Please try again later.",
    processFailed: "Processing failed. Please try again.",
    selectName: (name: string) => `Select ${name}`,
  },
  elements: {
    pagination: "Pagination",
    prevPage: "Previous page",
    nextPage: "Next page",
    search: "Search",
    clear: "Clear",
    clickToDismiss: "Click to dismiss",
    unsavedChanges: "Unsaved changes",
  },
  auth: {
    ownerLockedTitle: "Account Not Available",
    // Sentence split around the styled owner element: prefix + owner + suffix.
    ownerLockedPrefix: "This console is managed by ",
    ownerLockedSuffix: ".",
    ownerFallback: "another account",
    ownerLockedBody: "You can't use the console with this account.",
  },
  teams: {
    // Add-member modal
    emailLabel: "Email (account)",
    invalidEmail: "Invalid email format.",
    usernameLabel: "Username",
    usernamePlaceholder: "Username",
    roleLabel: "Role",
    selectRole: "Select Role",
    // Sentence split around the inline member-status chip: prefix + status + suffix.
    invitePrefix:
      "Once the invited user connects rune, their status changes to ",
    inviteSuffix: ".",
    // Team name / create / rename
    teamNameRule: "Only numbers, Korean and English letters, - and _ are allowed.",
    teamName: "Team Name",
    teamNamePlaceholder: "e.g. platform-team",
    parentTeamOptional: "Parent Team (Optional)",
    selectTeam: "Select Team",
    parentCopyInfo1:
      "Selecting a parent team automatically copies its members to the new team.",
    parentCopyInfo2:
      "You can edit members on the detail page after the team is created.",
    dupName: "A team with this name already exists under the same parent team.",
    invalidTeamName: "Invalid team name format.",
    // Delete-team modal
    hasChildrenAlert1: "Teams with sub-teams cannot be deleted.",
    hasChildrenAlert2: "Delete the sub-teams first, then try again.",
    memoryChoiceInfo: "Choose how to handle this team's memory before deleting.",
    transferOption: "① Transfer to Another Team",
    defaultTag: "(Default)",
    destinationTeam: "Destination Team",
    confirmTargetLabel: "Confirm — Enter Target Team Name",
    selectTeamFirst: "Select a team first",
    targetNameMismatch: "Target team name does not match.",
    purgeOption: "② Delete This Team's Memory",
    purgeDesc:
      "Memories shared with other teams will remain accessible from those teams.",
    confirmDeleteLabel: "Confirm — Enter Team Name to Delete",
    nameMismatch: "Team name does not match.",
    cannotDeleteHasChildren: "Cannot delete a team that has sub-teams.",
    // Org chart
    viewDetails: (name: string) => `View ${name} details`,
    zoomOut: "Zoom Out",
    zoomIn: "Zoom In",
    // Remove-membership / role-change modals
    removeIntro: "The following memberships will be removed:",
    removeKeepSubteams:
      "Sub-team memberships are kept. Select them individually to remove if needed.",
    roleChangeIntro: "The following members' roles will be changed:",
    roleChangeCol: "Role Change",
    // Tree & detail view
    teamTree: "Team Tree",
    teamMeta: (parent: string, children: string, count: number, date: string) =>
      `Parent team: ${parent} | Sub-teams: ${children} | Members: ${count} | Created: ${date}`,
    countItems: (count: number) => `${count}`,
    membersHeading: (total: number) => `Members (${total})`,
    memberPageInfo: (total: number, pageSize: number) =>
      `${total} total · ${pageSize} per page`,
    roleHeader: "Role",
    joinedAt: "Joined",
    membersLoadError: "Unable to load the member list.",
    noMembers: "No members.",
    changesSaved: "Changes saved.",
    // Notices (team CRUD / member ops)
    createTeamTitle: "Create Team",
    teamCreated: "Team created.",
    createTeamFailed: "Failed to create the team.",
    teamRenamed: "Team name changed.",
    renameFailed: "Failed to rename the team.",
    deleteTeamTitle: "Delete Team",
    teamDeleted: "Team deleted.",
    deleteFailed: "Failed to delete the team.",
    addMemberTitle: "Add Member",
    memberAdded: "Member added.",
    addMemberFailed: "Failed to add the member.",
    roleChangeFailed: "Failed to change roles.",
    membershipsRemoved: "Memberships removed.",
    removeFailed: "Failed to remove memberships.",
    // Server error reasons
    userNotFound: "User not found",
    notTeamMember: "Not a team member",
    alreadyInvited: "This user has already been invited.",
    notRegistered: "This account is not registered.",
    cannotAddAdmin: "Console admin accounts cannot be added.",
    inviteSendFailed: "Failed to send the invite code. Please try again.",
    // Team tree / nodes
    checkTeamName: "Check the team name and try again.",
    collapseName: (name: string) => `Collapse ${name}`,
    expandName: (name: string) => `Expand ${name}`,
    // Teams page
    switchView: "Switch view",
    treeDetail: "Tree & Detail",
    orgChartView: "Org Chart",
    searchTeams: "Search teams",
    teamsLoadError: "Couldn't load teams.",
    emptyTitle: "Create your first team.",
    emptyDesc: "Create a team to manage members and memory.",
  },
  members: {
    // Invite modal (SC-12)
    emailFormatError: "Please enter a valid email address",
    duplicateAccount:
      "This account is already registered. Use Add Member or Resend Invitation Code instead.",
    sendFailed: "Failed to send invitation. Please try again.",
    teamRole: "Team / Role",
    selectTeam: "Select team",
    selectRole: "Select role",
    setTeamAria: (n: number) => `Set ${n} team`,
    setRoleAria: (n: number) => `Set ${n} role`,
    subteamPreview: "Sub-Team Role Preview",
    reason: "Reason",
    // Sentence split around the inline member-status chip: prefix + status + suffix.
    invitePrefix:
      "When invited, the user receives an invitation code; once they connect rune within 24 hours, their status changes to ",
    inviteSuffix: ".",
    inviteExpiry:
      "If they do not connect, the code expires and you can invite them again with Resend Invitation Code.",
    // Invite preview rows (invitePreview.ts)
    directlyAssigned: "Directly assigned",
    teamAlreadyInvited: "Team already invited.",
    subteamOf: (teamName: string) => `Sub-team of ${teamName}`,
    // Cancel-invitation modal (D15)
    cancelInviteExpires: (account: string) =>
      `All unused invitation codes for ${account} will expire.`,
    userNotDeleted: "The user will not be deleted.",
    // Session-deactivate modal (D12)
    deactivateConfirm: (account: string) =>
      `Deactivate the session for ${account}?`,
    allMcpTerminated: "All MCP sessions will be terminated.",
    // Member-delete modal (SC-15)
    deleteFailed: "Failed to delete member. Please try again.",
    noTeams: "Not a member of any team.",
    deleteSingleIntro: (account: string) =>
      `The account ${account} will be deleted and removed from the teams below:`,
    deleteBulkIntro:
      "The following members' accounts will be deleted and removed from the teams below:",
    // Membership-remove / role-change modals
    removeFailed: "Failed to remove membership. Please try again.",
    roleChangeFailedRetry: "Failed to change role. Please try again.",
    roleChanged: "Role changed.",
    // Member-detail drawer (SC-13)
    lastAccessed: (date: string) => `Last accessed ${date}`,
    redeemedAwaiting: "Invitation code used · Awaiting connection",
    lastInviteSentAt: (datetime: string) =>
      `Last invitation code sent ${datetime}`,
    teamNotFound: "Team not found",
    inviteCodeResent: "Invitation code resent.",
    resendCodeFailed: "Failed to resend invitation code. Please try again.",
    addTeamTitle: "Add Team",
    addedToTeam: "Added to team.",
    alreadyTeamMember: "Already a member of this team.",
    addTeamFailed: "Failed to add team. Please try again.",
    teamsHeading: (count: number) => `Teams (${count})`,
    selectAll: "Select all",
    noTeamsToAdd: "No teams to add",
    teamToAdd: "Team to add",
    roleToAdd: "Role to add",
    sessionDeactivated: "Session deactivated.",
    sessionAlreadyExpired: "This session has already expired.",
    deactivateFailed: "Failed to deactivate session. Please try again.",
    invitationCanceled: "Invitation canceled.",
    noInvitationToCancel: "No invitation to cancel.",
    cancelInvitationFailed: "Failed to cancel invitation. Please try again.",
    // Users page (SC-11)
    lastInviteSent: "Last Invitation Code Sent",
    resendFailedShort: "Resend failed",
    deleteMembersTitle: "Delete Members",
    membersDeleted: "Members deleted.",
    membersLoadError: "Couldn't load members.",
    noMembersYet: "No members invited yet",
    inviteHint: "Invite a member and an invitation code will be sent by email",
    searchByName: "Search by name",
    statusFilterAria: "Status filter",
    teamFilterAria: "Team filter",
    teamRoleHeader: "Team (Role)",
    // Sessions page (SC-16)
    lastIssued: "Last Issued",
    lastAccessedAt: "Last Accessed",
    historyLoadError: "Couldn't load history.",
    sessionPageInfo: (total: number, pageSize: number) =>
      `${total} total · ${pageSize} per page`,
    issuedAt: "Issued At",
    noHistory: "No history.",
  },
  workspace: {
    // Navbar badge (SC-03 callout 2)
    badgeLabel: "Workspace",
    badgeRecreate: "Recreation Required",
    badgeReconnect: "Reconnection Required",
    badgeNone: "No Workspace",
    // Management modal (SC-02 state D + variants)
    stopFailed: "Failed to stop workspace. Please try again.",
    restartFailed: "Failed to restart workspace. Please try again.",
    recreateFailed: "Failed to recreate workspace. Please try again.",
    deleteFailed: "Failed to delete workspace. Please try again.",
    deleteConfirm: "Delete this workspace?",
    deleteIrreversible: "This action cannot be undone.",
    tearingDown1: "Deleting the existing workspace…",
    tearingDown2: "Workspace creation will start once deletion is complete.",
    orphaned1:
      "The console was reinstalled and can no longer connect to this workspace.",
    orphaned2:
      "Previously stored data is encrypted with the old security key and cannot be recovered.",
    orphaned3: "Delete and recreate to start over with an empty workspace.",
    reconnectExpired: "The workspace connection has expired.",
    reconnectPrompt: "Reconnect to reactivate the data plane.",
    reconnectFailed: "Failed to reconnect. Please try again.",
    loadFailed: "Unable to load workspace information.",
    plan: "Plan",
    statusLabel: "Status",
    storedMemories: "Stored Memories",
    // Empty-workspace page (SC-02 states A/B/C)
    pageAria: "Workspace management",
    creatingTitle: "Creating your workspace…",
    creatingDesc: "This takes about 3–5 minutes.",
    createFailedTitle: "Workspace Creation Failed",
    createFailedDesc: "Couldn't create the workspace. Please try again.",
    createWorkspace: "Create Workspace",
    emptyTitle: "No workspace has been created.",
    emptyDesc: "Create a workspace to store memory.",
    // Console update floating card
    updatingTitle: "Updating the Console",
    updateFailedTitle: "Update Failed",
    newVersionTitle: "New Version Available",
    updateRunning: "Backing up and updating…",
    updatePreparing: "Preparing the update…",
    updateFailedBody:
      "The update could not be completed. Check the status and try again.",
    updateRestartNotice:
      "RUNE may be temporarily unavailable while the console restarts.",
  },
  validation: {
    usernameRule:
      "Only Korean, lowercase English letters, and single spaces between words are allowed.",
  },
};

export type TLocale = typeof en;
