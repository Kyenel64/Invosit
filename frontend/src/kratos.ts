// Minimal client for Ory Kratos's self-service browser login flow.
// Docs: https://www.ory.com/docs/kratos/self-service/flows/user-login

const KRATOS_URL = "http://127.0.0.1:4433";

export const LOGIN_BROWSER_INIT = `${KRATOS_URL}/self-service/login/browser`;
export const WHOAMI = `${KRATOS_URL}/sessions/whoami`;

export interface Identity {
  id: string;
  traits: { email?: string } & Record<string, unknown>;
}

export interface Session {
  id: string;
  active: boolean;
  identity: Identity;
}

// whoami returns the current session if the browser has a valid Kratos
// session cookie, or null if not signed in. Kratos returns 401 when no
// session exists; treat that as "not signed in" rather than an error so
// callers can branch cleanly.
export async function whoami(): Promise<Session | null> {
  const res = await fetch(WHOAMI, {
    headers: { Accept: "application/json" },
    credentials: "include",
  });
  if (res.status === 401 || res.status === 403) return null;
  if (!res.ok) throw new Error(`whoami failed: ${res.status}`);
  return res.json();
}

export interface UiText {
  id: number;
  type: "info" | "error" | "success";
  text: string;
}

export interface UiNodeAttributes {
  name?: string;
  type?: string;
  value?: string;
  required?: boolean;
  disabled?: boolean;
}

export interface UiNode {
  type: string;
  group: string;
  attributes: UiNodeAttributes;
  messages?: UiText[];
}

export interface LoginFlow {
  id: string;
  ui: {
    action: string;
    method: string;
    nodes: UiNode[];
    messages?: UiText[];
  };
}

export interface LoginSuccess {
  session: unknown;
  redirect_browser_to?: string;
}

export class FlowExpiredError extends Error {
  constructor() {
    super("login flow expired");
    this.name = "FlowExpiredError";
  }
}

export async function fetchLoginFlow(flowId: string): Promise<LoginFlow> {
  const res = await fetch(
    `${KRATOS_URL}/self-service/login/flows?id=${encodeURIComponent(flowId)}`,
    {
      headers: { Accept: "application/json" },
      credentials: "include",
    },
  );
  if (res.status === 410 || res.status === 403 || res.status === 404) {
    throw new FlowExpiredError();
  }
  if (!res.ok) {
    throw new Error(`failed to fetch login flow: ${res.status}`);
  }
  return res.json();
}

export function csrfTokenFromFlow(flow: LoginFlow): string {
  const node = flow.ui.nodes.find(
    (n) => n.attributes.name === "csrf_token" && n.attributes.type === "hidden",
  );
  return node?.attributes.value ?? "";
}

export interface SubmitResult {
  ok: true;
  redirectTo: string | null;
}

export interface SubmitFailure {
  ok: false;
  flow: LoginFlow;
}

// listOidcProviders returns the OIDC submit buttons present in a flow.
// Kratos adds one node per configured provider when the OIDC strategy
// is enabled in kratos.yml. Each node's `attributes.value` is the
// provider id we send back on submit; `meta.label.text` is a
// human-readable label ("Sign in with github").
export interface OidcProvider {
  id: string;
  label: string;
}
export function listOidcProviders(flow: LoginFlow): OidcProvider[] {
  return flow.ui.nodes
    .filter(
      (n) =>
        n.group === "oidc" &&
        n.attributes.type === "submit" &&
        n.attributes.name === "provider" &&
        typeof n.attributes.value === "string",
    )
    .map((n) => ({
      id: n.attributes.value as string,
      label:
        (n as { meta?: { label?: { text?: string } } }).meta?.label?.text ??
        `Sign in with ${n.attributes.value}`,
    }));
}

// submitOidcLogin starts the OIDC handoff. Submitting `provider=<id>`
// to the flow action returns (with Accept: application/json) a
// `redirect_browser_to` URL pointing at the IdP's authorize endpoint.
// The caller navigates there; the browser then completes the OAuth
// dance through the provider and back to Kratos's callback.
export async function submitOidcLogin(
  flow: LoginFlow,
  providerId: string,
): Promise<string> {
  const res = await fetch(flow.ui.action, {
    method: flow.ui.method,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({
      method: "oidc",
      provider: providerId,
      csrf_token: csrfTokenFromFlow(flow),
    }),
  });

  // Kratos returns 422 with a redirect_browser_to when the flow needs
  // to bounce the user through the IdP. 200 here would mean a
  // surprise — there's no path to a "synchronous" OIDC success.
  if (res.status === 422 || res.ok) {
    const body = (await res.json()) as { redirect_browser_to?: string };
    if (!body.redirect_browser_to) {
      throw new Error("OIDC submit succeeded but no redirect URL was returned");
    }
    return body.redirect_browser_to;
  }
  if (res.status === 410 || res.status === 403) {
    throw new FlowExpiredError();
  }
  throw new Error(`OIDC submit failed: ${res.status}`);
}

export async function submitPasswordLogin(
  flow: LoginFlow,
  identifier: string,
  password: string,
): Promise<SubmitResult | SubmitFailure> {
  const res = await fetch(flow.ui.action, {
    method: flow.ui.method,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({
      method: "password",
      identifier,
      password,
      csrf_token: csrfTokenFromFlow(flow),
    }),
  });

  if (res.status === 410 || res.status === 403) {
    throw new FlowExpiredError();
  }

  if (res.ok) {
    const body = (await res.json()) as LoginSuccess;
    return { ok: true, redirectTo: body.redirect_browser_to ?? null };
  }

  if (res.status === 400) {
    const body = (await res.json()) as LoginFlow;
    return { ok: false, flow: body };
  }

  throw new Error(`login submit failed: ${res.status}`);
}

export function collectErrorMessages(flow: LoginFlow): string[] {
  const out: string[] = [];
  for (const m of flow.ui.messages ?? []) {
    if (m.type === "error") out.push(m.text);
  }
  for (const n of flow.ui.nodes) {
    for (const m of n.messages ?? []) {
      if (m.type === "error") out.push(m.text);
    }
  }
  return out;
}
