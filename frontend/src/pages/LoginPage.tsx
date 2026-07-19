import { useState } from "react";
import { Navigate, useSearchParams } from "react-router";

import Button from "@/components/elements/Button";
import RuneMark from "@/components/elements/RuneMark";
import PublicNavbar from "@/components/navigation/PublicNavbar";
import { useSessionQuery } from "@/hooks/queries/useSessionQuery";
import { postAuthStart } from "@/api/authAPIs";
import { redirectTo } from "@/utils/redirect";
import {
  BRAND_WORDMARK,
  BTN_TEXT,
  PATH_LIST,
} from "@/constants/commonConstants";

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
        <div className="border-border bg-panel-solid flex w-100 flex-col gap-8 rounded-lg border p-7 text-center">
          <div className="flex items-center justify-center gap-2">
            <RuneMark />
            <h1 className="text-lg font-semibold">{BRAND_WORDMARK}</h1>
          </div>
          {failed && (
            <p role="alert" className="text-negative text-md mb-4">
              <span className="text-muted-foreground">
                로그인 중 문제가 발생했습니다.
                <br />
                다시 시도해 주세요.
              </span>
            </p>
          )}
          <div className="flex flex-col gap-5">
            <Button
              btnText={BTN_TEXT.login}
              btnSize="lg"
              btnColor="mintFilled"
              handleClick={handleLogin}
              disabled={starting}
            />
            {!failed && (
              <p className="text-muted-foreground mt-3 text-sm">
                콘솔을 사용하기 위해 RUNE 계정이 필요합니다.
              </p>
            )}
          </div>
        </div>
      </main>
    </div>
  );
};

export default LoginPage;
