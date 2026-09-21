// Vitest global setup.
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// globals: false means @testing-library/react's automatic afterEach(cleanup) never
// registers (it only self-wires when it finds afterEach on globalThis), so each
// render() would otherwise pile up in the jsdom document across tests in a file.
afterEach(() => cleanup());

// jsdom does not implement matchMedia; components that read prefers-color-scheme
// (src/lib/theme.ts) need a stand-in or every render throws.
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}
