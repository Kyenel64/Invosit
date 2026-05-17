import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: Home,
});

function Home() {
  return (
    <div className="text-center">
      <h1 className="text-3xl font-semibold">Welcome to Invosit</h1>
    </div>
  );
}
