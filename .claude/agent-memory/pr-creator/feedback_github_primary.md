---
name: GitHub is primary; Linear is personal mirror
description: Default to GitHub Issues/PRs for all tracking — Linear is the user's personal management mirror, kept linked bi-directionally but not posted to by this agent
type: feedback
---

GitHub Issues and PRs are the **primary** tracker. Linear is the user's **personal** mirror for project management — they don't want the agent filing or posting in Linear. The GitHub ↔ Linear integration is two-way: including an `INV-NN` in the branch name or PR title auto-links the PR to the matching Linear issue and moves it through Linear's workflow on merge. That's the only Linear touchpoint this agent needs.

**Why:** The user explicitly said: "I only use Linear for management on my own side, I primarily still use GitHub issues and PRs. And they're linked both ways." Linear isn't a team workflow here — it's a personal cross-reference.

**How to apply:**
- Default to `gh issue create` / `gh issue list` / `gh pr create`. Use `Closes #N` / `Fixes #N` / `Refs #N` in the PR body.
- Don't ask the user to file in Linear. Don't draft Linear issues. Don't add `Closes INV-NN` to the PR body.
- If the user mentions an `INV-NN`: confirm it back, embed it in the branch name (`inv-NN-slug`) or PR title to trigger the auto-link. The auto-link is enough — no body reference needed.
- If a fresh GitHub issue needs to be created, do it without asking (per the agent's autonomy rules) and link it.
