// HTTP helpers shared by every route handler.
import type { IncomingMessage, ServerResponse } from "node:http";

// HttpError is thrown by handlers and translated to a JSON error body by the
// server dispatcher. Note: no TS parameter properties (unsupported by Node's
// --experimental-strip-types), so fields are assigned explicitly.
export class HttpError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export const nowISO = (): string => new Date().toISOString();

/** isoIn returns an ISO timestamp `ms` milliseconds from now. */
export const isoIn = (ms: number): string =>
  new Date(Date.now() + ms).toISOString();

export const sendJson = (
  res: ServerResponse,
  status: number,
  body: unknown,
): void => {
  const payload = JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json; charset=utf-8" });
  res.end(payload);
};

export const sendNoContent = (res: ServerResponse): void => {
  res.writeHead(204);
  res.end();
};

/** sendError emits the common `{ code, message }` error body (see Common contract). */
export const sendError = (
  res: ServerResponse,
  status: number,
  code: string,
  message: string,
): void => sendJson(res, status, { code, message });

/** readBody parses a JSON request body; returns undefined for an empty body. */
export const readBody = async (req: IncomingMessage): Promise<unknown> => {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  if (chunks.length === 0) return undefined;
  const raw = Buffer.concat(chunks).toString("utf-8").trim();
  if (raw === "") return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    throw new HttpError(400, "VALIDATION_ERROR", "malformed JSON body");
  }
};

export type Page<T> = { total: number; page: number; size: number; items: T[] };

/**
 * parsePaging reads `page`/`size` per the common contract: page 1-based,
 * size default 10 / max 100 (400 beyond). A page past the last is not an
 * error — the caller slices and returns an empty `items`.
 */
export const parsePaging = (
  query: URLSearchParams,
): { page: number; size: number } => {
  const page = query.has("page") ? Number(query.get("page")) : 1;
  const size = query.has("size") ? Number(query.get("size")) : 10;
  if (!Number.isInteger(page) || page < 1)
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "page must be a positive integer",
    );
  if (!Number.isInteger(size) || size < 1)
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "size must be a positive integer",
    );
  if (size > 100)
    throw new HttpError(400, "VALIDATION_ERROR", "size exceeds max of 100");
  return { page, size };
};

export const paginate = <T>(
  items: T[],
  page: number,
  size: number,
): Page<T> => {
  const start = (page - 1) * size;
  return {
    total: items.length,
    page,
    size,
    items: items.slice(start, start + size),
  };
};

export type BatchFailure = { id: string; code: string; message: string };
export type BatchResult = { succeeded: string[]; failed: BatchFailure[] };

/** parseCsvQuery splits a comma-separated query value (e.g. ?userIds=u_1,u_2). */
export const parseCsvQuery = (
  query: URLSearchParams,
  key: string,
): string[] => {
  const raw = query.get(key);
  if (raw === null || raw.trim() === "") return [];
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s !== "");
};
