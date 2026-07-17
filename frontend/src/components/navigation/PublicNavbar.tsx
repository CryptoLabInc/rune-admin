import { Link, useLocation, useNavigate } from "react-router";

import Button from "@/components/elements/Button";
import { PATH_LIST } from "@/constants/commonConstants";

/**
 * PublicNavbar is the signed-out top bar (SC-01 / SC-04, wireframe v0.16). It
 * mirrors the console Navbar shell — full-width background and border with the
 * inner content capped at the content width — but the rune badge and avatar
 * are login-gated, so their slot is replaced by a single [시작하기] call to
 * action that routes to the sign-in screen. The CTA is hidden on the login
 * page itself — the user is already there, so it would route to nowhere.
 */
const PublicNavbar = () => {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const onLoginPage = pathname === PATH_LIST.login;

  return (
    <header className="bg-background border-b">
      <div className="max-w-content mx-auto flex h-14 w-full items-center justify-between px-6">
        <Link to={PATH_LIST.home} className="text-lg font-semibold">
          Rune Console
        </Link>
        {!onLoginPage && (
          <Button
            btnText="시작하기"
            btnSize="sm"
            btnColor="mintFilled"
            handleClick={() => navigate(PATH_LIST.login)}
            className="w-auto"
          />
        )}
      </div>
    </header>
  );
};

export default PublicNavbar;
