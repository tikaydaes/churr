/// Every operation of the server's API that the app calls, by the
/// `operationId` of docs/openapi.yaml.
///
/// This list is the only place a path or method is written. A test
/// (`test/core/openapi_contract_test.dart`) checks each entry against
/// docs/openapi.yaml, and checks that no other file under `lib/` spells
/// an API path, so the client and the contract cannot drift apart. To
/// call a new operation, add it here first.
library;

/// One operation: its identifier in the contract, its method and its
/// path template with `{name}` parameters.
class Op {
  const Op(this.id, this.method, this.path);
  final String id;
  final String method;
  final String path;

  /// The path with its parameters filled in, each one percent-encoded.
  String fill(Map<String, String> params) {
    var out = path;
    params.forEach((k, v) {
      out = out.replaceAll('{$k}', Uri.encodeComponent(v));
    });
    assert(!out.contains('{'), 'unfilled parameter in $path');
    return out;
  }
}

/// The operations, grouped as docs/API.md section 11 groups them.
abstract final class Ops {
  // Server and OAuth.
  static const getServer = Op('getServer', 'GET', '/api/v1/server');
  static const getOAuthMetadata = Op(
    'getOAuthMetadata',
    'GET',
    '/.well-known/oauth-authorization-server',
  );
  static const authorize = Op('authorize', 'GET', '/oauth/authorize');
  static const exchangeToken = Op('exchangeToken', 'POST', '/oauth/token');
  static const revokeOAuthToken = Op(
    'revokeOAuthToken',
    'POST',
    '/oauth/revoke',
  );

  // Me.
  static const getMe = Op('getMe', 'GET', '/api/v1/me');
  static const getSidebar = Op('getSidebar', 'GET', '/api/v1/me/sidebar');
  static const changeSidebar = Op('changeSidebar', 'POST', '/api/v1/me/sidebar');
  static const getStatusSummary = Op(
    'getStatusSummary',
    'GET',
    '/api/v1/status',
  );

  // The lock (docs/AUTH.md section 10.6).
  static const lockSession = Op('lockSession', 'POST', '/api/v1/session/lock');
  static const beginSessionUnlock = Op(
    'beginSessionUnlock',
    'POST',
    '/api/v1/session/unlock/begin',
  );

  // Accounts and the small lists.
  static const listAccounts = Op('listAccounts', 'GET', '/api/v1/accounts');
  static const listGroups = Op('listGroups', 'GET', '/api/v1/groups');
  static const joinGroup = Op(
    'joinGroup',
    'POST',
    '/api/v1/groups/{id}/members/me',
  );
  static const leaveGroup = Op(
    'leaveGroup',
    'DELETE',
    '/api/v1/groups/{id}/members/me',
  );
  static const listSections = Op('listSections', 'GET', '/api/v1/sections');
  static const listEmoji = Op('listEmoji', 'GET', '/api/v1/emoji');
  static const createEmoji = Op('createEmoji', 'POST', '/api/v1/emoji');

  // The names of Unicode code points.
  static const lookupCodePoints = Op(
    'lookupCodePoints',
    'GET',
    '/api/v1/unicode',
  );

  // Reference patterns (ruling R73).
  static const listReferencePatterns = Op(
    'listReferencePatterns',
    'GET',
    '/api/v1/reference-patterns',
  );
  static const createReferencePattern = Op(
    'createReferencePattern',
    'POST',
    '/api/v1/reference-patterns',
  );
  static const updateReferencePattern = Op(
    'updateReferencePattern',
    'PATCH',
    '/api/v1/reference-patterns/{id}',
  );
  static const deleteReferencePattern = Op(
    'deleteReferencePattern',
    'DELETE',
    '/api/v1/reference-patterns/{id}',
  );

  // Conversations.
  static const listConversations = Op(
    'listConversations',
    'GET',
    '/api/v1/conversations',
  );
  static const getConversation = Op(
    'getConversation',
    'GET',
    '/api/v1/conversations/{id}',
  );
  static const setReadPosition = Op(
    'setReadPosition',
    'PUT',
    '/api/v1/conversations/{id}/read',
  );
  static const updateMembership = Op(
    'updateMembership',
    'PATCH',
    '/api/v1/conversations/{id}/membership',
  );

  // Messages.
  static const listMessages = Op(
    'listMessages',
    'GET',
    '/api/v1/conversations/{id}/messages',
  );
  static const checkBroadMentions = Op(
    'checkBroadMentions',
    'POST',
    '/api/v1/conversations/{id}/broad-mentions/check',
  );
  static const postMessage = Op(
    'postMessage',
    'POST',
    '/api/v1/conversations/{id}/messages',
  );
  static const addReaction = Op(
    'addReaction',
    'PUT',
    '/api/v1/messages/{id}/reactions/{emoji}',
  );
  static const removeReaction = Op(
    'removeReaction',
    'DELETE',
    '/api/v1/messages/{id}/reactions/{emoji}',
  );
  static const setChecklistItem = Op(
    'setChecklistItem',
    'PUT',
    '/api/v1/messages/{id}/checklist/{item}',
  );

  // Events.
  static const openEvents = Op('openEvents', 'GET', '/api/v1/events');
  static const listEvents = Op(
    'listEvents',
    'GET',
    '/api/v1/conversations/{id}/events',
  );

  // Files. A file's bytes are normally fetched from the `url` and
  // `thumb_url` paths the server puts in its objects; these two are here
  // so the contract test covers the shapes of those paths, and because a
  // card names its image by identifier only.
  static const downloadFile = Op(
    'downloadFile',
    'GET',
    '/api/v1/files/{id}/{name}',
  );
  static const downloadThumbnail = Op(
    'downloadThumbnail',
    'GET',
    '/api/v1/files/{id}/thumb',
  );

  /// Everything above, for the contract test.
  static const all = [
    getServer,
    getOAuthMetadata,
    authorize,
    exchangeToken,
    revokeOAuthToken,
    getMe,
    getStatusSummary,
    lockSession,
    beginSessionUnlock,
    listAccounts,
    listGroups,
    joinGroup,
    leaveGroup,
    listSections,
    listEmoji,
    createEmoji,
    lookupCodePoints,
    listReferencePatterns,
    createReferencePattern,
    updateReferencePattern,
    deleteReferencePattern,
    listConversations,
    getConversation,
    setReadPosition,
    updateMembership,
    listMessages,
    postMessage,
    checkBroadMentions,
    addReaction,
    removeReaction,
    setChecklistItem,
    openEvents,
    listEvents,
    downloadFile,
    downloadThumbnail,
    ...formsChannelsSearch,
    ...conversationScreen,
    ...accountArea,
    ...themesAndHubs,
    ...notifications,
    ...sealedChats,
    ...calls,
    ...slashCommands,
  ];

  // The moderation commands of rulings R67 and R72 (/mute, /ban, /unban,
  // /exile, /unexile) and the channel's list of bans; the others call
  // operations listed elsewhere.
  static const timeOutMember = Op(
    'timeOutMember',
    'POST',
    '/api/v1/conversations/{id}/timeouts',
  );
  static const createBan = Op('createBan', 'POST', '/api/v1/admin/bans');
  static const listBans = Op('listBans', 'GET', '/api/v1/admin/bans');
  static const liftBan = Op('liftBan', 'DELETE', '/api/v1/admin/bans/{id}');
  static const listChannelBans = Op(
    'listChannelBans',
    'GET',
    '/api/v1/conversations/{id}/bans',
  );
  static const banFromChannel = Op(
    'banFromChannel',
    'POST',
    '/api/v1/conversations/{id}/bans',
  );
  static const liftChannelBan = Op(
    'liftChannelBan',
    'DELETE',
    '/api/v1/conversations/{id}/bans/{account_id}',
  );
  // Knocks and the server broadcast (ruling R74).
  static const knock = Op('knock', 'POST', '/api/v1/conversations/{id}/knocks');
  static const listKnocks = Op(
    'listKnocks',
    'GET',
    '/api/v1/conversations/{id}/knocks',
  );
  static const answerKnock = Op(
    'answerKnock',
    'POST',
    '/api/v1/conversations/{id}/knocks/{knock_id}',
  );
  static const sendBroadcast = Op(
    'sendBroadcast',
    'POST',
    '/api/v1/admin/broadcast',
  );
  static const clearBroadcast = Op(
    'clearBroadcast',
    'DELETE',
    '/api/v1/admin/broadcast',
  );
  static const slashCommands = [
    timeOutMember,
    createBan,
    listBans,
    liftBan,
    listChannelBans,
    banFromChannel,
    liftChannelBan,
    knock,
    listKnocks,
    answerKnock,
    sendBroadcast,
    clearBroadcast,
  ];

  // Phone wake-ups and device registration (docs/PUSH.md sections 3
  // and 9). `removeDevice` and `getStatusSummary` are listed above.
  static const registerDevice = Op(
    'registerDevice',
    'PUT',
    '/api/v1/me/devices/current',
  );
  static const fetchWakeUp = Op(
    'fetchWakeUp',
    'GET',
    '/api/v1/wake-ups/{event_id}',
  );

  static const notifications = [registerDevice, fetchWakeUp];

  // Calls (docs/CALLS.md; lib/core/api/calls.dart).
  static const getCall = Op(
    'getCall',
    'GET',
    '/api/v1/conversations/{id}/call',
  );
  static const joinCall = Op(
    'joinCall',
    'POST',
    '/api/v1/conversations/{id}/call/join',
  );
  static const leaveCall = Op(
    'leaveCall',
    'POST',
    '/api/v1/conversations/{id}/call/leave',
  );

  static const calls = [getCall, joinCall, leaveCall];

  // Sealed chats (docs/SEALED.md; the sealed-chat screens,
  // lib/core/sealed_client). `listDevices` and `removeDevice` are listed
  // with the account area, `registerDevice` with notifications.
  static const openSealedChat = Op('openSealedChat', 'POST', '/api/v1/sealed');
  static const listDeviceListVersions = Op(
    'listDeviceListVersions',
    'GET',
    '/api/v1/accounts/{id}/device-lists',
  );
  static const publishDeviceList = Op(
    'publishDeviceList',
    'POST',
    '/api/v1/me/device-lists',
  );
  static const postEpochRecord = Op(
    'postEpochRecord',
    'POST',
    '/api/v1/conversations/{id}/sealed/epochs',
  );
  static const listEpochRecords = Op(
    'listEpochRecords',
    'GET',
    '/api/v1/conversations/{id}/sealed/epochs',
  );
  static const getBackup = Op('getBackup', 'GET', '/api/v1/me/backup');
  static const getAccessKey = Op('getAccessKey', 'GET', '/api/v1/me/access-key');
  static const createBackup = Op('createBackup', 'POST', '/api/v1/me/backup');
  static const addLockedCopy = Op(
    'addLockedCopy',
    'POST',
    '/api/v1/me/backup/copies',
  );
  static const putAccountRecord = Op(
    'putAccountRecord',
    'PUT',
    '/api/v1/me/backup/account-record',
  );
  static const deleteLockedCopy = Op(
    'deleteLockedCopy',
    'DELETE',
    '/api/v1/me/backup/copies/{id}',
  );
  static const listBackupRecords = Op(
    'listBackupRecords',
    'GET',
    '/api/v1/me/backup/records',
  );
  static const putBackupRecord = Op(
    'putBackupRecord',
    'PUT',
    '/api/v1/me/backup/records/{conversation_id}/{key_epoch}',
  );
  static const replaceBackup = Op(
    'replaceBackup',
    'POST',
    '/api/v1/me/backup/replace',
  );
  static const resetBackup = Op(
    'resetBackup',
    'POST',
    '/api/v1/me/backup/reset',
  );
  static const createUnlockRequest = Op(
    'createUnlockRequest',
    'POST',
    '/api/v1/me/unlock-requests',
  );
  static const listUnlockRequests = Op(
    'listUnlockRequests',
    'GET',
    '/api/v1/me/unlock-requests',
  );
  static const getUnlockRequest = Op(
    'getUnlockRequest',
    'GET',
    '/api/v1/me/unlock-requests/{id}',
  );
  static const answerUnlockRequest = Op(
    'answerUnlockRequest',
    'PUT',
    '/api/v1/me/unlock-requests/{id}/answer',
  );
  static const listPasskeys = Op('listPasskeys', 'GET', '/api/v1/me/passkeys');
  static const setPasskeyUnlockKey = Op(
    'setPasskeyUnlockKey',
    'PUT',
    '/api/v1/me/passkeys/{id}/unlock-key',
  );
  static const beginReattachDevice = Op(
    'beginReattachDevice',
    'POST',
    '/api/v1/me/devices/reattach/begin',
  );
  static const finishReattachDevice = Op(
    'finishReattachDevice',
    'POST',
    '/api/v1/me/devices/reattach/finish',
  );
  static const openUnlockPage = Op('openUnlockPage', 'GET', '/oauth/unlock');

  static const sealedChats = [
    openSealedChat,
    listDeviceListVersions,
    publishDeviceList,
    postEpochRecord,
    listEpochRecords,
    getBackup,
    createBackup,
    addLockedCopy,
    putAccountRecord,
    deleteLockedCopy,
    listBackupRecords,
    putBackupRecord,
    replaceBackup,
    resetBackup,
    createUnlockRequest,
    listUnlockRequests,
    getUnlockRequest,
    answerUnlockRequest,
    listPasskeys,
    setPasskeyUnlockKey,
    beginReattachDevice,
    finishReattachDevice,
    openUnlockPage,
  ];

  // Forms, checklists, bot dialogs, search and channel management
  // (the forms, search and channels screens).
  static const answerForm = Op(
    'answerForm',
    'PUT',
    '/api/v1/messages/{id}/form/answer',
  );
  static const withdrawFormAnswer = Op(
    'withdrawFormAnswer',
    'DELETE',
    '/api/v1/messages/{id}/form/answer',
  );
  static const pressFormButton = Op(
    'pressFormButton',
    'POST',
    '/api/v1/messages/{id}/form/buttons/{button}',
  );
  static const addFormOption = Op(
    'addFormOption',
    'POST',
    '/api/v1/messages/{id}/form/options',
  );
  static const closeForm = Op(
    'closeForm',
    'POST',
    '/api/v1/messages/{id}/form/close',
  );
  static const listFormResults = Op(
    'listFormResults',
    'GET',
    '/api/v1/messages/{id}/form/results',
  );
  static const downloadFormResults = Op(
    'downloadFormResults',
    'GET',
    '/api/v1/messages/{id}/form/results.csv',
  );
  static const getChecklistItemHistory = Op(
    'getChecklistItemHistory',
    'GET',
    '/api/v1/messages/{id}/checklist/{item}/history',
  );
  static const submitDialog = Op(
    'submitDialog',
    'POST',
    '/api/v1/dialogs/{id}/submit',
  );
  static const dismissDialog = Op(
    'dismissDialog',
    'DELETE',
    '/api/v1/dialogs/{id}',
  );
  static const search = Op('search', 'GET', '/api/v1/search');
  static const browseChannels = Op('browseChannels', 'GET', '/api/v1/channels');
  static const createChannel = Op('createChannel', 'POST', '/api/v1/channels');
  static const openChat = Op('openChat', 'POST', '/api/v1/chats');
  // Closing a direct or sealed chat, and withdrawing a request that waits
  // (rulings R78, R79).
  static const closeChat = Op(
    'closeChat',
    'POST',
    '/api/v1/conversations/{id}/close',
  );
  static const withdrawChatRequest = Op(
    'withdrawChatRequest',
    'POST',
    '/api/v1/conversations/{id}/withdraw',
  );
  static const updateConversation = Op(
    'updateConversation',
    'PATCH',
    '/api/v1/conversations/{id}',
  );
  static const joinConversation = Op(
    'joinConversation',
    'POST',
    '/api/v1/conversations/{id}/join',
  );
  static const leaveConversation = Op(
    'leaveConversation',
    'POST',
    '/api/v1/conversations/{id}/leave',
  );
  static const archiveConversation = Op(
    'archiveConversation',
    'POST',
    '/api/v1/conversations/{id}/archive',
  );
  static const unarchiveConversation = Op(
    'unarchiveConversation',
    'DELETE',
    '/api/v1/conversations/{id}/archive',
  );
  static const lockConversation = Op(
    'lockConversation',
    'POST',
    '/api/v1/conversations/{id}/lock',
  );
  static const listMembers = Op(
    'listMembers',
    'GET',
    '/api/v1/conversations/{id}/members',
  );
  static const addMembers = Op(
    'addMembers',
    'POST',
    '/api/v1/conversations/{id}/members',
  );
  static const setMemberLevel = Op(
    'setMemberLevel',
    'PATCH',
    '/api/v1/conversations/{id}/members/{account_id}',
  );
  static const setMemberMayPost = Op(
    'setMemberMayPost',
    'PUT',
    '/api/v1/conversations/{id}/members/{account_id}/may-post',
  );
  static const removeMember = Op(
    'removeMember',
    'DELETE',
    '/api/v1/conversations/{id}/members/{account_id}',
  );
  static const listChannelGroups = Op(
    'listChannelGroups',
    'GET',
    '/api/v1/conversations/{id}/groups',
  );
  static const addChannelGroup = Op(
    'addChannelGroup',
    'PUT',
    '/api/v1/conversations/{id}/groups/{group_id}',
  );
  static const removeChannelGroup = Op(
    'removeChannelGroup',
    'DELETE',
    '/api/v1/conversations/{id}/groups/{group_id}',
  );
  static const listPins = Op(
    'listPins',
    'GET',
    '/api/v1/conversations/{id}/pins',
  );
  static const requestWebhook = Op(
    'requestWebhook',
    'POST',
    '/api/v1/conversations/{id}/webhook-requests',
  );

  static const formsChannelsSearch = [
    answerForm,
    withdrawFormAnswer,
    pressFormButton,
    addFormOption,
    closeForm,
    listFormResults,
    downloadFormResults,
    getChecklistItemHistory,
    submitDialog,
    dismissDialog,
    search,
    browseChannels,
    createChannel,
    openChat,
    closeChat,
    withdrawChatRequest,
    updateConversation,
    joinConversation,
    leaveConversation,
    archiveConversation,
    unarchiveConversation,
    lockConversation,
    listMembers,
    addMembers,
    setMemberLevel,
    setMemberMayPost,
    removeMember,
    listChannelGroups,
    addChannelGroup,
    removeChannelGroup,
    listPins,
    requestWebhook,
  ];

  // Inside a conversation: threads, the composer, message actions and
  // files (the conversation screen).
  static const getMessage = Op('getMessage', 'GET', '/api/v1/messages/{id}');
  static const editMessage = Op(
    'editMessage',
    'PATCH',
    '/api/v1/messages/{id}',
  );
  static const deleteMessage = Op(
    'deleteMessage',
    'DELETE',
    '/api/v1/messages/{id}',
  );
  static const getThread = Op(
    'getThread',
    'GET',
    '/api/v1/messages/{id}/thread',
  );
  static const getMessageHistory = Op(
    'getMessageHistory',
    'GET',
    '/api/v1/messages/{id}/history',
  );
  static const purgeMessage = Op(
    'purgeMessage',
    'POST',
    '/api/v1/messages/{id}/purge',
  );
  static const listReactions = Op(
    'listReactions',
    'GET',
    '/api/v1/messages/{id}/reactions',
  );
  static const notifyAnyway = Op(
    'notifyAnyway',
    'POST',
    '/api/v1/messages/{id}/notify-anyway',
  );
  static const reportMessage = Op(
    'reportMessage',
    'POST',
    '/api/v1/messages/{id}/report',
  );
  static const addBookmark = Op(
    'addBookmark',
    'PUT',
    '/api/v1/messages/{id}/bookmark',
  );
  static const removeBookmark = Op(
    'removeBookmark',
    'DELETE',
    '/api/v1/messages/{id}/bookmark',
  );
  static const moveThreadToChannel = Op(
    'moveThreadToChannel',
    'POST',
    '/api/v1/messages/{id}/move-to-channel',
  );
  static const setThreadState = Op(
    'setThreadState',
    'PUT',
    '/api/v1/messages/{id}/thread/state',
  );
  static const setThreadReadPosition = Op(
    'setThreadReadPosition',
    'PUT',
    '/api/v1/messages/{id}/thread/read',
  );
  static const pinMessage = Op(
    'pinMessage',
    'PUT',
    '/api/v1/conversations/{id}/pins/{message_id}',
  );
  static const unpinMessage = Op(
    'unpinMessage',
    'DELETE',
    '/api/v1/conversations/{id}/pins/{message_id}',
  );
  static const sendTyping = Op(
    'sendTyping',
    'POST',
    '/api/v1/conversations/{id}/typing',
  );
  static const uploadFile = Op(
    'uploadFile',
    'POST',
    '/api/v1/conversations/{id}/files',
  );
  static const downloadPoster = Op(
    'downloadPoster',
    'GET',
    '/api/v1/files/{id}/poster',
  );
  static const listCommands = Op('listCommands', 'GET', '/api/v1/commands');
  static const runCommand = Op(
    'runCommand',
    'POST',
    '/api/v1/conversations/{id}/commands',
  );
  static const createScheduled = Op(
    'createScheduled',
    'POST',
    '/api/v1/scheduled',
  );
  static const updatePrefs = Op('updatePrefs', 'PATCH', '/api/v1/me/prefs');

  static const conversationScreen = [
    getMessage,
    editMessage,
    deleteMessage,
    getThread,
    getMessageHistory,
    purgeMessage,
    listReactions,
    notifyAnyway,
    reportMessage,
    addBookmark,
    removeBookmark,
    moveThreadToChannel,
    setThreadState,
    setThreadReadPosition,
    pinMessage,
    unpinMessage,
    sendTyping,
    uploadFile,
    downloadPoster,
    listCommands,
    runCommand,
    createScheduled,
    updatePrefs,
  ];

  // The person's own state, sign-in, sessions and devices, bookmarks
  // and threads across conversations (the account screens). Bookmarks,
  // thread state and `updatePrefs` are listed above.
  static const updateMe = Op('updateMe', 'PATCH', '/api/v1/me');
  static const setMyAvatar = Op('setMyAvatar', 'PUT', '/api/v1/me/avatar');
  static const removeMyAvatar = Op(
    'removeMyAvatar',
    'DELETE',
    '/api/v1/me/avatar',
  );
  static const setStatus = Op('setStatus', 'PUT', '/api/v1/me/status');
  static const clearStatus = Op('clearStatus', 'DELETE', '/api/v1/me/status');
  static const setAvailability = Op(
    'setAvailability',
    'PUT',
    '/api/v1/me/availability',
  );
  static const clearAvailability = Op(
    'clearAvailability',
    'DELETE',
    '/api/v1/me/availability',
  );
  static const setDnd = Op('setDnd', 'PUT', '/api/v1/me/dnd');
  static const clearDnd = Op('clearDnd', 'DELETE', '/api/v1/me/dnd');
  static const getQuietHours = Op(
    'getQuietHours',
    'GET',
    '/api/v1/me/quiet-hours',
  );
  static const setQuietHours = Op(
    'setQuietHours',
    'PUT',
    '/api/v1/me/quiet-hours',
  );
  static const removeQuietHours = Op(
    'removeQuietHours',
    'DELETE',
    '/api/v1/me/quiet-hours',
  );
  static const skipQuietHours = Op(
    'skipQuietHours',
    'PUT',
    '/api/v1/me/quiet-hours/skip',
  );
  static const unskipQuietHours = Op(
    'unskipQuietHours',
    'DELETE',
    '/api/v1/me/quiet-hours/skip',
  );
  static const setAway = Op('setAway', 'PUT', '/api/v1/me/away');
  static const clearAway = Op('clearAway', 'DELETE', '/api/v1/me/away');
  static const listStatusPresets = Op(
    'listStatusPresets',
    'GET',
    '/api/v1/status-presets',
  );
  static const createStatusPreset = Op(
    'createStatusPreset',
    'POST',
    '/api/v1/me/status-presets',
  );
  static const deleteStatusPreset = Op(
    'deleteStatusPreset',
    'DELETE',
    '/api/v1/me/status-presets/{id}',
  );
  static const getAccount = Op('getAccount', 'GET', '/api/v1/accounts/{id}');
  static const listBlocks = Op('listBlocks', 'GET', '/api/v1/me/blocks');
  static const blockAccount = Op(
    'blockAccount',
    'PUT',
    '/api/v1/me/blocks/{account_id}',
  );
  static const unblockAccount = Op(
    'unblockAccount',
    'DELETE',
    '/api/v1/me/blocks/{account_id}',
  );
  // Watches: "tell me when this person is around" (ruling R76), listed
  // and cancelled with the scheduled items.
  static const watchAccount = Op(
    'watchAccount',
    'PUT',
    '/api/v1/me/watches/{account_id}',
  );
  static const unwatchAccount = Op(
    'unwatchAccount',
    'DELETE',
    '/api/v1/me/watches/{account_id}',
  );
  // Chat requests (ruling R78): the person asked answers them.
  static const listChatRequests = Op(
    'listChatRequests',
    'GET',
    '/api/v1/me/chat-requests',
  );
  static const acceptChatRequest = Op(
    'acceptChatRequest',
    'POST',
    '/api/v1/me/chat-requests/{id}/accept',
  );
  static const declineChatRequest = Op(
    'declineChatRequest',
    'POST',
    '/api/v1/me/chat-requests/{id}/decline',
  );
  static const dismissChatRequest = Op(
    'dismissChatRequest',
    'POST',
    '/api/v1/me/chat-requests/{id}/dismiss',
  );
  static const blockChatRequester = Op(
    'blockChatRequester',
    'POST',
    '/api/v1/me/chat-requests/{id}/block',
  );
  // Approvals (docs/FEATURES.md section 31): the requests that wait for
  // whoever may decide them, and asking for a channel while new channels
  // wait for approval.
  static const getApprovalSummary = Op(
    'getApprovalSummary',
    'GET',
    '/api/v1/approvals/summary',
  );
  static const listApprovals = Op('listApprovals', 'GET', '/api/v1/approvals');
  static const decideApproval = Op(
    'decideApproval',
    'POST',
    '/api/v1/approvals/{kind}/{id}',
  );
  static const requestChannel = Op(
    'requestChannel',
    'POST',
    '/api/v1/approvals/channels',
  );
  static const listScheduled = Op('listScheduled', 'GET', '/api/v1/scheduled');
  static const cancelScheduled = Op(
    'cancelScheduled',
    'DELETE',
    '/api/v1/scheduled/{id}',
  );
  static const updateScheduled = Op(
    'updateScheduled',
    'PATCH',
    '/api/v1/scheduled/{id}',
  );
  static const sendScheduledNow = Op(
    'sendScheduledNow',
    'POST',
    '/api/v1/scheduled/{id}/send',
  );
  static const listSavedReplies = Op(
    'listSavedReplies',
    'GET',
    '/api/v1/saved-replies',
  );
  static const createSavedReply = Op(
    'createSavedReply',
    'POST',
    '/api/v1/saved-replies',
  );
  static const updateSavedReply = Op(
    'updateSavedReply',
    'PATCH',
    '/api/v1/saved-replies/{id}',
  );
  static const deleteSavedReply = Op(
    'deleteSavedReply',
    'DELETE',
    '/api/v1/saved-replies/{id}',
  );
  static const listBookmarks = Op('listBookmarks', 'GET', '/api/v1/bookmarks');
  static const listThreads = Op('listThreads', 'GET', '/api/v1/threads');
  static const listSessions = Op('listSessions', 'GET', '/api/v1/me/sessions');
  static const endOtherSessions = Op(
    'endOtherSessions',
    'DELETE',
    '/api/v1/me/sessions',
  );
  static const endSession = Op(
    'endSession',
    'DELETE',
    '/api/v1/me/sessions/{id}',
  );
  static const listDevices = Op('listDevices', 'GET', '/api/v1/me/devices');
  static const removeDevice = Op(
    'removeDevice',
    'DELETE',
    '/api/v1/me/devices/{id}',
  );

  static const accountArea = [
    updateMe,
    setMyAvatar,
    removeMyAvatar,
    setStatus,
    clearStatus,
    setAvailability,
    clearAvailability,
    setDnd,
    clearDnd,
    getQuietHours,
    setQuietHours,
    removeQuietHours,
    skipQuietHours,
    unskipQuietHours,
    setAway,
    clearAway,
    listStatusPresets,
    createStatusPreset,
    deleteStatusPreset,
    getAccount,
    listBlocks,
    blockAccount,
    unblockAccount,
    watchAccount,
    unwatchAccount,
    listChatRequests,
    acceptChatRequest,
    declineChatRequest,
    dismissChatRequest,
    blockChatRequester,
    getApprovalSummary,
    listApprovals,
    decideApproval,
    requestChannel,
    listScheduled,
    cancelScheduled,
    updateScheduled,
    sendScheduledNow,
    listSavedReplies,
    createSavedReply,
    updateSavedReply,
    deleteSavedReply,
    listBookmarks,
    listThreads,
    listSessions,
    endOtherSessions,
    endSession,
    listDevices,
    removeDevice,
  ];

  // Themes (ruling R61).
  static const listMyThemes = Op('listMyThemes', 'GET', '/api/v1/me/themes');
  static const createTheme = Op('createTheme', 'POST', '/api/v1/me/themes');
  static const getTheme = Op('getTheme', 'GET', '/api/v1/themes/{id}');
  static const updateTheme = Op(
    'updateTheme',
    'PATCH',
    '/api/v1/me/themes/{id}',
  );
  static const deleteTheme = Op(
    'deleteTheme',
    'DELETE',
    '/api/v1/me/themes/{id}',
  );
  static const publishTheme = Op(
    'publishTheme',
    'POST',
    '/api/v1/me/themes/{id}/publish',
  );
  static const unpublishTheme = Op(
    'unpublishTheme',
    'DELETE',
    '/api/v1/me/themes/{id}/publish',
  );
  static const listThemeLibrary = Op(
    'listThemeLibrary',
    'GET',
    '/api/v1/themes',
  );
  static const addTheme = Op('addTheme', 'POST', '/api/v1/themes/{id}/add');
  static const removeLibraryTheme = Op(
    'removeLibraryTheme',
    'DELETE',
    '/api/v1/admin/themes/{id}',
  );
  static const setDefaultTheme = Op(
    'setDefaultTheme',
    'PUT',
    '/api/v1/admin/theme-defaults',
  );

  // Channels on a hub (rulings R56, R59) and the owner's hub screens.
  static const listPeers = Op('listPeers', 'GET', '/api/v1/admin/peers');
  static const shareChannel = Op(
    'shareChannel',
    'POST',
    '/api/v1/admin/streams',
  );
  static const listHubDirectory = Op(
    'listHubDirectory',
    'GET',
    '/api/v1/admin/peers/{id}/directory',
  );
  static const joinHubChannel = Op(
    'joinHubChannel',
    'POST',
    '/api/v1/admin/peers/{id}/directory/join',
  );
  static const listStreams = Op('listStreams', 'GET', '/api/v1/admin/streams');
  static const getHubChannel = Op(
    'getHubChannel',
    'GET',
    '/api/v1/streams/{id}/channel',
  );
  static const moderateHubChannel = Op(
    'moderateHubChannel',
    'POST',
    '/api/v1/streams/{id}/channel/actions',
  );
  static const askToJoinHubChannel = Op(
    'askToJoinHubChannel',
    'POST',
    '/api/v1/streams/{id}/channel/requests',
  );
  static const listHubChannelRequests = Op(
    'listHubChannelRequests',
    'GET',
    '/api/v1/streams/{id}/channel/requests',
  );
  static const listHubChannelCandidates = Op(
    'listHubChannelCandidates',
    'GET',
    '/api/v1/streams/{id}/channel/candidates',
  );
  static const moderateHubChannelMember = Op(
    'moderateHubChannelMember',
    'POST',
    '/api/v1/streams/{id}/channel/members',
  );
  static const proposeStreamMove = Op(
    'proposeStreamMove',
    'POST',
    '/api/v1/admin/streams/{id}/move',
  );
  static const listStreamMoves = Op(
    'listStreamMoves',
    'GET',
    '/api/v1/admin/moves',
  );
  static const acceptStreamMove = Op(
    'acceptStreamMove',
    'POST',
    '/api/v1/admin/moves/{id}/accept',
  );
  static const declineStreamMove = Op(
    'declineStreamMove',
    'POST',
    '/api/v1/admin/moves/{id}/decline',
  );
  static const cancelStreamMove = Op(
    'cancelStreamMove',
    'POST',
    '/api/v1/admin/moves/{id}/cancel',
  );

  static const themesAndHubs = [
    listMyThemes,
    createTheme,
    getTheme,
    updateTheme,
    deleteTheme,
    publishTheme,
    unpublishTheme,
    listThemeLibrary,
    addTheme,
    removeLibraryTheme,
    setDefaultTheme,
    listPeers,
    shareChannel,
    listHubDirectory,
    joinHubChannel,
    listStreams,
    getHubChannel,
    moderateHubChannel,
    askToJoinHubChannel,
    listHubChannelRequests,
    listHubChannelCandidates,
    moderateHubChannelMember,
    proposeStreamMove,
    listStreamMoves,
    acceptStreamMove,
    declineStreamMove,
    cancelStreamMove,
  ];
}

/// The thumbnail path of a file known only by its identifier (a card's
/// image). Everything else is fetched from the paths the server sends.
String thumbnailPath(String fileId) =>
    Ops.downloadThumbnail.fill({'id': fileId});
