# Issue tracker: GitHub

Issues and specs live in GitHub Issues. Use the `gh` CLI.

## Repository selection

Infer the repository from `git remote -v`. If no GitHub remote is
configured, obtain the target OWNER/REPO from the user and pass
`--repo OWNER/REPO` to issue commands.

## Issue operations

- Create: `gh issue create --title "..." --body-file <path>`.
- Read: `gh issue view <number> --comments`.
- List: `gh issue list --state open --json number,title,body,labels,comments`.
- Comment: `gh issue comment <number> --body-file <path>`.
- Add or remove labels: `gh issue edit <number> --add-label "..."`
  or `--remove-label "..."`. Use the mapping in `triage-labels.md`.
- Close: `gh issue close <number> --comment "..."`.

Write multiline bodies to a temporary file and pass its path
with `--body-file`.

When a skill says "publish to the issue tracker", create a GitHub
issue. When it says "fetch the relevant ticket", read the issue
and its comments.

## Pull requests as a triage surface

PRs as a request surface: no.

## Wayfinding operations

- Map: one issue labelled `wayfinder:map`, containing Notes,
  Decisions-so-far, and Fog.
- Child tickets: GitHub sub-issues labelled `wayfinder:<type>`,
  where type is research, prototype, grilling, or task.
  If sub-issues are unavailable, use a task list in the map and
  put `Part of #<map>` at the top of each child.
- Blocking: use native GitHub issue dependencies. When unavailable,
  record `Blocked by: #<n>, #<n>` near the top of the child.
  A ticket is unblocked when all blockers are closed.
- Frontier: open, unblocked, unassigned children; first in map
  order wins.
- Claim: assign the ticket to the driving developer before work.
- Resolve: comment with the answer, close the child, and append
  a gist and link to Decisions-so-far in the map.
