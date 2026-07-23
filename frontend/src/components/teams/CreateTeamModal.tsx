import { useState } from "react";

import Button from "@/components/elements/Button";
import Dropdown from "@/components/elements/Dropdown";
import Input from "@/components/elements/Input";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import {
  buildTeamOptions,
  TEAM_NAME_PATTERN,
  TEAM_NAME_RULE_TEXT,
} from "@/components/teams/teamOptions";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { L } from "@/locales";
import type { TTeamTree } from "@/types/teamTypes";

interface CreateTeamModalProps {
  /** Real GET /teams/tree result — feeds the parent-team picker and the
      client-side sibling-name dup hint. */
  teams: TTeamTree;
  /** Server-mapped error copy from the last failed create attempt
      (409 TEAM_NAME_DUPLICATE and friends) — null/undefined renders
      nothing. */
  error?: string | null;
  onClose: () => void;
  onCreate: (name: string, parentId: string | null) => void;
}

/**
 * CreateTeamModal is the 새 팀 만들기 modal (SC-07): team name + optional
 * parent team; picking a parent copies its members/roles downward (R1).
 * Mount conditionally — state resets by unmounting.
 */
const CreateTeamModal = ({
  teams,
  error,
  onClose,
  onCreate,
}: CreateTeamModalProps) => {
  const [name, setName] = useState("");
  const [parentId, setParentId] = useState("");

  const trimmed = name.trim();
  const isInvalidFormat =
    trimmed.length > 0 && !TEAM_NAME_PATTERN.test(trimmed);

  const isDuplicate = teams.some(
    (team) => team.parentId === (parentId || null) && team.name === trimmed,
  );
  const canSubmit = trimmed.length > 0 && !isInvalidFormat && !isDuplicate;

  const nameError = isInvalidFormat
    ? TEAM_NAME_RULE_TEXT
    : trimmed && isDuplicate
      ? L.teams.dupName
      : undefined;

  return (
    <ModalLayout title={MODAL_TITLES.createTeam} isOpen>
      <div className="flex w-full flex-col gap-5">
        <Input
          id="create-team-name"
          labelText={L.teams.teamName}
          placeholder={L.teams.teamNamePlaceholder}
          maxLength={50}
          value={name}
          setValue={setName}
          hint={TEAM_NAME_RULE_TEXT}
          error={nameError}
        />
        <Dropdown
          label={L.teams.parentTeamOptional}
          placeholder={L.teams.selectTeam}
          options={buildTeamOptions(teams)}
          value={parentId}
          onChange={setParentId}
        />
        <Notice tone="info">
          {L.teams.parentCopyInfo1} <br />
          {L.teams.parentCopyInfo2}
        </Notice>
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
          btnText={BTN_TEXT.create}
          btnSize="md"
          btnColor="mintFilled"
          disabled={!canSubmit}
          handleClick={() => onCreate(trimmed, parentId || null)}
        />
      </div>
    </ModalLayout>
  );
};

export default CreateTeamModal;
