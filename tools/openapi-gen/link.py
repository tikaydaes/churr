"""The link protocol in Protocol Buffers (ruling R70, docs/LINK-PROTOCOL.md section 2).

Everything servers and hubs say to each other is declared here and written, by the same build as the
client schema, to:

    proto/churr/link/v1/link.proto        the schema
    proto/churr/link/v1/link.lock.json    every number ever given out
    internal/link/wire/schema_gen.go      the same schema as a table the link's codec walks

Numbers never change: the lock is read first, a field in it keeps its number, a new one takes the
next number after the highest its message ever used, and one that has gone stays in the lock and is
written as `reserved`. Running the build with no change here changes no file.

The notation, one type per field:

    'str' 'int' 'double' 'bool' 'bytes'
    'uuid'             an identifier: 16 bytes on the wire
    'time'             a time: int64 microseconds on the wire; an RFC 3339 text in the server's JSON
    'ms'               a time the server holds as whole milliseconds: int64 microseconds on the wire
    'value'            any JSON value: churr.v1.Value
    '#Name'            a message of this file
    'client:Name'      a message of the client schema (proto/churr/v1/churr.proto), encoded by its codec
    '[type]'           a list
    '{type}'           a map with text keys
    '<type>'           a map whose keys are identifiers

There are three unions: the frame of a connection, the plaintext of a direct message, and the
plaintext of an envelope (an inner event). Each has many members that share fields, so each has one
dictionary of fields (FRAME_FIELDS, DIRECT_FIELDS, EVENT_FIELDS) and every member lists the fields it
carries. A field has the same number in every member that carries it.

The ORDER of the members matters once, when each is first given its number in its union: the ones
sent most often come first and get the numbers that take one byte.
"""
import collections
import json
import os
import re
import shutil
import subprocess

OD = collections.OrderedDict

# The link protocol version this schema belongs to (docs/LINK-PROTOCOL.md section 5). It is the
# version of the handshake and of the well-known document, not a count of schema changes: fields and
# members are added without changing it.
VERSION = 3
PROTO_DIR = ('proto', 'churr', 'link', 'v1')

MSGS = OD()      # name -> {'fields': OD(json -> type), 'doc': str, 'wrap': bool, 'base': str or None, 'of': bag or None}
UNIONS = OD()    # name -> {'own': [(json, type)], 'first': int, 'members': OD(kind -> message), 'doc': str}
BLOCKS = OD()    # signed block name -> message
BAGS = {}        # bag name -> OD(json -> type)


def M(name, fields, doc=''):
    assert name not in MSGS, name
    MSGS[name] = {'fields': OD(fields), 'doc': doc, 'wrap': False, 'base': None, 'of': None}


def W(name, typ, doc=''):
    """A message that stands for a list or a map where protobuf cannot nest one directly."""
    assert name not in MSGS, name
    MSGS[name] = {'fields': OD([('values', typ)]), 'doc': doc, 'wrap': True, 'base': None, 'of': None}


def X(name, base, fields, doc=''):
    """A client message with fields of the link's own beside it: the client's fields travel whole
    in field 1, encoded by the client codec, and in JSON all of them are one object."""
    assert name not in MSGS, name
    MSGS[name] = {'fields': OD(fields), 'doc': doc, 'wrap': False, 'base': base, 'of': None}


def camel(s):
    return ''.join(w[:1].upper() + w[1:] for w in re.split(r'[^A-Za-z0-9]+', s) if w)


def member(union, bag, suffix, kinds, fields, doc=''):
    for kind in kinds.split():
        name = camel(kind) + suffix
        assert name not in MSGS, name
        for f in fields.split():
            assert f in BAGS[bag], (kind, f)
        MSGS[name] = {'fields': OD((f, BAGS[bag][f]) for f in fields.split()), 'doc': doc, 'wrap': False,
                      'base': None, 'of': bag}
        UNIONS[union]['members'][kind] = name


# ---------------------------------------------------------------------------------- what frames carry

M('Limits', [('max_frame_bytes', 'int'), ('max_envelope_bytes', 'int'), ('submits_per_minute', 'int'),
             ('ephemeral_per_minute', 'int'), ('direct_per_minute', 'int'), ('blob_bytes_per_hour', 'int')],
  'What the sender of a hello will accept from the other side.')
M('Envelope', [('stream_id', 'uuid'), ('key_epoch', 'int'), ('origin', 'str'), ('origin_event_id', 'uuid'),
               ('at', 'ms'), ('n', 'int'), ('offset', 'int'), ('ciphertext', 'bytes'), ('sig', 'bytes'),
               ('cursor', 'int'), ('prev', 'int')],
  'One signed, encrypted event of a stream (section 9). sig is over a byte layout built from the first eight '
  'fields, never over this message; cursor and prev are added by whoever delivers it and are not signed.')
M('Clock', [('offset_ms', 'int'), ('latency_ms', 'int'), ('jitter_ms', 'int'), ('uncertainty_ms', 'int'),
            ('forward_ms', 'int'), ('forward_jitter_ms', 'int'), ('back_ms', 'int'), ('back_jitter_ms', 'int'),
            ('asymmetry_ms', 'int'), ('synced', 'bool'), ('samples', 'int'), ('measured_at', 'time')],
  'What a time reference has measured of one server\'s clock (section 5). Durations are milliseconds.')
M('HomeClock', [('offset_ms', 'int'), ('uncertainty_ms', 'int'), ('measured_at', 'time')],
  'A server\'s clock against a hub it is not connected to (section 23b).')
M('ClusterEntry', [('signed', 'bytes'), ('sig', 'bytes'), ('via_sig', 'bytes'), ('gone', 'bytes'), ('gone_sig', 'bytes')],
  'One admission to a cluster: a signed ClusterMember with its signatures and, once it is over, a signed ClusterGone.')
M('SyncStream', [('v', 'int'), ('have', '{int}')], 'What a hub holds of one stream when a trunk comes up.')
M('RelayItem', [('envelope', '#Envelope'), ('prev', 'int'), ('window', '[ms]')],
  'One envelope of a relay frame and what the sending hub adds beside it.')
M('RelayRefusal', [('stream_id', 'uuid'), ('origin_event_id', 'uuid'), ('origin', 'str'), ('code', 'str'), ('message', 'str')],
  'An envelope a hub would not take from a trunk.')
M('HubClock', [('offset_ms', 'int'), ('uncertainty_ms', 'int'), ('age_ms', 'int')], 'One line of a hub_clocks frame.')
W('Counters', '{int}', 'For each origin, a counter.')
M('Handoff', [('creator', 'str'), ('inviters', '{str}'), ('joined_have', '{#Counters}'), ('declined', '[str]'),
              ('move_id', 'str'), ('epoch', 'int'), ('keys', 'bytes')],
  'What a channel\'s home hub keeps beside the record, handed to the hub that takes the home over (section 23b). keys is '
  'a HandoffKeys message sealed from the old home to the new one (section 7, Sealing).')
M('HandoffKey', [('epoch', 'int'), ('key', 'bytes'), ('servers', '[str]')], 'One key epoch\'s stream key and the servers it began for.')
M('HandoffKeys', [('keys', '[#HandoffKey]')], 'Every stream key a channel\'s home hub holds, handed to the hub that takes the home over.')

BAGS['frame'] = OD([
    # hello
    ('versions', '[int]'), ('version', 'int'), ('kind', 'str'), ('name', 'str'), ('sign_key', 'bytes'),
    ('box_key', 'bytes'), ('box_key_sig', 'bytes'), ('nonce', 'bytes'), ('software', 'str'), ('limits', '#Limits'),
    # auth, signed blocks, direct
    ('sig', 'bytes'), ('invite_proof', 'bytes'), ('signed', 'bytes'), ('home_hub', 'str'),
    # submit, deliver, ephemeral
    ('envelope', '#Envelope'), ('envelopes', '[#Envelope]'),
    # ack, catch_up, caught_up, accepted, error
    ('stream_id', 'uuid'), ('cursor', 'int'), ('have', '{int}'), ('floor', '{int}'), ('ids', '[uuid]'),
    ('origin_event_id', 'uuid'),
    # direct
    ('from', 'str'), ('to', 'str'), ('body', 'bytes'),
    # ping, pong
    ('t0', 'ms'), ('t1', 'ms'), ('t2', 'ms'), ('clock', '#Clock'), ('homes', '{#HomeClock}'),
    # between hubs
    ('cluster', '[#ClusterEntry]'), ('streams', '<#SyncStream>'), ('done', 'bool'), ('items', '[#RelayItem]'),
    ('refused', '[#RelayRefusal]'), ('hub', 'str'), ('server', 'str'), ('inner', 'bytes'), ('clocks', '{#HubClock}'),
    ('reach', '[str]'), ('joined_have', '{#Counters}'), ('also', '[str]'), ('state', '#Handoff'), ('step', 'str'),
    ('offer', 'str'),
    # error, bye
    ('code', 'str'), ('message', 'str'), ('retry_after_seconds', 'int'), ('reason', 'str'),
])

UNIONS['Frame'] = {'own': [('id', 'int'), ('re', 'int')], 'first': 3, 'members': OD(),
                   'doc': 'One frame of a link connection: one binary WebSocket message (section 6). The body that is '
                          'set says which frame it is.'}


def F(kinds, fields, doc=''):
    member('Frame', 'frame', 'Frame', kinds, fields, doc)


CONTROL = 'signed sig have home_hub'
# The ones sent most often.
F('submit deliver', 'envelope envelopes')
F('accepted', 'ids code message stream_id origin_event_id clock retry_after_seconds')
F('ack', 'stream_id cursor')
F('ephemeral', 'envelope')
F('direct', 'from to body sig')
F('ping', 't0 clock homes')
F('pong', 't0 t1 t2')
F('relay', 'items')
F('relay_ack', 'refused')
F('error', 'code message retry_after_seconds stream_id origin_event_id clock')
# The handshake.
F('hello', 'versions version kind name sign_key box_key box_key_sig nonce software limits')
F('auth', 'sig invite_proof')
F('ready', '')
F('bye', 'reason')
# Streams.
F('catch_up', 'stream_id have')
F('caught_up', 'stream_id have floor')
F('stream_open server_joined server_left leave owner_changed', CONTROL,
  'A stream control frame: a signed Control (section 6).')
F('stream_close', CONTROL + ' stream_id reason hub',
  'A signed Control. Unsigned, with stream_id, reason and hub: a hub\'s word to its own server that a stream has stopped for it.')
# The hub and its channels.
F('roster', 'signed sig', 'A signed Roster.')
F('channel_create', 'signed sig', 'A signed ChannelCreate.')
F('channel', 'signed sig home_hub have joined_have also state', 'A signed ChannelRecord.')
F('channel_action join', 'signed sig', 'A signed ChannelAction.')
F('directory', 'signed sig', 'A signed Directory; with nothing in it, the request for one.')
F('channel_commit', 'signed sig inner', 'A signed MoveCommit; inner is the channel frame the hub signed when it prepared the move.')
# Trunks.
F('cluster', 'hub cluster')
F('cluster_join', 'cluster')
F('hub_servers', 'signed sig', 'A signed Roster of one hub\'s own servers.')
F('hub_clocks', 'clocks reach')
F('sync', 'streams done')
F('ask', 'hub server inner', 'inner is a Frame, as the server sent it.')
F('forward', 'server inner', 'inner is a Frame for that server.')
F('rehome', 'from to step offer stream_id signed sig state message',
  'A channel\'s home offered to another hub, and that hub\'s answer. With an offer, signed is the ChannelRecord that hands '
  'the channel over; with a refusal, a RehomeAnswer.')
F('stream_ctl', 'inner joined_have also', 'inner is a stream control Frame, as the reference relayed it.')

# ------------------------------------------------------------------------------- what is signed as a block

M('RosterServer', [('name', 'str'), ('sign_key', 'bytes'), ('box_key', 'bytes'), ('box_key_sig', 'bytes'), ('joined_at', 'time')])
M('Roster', [('hub', 'str'), ('version', 'int'), ('at', 'time'), ('servers', '[#RosterServer]')],
  'The servers on a hub and their keys, signed by the hub.')
M('Control', [('stream_id', 'uuid'), ('owner', 'str'), ('server', 'str'), ('new_owner', 'str'), ('reason', 'str'),
              ('servers', '[str]'), ('version', 'int'), ('at', 'time')],
  'The signed content of a stream control frame.')
M('ChannelAccount', [('account_id', 'uuid'), ('server', 'str'), ('username', 'str')],
  'An account named in a channel record: one of its managers or moderators, with its home instance.')
M('ChannelRecord', [('stream_id', 'uuid'), ('hub', 'str'), ('hub_key', 'bytes'), ('home_hub', 'str'), ('version', 'int'),
                    ('name', 'str'), ('description', 'str'), ('listing', 'str'), ('servers', '[str]'),
                    ('managers', '[#ChannelAccount]'), ('invited', '[str]'), ('requests', '[str]'), ('epoch', 'int'),
                    ('joined', '[str]'), ('left', '[str]'), ('closed', 'bool'),
                    ('prepared', 'bool'), ('at', 'time'), ('private', 'bool'), ('moderators', '[#ChannelAccount]'),
                    ('history_days', 'int')],
  'Everything a hub records of a channel it owns, whole, at one version, signed by the hub (section 23a).')
M('ChannelCreate', [('stream_id', 'uuid'), ('name', 'str'), ('description', 'str'), ('listing', 'str'),
                    ('creator', '#ChannelAccount'), ('move_id', 'uuid'), ('servers', '[str]'), ('managers', '[#ChannelAccount]'),
                    ('epoch', 'int'), ('accept', 'bytes'), ('accept_sig', 'bytes'), ('at', 'time'),
                    ('private', 'bool'), ('history_days', 'int')],
  'A server shares a channel to a hub, signed by that server.')
M('MoveAccept', [('move_id', 'uuid'), ('stream_id', 'uuid'), ('hub', 'str'), ('hub_key', 'bytes'), ('owner', 'str'),
                 ('server', 'str'), ('managers', '[#ChannelAccount]'), ('expires_at', 'time'), ('at', 'time')],
  'The other server of a direct link agrees to move a stream onto a hub (section 21a).')
M('MoveCommit', [('move_id', 'uuid'), ('stream_id', 'uuid'), ('hub', 'str'), ('owner', 'str'), ('have', '{int}'),
                 ('have_at', '{ms}'), ('epoch', 'int'), ('managers', '[#ChannelAccount]'), ('at', 'time')],
  'The former owner server says it has committed a move.')
M('ChannelAction', [('stream_id', 'uuid'), ('action_id', 'uuid'), ('action', 'str'), ('by', '#ChannelAccount'), ('server', 'str'),
                    ('account', '#ChannelAccount'), ('name', 'str'), ('description', 'str'), ('listing', 'str'), ('at', 'time')],
  'What a manager asks of the hub, signed by the manager\'s home instance; and an instance\'s wish to join.')
M('DirectoryEntry', [('stream_id', 'uuid'), ('name', 'str'), ('description', 'str'), ('listing', 'str'),
                     ('managers', '[#ChannelAccount]'), ('servers', 'int'), ('yours', 'str')])
M('Directory', [('hub', 'str'), ('hub_key', 'bytes'), ('at', 'time'), ('channels', '[#DirectoryEntry]')],
  'A hub\'s listed channels, signed by the hub.')
M('ClusterMember', [('cluster', 'bytes'), ('hub', 'str'), ('sign_key', 'bytes'), ('url', 'str'), ('via', 'str'),
                    ('joined_at', 'time'), ('box_key', 'bytes')],
  'One hub\'s admission to a cluster, signed by that hub and countersigned by the member that let it in.')
M('ClusterGone', [('cluster', 'bytes'), ('hub', 'str'), ('joined_at', 'time'), ('by', 'str'), ('at', 'time')],
  'An admission is over.')
M('RehomeAnswer', [('offer', 'str'), ('stream_id', 'uuid'), ('hub', 'str'), ('dead', 'bool'), ('why', 'str'), ('at', 'time')],
  'A hub\'s signed word that it will never take an offer of a channel\'s home.')

BLOCKS.update([
    ('roster', 'Roster'), ('hub_servers', 'Roster'),
    ('stream_open', 'Control'), ('server_joined', 'Control'), ('server_left', 'Control'), ('leave', 'Control'),
    ('owner_changed', 'Control'), ('stream_close', 'Control'),
    ('channel', 'ChannelRecord'), ('channel_create', 'ChannelCreate'), ('move_accept', 'MoveAccept'),
    ('channel_commit', 'MoveCommit'), ('channel_action', 'ChannelAction'), ('join', 'ChannelAction'), ('directory', 'Directory'),
    ('cluster_member', 'ClusterMember'), ('cluster_gone', 'ClusterGone'), ('rehome_answer', 'RehomeAnswer'),
])

# --------------------------------------------------------------------- what events and direct messages carry

X('Entity', 'Entity', [('stream_id', 'uuid')],
  'An entity of a message. A link to the stream\'s own channel carries stream_id in place of conversation_id.')
M('File', [('id', 'uuid'), ('name', 'str'), ('size', 'int'), ('media_type', 'str'), ('kind', 'str'), ('width', 'int'),
           ('height', 'int'), ('duration_seconds', 'int'), ('caption', 'str'), ('sha256', 'bytes')],
  'A file entry as it travels: no addresses, and the hash of its bytes.')
M('Blob', [('id', 'uuid'), ('sha256', 'bytes'), ('size', 'int'), ('media_type', 'str'), ('purpose', 'str'), ('name', 'str')],
  'A file an event refers to (section 11).')
M('Message', [('id', 'uuid'), ('author_id', 'uuid'), ('client_id', 'str'), ('parent_id', 'uuid'), ('quote', 'client:Quote'),
              ('text', 'str'), ('entities', '[#Entity]'), ('files', '[#File]'), ('cards', '[client:Card]'),
              ('form', 'client:Form'), ('fallback', 'str'), ('via', 'client:Via'), ('system', 'value'),
              ('sealed', 'client:SealedPayload'), ('quiet', 'bool'), ('action', 'bool'), ('chance', 'client:Chance'),
              ('created_at', 'time'), ('edited_at', 'time')],
  'A message as it travels between servers (section 11, "A message on the wire").')
M('Profile', [('id', 'uuid'), ('server', 'str'), ('username', 'str'), ('display_name', 'str'), ('title', 'str'),
              ('pronouns', 'str'), ('time_zone', 'str'), ('bot', 'bool'), ('avatar_file_id', 'uuid'), ('version', 'int'),
              ('removed', 'bool')],
  'An account\'s profile as it travels (section 14).')
M('Presence', [('account_id', 'uuid'), ('presence', 'str'), ('in_call', 'bool'), ('dnd', 'bool'), ('status', 'value'),
               ('availability', 'value')])
M('HistoryConversation', [('name', 'str'), ('topic', 'str')])
M('HistoryReaction', [('emoji', 'str'), ('account_ids', '[uuid]')])
M('HistoryMessage', [('order', 'str'), ('message', '#Message'), ('reactions', '[#HistoryReaction]'), ('tasks', '[client:Task]')])
M('AddedOption', [('field_id', 'str'), ('id', 'str'), ('label', 'str')])
M('FormAnswer', [('button_id', 'str'), ('answers', 'value'), ('added_options', '[#AddedOption]')],
  'An answer to a form, sealed to the form\'s home server inside form.answered (section 19).')
M('CallMedia', [('host', 'str'), ('udp_port', 'int'), ('tcp_port', 'int')])
M('DeviceListVersion', [('version', 'int'), ('signed_list', 'bytes')])
M('DeviceLists', [('account_id', 'uuid'), ('versions', '[#DeviceListVersion]')])
M('DeafEars', [('account_id', 'uuid'), ('account_ids', '[uuid]')],
  'One account of the sender and the accounts of the receiver it has blocked: they are shown it online and nothing more '
  '(section 17a).')

BAGS['direct'] = OD([
    ('stream_id', 'uuid'), ('reason', 'str'), ('code', 'str'), ('message', 'str'),
    # key, key_request
    ('epoch', 'int'), ('key', 'bytes'), ('version', 'int'), ('history_days', 'int'),
    # offer
    ('stream_kind', 'str'), ('owner', 'str'), ('name', 'str'), ('topic', 'str'), ('private', 'bool'), ('servers', '[str]'), ('invited', '[str]'),
    ('max_blob_bytes', 'int'), ('expires_at', 'time'), ('by_account', '#Profile'), ('account_ids', '[uuid]'),
    ('members', '[#Profile]'), ('device_lists', '#DeviceLists'), ('epoch_record', 'bytes'),
    # history, history_request
    ('part', 'int'), ('last', 'bool'), ('conversation', '#HistoryConversation'), ('accounts', '[#Profile]'),
    ('member_ids', '[uuid]'), ('messages', '[#HistoryMessage]'), ('blobs', '[#Blob]'), ('after_at', 'ms'),
    ('through_at', 'ms'), ('first', 'bool'),
    # open_request, opened
    ('envelope', '#Envelope'), ('plain', 'bytes'),
    # blob_request, blob_chunk
    ('blob_id', 'uuid'), ('from_index', 'int'), ('index', 'int'), ('data', 'bytes'),
    # calls
    ('call_id', 'uuid'), ('account_id', 'uuid'), ('pass', 'str'), ('media', '#CallMedia'), ('signal', 'value'),
    # device lists
    ('versions', '[#DeviceListVersion]'), ('by_account_id', 'uuid'), ('after_version', 'int'),
    # moves
    ('move_id', 'uuid'), ('hub', 'str'), ('hub_fingerprint', 'str'), ('accept', 'bytes'), ('accept_sig', 'bytes'),
    # deaf ears
    ('deaf_ears', '[#DeafEars]'), ('ask', 'bool'),
])

UNIONS['Direct'] = {'own': [('msg_id', 'uuid'), ('at', 'time'), ('re', 'uuid'), ('json', 'bytes')], 'first': 5,
                    'members': OD(),
                    'doc': 'The plaintext of a direct message (section 7). The body that is set is its kind. One the schema '
                           'cannot hold exactly travels whole as JSON text in `json`, with no body.'}


def D(kinds, fields, doc=''):
    member('Direct', 'direct', 'Direct', kinds, fields, doc)


D('blob_chunk', 'stream_id blob_id index last data')
D('blob_request', 'stream_id blob_id from_index')
D('key', 'stream_id epoch key version')
D('key_request', 'stream_id epoch')
D('history', 'stream_id part last conversation accounts member_ids messages blobs')
D('history_request', 'stream_id after_at through_at first')
D('open_request', 'stream_id envelope')
D('opened', 'stream_id plain')
D('error', 'code message')
D('offer', 'stream_id stream_kind owner name topic private servers invited history_days max_blob_bytes expires_at by_account '
           'account_ids members device_lists epoch_record')
D('accept', 'stream_id')
D('decline', 'stream_id reason')
D('call_pass_request', 'stream_id call_id account_id')
D('call_pass', 'call_id account_id pass expires_at media')
D('call_signal', 'stream_id call_id account_id pass signal')
D('device_list', 'account_id versions')
D('device_list_request', 'account_id by_account_id after_version')
D('move_propose', 'move_id stream_id hub hub_fingerprint expires_at')
D('move_accept', 'move_id stream_id accept accept_sig')
D('move_decline', 'move_id stream_id')
D('move_abandon', 'move_id stream_id reason')
D('deaf_ears', 'deaf_ears ask')

BAGS['event'] = OD([
    ('message', '#Message'), ('blobs', '[#Blob]'), ('message_id', 'uuid'), ('account_id', 'uuid'),
    # deletes, expiry
    ('deleted_by_id', 'uuid'), ('deleted_at', 'time'), ('removed_by_id', 'uuid'), ('removed_at', 'time'),
    ('removed_by', '#Profile'), ('erase', 'str'), ('through_at', 'ms'), ('scope', 'str'),
    # previews, reactions, tasks
    ('cards', '[client:Card]'), ('url', 'str'), ('emoji', 'value'), ('item_id', 'str'), ('checked', 'bool'),
    # forms
    ('form_id', 'uuid'), ('answer_box', 'bytes'), ('answer_event_id', 'uuid'), ('reason', 'str'), ('field_id', 'str'),
    ('option', 'client:FormOption'), ('added_by_id', 'uuid'), ('answer_count', 'int'), ('tally', '{client:FormTallyValue}'),
    ('voters', '{client:FormVotersValueOuter}'), ('closed_by_id', 'uuid'), ('closed_at', 'time'),
    # people and the conversation
    ('account', '#Profile'), ('joined_at', 'time'), ('by_account_id', 'uuid'), ('server', 'str'), ('name', 'str'),
    ('topic', 'str'), ('by_account', '#Profile'),
    # calls
    ('call_id', 'uuid'), ('started_by_id', 'uuid'), ('host', 'str'), ('call', 'client:Call'),
    ('participant', 'client:CallParticipant'), ('ended_at', 'time'), ('duration_seconds', 'int'),
    # the stream itself
    ('version', 'int'), ('servers', '[str]'), ('joined', '[str]'), ('left', '[str]'), ('epoch', 'int'),
    ('new_owner', 'str'), ('max_blob_bytes', 'int'), ('move_id', 'uuid'), ('hub', 'str'), ('record', 'bytes'),
    ('record_sig', 'bytes'), ('commit', 'bytes'), ('commit_sig', 'bytes'),
    # sealed chats
    ('key_epoch', 'int'), ('device_id', 'uuid'),
    # ephemeral
    ('parent_id', 'uuid'), ('presence', 'str'), ('in_call', 'bool'), ('dnd', 'bool'), ('status', 'value'),
    ('availability', 'value'), ('accounts', '[#Presence]'),
])

UNIONS['Event'] = {'own': [('at', 'time'), ('json', 'bytes')], 'first': 3, 'members': OD(),
                   'doc': 'The plaintext of an envelope: one inner event (section 11). The body that is set is its type. '
                          'One the schema cannot hold exactly travels whole as JSON text in `json`, with no body.'}


def E(kinds, fields, doc=''):
    member('Event', 'event', 'Event', kinds, fields, doc)


E('message.created message.edited', 'message blobs')
E('reaction.added reaction.removed', 'message_id account_id emoji blobs')
E('typing.started', 'account_id parent_id')
E('presence.changed', 'account_id presence in_call dnd status availability')
E('presence.snapshot', 'accounts')
E('message.deleted', 'message_id deleted_by_id deleted_at')
E('message.removed', 'message_id removed_by_id removed_at removed_by blobs')
E('message.purged', 'message_id erase')
E('messages.expired', 'through_at scope')
E('preview.added', 'message_id cards blobs')
E('preview.removed', 'message_id url')
E('task.changed', 'message_id item_id checked account_id')
E('form.answered', 'message_id form_id account_id answer_box')
E('form.answer_withdrawn', 'message_id form_id account_id')
E('form.answer_refused', 'message_id form_id account_id answer_event_id reason')
E('form.option_added', 'message_id form_id field_id option added_by_id')
E('form.tallied', 'message_id form_id answer_count tally voters')
E('form.closed', 'message_id form_id closed_by_id closed_at answer_count tally voters')
E('member.joined', 'account blobs joined_at')
E('member.left', 'account_id reason')
E('member.invited', 'account_id by_account_id server')
E('account.updated', 'account blobs')
E('conversation.updated', 'name topic')
E('call.started', 'call_id started_by_id host message_id call')
E('call.participant_joined call.participant_left', 'call_id account_id participant reason')
E('call.ended', 'call_id ended_at reason duration_seconds')
E('stream.servers_changed', 'version servers joined left epoch')
E('stream.owner_changed', 'new_owner version')
E('stream.limits_changed', 'max_blob_bytes')
E('stream.closed', '')
E('stream.moved', 'move_id hub record record_sig commit commit_sig')
E('sealed.epoch_started', 'key_epoch account_id device_id record')
E('member.requested', 'account blobs',
  'Someone asks the moderators and managers of a private channel a hub owns to admit them; their home instance sends it.')
E('member.admitted', 'account_id server by_account_id by_account blobs',
  'A moderator or manager admits someone to a private channel a hub owns; their home instance sends it.')
E('member.request_declined', 'account_id server by_account_id by_account blobs',
  'A moderator or manager declines someone\'s request to join a private channel a hub owns.')
E('member.removed', 'account_id server by_account_id by_account blobs',
  'A manager or moderator removes someone from a channel a hub owns, on every instance.')


# ------------------------------------------------------------------------------------------- the build

KINDS = ['str', 'int', 'double', 'bool', 'uuid', 'time', 'ms', 'bytes', 'msg', 'client', 'value']
PROTO_TYPE = {'str': 'string', 'int': 'int64', 'double': 'double', 'bool': 'bool', 'uuid': 'bytes', 'time': 'int64',
              'ms': 'int64', 'bytes': 'bytes', 'value': '.churr.v1.Value'}
NOTE = {'uuid': ' // identifier', 'time': ' // time', 'ms': ' // time'}


def parse(t):
    """(card, kind, ref) for a type in the notation above."""
    card = 'one'
    for o, c, name in (('[', ']', 'list'), ('{', '}', 'map'), ('<', '>', 'umap')):
        if t.startswith(o) and t.endswith(c):
            card, t = name, t[1:-1]
            break
    if t.startswith('#'):
        assert t[1:] in MSGS, t
        return card, 'msg', t[1:]
    if t.startswith('client:'):
        return card, 'client', t[7:]
    assert t in PROTO_TYPE, t
    return card, t, None


def ident(s):
    return re.sub(r'[^A-Za-z0-9]+', '_', s).strip('_').lower()


def number(lock):
    """Give every field and union member its number, old ones from the lock. Returns
    {message: {json: num}}, {union: {kind: num}}."""
    lm, lb, lu = (lock.setdefault(k, {}) for k in ('messages', 'fields', 'unions'))
    for bag, fields in BAGS.items():
        have = lb.setdefault(bag, {})
        for f in fields:
            if f not in have:
                have[f] = max(list(have.values()) + [0]) + 1
    nums = {}
    for name, m in MSGS.items():
        if m['of']:
            nums[name] = {f: lb[m['of']][f] for f in m['fields']}
            continue
        have = lm.setdefault(name, {})
        first = 2 if m['base'] else 1
        for f in m['fields']:
            if f not in have:
                have[f] = max(list(have.values()) + [first - 1]) + 1
        nums[name] = {f: have[f] for f in m['fields']}
    unums = {}
    for name, u in UNIONS.items():
        have = lu.setdefault(name, {})
        for kind in u['members']:
            if kind not in have:
                have[kind] = max(list(have.values()) + [u['first'] - 1]) + 1
        unums[name] = {kind: have[kind] for kind in u['members']}
    return nums, unums


def proto_type(kind, ref):
    if kind == 'msg':
        return ref
    if kind == 'client':
        return '.churr.v1.' + ref
    return PROTO_TYPE[kind]


def proto_field(m, json_name, t, num, entries):
    card, kind, ref = parse(t)
    pt = proto_type(kind, ref)
    note = NOTE.get(kind, '')
    name = ident(json_name)
    if card == 'one':
        opt = '' if kind in ('msg', 'client', 'value') else 'optional '
        return '  %s%s %s = %d;%s\n' % (opt, pt, name, num, note)
    if card == 'list':
        return '  repeated %s %s = %d;%s\n' % (pt, name, num, note)
    if card == 'map':
        return '  map<string, %s> %s = %d;%s\n' % (pt, name, num, note)
    entry = camel(json_name) + 'Entry'
    entries[entry] = pt
    return '  repeated %s %s = %d; // a map, by identifier\n' % (entry, name, num)


def reserved(have, used):
    gone = sorted(n for k, n in have.items() if k not in used)
    return '  reserved %s;\n' % ', '.join(str(n) for n in gone) if gone else ''


PROTO_HEADER = '''// The Churr link protocol in Protocol Buffers: every frame servers and hubs send each other, what
// they sign, and what they seal to each other. Protocol version %d.
// Written by tools/openapi-gen/build.py from tools/openapi-gen/link.py. Do not edit by hand: change
// link.py and run it again. The numbers are kept in link.lock.json and never change.
// docs/LINK-PROTOCOL.md is the protocol; what is needed to read this file:
//
//   - `bytes` marked "identifier" is the 16 bytes of a UUID.
//   - `int64` marked "time" is microseconds since the Unix epoch, UTC.
//   - A field that is absent means what its zero would. A reader skips fields it does not know.
//   - A field has the same number in every frame (or direct message, or event) that carries it.
//   - Nothing is signed as one of these messages re-serialised. A signature is either over the exact
//     bytes of a `signed` field, which every carrier passes on untouched, or over a byte layout
//     that LINK-PROTOCOL.md gives (envelopes, direct messages, the handshake).
//   - Messages named churr.v1.* are the client schema's (proto/churr/v1/churr.proto).

syntax = "proto3";

package churr.link.v1;

import "churr/v1/churr.proto";

'''


def proto_text(nums, unums, lock):
    out = [PROTO_HEADER % VERSION]
    entries = OD()
    for name, u in UNIONS.items():
        out.append('// %s\n' % u['doc'])
        out.append('message %s {\n' % name)
        n = 1
        for json_name, t in u['own']:
            out.append(proto_field(None, json_name, t, n, entries))
            n += 1
        out.append(reserved(lock['unions'][name], set(u['members'])))
        out.append('  oneof body {\n')
        for kind, msg in u['members'].items():
            out.append('    %s %s = %d;\n' % (msg, ident(kind), unums[name][kind]))
        out.append('  }\n}\n\n')
    for name, m in MSGS.items():
        if m['doc']:
            out.append('// %s\n' % m['doc'])
        out.append('message %s {\n' % name)
        if m['base']:
            out.append('  .churr.v1.%s %s = 1; // every field of the client\'s message\n' % (m['base'], ident(m['base'])))
        if not m['of'] and not m['wrap']:
            out.append(reserved(lock['messages'][name], set(m['fields'])))
        for json_name, t in sorted(m['fields'].items(), key=lambda kv: nums[name][kv[0]]):
            out.append(proto_field(m, json_name, t, nums[name][json_name], entries))
        out.append('}\n\n')
    for entry, pt in entries.items():
        out.append('message %s {\n  bytes key = 1; // identifier\n  %s value = 2;\n}\n\n' % (entry, pt))
    return ''.join(out).rstrip('\n') + '\n'


def go_text(nums, unums):
    mi = {name: i for i, name in enumerate(MSGS)}
    q = json.dumps
    out = ['// Code generated by tools/openapi-gen/build.py from tools/openapi-gen/link.py. DO NOT EDIT.\n'
           '// Change link.py and run the build again.\n\npackage wire\n\n'
           '// pbMessages is every message of proto/churr/link/v1/link.proto, with the JSON name of each field,\n'
           '// in the order of their numbers.\n'
           'var pbMessages = []pbMessage{\n']
    for name, m in MSGS.items():
        out.append('\t{name: %s, wrap: %s, base: %s, fields: []pbField{\n' % (q(name), 'true' if m['wrap'] else 'false', q(m['base'] or '')))
        for json_name, t in sorted(m['fields'].items(), key=lambda kv: nums[name][kv[0]]):
            card, kind, ref = parse(t)
            out.append('\t\t{%s, %d, pb%s, pb%s, %d, %s},\n' % (
                q(json_name), nums[name][json_name], camel(kind), camel(card), mi[ref] if kind == 'msg' else 0,
                q(ref if kind == 'client' else '')))
        out.append('\t}},\n')
    out.append('}\n')
    for name, u in UNIONS.items():
        out.append('\n// pb%ss is every body of %s: its name, its number and its message.\nvar pb%ss = []pbMember{\n' % (name, name, name))
        for kind, msg in u['members'].items():
            out.append('\t{%s, %d, %d},\n' % (q(kind), unums[name][kind], mi[msg]))
        out.append('}\n')
    out.append('\n// pbBlocks gives each signed block the message its signed bytes hold.\nvar pbBlocks = map[string]int{\n')
    for block, msg in BLOCKS.items():
        out.append('\t%s: %d, // %s\n' % (q(block), mi[msg], msg))
    out.append('}\n')
    return ''.join(out)


def put(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    if not os.path.exists(path) or open(path).read() != text:
        with open(path, 'w') as f:
            f.write(text)


def write(root):
    """Write the three files. Returns a one-line summary."""
    for u in UNIONS.values():
        for json_name, t in u['own']:
            parse(t)
    for m in MSGS.values():
        for t in m['fields'].values():
            parse(t)
    lock_path = os.path.join(root, *PROTO_DIR, 'link.lock.json')
    lock = json.load(open(lock_path)) if os.path.exists(lock_path) else {}
    nums, unums = number(lock)
    put(os.path.join(root, *PROTO_DIR, 'link.proto'), proto_text(nums, unums, lock))
    put(lock_path, json.dumps(lock, indent=1, sort_keys=True) + '\n')
    go = os.path.join(root, 'internal', 'link', 'wire', 'schema_gen.go')
    if os.path.isdir(os.path.dirname(go)):
        put(go, go_text(nums, unums))
        if shutil.which('gofmt'):
            subprocess.run(['gofmt', '-w', go], check=True)
    return ('proto/churr/link/v1/link.proto: %d messages, %d frames, %d direct messages, %d events.'
            % (len(MSGS), len(UNIONS['Frame']['members']), len(UNIONS['Direct']['members']), len(UNIONS['Event']['members'])))


if __name__ == '__main__':
    import sys
    print(write(os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..'))))
    sys.exit(0)
