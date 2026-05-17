import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Login } from "./Login";
import { CliSuccess } from "./CliSuccess";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

function Page() {
  switch (window.location.pathname) {
    case "/cli-success":
      return <CliSuccess />;
    default:
      return <Login />;
  }
}

createRoot(root).render(
  <StrictMode>
    <div className="min-h-screen flex items-center justify-center">
      <Page />
    </div>
  </StrictMode>,
);
