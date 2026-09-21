/// <reference types="vitest/config" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: {
    // Proxy /api to the backend so only ONE port is ever exposed. The Go API stays bound
    // to loopback; the browser talks to Vite, Vite talks to the API over 127.0.0.1. That
    // also means same-origin requests, so CORS never enters the picture.
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8099",
        changeOrigin: false,
      },
    },
    // Vite rejects requests whose Host header it does not recognise — a DNS-rebinding
    // guard. Forwarded traffic arrives with whatever host the client used, so when the
    // server is bound to a non-loopback interface the guard has to be told what to expect
    // or every request 403s.
    //
    // List the exact hostnames in BALISE_ALLOWED_HOSTS and the guard stays on. The literal
    // "all" turns it off, which is what `make expose` passes when you have not named any:
    // it is the honest default for "I am deliberately reaching this from elsewhere and do
    // not know the hostname yet", and it is strictly a rebinding guard — it is not what
    // keeps anyone out. Nothing here authenticates; that is the tunnel's or firewall's job.
    allowedHosts: process.env.BALISE_ALLOWED_HOSTS === "all"
      ? true
      : (process.env.BALISE_ALLOWED_HOSTS ?? "").split(",").map((h) => h.trim()).filter(Boolean),
  },
  test: {
    environment: "jsdom",
    globals: false,
    include: ["tests/**/*.test.{ts,tsx}"],
    setupFiles: ["tests/setup.ts"],
    css: false,
    coverage: {
      provider: "v8",
      reporter: ["text", "html"],
      include: ["src/**/*.{ts,tsx}"],
    },
  },
});
