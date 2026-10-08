# Instructions for coding agents

## Commit authorship

Commits in this repository are authored by the project owner, with the agent credited
as co-author.

- Before the first commit in a session, set the identity for this clone:

  ```
  git config user.name Toldry
  git config user.email toldry@gmail.com
  ```

- Leave commit signing off for this clone (`git config commit.gpgsign false`): a
  signature made with the agent's key under the owner's identity shows as unverified.
- End every commit message with a co-author trailer for the agent, for example
  `Co-Authored-By: Claude <noreply@anthropic.com>`.
- Check before pushing: `git log -1 --format='%an <%ae> | %cn <%ce>'` must show
  `Toldry <toldry@gmail.com>` for both author and committer.
