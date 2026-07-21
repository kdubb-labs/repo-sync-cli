# Repository Rules

- Run `make ci` before committing.
- Preserve the safety contract: never reset, force-checkout, force-push, or commit in user repositories.
- Add a failing Go test before production behavior changes.
- Keep normal JSON output compact: counts and actionable skipped/failed entries only.
