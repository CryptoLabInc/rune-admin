import { describe, expect, it } from "vitest";

import { L, language } from "@/locales";
import { en } from "@/locales/en";
import { ko } from "@/locales/ko";
import {
  BTN_TEXT,
  MODAL_TITLES,
  NAV_LIST,
} from "@/constants/commonConstants";
import { WORKSPACE_STATUS_VAR } from "@/constants/styleConstants";

/* Key parity between en and ko is enforced at COMPILE time (ko is typed as
 * TLocale = typeof en), so a drifted key never survives tsc. The runtime
 * checks here cover what types can't: the actual copy and interpolation. */

describe("locales", () => {
  it("resolves Korean under the test pin (setup.ts sets rc_lang=ko)", () => {
    expect(language).toBe("ko");
    expect(L).toBe(ko);
    expect(BTN_TEXT.close).toBe("닫기");
    expect(MODAL_TITLES.deleteTeam("platform")).toBe("팀 삭제 — platform");
    expect(MODAL_TITLES.deleteMemberBulk(3)).toBe("멤버 삭제 (3명)");
    expect(NAV_LIST[0].title).toBe("팀 관리");
    expect(WORKSPACE_STATUS_VAR.running.label).toBe("실행 중");
  });

  it("carries the English copy with its own word order", () => {
    expect(en.btn.close).toBe("Close");
    expect(en.btn.resendInvitationCode).toBe("Resend Invitation Code");
    expect(en.modal.deleteTeam("platform")).toBe("Delete Team — platform");
    expect(en.modal.deleteMemberBulk(3)).toBe("Delete Members (3)");
    expect(en.nav.teams).toBe("Team Management");
    expect(en.status.workspace.running).toBe("Running");
    expect(en.status.invitation.pending).toBe("Invitation Pending");
  });

  it("keeps both locales structurally identical at runtime too", () => {
    const shape = (obj: object, prefix = ""): string[] =>
      Object.entries(obj)
        .flatMap(([k, v]) =>
          typeof v === "object" && v !== null
            ? shape(v, `${prefix}${k}.`)
            : [`${prefix}${k}:${typeof v}`],
        )
        .sort();
    expect(shape(en)).toEqual(shape(ko));
  });
});
