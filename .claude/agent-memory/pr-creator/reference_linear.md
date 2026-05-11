---
name: Linear workspace + team for cross-linking
description: Linear workspace `invosit`, team INV — used as a personal mirror; cross-link from GitHub PRs via inv-NN in the branch name or PR title
type: reference
---

The user mirrors Invosit work to **Linear** for personal management: workspace `invosit`, team **INV** (https://linear.app/invosit). The MVP project is **Invosit MVP** with milestones per build-order phase (M1 Infra bootstrap, M2 Data model + auth, …). Cycles are one week long.

**Branch convention for auto-linking:** when a Linear `INV-NN` is referenced, name the branch `inv-NN-short-slug` (Linear's "Copy git branch name" produces this exact form). The GitHub ↔ Linear integration matches on `INV-NN` in the branch name or PR title and:
- Attaches the GitHub PR to the Linear issue
- Moves the Linear issue forward on merge (Done state)

No need to write `Closes INV-NN` in the PR body — GitHub's `Closes #N` covers GH-side closure; Linear handles its own workflow from the branch/title match.

**How to apply:** Read `feedback_github_primary.md` first — GitHub Issues remains the default, this is only the cross-link mechanism. Use `inv-NN-slug` branch names only when the user explicitly names a Linear issue.
