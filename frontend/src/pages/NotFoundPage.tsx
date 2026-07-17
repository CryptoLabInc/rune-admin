import { useNavigate } from "react-router";

import Button from "@/components/elements/Button";
import Navbar from "@/components/navigation/Navbar";
import PublicNavbar from "@/components/navigation/PublicNavbar";
import { useSessionQuery } from "@/hooks/queries/useSessionQuery";
import { PATH_LIST } from "@/constants/commonConstants";

/**
 * NotFoundPage is the 404 screen (SC-04). It is reachable whether or not the
 * visitor is signed in, so the top bar mirrors the session — the console
 * Navbar when logged in, the PublicNavbar otherwise. [홈으로] routes to the
 * console landing (teams) for a signed-in user and to the sign-in screen for a
 * signed-out one (wireframe D17).
 */
const NotFoundPage = () => {
  const navigate = useNavigate();
  const { data, isPending } = useSessionQuery();

  if (isPending) return null;

  const loggedIn = data?.logged_in ?? false;

  return (
    <div className="bg-background flex min-h-screen flex-col">
      {loggedIn ? <Navbar /> : <PublicNavbar />}
      <main className="grid flex-1 place-items-center px-4 text-center">
        <div>
          <h1 className="mb-4 text-2xl font-semibold">404 Not Found</h1>
          <Button
            btnText="홈으로"
            btnSize="lg"
            btnColor="mintFilled"
            handleClick={() =>
              navigate(loggedIn ? PATH_LIST.teams : PATH_LIST.login)
            }
            className="w-auto"
          />
        </div>
      </main>
    </div>
  );
};

export default NotFoundPage;
