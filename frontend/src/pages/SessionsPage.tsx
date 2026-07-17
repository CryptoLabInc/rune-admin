import { useEffect, useState } from "react";

import Button from "@/components/elements/Button";
import Dropdown from "@/components/elements/Dropdown";
import Feedback from "@/components/elements/Feedback";
import Pagination from "@/components/elements/Pagination";
import Table from "@/components/table/Table";
import TableCell from "@/components/table/TableCell";
import TableFoot from "@/components/table/TableFoot";
import TableHead from "@/components/table/TableHead";
import TableHeaderCell from "@/components/table/TableHeaderCell";
import TableRow from "@/components/table/TableRow";
import { useInvitationHistoryQuery } from "@/hooks/queries/useInvitationHistoryQuery";
import { cn } from "@/utils/cn";
import { formatDateTime } from "@/utils/formatDate";
import { BTN_TEXT } from "@/constants/commonConstants";
import type { TDropdownOption } from "@/types/commonTypes";

const styles = {
  page: "flex flex-col gap-3.5 p-4",
  accountCell: "max-w-[240px] truncate",
  timeCell: "text-muted-foreground font-mono text-xs",
};

/* Sort option set (SC-16 no.1) — values are the GET /invitations
   sort query params (console API design §6). No status filter or
   issuance button: issuance lives in user/team management. */
const SORT_OPTIONS: TDropdownOption[] = [
  { value: "account", label: "이메일 (account)" },
  { value: "issued_at", label: "최근 발급 시간" },
  { value: "last_access", label: "최근 접속 시간" },
];

/* 10 rows per page, fixed (SC-16 no.4) — the ?size=10 query param. */
const PAGE_SIZE = 10;

/**
 * SessionsPage is the session management screen (SC-16): the token
 * issuance/access history table (state A) with a 3-way sort and fixed
 * 10-per-page pagination, plus the fetch-failure state (state B).
 * Read-only — one row per issuance (D11), and the code value itself
 * is never shown. Sort and paging run server-side
 * (GET /invitations?view=history&sort&page&size).
 */
const SessionsPage = () => {
  const [sort, setSort] = useState("last_access");
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const currentPage = Math.min(page, totalPages);
  const historyQuery = useInvitationHistoryQuery(sort, currentPage, PAGE_SIZE);

  const rows = historyQuery.data?.items ?? [];
  const total = historyQuery.data?.total ?? 0;

  /* totalPages tracks the last response's total (a page/sort transition
     keeps the previous value via keepPreviousData until the new page
     resolves); currentPage clamps against it before the query call
     above, so the request itself is always in range. This effect only
     corrects the stored `page` once totalPages shrinks (e.g. a sort
     change reduces the result count), so Pagination and later renders
     resume from a valid value instead of the stale, too-high one. */
  useEffect(() => {
    const nextTotalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
    setTotalPages(nextTotalPages);
    if (page > nextTotalPages) setPage(nextTotalPages);
  }, [total, page]);

  /* Sort change resets to page 1 (SC-16 no.4). */
  const changeSort = (value: string) => {
    setSort(value);
    setPage(1);
  };

  /* ── SC-16 state B — 조회 실패 ──────────────────────────────────── */
  if (historyQuery.isError) {
    return (
      <section className={styles.page} aria-label="세션 관리">
        <Feedback
          state="error"
          /* Taller, fully centered variant — the SC-16 state B canvas
             centers icon/text/button in a 180px-min panel, unlike the
             default left-aligned 92px row. */
          className="flex min-h-45 flex-col items-center justify-center text-center"
          title="이력 정보를 불러올 수 없습니다."
          description="새로고침 후 다시 시도해 주세요."
          action={
            <Button
              btnText={BTN_TEXT.refresh}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-fit"
              handleClick={() => window.location.reload()}
            />
          }
        />
      </section>
    );
  }

  /* ── SC-16 state A — 기본 ───────────────────────────────────────── */
  return (
    <section className={styles.page} aria-label="세션 관리">
      <Table
        fluid
        /* Fixed page height: thead 34px + 10 rows × 36px (h-9). Short
           pages, empty, and loading all keep this height so pagination
           never shifts the layout. */
        scrollClassName="min-h-[394px]"
        toolbar={
          <div className="flex items-center gap-2 px-4 py-4">
            <span className="text-md text-faint">정렬 기준</span>
            <Dropdown
              options={SORT_OPTIONS}
              value={sort}
              onChange={changeSort}
              size="sm"
              ariaLabel="정렬"
              className="w-40"
            />
          </div>
        }
        foot={
          <TableFoot
            info={`총 ${total}건 · ${PAGE_SIZE}건/페이지`}
            className="flex-row"
          >
            <Pagination
              page={currentPage}
              totalPages={totalPages}
              onChange={setPage}
            />
          </TableFoot>
        }
      >
        {/* Fixed column widths — auto layout would resize per page's
            content and shift the headers while paginating. */}
        <TableHead>
          <TableHeaderCell className="w-2/5">사용자</TableHeaderCell>
          <TableHeaderCell className="w-[30%]">발급 시간</TableHeaderCell>
          <TableHeaderCell className="w-[30%]">최근 접속 시간</TableHeaderCell>
        </TableHead>
        <tbody>
          {historyQuery.isPending && (
            <tr>
              <td
                colSpan={3}
                className="text-faint px-3 py-8 text-center text-sm"
              >
                불러오는 중…
              </td>
            </tr>
          )}
          {!historyQuery.isPending && total === 0 && (
            <tr>
              <td
                colSpan={3}
                className="text-muted-foreground border-t px-3 py-8 text-center text-sm"
              >
                이력이 없습니다.
              </td>
            </tr>
          )}
          {rows.map((row) => (
            /* Reissues are separate rows (D11) — account alone is not
               unique, account+issuedAt is. */
            <TableRow
              key={`${row.account}-${row.issuedAt}`}
              hoverable={false}
              className="h-9"
            >
              <TableCell className={styles.accountCell}>
                <span title={row.account}>{row.account}</span>
              </TableCell>
              <TableCell className={styles.timeCell}>
                {formatDateTime(row.issuedAt)}
              </TableCell>
              {/* No access history yet ("—") drops to faint, per the
                  session-history variant in the UI test library. */}
              <TableCell
                className={cn(
                  styles.timeCell,
                  !row.lastAccessAt && "text-faint",
                )}
              >
                {formatDateTime(row.lastAccessAt)}
              </TableCell>
            </TableRow>
          ))}
        </tbody>
      </Table>
    </section>
  );
};

export default SessionsPage;
