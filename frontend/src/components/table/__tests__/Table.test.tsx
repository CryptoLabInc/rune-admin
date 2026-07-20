import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import Table from "@/components/table/Table";
import TableCell from "@/components/table/TableCell";
import TableErrorRow from "@/components/table/TableErrorRow";
import TableFoot from "@/components/table/TableFoot";
import TableHead from "@/components/table/TableHead";
import TableHeaderCell from "@/components/table/TableHeaderCell";
import TableRow from "@/components/table/TableRow";
import TableToolbar from "@/components/table/TableToolbar";

const renderTable = (rows: React.ReactNode) =>
  render(
    <Table
      toolbar={<TableToolbar title="사용자" count={23} selectedCount={2} />}
      foot={<TableFoot info="총 23명 · 10명/페이지" />}
    >
      <TableHead>
        <TableHeaderCell>멤버 이름</TableHeaderCell>
        <TableHeaderCell>멤버 상태</TableHeaderCell>
      </TableHead>
      <tbody>{rows}</tbody>
    </Table>,
  );

describe("Table", () => {
  it("renders toolbar, header cells, body cells, and foot info", () => {
    renderTable(
      <TableRow>
        <TableCell>k@corp.com</TableCell>
        <TableCell>온라인</TableCell>
      </TableRow>,
    );
    expect(screen.getByText("사용자")).toBeInTheDocument();
    expect(screen.getByText("23")).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: "멤버 이름" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("cell", { name: "k@corp.com" }),
    ).toBeInTheDocument();
    expect(screen.getByText("총 23명 · 10명/페이지")).toBeInTheDocument();
  });

  it("shows the plain-number SELECTED indicator only while rows are selected", () => {
    const { rerender } = render(
      <TableToolbar title="사용자" selectedCount={2} />,
    );
    expect(screen.getByText("2 SELECTED")).toBeInTheDocument();

    rerender(<TableToolbar title="사용자" selectedCount={0} />);
    expect(screen.queryByText(/SELECTED/)).not.toBeInTheDocument();
  });

  it("applies the selected and changed row states", () => {
    render(
      <table>
        <tbody>
          <TableRow selected changed>
            <TableCell>row</TableCell>
          </TableRow>
        </tbody>
      </table>,
    );
    const row = screen.getByRole("row");
    expect(row).toHaveClass("bg-mint/[3%]");
    expect(row).toHaveClass("shadow-accent-blue");
    expect(row).not.toHaveClass("hover:bg-muted-foreground/[3%]");
  });

  it("drops the hover wash on non-hoverable (read-only) rows", () => {
    render(
      <table>
        <tbody>
          <TableRow hoverable={false}>
            <TableCell>row</TableCell>
          </TableRow>
        </tbody>
      </table>,
    );
    expect(screen.getByRole("row")).not.toHaveClass(
      "hover:bg-muted-foreground/[3%]",
    );
  });

  it("announces a row error spanning the given columns", () => {
    renderTable(
      <TableErrorRow colSpan={2} message="요청을 처리하지 못했습니다." />,
    );
    const cell = screen.getByRole("alert");
    expect(cell).toHaveTextContent("요청을 처리하지 못했습니다.");
    expect(cell).toHaveAttribute("colspan", "2");
  });

  it("merges an external className on the frame", () => {
    const { container } = render(
      <Table className="max-w-content">
        <tbody />
      </Table>,
    );
    expect(container.firstChild).toHaveClass("max-w-content");
  });
});
