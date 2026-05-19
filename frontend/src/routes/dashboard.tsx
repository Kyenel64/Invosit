import { createFileRoute, redirect } from "@tanstack/react-router";
import { UnauthorizedError } from "../lib/api";
import { meQueryOptions } from "../lib/auth";

export const Route = createFileRoute("/dashboard")({
  beforeLoad: async ({ context }) => {
    try {
      await context.queryClient.ensureQueryData(meQueryOptions);
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        throw redirect({ to: "/login" });
      }
      throw err;
    }
  },
  component: Dashboard,
});

function Dashboard() {
  return (
    <div className="text-center space-y-2">
      <h1 className="text-2xl font-semibold">Dashboard</h1>
      <p className="text-sm text-gray-500">Coming soon.</p>
    </div>
  );
}
