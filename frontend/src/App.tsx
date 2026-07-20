import { Route, Routes } from "react-router";

import LoginPage from "@/pages/LoginPage";
import NotFoundPage from "@/pages/NotFoundPage";
import SessionsPage from "@/pages/SessionsPage";
import TeamsPage from "@/pages/TeamsPage";
import UITestPage from "@/pages/UITestPage";
import UsersPage from "@/pages/UsersPage";
import WorkspacePage from "@/pages/WorkspacePage";
import LandingRedirect from "@/components/auth/LandingRedirect";
import RequireAuth from "@/components/auth/RequireAuth";
import NoticeModal from "@/components/elements/NoticeModal";
import AppLayout from "@/components/layout/AppLayout";
import ToastContainer from "@/components/toast/ToastContainer";
import { PATH_LIST } from "@/constants/commonConstants";

/** App defines the top-level route table for Rune Console. */
const App = () => {
  return (
    <>
      <Routes>
        <Route path={PATH_LIST.login} element={<LoginPage />} />
        <Route element={<RequireAuth />}>
          <Route element={<AppLayout />}>
            <Route index element={<LandingRedirect />} />
            <Route path={PATH_LIST.workspace} element={<WorkspacePage />} />
            <Route path={PATH_LIST.teams} element={<TeamsPage />} />
            <Route path={PATH_LIST.users} element={<UsersPage />} />
            <Route path={PATH_LIST.sessions} element={<SessionsPage />} />
            <Route path={PATH_LIST.uiTest} element={<UITestPage />} />
          </Route>
        </Route>
        {/* 404 (SC-04) — reachable regardless of auth; outside RequireAuth so a
            logged-out visitor sees it instead of a redirect to sign-in. */}
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
      <ToastContainer />
      <NoticeModal />
    </>
  );
};

export default App;
