import { Link } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Session, whoami } from "../kratos";

type AuthState =
  | { status: "loading" }
  | { status: "signed-in"; session: Session }
  | { status: "signed-out" };

export function Header() {
  const [auth, setAuth] = useState<AuthState>({ status: "loading" });

  useEffect(() => {
    whoami()
      .then((s) => {
        setAuth(s ? { status: "signed-in", session: s } : { status: "signed-out" });
      })
      .catch(() => setAuth({ status: "signed-out" }));
  }, []);

  return (
    <header className="border-b border-gray-200 bg-white">
      <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
        <Link to="/" className="text-lg font-semibold">
          Invosit
        </Link>
        <nav>
          {auth.status === "loading" ? (
            <span className="text-sm text-gray-400">…</span>
          ) : auth.status === "signed-in" ? (
            <Link
              to="/dashboard"
              className="text-sm border rounded px-3 py-1.5 hover:bg-gray-50"
            >
              Dashboard
            </Link>
          ) : (
            <Link
              to="/login"
              className="text-sm border rounded px-3 py-1.5 bg-black text-white hover:bg-gray-800"
            >
              Login
            </Link>
          )}
        </nav>
      </div>
    </header>
  );
}
