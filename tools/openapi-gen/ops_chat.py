"""Conversations, messages and everything beside them."""
from lib import area, op


# ----------------------------------------------------------------------------
area('Conversations',
     'Channels, ad-hoc channels and sealed chats: finding, creating, changing, joining and leaving them.')

op('GET conversations', 'listConversations',
   'List the conversations the caller is in',
   who='Any signed-in account.',
   desc="Each carries the caller's `membership` with read position, `mode`, `show` and counts. Every conversation is returned whatever its `show`; the client decides what its sidebar draws.",
   q=['kind:enum:channel|sealed:Only this kind.', 'archived:bool:Include archived conversations. Default false.'],
   res={'conversations': '[#Conversation]'},
   page='cursor')

op('GET conversations/{id}', 'getConversation',
   'Get one conversation',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   res={'conversation': '#Conversation'},
   err='not_found')

op('GET channels', 'browseChannels',
   'Browse named channels',
   who='Owner, admin and member: every public channel, and the private ones they are in. Guests and bots: only channels they are in.',
   desc=('Names are not unique: several channels may share one, and each result carries its `parent_id`, section, '
         'creation date and member count so a person can tell them apart. A private sub-channel is returned only to '
         'its members.'),
   q=['q:str:Match on name or topic.', 'section_id:uuid:Only this section.',
      'archived:bool:Include archived channels.', 'parent_id:uuid:Only the sub-channels of this channel.'],
   res={'conversations': '[#Conversation]'},
   page='cursor')

op('GET sections', 'listSections',
   'List the sections channels are grouped into',
   who='Any signed-in account.',
   res={'sections': '[#Section]'})

op('POST channels', 'createChannel',
   'Create a named channel',
   who='Owner, admin or member. The creator is its first manager.',
   desc=('Channel names are not unique: any number of channels may have the same name, and a name is never refused '
         'for being in use. A named channel may have a parent, another named channel, set with `parent_id`. One level only: a sub-channel cannot have sub-channels, and a channel that has sub-channels cannot be given a parent. Nothing is inherited: a sub-channel has its own members, levels, public or private setting, lock and retention, and joining a parent does not join its sub-channels. It is listed under its parent and takes the parent\'s section for display; its own `section_id` is ignored while it has a parent. The link is local to this server. To create a sub-channel the caller must be a member of the parent, or an admin (`forbidden` otherwise). A parent that is not a named channel is refused with `invalid_value`; one that is itself a sub-channel answers `conflict`; one the caller cannot see answers `not_found`.'),
   body={'name': ('str', 'A channel name: 1 to 64 characters of any script. Spaces and capitals are allowed; no control characters, no leading or trailing space, and runs of spaces are collapsed to one. Not unique.', {'minLength': 1, 'maxLength': 64}),
         'private': 'bool',
         'topic?': 'str',
         'welcome?': 'str',
         'default_level?': ('enum:reader|member', '`reader`: people who join only read, until a manager raises them.'),
         'announcement?': ('bool', 'Make it an announcement channel: only its managers and the members they allow post (`setMemberMayPost`); the others read and react.'),
         'announcement_replies?': ('bool', 'In an announcement channel, let the other members reply in threads.'),
         'section_id?': ('uuid', 'One of the sections the admins defined.'),
         'parent_id?': ('uuid', 'Make it a sub-channel of this named channel.'),
         'account_ids?': ('[uuid]', 'Accounts to add at the default level.')},
   res={'conversation': '#Conversation'},
   st=201,
   err='forbidden not_found conflict')

op('POST chats', 'openChat',
   'Find or create the ad-hoc channel with exactly these people',
   who='Owner, admin and member, with any accounts they can see. Guests and bots, with accounts already in a conversation with them. A remote account can be included only while it shares a shared channel with the caller. Refused with `forbidden` if any of them has blocked the caller.',
   desc=("The caller is always included; an empty list gives the caller's own one-person channel. Answers 200 when "
         'the channel already existed and 201 when it was made. An ad-hoc channel is always private. More than 50 '
         'people is refused: make a named channel.'),
   body={'account_ids': '[uuid]'},
   res={'conversation': '#Conversation'},
   st=200,
   err='forbidden timed_out not_found too_large',
   also={'201': 'Created.'})

op('POST sealed', 'openSealedChat',
   'Find or create the sealed chat with one person',
   who='Owner, admin and member, with a person they can see. Guests, with a person already in a conversation with them. Never bots, on either side. A remote account only while it shares a shared channel with the caller. Refused with `forbidden` if the other person has blocked the caller.',
   desc=('There is one sealed chat per pair. If it exists it is returned with 200 and the record sent is ignored. '
         'Otherwise it is created with 201, and `epoch_record` becomes its epoch record 1, so the chat can be '
         "written in at once. The record is sealed to every device on both accounts' current device lists; if "
         'the other person has no device list yet, to the caller\'s own devices, and the other person\'s first '
         'device starts a new epoch when it arrives. Epoch record 1 carries the new conversation\'s identifier under '
         'its maker\'s signature, so the creating device makes that identifier itself, a UUIDv7, and puts it in the '
         'record: the one exception to the server making every identifier. A record that is missing or not in its '
         'one valid form, that does not name a UUIDv7, or whose identifier is already in use, is refused with '
         '`invalid_value` on field `epoch_record`. For an account on another server the answer may be `unavailable` '
         'while the link to its server is down.'),
   body={'account_id': 'uuid', 'epoch_record': ('b64', 'Epoch record 1, naming the new chat\'s identifier. Ignored if the chat exists.')},
   res={'conversation': '#Conversation'},
   also={'201': 'Created.'},
   err='forbidden timed_out not_found feature_off unavailable')

op('PATCH conversations/{id}', 'updateConversation',
   "Change a channel's name, topic and settings",
   who="Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to. A manager may set a retention period shorter than the server's; an admin may set any. Sealed chats cannot be changed.",
   desc=('Giving an ad-hoc channel a name makes it a named private channel; nothing else about it changes. `private` '
         'may only be set to true: a public channel can be made private, and people who are not members then lose it '
         'at once, history and search included. A private channel can never be made public. Welcome text, section '
         'and retention are for named channels. Channel names are not unique, and a name is never refused for '
         'being in use. A named channel may have a parent, another named channel, set with `parent_id`. One level only: a sub-channel cannot have sub-channels, and a channel that has sub-channels cannot be given a parent. Nothing is inherited: a sub-channel has its own members, levels, public or private setting, lock and retention, and joining a parent does not join its sub-channels. It is listed under its parent and takes the parent\'s section for display; its own `section_id` is ignored while it has a parent. The link is local to this server. The parent may be set or cleared by an account acting as manager of the sub-channel that is also a member of the parent, or by an admin (`forbidden` otherwise). Send `parent_id` as null to make the channel top-level. A parent that is not a named channel is refused with `invalid_value`. A parent that is itself a sub-channel, and giving a parent to a channel that has sub-channels, answer `conflict` (sub-channels go one level deep). Archiving, locking or making a parent private does nothing to its sub-channels, and if a parent is deleted they become top-level.'),
   body={'name?': ('str', 'A channel name: 1 to 64 characters of any script. Spaces and capitals are allowed; no control characters, no leading or trailing space, and runs of spaces are collapsed to one. Not unique.', {'minLength': 1, 'maxLength': 64}),
         'topic?': ('str', 'Up to 250 characters.'),
         'welcome?': 'str',
         'private?': ('bool', 'Only `true`.'),
         'default_level?': 'enum:reader|member',
         'section_id?': ('uuid', 'One of the sections the admins defined.'),
         'parent_id?': {'$raw': {'type': ['string', 'null'], 'format': 'uuid', 'description': 'The named channel to put this one under, or null to clear it.'}},
         'retention_seconds?': ('int', "0 clears the channel's own period."),
         'slow_mode_seconds?': 'int',
         'broad_mentions?': ('enum:server|anyone|managers', 'Who may use `@channel`, `@here` and `@everyone` here. Managers set it.'),
         'lazy_delivery?': ('enum:server|on|off', 'Whether the channel is delivered in full only to the connections that have it open. Managers set it, in a named channel.'),
         'announcement?': ('bool', 'Make the channel an announcement channel, or an ordinary one again. Managers set it, in a named channel that is not shared with other servers (`conflict` otherwise). Nobody\'s level changes.'),
         'announcement_replies?': ('bool', 'Whether members who may not post may reply in threads. Only kept while the channel is an announcement channel.')},
   res={'conversation': '#Conversation'},
   err='forbidden not_found archived conflict')

op('POST conversations/{id}/join', 'joinConversation',
   'Join a public channel',
   who='Owner, admin or member. Guests and bots must be added.',
   desc=('For a public channel. Also for a private channel made by moving a thread, by members of the channel the '
         "thread came from. The caller joins at the channel's default level and sees all of its history."),
   res={'conversation': '#Conversation'},
   err='forbidden not_found archived locked')

op('POST conversations/{id}/leave', 'leaveConversation',
   'Leave a channel',
   who='Any member who is in the channel directly.',
   desc=('Refused with `conflict` for someone who is in through a group, and in a sealed chat, which can only be '
         'hidden from the sidebar. If the last manager leaves, the longest-standing member becomes manager. When the last member of a '
         'private or ad-hoc channel leaves, the channel and its content are deleted. Someone who leaves a locked '
         'channel cannot return.'),
   err='not_found conflict')

op('POST conversations/{id}/archive', 'archiveConversation',
   'Archive a channel',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to.',
   desc='An archived channel can be read and searched and accepts no posts.',
   res={'conversation': '#Conversation'},
   err='forbidden not_found')

op('DELETE conversations/{id}/archive', 'unarchiveConversation',
   'Bring an archived channel back',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to.',
   res={'conversation': '#Conversation'},
   err='forbidden not_found')

op('GET conversations/{id}/members', 'listMembers',
   'List the members of a conversation',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   q=['level:enum:reader|member|manager:Only this level.'],
   res={'members': '[#Member]'},
   err='not_found',
   page='cursor')

op('POST conversations/{id}/members', 'addMembers',
   'Add accounts to a channel',
   who='Any member of an ad-hoc channel. In a named channel, accounts acting as manager. Nobody, in a sealed chat. The caller must be able to see each account, and none of them may have blocked the caller.',
   desc=('Whoever is added sees the whole history. Adding to an ad-hoc channel past 50 members is refused with '
         '`too_large`. A remote account is never added to a channel that is not shared, nor to an ad-hoc channel '
         'that began local. In an ad-hoc channel that crosses servers, the accounts are invited through their '
         'servers: the answer is 200 and does not list them, and they appear with `member.joined` once their '
         'servers accept.'),
   body={'account_ids': '[uuid]', 'level?': ('enum:reader|member|manager', "Default: the channel's default level.")},
   res={'members': '[#Member]'},
   err='forbidden not_found archived locked too_large')

op('PATCH conversations/{id}/members/{account_id}', 'setMemberLevel',
   "Change a member's level in a channel",
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to.',
   desc=("Sets the level held directly. The member's level is the highest of that and the levels of their groups. A "
         "channel's last manager cannot lower their own level (`conflict`). A remote account is never a manager."),
   body={'level': 'enum:reader|member|manager'},
   res={'member': '#Member'},
   err='forbidden not_found conflict')

op('PUT conversations/{id}/members/{account_id}/may-post', 'setMemberMayPost',
   'Allow a member to post in an announcement channel, or stop allowing it',
   who='Accounts acting as manager of the channel, in a named channel.',
   desc=('In an announcement channel only its managers and the members allowed here post. The member must be at '
         'level `member` or above to use it: a `reader` posts nowhere. It can be set while the channel is not an '
         'announcement channel and applies once it is. The member is sent `membership.updated`.'),
   body={'may_post': 'bool'},
   res={'member': '#Member'},
   err='forbidden not_found')

op('DELETE conversations/{id}/members/{account_id}', 'removeMember',
   'Remove an account from a channel',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to. A manager cannot remove an admin or the owner.',
   desc=('Removes the direct membership. An account that is in through a group cannot be removed this way: remove '
         'the group, or take the person out of the group (`conflict`).'),
   err='forbidden not_found conflict')

op('GET conversations/{id}/groups', 'listChannelGroups',
   'List the groups added to a channel',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   res={'groups': '[#ChannelGroup]'},
   err='not_found')

op('PUT conversations/{id}/groups/{group_id}', 'addChannelGroup',
   'Add a group to a channel at a level, or change its level',
   who='Accounts acting as manager of a named channel, with the owner, admin or member role. Groups are not added to ad-hoc channels.',
   desc=('Everyone in the group is then in the channel at that level and stays in step as the group changes. In a '
         'locked channel a group cannot be added (`locked`), and a group already there brings in nobody new.'),
   body={'level': 'enum:reader|member|manager'},
   res={'group': '#ChannelGroup'},
   err='forbidden not_found archived locked')

op('DELETE conversations/{id}/groups/{group_id}', 'removeChannelGroup',
   'Remove a group from a channel',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to.',
   err='forbidden not_found')

op('PUT conversations/{id}/read', 'setReadPosition',
   "Move the caller's read position",
   who='Any member of the conversation.',
   desc='Moves it on every device. A lower number marks messages unread again.',
   body={'seq': 'int'},
   res={'membership': '#Membership'},
   err='not_found')

op('PATCH conversations/{id}/membership', 'updateMembership',
   "Set the caller's mode, mute, show, star, thread view and broad mentions for a conversation",
   who='Any member of the conversation, for themselves.',
   desc=('`mode` is what notifies and `show` is whether the conversation has a row in the sidebar. The two are '
         'independent: a hidden conversation still notifies and still counts in badges and totals. Mute is '
         '`muted_until`, not a mode: while it is set the conversation behaves as `nothing`, then `mode` returns. '
         "Send `muted_until` as null to unmute, `show` as null to follow the app's own default, and `thread_view` "
         'as null to follow the account preference. A sealed chat has no `mentions` mode. With '
         '`ignore_broad_mentions` true, `@channel`, `@here` and `@everyone` posted from then on write no mention '
         'for the caller here; a mention by name or of another group still does.'),
   body={'mode?': 'enum:any|mentions|nothing',
         'muted_until?': {'$raw': {'type': ['string', 'null'], 'format': 'date-time'}},
         'show?': {'$raw': {'type': ['string', 'null'], 'enum': ['always', 'activity', 'notification', 'hidden', None]}},
         'starred?': 'bool',
         'thread_view?': {'$raw': {'type': ['string', 'null'], 'enum': ['threaded', 'flat', None]}},
         'ignore_broad_mentions?': 'bool'},
   res={'membership': '#Membership'},
   err='not_found forbidden')

op('POST conversations/{id}/broad-mentions/check', 'checkBroadMentions',
   'Say what the broad mentions of a text would do',
   who='Members at level `member` or above, if the conversation is not archived or read-only.',
   desc=('Reads `text` as `postMessage` would and stores nothing. `broad` says it holds `@channel`, `@here` or '
         '`@everyone` outside code. `allowed` says the caller may use them here: its managers and the admins '
         'always, and anyone else unless the server or the channel limits broad mentions to managers or the '
         'channel has more members than `mentions.broad_mention_limit`. When it is false the message would be '
         'stored with those words as plain text and notify nobody, and a client says so before sending. '
         '`recipients` is how many people the broad mentions would write a mention for at this moment (for '
         '`@here`, the members online now; never the author, people who ignore broad mentions here or have '
         'blocked the author). `confirm` is true above 30 recipients: a client then asks "This will notify N '
         'people" before it posts.'),
   body={'text': ('str', 'Churr Markdown.', {'maxLength': 16000})},
   res={'broad': 'bool', 'allowed': 'bool', 'recipients': 'int', 'confirm': 'bool'},
   st=200,
   err='forbidden timed_out archived not_found')

op('POST conversations/{id}/typing', 'sendTyping',
   'Say the caller is typing',
   who='Members at level `member` or above.',
   desc='Not stored. Lasts eight seconds unless sent again. The live connection has a frame that does the same.',
   body={'parent_id?': ('uuid', 'When typing in a thread.')},
   err='forbidden not_found')

op('GET conversations/{id}/pins', 'listPins',
   "List a conversation's pinned messages",
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   res={'pins': '[#Pin]'},
   err='not_found')

op('PUT conversations/{id}/pins/{message_id}', 'pinMessage',
   'Pin a message',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to. There are no pins in a sealed chat.',
   desc='At most 100 pins per conversation; one more is refused with `conflict`.',
   err='forbidden not_found conflict archived')

op('DELETE conversations/{id}/pins/{message_id}', 'unpinMessage',
   'Unpin a message',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to.',
   err='forbidden not_found')

op('GET conversations/{id}/timeouts', 'listChannelTimeouts',
   'List the time-outs in force in a channel',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to.',
   res={'timeouts': '[#Timeout]'},
   err='forbidden not_found')

op('POST conversations/{id}/timeouts', 'timeOutMember',
   'Stop an account posting and reacting in a channel for a set time',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to. A manager cannot time out an admin or the owner.',
   desc=('The account can still read. It cannot post, react, tick, answer forms, edit, upload, join a call or send '
         'typing notices there. Written to the audit log.'),
   body='#TimeoutInput',
   res={'timeout': '#Timeout'},
   st=201,
   err='forbidden not_found')

op('DELETE conversations/{id}/timeouts/{timeout_id}', 'liftChannelTimeout',
   'Lift a time-out in a channel',
   who='Accounts acting as manager of the channel: its managers, and admins in a public channel or a private one they belong to.',
   err='forbidden not_found')

op('POST reports/{id}/handle', 'handleReport',
   'Handle a report',
   who='Whoever may handle it (see `listReports`). Each outcome needs the handler to hold that power anyway.',
   body={'outcome': 'enum:dismissed|deleted|timed_out', 'timeout_seconds?': ('int', 'With `timed_out`.')},
   res={'report': '#Report'},
   err='forbidden not_found conflict')

op('POST conversations/{id}/lock', 'lockConversation',
   'Lock a private channel for good',
   who='Managers of a private channel, named or ad-hoc. Any admin may lock a named private channel with `adminLockChannel`; an ad-hoc channel can be locked only from inside.',
   desc=('One-way. Afterwards nobody can be added or join, a group attached to it brings in nobody new (its current '
         'members stay), people can still leave, and someone who leaves cannot return. Sets `locked_at` and sends '
         '`conversation.updated`. A public channel is refused with `conflict`: make it private first.'),
   res={'conversation': '#Conversation'},
   err='forbidden not_found conflict')

op('GET reports', 'listReports',
   'List the reports the caller may handle',
   who='Any signed-in account. The list holds only reports the caller may handle: in a public channel, accounts acting as manager, which includes every admin; in a private or ad-hoc channel, only accounts whose level there is `manager`.',
   desc=('Reports made by people and messages the word filter flagged (`source` of `filter`) follow the same '
         "routing. A report about the caller's own message is left out if another handler exists."),
   q=['conversation_id:uuid:Only this conversation.', 'waiting:bool:Only reports nobody has handled.'],
   res={'reports': '[#Report]'},
   page='cursor')


# ----------------------------------------------------------------------------
area('Messages',
     'Reading, posting, editing and deleting messages, and the things that sit beside a message.')

op('GET conversations/{id}/messages', 'listMessages',
   'List messages in a conversation',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   desc=('A page is cut, and paged through, by `seq`, and comes in `seq` order. Replies are included among top-level '
         "messages unless `top_level` is true. `thread_states` holds the caller's state for the threads in the page; "
         'it is never inside the message object. A client shows what it has loaded sorted by `order`, not in the '
         'order of the page. In a conversation that is not shared the two are the same. In one shared with other '
         'servers a message that arrived late has a high `seq` and an early `order`: it is in the newest page, '
         'marked `late`, and belongs further up.'),
   q=['top_level:bool:Leave replies out, for the threaded view.',
      'around:int:Return the page centred on this `seq`, instead of `before` or `after`.'],
   res={'messages': '[#Message]', 'thread_states': '[#ThreadState]'},
   err='not_found',
   page='seq')

op('POST conversations/{id}/messages', 'postMessage',
   'Post a message',
   who='Members at level `member` or above, if the conversation is not archived or read-only. Managers, bots and webhooks are exempt from slow mode. Cards and the `bot` form action need a bot. The `channel` form action and `answerer_group_id` need an account acting as manager. The `group` form action needs an admin who is a member at `member` or above. In a conversation shared with other servers only `tally` forms are allowed.',
   desc=('Send `client_id` so a retry cannot post twice. Answers 201 for a new message and 200 for one already '
         'created with that `client_id`. `cannot_read_ids` names accounts the text mentions that cannot read this '
         'conversation, so the client can tell the author they will not be notified; it says nothing else about '
         'anyone. `broad_mentions_refused` is true when the text holds `@channel`, `@here` or `@everyone` and the '
         'author may not use them here: the message is stored with those words as plain text, with no entity, and '
         'they notify nobody. Bots and webhooks are held to the same rule. A reply to a thread that was moved to a channel is refused with `conflict`. In a sealed chat the '
         "body is `client_id`, `sealed`, `quiet` and `file_ids`; a `key_epoch` that is not the chat's current one is "
         'refused with `conflict`, and the device encrypts again. A sealed chat that can be read but not written in '
         '(sealed chats were switched off, or the other person is gone) answers `forbidden`.'),
   body='#MessageInput',
   res={'message': '#Message',
        'cannot_read_ids?': ('[uuid]', 'Mentioned accounts that cannot read this conversation.'),
        'broad_mentions_refused?': ('bool', 'The text holds a broad mention the author may not use here; it was stored as plain text and notified nobody.')},
   st=201,
   err='forbidden timed_out archived not_found too_large blocked_by_filter slow_mode conflict',
   also={'200': 'It already existed: the message made earlier with this `client_id`.'})

op('GET messages/{id}', 'getMessage',
   'Get one message',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   res={'message': '#Message'},
   err='not_found')

op('GET messages/{id}/thread', 'getThread',
   'Get a thread: the parent and its replies',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   desc='`{id}` may be the parent or any reply. Replies are paged by `seq`.',
   res={'parent': '#Message', 'messages': '[#Message]', 'thread_state?': '#ThreadState'},
   err='not_found',
   page='seq')

op('PATCH messages/{id}', 'editMessage',
   'Edit a message',
   who="Its author, at level `member` or above, within the edit window the server sets. Bots are not held to the edit window. Nobody can edit another account's message.",
   desc=('Inside the quick-correction window the message is not marked as edited and no earlier version is kept. '
         'After it, the earlier version is kept as history. A sealed message is never edited in place: the device posts '
         'an `edit` as a new sealed message (SEALED.md section 8.3), and this operation answers `invalid_request` for '
         'one.'),
   body='#MessageEdit',
   res={'message': '#Message'},
   err='forbidden timed_out archived not_found too_large blocked_by_filter conflict')

op('DELETE messages/{id}', 'deleteMessage',
   'Delete a message',
   who="Its author, while they can read the conversation. For another account's message, an account acting as manager of the channel. In a sealed chat, the author only. Only an admin may pass `purge`.",
   desc=('Leaves a marker saying who deleted it and when. The content is kept as history until purged or removed by '
         "retention. In a sealed chat the server erases the row's ciphertext and file blobs at once and leaves the "
         'marker.'),
   q=['purge:bool:Admins only: delete and purge in one step.'],
   res={'message': '#Message'},
   err='forbidden not_found')

op('GET messages/{id}/history', 'getMessageHistory',
   "Open a message's history",
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member. After a purge request in a public channel, admins only.',
   desc='Earlier versions, and the last content of a deleted message. Sealed chats keep none, and asking for one there answers `forbidden`.',
   res={'versions': '[#MessageVersion]'},
   err='not_found forbidden')

op('POST messages/{id}/purge', 'purgeMessage',
   "Purge a message's history, or ask for it",
   who='The author, for their own message, while they can read the conversation. An admin, for any message they can read. Not in a sealed chat, which keeps no history.',
   desc=('There must be something to purge: the message is deleted or has history (`conflict` otherwise). In a '
         "private or ad-hoc channel the purge happens at once (`purged`). In a public channel the author's request "
         'hides the history from everyone but admins at once and waits for an admin (`requested`); the outcome is '
         'never returned to a non-admin. After an admin chose to show the history again, asking again is refused '
         "with `conflict` until the message changes. An admin's own call deletes and purges in one step. Erased "
         'means deleted from the database, the search index and the file store.'),
   res={'state': 'enum:purged|requested', 'message': '#Message'},
   err='forbidden not_found conflict')

op('GET messages/{id}/reactions', 'listReactions',
   'List every reaction on a message with the accounts behind each',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   res={'reactions': '[#Reaction]'},
   err='not_found')

op('PUT messages/{id}/reactions/{emoji}', 'addReaction',
   'React to a message',
   who='Any member of the conversation, at any level. Not while timed out.',
   desc=('One of each emoji per account. A twenty-first different emoji on a message is refused with `conflict`. '
         "Another server's custom emoji is written `:name@server.name:` and can only be added where it is already on "
         'the message. Not used in sealed chats, where reactions are sealed messages.'),
   res={'reaction': '#Reaction'},
   err='forbidden timed_out not_found conflict archived')

op('DELETE messages/{id}/reactions/{emoji}', 'removeReaction',
   'Take back a reaction',
   who='The account that reacted.',
   err='not_found')

op('PUT messages/{id}/checklist/{item}', 'setChecklistItem',
   'Tick or untick a checklist item',
   who='Members at level `member` or above. Not while timed out.',
   desc='Not an edit. Every change is kept.',
   body={'checked': 'bool'},
   res={'task': '#Task'},
   err='forbidden timed_out archived not_found')

op('GET messages/{id}/checklist/{item}/history', 'getChecklistItemHistory',
   "Open a checklist item's history",
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   desc='Every tick and untick: who, which, when. Nothing here is ever hidden by a purge request.',
   res={'changes': '[#TaskChange]'},
   err='not_found')

op('DELETE messages/{id}/previews/{index}', 'removeLinkPreview',
   'Remove a link preview from a message',
   who="The message's author, at level `member` or above, or an account acting as manager of the channel.",
   desc="`{index}` is the card's position in `cards`, from 0.",
   res={'message': '#Message'},
   err='forbidden not_found')

op('POST messages/{id}/notify-anyway', 'notifyAnyway',
   "Push a message through one person's do not disturb",
   who="The message's author, for a message they typed and posted themselves. Not bots, webhooks or scheduled messages.",
   desc=('Always answers as if it worked. The server quietly does nothing for a target who would not have been '
         'notified anyway: one whose mode for the conversation is `nothing` or who has muted it, was reached only by `@channel`, '
         '`@here` or a group mention, has blocked the sender, has turned the feature off, is not in do not disturb, '
         'cannot read the conversation, or is an account on another server. The answer never says which, and carries '
         'nothing about the target. The message is marked `notified_anyway` for everyone whenever the author used '
         'it. One person per call.'),
   body={'account_id': 'uuid'},
   res={'message': '#Message'},
   err='forbidden not_found')

op('POST messages/{id}/report', 'reportMessage',
   'Report a message',
   who='In a public channel, anyone who can read it. In a private or ad-hoc channel, its members. Nobody, in a sealed chat.',
   desc=("In a private or ad-hoc channel the report goes to that channel's managers only. In a public channel it "
         'goes to accounts acting as manager, which includes the admins.'),
   body={'reason?': 'str'},
   res={'report_id': 'uuid'},
   st=201,
   err='not_found already_exists forbidden')

op('PUT messages/{id}/bookmark', 'addBookmark',
   'Bookmark a message for yourself',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   err='not_found')

op('DELETE messages/{id}/bookmark', 'removeBookmark',
   'Remove a bookmark',
   who='Any signed-in account.',
   err='not_found')

op('GET bookmarks', 'listBookmarks',
   "List the caller's bookmarks, newest first",
   who='Any signed-in account.',
   desc='A bookmark of a message the caller can no longer read is left out.',
   res={'bookmarks': ('[#Bookmark]', 'Each with its message.')},
   page='cursor')

op('POST messages/{id}/move-to-channel', 'moveThreadToChannel',
   'Move a thread out into a channel of its own',
   who="The author of the thread's parent message, at level `member` or above and with a role that may create channels; or an account acting as manager of the channel. Not in a shared channel, a sealed chat or an archived channel.",
   desc=('The replies are moved, not copied, and the move is not undone. The parent stays in the old channel '
         "untouched; a copy of it is the new channel's first message, and the replies become top-level messages "
         "after it. The new channel is private if the old one was, and is a sub-channel of the channel the thread was in, or, if that channel is itself a sub-channel, a sibling under the same parent. Its name need not be unique. The thread's followers become its members and "
         'anyone else in the old channel can join it. In the old channel a system reply of kind `thread_moved` under '
         'the parent points to the new channel, and the old thread accepts no more replies. A message that is a '
         'reply, a system notice or has no replies is refused with `invalid_request`; a thread already moved with '
         '`conflict`.'),
   body={'name': ('str', 'A channel name: 1 to 64 characters of any script. Spaces and capitals are allowed; no control characters, no leading or trailing space, and runs of spaces are collapsed to one. Not unique.', {'minLength': 1, 'maxLength': 64})},
   res={'conversation': '#Conversation'},
   st=201,
   err='forbidden not_found conflict archived')


# ----------------------------------------------------------------------------
area('Forms',
     'Answering forms and polls, and reading their results.')

op('PUT messages/{id}/form/answer', 'answerForm',
   'Answer a form, or change the answer',
   who='Any member of the conversation, at any level. Not bots, and not while timed out. Only members of `answerer_group_id`, when the form names one.',
   desc=('One answer per account unless the form has `one_answer` false. Changeable until the form closes, except on '
         '`thread` and `channel` forms, whose answers cannot be changed once sent. For a form with the `bot` action '
         'the answer is sent to the bot and stored nowhere, and a person may answer any number of times; if the bot '
         'is not connected the answer is refused with `unavailable`. In a shared conversation the answer goes, '
         'sealed, to the server the form was posted on, which keeps the tally. A form other than a `tally` in a '
         'conversation shared with other servers stopped taking answers when it was shared, and answers `conflict`. '
         'In an archived conversation the answer is `archived`.'),
   body={'answers': '#Answers'},
   res={'form': '#Form', 'interaction_id?': ('uuid', "For a bot's form.")},
   err='forbidden timed_out archived not_found conflict unavailable')

op('DELETE messages/{id}/form/answer', 'withdrawFormAnswer',
   "Withdraw the caller's answer",
   who='The account that answered, until the form closes, while it may still answer the form (`forbidden` otherwise).',
   res={'form': '#Form'},
   err='forbidden not_found conflict')

op('POST messages/{id}/form/buttons/{button}', 'pressFormButton',
   'Press a form button',
   who='Any member of the conversation, at any level. Not bots, and not while timed out. Only members of `answerer_group_id`, when the form names one.',
   desc=("Submits the form's current fields together with the button. For the `group` action it joins or leaves the "
         "button's group; for `channel` it opens the ticket channel, returned as `conversation_id`, as a sub-channel of the channel holding the form (or a sibling, if that channel is itself a sub-channel); for a bot's form "
         'it starts an interaction, returned as `interaction_id`.'),
   body={'answers?': '#Answers'},
   res={'form': '#Form', 'conversation_id?': 'uuid', 'interaction_id?': 'uuid'},
   err='forbidden timed_out not_found conflict unavailable')

op('POST messages/{id}/form/options', 'addFormOption',
   'Add an option to a choice field',
   who='Any member of the conversation, at any level. Only when the form has `allow_added_options`.',
   body={'field_id': 'str', 'label': 'str'},
   res={'form': '#Form'},
   st=201,
   err='forbidden not_found conflict')

op('POST messages/{id}/form/close', 'closeForm',
   'Close a form',
   who="The message's author, while they can read the conversation, or an account acting as manager of the channel.",
   desc=('A closed form keeps its result on the message and accepts no more answers. In a shared conversation only '
         'the server the form was posted on can close it; elsewhere this answers `forbidden`.'),
   res={'form': '#Form'},
   err='forbidden not_found conflict')

op('GET messages/{id}/form/results', 'listFormResults',
   'Read the answers to a form',
   who="For `collect`: the form's author and the channel's managers. For a named `tally`: anyone who can read the conversation, when `results` allows it for them.",
   desc='On an anonymous form the rows carry no account, for anyone.',
   res={'answers': '[#FormAnswer]'},
   err='forbidden not_found',
   page='cursor')

op('GET messages/{id}/form/results.csv', 'downloadFormResults',
   'Download the answers to a form as CSV',
   who='The same accounts as `listFormResults`.',
   err='forbidden not_found',
   raw={'200': {'description': 'One row per answer, one column per field.',
                'content': {'text/csv': {'schema': {'type': 'string'}}}}})


# ----------------------------------------------------------------------------
area('Threads',
     'Following, ignoring and reading threads, and the Threads view.')

op('GET threads', 'listThreads',
   'List the threads the caller follows that have unread replies, newest first',
   who='Any signed-in account.',
   desc="Across all the caller's conversations.",
   q=['all:bool:Include followed threads with nothing unread.'],
   res={'threads': ('[#ThreadEntry]', 'Each with its parent message.')},
   page='cursor')

op('PUT messages/{id}/thread/state', 'setThreadState',
   'Follow, ignore, or stop following a thread',
   who='Any member of the conversation, at any level.',
   desc=('`{id}` is the parent. `none` returns the thread to "neither". Replying in an ignored thread sets it back '
         'to following.'),
   body={'state': 'enum:following|ignored|none'},
   res={'thread_state?': '#ThreadState'},
   err='not_found forbidden')

op('PUT messages/{id}/thread/read', 'setThreadReadPosition',
   "Move the caller's read position in a thread",
   who='Any member of the conversation, at any level.',
   body={'seq': 'int'},
   res={'thread_state': '#ThreadState'},
   err='not_found forbidden')


# ----------------------------------------------------------------------------
area('Files',
     'Uploads, downloads and custom emoji.')

op('POST conversations/{id}/files', 'uploadFile',
   'Upload a file to a conversation',
   who='Members at level `member` or above.',
   desc=('The file is attached later by naming it in `file_ids`. An upload never attached is deleted after 24 hours. '
         'Kind and media type come from the content. The server makes no poster frame for a video: the client may '
         'send one image as `poster`. In a sealed chat the client uploads an encrypted blob, and the server stores '
         "it with an empty name and no thumbnail. In a shared conversation an upload over the stream's limit is "
         'refused with `too_large`.'),
   multipart={'file': 'bin',
              'caption?': 'str',
              'poster?': ('bin', 'For a video: one PNG, JPEG or WebP image to show before it plays.')},
   res={'file': '#File'},
   st=201,
   err='forbidden timed_out archived not_found too_large')

op('GET files/{id}/{name}', 'downloadFile',
   'Download a file',
   who='Any account that can read the conversation the file belongs to. Avatars, emoji and card images: any signed-in account that can see the thing they belong to.',
   desc=('Sent with a content type the server chose, `X-Content-Type-Options: nosniff` and `Content-Security-Policy: '
         "default-src 'none'; sandbox`. Kinds other than image, video and audio are sent as attachments. `Range` "
         'requests are supported. A file from another server that has not arrived answers `not_found`.'),
   err='not_found',
   raw={'200': {'description': 'The file.', 'content': {'*/*': {'schema': {'type': 'string', 'format': 'binary'}}}},
        '206': {'description': 'Part of the file.',
                'content': {'*/*': {'schema': {'type': 'string', 'format': 'binary'}}}}})

op('GET files/{id}/thumb', 'downloadThumbnail',
   "Download an image's thumbnail",
   who='The same accounts as `downloadFile`.',
   desc='At most 720 px on the long edge, re-encoded, with location and camera details removed.',
   err='not_found',
   raw={'200': {'description': 'The thumbnail.',
                'content': {'image/*': {'schema': {'type': 'string', 'format': 'binary'}}}}})

op('GET emoji', 'listEmoji',
   'List the custom emoji',
   who='Any signed-in account.',
   desc='The whole list, including emoji that came with shared channels, each with its `server_name`.',
   res={'emoji': '[#Emoji]'})

op('POST emoji', 'createEmoji',
   'Add a custom emoji',
   who='Admins. Members too, if the setting allows.',
   desc="At most 256 KB and 256 px square. Names are unique among the server's own emoji.",
   multipart={'name': 'str', 'file': 'bin'},
   res={'emoji': '#Emoji'},
   st=201,
   err='forbidden name_taken too_large')

op('DELETE emoji/{id}', 'deleteEmoji',
   'Delete a custom emoji',
   who='Admins. The account that added it, while members may add emoji.',
   err='forbidden not_found')

op('GET files/{id}/poster', 'downloadPoster',
   "Download a video's poster image",
   who='The same accounts as `downloadFile`.',
   err='not_found',
   raw={'200': {'description': 'The poster image.',
                'content': {'image/*': {'schema': {'type': 'string', 'format': 'binary'}}}}})


# ----------------------------------------------------------------------------
area('Search',
     'Full-text search across what the caller may read.')

op('GET search', 'search',
   'Search messages',
   who='Any signed-in account.',
   desc=('Covers channels the caller can read, replies included. Never sealed chats, and never message history. '
         'Paged by cursor. `q` may be empty when a filter (`in`, `from`, `since`, `until` or `has_file`) is given; '
         'with neither, the answer is `invalid_value`. Each result carries the message and a snippet, not its '
         'conversation.'),
   q=['q:str:The words to find. May be empty when a filter is given.', 'in:uuid:Only this conversation.', 'from:uuid:Only this author.',
      'since:time:Only messages posted at or after this time.', 'until:time:Only messages posted before this time.',
      'has_file:bool:Only messages with a file.'],
   res={'results': '[#SearchResult]'},
   page='cursor')


# ----------------------------------------------------------------------------
area('Events',
     'The live connection and catch-up. docs/EVENTS.md describes the frames and every event.')

op('GET events', 'openEvents',
   'Open the live connection (WebSocket)',
   who="Any signed-in account. With a cookie the `Origin` must be the server's own.",
   desc='An HTTP upgrade to a WebSocket with subprotocol `churr.v1`. See docs/EVENTS.md.',
   err='forbidden',
   raw={'101': {'description': 'Switching to the WebSocket protocol.'}})

op('GET conversations/{id}/events', 'listEvents',
   "Catch up on a conversation's events",
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   desc=('Events with `seq` greater than `after`, oldest first, up to `until` if given, so a client can fill one '
         'gap. Events are kept for 30 days. If `after` is older than what is kept, `reload` is true and `events` is '
         'empty: the client drops what it holds for the conversation and loads it again. In a shared conversation '
         'some numbers may be missing.'),
   q=['after!:int:The last `seq` the client has.', 'until:int:Stop at this `seq`, to fill a gap.',
      'limit:int:Default 50, at most 200.'],
   res={'events': '[#Event]', 'has_more': 'bool', 'reload?': 'bool'},
   err='not_found')


# ----------------------------------------------------------------------------
area('Scheduled',
     'Messages to send later, reminders and recurring forms.')

op('GET scheduled', 'listScheduled',
   "List the caller's scheduled items",
   who='Any signed-in account.',
   desc='A channel manager also sees the recurring forms of channels they manage with `conversation_id`.',
   q=['kind:enum:message|reminder|form:Only this kind.', 'conversation_id:uuid:Only this conversation.'],
   res={'scheduled': '[#ScheduledItem]'},
   page='cursor')

op('POST scheduled', 'createScheduled',
   'Schedule a message, a reminder or a recurring form',
   who='For `message` and `reminder`: any person who may post in the conversation. For `form` (a recurring form such as a check-in): managers of the channel. Not bots, and not in sealed chats.',
   desc=('When it runs it is posted as an ordinary message with `via.kind` of `scheduled` or `reminder`. The request '
         'is checked as if posted now, and again when it runs. A scheduled message does not repeat. An account may '
         'have at most 200 waiting items (`conflict`).'),
   body='#ScheduledInput',
   res={'scheduled': '#ScheduledItem'},
   st=201,
   err='forbidden not_found archived too_large conflict blocked_by_filter')

op('GET scheduled/{id}', 'getScheduled',
   'Get one scheduled item',
   who='Its owner, or a manager of the channel for a recurring form.',
   res={'scheduled': '#ScheduledItem'},
   err='not_found')

op('PATCH scheduled/{id}', 'updateScheduled',
   'Change a scheduled item before it runs',
   who='Its owner, or a manager of the channel for a recurring form.',
   desc='Whoever edits a recurring form becomes its owner.',
   body={'content?': '#MessageInput', 'run_at?': 'time', 'repeat?': '#Repeat'},
   res={'scheduled': '#ScheduledItem'},
   err='forbidden not_found conflict')

op('DELETE scheduled/{id}', 'cancelScheduled',
   'Cancel a scheduled item',
   who='Its owner, or a manager of the channel for a recurring form.',
   err='not_found')

op('POST scheduled/{id}/send', 'sendScheduledNow',
   'Send a scheduled message now',
   who='Its owner.',
   desc='Also how a failed item is sent by hand.',
   res={'scheduled': '#ScheduledItem', 'message': '#Message'},
   err='forbidden not_found archived conflict')


# ----------------------------------------------------------------------------
area('Commands',
     'What can be typed as `/name`: built-in commands, saved replies and bot commands.')

op('GET commands', 'listCommands',
   'List the commands available in a conversation',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   desc=("The built-in commands, the caller's saved replies, the custom commands (community saved replies), and the "
         'commands of bots that are in the conversation. The whole list.'),
   q=['conversation_id!:uuid:The conversation the person is typing in.'],
   res={'commands': '[#Command]'},
   err='not_found')

op('POST conversations/{id}/commands', 'runCommand',
   'Run a bot command or a saved reply',
   who='Any local member of the conversation, at any level. Not while timed out.',
   desc=("The server looks the name up in this order and stops at the first match: the caller's own saved replies, "
         'custom commands, then commands of bots in this conversation. No match is `not_found`. A saved reply or '
         'custom command is posted as a message from the caller, who must be allowed to post; text after its name is '
         'refused with `invalid_value`. A bot command posts nothing: the server makes an interaction, sends the bot '
         '`command.invoked`, and returns the `interaction_id` at once; a bot that is not connected gives '
         '`unavailable`. Built-in commands are carried out by the client with ordinary calls and are refused here '
         'with `invalid_value`.'),
   body={'name': 'str',
         'text?': ('str', 'What was typed after the name. Up to 4,000 characters.'),
         'parent_id?': ('uuid', 'When typed in a thread.')},
   res={'interaction_id?': ('uuid', 'For a bot command.'), 'message?': ('#Message', 'For a saved reply.')},
   err='forbidden timed_out archived not_found unavailable slow_mode blocked_by_filter')

op('GET saved-replies', 'listSavedReplies',
   "List the community's saved replies and the caller's own",
   who='Any signed-in account.',
   desc='The whole list.',
   res={'saved_replies': '[#SavedReply]'})

op('POST saved-replies', 'createSavedReply',
   'Save a personal reply',
   who='Any signed-in person.',
   body={'name': 'str', 'text': 'str'},
   res={'saved_reply': '#SavedReply'},
   st=201,
   err='name_taken')

op('PATCH saved-replies/{id}', 'updateSavedReply',
   'Change a personal saved reply',
   who='Its owner.',
   body={'name?': 'str', 'text?': 'str'},
   res={'saved_reply': '#SavedReply'},
   err='not_found name_taken')

op('DELETE saved-replies/{id}', 'deleteSavedReply',
   'Delete a personal saved reply',
   who='Its owner.',
   err='not_found')
