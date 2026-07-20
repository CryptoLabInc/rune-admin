# 초대/세션 상태 2축 분리 설계

- 작성일: 2026-07-20
- 대상: rune-console (frontend + mock-server + internal 백엔드)
- 브랜치: release/v1.0.0

## 배경 및 목표

현재 멤버 상태는 단일 enum 5개 값으로 표현된다.

```ts
// frontend/src/types/teamTypes.ts:16
type TTeamMemberStatus =
  "online" | "invite_redeemed" | "invite_pending" | "invite_expired" | "session_expired";
```

이 단일 축은 두 개의 서로 다른 생애주기(초대 코드의 상태 / 세션 토큰의 상태)를 한 값에 섞어 놓았다.
초대 코드는 여러 번 발송될 수 있으므로 두 관심사를 분리해 각각 독립적으로 표현하는 것이 목표다.

핵심 결정: 실 백엔드(`internal/`)는 이미 원천 데이터(멤버 lifecycle + 토큰 liveness/activation + 최신 초대 만료)를 모두 추적하고 있고, 단일 status는 그것을 파생시킨 값일 뿐이다. 따라서 이 작업은 **원천 데이터 변경이 아니라 파생 출력 형태를 두 필드로 바꾸는 작업**이다.

## 상태 모델

두 개의 독립된 필드로 분리한다.

```ts
type TInvitationStatus = "invite_pending" | "invite_expired" | "invite_redeemed";
type TSessionStatus    = "online" | "offline";
```

### invitation status = "가장 최근 발급된 코드의 상태"

| 값 | 조건 |
|---|---|
| `invite_pending` | 최신 코드가 미사용 + 24h(만료시간) 이내 |
| `invite_expired` | 최신 코드가 미사용 + 24h 경과, 또는 관리자 취소(revoke) |
| `invite_redeemed` | 최신 코드가 사용됨 (그 이후 새 코드 미발급) |

이 "최신 코드 상태" 정의가 재전송 규칙을 자연스럽게 만든다:
- `invite_expired` 또는 `invite_redeemed` 상태에서 [재전송] → 새 코드가 최신이 되고, 미사용 + 24h 이내이므로 → `invite_pending`.
- 별도의 특수 전이 로직이 필요 없다. 항상 "최신 코드 상태"를 파생하면 된다.

### session status = "현재 세션 토큰의 생존 여부"

| 값 | 조건 |
|---|---|
| `online` | 세션 토큰이 살아있음 **AND** 에이전트가 ReportActivation을 보고함(접속 성공) |
| `offline` | 그 외 전부 — 토큰 없음/만료/파괴, 또는 토큰은 있으나 아직 활성화(ReportActivation) 전 |

### 기존 5개 값 → 두 필드 매핑

| 기존 단일 status | invitationStatus | sessionStatus |
|---|---|---|
| `invite_pending` | invite_pending | offline |
| `invite_expired` | invite_expired | offline |
| `invite_redeemed` (토큰 살아있으나 미활성) | invite_redeemed | offline |
| `online` | invite_redeemed | online |
| `session_expired` | invite_redeemed | offline |

→ 기존 `session_expired`는 "코드는 사용됐으나 세션은 offline"으로 흡수되어 **별도 상태로 사라진다** (원 요구사항의 "사실상 session_expired 상태는 보일 이유 없음"과 일치).

## UI

### 목록 뷰 — 세션 상태만
- `UsersPage` 멤버 목록, `TreeDetailView` 팀 멤버 목록: 상태 컬럼에 **세션 상태(온라인/오프라인)**만 표시.
- 상태 필터 드롭다운: `전체 / 온라인 / 오프라인` 3개로 단순화 (기존 5개 → 3개).

### 상세 뷰 — invitation status 표시
- `MemberDetailDrawer`: invitation status를 구체적으로 표시하고 세션 상태를 병기.
- 상태별 부제(subtitle) 타임스탬프는 두 축 조합에 맞게 재구성:
  - session=online → `최근 접속 {lastAccessAt}`
  - invitation=invite_redeemed & session=offline → `초대 코드 사용됨 · 연결 대기 중`
  - invitation=invite_pending / invite_expired → `최근 초대 코드 발송 {lastInvitedAt}`

### 액션 버튼 활성 규칙
| 버튼 | 활성 조건 |
|---|---|
| [재전송] (resendCode) | 항상 활성 (redeemed/expired → pending 전이) |
| [초대 취소] (cancelInvitation) | `invitationStatus === "invite_pending"` |
| [세션 비활성화] (deactivateSession) | `sessionStatus === "online"` |

세션 비활성화 결정 근거: 세션 비활성화는 토큰을 파괴하는 동작이다. "토큰 살아있으나 미활성"인 찰나의 구간은 session=offline으로 표시되며 이때는 비활성화 불가 — 곧 자연 만료되거나 online이 되면 그때 비활성화 가능. 보이는 세션 상태와 버튼 활성 조건이 1:1로 대응해 모델이 가장 단순해진다. (기존 백엔드는 미활성 토큰도 파괴 가능했으나, 이 미세한 동작 변경을 수용한다.)

## 작업 순서 (전략)

3단계로 진행한다. 계약(contract)을 프론트 쪽에서 먼저 굳혀 리스크를 최소화한다.

1. **API 계약 확정** — 이 설계 문서의 상태 모델/전이/매핑 표가 계약이다. 두 필드 이름/값/파생 규칙 고정.
2. **mock-server + frontend 동시 수정** — 계약 기반으로:
   - `mock-server/types.ts`, `store.ts`, `routes/{invitations,users,teamMembers}.ts` 를 두 필드로 변경, 시드 데이터 재구성.
   - `frontend/src/types/{userTypes,teamTypes}.ts`, `memberStatusMap.ts`, `styleConstants.ts` 상태 매핑/라벨 변경.
   - 목록/상세/필터/액션 버튼 UI 로직 갱신.
   - 기존 테스트(mock-server/__tests__, api/__tests__, 컴포넌트 __tests__) 갱신 및 통과 확인.
3. **백엔드 수정** — 확정된 계약을 스펙 삼아 `internal/server/console_api_users.go`의 `status()` 파생 함수를 두 필드(`invitationStatus`, `sessionStatus`) 출력으로 변경. 원천 데이터(토큰 liveness/activatedAt, 최신 invite 만료)는 이미 존재하므로 변경 폭은 파생 로직과 응답 DTO에 국한된다.

### 선행 정리
워킹트리에 username 추가 관련 미커밋 변경(37개 파일)이 있다. 이 작업과 **직교**하므로, 상태 분리를 시작하기 전에 username 작업을 먼저 커밋/정리하여 diff를 깨끗이 한다.

## 영향 파일 (참고)

**프론트**
- 타입: `frontend/src/types/teamTypes.ts:16`, `frontend/src/types/userTypes.ts`
- 매핑/라벨: `frontend/src/components/users/memberStatusMap.ts`, `frontend/src/constants/styleConstants.ts:83`
- 목록/필터: `frontend/src/pages/UsersPage.tsx`, `frontend/src/components/teams/TreeDetailView.tsx`
- 상세/액션: `frontend/src/components/users/MemberDetailDrawer.tsx`

**mock-server**
- `frontend/mock-server/types.ts:6`, `store.ts`, `routes/invitations.ts`, `routes/users.ts`, `routes/teamMembers.ts`

**백엔드**
- `internal/server/console_api_users.go:209` (`status()` 파생 함수 + 응답 DTO)
- 참고: `internal/members/`, `internal/invites/`

## 테스트 전략
- mock-server 라우트 단위 테스트: 재전송(redeemed→pending, expired→pending), 취소(pending→expired), 비활성화(online→offline) 전이 검증.
- 프론트 컴포넌트 테스트: 목록은 세션 상태만 렌더, 필터 3종 동작, 드로어의 invitation status 표시 및 버튼 활성 규칙.
- 백엔드: `status()` 파생 테이블 케이스(위 5개 값 → 두 필드 매핑표)를 그대로 테스트로 옮김.

## 결정 요약 (사용자 확정)
1. 세션 상태는 `online | offline` 2개만. `session_expired`는 제거/흡수.
2. redeemed에서 재전송 시 invitation status는 `invite_pending`으로 복귀.
3. 목록 뷰는 세션 상태만, 상세 드로어는 invitation status를 구체 표시.
4. 상태 필터는 세션 상태(전체/온라인/오프라인) 기준.
5. [세션 비활성화] 버튼은 `sessionStatus === "online"`일 때만 활성.
6. 작업 순서: 계약 확정 → mock+frontend → 백엔드.
