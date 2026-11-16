"""The caller's own account, status, other accounts and groups."""
from lib import area, op


# ----------------------------------------------------------------------------
area('Me',
     "The caller's own account, preferences and credentials.")

op('GET me', 'getMe',
   "Get the caller's account",
   who='Any signed-in account.',
   res={'account': '#Me'})

op('PATCH me', 'updateMe',
   "Change the caller's profile",
   who='Any signed-in account. A recovery session can only read.',
   body={'username?': 'str',
         'display_name?': 'str',
         'title?': 'str',
         'pronouns?': 'str',
         'time_zone?': ('str', 'IANA name. Clients keep it current.')},
   res={'account': '#Me'},
   err='name_taken')

op('PUT me/avatar', 'setMyAvatar',
   "Upload the caller's avatar",
   who='Any signed-in account.',
   multipart={'file': 'bin'},
   res={'account': '#Me'},
   err='too_large')

op('DELETE me/avatar', 'removeMyAvatar',
   "Remove the caller's avatar",
   who='Any signed-in account.')

op('GET me/prefs', 'getPrefs',
   "Get the caller's preferences",
   who='Any signed-in account.',
   res={'prefs': '#Prefs'})

op('PATCH me/prefs', 'updatePrefs',
   "Change the caller's preferences",
   who='Any signed-in account.',
   body={'thread_view?': 'enum:threaded|flat', 'meeting_pauses?': 'bool', 'allow_notify_anyway?': 'bool', 'hide_last_seen?': 'bool'},
   res={'prefs': '#Prefs'})

op('GET me/passkeys', 'listPasskeys',
   "List the caller's passkeys",
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Also a recovery session.',
   res={'passkeys': '[#Passkey]'},
   err='forbidden')

op('POST me/passkeys/begin', 'beginAddPasskey',
   'Start adding a passkey',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Needs a fresh check. Also, without one: a recovery session, and a pending session whose account has no proof set up yet when `passkey` is accepted.',
   res='#Ceremony',
   err='forbidden fresh_check_required',
   auth='pending')

op('POST me/passkeys/finish', 'finishAddPasskey',
   'Finish adding a passkey',
   who='Whoever began it.',
   desc=('From a pending session this counts as the proof and completes the sign-in. From a recovery session it '
         "replaces that session with an ordinary one. The passkey's first creation on an account also makes the "
         'eight recovery codes. A new passkey unlocks sealed chats only after its unlock public key is given with '
         '`setPasskeyUnlockKey` and an unlocked device stores a locked copy for it.'),
   body='#PasskeyCreated',
   res={'passkey': '#Passkey', 'sign_in?': ('#SignIn', 'When the call changed the session.')},
   st=201,
   err='forbidden not_found unauthenticated',
   auth='pending')

op('PATCH me/passkeys/{id}', 'renamePasskey',
   'Rename a passkey, or note that it gave no PRF value',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   body={'name?': 'str', 'has_prf?': ('bool', 'Only `false`: a later use asked for a PRF value and got none.')},
   res={'passkey': '#Passkey'},
   err='forbidden not_found')

op('DELETE me/passkeys/{id}', 'deletePasskey',
   'Delete a passkey',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Needs a fresh check. Also a recovery session.',
   desc=('Deletes its locked copy too. If it had one, the backup key must then be replaced (`replaceBackup`); when '
         'no unlocked device is at hand the server marks the account "replacement due". Refused with `conflict` for '
         'the last passkey of an account with no access key and no linked identity.'),
   err='forbidden not_found conflict fresh_check_required')

op('GET me/access-key', 'getAccessKey',
   'See whether the caller has an access key',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Also a recovery session.',
   desc='Never the key: the server does not have it.',
   res={'access_key?': ('#AccessKey', 'Absent when the account has none.'),
        'may_create': ('bool', 'The server makes new access keys (setting `auth.allow_access_keys`), or the account has one to replace.')},
   err='forbidden')

op('PUT me/access-key', 'setAccessKey',
   'Make a new access key, replacing the one the caller has',
   who=('Any signed-in person, from a web or app session. Not personal tokens, and not bots. Needs a fresh check. '
        'Also a recovery session, where the role may sign in without company sign-in.'),
   desc=('The page makes the key, shows it once and has it confirmed; the server is sent only the value derived '
         'from it. The old key stops working at once. Refused with `forbidden` when the server does not allow '
         'accounts without a passkey and the account has no access key to replace. From a recovery session it '
         'replaces that session with an ordinary one.'),
   body={'access_key_auth': ('b64', 'The 32-byte value the page derives from the access key (AUTH.md section 5.6), never the key itself.')},
   res={'access_key': '#AccessKey', 'sign_in?': ('#SignIn', 'When the call changed the session.')},
   err='forbidden fresh_check_required')

op('DELETE me/access-key', 'removeAccessKey',
   'Remove the access key',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Needs a fresh check.',
   desc='Refused with `conflict` unless the account has a passkey or a linked company identity: an account always keeps a way in.',
   err='forbidden not_found conflict fresh_check_required')

op('GET me/recovery-codes', 'getRecoveryCodes',
   'See how many recovery codes are left',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   res={'remaining': 'int'},
   err='forbidden')

op('POST me/recovery-codes', 'regenerateRecoveryCodes',
   'Replace the recovery codes with eight new ones',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Needs a fresh check.',
   res={'recovery_codes': '[str]'},
   st=201,
   err='forbidden fresh_check_required')

op('GET me/totp', 'getTotp',
   'See whether an authenticator app is set up',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Also a pending session.',
   res={'totp': '#Totp'},
   err='forbidden',
   auth='pending')

op('POST me/totp', 'beginTotp',
   'Start setting up an authenticator app',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Needs a fresh check. Also a pending session whose account has no proof set up yet, when `totp` is accepted.',
   desc=('Replaces any unconfirmed enrolment. An unconfirmed one is never accepted as a proof and is deleted after '
         'an hour.'),
   res={'secret': ('str', 'Base32.'), 'otpauth_url': ('str', 'For a QR code.')},
   st=201,
   err='forbidden fresh_check_required',
   auth='pending')

op('POST me/totp/confirm', 'confirmTotp',
   'Confirm the authenticator app with its first code',
   who='Whoever began it.',
   desc='From a pending session this counts as the proof and completes the sign-in.',
   body={'code': 'str'},
   res={'totp': '#Totp', 'sign_in?': ('#SignIn', 'When the call completed a sign-in.')},
   err='forbidden unauthenticated not_found',
   auth='pending')

op('DELETE me/totp', 'removeTotp',
   'Remove the authenticator app',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Needs a fresh check.',
   desc='Refused with `conflict` when it would leave an account that needs a second factor without any.',
   err='forbidden conflict fresh_check_required')

op('GET me/identities', 'listIdentities',
   'List the company sign-in identities linked to the caller',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   desc=('An identity is linked by `startSso` with purpose `link`. A subject belongs to one account; an account may '
         'hold several identities.'),
   res={'identities': '[#Identity]'},
   err='forbidden')

op('GET me/sessions', 'listSessions',
   "List the caller's sessions",
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Also a recovery session.',
   desc=('Every kind: web, app and personal tokens, with which one is making the request. Token hashes are never '
         'returned.'),
   res={'sessions': '[#Session]'},
   err='forbidden')

op('DELETE me/sessions/{id}', 'endSession',
   "End one of the caller's sessions",
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots. Also a recovery session.',
   desc='Its live connections are closed at once and its device is retired.',
   err='forbidden not_found')

op('DELETE me/sessions', 'endOtherSessions',
   'End every session except the current one',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   err='forbidden')

op('GET me/tokens', 'listTokens',
   "List the caller's personal tokens",
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   res={'tokens': '[#Token]'},
   err='forbidden')

op('POST me/tokens', 'createToken',
   'Make a personal token that acts as the caller',
   who='Owner, admin or member, from a web or app session. Not guests. Needs a fresh check.',
   desc=('The secret starts `churr_pat_` and is returned only here. A personal token acts as the person, except that '
         'it cannot do anything that needs a fresh check, cannot touch passkeys, recovery, the authenticator app, '
         'identities, sessions or tokens, and cannot use sealed chats. Where company sign-in is required for the '
         "account's role, it stops working 30 days after the account's last company sign-in."),
   body={'label': 'str', 'expires_in_seconds?': ('int', '30 days, 90 days, a year, or absent for none.')},
   res={'token': '#Token', 'secret': 'str'},
   st=201,
   err='forbidden fresh_check_required')

op('DELETE me/tokens/{id}', 'revokePersonalToken',
   'Revoke a personal token',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   err='forbidden not_found')

op('GET me/devices', 'listDevices',
   "List the caller's devices",
   who='Any signed-in person.',
   desc='Retired devices are included until an unlocked device has taken them out of the device list; a retired device that is not in the newest list is not shown.',
   res={'devices': '[#Device]'})

op('PUT me/devices/current', 'registerDevice',
   'Register or update the device of the current session',
   who='Any signed-in person, from a web or app session.',
   desc=('One device per session. Idempotent: apps call it after sign-in, on every start, and whenever the push '
         'address changes. Sets the push address for wake-ups and, for sealed chats, its `sign_key` and `box_key`. A '
         '`push_kind` the server cannot serve is refused with `feature_off`.'),
   body='#DeviceInput',
   res={'device': '#Device'},
   err='forbidden feature_off')

op('DELETE me/devices/{id}', 'removeDevice',
   'Remove a device and end its session',
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   desc=('Revokes its session and retires it. An unlocked device of the account then publishes a device list without '
         "it. Removing another device that is in the account's device list (it may be in someone else's hands) also "
         'sets `replacement_due` on the backup, so the next unlocked device replaces the backup key.'),
   err='forbidden not_found')

op('GET status', 'getStatusSummary',
   'Get badge and unread totals',
   who='Any signed-in account.',
   desc=('Cheap: answered from memory. Carries a strong `ETag` and `Cache-Control: private, no-cache`; send '
         '`If-None-Match` to get `304` when nothing changed. Has its own limit of 30 a minute per session.'),
   res='#StatusSummary',
   also={'304': 'Nothing changed since the `ETag` sent in `If-None-Match`.'})

op('GET wake-ups/{event_id}', 'fetchWakeUp',
   'Fetch what a phone wake-up points at',
   who='The session of the device the wake-up was sent to.',
   desc=('`{event_id}` is the identifier the wake-up carried. One the server cannot read, that is older than seven '
         "days, that names another session's device, or whose conversation the account can no longer read, answers "
         '`not_found`. Does not move the read position. Limited to 60 a minute per device.'),
   res={'wake_up': '#WakeUp'},
   err='not_found')

op('PUT me/passkeys/{id}/unlock-key', 'setPasskeyUnlockKey',
   "Give a passkey's unlock public key",
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   desc=('The page that holds the passkey derives the key pair from its PRF output and gives the public half, which '
         'the server keeps with the passkey. An unlocked device that sees a passkey with an unlock key and no locked '
         'copy asks its owner before making one.'),
   body={'unlock_public': 'b64'},
   res={'passkey': '#Passkey'},
   err='forbidden not_found')

op('POST me/devices/reattach/begin', 'beginReattachDevice',
   'Start attaching an existing device to this new session',
   who='Any signed-in person, from a web or app session that has no device yet.',
   desc=('For the same app or browser signing in to the same account again after its session ended by itself. The '
         'device keeps its place in every sealed chat.'),
   body={'device_id': 'uuid'},
   res={'challenge': 'b64'},
   err='not_found forbidden')

op('POST me/devices/reattach/finish', 'finishReattachDevice',
   'Prove the device\'s signing key and attach the device',
   who='Whoever began it.',
   desc='The signature is by the `sign_key` the device registered, over the challenge. A retired device cannot be reattached.',
   body={'device_id': 'uuid', 'signature': 'b64'},
   res={'device': '#Device'},
   err='not_found forbidden unauthenticated')


# ----------------------------------------------------------------------------
area('Status',
     'Status, availability, do not disturb, quiet hours, appear away and presets. All belong to the account, not to a channel.')

op('PUT me/status', 'setStatus',
   "Set the caller's status",
   who="Any signed-in account, bots included. A person's own script may use a personal token.",
   desc='Give the fields, or `preset_id` to apply a preset: its text, duration, switches and availability together.',
   body={'emoji?': 'str',
         'text?': ('str', 'Up to 100 characters.'),
         'until?': 'time',
         'duration_seconds?': ('int', 'Instead of `until`.'),
         'pause_notifications?': 'bool',
         'appear_away?': 'bool',
         'preset_id?': 'uuid'},
   res={'account': '#Me'},
   err='not_found')

op('DELETE me/status', 'clearStatus',
   "Clear the caller's status",
   who='Any signed-in account.',
   desc='Its two switches end with it.')

op('PUT me/availability', 'setAvailability',
   'Set in a meeting or out of office',
   who='Any signed-in person, or their own script with a personal token.',
   body={'value': 'enum:meeting|out_of_office', 'until?': 'time'},
   res={'account': '#Me'})

op('DELETE me/availability', 'clearAvailability',
   'Clear in a meeting or out of office',
   who='Any signed-in account.')

op('PUT me/dnd', 'setDnd',
   'Turn do not disturb on',
   who='Any signed-in account.',
   desc=('For a duration, until a time, or with neither until turned off. Independent of quiet hours: neither '
         'shortens the other. The answer lists every source that is on.'),
   body={'until?': 'time', 'duration_seconds?': 'int'},
   res={'dnd': '#Dnd'})

op('DELETE me/dnd', 'clearDnd',
   'Turn do not disturb off',
   who='Any signed-in account.',
   desc=('Makes the account available now: it clears a manual do not disturb, the pause switch of the current '
         'status, the current quiet period, and the pause from the current meeting. The answer lists any source '
         'still on, which after this call is none.'),
   res={'dnd': '#Dnd'})

op('GET me/quiet-hours', 'getQuietHours',
   'Get the quiet-hours schedule',
   who='Any signed-in account.',
   res={'quiet_hours?': '#QuietHours'})

op('PUT me/quiet-hours', 'setQuietHours',
   'Set the quiet-hours schedule',
   who='Any signed-in account.',
   desc="Times are in the account's time zone.",
   body={'start': 'hhmm', 'end': 'hhmm', 'days': 'days'},
   res={'quiet_hours': '#QuietHours'})

op('DELETE me/quiet-hours', 'removeQuietHours',
   'Switch the quiet-hours schedule off',
   who='Any signed-in account.',
   desc='Clears the schedule and any skip. The client keeps the old times to offer them again.')

op('PUT me/quiet-hours/skip', 'skipQuietHours',
   'Skip the next quiet period',
   who='Any signed-in account.',
   desc=("Affects the next period that has not started, and only that one. The answer's do not disturb state lists "
         'the sources still on.'),
   res={'quiet_hours': '#QuietHours', 'dnd': '#Dnd'},
   err='not_found')

op('DELETE me/quiet-hours/skip', 'unskipQuietHours',
   'Stop skipping the next quiet period',
   who='Any signed-in account.',
   res={'quiet_hours': '#QuietHours'},
   err='not_found')

op('PUT me/away', 'setAway',
   'Appear away while active',
   who='Any signed-in account.',
   desc='For a duration, until a time, or with neither until turned off.',
   body={'until?': 'time', 'duration_seconds?': 'int'},
   res={'account': '#Me'})

op('DELETE me/away', 'clearAway',
   'Stop appearing away',
   who='Any signed-in account.')

op('GET me/sidebar', 'getSidebar',
   "Read the caller's own sidebar",
   who='Any signed-in account.',
   desc='The whole layout in one call (ruling R83). Clients read it on connecting and follow `sidebar.changed`.',
   res={'sidebar': '#Sidebar'})

op('POST me/sidebar', 'changeSidebar',
   "Change the caller's own sidebar",
   who='Any signed-in person.',
   desc='Applies the changes in order, all or none, to the layout as it stood at `version`. If it has changed since, '
        '`conflict`: read it again and redo the change. At most 50 changes a call and 50 sections of one\'s own. '
        'Other connections get `sidebar.changed`.',
   body={'version': 'int', 'ops': '[#SidebarOp]'},
   res={'sidebar': '#Sidebar'},
   err='conflict')

op('GET status-presets', 'listStatusPresets',
   "List the community's status presets and the caller's own",
   who='Any signed-in account.',
   desc='The whole list. Presets have no event; clients fetch the list when the status picker opens.',
   res={'presets': '[#StatusPreset]'})

op('POST me/status-presets', 'createStatusPreset',
   'Save a personal status preset',
   who='Any signed-in person.',
   body='#StatusPresetInput',
   res={'preset': '#StatusPreset'},
   st=201)

op('PATCH me/status-presets/{id}', 'updateStatusPreset',
   'Change a personal status preset',
   who='Any signed-in person.',
   body='#StatusPresetInput',
   res={'preset': '#StatusPreset'},
   err='not_found')

op('DELETE me/status-presets/{id}', 'deleteStatusPreset',
   'Delete a personal status preset',
   who='Any signed-in person.',
   err='not_found')


# ----------------------------------------------------------------------------
area('Accounts',
     'Other accounts, groups and blocks.')

op('GET accounts', 'listAccounts',
   'List accounts the caller can see',
   who='Owner, admin and member see every account. Guests and bots see only accounts in conversations they are in.',
   q=['q:str:Match on username or display name.', 'role:str:Only this role.',
      'group_id:uuid:Only members of this group.', 'ids:str:Comma-separated identifiers, up to 100.',
      'include_removed:bool:Include former members.'],
   res={'accounts': '[#Account]'},
   page='cursor')

op('GET accounts/{id}', 'getAccount',
   'Get one account',
   who='Any account that can see it (see `listAccounts`).',
   res={'account': '#Account'},
   err='not_found')

op('GET groups', 'listGroups',
   'List groups',
   who='Owner, admin and member see every group. Guests and bots see the groups they are in and groups added to conversations they are in.',
   desc='The whole list.',
   res={'groups': '[#Group]'})

op('GET groups/{id}', 'getGroup',
   'Get one group',
   who='Any account that can see the group.',
   res={'group': '#Group'},
   err='not_found')

op('GET groups/{id}/members', 'listGroupMembers',
   'List the accounts in a group',
   who='Any account that can see the group.',
   res={'account_ids': '[uuid]'},
   err='not_found',
   page='cursor')

op('POST groups/{id}/members/me', 'joinGroup',
   'Join an open group',
   who='Owner, admin or member. The group must be open and not filled from a provider. Guests and bots are put in groups by admins.',
   desc="The caller is added to every channel the group is in, at the group's level there.",
   res={'group': '#Group'},
   err='forbidden not_found')

op('DELETE groups/{id}/members/me', 'leaveGroup',
   'Leave an open group',
   who='Owner, admin or member who is in the group, if it is open.',
   res={'group': '#Group'},
   err='forbidden not_found')

op('GET me/blocks', 'listBlocks',
   'List the accounts the caller has blocked',
   who='Any signed-in account.',
   res={'account_ids': '[uuid]'})

op('PUT me/blocks/{account_id}', 'blockAccount',
   'Block an account',
   who='Any signed-in person.',
   desc=('Its messages are collapsed for the caller and its mentions do not notify them. Whatever it starts with the '
         'caller, or adds them to, looks normal to it and never reaches them (ruling R77), and it always sees the '
         'caller online with no status.'),
   err='not_found feature_off')

op('DELETE me/blocks/{account_id}', 'unblockAccount',
   'Unblock an account',
   who='Any signed-in person.',
   err='not_found')

op('GET me/chat-requests', 'listChatRequests',
   'List the chat requests waiting for the caller',
   who='Any signed-in person.',
   desc=('Ruling R78. Oldest first. A request from someone the caller has blocked is not listed, and comes back if '
         'they are unblocked while it still waits. Each shows who is asking and nothing more.'),
   res={'requests': '[#ChatRequest]'})

op('POST me/chat-requests/{id}/accept', 'acceptChatRequest',
   'Accept a chat request',
   who='The person asked.',
   desc='The chat opens for both people.',
   res={'conversation': '#Conversation'},
   err='not_found conflict')

op('POST me/chat-requests/{id}/decline', 'declineChatRequest',
   'Decline a chat request',
   who='The person asked.',
   desc=('"I don\'t want to". The person asking sees that it was declined, and cannot ask the caller again for 24 '
         'hours.'),
   err='not_found conflict')

op('POST me/chat-requests/{id}/dismiss', 'dismissChatRequest',
   'Dismiss a chat request',
   who='The person asked.',
   desc='It leaves the caller\'s list. The person asking goes on seeing it wait, as if it had not been seen yet.',
   err='not_found conflict')

op('POST me/chat-requests/{id}/block', 'blockChatRequester',
   'Block the person asking',
   who='The person asked.',
   desc=('The same as `blockAccount` for the person asking. Their request leaves the list; to them it goes on '
         'waiting.'),
   err='not_found feature_off')

op('PUT me/watches/{account_id}', 'watchAccount',
   'Be told once when someone is next around',
   who=('Any signed-in person, for an account whose presence they are shown: someone they share a conversation '
        'with, on this server or another. Not for a bot, not for oneself. Refused with `forbidden` if the caller '
        'has blocked them.'),
   desc=('Ruling R76. When the person is next shown online (a client of theirs active in the last five minutes, '
         'and not appearing away), the caller is sent one reminder in their own one-person channel, "@ada is '
         'around", which notifies like any reminder; the watch is then done. It is a scheduled item of kind '
         '`watch`, listed and cancelled with the others, and expires unfired 48 hours after it is made. If the '
         'person is around already, as the caller may see them, nothing is stored and the answer is 200 with '
         '`around` true; someone who has blocked the caller always appears online to them (ruling R77), so that '
         'is the answer for them too. Otherwise 201 with the item. The person is never told. At most 5 waiting '
         'and one per person (`conflict`); at most 20 asked for a day (`rate_limited`).'),
   res={'around': ('bool', 'True when the person is around now and nothing was stored.'),
        'scheduled?': ('#ScheduledItem', 'The watch, when one was stored.')},
   err='forbidden not_found conflict',
   also={'201': 'The watch was stored.'})

op('DELETE me/watches/{account_id}', 'unwatchAccount',
   'Stop watching for someone',
   who='Any signed-in person, for their own watch.',
   desc='The same as cancelling the scheduled item.',
   err='not_found')
