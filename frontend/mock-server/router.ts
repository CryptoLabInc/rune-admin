// A tiny method + path-pattern router. Patterns use `:name` for path params,
// e.g. "/teams/:teamId/members". No external dependency.
import type { IncomingMessage, ServerResponse } from "node:http";

export type Ctx = {
  req: IncomingMessage;
  res: ServerResponse;
  params: Record<string, string>;
  query: URLSearchParams;
  body: unknown;
};

export type Handler = (ctx: Ctx) => void | Promise<void>;

type Route = {
  method: string;
  regex: RegExp;
  keys: string[];
  handler: Handler;
};

const compile = (pattern: string): { regex: RegExp; keys: string[] } => {
  const keys: string[] = [];
  const source = pattern
    .split("/")
    .map((seg) => {
      if (seg.startsWith(":")) {
        keys.push(seg.slice(1));
        return "([^/]+)";
      }
      return seg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    })
    .join("/");
  return { regex: new RegExp(`^${source}/?$`), keys };
};

export class Router {
  private routes: Route[] = [];

  add(method: string, pattern: string, handler: Handler): void {
    const { regex, keys } = compile(pattern);
    this.routes.push({ method: method.toUpperCase(), regex, keys, handler });
  }

  get(pattern: string, handler: Handler): void {
    this.add("GET", pattern, handler);
  }
  post(pattern: string, handler: Handler): void {
    this.add("POST", pattern, handler);
  }
  put(pattern: string, handler: Handler): void {
    this.add("PUT", pattern, handler);
  }
  delete(pattern: string, handler: Handler): void {
    this.add("DELETE", pattern, handler);
  }

  /** match returns the handler + extracted params for a method/pathname, or null. */
  match(
    method: string,
    pathname: string,
  ): { handler: Handler; params: Record<string, string> } | null {
    for (const route of this.routes) {
      if (route.method !== method.toUpperCase()) continue;
      const m = route.regex.exec(pathname);
      if (!m) continue;
      const params: Record<string, string> = {};
      route.keys.forEach((key, i) => {
        params[key] = decodeURIComponent(m[i + 1]);
      });
      return { handler: route.handler, params };
    }
    return null;
  }
}
