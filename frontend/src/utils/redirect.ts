/**
 * redirectTo performs a full-page navigation. Used for the delegated OAuth
 * hand-off where the browser must leave the SPA (isolated here so tests can
 * mock it instead of touching window.location, which jsdom cannot navigate).
 */
export const redirectTo = (url: string): void => {
  window.location.href = url;
};
