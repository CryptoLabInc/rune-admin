import { useState } from "react";

import Button from "@/components/elements/Button";
import Dropdown from "@/components/elements/Dropdown";
import Input from "@/components/elements/Input";
import Notice from "@/components/elements/Notice";
import Radio from "@/components/elements/Radio";
import ModalLayout from "@/components/layout/ModalLayout";
import { buildTeamOptions } from "@/components/teams/teamOptions";
import { cn } from "@/utils/cn";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import type { TTeamTree } from "@/types/teamTypes";

const styles = {
  /* One card per option — the whole area is the selection surface and
     contains the option's fields (wireframe SC-09 wf-radio card). */
  optionCard:
    "border-border bg-muted-foreground/[2%] flex w-full cursor-pointer flex-col gap-3 rounded-md border p-3 transition-colors",
  optionCardActive: "border-mint/40 bg-mint/[4%]",
  /* The inner Radio sheds its own card chrome — the outer card owns it.
     items-start + label top margin: dot top and text top align. */
  optionRadio:
    "w-full items-start border-0 bg-transparent p-0 [&_input+span]:mt-0.5",
  optionFields: "flex flex-col gap-3 pl-6",
};

export type TMemoryAction = "transfer" | "purge";

interface DeleteTeamModalProps {
  teamId: string;
  teamName: string;
  /** Teams with sub-teams cannot be deleted — shows the alert instead (SC-06 state D). */
  hasChildren: boolean;
  /** Real GET /teams/tree result — feeds the transfer-target dropdown
      (excluding the team being deleted). */
  teams: TTeamTree;
  /** Server-mapped error copy from the last failed delete attempt
      (409 TEAM_HAS_CHILDREN and friends) — null/undefined renders
      nothing. */
  error?: string | null;
  onClose: () => void;
  onDelete: (action: TMemoryAction, targetTeamId?: string) => void;
}

/**
 * DeleteTeamModal is the 팀 삭제 modal (SC-09 v0.12): choose memory
 * handling — ① transfer to another team (default) or ② purge — each
 * gated by a typed confirmation that must exactly match the relevant
 * team name (case-sensitive). The unselected option's fields stay
 * visible but disabled; switching options resets the other option's
 * confirmation input. A team with sub-teams gets the blocking alert
 * variant instead. Mount conditionally.
 */
const DeleteTeamModal = ({
  teamId,
  teamName,
  hasChildren,
  teams,
  error,
  onClose,
  onDelete,
}: DeleteTeamModalProps) => {
  const [action, setAction] = useState<TMemoryAction>("transfer");
  const [targetTeamId, setTargetTeamId] = useState("");
  const [transferConfirm, setTransferConfirm] = useState("");
  const [purgeConfirm, setPurgeConfirm] = useState("");

  if (hasChildren) {
    return (
      <ModalLayout title={MODAL_TITLES.deleteTeam(teamName)} isOpen>
        <p className="text-center text-base">
          하위 팀이 있는 팀은 삭제할 수 없습니다.
          <br />
          하위 팀을 먼저 삭제한 후 다시 시도해 주세요.
        </p>
        <Button
          btnText={BTN_TEXT.close}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
      </ModalLayout>
    );
  }

  const teamOptions = buildTeamOptions(teams);
  const targetOptions = teamOptions.filter((option) => option.value !== teamId);
  const targetName =
    teamOptions.find((option) => option.value === targetTeamId)?.label ?? "";

  /* Confirmation rule: exact, case-sensitive match (SC-09 no.2/no.4). */
  const transferValid = targetTeamId !== "" && transferConfirm === targetName;
  const purgeValid = purgeConfirm === teamName;
  const canSubmit = action === "transfer" ? transferValid : purgeValid;

  const transferMismatch =
    action === "transfer" &&
    transferConfirm.length > 0 &&
    transferConfirm !== targetName;
  const purgeMismatch =
    action === "purge" && purgeConfirm.length > 0 && purgeConfirm !== teamName;

  /* Switching options resets the previous option's confirmation (no.1). */
  const selectTransfer = () => {
    setAction("transfer");
    setPurgeConfirm("");
  };
  const selectPurge = () => {
    setAction("purge");
    setTransferConfirm("");
  };

  return (
    <ModalLayout title={MODAL_TITLES.deleteTeam(teamName)} isOpen isWide>
      <div className="flex w-full flex-col gap-4">
        <Notice tone="info">
          삭제하려는 팀의 기억 처리 방식을 선택해 주세요.
        </Notice>

        {/* Option ① — transfer (default): the card is the selection area */}
        <div
          className={cn(
            styles.optionCard,
            action === "transfer" && styles.optionCardActive,
          )}
          onClick={selectTransfer}
        >
          <Radio
            name="memory-action"
            checked={action === "transfer"}
            onChange={selectTransfer}
            label="① 다른 팀으로 이전"
            desc="(기본값)"
            className={styles.optionRadio}
          />
          <div className={styles.optionFields}>
            <Dropdown
              label="이전받을 팀"
              placeholder="팀 선택"
              options={targetOptions}
              value={targetTeamId}
              onChange={setTargetTeamId}
              disabled={action !== "transfer"}
            />
            <Input
              id="delete-team-transfer-confirm"
              labelText="확인 - 타겟 팀명 입력"
              placeholder={targetName || "팀을 먼저 선택하세요"}
              maxLength={50}
              value={transferConfirm}
              setValue={setTransferConfirm}
              disabled={action !== "transfer"}
              error={
                transferMismatch ? "타겟 팀명이 일치하지 않습니다." : undefined
              }
            />
          </div>
        </div>

        {/* Option ② — purge this team's memory */}
        <div
          className={cn(
            styles.optionCard,
            action === "purge" && styles.optionCardActive,
          )}
          onClick={selectPurge}
        >
          <Radio
            name="memory-action"
            checked={action === "purge"}
            onChange={selectPurge}
            label="② 팀 내 기억 삭제"
            desc="다른 팀과 공유 중인 기억은 해당 팀에서 계속 조회할 수 있습니다."
            className={styles.optionRadio}
          />
          <div className={styles.optionFields}>
            <Input
              id="delete-team-purge-confirm"
              labelText="확인 - 삭제할 팀명 입력"
              placeholder={teamName}
              maxLength={50}
              value={purgeConfirm}
              setValue={setPurgeConfirm}
              disabled={action !== "purge"}
              error={purgeMismatch ? "팀명이 일치하지 않습니다." : undefined}
            />
          </div>
        </div>

        {error && <Notice tone="error">{error}</Notice>}
      </div>

      <div className="flex w-full gap-2">
        <Button
          btnText={BTN_TEXT.cancel}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText={BTN_TEXT.deleteTeam}
          btnSize="md"
          btnColor="redFilled"
          disabled={!canSubmit}
          handleClick={() =>
            onDelete(action, action === "transfer" ? targetTeamId : undefined)
          }
        />
      </div>
    </ModalLayout>
  );
};

export default DeleteTeamModal;
