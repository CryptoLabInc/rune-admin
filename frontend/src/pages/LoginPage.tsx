import { useState } from "react";
import { Navigate, useSearchParams } from "react-router";

import Button from "@/components/elements/Button";
import PublicNavbar from "@/components/navigation/PublicNavbar";
import { useSessionQuery } from "@/hooks/queries/useSessionQuery";
import { postAuthStart } from "@/api/authAPIs";
import { redirectTo } from "@/utils/redirect";
import { PATH_LIST } from "@/constants/commonConstants";

/**
 * LoginPage is the console sign-in screen (SC-01). Login is delegated to
 * Runespace via a redirect: the button starts the flow and the browser leaves
 * the SPA. A `?error` query param renders the single, code-agnostic failure
 * message (API design LD8).
 */
const LoginPage = () => {
  const [params] = useSearchParams();
  const [starting, setStarting] = useState(false);
  const [failed, setFailed] = useState(params.get("error") !== null);
  const { data, isPending } = useSessionQuery();

  if (isPending) return null;
  if (data?.logged_in) return <Navigate to={PATH_LIST.teams} replace />;

  const handleLogin = async () => {
    setStarting(true);
    setFailed(false);
    try {
      const res = await postAuthStart();
      if (!res.ok) throw res;
      const { authorize_url } = (await res.json()) as {
        authorize_url: string;
      };
      redirectTo(authorize_url); // browser leaves the SPA; success path intentionally does NOT reset starting
    } catch {
      setFailed(true);
      setStarting(false);
    }
  };

  return (
    <div className="bg-background flex min-h-screen flex-col">
      <PublicNavbar />
      <main className="grid flex-1 place-items-center px-4">
        <div className="border-border bg-panel-solid w-[320px] rounded-lg border p-7 text-center">
          <h1 className="mb-4 text-lg font-semibold">Rune Console</h1>
          {failed && (
            <p role="alert" className="text-negative mb-4 text-sm">
              로그인 실패
              <br />
              <span className="text-muted-foreground">
                문제가 발생했습니다. 다시 시도해 주세요.
              </span>
            </p>
          )}
          <Button
            btnText="로그인하기"
            btnSize="lg"
            btnColor="mintFilled"
            handleClick={handleLogin}
            disabled={starting}
          />
          {!failed && (
            <p className="text-muted-foreground mt-3 text-xs">
              로그인을 위해 Runespace로 이동합니다.
              <br />
              로그인이 완료되면 관리자 페이지로 되돌아옵니다.
            </p>
          )}
        </div>
      </main>
    </div>
  );
};

export default LoginPage;
