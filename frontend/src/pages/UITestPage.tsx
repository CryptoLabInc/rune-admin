import { Fragment, useEffect, useState } from "react";

import MembershipRow from "@/components/drawer/MembershipRow";
import Badge from "@/components/elements/Badge";
import Button from "@/components/elements/Button";
import Checkbox from "@/components/elements/Checkbox";
import Dropdown from "@/components/elements/Dropdown";
import Feedback from "@/components/elements/Feedback";
import Input from "@/components/elements/Input";
import MemberStatus from "@/components/elements/MemberStatus";
import Notice from "@/components/elements/Notice";
import Pagination from "@/components/elements/Pagination";
import Radio from "@/components/elements/Radio";
import SearchInput from "@/components/elements/SearchInput";
import WorkspaceStatus from "@/components/elements/WorkspaceStatus";
import TextButton from "@/components/elements/TextButton";
import DrawerLayout from "@/components/layout/DrawerLayout";
import ModalLayout from "@/components/layout/ModalLayout";
import Table from "@/components/table/Table";
import TableCell from "@/components/table/TableCell";
import TableErrorRow from "@/components/table/TableErrorRow";
import TableFoot from "@/components/table/TableFoot";
import TableHead from "@/components/table/TableHead";
import TableHeaderCell from "@/components/table/TableHeaderCell";
import TableRow from "@/components/table/TableRow";
import TableToolbar from "@/components/table/TableToolbar";
import TeamTree from "@/components/tree/TeamTree";
import TeamTreeFooter from "@/components/tree/TeamTreeFooter";
import { cn } from "@/utils/cn";
import { BTN_TEXT } from "@/constants/commonConstants";
import {
  BTN_HOT_VAR,
  MEMBER_STATUS_VAR,
  WORKSPACE_STATUS_VAR,
} from "@/constants/styleConstants";
import type {
  TMemberStatus,
  TWorkspaceStatus,
  TTeamNode,
} from "@/types/commonTypes";
import type { TBTNColor } from "@/types/styleTypes";
import { useToastStore } from "@/stores/toastStore";

type TUITestModal = "alert" | "confirm" | "wide" | "scroll" | null;

/* Button theme roles (2026-07-13): filled primary/secondary/warning +
   outline primary/secondary/danger. Disabled is the shared gray fill. */
const BTN_THEMES: { color: TBTNColor; role: string; text: string }[] = [
  { color: "mintFilled", role: "filled · primary", text: "초대하기" },
  { color: "grayFilled", role: "filled · secondary", text: "나중에" },
  { color: "redFilled", role: "filled · warning", text: "팀 삭제" },
  { color: "mintOutline", role: "outline · primary", text: "새 팀 만들기" },
  { color: "grayOutline", role: "outline · secondary", text: "닫기" },
  { color: "redOutline", role: "outline · danger", text: "멤버 삭제" },
];

const ROLE_OPTIONS = [
  { value: "edit", label: "edit" },
  { value: "write", label: "write" },
  { value: "read", label: "read" },
];

const TEAM_OPTIONS = [
  { value: "platform", label: "플랫폼팀" },
  { value: "fe", label: "프론트엔드", depth: 1 },
  { value: "be", label: "백엔드", depth: 1, disabled: true },
  { value: "infra", label: "인프라", depth: 2 },
];

/* Table demo fixture — the "changed" row carries an unsaved role change,
   and the error row is inserted under the last account. */
const TABLE_ROWS: {
  account: string;
  status: TMemberStatus;
  teams: string;
  role: string;
  invitedAt: string;
  changed?: boolean;
}[] = [
  {
    account: "k@corp.com",
    status: "online",
    teams: "A · edit",
    role: "edit",
    invitedAt: "2026-07-06 09:00",
  },
  {
    account: "a@corp.com",
    status: "pending",
    teams: "A · read",
    role: "read",
    invitedAt: "2026-07-05 18:20",
  },
  {
    account: "b@corp.com",
    status: "online",
    teams: "b · write +1",
    role: "write",
    invitedAt: "2026-07-02 11:40",
    changed: true,
  },
  {
    account: "c@corp.com",
    status: "session-expired",
    teams: "c · read",
    role: "read",
    invitedAt: "2026-07-01 10:10",
  },
];

const SESSION_ROWS = [
  {
    account: "k@corp.com",
    issuedAt: "2026-07-01 09:00",
    connectedAt: "2026-07-06 09:00",
  },
  { account: "a@corp.com", issuedAt: "2026-07-05 18:20", connectedAt: "" },
];

const TEAM_FIXTURE: TTeamNode[] = [
  {
    id: "platform",
    name: "Platform",
    members: 18,
    children: [
      { id: "fe", name: "Frontend", members: 6 },
      {
        id: "infra",
        name: "Infrastructure",
        members: 7,
        children: [{ id: "zerotrust", name: "Zero Trust", members: 3 }],
      },
    ],
  },
  {
    id: "research",
    name: "Research",
    members: 9,
    children: [{ id: "crypto", name: "Cryptography", members: 5 }],
  },
  { id: "ops", name: "Operations", members: 11 },
];

const MEMBERSHIP_FIXTURE = [
  { id: "platform", name: "Platform", baseRole: "edit", checked: true },
  { id: "infra", name: "Infrastructure", baseRole: "write", checked: false },
  { id: "security", name: "Security", baseRole: "read", checked: false },
];

/** Section groups one element with a heading and a spec note. */
const Section = ({
  title,
  note,
  children,
}: {
  title: string;
  note?: string;
  children: React.ReactNode;
}) => (
  <section className="border-border flex flex-col gap-3 border-t pt-6">
    <div>
      <h2 className="text-lg font-semibold">{title}</h2>
      {note && <p className="text-faint text-xs">{note}</p>}
    </div>
    {children}
  </section>
);

/** Specimen frames one state with a mono label (UIKIT anatomy grammar). */
const Specimen = ({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) => (
  <figure className="border-border-strong m-0 flex min-w-0 flex-col overflow-hidden rounded-md border border-dashed">
    <figcaption className="border-border text-tag text-muted-foreground border-b border-dashed px-3 py-2 font-mono tracking-[0.07em]">
      {label}
    </figcaption>
    <div className="flex min-h-16 grow items-center justify-center p-4">
      {children}
    </div>
  </figure>
);

const specimenGrid = "grid grid-cols-4 gap-3";

/**
 * UITestPage is a dev-only showcase for shared UI components (not linked
 * from the navigation). Mirrors the UIKIT specimen grammar so the real
 * implementations can be compared against the UIKIT showcase side by side.
 */
const UITestPage = () => {
  const [openModal, setOpenModal] = useState<TUITestModal>(null);
  const [inputValue, setInputValue] = useState("");
  const [errorValue, setErrorValue] = useState("console.acme.internal:443x");
  const [search, setSearch] = useState("");
  const [role, setRole] = useState("");
  const [team, setTeam] = useState("");
  const [checkOn, setCheckOn] = useState(true);
  const [radioPick, setRadioPick] = useState("keep");
  const [page, setPage] = useState(1);
  const [tableSel, setTableSel] = useState<string[]>(["a@corp.com"]);
  const [tableSearch, setTableSearch] = useState("");
  const [tablePage, setTablePage] = useState(1);
  const [treeQuery, setTreeQuery] = useState("");
  const [treeSelected, setTreeSelected] = useState<TTeamNode>(TEAM_FIXTURE[0]);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [memberships, setMemberships] = useState(
    MEMBERSHIP_FIXTURE.map((m) => ({ ...m, role: m.baseRole })),
  );
  const showToast = useToastStore((state) => state.showToast);
  const [lightMode, setLightMode] = useState(false);

  /* Light-theme preview — the class sits on <html> so portaled
     overlays (modal/drawer/toast/menus) re-theme too. Removed on
     unmount so other pages stay on the default dark theme. */
  useEffect(() => {
    document.documentElement.classList.toggle("light", lightMode);
    return () => document.documentElement.classList.remove("light");
  }, [lightMode]);

  const closeModal = () => setOpenModal(null);
  const allSelected = tableSel.length === TABLE_ROWS.length;
  const toggleAll = (checked: boolean) =>
    setTableSel(checked ? TABLE_ROWS.map((r) => r.account) : []);
  const toggleRow = (account: string, checked: boolean) =>
    setTableSel((prev) =>
      checked ? [...prev, account] : prev.filter((a) => a !== account),
    );

  return (
    <div className="flex flex-col gap-8 pb-16">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-[-0.02em]">UI Test</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            공용 UI 컴포넌트 확인용 페이지 — 네비게이션에 노출되지 않습니다.
            UIKIT 쇼케이스와 나란히 비교하세요.
          </p>
        </div>
        <Checkbox
          checked={lightMode}
          onChange={setLightMode}
          label="라이트 모드 (실험)"
        />
      </div>

      <Section
        title="Typography"
        note="텍스트 롤 — 정의된 클래스 조합 외 임의 크기·색 조합 금지 (타이포 설계 §1.2)"
      >
        <h3 className="text-tag text-faint font-mono tracking-[0.07em]">
          HEADING
        </h3>
        <div className="flex flex-col gap-3">
          <p className="text-3xl font-bold tracking-[-0.02em]">
            h1 페이지 타이틀 — 24px / 700
          </p>
          <p className="text-xl font-semibold">
            h2 패널·모달 타이틀 — 18px / 600
          </p>
          <p className="text-md font-semibold">
            h3 카드·툴바 타이틀 — 14px / 600
          </p>
          <p className="text-tag text-mint font-mono font-medium tracking-[0.12em] uppercase">
            Session History — 키커 10px mono
          </p>
        </div>
        <h3 className="text-tag text-faint font-mono tracking-[0.07em]">
          TEXT
        </h3>
        <div className="flex flex-col gap-3">
          <p className="text-lg">
            본문 프로즈 16px — 팀을 생성하면 멤버와 기억(memory)을 관리할 수
            있습니다.
          </p>
          <p className="text-base">
            UI 본문 14px — 다음 멤버의 role을 변경합니다.
          </p>
          <p className="text-muted-foreground text-sm">
            보조 텍스트 12px — 상위 팀: A · 멤버 3명 · 생성일 2026-07-01
          </p>
          <p className="text-sm font-semibold">폼 라벨 12px / 600 — 팀 이름</p>
          <p className="text-faint text-xs">
            캡션·힌트 10px — 숫자·한글·영어와 - _ 만 사용할 수 있습니다.
          </p>
          <p className="text-negative text-xs">
            에러 10px — 같은 위치에 동일한 이름의 팀이 있습니다.
          </p>
          <p className="text-warning text-xs">
            경고 10px — 이미 초대된 사용자입니다.
          </p>
          <p className="text-muted-foreground font-mono text-xs whitespace-nowrap">
            mono 데이터 10px mono / 400 — 2026-07-06 09:00
          </p>
        </div>
        <h3 className="text-tag text-faint font-mono tracking-[0.07em]">
          COLOR HIERARCHY
        </h3>
        <div className="flex flex-col gap-2">
          <p className="text-base">
            1 foreground — 주 텍스트 (헤딩·본문·입력값)
          </p>
          <p className="text-subtle text-base">
            2 subtle — 긴 설명·카드 본문 (주 본문보다 한 단계 낮은 프로즈)
          </p>
          <p className="text-muted-foreground text-base">
            3 muted-foreground — 보조 (테이블 셀·옵션)
          </p>
          <p className="text-faint text-base">
            4 faint — 캡션·힌트·placeholder (12px 초과 본문 금지)
          </p>
        </div>
      </Section>

      <Section
        title="Button"
        note="폼 컨트롤 — w-full 내장, 부모가 폭 제한. btnColor 6종 / btnSize 4종(높이 24·32·36·42px) · disabled는 전 테마 공통 gray fill(요청 진행 중에도 disabled 전달 — 별도 loading UI 없음)"
      >
        <h3 className="text-tag text-faint font-mono tracking-[0.07em]">
          THEME × STATE — btnSize=md 고정 · hover = focus-visible 동일 UI(박제)
        </h3>
        {BTN_THEMES.map((theme) => (
          <div key={theme.color} className="grid grid-cols-3 gap-3">
            <Specimen label={`${theme.color} — ${theme.role} · default`}>
              <div className="w-[160px]">
                <Button
                  btnText={theme.text}
                  btnSize="md"
                  btnColor={theme.color}
                />
              </div>
            </Specimen>
            <Specimen label=":hover / :focus-visible">
              <div className="w-[160px]">
                <Button
                  btnText={theme.text}
                  btnSize="md"
                  btnColor={theme.color}
                  className={BTN_HOT_VAR[theme.color]}
                />
              </div>
            </Specimen>
            <Specimen label="disabled">
              <div className="w-[160px]">
                <Button
                  btnText={theme.text}
                  btnSize="md"
                  btnColor={theme.color}
                  disabled
                />
              </div>
            </Specimen>
          </div>
        ))}
        <h3 className="text-tag text-faint font-mono tracking-[0.07em]">
          SIZE — btnColor=mintFilled 고정
        </h3>
        <div className={specimenGrid}>
          <Specimen label="xs — 24px">
            <div className="w-[100px]">
              <Button
                btnText={BTN_TEXT.change}
                btnSize="xs"
                btnColor="mintFilled"
              />
            </div>
          </Specimen>
          <Specimen label="sm — 32px">
            <div className="w-[120px]">
              <Button
                btnText={BTN_TEXT.change}
                btnSize="sm"
                btnColor="mintFilled"
              />
            </div>
          </Specimen>
          <Specimen label="md — 36px">
            <div className="w-[160px]">
              <Button
                btnText={BTN_TEXT.invite}
                btnSize="md"
                btnColor="mintFilled"
              />
            </div>
          </Specimen>
          <Specimen label="lg — 42px">
            <div className="w-[200px]">
              <Button
                btnText={BTN_TEXT.getStarted}
                btnSize="lg"
                btnColor="mintFilled"
              />
            </div>
          </Specimen>
        </div>
      </Section>

      <Section title="TextButton" note="조용한 액션 — 회원탈퇴·행 단위 삭제">
        <div className={specimenGrid}>
          <Specimen label="gray">
            <TextButton btnText="회원탈퇴" />
          </Specimen>
          <Specimen label="red">
            <TextButton btnText={BTN_TEXT.delete} tone="red" />
          </Specimen>
          <Specimen label="disabled">
            <TextButton btnText={BTN_TEXT.delete} tone="red" disabled />
          </Specimen>
        </div>
      </Section>

      <Section
        title="Input"
        note="라벨+인풋+힌트/에러 한 단위 — 에러가 힌트 대체"
      >
        <div className="grid max-w-[530px] grid-cols-1 gap-4">
          <Input
            id="ui-team-name"
            labelText="팀 이름"
            placeholder="새 팀 이름"
            maxLength={50}
            hint="숫자·한글·영어와 - _ 만 사용할 수 있습니다."
            value={inputValue}
            setValue={setInputValue}
          />
          <Input
            id="ui-console-addr"
            labelText="Console 주소"
            error="포트 형식이 올바르지 않습니다 — 예: console.acme.internal:8200"
            value={errorValue}
            setValue={setErrorValue}
          />
          <Input
            id="ui-invite-code"
            labelText="초대 코드"
            placeholder="발급 대기 중"
            hint="관리자가 발급하면 활성화됩니다."
            disabled
            value=""
            setValue={() => {}}
          />
        </div>
      </Section>

      <Section
        title="SearchInput"
        note="⌕ + 지우기 — 팀 검색 max 50 / 계정 검색 max 100"
      >
        <div className="grid max-w-[320px] grid-cols-1 gap-3">
          <SearchInput
            value={search}
            onChange={setSearch}
            placeholder="팀 검색"
            maxLength={50}
          />
          <SearchInput value="acme" onChange={() => {}} disabled />
        </div>
      </Section>

      <Section
        title="Dropdown"
        note="커스텀 리스트박스 — portal·flip·키보드 내비. depth 들여쓰기(팀 트리)"
      >
        <div className="grid max-w-[530px] grid-cols-1 gap-4">
          <Dropdown
            label="role"
            placeholder="role 선택"
            options={ROLE_OPTIONS}
            value={role}
            onChange={setRole}
          />
          <Dropdown
            label="팀 선택"
            placeholder="팀 선택"
            options={TEAM_OPTIONS}
            value={team}
            onChange={setTeam}
            hint="이미 초대된 팀은 선택할 수 없습니다."
          />
          <Dropdown
            label="role (error)"
            placeholder="role 선택"
            options={ROLE_OPTIONS}
            error="role을 선택해 주세요."
          />
          <Dropdown
            label="disabled"
            placeholder="role 선택"
            options={ROLE_OPTIONS}
            disabled
          />
          <Dropdown
            label="sm — 32px / 12px (툴바·테이블 셀)"
            placeholder="role 선택"
            options={ROLE_OPTIONS}
            size="sm"
          />
        </div>
      </Section>

      <Section title="Checkbox / Radio">
        <div className="flex flex-wrap items-center gap-x-7 gap-y-3">
          <Checkbox checked={checkOn} onChange={setCheckOn} label="선택" />
          <Checkbox checked disabled onChange={() => {}} label="disabled" />
        </div>
        <div className="flex flex-wrap gap-3">
          <Radio
            checked={radioPick === "keep"}
            onChange={() => setRadioPick("keep")}
            name="ui-del"
            label="팀만 삭제"
            desc="기억은 상위 팀으로 유지됩니다."
          />
          <Radio
            checked={radioPick === "purge"}
            onChange={() => setRadioPick("purge")}
            name="ui-del"
            label="팀 내 기억 삭제"
            desc="팀에 축적된 기억을 함께 삭제합니다."
          />
        </div>
      </Section>

      <Section title="Badge" note="0 이하 미렌더 · max 초과 99+ 클램프">
        <div className="flex flex-wrap items-center gap-4">
          <Badge value={4} />
          <Badge value={120} />
          <Badge value={4} tone="neutral" />
          <span className="text-faint text-xs">value=0 → (미렌더)</span>
          <Badge value={0} />
        </div>
      </Section>

      <Section title="MemberStatus / WorkspaceStatus" note="시맨틱 컬러 = 상태">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
          {(Object.keys(MEMBER_STATUS_VAR) as TMemberStatus[]).map((s) => (
            <MemberStatus key={s} status={s} />
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
          {(Object.keys(WORKSPACE_STATUS_VAR) as TWorkspaceStatus[]).map((s) => (
            <WorkspaceStatus key={s} status={s} />
          ))}
        </div>
      </Section>

      <Section title="Pagination" note="숫자형 — 현재 페이지 mint 채움 (T2)">
        <Pagination page={page} totalPages={5} onChange={setPage} />
      </Section>

      <Section
        title="Table"
        note="프레임·툴바·헤더·행 상태(hover/selected/changed/error)·푸터 — empty 상태는 2차-B Feedback 이식 후 추가"
      >
        <Table
          toolbar={
            <TableToolbar
              title="사용자"
              count={23}
              selectedCount={tableSel.length}
            >
              <SearchInput
                className="w-[200px]"
                value={tableSearch}
                onChange={setTableSearch}
                placeholder="계정 검색"
                maxLength={100}
              />
              <Button
                btnText="멤버 초대"
                btnSize="sm"
                btnColor="mintFilled"
                className="w-fit"
              />
            </TableToolbar>
          }
          foot={
            <TableFoot info="총 23명 · 10명/페이지">
              <Pagination
                page={tablePage}
                totalPages={3}
                onChange={setTablePage}
              />
            </TableFoot>
          }
        >
          <TableHead>
            <TableHeaderCell className="w-8 pr-1">
              <Checkbox
                checked={allSelected}
                onChange={toggleAll}
                ariaLabel="전체 선택"
              />
            </TableHeaderCell>
            <TableHeaderCell>account</TableHeaderCell>
            <TableHeaderCell>status</TableHeaderCell>
            <TableHeaderCell>teams (role)</TableHeaderCell>
            <TableHeaderCell>role</TableHeaderCell>
            <TableHeaderCell>최근 초대 코드 발송</TableHeaderCell>
          </TableHead>
          <tbody>
            {TABLE_ROWS.map((r) => (
              <Fragment key={r.account}>
                <TableRow
                  selected={tableSel.includes(r.account)}
                  changed={r.changed}
                >
                  <TableCell className="w-8 pr-1">
                    <Checkbox
                      checked={tableSel.includes(r.account)}
                      onChange={(checked) => toggleRow(r.account, checked)}
                      ariaLabel={`${r.account} 선택`}
                    />
                  </TableCell>
                  <TableCell>
                    {/* account is 14px in role-editable tables, 12px in
                        read-only ones (session-history variant below) */}
                    <button
                      type="button"
                      className="text-foreground hover:text-mint cursor-pointer text-base font-medium"
                    >
                      {r.account}
                    </button>
                  </TableCell>
                  <TableCell>
                    <MemberStatus status={r.status} />
                  </TableCell>
                  <TableCell className="max-w-[180px]">
                    <span className="block truncate" title={r.teams}>
                      {r.teams}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Dropdown
                      options={ROLE_OPTIONS}
                      defaultValue={r.role}
                      size="sm"
                      ariaLabel={`${r.account} role`}
                      className="w-[104px]"
                    />
                  </TableCell>
                  <TableCell className="text-faint font-mono text-xs">
                    {r.invitedAt}
                  </TableCell>
                </TableRow>
                {r.account === "c@corp.com" && (
                  <TableErrorRow
                    colSpan={6}
                    message="요청을 처리하지 못했습니다. 다시 시도해 주세요."
                  />
                )}
              </Fragment>
            ))}
          </tbody>
        </Table>

        <h3 className="text-tag text-faint font-mono tracking-[0.07em]">
          SESSION HISTORY VARIANT — 읽기 전용 · hover 반응 없음 · 시간 없음 =
          "—"
        </h3>
        <Table
          foot={
            <TableFoot info="초대 코드 값은 이메일로만 전달되며 콘솔에는 표시하지 않습니다.">
              <Pagination page={1} totalPages={1} onChange={() => {}} />
            </TableFoot>
          }
        >
          <TableHead>
            <TableHeaderCell>account</TableHeaderCell>
            <TableHeaderCell>발급 시간</TableHeaderCell>
            <TableHeaderCell>최근 접속 시간</TableHeaderCell>
          </TableHead>
          <tbody>
            {SESSION_ROWS.map((r) => (
              <TableRow key={r.account} hoverable={false}>
                <TableCell>{r.account}</TableCell>
                <TableCell className="text-muted-foreground font-mono text-xs">
                  {r.issuedAt}
                </TableCell>
                <TableCell
                  className={cn(
                    "font-mono text-xs",
                    r.connectedAt ? "text-muted-foreground" : "text-faint",
                  )}
                >
                  {r.connectedAt || "—"}
                </TableCell>
              </TableRow>
            ))}
          </tbody>
        </Table>
      </Section>

      <Section
        title="Feedback"
        note="빈 상태·로딩·조회 실패 패널 — 테이블 empty 뷰, SC-06 C·SC-11 C·SC-16 B"
      >
        <div className="flex max-w-[640px] flex-col gap-3">
          <Feedback
            state="empty"
            title="새로운 팀을 만들어 주세요."
            description="첫 팀은 조직 위계의 루트가 됩니다."
            action={
              <Button
                btnText={BTN_TEXT.createTeam}
                btnSize="sm"
                btnColor="mintFilled"
                className="w-fit"
              />
            }
          />
          <Feedback
            state="loading"
            title="팀을 불러오고 있습니다."
            description="위계와 멤버 정보를 함께 확인합니다."
          />
          <Feedback
            state="error"
            title="팀 정보를 불러올 수 없습니다."
            description="새로고침 후 다시 시도해 주세요."
            action={
              <Button
                btnText={BTN_TEXT.refresh}
                btnSize="sm"
                btnColor="grayOutline"
                className="w-fit"
              />
            }
          />
        </div>
      </Section>

      <Section
        title="Notice"
        note="문서 흐름 안의 인라인 알림 배너 — 토스트 아님. SC-10 모달 메시지란·폼 안내"
      >
        <div className="flex max-w-[530px] flex-col gap-3">
          <Notice>
            팀의 기억은 <b>Research</b>로 이전됩니다. 멤버십은 함께 이동하지
            않습니다.
          </Notice>
          <Notice tone="success">변경사항이 저장되었습니다.</Notice>
          <Notice tone="error">
            멤버 추가에 실패했습니다. 다시 시도해 주세요.
          </Notice>
        </div>
      </Section>

      <Section
        title="TeamTree"
        note="SC-06 팀 위계 — 토글(−/+)·18px/레벨 들여쓰기·검색(조상 유지)·선택 푸터. 프레임·헤더는 화면 조립"
      >
        <div className="max-w-[420px] overflow-hidden rounded-lg border bg-[color:color-mix(in_srgb,color-mix(in_srgb,var(--color-panel-solid)_60%,var(--color-well))_55%,transparent)]">
          <div className="flex items-center justify-between border-b px-4 py-4">
            <div className="flex items-center gap-2">
              <b className="text-md font-semibold">팀</b>
              <span className="text-faint font-mono text-xs">총 7개</span>
            </div>
            <Button
              btnText="+ 새 팀 만들기"
              btnSize="sm"
              btnColor="grayOutline"
              className="w-fit"
            />
          </div>
          <div className="m-3">
            <SearchInput
              value={treeQuery}
              onChange={setTreeQuery}
              placeholder="팀 검색"
              maxLength={50}
            />
          </div>
          <TeamTree
            teams={TEAM_FIXTURE}
            query={treeQuery}
            selectedId={treeSelected.id}
            onSelect={setTreeSelected}
            defaultExpandedIds={["platform"]}
          />
          <TeamTreeFooter node={treeSelected} />
        </div>
      </Section>

      <Section
        title="DrawerLayout"
        note="SC-13 멤버 상세 — 우측 슬라이드 472px · 스크림 클릭/버튼으로 닫힘(✕ 없음) · MembershipRow"
      >
        <Button
          btnText="멤버 상세 열기"
          btnSize="sm"
          btnColor="grayOutline"
          className="w-fit"
          handleClick={() => setDrawerOpen(true)}
        />
        <DrawerLayout
          isOpen={drawerOpen}
          title="nia@cryptolab.dev"
          eyebrow="MEMBER DETAIL"
          subtitle="최근 접속 2026-07-10 13:08"
          onClose={() => setDrawerOpen(false)}
          footer={
            <Button
              btnText={BTN_TEXT.close}
              btnSize="md"
              btnColor="grayOutline"
              handleClick={() => setDrawerOpen(false)}
            />
          }
        >
          <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-[18px]">
            <MemberStatus status="online" />
            <span className="text-faint font-mono text-xs">
              teams: Platform (edit) +2
            </span>
          </div>
          <section className="mt-5">
            <div className="mb-2 flex items-center justify-between">
              <b className="text-sm">소속 팀</b>
              <span className="text-accent-blue text-tag font-mono">
                {memberships.filter((m) => m.checked).length} selected
              </span>
            </div>
            {/* Select-all head row, aligned to the account-table pattern
                (two-state per T1 — no indeterminate). */}
            <div className="grid grid-cols-[22px_1fr_88px] items-center gap-2 py-1.5">
              <Checkbox
                checked={memberships.every((m) => m.checked)}
                onChange={(checked) =>
                  setMemberships((prev) => prev.map((x) => ({ ...x, checked })))
                }
                ariaLabel="전체선택"
              />
              <span className="text-tag text-faint font-mono tracking-[0.08em]">
                팀
              </span>
              <span className="text-tag text-faint font-mono tracking-[0.08em]">
                role
              </span>
            </div>
            {memberships.map((m) => (
              <MembershipRow
                key={m.id}
                name={m.name}
                role={m.role}
                roleOptions={ROLE_OPTIONS}
                checked={m.checked}
                changed={m.role !== m.baseRole}
                onCheck={(checked) =>
                  setMemberships((prev) =>
                    prev.map((x) => (x.id === m.id ? { ...x, checked } : x)),
                  )
                }
                onRoleChange={(role) =>
                  setMemberships((prev) =>
                    prev.map((x) => (x.id === m.id ? { ...x, role } : x)),
                  )
                }
              />
            ))}
          </section>
          <div className="mt-3 flex justify-end border-t pt-3">
            <Button
              btnText={BTN_TEXT.updateChanges}
              btnSize="sm"
              btnColor="mintFilled"
              className="w-fit"
              disabled={!memberships.some((m) => m.role !== m.baseRole)}
              handleClick={() => setDrawerOpen(false)}
            />
          </div>
          <div className="mt-5 grid grid-cols-2 gap-2">
            <Button
              btnText={BTN_TEXT.resendInvitationCode}
              btnSize="sm"
              btnColor="grayOutline"
            />
            <Button
              btnText={BTN_TEXT.cancelInvitation}
              btnSize="sm"
              btnColor="grayFilled"
            />
            <Button
              btnText={BTN_TEXT.deactivateSession}
              btnSize="sm"
              btnColor="grayOutline"
            />
            <TextButton
              btnText={BTN_TEXT.deleteMember}
              tone="red"
              className="justify-self-center"
            />
          </div>
        </DrawerLayout>
      </Section>

      <Section
        title="Toast"
        note="우상단 고정 · 우→좌 인 · 2초 유지 · 우로 아웃 (화면설계서 규격 — UIKIT에 없음)"
      >
        <div className="flex flex-wrap gap-2">
          <Button
            btnText="info 토스트"
            btnSize="sm"
            btnColor="grayOutline"
            className="w-fit"
            handleClick={() => showToast("초대 코드를 재전송했습니다.")}
          />
          <Button
            btnText="success 토스트"
            btnSize="sm"
            btnColor="grayOutline"
            className="w-fit"
            handleClick={() =>
              showToast("변경사항이 저장되었습니다.", "success")
            }
          />
          <Button
            btnText="error 토스트"
            btnSize="sm"
            btnColor="grayOutline"
            className="w-fit"
            handleClick={() =>
              showToast(
                "권한 변경에 실패했습니다. 다시 시도해 주세요.",
                "error",
              )
            }
          />
        </div>
      </Section>

      <Section
        title="ModalLayout"
        note="규격 C20: default 500px / wide 640px · min-h 280px · max-h 80vh(내부 스크롤) · 스크림 클릭으로 닫히지 않음"
      >
        <div className="flex max-w-[530px] flex-wrap gap-2 [&>div]:w-fit [&>div>button]:w-fit [&>div>button]:px-3">
          <Button
            btnText="알림형 (1버튼)"
            btnSize="sm"
            btnColor="grayOutline"
            handleClick={() => setOpenModal("alert")}
          />
          <Button
            btnText="확인형 (2버튼)"
            btnSize="sm"
            btnColor="grayOutline"
            handleClick={() => setOpenModal("confirm")}
          />
          <Button
            btnText="wide (640px)"
            btnSize="sm"
            btnColor="grayOutline"
            handleClick={() => setOpenModal("wide")}
          />
          <Button
            btnText="내부 스크롤"
            btnSize="sm"
            btnColor="grayOutline"
            handleClick={() => setOpenModal("scroll")}
          />
        </div>
      </Section>

      {openModal === "alert" && (
        <ModalLayout title="저장소 삭제" isOpen>
          <p className="text-center text-base">저장소가 삭제되었습니다.</p>
          <Button
            btnText={BTN_TEXT.close}
            btnSize="md"
            btnColor="mintFilled"
            handleClick={closeModal}
          />
        </ModalLayout>
      )}

      {openModal === "confirm" && (
        <ModalLayout title="role 변경" isOpen>
          <p className="text-center text-base">
            다음 멤버의 role을 변경합니다: m@corp.com (edit → read)
          </p>
          <div className="flex w-full items-center gap-4">
            <Button
              btnText={BTN_TEXT.close}
              btnSize="md"
              btnColor="grayOutline"
              handleClick={closeModal}
            />
            <Button
              btnText={BTN_TEXT.change}
              btnSize="md"
              btnColor="mintFilled"
              handleClick={closeModal}
            />
          </div>
        </ModalLayout>
      )}

      {openModal === "wide" && (
        <ModalLayout title="멤버 초대" isOpen isWide>
          <div className="flex flex-col gap-4">
            <Input
              id="ui-modal-email"
              labelText="이메일 (account)"
              placeholder="user@corp.com"
              value={inputValue}
              setValue={setInputValue}
            />
            <p className="text-muted-foreground text-sm">
              wide(640px) 변형 — 폼 필드는 full-width로 배치됩니다.
            </p>
          </div>
          <div className="flex w-full items-center gap-4">
            <Button
              btnText={BTN_TEXT.close}
              btnSize="md"
              btnColor="grayOutline"
              handleClick={closeModal}
            />
            <Button
              btnText={BTN_TEXT.invite}
              btnSize="md"
              btnColor="mintFilled"
              handleClick={closeModal}
            />
          </div>
        </ModalLayout>
      )}

      {openModal === "scroll" && (
        <ModalLayout title="멤버 삭제 (30명)" isOpen>
          <div className="flex flex-col gap-2">
            {Array.from({ length: 30 }, (_, i) => (
              <p key={i} className="rounded-sm border px-3 py-2 text-sm">
                member-{i + 1}@corp.com — 팀 A (edit)
              </p>
            ))}
          </div>
          <div className="flex w-full items-center gap-4">
            <Button
              btnText={BTN_TEXT.close}
              btnSize="md"
              btnColor="grayOutline"
              handleClick={closeModal}
            />
            <Button
              btnText={BTN_TEXT.delete}
              btnSize="md"
              btnColor="redFilled"
              handleClick={closeModal}
            />
          </div>
        </ModalLayout>
      )}
    </div>
  );
};

export default UITestPage;
