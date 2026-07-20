import { useEffect, useState } from "react";

import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import IconSpinner from "@/components/icons/IconSpinner";
import { useUpdateMutation } from "@/hooks/mutations/useUpdateMutation";
import {
  isSystemUpdateActive,
  useUpdateQuery,
} from "@/hooks/queries/useUpdateQuery";
import { reloadPage } from "@/utils/reloadPage";
import { BTN_TEXT } from "@/constants/commonConstants";

const DISMISSED_KEY_PREFIX = "runeconsole.system-update.dismissed:";
const QUEUED_TARGET_KEY = "runeconsole.system-update.queued-target";

const dismissedKey = (targetVersion: string) =>
  `${DISMISSED_KEY_PREFIX}${targetVersion}`;

const wasDismissed = (targetVersion: string): boolean => {
  try {
    return window.sessionStorage.getItem(dismissedKey(targetVersion)) === "1";
  } catch {
    return false;
  }
};

const rememberDismissed = (targetVersion: string): void => {
  try {
    window.sessionStorage.setItem(dismissedKey(targetVersion), "1");
  } catch {
    // Storage can be unavailable in hardened browsers; hiding for this render
    // still works through local component state.
  }
};

const readQueuedTarget = (): string | null => {
  try {
    return window.sessionStorage.getItem(QUEUED_TARGET_KEY);
  } catch {
    return null;
  }
};

const rememberQueuedTarget = (targetVersion: string): void => {
  try {
    window.sessionStorage.setItem(QUEUED_TARGET_KEY, targetVersion);
  } catch {
    // Component state still guards the current page when storage is blocked.
  }
};

const forgetQueuedTarget = (): void => {
  try {
    window.sessionStorage.removeItem(QUEUED_TARGET_KEY);
  } catch {
    // Nothing else is required: local state is cleared before the reload.
  }
};

/**
 * A non-blocking update prompt. It intentionally does not reuse ModalLayout:
 * updates should be visible without a scrim, focus trap, or page scroll lock.
 */
const UpdateFloatingCard = () => {
  const { data: status } = useUpdateQuery();
  const updateMutation = useUpdateMutation();
  const [dismissedTarget, setDismissedTarget] = useState<string | null>(null);
  const [queuedTarget, setQueuedTarget] = useState(readQueuedTarget);

  useEffect(() => {
    if (
      queuedTarget ||
      !status?.targetVersion ||
      !isSystemUpdateActive(status.state)
    ) {
      return;
    }

    // Another tab may have queued the update, or this tab may have opened
    // while it was already running. Adopt that server-pinned target so this
    // copy of the embedded SPA also reloads when the new binary is healthy.
    rememberQueuedTarget(status.targetVersion);
    setQueuedTarget(status.targetVersion);
  }, [queuedTarget, status]);

  useEffect(() => {
    if (!status || !queuedTarget) return;
    const installed = status.currentVersion === queuedTarget;
    const sameJobSucceeded =
      status.targetVersion === queuedTarget && status.state === "succeeded";
    if (!installed && !sameJobSucceeded) return;

    // Clear first: the next SPA must not enter a reload loop if the helper
    // continues reporting its terminal succeeded state.
    forgetQueuedTarget();
    setQueuedTarget(null);
    reloadPage();
  }, [queuedTarget, status]);

  if (!status?.targetVersion) return null;

  const targetVersion = status.targetVersion;
  const serverActive = isSystemUpdateActive(status.state);
  const busy = serverActive || updateMutation.isPending;
  const failed = !busy && (status.state === "failed" || updateMutation.isError);
  const initiallyEligible = status.capable && status.updateAvailable;

  if (!busy && !initiallyEligible) return null;

  const hiddenForTarget =
    !busy &&
    !failed &&
    (dismissedTarget === targetVersion || wasDismissed(targetVersion));
  if (hiddenForTarget) return null;

  const dismiss = () => {
    rememberDismissed(targetVersion);
    setDismissedTarget(targetVersion);
  };

  const requestUpdate = () => {
    updateMutation.mutate(targetVersion, {
      onSuccess: () => {
        rememberQueuedTarget(targetVersion);
        setQueuedTarget(targetVersion);
      },
    });
  };

  const title = busy
    ? "콘솔을 업데이트하는 중입니다"
    : failed
      ? "업데이트에 실패했습니다"
      : "새 버전이 출시되었습니다";

  return (
    <aside
      role="dialog"
      aria-modal="false"
      aria-labelledby="system-update-title"
      className="border-border bg-panel-solid fixed top-20 right-6 z-60 flex w-80 flex-col gap-4 rounded-xl border p-5 shadow-[0_24px_48px_-20px_rgba(0,0,0,0.65)]"
    >
      <div className="min-w-0">
        <h2 id="system-update-title" className="text-base font-semibold">
          {title}
        </h2>
        <p className="text-muted-foreground mt-1 text-sm">
          {status.currentVersion} → {targetVersion}
        </p>
      </div>

      {busy ? (
        <div
          role="status"
          className="text-muted-foreground flex items-center gap-2 text-sm"
        >
          <IconSpinner className="text-mint size-5 flex-none" />
          <span>
            {status.state === "running"
              ? "백업 및 업데이트를 진행하고 있습니다…"
              : "업데이트를 준비하고 있습니다…"}
          </span>
        </div>
      ) : failed ? (
        <Notice tone="error">
          업데이트를 완료하지 못했습니다. 상태를 확인한 뒤 다시 시도해 주세요.
        </Notice>
      ) : (
        <Notice>
          콘솔이 재시작되는 동안 RUNE 사용이 일시적으로 중단될 수 있습니다.
        </Notice>
      )}

      {!busy &&
        (failed ? (
          <Button
            btnText={BTN_TEXT.retry}
            btnSize="sm"
            btnColor="mintFilled"
            disabled={!status.capable}
            handleClick={requestUpdate}
          />
        ) : (
          <div className="flex gap-2">
            <Button
              btnText={BTN_TEXT.later}
              btnSize="sm"
              btnColor="grayOutline"
              handleClick={dismiss}
            />
            <Button
              btnText={BTN_TEXT.update}
              btnSize="sm"
              btnColor="mintFilled"
              handleClick={requestUpdate}
            />
          </div>
        ))}
    </aside>
  );
};

export default UpdateFloatingCard;
