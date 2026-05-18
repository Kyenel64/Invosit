import { Link } from "@tanstack/react-router";
import { useAuth } from "../lib/auth";

export function Header() {
  const { state } = useAuth();

  return (
    <header className="border-b border-gray-200 bg-white">
      <div className="max-w-5xl mx-auto px-4 py-3 flex items-center justify-between">
        <Link to="/" className="text-lg font-semibold">
          Invosit
        </Link>
        <nav>
          {state.status === "loading" ? (
            <span className="text-sm text-gray-400">…</span>
          ) : state.status === "signed-in" ? (
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
