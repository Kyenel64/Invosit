import { FormEvent, useEffect, useState } from "react";
import {
  LOGIN_BROWSER_INIT,
  LoginFlow,
  FlowExpiredError,
  collectErrorMessages,
  fetchLoginFlow,
  submitPasswordLogin,
} from "./kratos";

type Status = "loading" | "ready" | "submitting" | "expired" | "error";

export function Login() {
  const [status, setStatus] = useState<Status>("loading");
  const [flow, setFlow] = useState<LoginFlow | null>(null);
  const [errors, setErrors] = useState<string[]>([]);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  useEffect(() => {
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
  }, []);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!flow) return;
    setStatus("submitting");
    setErrors([]);
    try {
      const result = await submitPasswordLogin(flow, email, password);
      if (result.ok) {
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
      <h1 className="text-lg font-semibold">Sign in to Invosit</h1>

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
