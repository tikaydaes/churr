"""Every object the API returns or accepts. The notation is explained in lib.py."""
from lib import S, RAW


# ----------------------------------------------------------------------------
# Basics

S('Error', 'The error shape every failed request returns (CONVENTIONS.md, Errors).', {
   'error': {'code': ('enum:invalid_request|invalid_value|unauthenticated|second_factor_required|forbidden|timed_out|archived|locked|feature_off|fresh_check_required|not_found|conflict|name_taken|already_exists|too_large|blocked_by_filter|rate_limited|slow_mode|internal|unavailable',
                      'What a client acts on.'),
             'message': ('str',
                         'A plain sentence a client may show. `name_taken` is for usernames, group handles, emoji and command names, and reserved names; never for a channel name, since those need not be unique.'),
             'field?': ('str', 'With `invalid_value`: the request field that was wrong.'),
             'retry_after_seconds?': ('int', 'With `rate_limited` and `slow_mode`.')}})

S('Server', 'What a server says about itself before and after sign-in.', {
   'name': ('str', "The host name of the server's public address."),
   'community_name': ('str', "The community's display name (setting `server.name`)."),
   'time_zone': ('str', "The server's time zone (setting `server.time_zone`), an IANA name: the clients read typed times "
                        'in it for an account with no time zone of its own.'),
   'icon_url?': 'str',
   'version': 'str',
   'min_app_version': ('str', 'The lowest app version this server supports.'),
   'setup_needed': ('bool',
                    'True until the owner account has been made. Until then every other operation answers '
                    '`unavailable`.'),
   'sign_in': {'registration': 'enum:open|invite|closed',
               'member_sign_in': 'enum:passkey|either|sso',
               'second_factor': 'enum:off|required',
               'access_keys': ('bool', 'New accounts may be made with an access key in place of a passkey (setting `auth.allow_access_keys`).'),
               'providers': ('[#SsoProviderSummary]',
                             'Company sign-in providers that are switched on. Empty when member sign-in is '
                             '`passkey`.')},
   'features': ({'sealed_chats': 'bool',
                 'calls': 'bool',
                 'link_previews': 'bool',
                 'blocking': 'bool',
                 'members_may_invite': 'bool',
                 'members_may_add_emoji': 'bool',
                 'broadcast_mentions_managers_only': ('bool',
                                                      '`@channel`, `@here` and `@everyone` are limited to channel '
                                                      'managers.')},
                'Features an admin can switch.'),
   'push_kinds': ('[enum:apns|fcm|unifiedpush|none]',
                  'The push kinds a device may register with. Always contains `none`. `apns` and `fcm` only while the '
                  'relay is enabled and `push.relay_url` is set; `unifiedpush` only while its switch is on.'),
   'limits': {'message_text': ('int', 'Characters.'),
              'file_size': ('int', 'Bytes.'),
              'quick_correction_seconds': 'int',
              'edit_window_seconds?': ('int', 'Absent means no limit.'),
              'retention_seconds?': ('int',
                                     "The server's retention period. Absent means messages are kept. Clients drop "
                                     'older messages from their own storage; no event is sent when retention removes '
                                     'one.'),
              'call_participants': 'int',
              'call_cameras': 'int'},
   'welcome?': ('str', 'The server welcome text, in Churr Markdown. Only sent to a signed-in caller.'),
   'default_themes': ('#DefaultThemes',
                      "The server's default dark and light themes, for members who have not chosen their own. Sent "
                      'before sign-in too, so a client can apply them at once.')})

S('SsoProviderSummary', 'A company sign-in provider as shown on the sign-in page.', {'id': 'uuid', 'kind': 'enum:oidc|saml', 'name': ('str', 'Shown on the sign-in button.')})

S('WellKnown', 'The document at `/.well-known/churr`, exactly as `docs/LINK-PROTOCOL.md` defines it. A hub serves the same document with `kind` `hub` and no box key.', {
   'churr': ('int', 'The document format: 1.'),
   'kind': 'enum:server|hub',
   'name': ('str', "The server's name: its public host name."),
   'sign_key': ('b64', 'Ed25519 public key.'),
   'box_key?': ('b64', 'X25519 public key. Servers only.'),
   'box_key_sig?': ('b64', 'The signing key\'s signature over the box key, which ties the two together. Servers only.'),
   'link': ({'versions': ('[int]', 'Link protocol versions spoken.'), 'url': ('str', 'The address other servers and hubs dial.')},
            'Where and how to link.')})


# ----------------------------------------------------------------------------
# Sign-in and the caller's own account

S('Ceremony', "The start of a passkey step. `public_key` is passed to the browser's passkey API as it is.", {
   'ceremony_id': ('str', 'Sent back with the result. Valid for five minutes, usable once.'),
   'public_key': ('obj',
                  'WebAuthn options in their standard JSON form. Creation options always carry the `prf` extension '
                  'input, so the new passkey is made with PRF where its store supports it. User verification is '
                  'required.')})

S('SessionUnlock', 'The start of unlocking a locked session.', {
   'ceremony_id': ('str', 'Sent back with the assertion or the access key. Valid for five minutes, usable once.'),
   'public_key?': ('obj', 'WebAuthn request options, limited to the account\'s own passkeys, user verification required. Absent when the account has no passkey.'),
   'access_key': ('bool', 'The account has an access key, which unlocks too.'),
   'unlock_url': ('str',
                  "The server's page that runs the assertion, with the ceremony after the `#`. An app opens it in the "
                  'system browser, because a passkey works only at the server\'s own web address (ruling R43).')})

S('CeremonyResult', 'The end of a passkey step.', {
   'ceremony_id': 'str',
   'credential': ('obj',
                  'The credential the passkey API returned, in its standard JSON form. The client removes any PRF '
                  'output (`clientExtensionResults.prf.results`) before sending: the server never sees a PRF value.')})

S('PasskeyCreated', 'The end of a passkey creation.', {
   'ceremony_id': 'str',
   'credential': ('obj', 'As in `CeremonyResult`.'),
   'passkey_name?': 'str',
   'prf_enabled?': ('bool',
                    'What the browser reported in `prf.enabled`. A hint for the account page, not a security '
                    'decision.')})

S('Session', 'One signed-in client, token or bot credential.', {
   'id': 'uuid',
   'kind': 'enum:web|app|personal|bot',
   'label': 'str',
   'signed_in_by?': 'enum:passkey|access_key|oidc|saml|recovery_code|recovery_link|pairing',
   'second_factor?': 'enum:passkey|totp|provider',
   'pending_second_factor?': ('bool',
                              'True while a second step is still owed. A pending session lasts ten minutes and can '
                              'only list its proofs, give one, enrol a first one, and sign out.'),
   'recovery?': ('bool',
                 'True for a session made by a recovery code or link. It lasts one hour and can only read the '
                 'profile, list and remove passkeys, list and revoke sessions, create a passkey or an access key, or '
                 '(where company sign-in is required) link an identity.'),
   'current?': ('bool', 'This is the session making the request.'),
   'locked_at?': ('time',
                  'Set while its holder has locked it (the lock). A locked session can only unlock, with a passkey '
                  'assertion of the account\'s own or its access key, and sign out.'),
   'created_at': 'time',
   'last_seen_at?': 'time',
   'expires_at?': ('time', 'Absent means until revoked.')})

S('SignIn', 'The result of a sign-in step. The response also sets the `churr_session` cookie, with a new token each time the session changes state.', {
   'session': '#Session',
   'account?': ('#Me', 'Absent while a second factor is owed.'),
   'second_factor?': ('#SecondFactorState', 'Present when `session.pending_second_factor` is true.'),
   'recovery_codes?': ('[str]', "Eight single-use codes, shown once, when an account's first passkey is created.")})

S('AccessKey', "The caller's access key: whether it exists and when it was made and used. Never the key.", {
   'created_at': 'time',
   'last_used_at?': 'time'})

S('SecondFactorState', 'The proofs a pending session may give.', {
   'accepted': ('[enum:passkey|totp]',
                "The proofs the admin accepts that a person can give here. The provider's own statement was already "
                'looked at.'),
   'set_up': ('[enum:passkey|totp]',
              'What this account already has. If empty, the person enrols one now, which counts as the proof.')})

S('Passkey', "A passkey on the caller's account.", {
   'id': 'uuid',
   'name': 'str',
   'has_prf': ('bool', 'False for a passkey from a store without PRF. It signs in but cannot unlock sealed chats.'),
   'unlock_public?': ('b64',
                      "The unlock public key derived from this passkey's PRF output, once the page that holds the "
                      'passkey has given it.'),
   'can_unlock': ('bool', 'A locked copy of the backup key exists for this passkey.'),
   'created_at': 'time',
   'last_used_at?': 'time'})

S('Totp', "State of the authenticator-app code on the caller's account.", {'enabled': 'bool', 'confirmed_at?': 'time'})

S('Identity', "A link between the caller's account and a company sign-in provider.", {'provider_id': 'uuid', 'provider_name': 'str', 'linked_at': 'time', 'last_used_at?': 'time'})

S('Token', 'A personal or bot token as listed. The secret is shown only when it is made.', {'id': 'uuid', 'label': 'str', 'created_at': 'time', 'last_seen_at?': 'time', 'expires_at?': 'time'})

S('Device', 'An app install or browser profile signed in to one account. Used for phone wake-ups and for sealed chats. A device outlives its session by up to 30 days.', {
   'id': 'uuid',
   'session_id?': ('uuid', 'Absent while the device has no session.'),
   'name': 'str',
   'platform': 'enum:android|ios|macos|windows|linux|web',
   'push_kind?': 'enum:apns|fcm|unifiedpush|none',
   'push_address?': ('str', 'Device token or UnifiedPush endpoint. Only shown to the account that owns the device.'),
   'wake_route?': ('str',
                   'Eleven characters the app keeps beside this server, to tell which server a wake-up came from '
                   '(PUSH.md). Only shown to the account that owns the device.'),
   'sign_key?': ('b64', 'Its Ed25519 public key, published when the device registers for sealed chats.'),
   'box_key?': ('b64', 'The device box key: its X25519 public key.'),
   'retired_at?': ('time',
                   'Set when the device had no session for 30 days or its session was revoked. An unlocked device of '
                   'the account then takes it out of the device list.'),
   'current?': 'bool',
   'created_at': 'time',
   'last_seen_at?': 'time'})

S('DeviceInput', 'Fields a client sets on its device.', {
   'name?': 'str',
   'platform?': 'enum:android|ios|macos|windows|linux|web',
   'push_kind?': 'enum:apns|fcm|unifiedpush|none',
   'push_address?': ('str', 'Up to 4,096 characters.'),
   'push_public_key?': ('b64', 'For a UnifiedPush distributor that wants Web Push encryption: its 65-byte P-256 public key. Give it with `push_auth` or not at all.'),
   'push_auth?': ('b64', 'The 16-byte Web Push auth secret that goes with `push_public_key`.'),
   'sign_key?': ('b64', 'Ed25519. Set once. A device with a different key is a new device.'),
   'box_key?': 'b64'})

S('Prefs', 'Personal preferences saved on the server.', {
   'thread_view': ('enum:threaded|flat', 'The default view of threads. A conversation can override it.'),
   'meeting_pauses': ('bool', 'Whether "in a meeting" also pauses notifications.'),
   'hide_last_seen': ('bool', 'Nobody else is told roughly when the caller was last here: their `last_seen` is absent for everyone.'),
   'auto_approve_direct': ('bool', 'A new direct chat with the caller opens at once. Off (the default): it is a chat request the '
                                   'caller accepts, declines, dismisses or blocks (ruling R78). Existing chats are not affected.'),
   'auto_approve_sealed': ('bool', 'The same for a new sealed chat with the caller.'),
   'allow_notify_anyway': ('bool',
                           "Whether senders may push one message through this person's do not disturb. Never shown "
                           'to anyone else.')})

S('Status', 'The status a person or bot sets.', {
   'emoji?': ('str', 'One standard emoji, or `:name:` for a custom one.'),
   'text?': ('str', 'Up to 100 characters.', {'maxLength': 100}),
   'until?': ('time', 'When it clears itself. Absent means until cleared.'),
   'pause_notifications': ('bool', 'Do not disturb for as long as the status lasts.'),
   'appear_away': ('bool', 'Appear away for as long as the status lasts.')})

S('Availability', 'In a meeting, or out of office.', {'value': 'enum:meeting|out_of_office', 'until?': 'time', 'set_by?': ('enum:self|integration', 'Who set it.')})

S('QuietHours', 'The daily schedule that puts the account in do not disturb.', {
   'start': 'hhmm',
   'end': 'hhmm',
   'days': ('days', 'Seven characters, Monday first, `1` for a day the schedule runs on: `1111100`.'),
   'time_zone': ('str', "IANA name. This is the account's `time_zone`."),
   'skip_until?': ('time', 'Set while the next quiet period is being skipped: the end of that period.')})

S('Dnd', "The caller's do not disturb in detail. Other people see only the mark.", {
   'on': ('bool', 'True when any source is on.'),
   'sources': ('[enum:manual|status|meeting|quiet]',
               'What is keeping it on now: a manual do not disturb, a status that pauses notifications, a meeting '
               'with the preference that pauses them, or quiet hours.'),
   'manual_until?': ('time', 'With `manual`: when it ends. Absent means until turned off.'),
   'until?': ('time', 'When do not disturb will end, if every source that is on has an end.')})

S('StatusPreset', 'A saved status.', {
   'id': 'uuid',
   'community': ('bool', 'True for a preset an admin defined for everyone.'),
   'emoji?': 'str',
   'text': 'str',
   'duration_seconds?': ('int', 'Absent means until cleared.'),
   'pause_notifications': 'bool',
   'appear_away': 'bool',
   'availability?': 'enum:meeting|out_of_office',
   'position': 'int'})

S('StatusPresetInput', 'Fields of a saved status.', {
   'emoji?': 'str',
   'text?': 'str',
   'duration_seconds?': 'int',
   'pause_notifications?': 'bool',
   'appear_away?': 'bool',
   'availability?': 'enum:meeting|out_of_office',
   'position?': 'int'})

S('StatusSummary', 'Totals for the icon badge (PUSH.md section 11). The body holds no clock time, so it changes only when something changed.', {
   'badge': ('int',
             "The number for the app icon: the sum of the badges of the account's conversations, each counted "
             'according to its mode. `show` plays no part: a hidden conversation counts like any other.'),
   'mentions': ('int', "The sum of the conversations' mention counts, hidden ones included."),
   'unread_conversations': ('int', "The account's conversations with anything unread, hidden ones included."),
   'unread_threads': ('int', 'Followed threads with unread replies.'),
   'newest?': ('str', 'Opaque. Changes whenever something newer exists for this account.'),
   'ringing_call_id?': ('uuid', 'A call that is ringing for this account right now.'),
   'dnd': ('bool', 'In do not disturb, from any cause.'),
   'dnd_until?': ('time', 'When that ends, if it is known.'),
   'broadcast?': ('#Broadcast', 'The server broadcast showing now (ruling R74), so that a client that connects while it shows shows it too. Absent when none is.')})

S('WakeUp', 'What a phone needs to draw one notification after a wake-up (PUSH.md section 9).', {
   'kind': 'enum:message|ring',
   'conversation': {'id': 'uuid',
                    'kind': 'enum:channel|sealed',
                    'name?': 'str',
                    'title?': ('str', 'For an ad-hoc channel or sealed chat: the title to show, which is the other members\' display names joined with commas.')},
   'messages': ('[#WakeUpMessage]',
                'Messages in that conversation that notify this account and are after its read position, newest '
                'last, at most five. In a sealed chat they carry ciphertext.'),
   'more': ('int', 'How many more there are beyond those.'),
   'read': ('bool', "The account's read position is already past the event."),
   'call?': ({'id': 'uuid',
              'from_account_id': 'uuid',
              'from_name': 'str',
              'ringing': ('bool', 'Whether it should still ring.')},
             'For a ring.'),
   'badge': ('int', "The account's total badge now.")})

S('WakeUpMessage', "A message with its author's display name beside it.", {'message': '#Message', 'author_name': 'str'})

S('OAuthTokens', 'The token response of RFC 6749, with two additions.', {
   'access_token': ('str', 'Starts `churr_app_`. Works for one hour.'),
   'token_type': 'enum:Bearer',
   'expires_in': ('int', 'Seconds. 3600.'),
   'refresh_token': ('str', 'Starts `churr_ref_`. Used once: each use returns a new pair.'),
   'device_id?': ('uuid', 'The device row made for this app.'),
   'handover?': ('b64',
                 'The backup key sealed to the `box_key` the app sent, when the sign-in page could open it '
                 '(SEALED.md). The server cannot read it.')})

S('OAuthError', 'The error shape of RFC 6749, used only by `/oauth/token` and `/oauth/revoke`.', {
   'error': 'enum:invalid_request|invalid_grant|unsupported_grant_type|authorization_pending|slow_down',
   'error_description?': 'str'})


# ----------------------------------------------------------------------------
# Accounts and groups

S('Account', 'A person, a bot, or the local shadow of someone on another server, as other accounts see it.', {
   'id': 'uuid',
   'username': 'str',
   'server_name?': ('str', 'Home server, for an account from another server. Absent for a local account.'),
   'role': ('enum:owner|admin|member|guest|bot|remote',
            '`remote` is the local shadow of an account on another server.'),
   'display_name': 'str',
   'avatar_file_id?': 'uuid',
   'avatar_url?': 'str',
   'title?': 'str',
   'pronouns?': 'str',
   'time_zone?': ('str', 'IANA name.'),
   'presence': ('enum:online|away|offline',
                'Worked out by the server. `away` also when the person chose to appear away or is out of office.'),
   'in_call?': 'bool',
   'last_seen?': ('enum:online|recently|within_week|long_ago',
                  'Roughly when the person was last here, never a time: `online` now, `recently` within three days, `within_week`, or `long_ago`. Only on an account fetched by itself (`getAccount`), for a caller who is shown its presence; absent for a bot, for an account of another server, and when the person hides it (`hide_last_seen`). The server never sends the exact time to anyone else.'),
   'status?': '#Status',
   'availability?': '#Availability',
   'dnd?': ('bool',
            'The do not disturb mark. Nothing says why it is on, and nothing says whether the person accepts notify '
            'anyway.'),
   'group_ids?': ('[uuid]', 'Groups the account is in, for labels beside the name. Built-in groups are left out.'),
   'claimed?': ('bool', 'False for an imported account nobody has claimed yet.'),
   'created_at': 'time',
   'removed_at?': ('time', 'Set for a former member.')})

RAW('Me', {'description': "The caller's own account: the public shape plus what only they see.",
     'allOf': [{'$ref': '#/components/schemas/Account'},
               {'type': 'object',
                'required': ['prefs', 'dnd_state', 'passkey_count', 'has_access_key', 'has_totp', 'identity_count'],
                'properties': {'prefs': {'$ref': '#/components/schemas/Prefs'},
                               'dnd_state': {'$ref': '#/components/schemas/Dnd'},
                               'appear_away': {'type': 'object',
                                               'required': ['on'],
                                               'properties': {'on': {'type': 'boolean'},
                                                              'until': {'type': 'string', 'format': 'date-time'}},
                                               'description': 'The manual appear-away switch.'},
                               'quiet_hours': {'$ref': '#/components/schemas/QuietHours'},
                               'passkey_count': {'type': 'integer', 'format': 'int64'},
                               'has_access_key': {'type': 'boolean'},
                               'has_totp': {'type': 'boolean'},
                               'identity_count': {'type': 'integer', 'format': 'int64'},
                               'blocked_ids': {'type': 'array', 'items': {'type': 'string', 'format': 'uuid'}}}}]})

S('Group', 'A named set of local accounts with a handle.', {
   'id': 'uuid',
   'handle': 'str',
   'name': 'str',
   'description': 'str',
   'color?': 'enum:lavender|cyan|pink|yellow|green|red|grey',
   'open': ('bool', 'Members may join and leave it themselves.'),
   'builtin?': ('enum:everyone|admins', 'Set on the two built-in groups, which cannot be edited.'),
   'member_count': 'int',
   'is_member': ('bool', 'Whether the caller is in it.'),
   'provider_id?': ('uuid', 'Admins only: the provider this group is filled from.'),
   'provider_group?': ('str', "Admins only: the provider's group name."),
   'created_at': 'time'})

S('Section', 'A heading the admins define to group channels in the list.', {'id': 'uuid', 'name': 'str', 'position': 'int'})

S('Sidebar', "A person's own sidebar (ruling R83): how they group and order this server's conversations for themselves. "
  'Headings are named by keys: `p:<n>` one of their own sections, `s:<id>` an admin section, `s:` channels in no section, '
  '`chats`, `sealed`. Empty lists mean the server\'s default.', {
   'version': ('int', 'Goes up by one with every change. 0 until the first.'),
   'sections': ('[#SidebarSection]', 'The person\'s own sections, oldest first.'),
   'headings': ('[str]', 'The person\'s order of channel headings. A heading missing from it goes after those in it, in the default order: own sections, then the admin\'s in their order, then `s:`. Chats and Sealed are always last.'),
   'groups': ('[#SidebarGroup]', 'Each heading whose sort is not the default.'),
   'placements': ('[#SidebarPlacement]', 'Conversations put in the person\'s own sections. Any other sits where the server lists it.'),
   'stars': ('[uuid]', 'The order of starred conversations, each at the top of its heading.')})

S('SidebarSection', "One of a person's own sections.", {
   'id': ('int', 'Its number; its heading key is `p:<id>`.'),
   'name': 'str'})

S('SidebarGroup', 'How one heading sorts.', {
   'key': 'str',
   'sort': ('enum:alphabetical|activity|custom', 'The default is alphabetical, and activity for `chats` and `sealed`.'),
   'order': ('[uuid]', 'The custom order. Conversations not in it follow, by name.')})

S('SidebarPlacement', "A conversation put in one of the person's own sections.", {
   'conversation_id': 'uuid',
   'section_id': 'int'})

S('SidebarOp', 'One change to a sidebar. Which fields count depends on `op`.', {
   'op': ('enum:create_section|rename_section|delete_section|move_section|set_sort|reorder|reset_section|reset_all|move|order_stars',
          '`create_section` (name, index?), `rename_section` (key, name), `delete_section` (key: its conversations go back where the server lists them), '
          '`move_section` (key, index), `set_sort` (key, sort, ids?), `reorder` (key, ids: sets the custom order and the sort to custom), '
          '`reset_section` (key: back to the default sort), `reset_all` (everything back to the server\'s default; stars stay), '
          '`move` (conversation_id, key: one of the person\'s sections, or any other heading to go back where the server lists it; ids? sets that heading\'s custom order), '
          '`order_stars` (ids).'),
   'key?': 'str',
   'name?': ('str', '1 to 64 characters.'),
   'index?': ('int', 'Where among the channel headings.'),
   'sort?': 'enum:alphabetical|activity|custom',
   'conversation_id?': 'uuid',
   'ids?': ('[uuid]', 'Conversations the caller is in, at most 2000.')})


# ----------------------------------------------------------------------------
# Conversations

S('Membership', "The caller's own place in a conversation, with the settings only they see.", {
   'level': 'enum:reader|member|manager',
   'via_group': ('bool', 'In the channel through a group. Such a member cannot leave.'),
   'direct?': ('bool', 'Also added directly.'),
   'joined_at': 'time',
   'read_seq': ('int', 'The read position.'),
   'mode': ('enum:any|mentions|nothing', 'What notifies: every message, mentions and followed threads, or nothing. Under `nothing` a mention of the account by name still counts in the badge. Says nothing about whether the conversation is shown.'),
   'muted_until?': ('time', 'While set, the conversation behaves as `nothing`; `mode` is what returns afterwards.'),
   'show?': ('enum:always|activity|notification|hidden', "Whether the conversation has a row in the person's sidebar: always, only while it has unread messages, only while it has a badge, or never. Absent means the app's own default, which is kept on the device. The server stores this and does not work out visibility; clients do. Independent of `mode`: a hidden conversation still notifies and still counts in badges and totals."),
   'starred': 'bool',
   'thread_view?': ('enum:threaded|flat', 'Override of the account preference for this conversation.'),
   'may_post': ('bool', 'A manager has allowed the caller to post here while the conversation is an announcement channel. Its managers, and an admin who acts as one, post without it.'),
   'ignore_broad_mentions': ('bool', '`@channel`, `@here` and `@everyone` do not notify the caller here and are not counted as mentions of them. A mention by name or of another group still is.'),
   'unread_count': ('int',
                    'Unread messages. Replies are counted only in the flat view. Ignored threads never count.'),
   'mention_count': 'int',
   'badge_count': ('int', 'What the mode would have notified. The app badge is the sum of these over every conversation, whatever its `show`.'),
   'timed_out_until?': ('time', 'The caller is timed out here until then.')})

S('Conversation', 'A channel (named or ad-hoc) or a sealed chat.', {
   'id': 'uuid',
   'kind': 'enum:channel|sealed',
   'private': ('bool', 'True for a knock channel and a private one: neither can be read or joined by someone outside it. A public channel can be made private or knock; a knock channel public again; a private one never either.'),
   'access?': ('enum:public|knock|private', 'How someone outside comes in (ruling R79): public (join freely), knock (seen by owner, admin and member accounts, who ask to join with `knock`), private (invitation only). Absent for a sealed chat.'),
   'manager_ids?': ('[uuid]', 'The managers of a knock channel, sent only to someone outside it, who is shown nothing else of who is in it, of what is said, or of a call in progress.'),
   'name?': ('str', 'Absent for an ad-hoc channel and for a sealed chat. Free-form: 1 to 64 characters of any script, spaces and capitals allowed. Not unique: any number of channels may share a name, and a channel is identified by its `id`. Clients sort and match names without regard to case.', {'minLength': 1, 'maxLength': 64}),
   'topic': 'str',
   'welcome?': ('str', 'Churr Markdown shown on joining.'),
   'section_id?': ('uuid', 'Ignored for display while the channel has a parent: a sub-channel is shown in its parent\'s section.'),
   'parent_id?': ('uuid', 'The named channel this one is a sub-channel of. One level only. Absent for a top-level channel, an ad-hoc channel and a sealed chat.'),
   'default_level': ('enum:reader|member', '`reader` makes it an announcement channel.'),
   'retention_seconds?': ('int', "Absent means the server's period."),
   'slow_mode_seconds': ('int', '0 is off.'),
   'broad_mentions': ('enum:server|anyone|managers', "Who may use `@channel`, `@here` and `@everyone` here: `server` follows the server's settings (its managers-only switch, and above `mentions.broad_mention_limit` members only the channel's managers and the admins), `anyone` lets every member at level `member` or above whatever the channel's size, `managers` only the accounts acting as manager. The server's managers-only switch, when on, applies whatever this says."),
   'lazy_delivery': ('enum:server|on|off', "Whether the channel is delivered lazily: in full only to the live connections that have it open, with `conversation.changed` to the others (`docs/EVENTS.md` section 4.1). `server` follows `channels.large_channel_limit`; `on` and `off` decide whatever its size. Always `server` outside a named channel."),
   'announcement': ('bool', 'An announcement channel: only its managers and the members they allow (`setMemberMayPost`) post; the others read and react, are not shown who else is in it, and nobody is shown typing or presence there.'),
   'announcement_replies': ('bool', 'In an announcement channel, whether members at level `member` or above who may not post may still reply in threads. Off unless a manager turns it on.'),
   'large': ('bool', "The conversation has more members than `channels.large_feature_limit`, or is an announcement channel: typing notices are not sent, and a client shows an online count (`presence.counts`) in place of each member's presence. Worked out when the conversation is sent; it can go out of date as people join and leave."),
   'broad_mentions_allowed?': ('bool', 'Whether the caller may use `@channel`, `@here` and `@everyone` here now. Present with `membership`; absent in a conversation sent to everyone in an event, where a client keeps the value it holds. It can go out of date as the channel grows; `checkBroadMentions` and the answer to `postMessage` are always right.'),
   'locked_at?': ('time',
                  'Set on a private channel that has been locked: nobody can be added or join, and someone who '
                  'leaves cannot return. Never cleared.'),
   'created_by_id?': 'uuid',
   'created_at': 'time',
   'archived_at?': 'time',
   'last_seq': ('int', 'The newest event number.'),
   'last_message_at?': 'time',
   'member_count': 'int',
   'member_ids?': ('[uuid]', 'For ad-hoc channels and sealed chats only, so a client can title them.'),
   'moved_from_message_id?': ('uuid', 'The message whose thread this channel was made from.'),
   'ticket_number?': 'int',
   'shared?': ({'stream_id': 'uuid', 'owner_server': 'str', 'servers': '[str]', 'max_blob_bytes?': 'int'},
               'Present when the conversation is shared with other servers.'),
   'read_only?': ('bool',
                  'Nothing can be posted: a conversation with people on another server that is no longer linked, a '
                  'sealed chat while sealed chats are off, or one whose other person was removed.'),
   'chat_state?': ('enum:waiting|declined|closed',
                   'A direct or sealed chat that cannot be written in (rulings R78, R79): `waiting` while the chat request '
                   'that started it waits for the other person, `declined` once they declined it, `closed` once either '
                   'person closed it. Absent while it is open. It never says who closed a chat.'),
   'key_epoch?': ('int', 'Sealed chats: the current key epoch. A message must be encrypted in it.'),
   'call?': ('#Call', 'The call in progress, if there is one.'),
   'membership?': ('#Membership', 'Absent when the caller is not a member, as when browsing a public channel.')})

S('Member', 'An account in a conversation.', {
   'conversation_id': 'uuid',
   'account_id': 'uuid',
   'level': ('enum:reader|member|manager', "The highest of the direct level and the levels of the member's groups."),
   'direct_level?': ('enum:reader|member|manager', 'Absent for a member who is in only through groups.'),
   'via_group': 'bool',
   'may_post': ('bool', 'A manager has allowed this member to post while the conversation is an announcement channel.'),
   'joined_at': 'time'})

S('ChannelGroup', 'A group added to a channel at a level.', {
   'conversation_id': 'uuid',
   'group_id': 'uuid',
   'level': 'enum:reader|member|manager',
   'added_by_id?': 'uuid',
   'added_at': 'time'})

S('Pin', 'A pinned message.', {'message_id': 'uuid', 'pinned_by_id': 'uuid', 'pinned_at': 'time', 'message?': '#Message'})

S('Timeout', 'A time-out: the account can read but cannot post or react.', {
   'id': 'uuid',
   'account_id': 'uuid',
   'conversation_id?': ('uuid', 'Absent for a time-out on the whole server.'),
   'until': 'time',
   'reason': 'str',
   'created_by_id': 'uuid',
   'created_at': 'time',
   'lifted_at?': 'time'})

S('TimeoutInput', 'A new time-out.', {'account_id': 'uuid', 'seconds': 'int', 'reason?': 'str'})

S('Report', 'A report of a message.', {
   'id': 'uuid',
   'message_id': 'uuid',
   'conversation_id': 'uuid',
   'source': ('enum:person|filter', '`filter` when the word filter flagged the message.'),
   'reported_by_id?': ('uuid', 'Absent when the word filter flagged the message.'),
   'reason': 'str',
   'created_at': 'time',
   'handled_by_id?': 'uuid',
   'handled_at?': 'time',
   'outcome?': ('enum:dismissed|deleted|timed_out', 'Absent while waiting.'),
   'message?': '#Message'})


# ----------------------------------------------------------------------------
# Messages, in the shapes of MESSAGE-FORMAT.md

S('Quote', 'What a reply quotes. No range means the whole message.', {'message_id': 'uuid', 'start?': ('int', 'UTF-16 code units into the quoted text.'), 'end?': 'int'})

S('Entity', 'Something the server resolved in the text. `at` and `len` count UTF-16 code units. A channel link is written `<#conversation-id>` in the text and becomes a `conversation` entity whose `at` and `len` cover the whole `<#…>`; clients draw it as the channel\'s current name. `#name` typed as plain characters is plain text and is never resolved.', {
   'type': 'enum:account|group|broadcast|conversation|emoji|link|task|instant|reference|message_link|measure|colour|event|phone|email|geo|dice|number|codepoint',
   'at': 'int',
   'len': 'int',
   'account_id?': 'uuid',
   'group_id?': 'uuid',
   'scope?': 'enum:channel|here',
   'conversation_id?': 'uuid',
   'emoji_id?': 'uuid',
   'url?': 'str',
   'item_id?': 'str',
   'time?': ('time', 'For `instant`: the moment the typed date and time names, in UTC (ruling R69). `at` is the position, so the moment is `time`.'),
   'precision?': ('enum:minute|second', 'For `instant` and `event`: what the author typed down to.'),
   'zone?': ('str', 'For `instant`: the zone as written (`Z`, an offset such as `+10:00`, or an IANA name), or the author\'s IANA zone when none was written.'),
   'pattern_id?': ('uuid', 'For `reference`: the reference pattern that matched (ruling R73). The pattern may since have been changed or removed.'),
   'value?': ('str', 'For `reference`: the value matched after the pattern\'s prefix. The address it resolved to when the text was written is `url`. For `measure`: the number as typed, with `-` for a minus sign. For `colour`: the colour as `#rrggbb` or `#rrggbbaa`, lower case. For `phone`: `+` and the digits. For `email`: the address as typed. For `dice`: the roll written out in full (`2d6+3`). For `number`: the number in decimal (below 2^128). For `codepoint`: `U+` and at least four upper-case hex digits; its name is looked up with `lookupCodePoints`.'),
   'base?': ('int', 'For `number`: the base it was typed in: 16, 2 or 8.'),
   'end?': ('time', 'For `event`: when it ends, in UTC; it starts at `time`, and `precision` and `zone` are for both (ruling R73).'),
   'lat?': ('str', 'For `geo`: the latitude in decimal degrees, as typed (-90 to 90).'),
   'lon?': ('str', 'For `geo`: the longitude in decimal degrees, as typed (-180 to 180).'),
   'terms?': ('[#DiceTerm]', 'For `dice`: each term as it fell, drawn by the server when the message was posted (ruling R73).'),
   'total?': ('int', 'For `dice`: the result.'),
   'removed?': ('bool', 'For `dice`: an edit wrote or changed these dice, and nothing was rolled for them.'),
   'unit?': ('str', 'For `measure`: the unit\'s code in the fixed table of MESSAGE-FORMAT.md section 3.2 (`km`, `mi`, `°C`, `lb`, `l`, `mph`, …).'),
   'message_id?': ('uuid', 'For `message_link`: the message the link names; its conversation is `conversation_id` and the link as typed `url`. The entity holds nothing of the message: a client fetches it with `getMessage`, which answers only a reader who may see it.')})

S('CodePointName', 'A Unicode code point\'s name and general category (`lookupCodePoints`).', {
   'code_point': ('str', '`U+` and at least four upper-case hex digits.'),
   'name': ('str', 'The character\'s name, such as `GRINNING FACE`. For a control character, its Unicode 1 name (`LINE FEED (LF)`) where it has one; for an unassigned code point or a private-use one without a name, empty.'),
   'category': ('str', 'The general category, two letters (`So`, `Lu`, `Cn` when unassigned).'),
   'category_name': ('str', 'The general category in words, such as `Other Symbol`.')})

S('ObjectHint', 'What the author chose in the composer for a range of the text (ruling R73). `plain` keeps the range as plain text: no object is recognised in it or reaching into it. Any other kind is an entity type the range is to be read as, where it could be more than one. A hint that does not fit the text is ignored without an error.', {
   'start': ('int', 'UTF-16 code units.'),
   'end': ('int', 'UTF-16 code units, after the last.'),
   'kind': 'enum:plain|reference|message_link|measure|colour|phone|email|geo|dice|number|codepoint'})

S('ReferencePattern', 'A pattern that turns text into a reference (ruling R73): `prefix` followed at once by a value. One with no `conversation_id` is the server\'s and applies everywhere; one with applies in that channel, ahead of the server\'s.', {
   'id': 'uuid',
   'conversation_id?': 'uuid',
   'name': 'str',
   'prefix': ('str', '1 to 16 letters, digits and `# _ . / : -`, such as `PROJ-` or `#`. Not ending in a digit; one with no letter or digit in it starts with `#`.'),
   'expression?': ('str', 'What the value matches, in the restricted form of MESSAGE-FORMAT.md section 3.2. Absent: 1 to 12 digits.'),
   'url_template': ('str', 'An http or https address with `{value}` where the value goes.'),
   'created_by_id?': 'uuid',
   'created_at': 'time'})

S('ReferencePatternInput', 'Fields of a reference pattern. A change may leave any out.', {
   'conversation_id?': ('uuid', 'On create only: the channel the pattern is for. Absent: the server\'s.'),
   'name?': ('str', '1 to 64 characters.'),
   'prefix?': 'str',
   'expression?': ('str', 'At most 64 characters. Empty for digits.'),
   'url_template?': ('str', 'At most 500 characters.')})

S('InstantHint', 'A range of the text the author\'s client resolved to an instant: a phrase such as "this Friday at 5pm" the author accepted in the composer (ruling R69). The server stores each hint that holds up as an `instant` entity and drops the rest without an error: the range must lie inside one run of plain text (not in code, a link\'s text, across formatting marks or over another entity) on character boundaries; `time` must be an RFC 3339 instant in the years 1000 to 9999; `zone` an offset or a known IANA name.', {
   'start': ('int', 'UTF-16 code units.'),
   'end': ('int', 'UTF-16 code units, after the last.'),
   'time': 'time',
   'precision': 'enum:minute|second',
   'zone': ('str', 'An offset (`Z`, `+10:00`) or an IANA name.')})

S('Task', 'The state of one checklist item.', {
   'id': ('str', 'Unique within the message.', {'pattern': '^[a-z0-9_-]{1,32}$'}),
   'checked': 'bool',
   'checked_by_id?': 'uuid',
   'checked_at?': 'time'})

S('File', 'An uploaded file.', {
   'id': 'uuid',
   'name': 'str',
   'size': ('int', 'Bytes.'),
   'media_type': ('str', 'Decided by the server from the content.'),
   'kind': 'enum:image|video|audio|text|file',
   'width?': 'int',
   'height?': 'int',
   'duration_seconds?': 'int',
   'caption?': 'str',
   'preview_text?': ('str', 'For `text`: the first 20 lines.'),
   'url?': ('str', 'Only a `ready` file has one.'),
   'thumb_url?': 'str',
   'poster_url?': ('str', 'For a video: the poster image the uploading client sent. The server makes none.'),
   'state?': ('enum:ready|arriving|unavailable|too_large',
              'For a file from another server in a shared conversation. Absent means `ready`.')})

S('CardField', 'A label and value on a card.', {'label': 'str', 'value': ('str', 'Inline Churr Markdown.'), 'short?': 'bool'})

S('Card', 'A fixed layout for structured information. A card must have at least a title, text or image.', {
   'color?': 'enum:lavender|cyan|pink|yellow|green|red|grey',
   'author?': {'name': 'str', 'icon_file_id?': 'uuid'},
   'title?': ('str', 'Plain text.', {'maxLength': 200}),
   'url?': 'uri',
   'text?': ('str', 'Churr Markdown, inline constructs and lists only.', {'maxLength': 4000}),
   'fields?': ('[#CardField]', 'Up to ten.', {'maxItems': 10}),
   'image_file_id?': 'uuid',
   'footer?': 'str',
   'at?': 'time',
   'preview_url?': ('uri',
                    'Set by the server on a link preview: the address it previews. A card without it was posted by a '
                    'bot or webhook.'),
   'theme?': ('#CardTheme',
              'Set by the server on a theme card: the card for a link to one of its own themes '
              '(`{origin}/themes/{id}`). It also has `preview_url` and is removed like a link preview. A client '
              'draws its swatches and offers to try and to add it.')})

S('FormOption', 'One option of a choice field.', {
   'id': ('str', 'Unique within the field.', {'pattern': '^[a-z0-9_-]{1,32}$'}),
   'label': 'str',
   'max?': ('int', 'Places for this option, used by the sign-up preset.'),
   'added_by_id?': ('uuid', 'Set on an option an answerer added.')})

S('FormField', 'One field of a form.', {
   'id': ('str', 'Unique within the form.', {'pattern': '^[a-z0-9_-]{1,32}$'}),
   'type': 'enum:choice|text|longtext|number|date|time|account|conversation|checkbox',
   'label': 'str',
   'help?': 'str',
   'required?': 'bool',
   'multiple?': ('bool', 'For `choice`, `account` and `conversation`.'),
   'options?': ('[#FormOption]', 'For `choice`. Up to 50.', {'maxItems': 50}),
   'min?': ('any', 'For `choice` the fewest options to pick; for `number` and `date` the lowest value.'),
   'max?': ('any', 'For `choice` the most options to pick; for `number` and `date` the highest value.'),
   'step?': 'num',
   'placeholder?': 'str',
   'max_length?': 'int'})

S('FormButton', 'A button on a form.', {
   'id': ('str', 'Unique within the form.', {'pattern': '^[a-z0-9_-]{1,32}$'}),
   'label': 'str',
   'style?': 'enum:default|primary|danger',
   'group_id?': ('uuid', 'For the `group` action: the open group this button is for.'),
   'group_action?': ('enum:join|leave', 'For the `group` action: what this button does.')})

S('FormAction', 'What the server does with an answer.', {
   'type': 'enum:tally|collect|thread|channel|group|bot',
   'name_prefix?': ('str', 'For `channel`.'),
   'group_id?': ('uuid', 'For `channel`: the group put in the new channel with the answerer.'),
   'level?': ('enum:reader|member|manager', 'For `channel`: the level the group is added at.')})

S('FormInput', 'A form as a client sends it when posting a message.', {
   'preset?': ('enum:poll|rsvp|signup|approve|checkin|ticket|roles',
               'Tells clients how to draw it. Absent for a form made with the builder.'),
   'title?': 'str',
   'fields': ('[#FormField]', 'Up to 20.', {'maxItems': 20}),
   'buttons?': ('[#FormButton]', 'Up to five.', {'maxItems': 5}),
   'action': '#FormAction',
   'anonymous?': ('bool', 'Cannot be changed after posting. Only with `tally` and `collect`; never on a `bot` form.'),
   'results?': ('enum:live|after_answer|after_close', 'For `tally` only.'),
   'allow_added_options?': 'bool',
   'answerer_group_id?': ('uuid',
                          'Only members of this group may answer or press. Only an account acting as manager of the '
                          'conversation may set it. Refused in a conversation shared with other servers, from a '
                          'webhook and in a dialog.'),
   'one_answer?': ('bool',
                   'Default true. False allows many answers per account, for `thread` and `channel` actions.'),
   'closes_at?': 'time'})

S('Form', 'How a message asks for input. One per message.', {
   'id': 'uuid',
   'preset?': ('enum:poll|rsvp|signup|approve|checkin|ticket|roles',
               'Tells clients how to draw it. Absent for a form made with the builder.'),
   'title?': 'str',
   'fields': ('[#FormField]', 'Up to 20.', {'maxItems': 20}),
   'buttons?': ('[#FormButton]', 'Up to five.', {'maxItems': 5}),
   'action': '#FormAction',
   'anonymous?': ('bool', 'Cannot be changed after posting. Only with `tally` and `collect`; never on a `bot` form.'),
   'results?': ('enum:live|after_answer|after_close', 'For `tally` only.'),
   'allow_added_options?': 'bool',
   'answerer_group_id?': ('uuid',
                          'Only members of this group may answer or press. Only an account acting as manager of the '
                          'conversation may set it. Refused in a conversation shared with other servers, from a '
                          'webhook and in a dialog.'),
   'one_answer?': ('bool',
                   'Default true. False allows many answers per account, for `thread` and `channel` actions.'),
   'closes_at?': 'time',
   'closed_at?': 'time',
   'answer_count': 'int',
   'tally?': ('map:map:int',
              'Counts by field id, then option id. Present only when `results` allows it for the reader.'),
   'voters?': ('map:map:[uuid]',
               'Account ids by field id, then option id. Present only when `results` allows it. Never present on an '
               'anonymous form.'),
   'my_answer?': ('#Answers', "The reader's own answer. Never sent to anyone else.")})

RAW('Answers', {'type': 'object',
     'description': 'Values by field id. `choice`, `account` and `conversation` fields hold a list of ids; `text` '
                    'and `longtext` a string; `number` a number; `date` `YYYY-MM-DD`; `time` `HH:MM`; `checkbox` '
                    'true or false.',
     'additionalProperties': True})

S('FormAnswer', 'One stored answer to a form.', {
   'id': 'uuid',
   'account_id?': ('uuid', 'Absent on an anonymous form.'),
   'button_id?': 'str',
   'answers': '#Answers',
   'created_at': ('time', 'On an anonymous form, the form\'s own creation time, so that nothing tells when an answer was given.'),
   'updated_at?': 'time'})

S('Reaction', 'One emoji on a message, with the accounts behind it.', {
   'emoji': ('str',
             "A standard emoji sequence, `:name:` for a custom one, or `:name@server.name:` for another server's."),
   'count': 'int',
   'me': ('bool', 'The reader reacted with it.'),
   'account_ids': ('[uuid]', 'At most the first ten.')})

S('ThreadSummary', 'Thread information on a top-level message that has replies.', {
   'reply_count': 'int',
   'last_reply_seq': ('int', 'The seq of the newest reply that is not deleted.'),
   'last_reply_order': ('str', 'The `order` of that reply, so the thread\'s marker can be placed without holding the reply.'),
   'last_reply_at': 'time',
   'participant_ids': '[uuid]'})

S('Via', 'How a message was posted, if not typed by its author.', {
   'kind': 'enum:webhook|reminder|scheduled|command|import|system',
   'name?': 'str',
   'account_id?': ('uuid', 'Who caused it: the person who sent a command a bot answered.'),
   'icon_file_id?': ('uuid', "A webhook's icon for this message.")})

RAW('SystemNotice', {'type': 'object',
     'description': 'In place of text on a message the server posted itself. Other fields depend on `kind`: '
                    '`call_id` for calls, `conversation_id` for `thread_moved` (the new channel), `name` for '
                    '`channel_renamed`, `message_id` for `pinned`, `by_id` where someone else caused it. Clients '
                    'word these themselves.',
     'required': ['kind'],
     'properties': {'kind': {'type': 'string',
                             'enum': ['member_joined', 'member_left', 'call_started', 'call_ended', 'thread_moved',
                                      'channel_renamed', 'pinned']},
                    'call_id': {'type': 'string', 'format': 'uuid'},
                    'conversation_id': {'type': 'string', 'format': 'uuid'},
                    'message_id': {'type': 'string', 'format': 'uuid'},
                    'by_id': {'type': 'string', 'format': 'uuid'},
                    'account_id': {'type': 'string', 'format': 'uuid'},
                    'name': {'type': 'string'},
                    'duration_seconds': {'type': 'integer', 'format': 'int64'},
                    'participant_ids': {'type': 'array', 'items': {'type': 'string', 'format': 'uuid'}}},
     'additionalProperties': True})

S('SealedPayload', "The content of a sealed-chat message. Only the two people's devices can open it.", {'key_epoch': ('int', 'The key epoch it was encrypted in.'), 'ciphertext': ('b64', 'At most 128 KB.')})

S('DiceTerm', 'One term of a roll: some dice, or a plain number added or taken away.', {
   'sign': ('int', '1 to add the term, -1 to take it away.'),
   'count?': ('int', 'Dice only: how many.'),
   'sides?': ('int', 'Dice only: the sides of each die.'),
   'keep?': ('enum:kh|kl|dh|dl', 'Keep highest, keep lowest, drop highest, drop lowest.'),
   'keep_n?': ('int', 'How many are kept or dropped.'),
   'rolls?': ('[int]', 'Each die as it fell, in order.'),
   'dropped?': ('[int]', 'Positions in `rolls` that do not count.'),
   'value': ('int', "A plain number's value, or the sum of the dice that count. The sign is not applied to it.")})

S('Chance', 'A result the server drew for `/roll`, `/flip`, `/pick`, `/shuffle`, `/draw`, `/mahjongg` or `/tarot` (MESSAGE-FORMAT.md section 1.2). No edit changes it.', {
   'kind': 'enum:roll|flip|pick|shuffle|draw|tarot',
   'dice?': ('str', 'A roll: the expression as the server read it, written out in full (`d20` is `1d20`, `adv` is `2d20kh1`).'),
   'terms?': ('[#DiceTerm]', 'A roll: each term with its dice.'),
   'total?': ('int', 'A roll: the total.'),
   'outcome?': ('enum:heads|tails', 'A flip.'),
   'options?': ('[str]', 'A pick or shuffle: the options as given.'),
   'picked?': ('int', 'A pick: the position in `options` of the one picked.'),
   'order?': ('[int]', 'A shuffle: the positions in `options`, in the order the shuffle put them.'),
   'deck?': ('str', 'A draw or tarot reading: the name of the deck (testdata/commands/decks.json).'),
   'cards?': ('[str]', 'A draw: the cards drawn, in order, by code. A tarot reading: the card in each position of the spread.'),
   'reversed?': ('[bool]', 'A draw from a deck with orientation, and a tarot reading: whether each card came up reversed.'),
   'spread?': ('str', 'A tarot reading: the spread (testdata/commands/tarot.json).')})

S('ChanceInput', 'Asks the server to draw a result. People only; refused in a sealed chat.', {
   'kind': 'enum:roll|flip|pick|shuffle|draw|tarot',
   'dice?': ('str', 'A roll: the expression (FEATURES.md section 12.4). Empty is `1d20`.', {'maxLength': 100}),
   'options?': ('[str]', 'A pick or shuffle: 2 to 20 options of up to 200 characters.', {'maxItems': 20}),
   'count?': ('int', 'A draw: how many cards. Default 1.'),
   'deck?': ('str', 'A draw: the deck, by name or alias. Default `standard`.'),
   'spread?': ('str', 'A tarot reading: the spread, by name or alias. Default `three`.')})

S('Message', 'One post in a conversation, in the shape MESSAGE-FORMAT.md defines. In a sealed chat only `id`, `conversation_id`, `seq`, `order`, `author_id`, `client_id`, `sealed`, `quiet` and `created_at` are present, `late` and `unsent` where they apply, and `deleted_at` once deleted.', {
   'id': 'uuid',
   'conversation_id': 'uuid',
   'seq': ('int', "The number of the event that created it: the message's arrival number on this server. It serves "
           'catch-up, read positions and unread counts. It does not say where the message is shown: `order` does.'),
   'author_id': 'uuid',
   'client_id?': ('uuid',
                  'Sealed chats: the reference the sending device made. References inside sealed content use it.'),
   'parent_id?': ('uuid', 'The top-level message this replies to.'),
   'quote?': '#Quote',
   'text?': ('str', 'Churr Markdown, exactly as the author sent it.'),
   'entities?': '[#Entity]',
   'tasks?': '[#Task]',
   'files?': '[#File]',
   'cards?': '[#Card]',
   'form?': '#Form',
   'action?': ('bool', 'The text says what the author is doing (typed with `/me`); drawn after the author\'s name.'),
   'chance?': '#Chance',
   'fallback?': ('str', 'Plain text the server wrote to stand in for the cards, form and chance result.'),
   'reactions?': '[#Reaction]',
   'thread?': '#ThreadSummary',
   'via?': '#Via',
   'system?': '#SystemNotice',
   'sealed?': '#SealedPayload',
   'quiet?': ('bool',
              'Sealed chats: the sender marked it as not an ordinary message (a reaction, tick, edit or delete), so '
              'it does not notify or count as unread.'),
   'pinned?': 'bool',
   'notified_anyway?': ('bool',
                        'The author used "notify anyway" on it. Says nothing about whether anyone was reached.'),
   'has_history?': 'bool',
   'unsent?': ('bool',
               "In a conversation shared with other servers, on a message of this server's own: the link has not yet "
               'acknowledged it. It is stored, has its `seq` and `order`, and is shown in its place all the same. '
               '`message.sent` says when it no longer applies.'),
   'order': ('str',
             'Where the message is shown. Clients always show a conversation, and a thread, sorted by `order`, '
             'compared as plain text, character by character. It is opaque: nothing else may be read out of it. In a '
             'conversation that is not shared it follows `seq`. In one that is shared with other servers it is made '
             'from the time, by the clock all its servers share, at which the message was posted, and is the same on '
             'every server; it need not follow `seq`. It can change after the fact, in which case `message.placed` '
             'says so.'),
   'late?': ('bool',
             'In a shared conversation: the message arrived on this server after messages ordered later than it had '
             'already been delivered here, for example because its server was cut off for a while. It takes its place '
             'by `order` all the same, and counts as unread like any new message: its `seq` is above what was read.'),
   'created_at': 'time',
   'edited_at?': 'time',
   'deleted_at?': ('time', 'Set on a deleted message. Its text, files, cards and form are then empty.'),
   'deleted_by_id?': 'uuid'})

S('MessageInput', 'A new message. It needs at least one of text, a file, a card, a form, a chance request, or `sealed`.', {
   'client_id?': ('uuid',
                  'Made by the client. Sending the same one again in the same conversation within 24 hours returns '
                  'the message already created. Required in a sealed chat.'),
   'text?': ('str', 'Churr Markdown.', {'maxLength': 16000}),
   'file_ids?': ('[uuid]',
                 'Files already uploaded to this conversation, in display order. Up to ten. In a sealed chat: the '
                 'blobs the message carries, named outside the ciphertext so the server can tie them to it.',
                 {'maxItems': 10}),
   'parent_id?': ('uuid', "Reply in this message's thread. Naming a reply puts the new message in the same thread."),
   'quote?': '#Quote',
   'cards?': ('[#Card]', 'Bots only. Up to five.', {'maxItems': 5}),
   'form?': '#FormInput',
   'sealed?': ('#SealedPayload', 'Sealed chats only, and then the only content field allowed.'),
   'quiet?': ('bool', "Sealed chats only. Must match the flag in the ciphertext's header."),
   'action?': ('bool', 'The text says what the author is doing (`/me`). Needs text. In a sealed chat the mark goes inside the sealed content.'),
   'chance?': ('#ChanceInput', 'Have the server draw a result. The message then has no text, files, cards or form. People only; refused in a sealed chat.'),
   'time_zone?': ('str', 'The author\'s IANA zone (`Europe/Berlin`), in which a date and time typed without a zone is read as an `instant` entity (ruling R69). Without it such text stays plain; a date and time typed with a zone resolves either way.'),
   'instant_hints?': ('[#InstantHint]', 'Phrases the author made into instants in the composer. Up to 50 are read.', {'maxItems': 50}),
   'object_hints?': ('[#ObjectHint]', 'What the author chose for ranges of the text: plain text, or one kind of object. Up to 50 are read.', {'maxItems': 50})})

S('MessageEdit', 'The new content of a message. Fields left out stay as they are.', {
   'text?': 'str',
   'file_ids?': ('[uuid]', 'The files the message keeps or gains.'),
   'cards?': ('[#Card]', 'Bots only.'),
   'form?': ('#FormInput', 'Refused with `conflict` once anyone has answered.'),
   'sealed?': '#SealedPayload',
   'time_zone?': ('str', 'As for a new message, for the new text. An edit that leaves the text alone keeps its instants.'),
   'instant_hints?': ('[#InstantHint]', 'As for a new message, for the new text.', {'maxItems': 50}),
   'object_hints?': ('[#ObjectHint]', 'As for a new message, for the new text. An edit that leaves the text alone keeps its references.', {'maxItems': 50})})

S('MessageVersion', 'An earlier version of a message, or the last content of a deleted one.', {
   'version': 'int',
   'text': 'str',
   'entities': '[#Entity]',
   'cards': '[#Card]',
   'files': '[#File]',
   'replaced_at': 'time',
   'replaced_by_id': 'uuid',
   'reason': 'enum:edit|delete'})

S('TaskChange', 'One tick or untick of a checklist item.', {'item_id': 'str', 'account_id': 'uuid', 'checked': 'bool', 'at': 'time'})

S('ThreadState', 'The caller\'s state for one thread. No row means "neither".', {
   'parent_id': 'uuid',
   'conversation_id': 'uuid',
   'state': ('enum:following|ignored|none', '`none` ("neither") can appear in a response, as when a read position is kept for a thread the caller neither follows nor ignores.'),
   'read_seq': ('int', "The caller's read position in the thread."),
   'unread_count': 'int'})

S('ThreadEntry', 'A row of the Threads view.', {'thread_state': '#ThreadState', 'parent': '#Message'})

S('Bookmark', 'A message the caller saved for themselves.', {'message_id': 'uuid', 'created_at': 'time', 'message': '#Message'})

S('Emoji', 'A custom emoji.', {
   'id': 'uuid',
   'name': 'str',
   'file_id': 'uuid',
   'url': 'str',
   'created_by_id': 'uuid',
   'created_at': 'time',
   'server_name?': ('str', 'Set on an emoji that came with a shared channel. Written `:name@server.name:`.')})

S('SearchResult', 'One search hit.', {'message': '#Message', 'snippet?': ('str', 'A short plain-text excerpt around the match.')})

RAW('Event', {'type': 'object',
     'description': 'An event as the live connection and catch-up carry it. The other fields depend on `type`; '
                    'docs/EVENTS.md lists every type.',
     'required': ['type', 'at'],
     'properties': {'type': {'type': 'string', 'description': '`thing.happened`.'},
                    'at': {'type': 'string', 'format': 'date-time'},
                    'conversation_id': {'type': 'string', 'format': 'uuid'},
                    'seq': {'type': 'integer', 'format': 'int64'}},
     'additionalProperties': True})


# ----------------------------------------------------------------------------
# Scheduling and commands

S('Repeat', 'When a scheduled item runs again.', {
   'every': 'enum:day|week|month',
   'days?': ('days', 'For `day`, and for `week` on chosen days: which days of the week, Monday first.'),
   'interval?': ('int', 'For `week` with `days`: every this many weeks, 1 to 52, counted from the week of the first run '
                        'after the rule is set or changed. Absent means 1.'),
   'day?': ('int', 'For `month`: the day of the month, 1 to 31, or the month\'s last day if it has no such day.'),
   'week?': ('int', 'For `month`, with `weekday`: the nth such weekday of the month, 1 to 4, or -1 for the last.'),
   'weekday?': ('int', 'For `month`, with `week`: 1 (Monday) to 7 (Sunday).'),
   'at': 'hhmm',
   'time_zone': ('str', 'IANA name.')})

S('ChatRequest', 'A request for a new chat, as the person asked sees it: who is asking and for what kind of chat, nothing more (ruling R78).', {
   'id': 'uuid',
   'kind': ('enum:direct|sealed', 'A direct chat or a sealed chat.'),
   'account': ('#Account', 'Who is asking, as the caller is shown them.'),
   'created_at': 'time'})

S('ScheduledItem', 'A message to send later, a reminder, or a recurring form.', {
   'id': 'uuid',
   'account_id': 'uuid',
   'conversation_id': 'uuid',
   'kind': ('enum:message|reminder|form|watch',
            '`watch` is "tell me when this person is around" (ruling R76), made with `watchAccount`.'),
   'content': ('#MessageInput', 'Empty for a watch.'),
   'about_message_id?': ('uuid', 'For "remind me about this message". The delivered message quotes it.'),
   'watch_account_id?': ('uuid', 'For a watch: the person it waits for.'),
   'next_run_at?': ('time', 'Absent when it has finished or failed. For a watch: when it expires unfired.'),
   'repeat?': '#Repeat',
   'last_run_at?': 'time',
   'failed_at?': ('time',
                  'Set when a run could not post. The content is kept so the owner can send it by hand or delete '
                  'it.'),
   'failure?': ('str', 'The error code of the failed run.'),
   'created_at': 'time'})

S('ScheduledInput', 'A new scheduled item. Give `run_at` for one run, `repeat` for a repeating one, or both to start repeating at a given time.', {
   'conversation_id': ('uuid',
                       'Where it is posted. A reminder to yourself goes to your own one-person channel; a reminder '
                       'to a person goes to the ad-hoc channel with them.'),
   'kind': 'enum:message|reminder|form',
   'content': '#MessageInput',
   'about_message_id?': 'uuid',
   'run_at?': 'time',
   'repeat?': '#Repeat'})

S('SavedReply', 'A `/name` command that posts preset text.', {
   'id': 'uuid',
   'community': ('bool', 'True for one an admin defined for everyone.'),
   'name': 'str',
   'text': 'str',
   'created_at': 'time'})

S('Command', 'Something that can be typed as `/name` in a conversation.', {
   'name': 'str',
   'kind': ('enum:builtin|saved_reply|bot',
            'Built-in commands are carried out by the client with ordinary API calls.'),
   'description': 'str',
   'hint?': ('str', 'The argument hint.'),
   'bot_id?': 'uuid',
   'saved_reply_id?': 'uuid'})


# ----------------------------------------------------------------------------
# Bots and webhooks

S('Bot', 'A bot account with what admins see about it.', {
   'account': '#Account',
   'description': 'str',
   'created_by_id': 'uuid',
   'webhook_only': ('bool', 'The account behind a webhook.'),
   'commands?': '[#Command]',
   'channel_ids?': ('[uuid]', 'The named channels the bot is in. Ad-hoc channels are never listed.')})

S('Webhook', 'A webhook, from request to revocation. The secret address is not part of this object.', {
   'id': 'uuid',
   'conversation_id': 'uuid',
   'bot_id': 'uuid',
   'name': 'str',
   'purpose': 'str',
   'state': 'enum:waiting|approved|declined|revoked',
   'requested_by_id': 'uuid',
   'requested_at': 'time',
   'decided_by_id?': 'uuid',
   'decided_at?': 'time',
   'has_secret': ('bool', 'The requester has collected the address. Until then the webhook cannot post.'),
   'last_used_at?': 'time'})

RAW('WebhookCard', {'description': 'A card as a webhook sends it. In place of file identifiers it gives addresses for the server to '
                    'fetch, or names parts of a multipart request.',
     'allOf': [{'$ref': '#/components/schemas/Card'},
               {'type': 'object',
                'properties': {'image_url': {'type': 'string', 'format': 'uri'},
                               'image_part': {'type': 'string'},
                               'author': {'type': 'object',
                                          'required': ['name'],
                                          'properties': {'name': {'type': 'string'},
                                                         'icon_url': {'type': 'string', 'format': 'uri'},
                                                         'icon_part': {'type': 'string'}}}}}]})

S('WebhookMessage', 'The body of `POST /hooks/{token}` (BOTS.md section 10). At least one of text, cards, form and files.', {
   'text?': ('str', 'Churr Markdown.', {'maxLength': 16000}),
   'cards?': ('[#WebhookCard]', 'Up to five.', {'maxItems': 5}),
   'form?': ('#FormInput', 'Actions allowed: `tally`, `collect`, `thread`.'),
   'files?': ({'$raw': {'type': 'array',
                        'maxItems': 10,
                        'items': {'type': 'object',
                                  'required': ['part'],
                                  'properties': {'part': {'type': 'string', 'description': '`file1` to `file10`.'},
                                                 'name': {'type': 'string'},
                                                 'caption': {'type': 'string'}}}}},
              'Each names a part of a multipart request to attach as a file.'),
   'parent_id?': ('uuid', "A top-level message in the webhook's channel."),
   'client_id?': 'uuid',
   'via?': ({'name?': 'str', 'icon_url?': 'uri', 'icon_part?': 'str'},
            'How the message is labelled: "via *name*" beside the webhook\'s own name.')})

S('InteractionResponse', "A bot's response to an interaction: a command, a button press, or an answer to a `bot` form or dialog (BOTS.md section 5).", {
   'type': ('enum:message|private|dialog|none',
            '`message`: post `message` in the conversation for everyone. `private`: show `text` and `cards` to the '
            'asker only; not stored. `dialog`: show the asker `form` in a dialog. `none`: show nothing and tell the '
            "asker's client to stop waiting."),
   'message?': ('#MessageInput', 'For `message`.'),
   'text?': ('str', 'For `private`.'),
   'cards?': ('[#Card]', 'For `private`. Up to five.', {'maxItems': 5}),
   'form?': ({'title?': 'str', 'fields': '[#FormField]', 'buttons?': '[#FormButton]'},
             'For `dialog`. The action is always `bot`.')})

S('Dialog', 'A form shown to one person in a dialog. It is not a message and nobody else sees it.', {
   'id': ('uuid', "The dialog form's identifier."),
   'bot_id': 'uuid',
   'conversation_id': 'uuid',
   'title?': 'str',
   'fields': '[#FormField]',
   'buttons?': '[#FormButton]'})


# ----------------------------------------------------------------------------
# Calls

S('CallTrack', 'One media track a participant is sending (CALLS.md).', {
   'id': ('str', 'Short, unique within the call, made by the server.'),
   'source': 'enum:microphone|camera|screen',
   'kind': 'enum:audio|video',
   'muted': 'bool',
   'codec?': 'str',
   'layers?': '[str]',
   'width?': 'int',
   'height?': 'int'})

S('CallParticipant', 'An account in a call.', {
   'account_id': 'uuid',
   'device_id?': ('uuid', "The device the account is in the call on. In a sealed chat's call it names whose call sender key decrypts the participant's frames (CALL-FRAMES.md). Absent when the session has no device, and for a participant another server relays. It changes if the call moves to the account's other device; `call.track_published` and `call.offer` name the device of each track."),
   'joined_at': 'time',
   'tracks?': ('[#CallTrack]', 'Present only in copies sent to participants.')})

S('Call', 'A call in a conversation. A conversation has one at most. CALLS.md describes it in full.', {
   'id': 'uuid',
   'conversation_id': 'uuid',
   'message_id?': ('uuid', 'The system message for the call.'),
   'started_by_id': 'uuid',
   'started_at': 'time',
   'ended_at?': 'time',
   'sealed': ('bool', 'True in a sealed chat: media frames are encrypted on the devices with keys derived from the epoch secret.'),
   'hosted_by?': ('str', 'The name of the server carrying the media, when that is not this one.'),
   'participants': '[#CallParticipant]'})

S('LiveCall', 'A call in progress as the admin console shows it: how many people, not who.', {
   'id': 'uuid',
   'started_at': 'time',
   'participant_count': 'int',
   'camera_count': 'int',
   'conversation_id?': ('uuid', 'Only for a call in a public channel.')})


# ----------------------------------------------------------------------------
# Sealed chats, in the names of SEALED.md

S('DeviceListVersion', "One version of an account's signed device list. The server stores every version.", {
   'account_id': 'uuid',
   'version': ('int', 'Starts at 1 and goes up by exactly one.'),
   'list': ('b64', 'The exact bytes that were signed: the binary list record of SEALED.md section 4.3, in base64url.'),
   'sig': ('b64', "By the list's own account key."),
   'prev_sig?': ('b64', 'By the previous account key. Present only when the account key changed.'),
   'created_at': 'time'})

S('DeviceListInput', "A new version of the caller's device list.", {'version': 'int', 'list': 'b64', 'sig': 'b64', 'prev_sig?': 'b64'})

S('EpochRecord', "One key epoch of a sealed chat. The record is made and signed by one device and holds the epoch secret sealed separately to every device on both accounts' device lists, the device set, and the hash of the previous record. The server stores one per key epoch and cannot open it.", {
   'conversation_id': 'uuid',
   'key_epoch': ('int', 'A plain counter starting at 1.'),
   'seq': ('int', "Its place in the conversation's order."),
   'account_id': 'uuid',
   'device_id': ('uuid', 'The device that made it.'),
   'record': 'b64',
   'created_at': 'time'})

S('LockedCopy', 'The backup key sealed to one way of unlocking.', {
   'id': 'uuid',
   'kind': 'enum:passkey|phrase|device',
   'passkey_id?': ('uuid', 'For `passkey`.'),
   'device_id?': ('uuid', "For `device`: sealed to that device's box key."),
   'key_version': 'int',
   'locked_key': ('b64', "The backup key sealed to that way's public key, with the account key's signature."),
   'created_at': 'time'})

S('LockedCopyInput', 'A locked copy to store.', {
   'kind': 'enum:passkey|phrase|device',
   'passkey_id?': 'uuid',
   'device_id?': 'uuid',
   'key_version': 'int',
   'locked_key': 'b64'})

S('AccountRecord', 'The account key and the list of ways to unlock, encrypted under the backup key.', {
   'key_version': 'int',
   'revision': ('int', 'Goes up by one each time the ways change.'),
   'record': 'b64',
   'updated_at?': 'time'})

S('Backup', 'What a complete session of an account is given to unlock sealed chats. The server can open none of it.', {
   'key_version': 'int',
   'account_record': '#AccountRecord',
   'locked_copies': '[#LockedCopy]',
   'replacement_due': ('bool',
                       'A passkey or device was removed where no unlocked device was at hand. The next unlocked '
                       'device replaces the backup key before anything else.')})

S('BackupRecord', 'A history secret for one sealed chat and one key epoch, encrypted under the backup key.', {'conversation_id': 'uuid', 'key_epoch': 'int', 'key_version': 'int', 'record': 'b64', 'created_at?': 'time'})

S('UnlockRequest', 'A new, locked device asking an unlocked device of the same account for the backup key. Expires after ten minutes.', {
   'id': 'uuid',
   'device_id': 'uuid',
   'device?': '#Device',
   'request': ('b64', "The device's public keys and proof, as the device made them."),
   'answer?': ('b64', "The backup key sealed to the asking device's box key. Only the asking device is given it."),
   'created_at': 'time',
   'expires_at': 'time',
   'answered_at?': 'time'})


# ----------------------------------------------------------------------------
# Admin

S('Settings', 'Server settings, by the dotted keys of OPERATIONS.md section 5, with `auth.member_sign_in`, `auth.second_factor` and `auth.second_factor_accepted` for the sign-in policy. A read returns every key. A change sends any subset.', {
   'server.name?': ('str', "The community's name."),
   'server.icon_file_id?': {'$raw': {'type': ['string', 'null'], 'format': 'uuid'}},
   'server.time_zone?': ('str', 'IANA name used for an account that has none.'),
   'welcome.text?': ('str', 'Churr Markdown shown to a new member on first sign-in.'),
   'registration.mode?': 'enum:open|invite|closed',
   'invites.members_may_invite?': 'bool',
   'auth.member_sign_in?': 'enum:passkey|either|sso',
   'auth.second_factor?': 'enum:off|required',
   'auth.second_factor_accepted?': '[enum:passkey|totp|provider]',
   'auth.allow_access_keys?': ('bool', 'Allow accounts without a passkey: new access keys may be made. Off, existing access keys keep working. Default on.'),
   'sessions.passkey_seconds?': 'int',
   'sessions.sso_seconds?': 'int',
   'messages.quick_correction_seconds?': 'int',
   'messages.edit_window_seconds?': {'$raw': {'type': ['integer', 'null'], 'description': 'Null is no limit.'}},
   'retention.seconds?': {'$raw': {'type': ['integer', 'null'], 'description': 'Null keeps messages until deleted.'}},
   'mentions.broadcast_managers_only?': 'bool',
   'channels.large_channel_limit?': ('int', 'A channel with more members than this is delivered in full only to the live connections that have it open; the others are told that it changed, at most once a second, and fetch the difference when it is opened. A channel can say otherwise (`lazy_delivery`). Default 1000. 0 delivers every channel in full to everyone.'),
   'channels.large_feature_limit?': ('int', 'A conversation with more members than this has no typing notices and no presence of single members; clients show an online count. Default 50. 0 is no limit.'),
   'mentions.broad_mention_limit?': ('int', 'In a channel with more members than this, only its managers and the admins may use `@channel`, `@here` and `@everyone`, unless the channel says otherwise. Default 250. 0 is no limit.'),
   'emoji.members_may_add?': 'bool',
   'blocking.enabled?': 'bool',
   'webhooks.managers_may_approve?': 'bool',
   'files.max_bytes?': 'int',
   'files.store?': 'enum:disk|s3',
   'files.s3?': {'$raw': {'type': ['object', 'null'],
                          'description': 'When the store is `s3`.',
                          'properties': {'endpoint': {'type': 'string'},
                                         'region': {'type': 'string'},
                                         'bucket': {'type': 'string'},
                                         'prefix': {'type': 'string'},
                                         'path_style': {'type': 'boolean'},
                                         'access_key_id': {'type': 'string'}}}},
   'files.s3_secret?': ('str', 'The secret access key. Write-only: never returned.', {'writeOnly': True}),
   'files.s3_secret_set?': ('bool', 'Read-only: a secret is stored.', {'readOnly': True}),
   'push.relay_enabled?': 'bool',
   'push.relay_url?': 'str',
   'push.unifiedpush_enabled?': 'bool',
   'push.send_badge?': 'bool',
   'previews.enabled?': 'bool',
   'sealed.enabled?': ('bool', 'Off: no sealed chat can be started and existing ones become read-only.'),
   'calls.enabled?': 'bool',
   'calls.max_participants?': 'int',
   'calls.max_cameras?': 'int',
   'links.send_presence?': 'bool',
   'links.default_max_file_bytes?': 'int'})

S('SsoProvider', 'A company sign-in provider. Secrets are write-only.', {
   'id': 'uuid',
   'kind': 'enum:oidc|saml',
   'name': 'str',
   'config': ('obj',
              'OIDC: `issuer`, `client_id`, extra scopes, claim names, the `acr` values that count as multi-factor. '
              'SAML: entity ID, sign-on address, signing certificates, attribute names, the context values that '
              'count as multi-factor; or `metadata_xml` or `metadata_url`, loaded once.'),
   'has_secret': 'bool',
   'group_claim?': 'str',
   'admin_group?': ('str', 'A provider group that decides the admin role at each sign-in.'),
   'enabled': 'bool',
   'callback_url': ('str', 'The fixed address on this server to register at the provider.'),
   'created_at': 'time'})

S('SsoProviderInput', 'Fields of a company sign-in provider.', {
   'kind?': 'enum:oidc|saml',
   'name?': 'str',
   'config?': 'obj',
   'secret?': ('str', 'OIDC client secret. Stored sealed and never returned.'),
   'group_claim?': 'str',
   'admin_group?': 'str',
   'enabled?': 'bool'})

S('Invite', 'An invite link.', {
   'id': 'uuid',
   'code': 'str',
   'url': ('str', 'The link to hand out: `https://<server>/join/<code>`, the page where the person joins.'),
   'role': 'enum:member|guest',
   'group_ids': '[uuid]',
   'conversation_ids': ('[uuid]', 'For guests: the channels they start in.'),
   'claims_account_id?': ('uuid', 'The imported account this link claims.'),
   'max_uses': ('int', '0 means no limit.'),
   'uses': 'int',
   'expires_at?': 'time',
   'created_by_id?': 'uuid',
   'created_at': 'time',
   'revoked_at?': 'time'})

RAW('AdminAccount', {'description': 'An account with what admins see about it. Never its sessions.',
     'allOf': [{'$ref': '#/components/schemas/Account'},
               {'type': 'object',
                'required': ['passkey_count', 'has_access_key', 'has_totp', 'provider_ids', 'banned'],
                'properties': {'passkey_count': {'type': 'integer', 'format': 'int64'},
                               'has_access_key': {'type': 'boolean', 'description': 'The account signs in with an access key. With no passkey it is one of the accounts without a passkey.'},
                               'has_totp': {'type': 'boolean'},
                               'provider_ids': {'type': 'array',
                                                'items': {'type': 'string', 'format': 'uuid'},
                                                'description': 'Providers the account has an identity at.'},
                               'last_seen_at': {'type': 'string', 'format': 'date-time'},
                               'claimed_at': {'type': 'string', 'format': 'date-time'},
                               'removed_by_id': {'type': 'string', 'format': 'uuid'},
                               'banned': {'type': 'boolean'}}}]})

S('Ban', 'A ban on an account or a sign-in identity.', {
   'id': 'uuid',
   'account_id?': 'uuid',
   'provider_id?': 'uuid',
   'subject?': 'str',
   'reason': 'str',
   'created_by_id': 'uuid',
   'created_at': 'time',
   'lifted_at?': 'time'})

S('ChannelBan', 'An account banned from one channel (docs/FEATURES.md section 19.1).', {
   'id': 'uuid',
   'conversation_id': 'uuid',
   'account_id': 'uuid',
   'reason': 'str',
   'created_by_id': 'uuid',
   'created_at': 'time',
   'expires_at?': ('time', 'Absent for a ban that lasts until it is lifted.'),
   'lifted_at?': 'time'})

S('ChannelBanInput', 'A new channel ban.', {
   'account_id': 'uuid',
   'reason?': ('str', 'At most 1,000 characters.'),
   'seconds?': ('int', 'How long it lasts, 60 seconds to 366 days. Absent or 0: until it is lifted.')})

S('Knock', 'A request to be added to a private channel (docs/FEATURES.md section 12.5, ruling R74).', {
   'id': 'uuid',
   'conversation_id': 'uuid',
   'account_id': ('uuid', 'Who asked.'),
   'created_at': 'time',
   'outcome': 'enum:pending|accepted|declined',
   'answered_by_id?': ('uuid', 'The manager who answered. Never sent to the one who asked.'),
   'answered_at?': 'time'})

S('Broadcast', 'A notice from an admin to everyone connected (docs/FEATURES.md section 12.6, ruling R74). Not a message.', {
   'id': 'uuid',
   'text': ('str', 'One line of plain text with the inline Markdown of messages, at most 500 characters.'),
   'by_id': ('uuid', 'The admin who sent it.'),
   'created_at': 'time',
   'until': ('time', 'When it stops showing. Clients hide it then by themselves; no event is sent.')})

S('BroadcastInput', 'A new server broadcast.', {
   'text': ('str', 'One line, 1 to 500 characters, plain text with the inline Markdown of messages.'),
   'seconds?': ('int', 'How long it shows, 60 to 86,400. Absent or 0: 600 (ten minutes).')})

S('AdminChannel', 'What the admin console shows about a named channel. No content.', {
   'id': 'uuid',
   'name': ('str', 'Free-form and not unique.', {'minLength': 1, 'maxLength': 64}),
   'private': 'bool',
   'topic?': ('str', 'Public channels only.'),
   'section_id?': 'uuid',
   'parent_id?': ('uuid', 'The channel it is a sub-channel of.'),
   'retention_seconds?': 'int',
   'member_count': 'int',
   'manager_ids': '[uuid]',
   'created_at': 'time',
   'archived_at?': 'time',
   'shared?': 'bool',
   'announcement?': ('bool', 'An announcement channel: only its managers and the members they allow post.')})

S('Integration', 'A credential for a program that works on the server as a whole.', {
   'id': 'uuid',
   'name': 'str',
   'allows': ('[enum:availability]', 'What it may do.'),
   'created_by_id': 'uuid',
   'created_at': 'time',
   'last_used_at?': 'time',
   'revoked_at?': 'time'})

S('WordFilter', 'A word or pattern and what a match does.', {
   'id': 'uuid',
   'pattern': 'str',
   'is_regex': 'bool',
   'action': 'enum:block|flag',
   'created_by_id': 'uuid',
   'created_at': 'time'})

S('PurgeRequest', "An author's request to purge their message in a public channel. Only admins are ever given this object with its outcome.", {
   'id': 'uuid',
   'message_id': 'uuid',
   'requested_by_id': 'uuid',
   'requested_at': 'time',
   'decided_by_id?': 'uuid',
   'decided_at?': 'time',
   'outcome?': ('enum:shown|hidden|erased', 'Absent while waiting.'),
   'message?': '#Message'})

S('KnownServer', 'Another Churr server this one knows.', {
   'id': 'uuid',
   'name': 'str',
   'fingerprint': ('str', 'Fingerprint of its signing key, for people to compare.'),
   'first_seen_at': 'time',
   'blocked_at?': 'time'})

S('Peer', 'A link to one other server, or to a hub.', {
   'id': 'uuid',
   'kind': 'enum:server|hub',
   'name?': ('str', "The other server's name, or the hub's. Absent on an invitation nobody has used yet."),
   'fingerprint?': 'str',
   'dials': ('bool', 'This server dials out to the peer. The side that accepted the invitation dials.'),
   'state': 'enum:invited|active|paused|removed',
   'invite_expires_at?': 'time',
   'servers?': ('[#KnownServer]', 'For a hub: the servers reachable through it.'),
   'created_by_id': 'uuid',
   'created_at': 'time',
   'connected_at?': 'time',
   'last_error?': 'str',
   'told_clock?': ('#PeerClock', "What the peer, as a time reference, has measured of this server's clock and told it. This server "
                   'stamps its events with it in every conversation the peer is the time reference of: every stream through a '
                   'hub, and the streams the other server owns on a direct link. Absent until the peer has told one; kept while '
                   'disconnected.'),
   'measured_clock?': ('#PeerClock', "On a direct link: what this server, as the time reference of the streams it owns, has measured "
                       "of the other server's clock. Absent for a hub, which does the measuring itself.")})

S('PeerClock', ('What a time reference has measured of one server\'s clock and of the path between the two, in the link\'s own ping '
                'and pong. The reference is a hub, or the owner server on a direct link. A constant difference between the two '
                'directions of a path cannot be told from a clock offset; its effect on `offset_ms` is at most half of `latency_ms`, '
                'which is why `uncertainty_ms` is never less than that unless `synced`.'), {
   'offset_ms': ('int', "The measured server's clock minus the reference's, as the reference applies it. May be negative."),
   'latency_ms': ('int', 'The lowest round trip among the recent samples.'),
   'jitter_ms': ('int', 'The mean by which the recent round trips exceed the lowest.'),
   'uncertainty_ms': ('int', 'A bound on the error of `offset_ms`.'),
   'forward_ms': ('int', 'The lowest one-way time from the reference to the server among the recent samples, with the offset taken out.'),
   'forward_jitter_ms': ('int', "The mean by which that direction's samples exceed its lowest."),
   'back_ms': ('int', 'The lowest one-way time from the server to the reference, with the offset taken out.'),
   'back_jitter_ms': ('int', "The mean by which that direction's samples exceed its lowest."),
   'asymmetry_ms': ('int', 'An estimate of how much longer the forward direction takes than the back one, from a change seen in one '
                    'direction only or, when `synced`, from the measured offset beyond 10 ms. 0 when nothing is known. May be negative.'),
   'synced?': ('bool', "True when the reference's operator has declared the clocks synchronised: `offset_ms` is then held within "
               '10 ms and the rest of what was measured is in `asymmetry_ms`.'),
   'samples': ('int', 'How many samples the figures rest on, at most 16.'),
   'measured_at': ('time', "The reference's clock when the newest sample arrived.")})

S('StreamServer', 'A server in a stream.', {'server_id': 'uuid', 'name': 'str', 'state': 'enum:invited|active|removed', 'joined_at?': 'time'})

S('Stream', 'One conversation shared over a peer. Admin operations list only streams of kind `channel`: ad-hoc channels and sealed chats across servers are never shown to an owner.', {
   'id': 'uuid',
   'peer_id': 'uuid',
   'conversation_id?': ('uuid', 'Absent until the offer is accepted here.'),
   'owner_server?': ('str', "Absent when this server owns the stream. For a channel on a hub, the hub's name."),
   'reference': ('enum:self|owner|hub',
                 "Who the stream's time reference is: the party whose clock its events are stamped by and that keeps "
                 'its log. The owner server on a direct link, the hub through a hub.'),
   'kind': 'enum:channel|adhoc|sealed',
   'state': ('enum:offered|active|left|removed',
             "`offered`: waiting for this server's owner, or for another server's. `left`: this server left. "
             '`removed`: the owner removed this server or closed the stream.'),
   'key_epoch': 'int',
   'offered_name?': 'str',
   'offered_topic?': 'str',
   'offered_at?': 'time',
   'offer_expires_at?': ('time', 'An unanswered offer lapses after seven days.'),
   'history_days': ('int', '0 to 90.'),
   'max_blob_bytes?': ('int', "The largest file the stream carries. Set by the stream's owner."),
   'servers': '[#StreamServer]',
   'created_at': 'time'})

S('HubModerator', 'A moderator of a channel a hub owns: an account, and the server it belongs to.', {
   'account_id': 'uuid', 'server': 'str', 'username': 'str'})

S('HubChannel', "A hub's record of a channel it owns, as the hub last signed it.", {
   'stream_id': 'uuid',
   'hub': ('str', "The hub's name."),
   'version': ('int', 'Grows by one with every change the hub signs.'),
   'name': 'str',
   'description': 'str',
   'listing': ('enum:unlisted|request|open',
               '`unlisted`: servers join only by a moderator\'s invitation. `request`: every server on the hub sees it in '
               'the directory and may ask; a moderator approves. `open`: every server on the hub may join.'),
   'servers': ('[str]', 'The names of the servers in it, longest-standing first.'),
   'moderators': '[#HubModerator]',
   'invited': ('[str]', 'Servers invited and not yet in. Empty for a caller who is neither a moderator nor the owner.'),
   'requests': ('[str]', 'Servers asking to join. Empty for a caller who is neither a moderator nor the owner.'),
   'key_maker': ('str', 'The server the hub named to make the current stream key. The hub never holds a key.'),
   'closed': 'bool',
   'updated_at': 'time'})

S('HubDirectoryEntry', "One channel in a hub's directory.", {
   'stream_id': 'uuid',
   'name': 'str',
   'description': 'str',
   'listing': 'enum:request|open',
   'moderators': '[#HubModerator]',
   'servers': ('int', 'How many servers are in it.'),
   'state': ('enum:none|invited|requested|member', "This server's own place in it.")})

S('StreamMove', 'One attempt to move a channel shared over a direct link onto a hub, as this server knows it.', {
   'id': 'uuid',
   'stream_id': 'uuid',
   'direction': ('enum:outgoing|incoming', '`outgoing`: this server proposed it. `incoming`: the other server did.'),
   'other_server': 'str',
   'hub_name': 'str',
   'hub_fingerprint': 'str',
   'hub_peer_id?': ('uuid', "This server's own link to that hub. Absent if it has none, in which case it cannot accept."),
   'state': ('enum:proposed|accepted|committed|done|declined|abandoned',
             '`committed`: the hand-over has happened and is being finished. `done`: both servers and the hub agree it '
             'has. `abandoned`: nothing changed, and `reason` says why.'),
   'reason?': 'str',
   'created_at': 'time',
   'expires_at': 'time',
   'updated_at': 'time'})

S('AuditEntry', 'One line of the audit log.', {
   'id': 'uuid',
   'at': 'time',
   'actor_id?': ('uuid', 'An account or an integration. Absent for the server itself.'),
   'actor_kind': 'enum:account|integration|server|cli',
   'action': ('str', 'For example `account.removed`, `setting.changed`.'),
   'target_kind?': 'str',
   'target_id?': 'str',
   'detail': 'obj'})

S('Export', "A full export of the community's data. Sealed chats are never in it.", {
   'id': 'uuid',
   'state': 'enum:running|ready|failed',
   'started_by_id': 'uuid',
   'started_at': 'time',
   'finished_at?': 'time',
   'size?': ('int', 'Bytes.'),
   'error?': 'str'})

S('OutsideConnection', 'One of the outside connections a server can make, and how it is doing.', {
   'name': 'enum:certificate|push_relay|unifiedpush|link_previews|object_storage|identity_provider|peers',
   'enabled': 'bool',
   'state': 'enum:ok|failing|off',
   'last_ok_at?': 'time',
   'last_error?': 'str'})

S('Overview', "What the admin console's Overview page shows.", {
   'version': 'str',
   'started_at': 'time',
   'uptime_seconds': 'int',
   'database': {'kind': 'enum:sqlite|postgres', 'size?': ('int', 'Bytes.')},
   'files': {'store': 'enum:disk|s3', 'count': 'int', 'size': ('int', 'Bytes.')},
   'accounts': {'people': 'int', 'guests': 'int', 'bots': 'int', 'remote': 'int',
                'without_passkey': ('int', 'People and guests who sign in with an access key and have no passkey.')},
   'members_online': 'int',
   'connections': ('int', 'Live connections open now.'),
   'live_calls': 'int',
   'media_address?': ('str', 'The address and port call media uses.'),
   'outside': '[#OutsideConnection]',
   'peers': '[#Peer]',
   'outbox': ('int', 'Events waiting for a peer.'),
   'janitor_last_run_at?': 'time',
   'waiting': ({'webhooks': 'int', 'reports': 'int', 'purge_requests': 'int'}, 'Queues that need an admin.'),
   'warnings': ('[str]',
                'Plain sentences: a certificate near expiry, a failing peer, a media port that looks closed.')})

S('CardTheme', 'A copy of a theme as a theme card or a server default carries it.', {
   'id': ('uuid', 'The theme it was copied from. It may since have changed or gone.'),
   'name': 'str',
   'kind': 'enum:dark|light',
   'colours': '#ThemeColours',
   'author_id': ('uuid', 'Who owned the theme.')})

S('ThemeColours', 'The base colours of a theme (docs/STYLE.md, "Themes"): exactly these nine, each six hex digits. '
  'Every other colour is derived from them by the rules there.', {
   k: ('str', d, {'pattern': '^#[0-9a-fA-F]{6}$'}) for k, d in [
       ('bg', 'Ground: page, message log, inputs.'), ('panel', 'Panel: sidebar, cards, headers.'),
       ('raise', 'Raised: hover, selected, code.'), ('ink', 'Text.'), ('dim', 'Secondary text, icons.'),
       ('lav', 'Lavender: the brand and yours.'), ('cyan', 'Cyan: live and go.'),
       ('pink', 'Pink: wants attention.'), ('yel', 'Yellow: counts and points, as a fill.')]})

S('Theme', 'A colour theme (ruling R61). Its link is `{origin}/themes/{id}`.', {
   'id': 'uuid',
   'owner_id': 'uuid',
   'name': ('str', '1 to 64 characters.'),
   'kind': 'enum:dark|light',
   'colours': '#ThemeColours',
   'published': ('bool', "In the server's theme library."),
   'published_at?': 'time',
   'created_at': 'time',
   'updated_at': 'time'})

S('ThemeInput', 'A theme as it is saved. On create every field is required; on change absent ones stay. The '
  'server refuses with `invalid_value` a theme whose derived text pairings fall below 4.5:1 or control-edge '
  'pairings below 3:1, naming the pairing and its measured ratio ("dim on raise is 4.06:1; text needs at least '
  '4.5:1."), and a dark theme whose ground is lighter than its ink or a light one the other way round (`kind`).', {
   'name?': ('str', '1 to 64 characters.'),
   'kind?': 'enum:dark|light',
   'colours?': '#ThemeColours'})

S('DefaultThemes', "The server's default themes. Absent: none set.", {
   'dark?': '#CardTheme',
   'light?': '#CardTheme'})
