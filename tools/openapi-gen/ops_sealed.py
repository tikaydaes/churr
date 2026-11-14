"""Sealed chats and the key backup."""
from lib import area, op


# ----------------------------------------------------------------------------
area('Sealed chats',
     'What the server stores and hands on for sealed chats and their key backup. It can read none of it. SEALED.md is the authority; the names here are its names.')

op('GET accounts/{id}/device-lists', 'listDeviceListVersions',
   "Fetch versions of an account's signed device list",
   who='The account itself, an account that is in a sealed chat with it, and an account that may start a sealed chat with it. To anyone else the account does not exist (`not_found`).',
   desc=('Every version after `after`, in order, so a client can check each step from its pin; at most 500 in one '
         'answer, and a client further behind asks again from the last version it got. With no `after`, the newest '
         'version only. For an account on another server whose list this server does not hold yet, its home server '
         'is asked first, and the answer may be `unavailable` while the link is down.'),
   q=['after:int:Only versions above this one.'],
   res={'versions': '[#DeviceListVersion]'},
   err='not_found feature_off unavailable')

op('POST me/device-lists', 'publishDeviceList',
   "Publish the next version of the caller's device list",
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   desc=('Accepted only when `version` is exactly one above the stored newest (`conflict` otherwise), and only when '
         'every device in it is a live device of the account whose stored `sign_key` and `box_key` match. Announced as '
         '`device_list.changed`.'),
   body='#DeviceListInput',
   res={'version': '#DeviceListVersion'},
   st=201,
   err='conflict forbidden feature_off')

op('POST conversations/{id}/sealed/epochs', 'postEpochRecord',
   'Post the next epoch record of a sealed chat',
   who='Either person in the sealed chat, from a device that is in its account\'s device list.',
   desc=('An epoch record starts a new key epoch: one device makes a fresh epoch secret, seals it separately to '
         "every device on both accounts' current device lists, and signs the record. The server cannot open it. It "
         'accepts exactly one record per `key_epoch`, and only the next number; it gives the record a `seq`, keeps '
         'it as long as the chat, makes that key epoch the current one, and delivers `sealed.epoch_started` to both '
         "accounts' devices. A second record for the same `key_epoch` is refused with `conflict`, and the answer "
         'carries the winning record beside the error so the device can use it. A device makes a new epoch when '
         "either account's device list changes, when it has just been unlocked and must include itself, and when "
         'the current epoch is more than seven days old at the time someone sends.'),
   body={'key_epoch': ('int', "One above the chat's current key epoch."), 'record': 'b64'},
   res={'epoch': '#EpochRecord'},
   st=201,
   err='forbidden not_found feature_off',
   raw409=True)

op('GET conversations/{id}/sealed/epochs', 'listEpochRecords',
   "List a sealed chat's epoch records from a given key epoch",
   who='Either person in the sealed chat, from a web or app session.',
   desc=('In `key_epoch` order. Records are kept for the life of the chat, so a device that is behind by any amount '
         'can read every one sealed to it. A device that was not yet listed when an epoch was made reads that '
         "epoch's secret from its account's backup instead."),
   q=['from:int:The first `key_epoch` to return. Default 1.', 'limit:int:Default 50, at most 200.'],
   res={'epochs': '[#EpochRecord]', 'has_more': 'bool'},
   err='not_found')

op('GET me/backup', 'getBackup',
   'Fetch the locked copies and the account record',
   who='Any complete web or app session of the account. Not personal tokens.',
   desc='Answers `not_found` when the account has no backup yet.',
   res={'backup': '#Backup'},
   err='not_found forbidden')

op('POST me/backup', 'createBackup',
   "Create the account's backup",
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   desc=('Made by the first device of an account that enters a sealed chat: the account record, a locked copy for '
         'each way of unlocking, and version 1 of the device list, with `key_version` 1. If two devices do this at '
         'once the server keeps the first; the other gets `already_exists` and unlocks in the ordinary way.'),
   body={'account_record': '#AccountRecord', 'locked_copies': '[#LockedCopyInput]', 'device_list': '#DeviceListInput'},
   res={'backup': '#Backup'},
   st=201,
   err='already_exists forbidden feature_off')

op('POST me/backup/copies', 'addLockedCopy',
   'Add a locked copy and update the account record',
   who="Any signed-in person, from the session a device is attached to. Not personal tokens. A `device` copy may also come from any complete web session of the account, which is how the server's unlock page makes one for an app after a passkey touch.",
   desc=('For a passkey the person just approved, for a device being unlocked, or a new phrase copy. A copy for the '
         'same way replaces the earlier one. `account_record` is sent when the ways changed, with the next '
         '`revision`.'),
   body={'locked_copy': '#LockedCopyInput', 'account_record?': '#AccountRecord'},
   res={'backup': '#Backup'},
   st=201,
   err='forbidden not_found conflict')

op('PUT me/backup/account-record', 'putAccountRecord',
   'Write the account record',
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   desc=('Accepted only when `revision` is exactly one above the stored one and `key_version` is the current one '
         '(`conflict` otherwise). Used when the ways of unlocking change without a new locked copy, as when one is '
         'taken away.'),
   body='#AccountRecord',
   res={'backup': '#Backup'},
   err='forbidden not_found conflict')

op('DELETE me/backup/copies/{id}', 'deleteLockedCopy',
   'Delete a locked copy',
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   err='forbidden not_found')

op('GET me/backup/records', 'listBackupRecords',
   "List the caller's backup records",
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   q=['conversation_id:uuid:Only this sealed chat.'],
   res={'records': '[#BackupRecord]'},
   err='forbidden',
   page='cursor')

op('PUT me/backup/records/{conversation_id}/{key_epoch}', 'putBackupRecord',
   'Write a backup record',
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   desc=('One record per sealed chat per key epoch. The server keeps the first and ignores the rest: a later write '
         'answers 200 with the record already stored. A `key_version` that is not the backup\'s current one is '
         'refused with `invalid_value`; a chat or key epoch the server does not hold, or an account with no backup, '
         'answers `not_found`.'),
   body={'key_version': 'int', 'record': 'b64'},
   res={'record': '#BackupRecord'},
   st=201,
   err='forbidden not_found',
   also={'200': 'A record was already stored; this is it.'})

op('POST me/backup/replace', 'replaceBackup',
   'Replace the backup key and account key in one step',
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   desc=('After a passkey that had a locked copy was removed, a device was revoked, the phrase was replaced, or '
         'because the person asked. The server swaps the account record, locked copies, records and device list '
         'together, deletes the old rows, clears "replacement due", and refuses a second replacement of the same '
         'version with `conflict`. The body may be up to 32 MB and carry up to 100,000 records (`too_large` '
         'beyond either).'),
   body={'replaces_key_version': 'int',
         'account_record': '#AccountRecord',
         'locked_copies': '[#LockedCopyInput]',
         'records': '[#BackupRecord]',
         'device_list': '#DeviceListInput'},
   res={'backup': '#Backup'},
   err='forbidden conflict too_large')

op('POST me/backup/reset', 'resetBackup',
   'Start again when no way of unlocking is left',
   who='Any signed-in person, from a web or app session with a registered device. Needs a fresh check.',
   desc=('The device makes a new backup key, account key and phrase with the next `key_version`, and a device list '
         "marked `reset` holding only itself. The server deletes the account's old locked copies, account record and "
         'backup records. Earlier sealed messages cannot be read again on any device of the account, and the other '
         'person in each sealed chat is shown a reset notice. An account with no backup rows left (they were lost) '
         'gets a new backup made from the request.'),
   body={'account_record': '#AccountRecord', 'locked_copies': '[#LockedCopyInput]', 'device_list': '#DeviceListInput'},
   res={'backup': '#Backup'},
   err='forbidden conflict fresh_check_required')

op('POST me/unlock-requests', 'createUnlockRequest',
   'Ask an unlocked device of the same account for the backup key',
   who='Any signed-in person, from a web or app session with a registered, locked device.',
   desc=("Stored for ten minutes and announced to the account's other connections as `unlock_request.created`. After "
         'QR pairing the request carries an HMAC under a key from the QR code, which the page checks in place of a '
         'typed code.'),
   body={'request': ('b64',
                     'The 170-byte request record (the device identifier, both public keys and the proof), optionally '
                     'followed by the 32-byte pairing MAC after QR pairing.')},
   res={'unlock_request': '#UnlockRequest'},
   st=201,
   err='forbidden feature_off')

op('GET me/unlock-requests', 'listUnlockRequests',
   "List the account's waiting unlock requests",
   who='Any signed-in person, from a web or app session. Not personal tokens, and not bots.',
   res={'unlock_requests': '[#UnlockRequest]'},
   err='forbidden')

op('GET me/unlock-requests/{id}', 'getUnlockRequest',
   'Read one unlock request, with its answer once given',
   who="The account the request belongs to. The answer is given only to the asking device's session.",
   res={'unlock_request': '#UnlockRequest'},
   err='not_found')

op('PUT me/unlock-requests/{id}/answer', 'answerUnlockRequest',
   'Answer an unlock request from an unlocked device',
   who='Any signed-in person, from the session a device is attached to. Not personal tokens.',
   desc=('The unlocked device first checks the 100-bit code the new device shows against the request it received '
         'through the server. It then stores a locked copy of kind `device` for the new device, which is the answer. '
         'Announced to the asking device as `unlock_request.answered`.'),
   body={'locked_copy': '#LockedCopyInput'},
   res={'unlock_request': '#UnlockRequest'},
   err='forbidden not_found conflict')
