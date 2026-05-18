import { createFileRoute } from "@tanstack/react-router";
import { FormEvent, useEffect, useState } from "react";
import {
  LOGIN_BROWSER_INIT,
  LoginFlow,
  FlowExpiredError,
  collectErrorMessages,
  fetchLoginFlow,
  listOidcProviders,
  submitOidcLogin,
  submitPasswordLogin,
} from "../lib/kratos";

export const Route = createFileRoute("/login")({
  component: Login,
});

type Status = "loading" | "ready" | "submitting" | "expired" | "error";

function Login() {
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

  async function onOidc(providerId: string) {
    if (!flow) return;
    setStatus("submitting");
    setErrors([]);
    try {
      const url = await submitOidcLogin(flow, providerId);
      window.location.assign(url);
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

  const oidcProviders = flow ? listOidcProviders(flow) : [];
  // CLI-init API flows can only finish via OIDC — the exchange-code
  // mechanism doesn't fire for the password method, so we hide that
  // form entirely. The CLI sets return_to + session_token_exchange_code
  // when initializing the flow.
  const cliMode =
    flow?.type === "api" && !!flow.session_token_exchange_code;

  return (
    <div className="w-80 space-y-4">
      <h1 className="text-lg font-semibold">
        {cliMode ? "Sign in to Invosit CLI" : "Sign in to Invosit"}
      </h1>

      {cliMode && (
        <p className="text-xs text-gray-500">
          You're signing in from the invosit CLI. After sign-in, this tab will
          hand off to your terminal.
        </p>
      )}

      {errors.length > 0 && (
        <div className="text-sm text-red-600">
          {errors.map((msg, i) => <div key={i}>{msg}</div>)}
        </div>
      )}

      {oidcProviders.length > 0 && (
        <div className="space-y-2">
          {oidcProviders.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => onOidc(p.id)}
              disabled={status === "submitting"}
              className="w-full border rounded px-3 py-2 hover:bg-gray-50 disabled:opacity-50 capitalize"
            >
              {p.label}
            </button>
          ))}
          {!cliMode && (
            <div className="flex items-center gap-2 text-xs text-gray-400">
              <span className="flex-1 border-t" />
              or
              <span className="flex-1 border-t" />
            </div>
          )}
        </div>
      )}

      {!cliMode && (
        <form onSubmit={onSubmit} className="space-y-3">
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
      )}
    </div>
  );
}
