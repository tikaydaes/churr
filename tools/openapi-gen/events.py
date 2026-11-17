"""Every frame of the live connection, in the notation of lib.py.

docs/EVENTS.md and docs/CALLS.md section 10 describe these in words; this file says the same thing in
a form the build can read, so the Protocol Buffers schema (proto.py) comes from the same source as
docs/openapi.yaml. The objects frames carry (`#Message`, `#Conversation` and the rest) are the ones
in schemas.py.

    F(type, {field: type, ...})

A frame's `type`, and the `at`, `conversation_id` and `seq` every event may have, are not listed: the
envelope carries them. The ORDER of this file matters once, when a frame is first given its number in
the envelope; after that the number is kept in proto/churr/v1/churr.lock.json. The first six are the
frames sent most often, so they get the numbers that take one byte.

Objects only frames use are declared with ES(), which keeps them out of docs/openapi.yaml.
"""
import collections

from lib import obj, SCHEMAS

FRAMES = collections.OrderedDict()          # type -> JSON Schema of its other fields
EVENT_SCHEMAS = collections.OrderedDict()   # name -> JSON Schema


def F(types, props):
    for t in types.split():
        assert t not in FRAMES, t
        for k in props:
            assert k.rstrip('?') not in ('type', 'at', 'conversation_id', 'seq'), (t, k)
        FRAMES[t] = obj(props)


def ES(name, props):
    assert name not in SCHEMAS and name not in EVENT_SCHEMAS, name
    EVENT_SCHEMAS[name] = obj(props)


def const(value):
    """A field that always holds the same text. It is not put on the wire."""
    return {'$raw': {'type': 'string', 'x-const': value}}


TOTAL = {'badge': 'int', 'mentions': 'int', 'unread_conversations': 'int', 'unread_threads': 'int'}
LEVEL_SOURCE = 'enum:microphone|camera|screen'
PC = 'enum:send|recv'
VIDEO_STATE = 'enum:forwarding|paused|muted|unavailable'

ES('PresenceChange', {'type': const('presence.changed'), 'at': 'time', 'account_id': 'uuid',
                      'presence': 'enum:online|away|offline', 'in_call?': 'bool'})
ES('ErrorDetail', {'code': {'$raw': SCHEMAS['Error']['properties']['error']['properties']['code']},
                   'message': 'str', 'field?': 'str', 'retry_after_seconds?': 'int'})
ES('CallMediaLine', {'mid': 'str', 'source?': LEVEL_SOURCE, 'track_id?': 'str', 'account_id?': 'uuid',
                     'device_id?': 'uuid', 'width?': 'int', 'height?': 'int'})

# ---- the six sent most often
F('message.created', {'message': '#Message', 'notify?': 'enum:none|badge|notify', 'client_id?': 'uuid'})
F('presence.changed', {'account_id': 'uuid', 'presence': 'enum:online|away|offline', 'in_call?': 'bool'})
F('typing.started', {'account_id': 'uuid', 'parent_id?': 'uuid'})
F('reaction.added', {'message_id': 'uuid', 'account_id': 'uuid', 'reaction': '#Reaction'})
F('read_position.moved', {'read_seq': 'int', 'unread_count': 'int', 'mention_count': 'int',
                          'badge_count': 'int', 'total': TOTAL})
F('presence.batch', {'changes': '[#PresenceChange]'})

# ---- control frames the server sends (EVENTS.md sections 2 to 5)
F('hello', {'connection_id': 'str', 'account_id': 'uuid', 'session_id': 'uuid', 'heartbeat_seconds': 'int',
            'resume_within_seconds': 'int', 'reconnect_spread_seconds?': 'int', 'presence_subscribe_max?': 'int',
            'conversations_open_max?': 'int', 'server_time': 'time'})
F('ready', {'new_conversation_ids': '[uuid]'})
F('reload', {'reason': 'enum:too_far_behind|no_longer_member|unknown', 'last_seq?': 'int'})
F('pong', {'server_time': 'time', 'reply_to?': 'str'})
F('ack', {'reply_to': 'str'})
F('error', {'reply_to?': 'str', 'error': '#ErrorDetail'})

# ---- frames a client sends (section 7). Any of them may carry `id`.
POSITIONS = {'$raw': {'type': 'object', 'propertyNames': {'format': 'uuid'},
                      'additionalProperties': {'type': 'integer', 'format': 'int64'}}}
F('resume', {'id?': 'str', 'positions': POSITIONS, 'open?': '[uuid]'})
F('ping activity', {'id?': 'str'})
F('typing', {'id?': 'str', 'parent_id?': 'uuid'})
F('presence.subscribe', {'id?': 'str', 'account_ids': '[uuid]'})
# Lazy delivery of large channels (section 4.1, ruling R66): what a connection has open, what the
# others are told, and the online counts.
F('conversations.open', {'id?': 'str', 'positions': POSITIONS})
F('conversation.changed', {'last_seq': 'int', 'messages?': 'int', 'mentioned?': 'bool', 'unread_count?': 'int',
                           'mention_count?': 'int', 'badge_count?': 'int', 'total?': TOTAL, 'event?': '#Event'})
F('presence.counts', {'online_count?': 'int',
                      'conversations': {'$raw': {'type': 'object', 'propertyNames': {'format': 'uuid'},
                                                 'additionalProperties': {'type': 'integer', 'format': 'int64'}}}})

# ---- conversation events (section 8)
F('reaction.removed', {'message_id': 'uuid', 'account_id': 'uuid', 'reaction': '#Reaction'})
F('message.edited message.deleted message.history_restored message.placed preview.added preview.removed',
  {'message': '#Message'})
F('message.purged', {'message': '#Message', 'erased?': 'bool'})
F('message.sent', {'message_id': 'uuid'})
F('task.changed', {'message_id': 'uuid', 'task': '#Task'})
# form.answered is two events with one name: the conversation's (message_id, form, account_id) and the
# one a bot receives for its own form or dialog (section 10), which has the rest.
F('form.answered', {'message_id?': 'uuid', 'form?': '#Form', 'account_id?': 'uuid',
                    'interaction_id?': 'uuid', 'form_id?': 'uuid', 'button_id?': 'str',
                    'answers?': '#Answers', 'dialog?': 'bool'})
F('form.answer_withdrawn', {'message_id': 'uuid', 'form': '#Form', 'account_id?': 'uuid',
                            'refused?': 'enum:closed|full|invalid'})
F('form.option_added', {'message_id': 'uuid', 'form': '#Form'})
F('form.closed', {'message_id': 'uuid', 'form': '#Form', 'closed_by_id?': 'uuid'})
F('member.joined', {'member': '#Member', 'by_id?': 'uuid', 'conversation?': '#Conversation'})
F('member.left', {'account_id': 'uuid', 'by_id?': 'uuid',
                  'reason': 'enum:left|removed|banned|group|account_removed'})
F('member.level_changed', {'member': '#Member', 'by_id?': 'uuid'})
F('channel_group.added channel_group.changed channel_group.removed',
  {'group': '#ChannelGroup', 'by_id?': 'uuid', 'conversation?': '#Conversation'})
F('conversation.updated conversation.archived conversation.unarchived',
  {'conversation': '#Conversation', 'by_id?': 'uuid'})
F('pin.added', {'pin': '#Pin'})
F('pin.removed', {'message_id': 'uuid', 'by_id?': 'uuid'})
F('thread.moved', {'parent': '#Message', 'to_conversation_id': 'uuid', 'pointer': '#Message', 'by_id?': 'uuid'})
F('file.changed', {'message_id': 'uuid', 'file': '#File'})
F('stream.channel_changed', {'stream_id': 'uuid', 'channel': '#HubChannel'})
F('stream.move_changed', {'stream_id': 'uuid', 'move': '#StreamMove'})
F('sealed.epoch_started', {'epoch': '#EpochRecord', 'replaced?': 'bool'})
F('call.started', {'call': '#Call'})
F('call.ended', {'call_id': 'uuid', 'reason': 'enum:empty|alone|too_long|closed|server', 'duration_seconds': 'int'})
F('call.participant_joined', {'call_id': 'uuid', 'participant': '#CallParticipant'})
F('call.participant_left', {'call_id': 'uuid', 'account_id': 'uuid',
                            'reason': 'enum:left|timeout|joined_elsewhere|removed|signed_out'})

# ---- account events (section 9)
F('status.changed', {'account_id': 'uuid', 'status?': '#Status'})
F('availability.changed', {'account_id': 'uuid', 'availability?': '#Availability'})
F('dnd.changed', {'account_id': 'uuid', 'dnd': 'bool', 'dnd_state?': '#Dnd'})
F('account.updated account.removed', {'account': '#Account'})
F('me.updated', {'account': '#Me'})
F('group.created group.updated group.deleted', {'group': '#Group'})
F('group.member_added group.member_removed', {'group_id': 'uuid', 'account_id': 'uuid', 'group?': '#Group'})
F('block.added block.removed', {'account_id': 'uuid'})
F('chat_request.received', {'request': '#ChatRequest', 'notify': 'bool'})
F('chat_request.removed', {'id': 'uuid'})
F('channel_offer.added', {'offer': '#ChannelOffer'})
F('channel_offer.removed', {})
F('membership.updated', {'membership': '#Membership'})
F('thread_state.changed', {'parent_id': 'uuid', 'thread_state?': '#ThreadState'})
F('bookmark.added', {'message_id': 'uuid', 'created_at?': 'time'})
F('bookmark.removed', {'message_id': 'uuid'})
F('badge.changed', {'unread_count': 'int', 'mention_count': 'int', 'badge_count': 'int', 'total': TOTAL})
F('scheduled.created scheduled.updated scheduled.cancelled', {'scheduled': '#ScheduledItem'})
F('scheduled.fired', {'scheduled': '#ScheduledItem', 'message_id?': 'uuid', 'error?': '#ErrorDetail'})
F('timeout.started timeout.lifted', {'timeout': '#Timeout'})
F('channel_ban.created channel_ban.lifted', {'ban': '#ChannelBan', 'conversation_name?': 'str'})
F('knock.created knock.answered', {'knock': '#Knock', 'conversation_name?': 'str'})
F('approval.created approval.decided', {'approval': '#Approval'})
F('approvals.changed', {'may_approve': 'bool'})
F('report.created report.handled', {'report': '#Report'})
F('purge_request.created purge_request.decided', {'purge_request': '#PurgeRequest'})
F('webhook.requested webhook.decided', {'webhook': '#Webhook'})
F('server.updated', {'server': '#Server'})
F('server.broadcast', {'broadcast?': '#Broadcast'})
F('sections.changed', {'sections': '[#Section]'})
F('sidebar.changed', {'sidebar': '#Sidebar'})
F('reference_patterns.changed', {'patterns': '[#ReferencePattern]'})
F('emoji.added emoji.updated emoji.removed', {'emoji': '#Emoji'})
F('theme.published', {'theme': '#Theme'})
F('theme.unpublished', {'theme_id': 'uuid'})
F('interaction.responded', {
    'interaction_id': 'uuid', 'parent_id?': 'uuid', 'bot_id': 'uuid',
    'response': {'type': 'enum:message|private|dialog|none', 'message_id?': 'uuid', 'text?': 'str',
                 'entities?': '[#Entity]', 'cards?': '[#Card]', 'dialog?': '#Dialog'}})
F('session.created session.ended', {'session': '#Session'})
F('device.retired', {'device': '#Device'})

# ---- sealed chats (section 12)
F('device_list.changed', {'account_id': 'uuid', 'version': 'int'})
F('unlock_request.created unlock_request.answered', {'unlock_request': '#UnlockRequest'})
F('backup.changed', {'key_version': 'int', 'replacement_due': 'bool'})

# ---- only bots receive (section 10)
F('command.invoked', {'interaction_id': 'uuid', 'parent_id?': 'uuid', 'account_id': 'uuid',
                      'command': 'str', 'text': 'str', 'entities': '[#Entity]'})

# ---- call signalling (section 11, CALLS.md section 10). call.offer and call.answer travel both
# ways with different fields, so each holds the fields of both directions.
F('call.join', {'id?': 'str', 'call_id': 'uuid', 'ticket': 'str',
                'client?': {'simulcast?': 'bool', 'video_codecs?': '[str]', 'frame_encryption?': 'bool'}})
F('call.offer', {'id?': 'str', 'reply_to?': 'str', 'pc': PC, 'offer_id?': 'int', 'sdp': 'str',
                 'ice_restart?': 'bool', 'tracks?': '[#CallMediaLine]'})
F('call.answer', {'id?': 'str', 'reply_to?': 'str', 'pc': PC, 'offer_id?': 'int', 'sdp': 'str',
                  'tracks?': '[#CallMediaLine]'})
F('call.candidate', {'id?': 'str', 'pc': PC, 'candidate?': 'str', 'sdp_mid?': 'str', 'sdp_mline_index?': 'int'})
F('call.subscribe', {'id?': 'str', 'video':
                     {'$raw': {'type': 'array', 'items': obj({'track_id': 'str', 'width?': 'int', 'height?': 'int'})}}})
F('call.mute', {'id?': 'str', 'source': LEVEL_SOURCE, 'muted': 'bool'})
F('call.restart', {'id?': 'str', 'pc': PC})
F('call.resume', {'id?': 'str', 'call_id': 'uuid', 'resume_token': 'str'})
F('call.decline call.leave', {'id?': 'str', 'call_id': 'uuid'})
F('call.ok', {'reply_to?': 'str', 'call?': '#Call', 'resume_token?': 'str',
              'limits?': {'max_participants': 'int', 'max_cameras': 'int', 'max_subscriptions': 'int',
                          'audio_kbps': 'int', 'screen_kbps': 'int', 'layers': 'map:int'},
              'video?': {'$raw': {'type': 'array', 'items': obj({'track_id': 'str', 'state': VIDEO_STATE,
                                                                  'layer?': 'str'})}}})
F('call.error', {'reply_to?': 'str', 'code': 'str', 'message': 'str', 'account_id?': 'uuid',
                 'retry_after_seconds?': 'int'})
F('call.track_published', {'call_id': 'uuid', 'account_id': 'uuid', 'device_id?': 'uuid', 'track': '#CallTrack'})
F('call.track_unpublished', {'call_id': 'uuid', 'account_id': 'uuid', 'track_id': 'str'})
F('call.mute_changed', {'call_id': 'uuid', 'account_id': 'uuid', 'track_id': 'str', 'source': LEVEL_SOURCE,
                        'muted': 'bool'})
F('call.speakers_changed', {'call_id': 'uuid', 'account_ids': '[uuid]'})
F('call.layer_changed', {'call_id': 'uuid', 'track_id': 'str', 'state': VIDEO_STATE, 'layer?': 'str',
                         'reason?': 'enum:size|bandwidth|publisher'})
F('call.demand_changed', {'call_id': 'uuid', 'track_id': 'str', 'layers': '[str]'})
F('call.declined', {'call_id': 'uuid', 'account_id': 'uuid'})
F('call.ring_started', {'call_id': 'uuid', 'from_account_id': 'uuid', 'until': 'time'})
F('call.ring_stopped', {'call_id': 'uuid', 'reason': 'enum:joined_elsewhere|declined_elsewhere|ended|timeout'})
