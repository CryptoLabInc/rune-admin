/**
 * parseErrorCode reads the { code } field from an errored API Response body
 * (the common error envelope { code, message }). Returns "" if the body is
 * missing or unparseable, so callers can fall back to a generic message.
 */
export const parseErrorCode = async (res: Response): Promise<string> => {
  try {
    const body = (await res.json()) as { code?: unknown };
    return typeof body.code === "string" ? body.code : "";
  } catch {
    return "";
  }
};
