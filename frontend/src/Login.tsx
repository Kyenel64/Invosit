import { FormEvent, useEffect, useState } from "react";
import {
  LOGIN_BROWSER_INIT,
  LoginFlow,
  FlowExpiredError,
  collectErrorMessages,
  fetchLoginFlow,
  initApiLoginFlow,
  submitPasswordLogin,
} from "./kratos";

type Status = "loading" | "ready" | "submitting" | "expired" | "error";

// CLI-handoff mode: when the URL has `?cli_callback=<loopback url>`,
// the page is being driven by the local invosit CLI. We initialize a
// Kratos *API* flow (no cookie session, returns a session_token in the
// submit response) and forward the session_token to the loopback after
// a successful sign-in.
function readCliCallback(): string | null {
  return new URLSearchParams(window.location.search).get("cli_callback");
}

function isAllowedCliCallback(raw: string): boolean {
  try {
    const u = new URL(raw);
    if (u.protocol !== "http:") return false;
    if (u.hostname !== "127.0.0.1" && u.hostname !== "localhost") return false;
    return true;
  } catch {
    return false;
  }
}

export function Login() {
  const [status, setStatus] = useState<Status>("loading");
  const [flow, setFlow] = useState<LoginFlow | null>(null);
  const [errors, setErrors] = useState<string[]>([]);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const cliCallback = readCliCallback();
  const cliMode = cliCallback !== null && isAllowedCliCallback(cliCallback);

  useEffect(() => {
    if (cliMode) {
      initApiLoginFlow()
        .then((f) => {
          setFlow(f);
          setErrors(collectErrorMessages(f));
          setStatus("ready");
        })
        .catch((err) => {
          setErrors([err instanceof Error ? err.message : "unknown error"]);
          setStatus("error");
        });
      return;
    }

    const flowId = new URLSearchParams(window.location.search).get("flow");
    if (!flowId) {
      window.location.assign(LOGIN_BROWSER_INIT);
      return;
    }
    fetchLoginFlow(flowId)
      .then((f) => {
        setFlow(f);
        setErrors(collectErrorMessages(f));
        setStatus("ready");
      })
      .catch((err) => {
        if (err instanceof FlowExpiredError) {
          setStatus("expired");
        } else {
          setErrors([err instanceof Error ? err.message : "unknown error"]);
          setStatus("error");
        }
      });
  }, [cliMode]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!flow) return;
    setStatus("submitting");
    setErrors([]);
    try {
      const result = await submitPasswordLogin(flow, email, password);
      if (result.ok) {
        if (cliMode && cliCallback && result.sessionToken) {
          const target = `${cliCallback}?token=${encodeURIComponent(result.sessionToken)}`;
          window.location.assign(target);
          return;
        }
        window.location.assign(result.redirectTo ?? "/");
        return;
      }
      setFlow(result.flow);
      setErrors(collectErrorMessages(result.flow));
      setStatus("ready");
    } catch (err) {
      if (err instanceof FlowExpiredError) {
        setStatus("expired");
        return;
      }
      setErrors([err instanceof Error ? err.message : "unknown error"]);
      setStatus("ready");
    }
  }

  if (status === "loading") return <p>Loading…</p>;

  if (status === "expired") {
    return (
      <div>
        <h1 className="text-lg font-semibold mb-2">Session expired</h1>
        <p className="mb-4">Your login session expired. Please try again.</p>
        <a href={LOGIN_BROWSER_INIT} className="underline">Restart login</a>
      </div>
    );
  }

  return (
    <form onSubmit={onSubmit} className="w-80 space-y-3">
      <h1 className="text-lg font-semibold">
        {cliMode ? "Sign in to Invosit CLI" : "Sign in to Invosit"}
      </h1>

      {cliMode && (
        <p className="text-xs text-gray-500">
          You're signing in from the invosit CLI. After sign-in this tab will
          hand off to your terminal.
        </p>
      )}

      {errors.length > 0 && (
        <div className="text-sm text-red-600">
          {errors.map((msg, i) => <div key={i}>{msg}</div>)}
        </div>
      )}

      <input
        type="email"
        placeholder="Email"
        autoComplete="username"
        required
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        disabled={status === "submitting"}
        className="w-full border rounded px-3 py-2"
      />

      <input
        type="password"
        placeholder="Password"
        autoComplete="current-password"
        required
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        disabled={status === "submitting"}
        className="w-full border rounded px-3 py-2"
      />

      <button
        type="submit"
        disabled={status === "submitting"}
        className="w-full border rounded px-3 py-2 bg-black text-white disabled:opacity-50"
      >
        {status === "submitting" ? "Signing in…" : "Sign in"}
      </button>
    </form>
  );
}
