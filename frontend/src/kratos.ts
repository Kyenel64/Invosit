// Minimal client for Ory Kratos's self-service browser login flow.
// Docs: https://www.ory.com/docs/kratos/self-service/flows/user-login

const KRATOS_URL = "http://127.0.0.1:4433";

export const LOGIN_BROWSER_INIT = `${KRATOS_URL}/self-service/login/browser`;

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
