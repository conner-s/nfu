# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker.

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Requires human implementation            |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from this table.

These are the defaults; nothing has been renamed to match an existing vocabulary.

## Creating the labels

All five labels exist in this repository on its home forge. If one goes
missing, recreate it with `fj`, run inside the clone:

```
fj repo labels view
fj repo labels create needs-triage d93f0b -d "Maintainer needs to evaluate this issue"
```

`/wayfinder` also uses `wayfinder:map`, `wayfinder:research`,
`wayfinder:prototype`, `wayfinder:grilling` and `wayfinder:task`; create them
the same way the first time they're needed.
