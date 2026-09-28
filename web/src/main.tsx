import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Global first: tokens and shared controls must sit underneath every screen stylesheet in
// the cascade, so a screen can refine .btn or .segmented without fighting import order.
import "./styles/global.css";
import App from "./App.tsx";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
