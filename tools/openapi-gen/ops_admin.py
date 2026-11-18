"""Everything under /admin, and invites."""
from lib import area, op


# ----------------------------------------------------------------------------
area('Admin: settings and sign-in',
     'Server settings, which include the sign-in policy, and company sign-in providers.')

op('GET admin/settings', 'getSettings',
   'Get the server settings',
   who='Admins (the owner included).',
   desc='Every key of OPERATIONS.md section 5. Secrets are never returned.',
   res={'settings': '#Settings'},
   err='forbidden')

op('PATCH admin/settings', 'updateSettings',
   'Change server settings',
   who='Admins (the owner included). Changing `registration.mode`, `invites.members_may_invite` or an `auth.*` key needs a fresh check.',
   desc=('Effective at once, with no restart. Each change is written to the audit log with the old and new value. '
         'Setting `auth.member_sign_in` to `sso` while no enabled provider exists is refused with `conflict`, and so '
         'is requiring a second factor with nothing accepted. Changing the file store tests object storage first and '
         'refuses the change if the test fails.'),
   body='#Settings',
   res={'settings': '#Settings'},
   err='forbidden conflict fresh_check_required unavailable')

op('GET admin/sso', 'listSsoProviders',
   'List company sign-in providers',
   who='Admins (the owner included).',
   res={'providers': '[#SsoProvider]'},
   err='forbidden')

op('POST admin/sso', 'createSsoProvider',
   'Add a company sign-in provider',
   who='Admins (the owner included). Needs a fresh check.',
   desc=("For OIDC the server fetches the provider's discovery document now. For SAML the metadata is loaded once "
         'and not refreshed.'),
   body='#SsoProviderInput',
   res={'provider': '#SsoProvider'},
   st=201,
   err='forbidden fresh_check_required unavailable')

op('GET admin/sso/{id}', 'getSsoProvider',
   'Get a company sign-in provider',
   who='Admins (the owner included).',
   res={'provider': '#SsoProvider'},
   err='forbidden not_found')

op('PATCH admin/sso/{id}', 'updateSsoProvider',
   'Change a company sign-in provider',
   who='Admins (the owner included). Needs a fresh check.',
   body='#SsoProviderInput',
   res={'provider': '#SsoProvider'},
   err='forbidden not_found fresh_check_required')

op('DELETE admin/sso/{id}', 'deleteSsoProvider',
   'Remove a company sign-in provider',
   who='Admins (the owner included). Needs a fresh check.',
   desc=('Its identities are removed with it. Refused with `conflict` while member sign-in is `sso` and it is the '
         'last enabled provider.'),
   err='forbidden not_found conflict fresh_check_required')

op('POST admin/settings/test-storage', 'testObjectStorage',
   'Test object storage settings without saving them',
   who='Admins (the owner included).',
   desc='Writes, reads and deletes one small object. With no body it tests the stored settings.',
   body={'files.s3?': 'obj', 'files.s3_secret?': 'str'},
   res={'ok': 'bool', 'error?': 'str'},
   err='forbidden')

op('PUT admin/settings/icon', 'setServerIcon',
   "Upload the community's icon",
   who='Admins (the owner included).',
   multipart={'file': 'bin'},
   res={'settings': '#Settings'},
   err='forbidden too_large')

op('POST admin/sso/{id}/test', 'testSsoProvider',
   'Test a company sign-in provider',
   who='Admins (the owner included).',
   desc=('Fetches the discovery document and key set (OIDC) or checks the stored metadata (SAML), and says what it '
         'found. It signs nobody in.'),
   res={'ok': 'bool', 'error?': 'str'},
   err='forbidden not_found')


# ----------------------------------------------------------------------------
area('Admin: people',
     'Invites, accounts and roles, bans, time-outs and groups.')

op('GET invites', 'listInvites',
   'List invite links',
   who='Admins see every invite. A member sees the ones they made.',
   desc='The code is stored readable, so the link can be copied again.',
   res={'invites': '[#Invite]'},
   page='cursor')

op('POST invites', 'createInvite',
   'Make an invite link',
   who='Admins, for any invite. Members too, if `invites.members_may_invite` is on, and then only single-use `member` invites with no groups or channels that expire within seven days. Guest invites are made by admins only and must name at least one channel.',
   body={'role?': 'enum:member|guest',
         'group_ids?': '[uuid]',
         'conversation_ids?': ('[uuid]',
                               'For a guest: channels to start in. The admin must be able to add people to each.'),
         'max_uses?': ('int', 'Default 1. 0 means no limit.'),
         'expires_in_seconds?': 'int'},
   res={'invite': '#Invite'},
   st=201,
   err='forbidden not_found')

op('DELETE invites/{id}', 'revokeInvite',
   'Revoke an invite link',
   who='Admins, and the member who made it.',
   err='forbidden not_found')

op('GET admin/accounts', 'adminListAccounts',
   'List every account with sign-in details',
   who='Admins (the owner included).',
   q=['q:str:Match on username or display name.', 'role:str:Only this role.',
      'state:enum:active|removed|unclaimed:Only accounts in this state.'],
   res={'accounts': '[#AdminAccount]'},
   err='forbidden',
   page='cursor')

op('GET admin/accounts/{id}', 'adminGetAccount',
   'Get one account with sign-in details',
   who='Admins (the owner included).',
   res={'account': '#AdminAccount'},
   err='forbidden not_found')

op('PUT admin/accounts/{id}/role', 'setAccountRole',
   "Change an account's role",
   who='The owner only.',
   desc=('Between `admin`, `member` and `guest`. Setting `owner` hands the server over: the caller becomes an admin. '
         "A bot's role cannot be changed."),
   body={'role': 'enum:owner|admin|member|guest'},
   res={'account': '#AdminAccount'},
   err='forbidden not_found conflict fresh_check_required')

op('DELETE admin/accounts/{id}', 'removeAccount',
   'Remove an account',
   who='Admins (the owner included). An admin may remove members, guests and bots. Only the owner can remove an admin. Nobody can remove the owner.',
   desc=('Ends its sessions and tokens, deletes its passkeys, recovery codes, authenticator app and identities, '
         'revokes webhooks it stands behind, and removes it from every conversation. Its messages stay, shown under '
         'its name as a former member. Its username stays taken.'),
   err='forbidden not_found fresh_check_required')

op('POST admin/accounts/{id}/recovery-link', 'createRecoveryLink',
   'Make a one-time recovery link for an account',
   who='Admins, for a member or guest. The owner, for an admin too. Nobody through the API for the owner. Needs a fresh check.',
   desc=('Valid for 24 hours. Revokes any earlier unused link for the account. Both the making and the use are in '
         'the audit log.'),
   res={'url': 'str', 'expires_at': 'time'},
   st=201,
   err='forbidden not_found fresh_check_required')

op('DELETE admin/accounts/{id}/totp', 'adminRemoveTotp',
   "Reset an account's authenticator app",
   who='Admins (the owner included).',
   desc=('The person enrols again at their next sign-in, on the same trust as a first enrolment. Written to the '
         'audit log.'),
   err='forbidden not_found fresh_check_required')

op('GET admin/bans', 'listBans',
   'List bans',
   who='Admins (the owner included).',
   res={'bans': '[#Ban]'},
   err='forbidden',
   page='cursor')

op('POST admin/bans', 'createBan',
   'Ban an account or a sign-in identity',
   who='Admins (the owner included). Only the owner can ban an admin. Nobody can ban the owner.',
   desc=('A removal, plus rows that keep the same company identity out: one for the account and one for each company '
         'identity it had. Nothing recognises a person who registered with a passkey or an access key: they can register '
         'again, at once if registration is open, or with a new invite. Give `account_id`, or `provider_id` and '
         '`subject` to ban an identity that has no account.'),
   body={'account_id?': 'uuid', 'provider_id?': 'uuid', 'subject?': 'str', 'reason?': 'str'},
   res={'ban': '#Ban'},
   st=201,
   err='forbidden not_found fresh_check_required')

op('DELETE admin/bans/{id}', 'liftBan',
   'Lift a ban',
   who='Admins (the owner included).',
   desc='Does not bring a removed account back.',
   err='forbidden not_found')

op('POST admin/broadcast', 'sendBroadcast',
   'Show a notice to everyone connected',
   who='Admins (the owner included).',
   desc=('`/broadcast` (ruling R74). Replaces any broadcast showing. Every connection gets `server.broadcast` at '
         'once, and a client that connects while it shows finds it in `getStatusSummary`. Not a message: it is kept '
         'in memory only (a restart clears it) and written to the audit log, and it does not cross to linked '
         'servers. Ten an hour per admin (`rate_limited`).'),
   body='#BroadcastInput',
   res={'broadcast': '#Broadcast'},
   err='forbidden')

op('DELETE admin/broadcast', 'clearBroadcast',
   'Take the server broadcast down',
   who='Admins (the owner included).',
   desc='Every connection gets `server.broadcast` without one. `not_found` when none is showing. Written to the audit log.',
   err='forbidden not_found')

op('GET admin/channel-bans', 'listAllChannelBans',
   'List the channel bans in force in every named channel',
   who='Admins (the owner included).',
   desc=('Beside the server\'s own bans (`listBans`), for the console. Ad-hoc channels are left out. Lift one with '
         '`liftChannelBan`. At most 1,000, newest first.'),
   res={'bans': '[#ChannelBan]'},
   err='forbidden')

op('GET admin/timeouts', 'listTimeouts',
   'List time-outs in force',
   who='Admins (the owner included).',
   desc='Server-wide ones and those in public channels.',
   res={'timeouts': '[#Timeout]'},
   err='forbidden',
   page='cursor')

op('POST admin/timeouts', 'createTimeout',
   'Time an account out on the whole server',
   who='Admins (the owner included). Only the owner can time out an admin. Nobody can time out the owner.',
   desc='60 seconds to 28 days. The account can still read.',
   body='#TimeoutInput',
   res={'timeout': '#Timeout'},
   st=201,
   err='forbidden not_found')

op('DELETE admin/timeouts/{id}', 'liftTimeout',
   'Lift a time-out',
   who='Admins (the owner included).',
   err='forbidden not_found')

op('POST admin/groups', 'createGroup',
   'Create a group',
   who='Admins (the owner included).',
   body={'handle': 'str',
         'name': 'str',
         'description?': 'str',
         'color?': 'enum:lavender|cyan|pink|yellow|green|red|grey',
         'open?': 'bool',
         'provider_id?': ('uuid', 'Fill the group from this provider.'),
         'provider_group?': 'str'},
   res={'group': '#Group'},
   st=201,
   err='forbidden name_taken not_found')

op('PATCH admin/groups/{id}', 'updateGroup',
   'Change a group',
   who='Admins (the owner included).',
   desc='The built-in groups cannot be edited.',
   body={'handle?': 'str',
         'name?': 'str',
         'description?': 'str',
         'color?': 'enum:lavender|cyan|pink|yellow|green|red|grey',
         'open?': 'bool',
         'provider_id?': 'uuid',
         'provider_group?': 'str'},
   res={'group': '#Group'},
   err='forbidden not_found name_taken')

op('DELETE admin/groups/{id}', 'deleteGroup',
   'Delete a group',
   who='Admins (the owner included).',
   desc='Its members leave the channels they were in only through it.',
   err='forbidden not_found')

op('PUT admin/groups/{id}/members/{account_id}', 'addGroupMember',
   'Put an account in a group',
   who='Admins (the owner included).',
   desc='Local accounts only: people, guests and bots. Refused with `conflict` for a group filled from a provider.',
   err='forbidden not_found conflict')

op('DELETE admin/groups/{id}/members/{account_id}', 'removeGroupMember',
   'Take an account out of a group',
   who='Admins (the owner included).',
   err='forbidden not_found conflict')

op('POST admin/accounts/{id}/claim-link', 'createClaimLink',
   'Make a fresh claim link for an unclaimed imported account',
   who='Admins (the owner included).',
   desc='Revokes the earlier one. Valid for 30 days.',
   res={'invite': '#Invite'},
   st=201,
   err='forbidden not_found conflict fresh_check_required')

op('DELETE admin/accounts/{id}/sessions', 'signOutAccount',
   'Sign an account out everywhere',
   who='Admins (the owner included). Only the owner can do this to an admin.',
   desc=("Revokes every session and token of the account. An admin cannot see a member's session list. Written to "
         'the audit log.'),
   err='forbidden not_found')

op('POST admin/sessions/sign-out-all', 'signOutEveryone',
   'Sign everyone out',
   who='Admins (the owner included). Needs a fresh check.',
   desc=("Revokes every web and app session but the caller's own, so a changed sign-in policy takes hold at once. "
         'Personal and bot tokens are left alone.'),
   err='forbidden fresh_check_required')


# ----------------------------------------------------------------------------
area('Admin: channels and content',
     'Sections, the channel list, emoji, saved replies, status presets, word filters, reports and purge requests.')

op('POST admin/sections', 'createSection',
   'Create a section',
   who='Admins (the owner included).',
   body={'name': 'str', 'position?': 'int'},
   res={'section': '#Section'},
   st=201,
   err='forbidden')

op('PATCH admin/sections/{id}', 'updateSection',
   'Rename or move a section',
   who='Admins (the owner included).',
   body={'name?': 'str', 'position?': 'int'},
   res={'section': '#Section'},
   err='forbidden not_found')

op('DELETE admin/sections/{id}', 'deleteSection',
   'Delete a section',
   who='Admins (the owner included).',
   desc='Its channels stay, outside any section.',
   err='forbidden not_found')

op('GET admin/channels', 'adminListChannels',
   'List every named channel, public and private',
   who='Admins (the owner included).',
   desc=('Every public channel, and every named private channel with its name, parent, member count and managers and nothing '
         'else. Names are not unique. Ad-hoc channels and sealed chats never appear.'),
   q=['q:str:Match on name.', 'private:bool:Only private, or only public.',
      'archived:bool:Include archived channels.', 'parent_id:uuid:Only the sub-channels of this channel.'],
   res={'channels': '[#AdminChannel]'},
   err='forbidden',
   page='cursor')

op('GET admin/channels/{id}', 'adminGetChannel',
   "Get a named channel's name, members and managers",
   who='Admins (the owner included).',
   res={'channel': '#AdminChannel', 'members': '[#Member]'},
   err='forbidden not_found')

op('PATCH admin/channels/{id}', 'adminUpdateChannel',
   "Set a named channel's section, parent or retention period",
   who='Admins (the owner included).',
   desc=("An admin may set any retention period, or 0 for the server's, and may set or clear the channel's parent. "
         'This works on a private channel the admin is not in. A parent that is not a named channel is refused with '
         '`invalid_value`. A parent that is itself a sub-channel, and giving a parent to a channel that has '
         'sub-channels, answer `conflict`.'),
   body={'section_id?': 'uuid', 'retention_seconds?': 'int',
         'parent_id?': {'$raw': {'type': ['string', 'null'], 'format': 'uuid', 'description': 'The named channel to put this one under, or null to clear it.'}}},
   res={'channel': '#AdminChannel'},
   err='forbidden not_found conflict archived')

op('PUT admin/channels/{id}/managers/{account_id}', 'appointManager',
   'Make someone a manager of a named channel, from the admin console',
   who=('Owners and Admins, from a session checked in the last ten minutes. This works on a channel they are not in '
        'and cannot read.'),
   desc=('For a channel that has lost its managers or has a rogue one (rulings R99, R104). Someone in the channel is '
         'made a manager at once. Anyone else is invited as a manager and is one once they accept (ruling R103), and '
         'is returned marked `invited`: in a public channel anyone of this instance who may join it, in a knock channel '
         'anyone who may ask. In a private channel, or a locked one, `override` must be true: the invitation then brings '
         'them in by override, with the `owner_override` notice every member is shown when they join '
         '(`ownerOverrideAdd`). Guests, bots and accounts of other servers cannot be appointed. A '
         'channel a hub owns has the managers on the hub\'s list (`conflict`). Written to the audit log.'),
   body={'override?': ('bool', 'Invite the account by override if it is not in the private channel.')},
   res={'member': '#Member'},
   err='forbidden not_found conflict archived fresh_check_required')

op('DELETE admin/channels/{id}/managers/{account_id}', 'dismissManager',
   "Take a manager's level in a named channel away, from the admin console",
   who=('Owners and Admins, from a session checked in the last ten minutes. This works on a channel they are not in '
        'and cannot read.'),
   desc=('The person stays in the channel as a member (ruling R104). Every member is shown a `manager_dismissed` '
         'notice naming who did it, which nobody can turn off, and it is written to the audit log '
         '(`channel.manager_dismissed`). An Admin does not dismiss an Owner or another Admin; nobody dismisses an Owner '
         'but that Owner (`forbidden`). Someone who is not a manager there answers `not_found`; a manager through a '
         'group, and the channel\'s last manager, `conflict` (appoint another first). A channel a hub owns has the '
         'managers on the hub\'s list (`conflict`).'),
   res={'member': '#Member'},
   err='forbidden not_found conflict archived fresh_check_required')

op('PUT admin/channels/{id}/members/{account_id}', 'ownerOverrideAdd',
   'Invite someone to a private channel by override',
   who=('Owners and Admins, from a session checked in the last ten minutes. This works on a channel they are not in '
        'and cannot read, locked or not.'),
   desc=('Invites an active account of this instance, not a bot, at the channel\'s default level (ruling R103: nobody '
         'is placed anywhere unasked); it is returned marked `invited`. The invitation carries `override`; when they '
         'accept, every member is shown an `owner_override` notice naming who did it, which nobody can turn off. It is '
         'written to the audit log when sent. The person reads from their join, as any newcomer (PERMISSIONS.md '
         'section 4.4); whoever did it gains nothing. A '
         'public channel answers `conflict`; a channel shared with other servers, and a guest, `forbidden`; someone '
         'banned from the channel, `forbidden`.'),
   res={'member': '#Member'},
   err='forbidden not_found conflict already_exists archived fresh_check_required')

op('GET admin/modules', 'adminListModules',
   'List the built-in modules with what each declares',
   who='Admins (the owner included).',
   desc='Each with whether the server has it on. A module that panics is switched off and the audit log says so.',
   res={'modules': '[#Module]'},
   err='forbidden')

op('PUT admin/modules/{name}', 'setModule',
   'Switch a built-in module on or off for the whole server',
   who='Admins (the owner included).',
   desc=('Off, its commands are not offered or run anywhere, inline objects it claims stay text, and what it made is '
         'served as its text; on again, it is all served as before. A channel that switched it off itself stays off. '
         'Written to the audit log.'),
   body={'on': 'bool'},
   res={'module': '#Module'},
   err='forbidden not_found')

op('GET admin/emoji', 'adminListEmoji',
   'List custom emoji with who added each',
   who='Admins (the owner included).',
   res={'emoji': '[#Emoji]'},
   err='forbidden',
   page='cursor')

op('PATCH admin/emoji/{id}', 'renameEmoji',
   'Rename a custom emoji',
   who='Admins (the owner included).',
   body={'name': 'str'},
   res={'emoji': '#Emoji'},
   err='forbidden not_found name_taken')

op('DELETE admin/emoji/{id}', 'adminDeleteEmoji',
   'Delete a custom emoji',
   who='Admins (the owner included).',
   err='forbidden not_found')

op('POST admin/saved-replies', 'createCommunityReply',
   'Define a `/name` command that posts preset text for everyone',
   who='Admins (the owner included).',
   desc=('A name held back for a command to come, or a protected built-in or one of its aliases (every built-in but '
         'the fun and games, FEATURES.md 12.1), is refused with `name_taken` naming the command. A reply already '
         'holding a protected name may have its text changed and is ignored until renamed.'),
   body={'name': 'str', 'text': 'str'},
   res={'saved_reply': '#SavedReply'},
   st=201,
   err='forbidden name_taken')

op('PATCH admin/saved-replies/{id}', 'updateCommunityReply',
   'Change a community saved reply',
   who='Admins (the owner included).',
   desc=('A name held back for a command to come, or a protected built-in or one of its aliases (every built-in but '
         'the fun and games, FEATURES.md 12.1), is refused with `name_taken` naming the command. A reply already '
         'holding a protected name may have its text changed and is ignored until renamed.'),
   body={'name?': 'str', 'text?': 'str'},
   res={'saved_reply': '#SavedReply'},
   err='forbidden not_found name_taken')

op('DELETE admin/saved-replies/{id}', 'deleteCommunityReply',
   'Delete a community saved reply',
   who='Admins (the owner included).',
   err='forbidden not_found')

op('POST admin/status-presets', 'createCommunityPreset',
   'Define a status preset for everyone',
   who='Admins (the owner included).',
   body='#StatusPresetInput',
   res={'preset': '#StatusPreset'},
   st=201,
   err='forbidden')

op('PATCH admin/status-presets/{id}', 'updateCommunityPreset',
   'Change a community status preset',
   who='Admins (the owner included).',
   body='#StatusPresetInput',
   res={'preset': '#StatusPreset'},
   err='forbidden not_found')

op('DELETE admin/status-presets/{id}', 'deleteCommunityPreset',
   'Delete a community status preset',
   who='Admins (the owner included).',
   err='forbidden not_found')

op('GET admin/word-filters', 'listWordFilters',
   'List the word filter',
   who='Admins (the owner included).',
   res={'word_filters': '[#WordFilter]'},
   err='forbidden')

op('POST admin/word-filters', 'createWordFilter',
   'Add a word or pattern to the filter',
   who='Admins (the owner included).',
   desc=('`block` refuses the post or edit with `blocked_by_filter`. `flag` lets it through and files a report with '
         '`source` of `filter`, routed like any report.'),
   body={'pattern': 'str', 'is_regex?': 'bool', 'action': 'enum:block|flag'},
   res={'word_filter': '#WordFilter'},
   st=201,
   err='forbidden')

op('PATCH admin/word-filters/{id}', 'updateWordFilter',
   'Change a filter entry',
   who='Admins (the owner included).',
   body={'pattern?': 'str', 'is_regex?': 'bool', 'action?': 'enum:block|flag'},
   res={'word_filter': '#WordFilter'},
   err='forbidden not_found')

op('DELETE admin/word-filters/{id}', 'deleteWordFilter',
   'Remove a filter entry',
   who='Admins (the owner included).',
   err='forbidden not_found')

op('GET admin/purge-requests', 'listPurgeRequests',
   'List purge requests from public channels',
   who='Admins (the owner included).',
   desc='While history is hidden, and after `hidden`, admins can still open it with `getMessageHistory`.',
   q=['waiting:bool:Only requests nobody has decided.'],
   res={'purge_requests': '[#PurgeRequest]'},
   err='forbidden',
   page='cursor')

op('POST admin/purge-requests/{id}/decide', 'decidePurgeRequest',
   'Decide a purge request',
   who='Admins (the owner included).',
   desc=('`shown` shows the history again to everyone who can read the channel. `hidden` keeps it hidden but kept '
         'for admins. `erased` carries out the purge. The author is not told which, and the outcome is never '
         'returned to a non-admin.'),
   body={'outcome': 'enum:shown|hidden|erased'},
   res={'purge_request': '#PurgeRequest'},
   err='forbidden not_found conflict')


# ----------------------------------------------------------------------------
area('Admin: bots and integrations',
     'Bots and their tokens, the webhook queue, and server integrations.')

op('GET admin/bots', 'listBots',
   'List bots',
   who='Admins (the owner included).',
   q=['webhooks:bool:Include the accounts behind webhooks.'],
   res={'bots': '[#Bot]'},
   err='forbidden',
   page='cursor')

op('POST admin/bots', 'createBot',
   'Create a bot',
   who='Admins (the owner included). Needs a fresh check.',
   desc=('Makes an account with role `bot` and its first token, which starts `churr_bot_` and is returned only here. '
         'The bot is in no channel until someone adds it.'),
   body={'username': 'str', 'display_name': 'str', 'description?': 'str'},
   res={'bot': '#Bot', 'token': '#Token', 'secret': 'str'},
   st=201,
   err='forbidden name_taken fresh_check_required')

op('GET admin/bots/{id}', 'getBot',
   'Get a bot',
   who='Admins (the owner included).',
   res={'bot': '#Bot'},
   err='forbidden not_found')

op('PATCH admin/bots/{id}', 'updateBot',
   "Change a bot's name or description",
   who='Admins (the owner included).',
   body={'username?': 'str', 'display_name?': 'str', 'description?': 'str'},
   res={'bot': '#Bot'},
   err='forbidden not_found name_taken')

op('PUT admin/bots/{id}/avatar', 'setBotAvatar',
   "Upload a bot's avatar",
   who='Admins (the owner included).',
   multipart={'file': 'bin'},
   res={'bot': '#Bot'},
   err='forbidden not_found too_large')

op('DELETE admin/bots/{id}', 'deleteBot',
   'Remove a bot',
   who='Admins (the owner included).',
   desc='Removed like any account: tokens end, it leaves every channel, its messages stay.',
   err='forbidden not_found')

op('GET admin/bots/{id}/tokens', 'listBotTokens',
   "List a bot's tokens",
   who='Admins (the owner included).',
   res={'tokens': '[#Token]'},
   err='forbidden not_found')

op('POST admin/bots/{id}/tokens', 'createBotToken',
   'Make another token for a bot',
   who='Admins (the owner included). Needs a fresh check.',
   desc='At most ten live tokens per bot. A token does not expire.',
   body={'label?': 'str'},
   res={'token': '#Token', 'secret': 'str'},
   st=201,
   err='forbidden not_found conflict fresh_check_required')

op('DELETE admin/bots/{id}/tokens/{token_id}', 'revokeBotToken',
   'Revoke a bot token',
   who='Admins (the owner included).',
   err='forbidden not_found')

op('GET admin/webhooks', 'listWebhooks',
   'List webhooks and waiting requests on the whole server',
   who='Admins (the owner included).',
   q=['state:enum:waiting|approved|declined|revoked:Only this state.'],
   res={'webhooks': '[#Webhook]'},
   err='forbidden',
   page='cursor')

op('GET admin/integrations', 'listIntegrations',
   'List server integrations',
   who='Admins (the owner included).',
   res={'integrations': '[#Integration]'},
   err='forbidden')

op('POST admin/integrations', 'createIntegration',
   'Create a server integration',
   who='Admins (the owner included). Needs a fresh check.',
   desc=('The token starts `churr_int_` and is returned only here. A lost token is replaced by making a new '
         'integration.'),
   body={'name': 'str', 'allows': '[enum:availability]'},
   res={'integration': '#Integration', 'secret': 'str'},
   st=201,
   err='forbidden fresh_check_required')

op('DELETE admin/integrations/{id}', 'revokeIntegration',
   'Revoke an integration',
   who='Admins (the owner included).',
   err='forbidden not_found')


# ----------------------------------------------------------------------------
area('Admin: linked servers',
     'Peers and streams for sharing channels with other servers (LINK-PROTOCOL.md). Owner only. Every one of these is written to the audit log.')

op('GET admin/peers', 'listPeers',
   'List links to other servers and hubs',
   who='The owner only.',
   res={'peers': '[#Peer]'},
   err='forbidden')

op('POST admin/peers/invitations', 'createPeerInvitation',
   'Make an invitation to link another server to this one',
   who='The owner only.',
   desc=("One string starting `churr_link_`, holding this server's name, address, key fingerprint and a one-time "
         'secret. Valid for seven days unless a shorter time is given. This server listens; the one that accepts '
         'dials. A direct link joins exactly two servers: three or more need a hub.'),
   body={'expires_in_seconds?': 'int'},
   res={'peer': '#Peer', 'invitation': ('str', 'Text to hand to the other owner. Shown once.'), 'expires_at': 'time'},
   st=201,
   err='forbidden fresh_check_required')

op('POST admin/peers', 'acceptPeerInvitation',
   'Accept an invitation from another server, or join a hub',
   who='The owner only.',
   desc=('This server dials the other side, each proves its key, the secret proves the invitation, and the keys are '
         'pinned.'),
   body={'invitation': 'str'},
   res={'peer': '#Peer'},
   st=201,
   err='forbidden conflict unavailable already_exists fresh_check_required')

op('GET admin/peers/{id}', 'getPeer',
   'Get a peer, with the servers on a hub',
   who='The owner only.',
   res={'peer': '#Peer'},
   err='forbidden not_found')

op('PATCH admin/peers/{id}', 'updatePeer',
   'Pause or resume a peer',
   who='The owner only.',
   body={'state': 'enum:active|paused'},
   res={'peer': '#Peer'},
   err='forbidden not_found')

op('DELETE admin/peers/{id}', 'removePeer',
   'Remove a peer, or revoke an unused invitation',
   who='The owner only.',
   desc=('Streams this server does not own are left. Streams it owns must first be handed over or closed (`conflict` '
         'otherwise). Each shared channel stays as an archived local channel with the history it has; cross-server '
         'ad-hoc channels and sealed chats become read-only.'),
   err='forbidden not_found conflict')

op('GET admin/servers', 'listKnownServers',
   'List the other servers this one knows',
   who='The owner only.',
   res={'servers': '[#KnownServer]'},
   err='forbidden')

op('PUT admin/servers/{id}/block', 'blockServer',
   'Refuse everything from a server',
   who='The owner only.',
   err='forbidden not_found')

op('DELETE admin/servers/{id}/block', 'unblockServer',
   'Stop refusing a server',
   who='The owner only.',
   err='forbidden not_found')

op('GET admin/streams', 'listStreams',
   'List shared channels, and offers received and sent',
   who='The owner only.',
   q=['state:enum:offered|active|left|removed:Only this state.'],
   res={'streams': '[#Stream]'},
   err='forbidden')

op('POST admin/streams', 'shareChannel',
   'Share a channel over a peer',
   who='The owner only. The channel must be a named channel the owner can read.',
   desc=('Over a direct link this server owns the stream and sends the other server an offer; an offer nobody answers '
         'lapses after seven days. Over a hub the hub owns the channel from the start (LINK-PROTOCOL.md section 23a): '
         'the caller becomes its first manager, the channel\'s name and topic become the name and description the '
         'hub records, and each server named is invited in the caller\'s name. Sharing over a hub that is not '
         'connected answers `conflict`.'),
   body={'conversation_id': 'uuid',
         'peer_id': 'uuid',
         'server_ids?': ('[uuid]', 'On a hub: which servers to invite, if any. On a direct link there is one.'),
         'listing?': ('enum:unlisted|request|open',
                      'On a hub only: the channel\'s listing mode. `unlisted` unless given.'),
         'history_days?': ('int', '0 to 90. How much history to send.', {'minimum': 0, 'maximum': 90}),
         'max_blob_bytes?': ('int',
                             'The largest file the stream will carry. Default: `links.default_max_file_bytes`.')},
   res={'stream': '#Stream'},
   st=201,
   err='forbidden not_found conflict unavailable fresh_check_required')

op('POST admin/streams/{id}/accept', 'acceptStream',
   'Accept an offered channel as a new local channel',
   who='The owner only.',
   desc=('The accepting owner picks its local name, which need not be unique, its section, and whether it is public or private here. '
         'An offer of a channel on a hub is a manager\'s invitation: accepting it joins the channel. If the other '
         'side refuses the acceptance, the answer is `conflict`.'),
   body={'name': ('str', 'A channel name: 1 to 64 characters of any script. Spaces and capitals are allowed; no control characters, no leading or trailing space, and runs of spaces are collapsed to one. Not unique.', {'minLength': 1, 'maxLength': 64}), 'private?': 'bool', 'section_id?': 'uuid'},
   res={'stream': '#Stream', 'conversation': '#Conversation'},
   err='forbidden not_found conflict')

op('POST admin/streams/{id}/decline', 'declineStream',
   'Decline an offered channel',
   who='The owner only.',
   err='forbidden not_found conflict')

op('DELETE admin/streams/{id}', 'leaveStream',
   'Stop receiving a shared channel',
   who='The owner of a server that is in the stream and does not own it.',
   desc=('The local channel stays, archived, with the history it has; admins can unarchive it and carry on locally. '
         'The server that owns the stream cannot leave it this way (`conflict`).'),
   err='forbidden not_found conflict')

op('PUT admin/streams/{id}/servers/{server_id}', 'addStreamServer',
   'Offer a shared channel to another server',
   who=('The owner of the server that owns the stream. For a channel on a hub: an owner who is one of the channel\'s '
        'managers; it is that manager\'s invitation (`moderateHubChannel`).'),
   res={'stream': '#Stream'},
   err='forbidden not_found conflict unavailable fresh_check_required')

op('DELETE admin/streams/{id}/servers/{server_id}', 'removeStreamServer',
   'Remove a server from a shared channel',
   who=('The owner of the server that owns the stream. For a channel on a hub: an owner who is one of the channel\'s '
        'managers.'),
   desc='Starts a new key epoch. The removed server is sent nothing further.',
   res={'stream': '#Stream'},
   err='forbidden not_found conflict unavailable')

op('POST admin/streams/{id}/owner', 'transferStream',
   "Hand a stream's ownership to another server in it",
   who='The owner of the server that owns the stream.',
   desc=("With the other server's owner's agreement. On a direct link the new owner also becomes the stream's time reference. "
         'Ownership cannot be handed to a hub this way, and a channel a hub owns cannot be handed to anyone: to bring a '
         'direct-link channel onto a hub, use `proposeStreamMove`.'),
   body={'server_id': 'uuid'},
   res={'stream': '#Stream'},
   err='forbidden not_found conflict fresh_check_required')

op('POST admin/peers/invitations/inspect', 'inspectPeerInvitation',
   'Read an invitation before accepting it',
   who='The owner only.',
   desc="Shows the issuer's name and fingerprint so the owner can confirm before this server dials.",
   body={'invitation': 'str'},
   res={'kind': 'enum:server|hub', 'name': 'str', 'fingerprint': 'str', 'expires_at': 'time'},
   err='forbidden conflict')

op('PATCH admin/streams/{id}', 'updateStream',
   "Change a shared channel's file size limit",
   who='The owner of the server that owns the stream.',
   body={'max_blob_bytes': 'int'},
   res={'stream': '#Stream'},
   err='forbidden not_found')

op('POST admin/streams/{id}/close', 'closeStream',
   'Stop sharing a channel with every server',
   who=('The owner of the server that owns the stream. For a channel on a hub: an owner who is one of the channel\'s '
        'managers.'),
   desc='Every server detaches and keeps its copy.',
   err='forbidden not_found conflict unavailable')

op('GET admin/peers/{id}/directory', 'listHubDirectory',
   "List the channels in a hub's directory",
   who='The owner only.',
   desc=('The channels on the hub whose listing mode is `request` or `open`, as the hub signs them: name, description, '
         'listing mode, managers and the number of servers. Unlisted channels are never in it. The peer must be a hub '
         'and connected.'),
   res={'channels': '[#HubDirectoryEntry]'},
   err='forbidden not_found unavailable')

op('POST admin/peers/{id}/directory/join', 'joinHubChannel',
   "Join a channel from a hub's directory, or ask to",
   who='The owner only.',
   desc=('Makes a new local channel for it, as `acceptStream` does. An `open` channel is joined at once. For a `request` '
         'channel the hub records the request and the stream stays `offered` here until a manager approves it; if a '
         'manager declines, the stream becomes `removed` and the local channel stays as an archived local channel.'),
   body={'stream_id': 'uuid',
         'name': ('str', 'The local name, as for `acceptStream`.', {'minLength': 1, 'maxLength': 64}),
         'private?': 'bool', 'section_id?': 'uuid'},
   res={'stream': '#Stream', 'conversation': '#Conversation'},
   err='forbidden not_found conflict unavailable fresh_check_required')

op('POST admin/streams/{id}/move', 'proposeStreamMove',
   'Propose moving a direct-link channel onto a hub',
   who='The owner of the server that owns the stream.',
   desc=("The stream must be a channel shared over a direct link, and both of its servers must be on the hub named. "
         "Nothing changes until the other server's owner accepts; then the move runs by itself and is all or nothing "
         '(LINK-PROTOCOL.md section 21a). While a move is in progress a second one, handing over, closing, leaving, '
         'removing either link, and changing who is a manager of the channel are refused with `conflict`. Messages are '
         'never refused.'),
   body={'peer_id': ('uuid', "This server's link to the hub."),
         'expires_in_seconds?': ('int', 'How long the proposal stands. One day unless given; at most seven.',
                                 {'minimum': 60, 'maximum': 604800})},
   res={'move': '#StreamMove'},
   st=201,
   err='forbidden not_found conflict unavailable fresh_check_required')

op('GET admin/moves', 'listStreamMoves',
   'List moves of channels onto hubs, newest first',
   who='The owner only.',
   res={'moves': '[#StreamMove]'},
   err='forbidden')

op('POST admin/moves/{id}/accept', 'acceptStreamMove',
   'Accept a proposed move of a shared channel onto a hub',
   who="The owner of the stream's other server.",
   desc=("Signs this server's agreement, naming the hub by name and key and the managers of the local channel, who "
         'become managers at the commit. This server must itself be on that hub.'),
   res={'move': '#StreamMove'},
   err='forbidden not_found conflict unavailable fresh_check_required')

op('POST admin/moves/{id}/decline', 'declineStreamMove',
   'Decline a proposed move',
   who="The owner of the stream's other server.",
   res={'move': '#StreamMove'},
   err='forbidden not_found conflict')

op('POST admin/moves/{id}/cancel', 'cancelStreamMove',
   'Withdraw a move this server proposed',
   who='The owner of the server that proposed it.',
   desc='Possible until the move has been committed; `conflict` after.',
   res={'move': '#StreamMove'},
   err='forbidden not_found conflict')


# ----------------------------------------------------------------------------
area('Channels on a hub',
     ('A channel shared through a hub is owned by the hub (LINK-PROTOCOL.md section 23a). Its managers and moderators '
      'are accounts, named in the record the hub signs; their authority reaches every instance in the channel and does '
      'not depend on their role or level on their own instance. Managers hold the channel as an owner would; '
      'moderators keep the peace: they admit and remove people and remove messages.'))

op('GET streams/{id}/channel', 'getHubChannel',
   "Get the hub's record of a channel",
   who=("A member of the local channel, one of the channel's managers, or the owner. Only managers and the owner "
        'see which servers are invited or asking to join.'),
   res={'channel': '#HubChannel', 'manager': ('bool', 'Whether the caller is one of its managers.'),
        'moderator': ('bool', 'Whether the caller is one of its moderators.')},
   err='not_found')

op('POST streams/{id}/channel/actions', 'moderateHubChannel',
   'Act as a manager of a channel on a hub',
   who="One of the channel's managers whose account is on this server.",
   desc=("This server signs the action and the hub applies it if the account is on the channel's manager list; the "
         'answer is the record as the hub signed it afterwards. `invite`, `approve`, `decline` and `remove_server` name '
         'a server; `add_manager`, `remove_manager`, `add_moderator` and `remove_moderator` name an account, which '
         'must belong to an instance in the channel (a channel keeps at least one manager); `describe` changes the name, '
         'the description or both; `set_listing` the listing mode, which a private channel never changes; `close` ends '
         'the channel for every instance. Deleting a message for everyone is not an action here: a manager or moderator '
         'deletes it with `deleteMessage`; people are admitted and removed with `moderateHubChannelMember`.'),
   body={'action': 'enum:invite|approve|decline|remove_server|add_manager|remove_manager|add_moderator|remove_moderator|'
                   'describe|set_listing|close',
         'server?': ('str', "A server's name."),
         'account_id?': 'uuid',
         'name?': ('str', "The channel's name on the hub.", {'minLength': 1, 'maxLength': 64}),
         'description?': ('str', 'At most 1,000 characters.', {'maxLength': 1000}),
         'listing?': 'enum:unlisted|request|open'},
   res={'channel': '#HubChannel'},
   err='forbidden not_found conflict unavailable')

op('POST streams/{id}/channel/requests', 'askToJoinHubChannel',
   'Ask to be admitted to a private channel on a hub',
   who=('Owner, admin and member accounts of an instance in the channel that are not in it. Nobody outside a private '
        'channel sees it: a moderator or manager gives the person its identifier, the join code.'),
   desc=('The request travels to every instance in the channel, where its moderators and managers see it '
         '(`listHubChannelRequests`). Asking again while a request waits changes nothing. Ten requests an hour that '
         'were not accepted, counted with knocks.'),
   res={'request': '#HubPerson'},
   err='forbidden not_found conflict rate_limited')

op('GET streams/{id}/channel/requests', 'listHubChannelRequests',
   'List the requests to join a private channel on a hub',
   who="One of the channel's moderators or managers whose account is on this instance.",
   desc='The requests made on every instance in the channel and not yet answered, oldest first.',
   res={'requests': '[#HubPerson]'},
   err='forbidden not_found')

op('GET streams/{id}/channel/candidates', 'listHubChannelCandidates',
   'List the people a moderator or manager can admit to a private channel on a hub',
   who="One of the channel's moderators or managers whose account is on this instance.",
   desc=('The active accounts this instance knows, of the instances in the channel, that are not in it: every person of '
         'this instance, and those of other instances this one has met in a shared conversation. Anyone else asks to '
         'join with the join code. At most 50.'),
   q=['q:str:Match on username or display name.'],
   res={'people': '[#HubPerson]'},
   err='forbidden not_found')

op('POST streams/{id}/channel/members', 'moderateHubChannelMember',
   'Admit, decline or remove someone in a channel on a hub',
   who=("One of the channel's moderators or managers whose account is on this instance. The account may be of any "
        'instance in it.'),
   desc=('`admit` lets the person into a private channel: their own instance adds them to its copy, and their arrival is '
         'announced on every instance. `decline` answers their request to join one. `remove` takes them out on every '
         'instance, in any channel on a hub. Each instance checks for itself that the acting account is on the moderator '
         'or the manager list the hub signed. In a private channel on a hub nobody else adds people: not a local '
         'manager who is not on the lists, not an admin.'),
   body={'action': 'enum:admit|decline|remove', 'account_id': 'uuid'},
   err='forbidden not_found conflict invalid_value unavailable')


# ----------------------------------------------------------------------------
area('Admin: records',
     'The audit log, the export and server status.')

op('GET admin/audit', 'listAuditLog',
   'Read the audit log, newest first',
   who='Admins (the owner included).',
   q=['actor_id:uuid:Only this actor.', 'action:str:Only this action, or a prefix such as `account.`.',
      'target_id:str:Only this target.', 'since:time:Only entries at or after this time.',
      'until:time:Only entries before this time.'],
   res={'entries': '[#AuditEntry]'},
   err='forbidden',
   page='cursor')

op('POST admin/export', 'startExport',
   "Start a full export of the community's data",
   who='Owners only, from a browser or app session checked in the last ten minutes; never a personal token.',
   desc=('Includes private and ad-hoc channels. Leaves sealed chats out entirely. One export runs at a time. Written '
         'to the audit log, and every Owner is told.'),
   res={'export': '#Export'},
   st=201,
   err='forbidden conflict fresh_check_required')

op('GET admin/export', 'getExport',
   'See the state of the latest export',
   who='Owners only.',
   res={'export?': '#Export'},
   err='forbidden')

op('POST admin/export/download', 'createExportDownload',
   'Get a one-time address to download the latest finished export',
   who='Owners only, from a browser or app session checked in the last ten minutes; never a personal token.',
   desc=('The address works once, for one minute, for the Owner who asked. Asking is the download as far as the '
         'audit log is concerned (`export.downloaded`), and every Owner is told.'),
   res={'url': 'str', 'expires_at': 'time'},
   st=201,
   err='forbidden not_found fresh_check_required')

op('GET admin/export/download', 'downloadExport',
   'Download the latest finished export',
   who='The Owner who asked for the address (`createExportDownload`).',
   q=['ticket!:str:The one-time value from `createExportDownload`.'],
   err='forbidden not_found',
   raw={'200': {'description': 'A zip archive.',
                'content': {'application/zip': {'schema': {'type': 'string', 'format': 'binary'}}}}})

op('GET admin/overview', 'getOverview',
   "Get what the admin console's Overview page shows",
   who='Admins (the owner included).',
   desc='There is no metrics endpoint; these are the numbers an operator needs, for anyone who wants to scrape them.',
   res={'overview': '#Overview'},
   err='forbidden')

op('GET admin/calls', 'listLiveCalls',
   'List the calls in progress',
   who='Admins (the owner included).',
   desc='Participant counts, not who.',
   res={'calls': '[#LiveCall]', 'media_address': 'str'},
   err='forbidden')

op('POST admin/calls/test', 'testCalls',
   'Test whether call media can reach the server',
   who='Admins (the owner included).',
   desc=("The admin's browser makes a media connection to the server with this offer and reports which of UDP and "
         'TCP worked. The quickest way to find a closed port or a wrong media address.'),
   body={'sdp': 'str'},
   res={'sdp': 'str', 'media_address': 'str'},
   err='forbidden feature_off')


area('Approvals',
     ('The instance\'s one queue of requests that wait for approval (FEATURES.md section 31): registrations '
      'without an invite while `registration.approval` is on, new channels while `channels.creation_approval` is on, '
      'and webhook requests. Owners and admins decide every kind; the accounts given the flag "may approve requests" '
      'decide registrations and channels, and see nothing else of the admin console.'))

op('GET approvals/summary', 'getApprovalSummary',
   'Get what the caller may approve and how many wait',
   who='Any signed-in account. Someone who may approve nothing is told so, with no kinds.',
   res={'summary': '#ApprovalSummary'})

op('GET approvals', 'listApprovals',
   'List the requests that wait for the caller',
   who='Owners, admins, and the accounts that may approve requests, for the kinds each may decide.',
   desc='Oldest first, each with its requester. A request past its end (14 days) has lapsed and is not listed.',
   q=['kind:enum:registration|channel|webhook:Only this kind.'],
   res={'approvals': '[#Approval]'},
   err='forbidden')

op('POST approvals/{kind}/{id}', 'decideApproval',
   'Approve or decline a request',
   who='Owners, admins, and the accounts that may approve requests, for the kinds each may decide.',
   desc=('Approving a registration lets its account sign in, as a member; approving a channel makes it in its '
         "requester's name, with them as its first manager; approving a webhook is `approveWebhook`. The requester is "
         'told the outcome and the reason (`approval.decided`), never who decided; an applicant is told when they next '
         'sign in. Written to the audit log with who decided. A request that does not wait any more is `not_found`.'),
   body={'approve': 'bool', 'reason?': ('str', 'Why it was declined, at most 500 characters. The requester is shown it.')},
   res={'approval': '#Approval'},
   err='forbidden not_found conflict')

op('POST approvals/channels', 'requestChannel',
   'Ask for a new channel while new channels wait for approval',
   who='An owner, admin or member whose new channels wait for approval (`createChannel` answered `approval_required`).',
   desc=('Takes what `createChannel` takes, and a note, checked as making the channel would be. The request waits '
         'for 14 days. Someone whose channels need no approval is answered `conflict`: they make it.'),
   body={'name': 'str', 'private?': 'bool', 'access?': 'enum:public|knock|private', 'topic?': 'str', 'welcome?': 'str',
         'default_level?': 'enum:reader|member', 'section_id?': 'uuid', 'account_ids?': '[uuid]', 'parent_id?': 'uuid',
         'announcement?': 'bool', 'announcement_replies?': 'bool',
         'note?': ('str', 'For whoever decides, at most 500 characters.')},
   res={'approval': '#Approval'},
   st=201,
   err='forbidden not_found conflict rate_limited')

op('PUT admin/accounts/{id}/may-approve', 'setMayApprove',
   'Give an account the flag "may approve requests", or take it away',
   who='Admins (the owner included).',
   desc=('A flag, not a role: its holder decides registrations and channel requests in the approvals queue, and sees '
         'nothing else of the admin console. Only an active owner, admin or member can be given it. Written to the '
         'audit log; the account gets `approvals.changed`.'),
   body={'may_approve': 'bool'},
   res={'may_approve': 'bool'},
   err='forbidden not_found conflict')
