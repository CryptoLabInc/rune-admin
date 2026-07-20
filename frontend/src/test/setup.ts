import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Auto-cleanup is not registered by Testing Library when vitest runs with
// globals: false, so register it explicitly.
afterEach(() => {
  cleanup();
});

/* jsdom does not implement scrollIntoView (jsdom/jsdom#1695). */
Element.prototype.scrollIntoView = () => {};
