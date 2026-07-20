import { serviceFetch } from "@/api/createFetch";

/** Read update availability and any in-flight update state. */
export const getSystemUpdate = async () => await serviceFetch("system/update");

/** Queue an update to the server-selected release version. */
export const postSystemUpdate = async (version: string) =>
  await serviceFetch("system/update", {
    method: "POST",
    body: JSON.stringify({ version }),
  });
