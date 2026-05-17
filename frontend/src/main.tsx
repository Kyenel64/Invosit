import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Login } from "./Login";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>
    <div className="min-h-screen flex items-center justify-center">
      <Login />
    </div>
  </StrictMode>,
);
