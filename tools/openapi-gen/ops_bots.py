"""Bots, webhooks, calls and server integrations."""
from lib import area, op


# ----------------------------------------------------------------------------
area('Bots',
     'What bots do beyond the ordinary API: register commands, answer interactions and open dialogs. Also webhook requests.')

op('POST dialogs/{id}/submit', 'submitDialog',
   'Submit a dialog a bot opened',
   who='The person the dialog was shown to.',
   desc=('The bot receives `form.answered` with `dialog` true and a new `interaction_id`, which is returned here '
         'too. Refused with `unavailable` if the bot is not connected.'),
   body={'button_id?': 'str', 'answers': '#Answers'},
   res={'interaction_id': 'uuid'},
   err='not_found unavailable')

op('DELETE dialogs/{id}', 'dismissDialog',
   'Dismiss a dialog a bot opened',
   who='The person the dialog was shown to.',
   desc='The bot is told nothing.',
   err='not_found')

op('POST conversations/{id}/webhook-requests', 'requestWebhook',
   'Ask for a webhook for a channel',
   who='Owner, admin or member who is in the channel. Named channels only: a webhook never exists in an ad-hoc channel or a sealed chat.',
   desc=('The request waits for an admin (or a channel manager, if the admins allow that). A person may have at most '
         'three requests waiting; one more is refused with `conflict`.'),
   body={'name': ('str', 'Shown as the author of its messages.'), 'purpose?': 'str'},
   res={'webhook': '#Webhook'},
   st=201,
   err='forbidden not_found conflict')

op('GET conversations/{id}/webhooks', 'listChannelWebhooks',
   "List a channel's webhooks and requests",
   who='Any member of the channel.',
   desc='Name, who asked, who approved and when each was last used. Nobody can see an address.',
   res={'webhooks': '[#Webhook]'},
   err='not_found')

op('POST webhooks/{id}/approve', 'approveWebhook',
   'Approve a webhook request',
   who='Admins. Managers of the channel too, if the setting allows.',
   desc="Adds the webhook's account to the channel at level `member`.",
   res={'webhook': '#Webhook'},
   err='forbidden not_found conflict')

op('POST webhooks/{id}/decline', 'declineWebhook',
   'Decline a webhook request',
   who='Admins. Managers of the channel too, if the setting allows.',
   res={'webhook': '#Webhook'},
   err='forbidden not_found conflict')

op('DELETE webhooks/{id}', 'revokeWebhook',
   'Withdraw a webhook request or revoke a webhook',
   who='The account that requested it, managers of the channel, and admins.',
   err='forbidden not_found')

op('PUT commands', 'setCommands',
   "Set the bot's whole list of commands",
   who='Bots. Not webhooks.',
   desc=('The list replaces the one the bot had; an empty list removes them all. At most 50. A name is unique on the '
         "server: one that is a built-in command, a community saved reply or another bot's command is refused with "
         '`name_taken`.'),
   body={'commands': {'$raw': {'type': 'array',
                               'maxItems': 50,
                               'items': {'type': 'object',
                                         'required': ['name', 'description'],
                                         'properties': {'name': {'type': 'string'},
                                                        'description': {'type': 'string',
                                                                        'description': '1 to 100 characters.'},
                                                        'hint': {'type': 'string',
                                                                 'description': 'Up to 100 characters. Not '
                                                                                'parsed.'}}}}}},
   res={'commands': '[#Command]'},
   err='forbidden name_taken')

op('POST interactions/{interaction_id}/responses', 'respondToInteraction',
   'Respond to a command, a button press, or an answer to a bot form or dialog',
   who='The bot the interaction was sent to.',
   desc=('An interaction lasts 15 minutes and takes at most five responses; after that the call answers `not_found`. '
         'A `message` response needs the bot to be at level `member` or above and is posted in the thread the '
         'interaction came from unless `message.parent_id` says otherwise. A `private` response reaches only the '
         'asker and is not stored.'),
   body='#InteractionResponse',
   res={'message?': ('#Message', 'For `message`.'), 'dialog?': ('#Dialog', 'For `dialog`.')},
   err='forbidden not_found archived too_large blocked_by_filter')

op('POST webhooks/{id}/address', 'collectWebhookAddress',
   'Collect the address of an approved webhook, once',
   who='The account that requested it.',
   desc=('The token and the signing secret are made at this moment and shown once. Until then the webhook cannot '
         'post. The signing secret is what GitHub is given to sign its deliveries; it is not the token. A second '
         'call is refused with `conflict`: use `resetWebhookAddress`.'),
   res={'url': ('str', '`https://<server>/hooks/churr_hook_…`'), 'signing_secret': 'str'},
   err='forbidden not_found conflict')

op('POST webhooks/{id}/reset', 'resetWebhookAddress',
   "Replace a webhook's address and signing secret",
   who='The account that requested it, a manager of the channel, or an admin.',
   desc='The old address stops working at once. The new address and secret are shown once to whoever reset it.',
   res={'url': 'str', 'signing_secret': 'str'},
   err='forbidden not_found conflict')


# ----------------------------------------------------------------------------
area('Webhook intake',
     'Where outside tools post. Outside `/api/v1`. The secret in the path is the only credential. Bodies are at most 1 MB.')

op('POST /hooks/{token}', 'postWebhook',
   "Post a message through a webhook, in Churr's own shape",
   who='Whoever holds the address.',
   desc=('A webhook can post into its one channel and read nothing. To send files or images, post '
         '`multipart/form-data` with the JSON in a part named `payload` and up to ten parts named `file1` to '
         '`file10`; the whole request is still limited to 1 MB. BOTS.md section 10 has the detail.'),
   body='#WebhookMessage',
   multipart={'payload': ('str', 'The JSON body.'),
              'file1?': ('bin',
                         'Parts `file1` to `file10` hold file bytes. A part the JSON does not name is refused.')},
   res={'message': {'id': 'uuid', 'seq': 'int', 'created_at': 'time'}},
   st=200,
   err='not_found too_large rate_limited archived blocked_by_filter',
   auth='hook')

op('POST /hooks/{token}/slack', 'postSlackWebhook',
   'Post a Slack incoming-webhook payload',
   who='Whoever holds the address.',
   desc=('Slack attachments become cards. Slack markup becomes Churr Markdown. Layout blocks become text. Unknown '
         'fields are ignored. Answers `ok` as plain text, as Slack does. BOTS.md section 12.'),
   body='obj',
   err='not_found too_large rate_limited archived blocked_by_filter',
   auth='hook',
   raw={'200': {'description': 'Posted.', 'content': {'text/plain': {'schema': {'type': 'string', 'const': 'ok'}}}}})

op('POST /hooks/{token}/discord', 'postDiscordWebhook',
   'Post a Discord webhook payload',
   who='Whoever holds the address.',
   desc=('Embeds become cards. Anything with no equivalent is dropped or becomes text. Unknown fields are ignored. '
         'BOTS.md section 13.'),
   body='obj',
   err='not_found too_large rate_limited archived blocked_by_filter',
   auth='hook')

op('POST /hooks/{token}/github', 'postGithubWebhook',
   'Receive an event from a GitHub repository',
   who='Whoever holds the address.',
   desc=("Reads the `X-GitHub-Event` header and checks `X-Hub-Signature-256` against the webhook's own signing "
         'secret, which was shown with the address. The token is never the signing secret. Answers 200 with the '
         'message named when something was posted and 204 when the event posts nothing. BOTS.md section 14.'),
   body='obj',
   res={'message': {'id': 'uuid', 'seq': 'int', 'created_at': 'time'}},
   err='not_found too_large rate_limited archived blocked_by_filter unauthenticated invalid_request',
   auth='hook',
   also={'204': 'Accepted. The event posts nothing.'})


# ----------------------------------------------------------------------------
area('Calls',
     'Joining and leaving a call. Signalling runs over the live connection (docs/EVENTS.md).')

op('GET conversations/{id}/call', 'getCall',
   'Get the call in progress in a conversation',
   who='Any account that can read the conversation: its members, and for a public channel every owner, admin and member.',
   desc='With no call in progress the answer is an empty object, `{}`.',
   res={'call?': '#Call'},
   err='not_found')

op('POST conversations/{id}/call/join', 'joinCall',
   "Ask to join the conversation's call, starting it if there is none",
   who='Members at level `member` or above. Not while timed out.',
   desc=('Checks permission, starts the call if none is in progress (which posts a system message and may ring '
         'people), and answers with a ticket: good for 60 seconds and one use. The client then sends `call.join` '
         'with the ticket on its live connection (CALLS.md section 10). A full call is refused with `conflict`, and '
         '`feature_off` when calls are switched off. For a call hosted on another server the ticket comes from that '
         'server, and the answer is `unavailable` while it cannot be reached.'),
   res={'call': '#Call', 'ticket': 'str'},
   err='forbidden timed_out archived feature_off not_found conflict unavailable')

op('POST conversations/{id}/call/leave', 'leaveCall',
   "Leave the conversation's call",
   who='An account that is in the call.',
   desc='The same as the `call.leave` frame, for a client whose live connection has gone.',
   err='not_found')


# ----------------------------------------------------------------------------
area('Server integrations',
     'For programs that work on the server as a whole. Authenticated by an integration token (`churr_int_`), which works nowhere else.')

op('GET integration/accounts', 'findAccountForIntegration',
   'Find an account by username or company sign-in identifier',
   who='An integration that may set availability.',
   desc=('Give `username`, or `provider_id` and `subject`. Returns the identifier and username only. Only local '
         'people can be found: not bots, removed accounts or accounts from other servers.'),
   q=['username:str:A local username.', 'provider_id:uuid:A company sign-in provider.',
      "subject:str:The person's identifier at that provider."],
   res={'account': {'id': 'uuid', 'username': 'str'}},
   err='forbidden not_found',
   auth='integration')

op('PUT integration/accounts/{id}/availability', 'setAvailabilityForAccount',
   'Set or clear in a meeting or out of office for an account',
   who='An integration that may set availability. An integration posts nothing and is in no conversation.',
   desc=('`availability` of null clears. Clearing only clears a value this same integration set; a value the person '
         'or another integration set is left alone and the answer is `conflict`. `until` is required with `meeting` '
         'and at most 24 hours ahead. Written to the audit log.'),
   body={'availability': {'$raw': {'type': ['string', 'null'], 'enum': ['meeting', 'out_of_office', None]}},
         'until?': 'time'},
   res={'id': 'uuid', 'availability?': '#Availability'},
   err='forbidden not_found conflict',
   auth='integration')
