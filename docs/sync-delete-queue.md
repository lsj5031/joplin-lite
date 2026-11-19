# Held remote deletes

A permanent local delete can reach a remote only if the item had synced there
and the delete was queued while that remote was the identified, active target.
Restarting on the same remote keeps eligible deletes queued. Selecting another
target or turning sync off holds outstanding deletes instead of replaying them
when the old target returns. An offline network alone does not turn sync off.

Existing `deleted_items` rows from before this feature have no reliable
remote provenance. The upgrade preserves them but holds them all, even for
the target currently configured. The upgrade itself does not reset the existing
sync cursor or sync points. New deletes made on the identified active target
continue to sync normally.

## Review and recover

Held rows are not retried automatically. To inventory them, use a read-only
query against a **backup copy** of the joplin-lite database:

```sql
SELECT id, sync_target, item_type, item_id, deleted_time
FROM deleted_items
WHERE jl_scope = ''
ORDER BY deleted_time, id;
```

The target number identifies a provider, not the account or remote directory
that originally saw the item. The time is Unix milliseconds. Review each item
on the intended remote using a synced peer. Keep or recover it if its deletion
is uncertain. If you still intend to delete it, make a fresh, deliberate
deletion of the verified item on that peer or after it has been pulled into
joplin-lite with the intended target active. Do not bulk requeue or rewrite
the held rows. No note content or credentials are stored by these bindings.

The binding detects configured endpoint, account/grant, or directory changes
using a local fingerprint. With an access-only Dropbox credential, changing
the credential holds old deletes even if it might be for the same account.
Replacements behind an unchanged endpoint/account or the same resolved directory
path are not distinguishable from the previous remote. Do not run multiple
writers against the same local database while changing targets. Older
joplin-lite binaries do not understand the hold flag; do not reopen an
upgraded vault with an older syncing binary.
