/** Username (표시용 사용자 이름) rules — API design §공통 계약 (2026-07-20):
 * 한글·영문 소문자만, 단어 사이 공백 1칸(양끝 공백 금지), 1~50자. */

import { L } from "@/locales";

export const USERNAME_MAX_LENGTH = 50;

export const USERNAME_FORMAT_ERROR = L.validation.usernameRule;

/* Compatibility jamo (ㄱ-ㅎ·ㅏ-ㅣ) are accepted while typing so an
   in-progress IME composition never flags as invalid mid-keystroke;
   the submit-time pattern below requires composed syllables. */
const TYPING_PATTERN = /^[a-z가-힣ㄱ-ㅎㅏ-ㅣ]+( [a-z가-힣ㄱ-ㅎㅏ-ㅣ]+)*( )?$/;
const SUBMIT_PATTERN = /^[a-z가-힣]+( [a-z가-힣]+)*$/;

/**
 * normalizeUsernameInput shapes raw keystrokes into the closest legal
 * value instead of rejecting them: uppercase lowers, repeated spaces
 * collapse to one, and a leading space is dropped. A single trailing
 * space survives so the user can keep typing the next word — it is
 * trimmed at submit time.
 */
export const normalizeUsernameInput = (value: string): string =>
  value.toLowerCase().replace(/ {2,}/g, " ").replace(/^ /, "");

/**
 * validateUsername returns the inline error for a normalized input
 * value, or undefined while it is acceptable. Empty input is not an
 * error here (the submit button just stays disabled).
 */
export const validateUsername = (value: string): string | undefined => {
  if (value === "") return undefined;
  if (value.length > USERNAME_MAX_LENGTH || !TYPING_PATTERN.test(value))
    return USERNAME_FORMAT_ERROR;
  return undefined;
};

/** isSubmittableUsername gates the submit button: trimmed, fully
    composed, 1~50 chars. */
export const isSubmittableUsername = (value: string): boolean => {
  const trimmed = value.trim();
  return (
    trimmed.length >= 1 &&
    trimmed.length <= USERNAME_MAX_LENGTH &&
    SUBMIT_PATTERN.test(trimmed)
  );
};
