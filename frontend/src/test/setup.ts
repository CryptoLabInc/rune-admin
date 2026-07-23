import "@testing-library/jest-dom/vitest";

// Pin the test language to Korean BEFORE any module imports @/locales (setup
// runs ahead of test-file module graphs; the locale is chosen once at import):
// the suite's assertions are written against the Korean copy, and jsdom's
// navigator.language would otherwise pick en.
window.localStorage.setItem("rc_lang", "ko");

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Auto-cleanup is not registered by Testing Library when vitest runs with
// globals: false, so register it explicitly.
afterEach(() => {
  cleanup();
});

/* jsdom does not implement scrollIntoView (jsdom/jsdom#1695). */
Element.prototype.scrollIntoView = () => {};
