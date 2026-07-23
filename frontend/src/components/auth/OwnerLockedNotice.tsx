import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";

import Button from "@/components/elements/Button";
import RuneMark from "@/components/elements/RuneMark";
import PublicNavbar from "@/components/navigation/PublicNavbar";
import { postLogout } from "@/api/authAPIs";
import { BTN_TEXT, PATH_LIST, QUERY_KEYS } from "@/constants/commonConstants";
import { L } from "@/locales";

/**
 * OwnerLockedNotice is the soft-block screen shown when a signed-in account is
 * NOT the console owner (GET /console/session → is_owner:false). The console is
 * a single-admin surface: the first account to sign in claims it, and any other
 * account is let in only far enough to learn who owns it and switch — never into
 * the admin shell. Sign out returns to the login screen so a different Google
 * account can be chosen.
 */
const OwnerLockedNotice = ({ owner }: { owner?: string }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const handleSignOut = async () => {
    try {
      await postLogout(); // idempotent server-side
    } finally {
      queryClient.setQueryData([QUERY_KEYS.session], { logged_in: false });
      navigate(PATH_LIST.login);
    }
  };

  return (
    <div className="bg-background flex min-h-screen flex-col">
      <PublicNavbar />
      <main className="grid flex-1 place-items-center px-4">
        <div className="border-border bg-panel-solid flex w-100 flex-col gap-8 rounded-lg border p-7 text-center">
          <div className="flex items-center justify-center gap-2">
            <RuneMark />
            <h1 className="text-lg font-semibold">{L.auth.ownerLockedTitle}</h1>
          </div>
          <p className="text-muted-foreground text-md">
            {L.auth.ownerLockedPrefix}
            {owner ? (
              <span className="text-foreground font-medium">{owner}</span>
            ) : (
              L.auth.ownerFallback
            )}
            {L.auth.ownerLockedSuffix}
            <br />
            {L.auth.ownerLockedBody}
          </p>
          <Button
            btnText={BTN_TEXT.signOut}
            btnSize="lg"
            btnColor="mintFilled"
            handleClick={handleSignOut}
          />
        </div>
      </main>
    </div>
  );
};

export default OwnerLockedNotice;
