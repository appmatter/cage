---
name: implement-pr
description: >
  Optional deep checklist for large multi-requirement PRs. Default Cursor-like
  verify behavior is already always on via APPEND_SYSTEM and the verify-done
  extension — users do not need to invoke this skill.
---

# Implement PR (optional detail)

Default agent behavior already requires map → verify → evidence.
Use this only when you want the longer checklist.

## Map → reuse → implement → verify → evidence

Same rules as `APPEND_SYSTEM.md`. Helper:

```bash
sh ~/.pi/agent/skills/implement-pr/scripts/verify.sh [packages...]
```
