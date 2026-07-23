import { useState } from "react";

import Button from "@/components/elements/Button";
import Input from "@/components/elements/Input";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import {
  TEAM_NAME_PATTERN,
  TEAM_NAME_RULE_TEXT,
} from "@/components/teams/teamOptions";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { L } from "@/locales";
import type { TTeamTree } from "@/types/teamTypes";

interface RenameTeamModalProps {
  currentName: string;
  /** Team's current parentId — the sibling-name dup hint is scoped to
      teams sharing this parent, matching the server's uniqueness rule. */
  currentParentId: string | null;
  /** Real GET /teams/tree result — feeds the client-side sibling-name
      dup hint. */
  teams: TTeamTree;
  /** Server-mapped error copy from the last failed rename attempt
      (409 TEAM_NAME_DUPLICATE and friends) — null/undefined renders
      nothing. */
  error?: string | null;
  onClose: () => void;
  onRename: (name: string) => void;
}

/**
 * RenameTeamModal is the 팀 이름 변경 modal (SC-08): rename only —
 * hierarchy moves are not supported in v1. Mount conditionally.
 */
const RenameTeamModal = ({
  currentName,
  currentParentId,
  teams,
  error,
  onClose,
  onRename,
}: RenameTeamModalProps) => {
  const [name, setName] = useState(currentName);

  const trimmed = name.trim();
  const isChanged = trimmed !== currentName;
  /* Format-checked only once edited — a pre-filled legacy name that
     violates the rule shouldn't alarm before the user types. */
  const isInvalidFormat =
    isChanged && trimmed.length > 0 && !TEAM_NAME_PATTERN.test(trimmed);
  const isDuplicate =
    isChanged &&
    teams.some(
      (team) => team.parentId === currentParentId && team.name === trimmed,
    );
  const canSubmit =
    trimmed.length > 0 && isChanged && !isInvalidFormat && !isDuplicate;

  const nameError = isInvalidFormat
    ? TEAM_NAME_RULE_TEXT
    : isDuplicate
      ? L.teams.dupName
      : undefined;

  return (
    <ModalLayout title={MODAL_TITLES.renameTeam} isOpen>
      <div className="flex w-full flex-col gap-5">
        <Input
          id="rename-team-name"
          labelText={L.teams.teamName}
          maxLength={50}
          value={name}
          setValue={setName}
          hint={TEAM_NAME_RULE_TEXT}
          error={nameError}
        />
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
          btnText={BTN_TEXT.save}
          btnSize="md"
          btnColor="mintFilled"
          disabled={!canSubmit}
          handleClick={() => onRename(trimmed)}
        />
      </div>
    </ModalLayout>
  );
};

export default RenameTeamModal;
