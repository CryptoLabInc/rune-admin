import { consoleFetch } from "@/api/createFetch";

/** postAuthStart begins delegated sign-in (POST /console/auth/start). */
export const postAuthStart = async () =>
  await consoleFetch("console/auth/start", { method: "POST" });

/** getConsoleSession reads the current console session (GET /console/session). */
export const getConsoleSession = async () =>
  await consoleFetch("console/session");

/** postLogout ends the console session (POST /console/auth/logout). */
export const postLogout = async () =>
  await consoleFetch("console/auth/logout", { method: "POST" });
