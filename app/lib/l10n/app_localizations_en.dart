// ignore: unused_import
import 'package:intl/intl.dart' as intl;

import 'app_localizations.dart';

// ignore_for_file: type=lint

/// The translations for English (`en`).
class L10nEn extends L10n {
  L10nEn([String locale = 'en']) : super(locale);

  @override
  String get appTitle => 'Churr';

  @override
  String get cancel => 'Cancel';

  @override
  String get save => 'Save';

  @override
  String get close => 'Close';

  @override
  String get done => 'Done';

  @override
  String get retry => 'Try again';

  @override
  String didNotWork(String reason) {
    return 'That did not work: $reason';
  }

  @override
  String get for30Minutes => 'For 30 minutes';

  @override
  String get forAnHour => 'For an hour';

  @override
  String get for8Hours => 'For 8 hours';

  @override
  String get forADay => 'For a day';

  @override
  String get forAWeek => 'For a week';

  @override
  String get untilTomorrowMorning => 'Until tomorrow morning';

  @override
  String get untilChosenTime => 'Until a chosen time…';

  @override
  String get untilTurnedOff => 'Until turned off';

  @override
  String untilTime(String time) {
    return 'until $time';
  }

  @override
  String untilTimeCap(String time) {
    return 'Until $time';
  }

  @override
  String get noEnd => 'No end';

  @override
  String get chooseDate => 'Choose the day';

  @override
  String get chooseTime => 'Choose the time';

  @override
  String refusedOn(String server, String reason) {
    return 'Not changed on $server: $reason';
  }

  @override
  String waitingFor(String server) {
    return 'Will be sent to $server when it is back';
  }

  @override
  String get signedOutShort => 'Signed out';

  @override
  String get notifications => 'Notifications';

  @override
  String get threads => 'Threads';

  @override
  String get bookmarks => 'Bookmarks';

  @override
  String get allServers => 'All servers';

  @override
  String get allKinds => 'All';

  @override
  String get mentionsOnly => 'Mentions';

  @override
  String get threadsOnly => 'Threads';

  @override
  String get markAllRead => 'Mark all read';

  @override
  String get markRead => 'Mark read';

  @override
  String get earlier => 'Earlier';

  @override
  String get nothingNew =>
      'Nothing new. Mentions, replies in threads you follow and messages in conversations set to Any activity show up here.';

  @override
  String get reasonMentionedYou => 'mentioned you';

  @override
  String get reasonMentionedGroup => 'mentioned a group you are in';

  @override
  String get reasonMentionedChannel => 'mentioned everyone here';

  @override
  String get reasonThreadReply => 'reply in a thread you follow';

  @override
  String get reasonNewMessage => 'new message';

  @override
  String get reasonMissedCall => 'missed call';

  @override
  String moreCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count more',
      one: '1 more',
    );
    return '$_temp0';
  }

  @override
  String get inThread => 'in a thread';

  @override
  String get sealedMessage => 'Sealed message';

  @override
  String get someone => 'Someone';

  @override
  String get conversationSettings => 'Notifications and sidebar…';

  @override
  String get followThread => 'Follow the thread';

  @override
  String get ignoreThread => 'Ignore the thread';

  @override
  String get stopFollowing => 'Stop following';

  @override
  String get filterServer => 'Server';

  @override
  String get filterKind => 'Kind';

  @override
  String itemLabel(String who, String where, String why, String when) {
    return '$who in $where, $why, $when';
  }

  @override
  String get whatNotifies => 'What notifies';

  @override
  String get showInSidebar => 'Show in sidebar';

  @override
  String get modeAny => 'Any activity';

  @override
  String get modeMentions => 'Mentions';

  @override
  String get modeNothing => 'Nothing';

  @override
  String get muteFor => 'Mute for…';

  @override
  String mutedUntil(String time) {
    return 'Muted until $time';
  }

  @override
  String get unmute => 'Unmute';

  @override
  String showDefault(String current) {
    return 'App default ($current)';
  }

  @override
  String get showAlways => 'Always';

  @override
  String get showActivity => 'When there is activity';

  @override
  String get showNotification => 'When it notifies';

  @override
  String get showHidden => 'Hidden';

  @override
  String get star => 'Star';

  @override
  String get starred => 'Starred';

  @override
  String get threadView => 'Thread view';

  @override
  String threadViewDefault(String current) {
    return 'Your default ($current)';
  }

  @override
  String get threadViewThreaded => 'Threaded';

  @override
  String get threadViewFlat => 'Flat';

  @override
  String get sumAny => 'Every message will notify you.';

  @override
  String get sumMentions =>
      'Mentions and replies in threads you follow will notify you.';

  @override
  String get sumNothing =>
      'Nothing will notify you; a mention of you by name still counts in the badge.';

  @override
  String sumMuted(String time, String then) {
    return 'Muted until $time, then: $then';
  }

  @override
  String get sumAlways => 'The conversation is always in the sidebar.';

  @override
  String get sumActivity =>
      'It appears in the sidebar only while it has something unread.';

  @override
  String get sumNotification =>
      'It appears in the sidebar only while it has a badge.';

  @override
  String get sumHidden => 'It has no row in the sidebar.';

  @override
  String get sumHiddenNotifies =>
      'Its notifications will be in the Notifications list.';

  @override
  String get muteTitle => 'Mute this conversation';

  @override
  String get showReadThreads => 'Show read threads';

  @override
  String get noThreads => 'No followed thread has anything new.';

  @override
  String unreadReplies(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count unread replies',
      one: '1 unread reply',
    );
    return '$_temp0';
  }

  @override
  String get replyInPlace => 'Reply…';

  @override
  String get replyHint => 'Reply in the thread';

  @override
  String get send => 'Send';

  @override
  String get openThread => 'Open the thread';

  @override
  String get noBookmarks =>
      'No bookmarks yet. Bookmark a message from its menu to keep it here.';

  @override
  String get removeBookmark => 'Remove the bookmark';

  @override
  String get openInPlace => 'Open in its conversation';

  @override
  String bookmarkedAt(String when) {
    return 'Bookmarked $when';
  }

  @override
  String get messageNotLoaded => 'The message is not on this device yet.';

  @override
  String get statusTitle => 'Status';

  @override
  String get statusEmoji => 'Emoji';

  @override
  String get statusText => 'What are you up to?';

  @override
  String get statusClears => 'Clears';

  @override
  String get statusClearsNever => 'Does not clear';

  @override
  String statusClearsAt(String time) {
    return 'Clears $time';
  }

  @override
  String get pauseNotifications => 'Pause notifications';

  @override
  String get appearAwaySwitch => 'Appear away';

  @override
  String get setStatus => 'Set status';

  @override
  String get clearStatus => 'Clear status';

  @override
  String get saveAsPreset => 'Save as preset';

  @override
  String get presets => 'Presets';

  @override
  String get sharedPresets => 'Saved from the app';

  @override
  String get communityPresets => 'From the community';

  @override
  String yourPresets(String server) {
    return 'Yours on $server';
  }

  @override
  String get deletePreset => 'Delete preset';

  @override
  String differsOn(String servers) {
    return 'Differs on $servers';
  }

  @override
  String get makeSame => 'Make them the same';

  @override
  String get availability => 'Availability';

  @override
  String get inMeeting => 'In a meeting';

  @override
  String get outOfOffice => 'Out of office';

  @override
  String get availableNow => 'Available';

  @override
  String get meetingPauses => 'A meeting pauses my notifications';

  @override
  String get clearAvailability => 'Clear';

  @override
  String get setUntil => 'Until…';

  @override
  String get appearAway => 'Appear away';

  @override
  String get appearAwayOff => 'Off';

  @override
  String appearAwayOn(String until) {
    return 'On, $until';
  }

  @override
  String get appearAwayForever => 'On until turned off';

  @override
  String get turnOn => 'Turn on…';

  @override
  String get turnOff => 'Turn off';

  @override
  String optedOutNote(String servers) {
    return 'Not sent to servers opted out of shared status: $servers';
  }

  @override
  String get dndTitle => 'Do not disturb';

  @override
  String dndOnServers(String servers) {
    return 'In do not disturb on $servers';
  }

  @override
  String get dndOff => 'You are available on every server.';

  @override
  String get dndWhyManual => 'turned on by you';

  @override
  String get dndWhyStatus => 'your status pauses notifications';

  @override
  String get dndWhyMeeting => 'a meeting pauses notifications';

  @override
  String get dndWhyQuiet => 'quiet hours';

  @override
  String get dndWhyMuted => 'this server is muted';

  @override
  String dndTurnOffClears(String what) {
    return 'Turning it off makes you available now. It will clear: $what';
  }

  @override
  String get dndTurnOnTitle => 'Do not disturb';

  @override
  String get quietHours => 'Quiet hours';

  @override
  String quietOn(String server) {
    return 'Quiet hours on $server';
  }

  @override
  String get quietFrom => 'From';

  @override
  String get quietTo => 'To';

  @override
  String get quietDays => 'Days';

  @override
  String quietZone(String zone) {
    return 'Time zone: $zone';
  }

  @override
  String get skipTonight => 'Skip tonight';

  @override
  String skipped(String time) {
    return 'Skipped until $time';
  }

  @override
  String get unskip => 'Do not skip';

  @override
  String get copyTo => 'Copy to…';

  @override
  String get copyToTitle => 'Copy this schedule to';

  @override
  String get copy => 'Copy';

  @override
  String get dayMon => 'Mon';

  @override
  String get dayTue => 'Tue';

  @override
  String get dayWed => 'Wed';

  @override
  String get dayThu => 'Thu';

  @override
  String get dayFri => 'Fri';

  @override
  String get daySat => 'Sat';

  @override
  String get daySun => 'Sun';

  @override
  String statusIn(String server) {
    return 'Status on $server';
  }

  @override
  String get profile => 'Profile';

  @override
  String get myProfile => 'Your profile';

  @override
  String get displayName => 'Display name';

  @override
  String get title => 'Title';

  @override
  String get pronouns => 'Pronouns';

  @override
  String get timeZone => 'Time zone';

  @override
  String useDeviceZone(String zone) {
    return 'Use this device\'s ($zone)';
  }

  @override
  String get username => 'Username';

  @override
  String get usernameNote =>
      'A username belongs to one server and is changed on its account page.';

  @override
  String get changeAvatar => 'Change picture';

  @override
  String get removeAvatar => 'Remove picture';

  @override
  String get applyTo => 'Apply to';

  @override
  String get applyToNote =>
      'Change these servers. Each server keeps its own copy of your profile.';

  @override
  String get apply => 'Apply';

  @override
  String get accountPage => 'Passkeys, access key, sessions and tokens…';

  @override
  String accountPageNote(String server) {
    return 'Opens $server\'s account page in your browser.';
  }

  @override
  String localTime(String time) {
    return 'Local time $time';
  }

  @override
  String get presenceOnline => 'Online';

  @override
  String get presenceAway => 'Away';

  @override
  String get presenceOffline => 'Offline';

  @override
  String get inACall => 'In a call';

  @override
  String outOfOfficeUntil(String date) {
    return 'Out of office until $date';
  }

  @override
  String outOfOfficeNote(String name) {
    return '$name is out of office.';
  }

  @override
  String inMeetingUntil(String time) {
    return 'In a meeting until $time';
  }

  @override
  String roleLabel(String role) {
    return 'Role: $role';
  }

  @override
  String groupsLabel(String groups) {
    return 'Groups: $groups';
  }

  @override
  String get message => 'Message';

  @override
  String get block => 'Block';

  @override
  String get unblock => 'Unblock';

  @override
  String blockConfirm(String name) {
    return 'Block $name? Their messages will be collapsed for you, they cannot start a chat with you, and their mentions will not notify you. They are not told.';
  }

  @override
  String get blocked => 'Blocked';

  @override
  String get doNotDisturbMark => 'In do not disturb';

  @override
  String statusUntil(String status, String time) {
    return '$status, until $time';
  }

  @override
  String get saved => 'Saved.';

  @override
  String get serverSettings => 'Server settings';

  @override
  String serverSettingsOf(String server) {
    return 'Settings for $server';
  }

  @override
  String get defaultThreadView => 'Default thread view';

  @override
  String get allowNotifyAnyway =>
      'Let people push a message through my do not disturb (notify anyway)';

  @override
  String get muteServer => 'Mute this server…';

  @override
  String get unmuteServer => 'Unmute this server';

  @override
  String serverMutedUntil(String time) {
    return 'Muted until $time';
  }

  @override
  String muteServerTitle(String server) {
    return 'Mute $server';
  }

  @override
  String muteServerWarning(String server) {
    return 'People on $server will see that you are in do not disturb.';
  }

  @override
  String get mute => 'Mute';

  @override
  String get takePartShared => 'Take part in shared status and do not disturb';

  @override
  String get takePartSharedNote =>
      'Off: status and do not disturb set from the app leave this server alone; set them here instead.';

  @override
  String get quietHoursLink => 'Quiet hours and do not disturb…';

  @override
  String get statusLink => 'Status on this server…';

  @override
  String get conversationsTable => 'Conversations';

  @override
  String selectedCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count selected',
      one: '1 selected',
    );
    return '$_temp0';
  }

  @override
  String get setModeFor => 'What notifies…';

  @override
  String get setShowFor => 'Show in sidebar…';

  @override
  String get sessionsAndDevices => 'Sessions and devices…';

  @override
  String get signOut => 'Sign out';

  @override
  String signOutConfirm(String server) {
    return 'Sign out of $server? Its messages and files are removed from this device.';
  }

  @override
  String get sessions => 'Sessions';

  @override
  String get devices => 'Devices';

  @override
  String get thisDevice => 'This device';

  @override
  String get endSession => 'Sign out';

  @override
  String get removeDevice => 'Remove';

  @override
  String lastSeen(String when) {
    return 'Last seen $when';
  }

  @override
  String get retired => 'Retired';

  @override
  String get kindWeb => 'Browser';

  @override
  String get kindApp => 'App';

  @override
  String get kindPersonal => 'Personal token';

  @override
  String get kindBot => 'Bot';

  @override
  String get freshCheckNeeded =>
      'This needs your passkey or access key: it is done on the account page in your browser.';

  @override
  String get openAccountPage => 'Open the account page';

  @override
  String signedOutBanner(String server) {
    return 'Signed out of $server';
  }

  @override
  String get signInAgain => 'Sign in again';

  @override
  String reSignInTitle(String server) {
    return 'Sign in to $server again';
  }

  @override
  String get reSignInBody =>
      'The server ended this device\'s session, as company sign-in does each day. What is on this device is kept until you sign in again as the same account.';

  @override
  String get reSignInOther =>
      'You signed in as a different account, so what this device kept for the old one was removed.';

  @override
  String get signInWithBrowser => 'Sign in with your browser';

  @override
  String get scanCode => 'Scan a code';

  @override
  String get copyFromDevice => 'Copy from another device';

  @override
  String get inviteLink => 'Invite link';

  @override
  String get inviteJoinNote =>
      'This is an invite. Join in your browser, then sign in here.';

  @override
  String get joinInBrowser => 'Join in your browser';

  @override
  String get pasteCode => 'Or paste the code\'s text';

  @override
  String pairingWaiting(String device) {
    return 'Confirm “$device” in your browser to finish signing in.';
  }

  @override
  String get pairingExpired =>
      'The code ran out. Make a new one on the account page.';

  @override
  String get notAChurrCode => 'That is not a Churr code.';

  @override
  String get cameraUnavailable =>
      'The camera is not available here. Paste the code\'s text instead.';

  @override
  String get setUpAnotherDevice => 'Set up another device';

  @override
  String get setUpNote =>
      'Scan this on the new device under Add a server → Copy from another device. It holds your app settings and the addresses of your servers. It holds no password, token or key.';

  @override
  String get setupApplied =>
      'Your settings are in place. Sign in to each server in turn.';

  @override
  String get setupServers => 'Servers to sign in to';

  @override
  String get skip => 'Skip';

  @override
  String get signedIn => 'Signed in';

  @override
  String get addAServer => 'Add a server';

  @override
  String codePart(int n, int count) {
    return 'Code $n of $count';
  }

  @override
  String setupReceived(int n, int count) {
    return '$n of $count codes scanned';
  }

  @override
  String get addServerToBegin => 'Add a server to begin.';

  @override
  String get serverAddress => 'Server\'s address';

  @override
  String get serverAddressHint => 'chat.example.org, or an invite link';

  @override
  String get continueButton => 'Continue';

  @override
  String get couldNotReach => 'Could not reach that server.';

  @override
  String signInDidNotFinish(String error) {
    return 'Sign-in did not finish ($error).';
  }

  @override
  String get enterAddress =>
      'Enter the server\'s address, such as chat.example.org.';

  @override
  String get notSetUp => 'This server has not been set up yet.';

  @override
  String serverVersion(String host, String version) {
    return '$host · Churr $version';
  }

  @override
  String get available => 'available';

  @override
  String get doNotDisturb => 'do not disturb';

  @override
  String get dndToggleOn => 'Turn on do not disturb';

  @override
  String get dndToggleOff => 'Turn off do not disturb';

  @override
  String get setAStatus => 'Set a status';

  @override
  String get yourStatus => 'Status, availability and appear away';

  @override
  String get serverAdminInBrowser => 'Server settings in the browser';

  @override
  String get moveUp => 'Move up';

  @override
  String get moveDown => 'Move down';

  @override
  String get muted => 'Muted';

  @override
  String get notConnected => 'Not connected';

  @override
  String get serversMenu => 'Servers';

  @override
  String get everythingElse => 'Everything else';

  @override
  String get newGroup => 'New group';

  @override
  String get groupName => 'Name';

  @override
  String get create => 'Create';

  @override
  String deleteGroup(String name) {
    return 'Delete group $name';
  }

  @override
  String get moveGroupUp => 'Move group up';

  @override
  String get moveGroupDown => 'Move group down';

  @override
  String get appSettings => 'App settings';

  @override
  String get sidebarLayout => 'Sidebar layout';

  @override
  String get layoutMerged => 'Merged tree';

  @override
  String get layoutRail => 'Server rail';

  @override
  String get layoutHybrid => 'Hybrid';

  @override
  String get showByDefault => 'Show in sidebar, by default';

  @override
  String get showHiddenSwitch => 'Show hidden';

  @override
  String get showHiddenNote =>
      'Draw conversations set to Hidden, in italics with a hidden tag.';

  @override
  String get serverOrder => 'Order of servers';

  @override
  String get servers => 'Servers';

  @override
  String get palette => 'Colours';

  @override
  String get paletteStandard => 'Standard';

  @override
  String get paletteHighDistinction => 'High distinction';

  @override
  String get paletteNote =>
      'High distinction swaps pink for orange and lavender for blue, so the colours stay apart with red-green colour blindness. The app also follows the system\'s \"increase contrast\" setting.';

  @override
  String get theme => 'Theme';

  @override
  String get themeDark => 'Dark';

  @override
  String get themeLight => 'Light';

  @override
  String get themeSystem => 'Follow the system';

  @override
  String get textSize => 'Text size';

  @override
  String get enterSends => 'Enter sends';

  @override
  String get enterSendsNote =>
      'Shift+Enter makes a new line. Phones always send with the button.';

  @override
  String get sounds => 'Sounds';

  @override
  String get startupSound => 'Play the start-up sound when the app opens';

  @override
  String get notificationsSection => 'Notifications on this device';

  @override
  String get showNotifications => 'Show notifications';

  @override
  String get notificationSoundsSwitch => 'Notification sounds';

  @override
  String get notificationTextSwitch =>
      'Show the message text (off: only who sent it)';

  @override
  String get language => 'Language';

  @override
  String get languageSystem => 'Follow the system';

  @override
  String get languageEnglish => 'English';

  @override
  String get fileCache => 'File cache';

  @override
  String fileCacheSize(String size, String cap) {
    return '$size used of $cap';
  }

  @override
  String get fileCacheCap => 'Keep at most';

  @override
  String get clearCache => 'Clear cache';

  @override
  String get cacheCleared => 'Cache cleared.';

  @override
  String get about => 'About';

  @override
  String get licences => 'Licences';

  @override
  String get sourceCode => 'Source code';

  @override
  String appVersion(String version) {
    return 'Version $version';
  }

  @override
  String get settingsFor => 'Settings…';

  @override
  String get arrivedLate => 'arrived late';

  @override
  String get arrivedLateExplained =>
      'Arrived late, after newer messages were already here';

  @override
  String get notSentToOtherServers => 'Not yet passed on to the other servers';

  @override
  String get themes => 'Themes';

  @override
  String get themesAndEditor => 'Themes and the theme editor';

  @override
  String get themesNote =>
      'Saved themes live on the server they were made on. The choices above stay on this device.';

  @override
  String get darkTheme => 'Dark theme';

  @override
  String get lightTheme => 'Light theme';

  @override
  String get change => 'Change';

  @override
  String get chooseDarkTheme => 'Choose the dark theme';

  @override
  String get chooseLightTheme => 'Choose the light theme';

  @override
  String get themePerServer => 'Use each server\'s default';

  @override
  String get themePerServerNote =>
      'Each server looks like itself: its own default theme while you are in it, where it has one, and your choice otherwise.';

  @override
  String themeOnServer(String name, String server) {
    return '$name on $server';
  }

  @override
  String get themeBuiltInDark => 'Dark';

  @override
  String get themeBuiltInLight => 'Light';

  @override
  String get themeBuiltInDarkHd => 'Dark, high distinction';

  @override
  String get themeBuiltInLightHd => 'Light, high distinction';

  @override
  String get kindDarkTag => 'dark';

  @override
  String get kindLightTag => 'light';

  @override
  String get builtInLabel => 'Built in';

  @override
  String get builtInThemes => 'Built-in themes';

  @override
  String get publishedLabel => 'in the library';

  @override
  String get inUse => 'in use';

  @override
  String savedOn(String server) {
    return 'Saved on $server';
  }

  @override
  String get themesCouldNotLoad =>
      'Could not load the themes saved on this server.';

  @override
  String get loading => 'Loading…';

  @override
  String get noSavedThemes => 'No saved themes here yet.';

  @override
  String get newTheme => 'New theme';

  @override
  String get moreActions => 'More actions';

  @override
  String get themeUse => 'Use';

  @override
  String get themeTry => 'Try';

  @override
  String get themeAdd => 'Add';

  @override
  String get themeEdit => 'Edit';

  @override
  String get themeEditACopy => 'Edit a copy';

  @override
  String get themeExport => 'Export a file';

  @override
  String get themeImport => 'Import a file';

  @override
  String get themeShareLink => 'Copy its link';

  @override
  String get themePublish => 'Publish';

  @override
  String get themeUnpublish => 'Unpublish';

  @override
  String get themeDelete => 'Delete';

  @override
  String themeLibraryOf(String server) {
    return 'Theme library of $server';
  }

  @override
  String get themeCardLabel => 'Theme';

  @override
  String themeBy(String name) {
    return 'by $name';
  }

  @override
  String themeCannotApply(String problem) {
    return 'This theme cannot be applied: $problem';
  }

  @override
  String themeAdded(String server) {
    return 'Added to your themes on $server.';
  }

  @override
  String themeTrying(String name) {
    return 'Trying “$name”. Nothing is saved.';
  }

  @override
  String get themeKeep => 'Keep it';

  @override
  String get themeStopTrying => 'Stop';

  @override
  String get themeGone => 'This theme no longer exists.';

  @override
  String get themeFileLabel => 'Churr theme';

  @override
  String themeExported(String file) {
    return 'Saved $file.';
  }

  @override
  String themeImported(String name) {
    return 'Imported “$name”. Save it to keep it.';
  }

  @override
  String get themeLinkCopied =>
      'The theme\'s link is copied. Anyone signed in to its server can open it.';

  @override
  String get themeEditor => 'Theme editor';

  @override
  String get themeNameLabel => 'Name';

  @override
  String themeCopyName(String name) {
    return '$name copy';
  }

  @override
  String get themeCopyMade => 'This is now a new, unsaved copy.';

  @override
  String get themeKind => 'Kind';

  @override
  String get themeKindDarkProblem =>
      'A dark theme needs a ground darker than its text.';

  @override
  String get themeKindLightProblem =>
      'A light theme needs a ground lighter than its text.';

  @override
  String get themeColours => 'Base colours';

  @override
  String get baseBg => 'Ground';

  @override
  String get basePanel => 'Panel';

  @override
  String get baseRaise => 'Raised';

  @override
  String get baseInk => 'Text';

  @override
  String get baseDim => 'Secondary text';

  @override
  String get baseLav => 'Lavender (yours)';

  @override
  String get baseCyan => 'Cyan (go)';

  @override
  String get basePink => 'Pink (attention)';

  @override
  String get baseYel => 'Yellow (counts)';

  @override
  String get hexInvalid => '#rrggbb';

  @override
  String get hslPicker => 'Hue, saturation and lightness';

  @override
  String get hue => 'Hue';

  @override
  String get saturation => 'Saturation';

  @override
  String get lightness => 'Lightness';

  @override
  String get themeContrast => 'Contrast';

  @override
  String themeAllPass(int count) {
    return 'All $count pairings pass.';
  }

  @override
  String themeSomeFail(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other:
          '$count pairings fail. A theme that fails cannot be saved, shared or applied.',
      one: '1 pairing fails. A theme that fails cannot be saved, shared or applied.',
    );
    return '$_temp0';
  }

  @override
  String get needText => 'text: needs 4.5:1';

  @override
  String get needEdge => 'edge or icon: needs 3:1';

  @override
  String get pass => 'pass';

  @override
  String get fail => 'fail';

  @override
  String get fix => 'Fix';

  @override
  String fixedColour(String colour, String hex) {
    return '$colour is now $hex: the smallest change that passes.';
  }

  @override
  String cannotFix(String colour) {
    return 'No change to $colour alone passes. Change another colour first.';
  }

  @override
  String get themeSaveSection => 'Save and share';

  @override
  String get themeSaveTo => 'Save to';

  @override
  String get themeNoServer => 'Choose a server';

  @override
  String get themeIsSaved => 'Saved. Only you can see it.';

  @override
  String get themeIsPublished => 'Saved and in the server\'s theme library.';

  @override
  String themeSavedOn(String server) {
    return 'Saved on $server.';
  }

  @override
  String get themeCannotSaveYet =>
      'It can be saved once it has a name and every pairing passes.';

  @override
  String get themePublishedNote =>
      'Published: everyone on the server can find it in the library.';

  @override
  String get themeUnpublishedNote =>
      'Taken out of the library. Copies people added stay theirs.';

  @override
  String themeDeleteTitle(String name) {
    return 'Delete “$name”?';
  }

  @override
  String get themeDeleteBody =>
      'It leaves the library if it is there. Copies others added and theme cards in messages stay.';

  @override
  String get themeDeleted => 'Deleted.';

  @override
  String get searchThemes => 'Search by name';

  @override
  String get libraryEmpty => 'Nobody has published a theme here yet.';

  @override
  String get libraryNoMatch => 'No theme here has that in its name.';

  @override
  String get showMore => 'Show more';

  @override
  String get makeDefaultDark => 'Make it the server\'s default dark theme';

  @override
  String get makeDefaultLight => 'Make it the server\'s default light theme';

  @override
  String get removeFromLibrary => 'Remove from the library';

  @override
  String defaultSet(String name) {
    return '“$name” is now this server\'s default.';
  }

  @override
  String get previewCommunity => 'Northwind';

  @override
  String get previewConnected => 'connected';

  @override
  String get previewSection => 'Channels';

  @override
  String get previewHiddenTag => 'hidden';

  @override
  String get previewTopic => 'Dates, owners and the launch list';

  @override
  String get previewMessage1 =>
      'The launch list is in the **planning doc**, see [the schedule](https://example.org) and `release.toml`.';

  @override
  String get previewMessage2 => 'can you check the dates by Friday?';

  @override
  String get previewThread => '2 new replies';

  @override
  String get previewPoll => 'Which day for the review?';

  @override
  String get previewPollA => 'Thursday';

  @override
  String get previewPollB => 'Friday';

  @override
  String get previewJoin => 'Join';

  @override
  String get previewSecondary => 'Later';

  @override
  String get previewDanger => 'Delete';

  @override
  String get previewTagPrivate => 'private';

  @override
  String get previewTagOn => 'on';

  @override
  String get previewTagFailed => 'failed';

  @override
  String get previewTagNew => 'new';

  @override
  String get previewComposer => 'Message #planning';

  @override
  String get hubsEntry => 'Hubs';

  @override
  String get hubsEntryNote => 'Channels on the hubs this server is on';

  @override
  String hubsOf(String server) {
    return 'Hubs of $server';
  }

  @override
  String get hubsNote =>
      'A hub carries channels between many servers and owns the channels on it. Linking this server to a hub is done in the web admin console.';

  @override
  String get linkInConsole => 'Link servers in the admin console';

  @override
  String get noHubs => 'This server is not on a hub.';

  @override
  String get hubConnected => 'connected';

  @override
  String get hubDirectory => 'Directory';

  @override
  String clockFigures(
    int offset,
    int uncertainty,
    int latency,
    int jitter,
    int samples,
  ) {
    return 'The hub\'s clock: this server is $offset ms off, within $uncertainty ms; round trip $latency ms, jitter $jitter ms ($samples samples).';
  }

  @override
  String clockSynced(int latency, int samples) {
    return 'The hub\'s clock: declared in step; round trip $latency ms ($samples samples).';
  }

  @override
  String directoryOf(String hub) {
    return 'Directory of $hub';
  }

  @override
  String get directoryNote =>
      'Channels this hub lists for its servers. Unlisted channels are never here; a moderator invites servers to those.';

  @override
  String get directoryEmpty => 'The hub lists no channels.';

  @override
  String get listedChannels => 'Listed channels';

  @override
  String directoryFacts(int servers, String moderators) {
    String _temp0 = intl.Intl.pluralLogic(
      servers,
      locale: localeName,
      other: '$servers servers',
      one: '1 server',
    );
    return '$_temp0 · moderators: $moderators';
  }

  @override
  String get stateMember => 'in it';

  @override
  String get stateRequested => 'asked';

  @override
  String get stateInvited => 'invited';

  @override
  String get joinChannel => 'Join';

  @override
  String get askToJoin => 'Ask to join';

  @override
  String get pendingRequests => 'Waiting for a moderator';

  @override
  String get waitingForModerator => 'asked, not answered yet';

  @override
  String get declinedRequests => 'Declined or removed';

  @override
  String get declinedNote => 'kept here as an archived channel';

  @override
  String joinTitle(String name) {
    return 'Join $name';
  }

  @override
  String askTitle(String name) {
    return 'Ask to join $name';
  }

  @override
  String get joinNote =>
      'A new channel is made here for it. Its messages come from every server in it.';

  @override
  String get askNote =>
      'A moderator of the channel approves or declines. Until then the channel waits here, and if declined it stays as an archived channel.';

  @override
  String get localName => 'Name on this server';

  @override
  String get publicHere => 'Public here';

  @override
  String get privateHere => 'Private here';

  @override
  String get noSection => 'No section';

  @override
  String joinedHubChannel(String name) {
    return 'Joined $name.';
  }

  @override
  String askedToJoin(String name) {
    return 'Asked to join $name.';
  }

  @override
  String get shareToHub => 'Share to a hub';

  @override
  String get shareToHubNote =>
      'The hub will own the channel; other servers on it can join.';

  @override
  String shareToHubTitle(String name) {
    return 'Share $name to a hub';
  }

  @override
  String get noHubsToShare =>
      'This server is on no hub it is connected to. Link it to a hub in the web admin console first.';

  @override
  String get whichHub => 'Which hub';

  @override
  String hubWillOwn(String hub) {
    return '$hub will own this channel.';
  }

  @override
  String get hubOperatorSees =>
      'Its operator can see the channel\'s name, description, listing mode, moderators and which servers are in it, and can admit and remove servers and appoint moderators.';

  @override
  String get hubOperatorCannot =>
      'Its operator cannot read messages, files or anything else inside the channel: the hub never holds its key.';

  @override
  String get hubYouModerate =>
      'You become its first moderator. This server keeps its own levels, pins and slow mode, but no longer decides who is in the channel.';

  @override
  String get listingMode => 'Listing mode';

  @override
  String get listingUnlisted => 'Unlisted';

  @override
  String get listingRequest => 'By request';

  @override
  String get listingOpen => 'Open';

  @override
  String get listingUnlistedNote =>
      'Servers join only when a moderator invites them.';

  @override
  String get listingRequestNote =>
      'Every server on the hub sees it in the directory and may ask to join; a moderator approves or declines.';

  @override
  String get listingOpenNote =>
      'Every server on the hub sees it in the directory and may join without asking.';

  @override
  String get serversToInvite => 'Servers to invite';

  @override
  String get noOtherServers => 'No other server is on this hub yet.';

  @override
  String sharedToHub(String hub) {
    return 'Shared. $hub owns the channel now.';
  }

  @override
  String get onAHub => 'On a hub';

  @override
  String ownedByHub(String hub) {
    return 'Owned by the hub $hub';
  }

  @override
  String get moderatorBadge => 'moderator';

  @override
  String get hubChannelClosed =>
      'Closed: the hub no longer carries this channel.';

  @override
  String nameOnHub(String name) {
    return 'Name on the hub: $name';
  }

  @override
  String memberServers(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count servers',
      one: '1 server',
    );
    return '$_temp0';
  }

  @override
  String moderators(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count moderators',
      one: '1 moderator',
    );
    return '$_temp0';
  }

  @override
  String get remove => 'Remove';

  @override
  String removeServerTitle(String server) {
    return 'Remove $server?';
  }

  @override
  String removeServerBody(String server) {
    return 'The hub takes $server out of the channel, with its moderators. It keeps what it already has and receives nothing new.';
  }

  @override
  String get joinRequests => 'Asking to join';

  @override
  String get approve => 'Approve';

  @override
  String get decline => 'Decline';

  @override
  String get invitedServers => 'Invited';

  @override
  String get withdraw => 'Withdraw';

  @override
  String get moderatorTools => 'Moderator tools';

  @override
  String get inviteServer => 'Invite a server';

  @override
  String get serverNameLabel => 'The server\'s name';

  @override
  String get renameOnHub => 'Rename';

  @override
  String get nameLabel => 'Name';

  @override
  String get describeOnHub => 'Describe';

  @override
  String get descriptionLabel => 'Description';

  @override
  String get addModerator => 'Add a moderator';

  @override
  String get closeChannel => 'Close the channel';

  @override
  String get closeHubChannelTitle => 'Close this channel for every server?';

  @override
  String get closeHubChannelBody =>
      'The hub stops carrying it. Every server keeps what it has. This cannot be undone.';

  @override
  String get deleteForEveryone => 'Delete for everyone';

  @override
  String get deleteForEveryoneTitle => 'Delete this message for everyone?';

  @override
  String get deleteForEveryoneBody =>
      'As a moderator, you delete it on every server in the channel. Each server checks that you are on the hub\'s moderator list.';

  @override
  String get deleteHere => 'Delete here';

  @override
  String get deleteHereTitle => 'Delete this message on this server?';

  @override
  String get deleteHereBody =>
      'You manage this channel here but are not one of its moderators, so it is deleted on this server only. The other servers keep it.';

  @override
  String notifyTitleInChannel(String sender, String channel) {
    return '$sender in #$channel';
  }

  @override
  String notifyTitleInChat(String sender, String chat) {
    return '$sender in $chat';
  }

  @override
  String notifyTitleOnServer(String title, String server) {
    return '$title · $server';
  }

  @override
  String get notifyNewMessage => 'New message';

  @override
  String get notifyLocked => 'You have a new notification';

  @override
  String get appLockTitle => 'Locked';

  @override
  String get appLockUnlock => 'Unlock';

  @override
  String get appLockWaiting =>
      'Touch your passkey, or type your access key, in the browser. Then come back here.';

  @override
  String get appLockCancel => 'Cancel';

  @override
  String get appLockMenu => 'Lock';

  @override
  String get appLockCallTitle => 'Lock and leave the call?';

  @override
  String get appLockCallBody =>
      'Locking ends your part in the call on this device.';

  @override
  String get appLockCallConfirm => 'Lock';

  @override
  String get appLockSettings => 'Lock';

  @override
  String appLockSettingsNote(String keys) {
    return 'Locking hides everything until a passkey or your access key unlocks it, without signing out. Lock from the account menu, with /lock, or with $keys.';
  }

  @override
  String get appLockAuto => 'Lock automatically';

  @override
  String get appLockAutoOff => 'Off';

  @override
  String appLockAutoAfter(int minutes) {
    String _temp0 = intl.Intl.pluralLogic(
      minutes,
      locale: localeName,
      other: 'After $minutes minutes without use',
      one: 'After 1 minute without use',
    );
    return '$_temp0';
  }

  @override
  String get appLockAutoNote =>
      'It locks during a call too, and ends your part in it.';

  @override
  String get appLockInBackground => 'Lock when the app goes to the background';

  @override
  String get notifySentFile => 'Sent a file';

  @override
  String get notifyPostedForm => 'Posted a poll or a form';

  @override
  String get notifySentCard => 'Sent a card';

  @override
  String notifyBodyWithMore(String body, int count) {
    return '$body (+$count more)';
  }

  @override
  String get notifyTestTitle => 'Notifications work';

  @override
  String get notifyTestBody => 'This is how a message from Churr will look.';

  @override
  String notifyCalling(String name) {
    return '$name is calling';
  }

  @override
  String get notifyCallBody => 'Open to join or decline';

  @override
  String notifyMissedCall(String name) {
    return 'Missed call from $name';
  }

  @override
  String notifyNewActivity(String server) {
    return 'New activity on $server';
  }

  @override
  String get notifyChannelDirect => 'Mentions and chats';

  @override
  String get notifyChannelActivity => 'Channel activity';

  @override
  String get notifyChannelCalls => 'Calls';

  @override
  String get notifyChannelService => 'Staying connected';

  @override
  String notifyServiceConnected(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Churr is connected to $count servers',
      one: 'Churr is connected to 1 server',
    );
    return '$_temp0';
  }

  @override
  String get trayOpen => 'Open Churr';

  @override
  String get trayQuit => 'Quit Churr';

  @override
  String get trayDnd => 'Do not disturb';

  @override
  String get trayDndFor30 => 'For 30 minutes';

  @override
  String get trayDndForHour => 'For an hour';

  @override
  String get trayDndTomorrow => 'Until tomorrow morning';

  @override
  String get trayDndUntilOff => 'Until I turn it off';

  @override
  String get trayDndTurnOff => 'Turn off do not disturb';

  @override
  String get trayMute => 'Mute a server';

  @override
  String get trayMuteFor1h => 'For an hour';

  @override
  String get trayMuteFor8h => 'For 8 hours';

  @override
  String get trayMuteForDay => 'For a day';

  @override
  String get trayMuteForWeek => 'For a week';

  @override
  String get trayUnmute => 'Unmute';

  @override
  String trayServerCount(String server, int count) {
    return '$server: $count';
  }

  @override
  String trayServerOffline(String server) {
    return '$server: not connected';
  }

  @override
  String get trayTooltip => 'Churr';

  @override
  String trayTooltipUnread(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Churr: $count unread',
      one: 'Churr: 1 unread',
    );
    return '$_temp0';
  }

  @override
  String get trayTooltipDnd => 'Churr: do not disturb';

  @override
  String get trayTooltipDisconnected => 'Churr: not connected';

  @override
  String get sendTestNotification => 'Send a test notification';

  @override
  String get testNotificationSent =>
      'Sent. If nothing appeared, notifications for Churr are off in the system\'s settings.';

  @override
  String get desktopSection => 'On this computer';

  @override
  String get startAtLogin => 'Start Churr when you log in';

  @override
  String get startAtLoginNote =>
      'The window stays hidden; Churr waits in the tray, connected.';

  @override
  String get closeToTray => 'Closing the window keeps Churr running';

  @override
  String get closeToTrayNote =>
      'Churr stays connected in the tray so notifications keep coming. Quit is in the tray menu.';

  @override
  String get androidPushTitle => 'How this phone hears about new messages';

  @override
  String get androidPushAutomatic => 'Automatic';

  @override
  String get androidPushAutomaticNote =>
      'Google\'s push service where this build has it, else UnifiedPush if a distributor is installed, else only when you open Churr.';

  @override
  String get androidPushUnified => 'UnifiedPush';

  @override
  String get androidPushUnifiedNote =>
      'Through the distributor app you chose. Each server must have UnifiedPush switched on.';

  @override
  String get androidPushStay => 'Stay connected';

  @override
  String get androidPushStayNote =>
      'Keeps a connection open, with a permanent notification. It costs battery, and Android may stop it unless Churr may run in the background.';

  @override
  String get androidPushOff => 'Only when Churr is open';

  @override
  String get androidPushOffNote =>
      'Nothing arrives in the background. Badges are brought up to date when you open the app.';

  @override
  String get androidPushNoDistributor =>
      'No UnifiedPush distributor is installed on this phone.';

  @override
  String androidPushRefused(String server) {
    return '$server does not send this kind of wake-up. It is checked when you open Churr instead.';
  }

  @override
  String get allowBackground => 'Allow running in the background';

  @override
  String get devSecretsInFile =>
      'Development build: secrets are stored unprotected in a file.';

  @override
  String get callAnswer => 'Join';

  @override
  String get callAnswerVideo => 'Join with video';

  @override
  String callBarTitle(String conversation, int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count people',
      one: '1 person',
    );
    return '$conversation: $_temp0 in the call';
  }

  @override
  String get callCameraDevice => 'Camera';

  @override
  String get callCameraOff => 'Turn camera off';

  @override
  String get callCameraOn => 'Turn camera on';

  @override
  String get callCamerasFull =>
      'The most cameras allowed are already on. Your microphone still works.';

  @override
  String get callCannotDecrypt => 'Cannot decrypt this call';

  @override
  String get callConnecting => 'Connecting…';

  @override
  String get callDecline => 'Decline';

  @override
  String callDeclined(String name) {
    return '$name declined.';
  }

  @override
  String get callDevices => 'Devices';

  @override
  String get callDnd => 'Do not disturb';

  @override
  String get callE2E => 'End-to-end encrypted';

  @override
  String get callEarpiece => 'Use the earpiece';

  @override
  String get callEndedElsewhere => 'You joined this call on another device.';

  @override
  String get callEndedEnded => 'The call ended.';

  @override
  String callEndedFailed(String code) {
    return 'The call stopped working ($code).';
  }

  @override
  String get callEndedFull => 'The call is full.';

  @override
  String get callEndedNetwork =>
      'This network blocks calls. You can still read and write.';

  @override
  String get callEndedOff => 'Calls are switched off on this server.';

  @override
  String callEndedRefused(String code) {
    return 'You could not join the call ($code).';
  }

  @override
  String get callEndedRemoved => 'You were removed from the call.';

  @override
  String get callEndedServer => 'The call ended because the server restarted.';

  @override
  String get callEndedSignedOut => 'You were signed out, so you left the call.';

  @override
  String callEndedSummary(int minutes, int people) {
    String _temp0 = intl.Intl.pluralLogic(
      minutes,
      locale: localeName,
      other: 'Call, $minutes minutes',
      one: 'Call, 1 minute',
      zero: 'Call, less than a minute',
    );
    String _temp1 = intl.Intl.pluralLogic(
      people,
      locale: localeName,
      other: '$people people',
      one: '1 person',
    );
    return '$_temp0, $_temp1';
  }

  @override
  String get callEndedTimeout => 'You were disconnected from the call.';

  @override
  String get callExpand => 'Back to the call';

  @override
  String callHostedElsewhere(String server) {
    return 'This call is carried by $server. Your device connects to that server, and its operator can see your device\'s network address.';
  }

  @override
  String callInCallNow(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count people in the call',
      one: '1 person in the call',
      zero: 'Nobody else is here yet',
    );
    return '$_temp0';
  }

  @override
  String callIncomingFrom(String name) {
    return '$name is calling';
  }

  @override
  String get callInIt => 'In the call';

  @override
  String callJoinCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Join, $count people',
      one: 'Join, 1 person',
    );
    return '$_temp0';
  }

  @override
  String get callJoining => 'Joining…';

  @override
  String get callLayoutGrid => 'Show everyone the same size';

  @override
  String get callLayoutSpeaker => 'Show the speaker large';

  @override
  String get callLeave => 'Leave the call';

  @override
  String get callMicrophoneDevice => 'Microphone';

  @override
  String get callMinimize => 'Keep the call going and use the app';

  @override
  String callMissedFrom(String name) {
    return 'Missed call from $name';
  }

  @override
  String get callMutedMark => 'microphone off';

  @override
  String get callMuteForMe => 'Mute for me';

  @override
  String get callMuteMic => 'Turn microphone off';

  @override
  String get callOutputDevice => 'Speaker';

  @override
  String get callPeople => 'People in the call';

  @override
  String get callPickScreen => 'Choose what to share';

  @override
  String get callPin => 'Pin';

  @override
  String get callPinned => 'pinned';

  @override
  String get callPreJoin => 'Join the call';

  @override
  String callProblem(String code) {
    return 'Something went wrong in the call ($code).';
  }

  @override
  String get callReconnecting => 'Reconnecting…';

  @override
  String get callRinging => 'Ringing';

  @override
  String callScreenInUse(String name) {
    return '$name is already sharing a screen.';
  }

  @override
  String callScreenOf(String name) {
    return '$name\'s screen';
  }

  @override
  String get callScreens => 'Screens';

  @override
  String get callSealedNoKeys =>
      'This device cannot join this sealed chat\'s call: it does not hold the chat\'s call keys.';

  @override
  String get callSealedUnavailable =>
      'This device cannot take part in this sealed chat\'s call.';

  @override
  String get callShareScreen => 'Share your screen';

  @override
  String get callSomeone => 'Someone';

  @override
  String get callSpeaker => 'Use the loudspeaker';

  @override
  String get callSpeakingMark => 'speaking';

  @override
  String get callStart => 'Start a call';

  @override
  String callStartedBy(String name) {
    return '$name started a call';
  }

  @override
  String get callStopShare => 'Stop sharing';

  @override
  String get callSwitchCamera => 'Switch camera';

  @override
  String get callUnmuteForMe => 'Unmute for me';

  @override
  String get callUnmuteMic => 'Turn microphone on';

  @override
  String get callUnpin => 'Unpin';

  @override
  String get callVideoPaused => 'Video paused';

  @override
  String get callWindows => 'Windows';

  @override
  String get callWithCamera => 'Camera on';

  @override
  String get callWithMic => 'Microphone on';

  @override
  String get callYou => 'You';

  @override
  String callMore(int count) {
    return '+$count';
  }

  @override
  String get back => 'Back';

  @override
  String get sealedChatTitle => 'Sealed chat';

  @override
  String get sealedNotRunning =>
      'Sealed chats are not running for this server on this device.';

  @override
  String get sealedEncrypted => 'End-to-end encrypted';

  @override
  String get sealedChatMenu => 'Sealed chat menu';

  @override
  String get sealedCompareCodes => 'Compare codes';

  @override
  String get sealedChatsAndDevices => 'Sealed chats and devices';

  @override
  String get sealedLockedHere =>
      'This device is locked. Sealed chats are listed here but cannot be read.';

  @override
  String get sealedUnlock => 'Unlock';

  @override
  String sealedReset(String name) {
    return '$name\'s sealed-chat identity was reset. Messages from now on go to new devices. If you did not expect this, compare codes before you send anything private.';
  }

  @override
  String get sealedAcknowledge => 'I understand';

  @override
  String sealedBrowserAmong(String name) {
    return '$name also uses Churr in a browser. A browser runs whatever code the server sends each time it loads.';
  }

  @override
  String sealedWaiting(String name) {
    return 'Earlier messages will appear when $name\'s device is next online.';
  }

  @override
  String sealedUnchecked(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count messages could not be checked.',
      one: '1 message could not be checked.',
    );
    return '$_temp0';
  }

  @override
  String get sealedOutOfOrder => 'The server delivered this out of order.';

  @override
  String sealedReplyingTo(String name) {
    return 'Replying to $name';
  }

  @override
  String get sealedVerified => 'Verified';

  @override
  String get sealedEmpty =>
      'Nothing here yet. Only the two of you can read what is written here.';

  @override
  String sealedMessageHint(String name) {
    return 'Sealed message to $name';
  }

  @override
  String get sealedRemovedHere =>
      'Another of your devices removed this one from sealed chats. Its sealed keys and messages were wiped.';

  @override
  String get sealedSetUpAgain => 'Set up again';

  @override
  String get sealedFileDownload => 'Save';

  @override
  String get sealedFileFailed =>
      'The file did not check out and was not saved.';

  @override
  String get sealedPlatformAndroid => 'Android app';

  @override
  String get sealedPlatformIos => 'iPhone app';

  @override
  String get sealedPlatformMac => 'Mac app';

  @override
  String get sealedPlatformWindows => 'Windows app';

  @override
  String get sealedPlatformLinux => 'Linux app';

  @override
  String get sealedPlatformBrowser => 'a browser';

  @override
  String sealedNoticeAdded(String name, String kind) {
    return '$name added a device: $kind.';
  }

  @override
  String sealedNoticeAddedMine(String kind) {
    return 'A device was added to your account: $kind.';
  }

  @override
  String sealedNoticeRemoved(String name) {
    return '$name removed a device.';
  }

  @override
  String get sealedNoticeRemovedMine =>
      'A device was removed from your account.';

  @override
  String sealedNoticeKey(String name) {
    return '$name\'s security key changed. Their old key signed the change.';
  }

  @override
  String get sealedNoticeKeyMine =>
      'Your account\'s security key changed. Your old key signed the change.';

  @override
  String get sealedNoticeResetMine => 'Your sealed-chat identity was reset.';

  @override
  String get sealedNoticeListRefused =>
      'The server sent a device list that does not check out.';

  @override
  String get sealedNoticeOutOfStep => 'This chat\'s keys are out of step.';

  @override
  String get unlockTitle => 'Unlock sealed chats';

  @override
  String get unlockIntro =>
      'This device is signed in but cannot read sealed chats yet. Choose a way to unlock it.';

  @override
  String get unlockPasskey => 'Use a passkey';

  @override
  String get unlockPasskeyNote =>
      'A passkey only works on a page served from the server\'s own address. So unlocking with a passkey passes your key through a page the server supplies, in your browser, in this app as in the web chat. The page hands the key back to this app sealed to this app\'s own key; after that, this app encrypts and decrypts in its own code.';

  @override
  String get unlockOpenPage => 'Open the unlock page';

  @override
  String get unlockCheckAgain => 'I have finished';

  @override
  String get unlockPasskeyNotYet =>
      'The unlock page has not unlocked this device yet.';

  @override
  String get unlockPhrase => 'Recovery phrase';

  @override
  String get unlockPhraseNote =>
      'Type your twelve words. They stay in this app and do not pass through any server page.';

  @override
  String get unlockPhraseField => 'Twelve words';

  @override
  String get unlockAnotherDevice => 'Another device';

  @override
  String get unlockAnotherNote =>
      'Show this code to a device of yours that is already unlocked: scan it there, or type its 20 characters. It is long on purpose: it is the only thing that stops a server swapping in keys of its own.';

  @override
  String get unlockShowCode => 'Show the code';

  @override
  String get unlockCodeQr => 'Approval code';

  @override
  String get unlockWaiting =>
      'Waiting for approval. The code lasts ten minutes.';

  @override
  String get unlockNewRequest => 'Make a new code';

  @override
  String get unlockCarryOn => 'Carry on locked';

  @override
  String get unlockCarryOnNote =>
      'Sealed chats are listed and cannot be read on this device.';

  @override
  String get unlockStartAgain => 'Start again';

  @override
  String get unlockStartAgainWarning =>
      'Only for when you have no passkey that can unlock, no recovery phrase and no other device. Earlier sealed messages can never be read again on any device of yours, and the other person in each sealed chat is shown that your keys were reset.';

  @override
  String unlockFailed(String why) {
    return 'That did not unlock this device. ($why)';
  }

  @override
  String get sealedFirstChat =>
      'This is your first sealed chat: it makes your recovery phrase, twelve words shown once.';

  @override
  String get sealedOtherWaysNote =>
      'The recovery phrase and approval from another of your devices do not pass through any server page.';

  @override
  String get sealedMakeBackup => 'Make my recovery phrase';

  @override
  String get phraseTitle => 'Your recovery phrase';

  @override
  String get phraseIntro =>
      'Write these twelve words down and keep them somewhere safe. They are shown once, never sent to the server. With them you can read your sealed chats on a new device; without them, and with no passkey or other device that can unlock, earlier sealed messages are lost.';

  @override
  String get phraseWrittenDown => 'I have written them down';

  @override
  String get phraseCheckIntro => 'Check two of the words before going on.';

  @override
  String phraseCheck(int n) {
    return 'Word $n';
  }

  @override
  String get phraseCheckWrong =>
      'Those words do not match. Look at what you wrote down, or go back.';

  @override
  String get phraseContinue => 'Continue';

  @override
  String approveTitle(String kind) {
    return 'Unlock a new device ($kind)?';
  }

  @override
  String approveSignedIn(String name, String when) {
    return '$name, signed in $when';
  }

  @override
  String get approveWhyLong =>
      'Scan the code the new device shows, or type its 20 characters. It is long on purpose: it is the only thing that stops a server swapping in keys of its own.';

  @override
  String get approveCodeField => 'The 20 characters it shows';

  @override
  String get approveScan => 'Scan its code';

  @override
  String get approveYes => 'Unlock it';

  @override
  String get approveNotNow => 'Not now';

  @override
  String get approveWrongCode =>
      'That code does not match this request. The request may have been tampered with.';

  @override
  String get approveEndSession => 'End that device\'s session';

  @override
  String get sealedStatusUnlocked => 'This device can read your sealed chats.';

  @override
  String get sealedStatusLocked =>
      'This device is locked: it cannot read sealed chats.';

  @override
  String get sealedStatusNoBackup =>
      'You have no sealed chats yet. Your first one makes your recovery phrase.';

  @override
  String get sealedRequestsTitle => 'Devices asking to be unlocked';

  @override
  String get sealedYourDevices => 'Your devices';

  @override
  String get sealedBrowserTag => 'browser';

  @override
  String get sealedThisDevice => 'this device';

  @override
  String get sealedNotListed => 'not in sealed chats';

  @override
  String sealedLastSeen(String when) {
    return 'Last seen $when';
  }

  @override
  String get sealedRemove => 'Remove';

  @override
  String sealedRemoveTitle(String name) {
    return 'Remove $name?';
  }

  @override
  String get sealedRemoveBody =>
      'Its session ends and it is taken out of your sealed chats. This app then replaces your backup key and security key, because that device held both, and starts new keys in each chat. The other person is shown that your key changed.';

  @override
  String get sealedRemoved =>
      'Removed. Your backup key and security key were replaced.';

  @override
  String get sealedPasskeys => 'Passkeys';

  @override
  String get passkeyCanUnlock => 'can unlock sealed chats';

  @override
  String get passkeyCannotUnlock => 'cannot unlock sealed chats';

  @override
  String get passkeyWaiting => 'waiting for your approval to unlock';

  @override
  String passkeyApproveTitle(String name) {
    return 'Let the passkey “$name” unlock sealed chats?';
  }

  @override
  String get passkeyApproveBody =>
      'Say yes only for a passkey you have just added. Its key comes through the server, which could offer a key of its own under that name.';

  @override
  String get passkeyApproveYes => 'Let it unlock';

  @override
  String get sealedReplaceKeys => 'Replace my backup key and security key';

  @override
  String get sealedReplaced =>
      'Your backup key and security key were replaced.';

  @override
  String compareIntro(String name) {
    return 'You and $name see the same 60 digits if and only if nobody stands between you. Compare them in person or on a call you trust, or scan the other code.';
  }

  @override
  String get compareDigitsLabel => 'Comparison code';

  @override
  String get compareProves =>
      'A match proves that your two devices hold each other\'s true account keys, so the devices reading this chat are ones you two added. It proves nothing about the code a browser is running, or about who is holding the other phone.';

  @override
  String compareMark(String name) {
    return 'They match: mark $name verified';
  }

  @override
  String compareMarked(String name) {
    return '$name is marked verified on this device. The mark is not synced; a new device starts without it.';
  }

  @override
  String get compareScan => 'Scan their code';

  @override
  String get compareScanMatch => 'The codes match.';

  @override
  String get compareScanNoMatch =>
      'The codes do not match. Do not mark them verified.';

  @override
  String get compareNotYet =>
      'The code appears once both device lists are known on this device.';

  @override
  String get newSealedTitle => 'New sealed chat';

  @override
  String get newSealedIntro =>
      'A sealed chat is between two people only, and nobody can be added. It is encrypted on your devices: the server cannot read it, and it is never searched or exported. If you lose every way to unlock, its history is lost.';

  @override
  String get newSealedLocked => 'Unlock this device first.';

  @override
  String get newSealedPick => 'Who with?';

  @override
  String reminderSet(String when) {
    return 'Reminder set for $when.';
  }

  @override
  String timedOutUntil(String time) {
    return 'You are timed out in this channel until $time. You can read, but not post.';
  }

  @override
  String slowModeWait(String time) {
    return 'Slow mode is on. You can post again in $time.';
  }

  @override
  String get channelCardPublic => 'Public channel';

  @override
  String get channelCardPrivate => 'Private channel';

  @override
  String get channelCardLocked => 'Locked channel';

  @override
  String get channelCardArchived => 'archived';

  @override
  String channelCardMembers(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count members',
      one: '1 member',
    );
    return '$_temp0';
  }

  @override
  String channelCardUnder(String parent) {
    return 'Under #$parent';
  }

  @override
  String get channelCardOpen => 'Open the channel';

  @override
  String get channelCardView => 'View the channel';

  @override
  String channelCardLabel(String name) {
    return 'About the channel $name';
  }

  @override
  String get searchTitle => 'Search';

  @override
  String get searchInWhich => 'In which conversation?';

  @override
  String get searchFromWhom => 'From whom?';

  @override
  String get searchHint =>
      'Words, \"a phrase\", depl*, -not, from:@sam, since:2026-11-01, has:file';

  @override
  String get searchAllServers => 'All servers';

  @override
  String get searchIn => 'In…';

  @override
  String searchInNamed(String name) {
    return 'In $name';
  }

  @override
  String get searchFrom => 'From…';

  @override
  String searchFromNamed(String name) {
    return 'From $name';
  }

  @override
  String get searchSince => 'Since…';

  @override
  String searchSinceDate(String date) {
    return 'Since $date';
  }

  @override
  String get searchUntil => 'Until…';

  @override
  String searchUntilDate(String date) {
    return 'Until $date';
  }

  @override
  String get searchHasFile => 'Has a file';

  @override
  String get searchNoSealed =>
      'Sealed chats are never searched, on any server.';

  @override
  String searchCouldNot(String server) {
    return 'Could not search $server';
  }

  @override
  String searchWaiting(String servers) {
    return 'Still waiting for $servers…';
  }

  @override
  String get searchNothing => 'Nothing found.';

  @override
  String get searchEmpty => 'Search messages on every server.';

  @override
  String get searchMore => 'More';

  @override
  String get searchAConversation => 'a conversation';

  @override
  String searchHitLabel(String who, String where, String when, String text) {
    return '$who in $where, $when: $text';
  }

  @override
  String searchHitLabelServer(
    String who,
    String where,
    String when,
    String server,
    String text,
  ) {
    return '$who in $where, $when, $server: $text';
  }

  @override
  String get inAThread => 'in a thread';

  @override
  String get filter => 'Filter';

  @override
  String get goTo => 'Go to';

  @override
  String get switcherHint => 'Find a conversation or a person';

  @override
  String get nothingMatches => 'Nothing matches.';

  @override
  String get sealedChat => 'Sealed chat';

  @override
  String get chat => 'Chat';

  @override
  String get hiddenTag => 'hidden';

  @override
  String get whichServer => 'Which server?';

  @override
  String get refusalLocked =>
      'This channel is locked: nobody can be added or join.';

  @override
  String get refusalArchived => 'This channel is archived.';

  @override
  String get refusalForbidden => 'You may not do that here.';

  @override
  String get refusalTooLarge =>
      'An ad-hoc channel holds at most 50 people. Make a named channel instead.';

  @override
  String browseTitle(String server) {
    return 'Browse channels · $server';
  }

  @override
  String get browseFilterHint => 'Filter by name or topic';

  @override
  String get browseIncludeArchived => 'Include archived channels';

  @override
  String get browseNone => 'No channels.';

  @override
  String get archivedTag => 'archived';

  @override
  String get sameNameAsAnother => 'same name as another';

  @override
  String get open => 'Open';

  @override
  String get join => 'Join';

  @override
  String subchannelsOf(String name) {
    return 'Sub-channels of $name';
  }

  @override
  String get thisChannel => 'this channel';

  @override
  String get noPublicSubchannels => 'There are no public sub-channels to join.';

  @override
  String get joiningDoesNotJoinSubs =>
      'Joining a channel does not join its sub-channels. Pick any you want.';

  @override
  String memberCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count members',
      one: '1 member',
    );
    return '$_temp0';
  }

  @override
  String get joinSelected => 'Join selected';

  @override
  String get newMenuTitle => 'New';

  @override
  String get newChannelOption => 'Channel';

  @override
  String newChannelTitle(String server) {
    return 'New channel · $server';
  }

  @override
  String get newSubchannelTitle => 'New sub-channel';

  @override
  String get channelNameLabel => 'Name';

  @override
  String get channelNameHelp =>
      'Any words, in any script. Spaces and capitals are fine.';

  @override
  String get sameNameOne =>
      'A channel you can see already has this name. You can still use it.';

  @override
  String sameNameMany(int count) {
    return '$count channels you can see already have this name. You can still use it.';
  }

  @override
  String get channelTopicOptional => 'Topic (optional)';

  @override
  String get channelUnder => 'Under';

  @override
  String get channelTopLevel => 'Nothing (top level)';

  @override
  String get subchannelExplained =>
      'A sub-channel is listed under its parent and keeps its own members and settings.';

  @override
  String get channelSection => 'Section';

  @override
  String get none => 'None';

  @override
  String get channelPrivate => 'Private';

  @override
  String get channelPrivateNote =>
      'Only people who are added can find and read it. A private channel can never be made public.';

  @override
  String get channelAnnouncements => 'Announcements only';

  @override
  String get channelAnnouncementsNote =>
      'Only its managers and the people they allow post. Everyone else reads and reacts.';

  @override
  String newChatTitle(String server) {
    return 'New chat · $server';
  }

  @override
  String get openChat => 'Open chat';

  @override
  String get chatHistoryNotice =>
      'Anyone added to a chat later sees its whole history.';

  @override
  String get findPeople => 'Find people';

  @override
  String botTooOld(String bot) {
    return '$bot asked something this app is too old to show.';
  }

  @override
  String get aBot => 'A bot';

  @override
  String get aBotAsks => 'A bot asks';

  @override
  String get onlyYouSee => 'Only you can see this.';

  @override
  String fromBotOnlyYou(String bot) {
    return 'From $bot. Only you can see this.';
  }

  @override
  String get botNotConnected => 'The bot is not connected. Try again later.';

  @override
  String get answer => 'Answer';

  @override
  String get pickADate => 'Pick a date';

  @override
  String get pickATime => 'Pick a time';

  @override
  String get choose => 'Choose…';

  @override
  String get aChannel => 'A channel';

  @override
  String get typeAName => 'Type a name';

  @override
  String get typeAChannelName => 'Type a channel name';

  @override
  String formNotPosted(String reason) {
    return 'The form was not posted. $reason';
  }

  @override
  String get newPoll => 'New poll';

  @override
  String get newRsvp => 'New RSVP';

  @override
  String get newSignup => 'New sign-up sheet';

  @override
  String get askApproval => 'Ask for approval';

  @override
  String get newForm => 'New form';

  @override
  String get pick => 'Pick';

  @override
  String noValue(String label) {
    return 'No $label';
  }

  @override
  String get question => 'Question';

  @override
  String get titleOptional => 'Title (optional)';

  @override
  String get what => 'What';

  @override
  String optionN(int n) {
    return 'Option $n';
  }

  @override
  String slotN(int n) {
    return 'Slot $n';
  }

  @override
  String get slotPlacesHelp => 'Add places in brackets: Morning (3)';

  @override
  String get addOption => 'Add an option';

  @override
  String get addSlot => 'Add a slot';

  @override
  String get pickMoreThanOne => 'People may pick more than one';

  @override
  String get anonymous => 'Anonymous';

  @override
  String get pollAnonymousNote =>
      'Nobody, not even you, can see who voted for what. This cannot be changed later.';

  @override
  String get mayAddOptions => 'People may add options';

  @override
  String get showResults => 'Show results';

  @override
  String get resultsLive => 'Live';

  @override
  String get resultsAfterAnswer => 'After answering';

  @override
  String get resultsAfterClose => 'After closing';

  @override
  String get when => 'When';

  @override
  String get whoMayAnswer => 'Who may answer';

  @override
  String get anyoneHere => 'Anyone here';

  @override
  String get closingTime => 'Closing time';

  @override
  String get post => 'Post';

  @override
  String get fieldOneLine => 'One line';

  @override
  String get fieldSeveralLines => 'Several lines';

  @override
  String get fieldChoice => 'Choice';

  @override
  String get fieldNumber => 'Number';

  @override
  String get fieldDate => 'Date';

  @override
  String get fieldTime => 'Time';

  @override
  String get fieldPeople => 'People';

  @override
  String get fieldChannels => 'Channels';

  @override
  String get fieldTickBox => 'Tick-box';

  @override
  String fieldN(int n) {
    return 'Field $n';
  }

  @override
  String get removeField => 'Remove field';

  @override
  String get fieldLabel => 'Label';

  @override
  String get helpOptional => 'Help (optional)';

  @override
  String get optionsPerLine => 'Options, one per line';

  @override
  String get required => 'Required';

  @override
  String get moreThanOne => 'More than one';

  @override
  String get addField => 'Add a field';

  @override
  String buttonN(int n) {
    return 'Button $n';
  }

  @override
  String get buttonPlain => 'Plain';

  @override
  String get buttonMain => 'Main';

  @override
  String get buttonDanger => 'Danger';

  @override
  String get removeButton => 'Remove button';

  @override
  String get addButton => 'Add a button';

  @override
  String get formAnonymousNote =>
      'Answers carry no names, for anyone. This cannot be changed later.';

  @override
  String get formAnswersGo =>
      'Answers go to you and the channel\'s managers, and can be downloaded.';

  @override
  String get channel => 'Channel';

  @override
  String get levelReader => 'Reader';

  @override
  String get levelMember => 'Member';

  @override
  String get levelManager => 'Manager';

  @override
  String makeLevel(String level) {
    return 'Make $level';
  }

  @override
  String makeGroupLevel(String level) {
    return 'Make ${level}s';
  }

  @override
  String get off => 'Off';

  @override
  String get seconds10 => '10 seconds';

  @override
  String get seconds30 => '30 seconds';

  @override
  String get minute1 => '1 minute';

  @override
  String get minutes5 => '5 minutes';

  @override
  String get minutes15 => '15 minutes';

  @override
  String get hour1 => '1 hour';

  @override
  String get retentionServer => 'The server\'s period';

  @override
  String get day1 => '1 day';

  @override
  String get days7 => '7 days';

  @override
  String get days30 => '30 days';

  @override
  String get days90 => '90 days';

  @override
  String get year1 => '1 year';

  @override
  String get noLongerInConversation =>
      'You are no longer in this conversation.';

  @override
  String get tabAbout => 'About';

  @override
  String tabMembers(int count) {
    return 'Members ($count)';
  }

  @override
  String get tabPins => 'Pins';

  @override
  String get nameInUse => 'That name is in use';

  @override
  String get nameInUseNote =>
      'Channels you can see already have this name. Names need not be unique.';

  @override
  String get useItAnyway => 'Use it anyway';

  @override
  String get parentChannel => 'Parent channel';

  @override
  String get parentChangeNote =>
      'This changes where the channel is listed and nothing else: not who is in it, not whether it is private.';

  @override
  String get noneTopLevel => 'None (top level)';

  @override
  String typeToConfirm(String word) {
    return 'Type \"$word\" to confirm.';
  }

  @override
  String get makePrivateTitle => 'Make this channel private?';

  @override
  String get makePrivateLine1 =>
      'People who are not members lose the channel at once: its history and its search results go with it.';

  @override
  String get makePrivateLine2 =>
      'A private channel can never be made public again.';

  @override
  String get makePrivate => 'Make private';

  @override
  String get lockTitle => 'Lock this channel for good?';

  @override
  String get lockLine1 =>
      'Nobody can be added or join, and a group attached to the channel brings in nobody new. The people here now stay.';

  @override
  String get lockLine2 =>
      'People can still leave, and someone who leaves can never come back.';

  @override
  String get lockLine3 => 'This cannot be undone.';

  @override
  String get lockWord => 'lock';

  @override
  String get lock => 'Lock';

  @override
  String get unarchiveTitle => 'Bring this channel back?';

  @override
  String get archiveTitle => 'Archive this channel?';

  @override
  String get unarchiveNote => 'People can post here again.';

  @override
  String get archiveNote =>
      'An archived channel can still be read and searched, and takes no new posts.';

  @override
  String get unarchive => 'Unarchive';

  @override
  String get archive => 'Archive';

  @override
  String get cannotLeaveTitle => 'You cannot leave on your own';

  @override
  String get cannotLeaveNote =>
      'You are here through a group. Hide the channel or set it to notify nothing, or leave the group if it is open.';

  @override
  String get ok => 'OK';

  @override
  String get leaveTitle => 'Leave this channel?';

  @override
  String get leaveLocked =>
      'This channel is locked: once you leave, there is no way back.';

  @override
  String get leavePrivate => 'You will need to be added again to come back.';

  @override
  String get leaveLast =>
      'You are the last person here. The channel and everything in it will be deleted.';

  @override
  String get leave => 'Leave';

  @override
  String get requestWebhook => 'Request a webhook';

  @override
  String get webhookNote =>
      'An admin (or a manager, if allowed) approves it. Its messages appear under the name you give.';

  @override
  String get webhookPurpose => 'What it is for';

  @override
  String get request => 'Request';

  @override
  String get webhookRequested => 'Requested. You will hear when it is decided.';

  @override
  String get webhookTooMany => 'You have three requests waiting already.';

  @override
  String chatPeople(int count) {
    return 'Chat · $count people';
  }

  @override
  String get lockedNote =>
      'Locked: nobody can be added or join, and someone who leaves cannot come back.';

  @override
  String get archivedNote =>
      'Archived: it can be read and searched, and takes no posts.';

  @override
  String get nameChatNote =>
      'Name (naming a chat makes it a named private channel)';

  @override
  String get topic => 'Topic';

  @override
  String get welcomeText => 'Welcome text (Churr Markdown, shown on joining)';

  @override
  String get peopleJoinAs => 'People join as';

  @override
  String get joinAsMembers => 'Members';

  @override
  String get joinAsReaders => 'Readers (announcements)';

  @override
  String get slowMode => 'Slow mode';

  @override
  String get keepMessagesFor => 'Keep messages for';

  @override
  String get subInParentSection =>
      'A sub-channel is listed in its parent\'s section.';

  @override
  String get subchannels => 'Sub-channels';

  @override
  String get subchannelsNote => 'Public sub-channels you could join';

  @override
  String get makePrivateNote =>
      'People who are not members lose it at once. There is no way back to public.';

  @override
  String get lockNote =>
      'Nobody can be added or join again. This cannot be undone.';

  @override
  String get throughGroup => 'You are here through a group.';

  @override
  String get addPeople => 'Add people';

  @override
  String get addAs => 'Add as';

  @override
  String get add => 'Add';

  @override
  String get addedSeesHistoryChat =>
      'Anyone added to a chat sees its whole history.';

  @override
  String get addedSeesHistory => 'Whoever is added sees the whole history.';

  @override
  String get addGroup => 'Add a group';

  @override
  String get addGroupNote =>
      'Everyone in the group is in the channel at that level, and stays in step as the group changes.';

  @override
  String get pickGroup => 'Pick a group';

  @override
  String get removeViaGroup =>
      'They are here through a group: remove the group, or take them out of it.';

  @override
  String removeName(String name) {
    return 'Remove $name?';
  }

  @override
  String get them => 'them';

  @override
  String get removeLocked =>
      'This channel is locked: they will not be able to come back.';

  @override
  String get membersLocked => 'This channel is locked, so nobody can be added.';

  @override
  String get groupsHeading => 'GROUPS';

  @override
  String get peopleHeading => 'PEOPLE';

  @override
  String get group => 'group';

  @override
  String get groupOptions => 'Group options';

  @override
  String get removeGroup => 'Remove group';

  @override
  String get hereThroughGroup => 'here through a group';

  @override
  String get alsoThroughGroup => 'also through a group';

  @override
  String get memberOptions => 'Member options';

  @override
  String get nothingPinned => 'Nothing is pinned here.';

  @override
  String get aMessage => 'A message';

  @override
  String pinnedBy(String name) {
    return 'pinned by $name';
  }

  @override
  String pinnedByAt(String name, String when) {
    return 'pinned by $name · $when';
  }

  @override
  String get someoneLower => 'someone';

  @override
  String reconnectingIn(int seconds) {
    String _temp0 = intl.Intl.pluralLogic(
      seconds,
      locale: localeName,
      other: '$seconds seconds',
      one: '1 second',
    );
    return 'Reconnecting in $_temp0';
  }

  @override
  String get notConnectedBanner =>
      'Not connected. Messages you send will go when it is back.';

  @override
  String reconnectingBanner(int seconds) {
    String _temp0 = intl.Intl.pluralLogic(
      seconds,
      locale: localeName,
      other: '$seconds seconds',
      one: '1 second',
    );
    return 'Not connected. Reconnecting in $_temp0. Messages you send will go when it is back.';
  }

  @override
  String get broadGoesPlain =>
      'Only managers can use @channel, @here and @everyone here. This will be sent as plain text and will notify nobody.';

  @override
  String get broadWentPlain =>
      'Only managers can use @channel, @here and @everyone here. Your message was sent with them as plain text and notified nobody.';

  @override
  String get broadConfirmTitle => 'Notify everyone?';

  @override
  String broadConfirmBody(int count) {
    final intl.NumberFormat countNumberFormat =
        intl.NumberFormat.decimalPattern(localeName);
    final String countString = countNumberFormat.format(count);

    return 'This will notify $countString people.';
  }

  @override
  String get broadConfirmSend => 'Send';

  @override
  String get broadConfirmCancel => 'Cancel';

  @override
  String get ignoreBroadMentions => 'Ignore @channel, @here and @everyone';

  @override
  String get ignoreBroadMentionsHint =>
      'They will not notify you here. A mention of you by name, or of another group you are in, still does.';

  @override
  String get broadMentionsSetting => '@channel, @here and @everyone';

  @override
  String get broadMentionsSettingHint =>
      'Who may notify the whole channel. The server lets anyone, until a channel is larger than the size its admins set; then only managers. Anyone else\'s is posted as plain text.';

  @override
  String get broadMentionsServer => 'Follow the server';

  @override
  String get broadMentionsAnyone => 'Anyone';

  @override
  String get broadMentionsManagers => 'Managers only';

  @override
  String get cardSuitAcorns => 'acorns';

  @override
  String get cardSuitBamboo => 'bamboo';

  @override
  String get cardSuitBatons => 'clubs';

  @override
  String get cardSuitBells => 'bells';

  @override
  String get cardSuitCharacters => 'characters';

  @override
  String get cardSuitCherry => 'cherry blossom';

  @override
  String get cardSuitChrysanthemum => 'chrysanthemum';

  @override
  String get cardSuitClover => 'bush clover';

  @override
  String get cardSuitClubs => 'clubs';

  @override
  String get cardSuitCoins => 'coins';

  @override
  String get cardSuitCups => 'cups';

  @override
  String get cardSuitDiamonds => 'diamonds';

  @override
  String get cardSuitDots => 'dots';

  @override
  String get cardSuitGheart => 'hearts';

  @override
  String get cardSuitHearts => 'hearts';

  @override
  String get cardSuitIris => 'iris';

  @override
  String get cardSuitLeaves => 'leaves';

  @override
  String get cardSuitMaple => 'maple';

  @override
  String get cardSuitPampas => 'pampas grass';

  @override
  String get cardSuitPaulownia => 'paulownia';

  @override
  String get cardSuitPentacles => 'pentacles';

  @override
  String get cardSuitPeony => 'peony';

  @override
  String get cardSuitPine => 'pine';

  @override
  String get cardSuitPlum => 'plum blossom';

  @override
  String get cardSuitRoses => 'roses';

  @override
  String get cardSuitShields => 'shields';

  @override
  String get cardSuitSpades => 'spades';

  @override
  String get cardSuitSwords => 'swords';

  @override
  String get cardSuitWands => 'wands';

  @override
  String get cardSuitWillow => 'willow';

  @override
  String get cardSuitWisteria => 'wisteria';

  @override
  String get cardRankAce => 'ace';

  @override
  String get cardRankBanner => 'banner';

  @override
  String get cardRankCaballo => 'caballo';

  @override
  String get cardRankCavallo => 'cavallo';

  @override
  String get cardRankFante => 'fante';

  @override
  String get cardRankJack => 'jack';

  @override
  String get cardRankKing => 'king';

  @override
  String get cardRankKnight => 'knight';

  @override
  String get cardRankOber => 'ober';

  @override
  String get cardRankPage => 'page';

  @override
  String get cardRankQueen => 'queen';

  @override
  String get cardRankRe => 're';

  @override
  String get cardRankRey => 'rey';

  @override
  String get cardRankSota => 'sota';

  @override
  String get cardRankUnder => 'under';

  @override
  String get cardRankUnter => 'unter';

  @override
  String get cardExtraMajor0 => 'The Fool';

  @override
  String get cardExtraMajor1 => 'The Magician';

  @override
  String get cardExtraMajor2 => 'The High Priestess';

  @override
  String get cardExtraMajor3 => 'The Empress';

  @override
  String get cardExtraMajor4 => 'The Emperor';

  @override
  String get cardExtraMajor5 => 'The Hierophant';

  @override
  String get cardExtraMajor6 => 'The Lovers';

  @override
  String get cardExtraMajor7 => 'The Chariot';

  @override
  String get cardExtraMajor8 => 'Strength';

  @override
  String get cardExtraMajor9 => 'The Hermit';

  @override
  String get cardExtraMajor10 => 'Wheel of Fortune';

  @override
  String get cardExtraMajor11 => 'Justice';

  @override
  String get cardExtraMajor12 => 'The Hanged Man';

  @override
  String get cardExtraMajor13 => 'Death';

  @override
  String get cardExtraMajor14 => 'Temperance';

  @override
  String get cardExtraMajor15 => 'The Devil';

  @override
  String get cardExtraMajor16 => 'The Tower';

  @override
  String get cardExtraMajor17 => 'The Star';

  @override
  String get cardExtraMajor18 => 'The Moon';

  @override
  String get cardExtraMajor19 => 'The Sun';

  @override
  String get cardExtraMajor20 => 'Judgement';

  @override
  String get cardExtraMajor21 => 'The World';

  @override
  String get cardExtraWindEast => 'east wind';

  @override
  String get cardExtraWindSouth => 'south wind';

  @override
  String get cardExtraWindWest => 'west wind';

  @override
  String get cardExtraWindNorth => 'north wind';

  @override
  String get cardExtraDragonRed => 'red dragon';

  @override
  String get cardExtraDragonGreen => 'green dragon';

  @override
  String get cardExtraDragonWhite => 'white dragon';

  @override
  String get cardRank1 => 'one';

  @override
  String get cardRank2 => 'two';

  @override
  String get cardRank3 => 'three';

  @override
  String get cardRank4 => 'four';

  @override
  String get cardRank5 => 'five';

  @override
  String get cardRank6 => 'six';

  @override
  String get cardRank7 => 'seven';

  @override
  String get cardRank8 => 'eight';

  @override
  String get cardRank9 => 'nine';

  @override
  String get cardRank10 => 'ten';

  @override
  String get cardRankAnimal => 'animal';

  @override
  String get cardRankBright => 'bright';

  @override
  String get cardRankChaff1 => 'chaff';

  @override
  String get cardRankChaff2 => 'chaff';

  @override
  String get cardRankChaff3 => 'chaff';

  @override
  String get cardRankRibbon => 'ribbon';

  @override
  String get cardExtraJokerRed => 'red joker';

  @override
  String get cardExtraJokerBlack => 'black joker';

  @override
  String get cardExtraJokerWhite => 'white joker';

  @override
  String get cardExtraFlowerPlum => 'plum flower';

  @override
  String get cardExtraFlowerOrchid => 'orchid flower';

  @override
  String get cardExtraFlowerChrysanthemum => 'chrysanthemum flower';

  @override
  String get cardExtraFlowerBamboo => 'bamboo flower';

  @override
  String get cardExtraSeasonSpring => 'spring';

  @override
  String get cardExtraSeasonSummer => 'summer';

  @override
  String get cardExtraSeasonAutumn => 'autumn';

  @override
  String get cardExtraSeasonWinter => 'winter';

  @override
  String get tarotUpMajor0 =>
      'New beginnings, innocence, spontaneity, free spirit';

  @override
  String get tarotRevMajor0 =>
      'Recklessness, risk-taking, foolishness, naivety';

  @override
  String get tarotUpMajor1 =>
      'Manifestation, resourcefulness, power, inspired action';

  @override
  String get tarotRevMajor1 => 'Manipulation, poor planning, untapped talents';

  @override
  String get tarotUpMajor2 =>
      'Intuition, sacred knowledge, divine feminine, subconscious mind';

  @override
  String get tarotRevMajor2 =>
      'Secrets, disconnected from intuition, withdrawal and silence';

  @override
  String get tarotUpMajor3 =>
      'Femininity, beauty, nature, nurturing, abundance';

  @override
  String get tarotRevMajor3 =>
      'Creative block, dependence on others, emptiness';

  @override
  String get tarotUpMajor4 =>
      'Authority, structure, control, fatherhood, stability';

  @override
  String get tarotRevMajor4 =>
      'Domination, excessive control, rigidity, inflexibility';

  @override
  String get tarotUpMajor5 =>
      'Spiritual wisdom, tradition, conformity, morality, ethics';

  @override
  String get tarotRevMajor5 =>
      'Rebellion, subversiveness, new approaches, unconventionality';

  @override
  String get tarotUpMajor6 =>
      'Love, harmony, relationships, values alignment, choices';

  @override
  String get tarotRevMajor6 =>
      'Self-love, disharmony, imbalance, misalignment of values';

  @override
  String get tarotUpMajor7 =>
      'Control, willpower, success, action, determination';

  @override
  String get tarotRevMajor7 => 'Self-discipline, opposition, lack of direction';

  @override
  String get tarotUpMajor8 =>
      'Courage, persuasion, influence, compassion, inner strength';

  @override
  String get tarotRevMajor8 =>
      'Self-doubt, weakness, insecurity, lack of self-discipline';

  @override
  String get tarotUpMajor9 =>
      'Soul-searching, introspection, solitude, inner guidance';

  @override
  String get tarotRevMajor9 => 'Isolation, loneliness, withdrawal, rejection';

  @override
  String get tarotUpMajor10 =>
      'Good luck, karma, life cycles, destiny, turning point';

  @override
  String get tarotRevMajor10 =>
      'Bad luck, resistance to change, breaking cycles';

  @override
  String get tarotUpMajor11 =>
      'Justice, fairness, truth, cause and effect, law';

  @override
  String get tarotRevMajor11 =>
      'Unfairness, lack of accountability, dishonesty';

  @override
  String get tarotUpMajor12 =>
      'Surrender, letting go, new perspectives, enlightenment';

  @override
  String get tarotRevMajor12 => 'Indecision, delay, resistance, stalling';

  @override
  String get tarotUpMajor13 =>
      'End of cycle, beginnings, change, metamorphosis, transition';

  @override
  String get tarotRevMajor13 =>
      'Resistance to change, inability to move on, stagnation';

  @override
  String get tarotUpMajor14 =>
      'Balance, moderation, patience, purpose, meaning';

  @override
  String get tarotRevMajor14 => 'Imbalance, excess, self-healing, realignment';

  @override
  String get tarotUpMajor15 =>
      'Shadow self, attachment, addiction, restriction, sexuality';

  @override
  String get tarotRevMajor15 =>
      'Releasing limiting beliefs, exploring dark thoughts, detachment';

  @override
  String get tarotUpMajor16 =>
      'Sudden change, upheaval, chaos, revelation, awakening';

  @override
  String get tarotRevMajor16 =>
      'Fear of change, avoiding disaster, delaying the inevitable';

  @override
  String get tarotUpMajor17 => 'Hope, faith, purpose, renewal, spirituality';

  @override
  String get tarotRevMajor17 =>
      'Lack of faith, despair, self-doubt, disconnection';

  @override
  String get tarotUpMajor18 =>
      'Illusion, fear, anxiety, subconscious, intuition';

  @override
  String get tarotRevMajor18 =>
      'Release of fear, repressed emotion, inner confusion';

  @override
  String get tarotUpMajor19 => 'Positivity, fun, warmth, success, vitality';

  @override
  String get tarotRevMajor19 => 'Inner child, feeling down, overly optimistic';

  @override
  String get tarotUpMajor20 => 'Judgement, rebirth, inner calling, absolution';

  @override
  String get tarotRevMajor20 => 'Self-doubt, inner critic, ignoring the call';

  @override
  String get tarotUpMajor21 =>
      'Completion, integration, accomplishment, travel';

  @override
  String get tarotRevMajor21 => 'Seeking personal closure, short-cuts, delays';

  @override
  String get tarotUpCupsAce =>
      'New feelings, intuition, intimacy, love, emotional fulfilment';

  @override
  String get tarotRevCupsAce => 'Emotional loss, blocked creativity, emptiness';

  @override
  String get tarotUpCups2 =>
      'Unity, partnership, mutual attraction, connection';

  @override
  String get tarotRevCups2 => 'Imbalanced relationship, tension, disconnection';

  @override
  String get tarotUpCups3 => 'Celebration, friendship, creativity, community';

  @override
  String get tarotRevCups3 => 'Overindulgence, gossip, isolation';

  @override
  String get tarotUpCups4 =>
      'Apathy, contemplation, disillusionment, meditation';

  @override
  String get tarotRevCups4 => 'Clarity, renewal, moving forward, acceptance';

  @override
  String get tarotUpCups5 => 'Loss, grief, disappointment, self-pity, failure';

  @override
  String get tarotRevCups5 => 'Acceptance, moving on, finding peace';

  @override
  String get tarotUpCups6 =>
      'Nostalgia, memories, reunions, childhood, innocence';

  @override
  String get tarotRevCups6 => 'Living in the past, unrealistic expectations';

  @override
  String get tarotUpCups7 =>
      'Daydreaming, fantasy, illusion, wishful thinking, choices';

  @override
  String get tarotRevCups7 => 'Clarity, making choices, disillusionment';

  @override
  String get tarotUpCups8 =>
      'Walking away, disillusionment, leaving behind, searching for truth';

  @override
  String get tarotRevCups8 =>
      'Avoidance, fear of change, staying in a bad situation';

  @override
  String get tarotUpCups9 =>
      'Contentment, satisfaction, gratitude, wish come true';

  @override
  String get tarotRevCups9 => 'Unmet wishes, materialism, dissatisfaction';

  @override
  String get tarotUpCups10 =>
      'Divine love, harmony, alignment, perfect union, happiness';

  @override
  String get tarotRevCups10 => 'Broken relationships, disharmony, misalignment';

  @override
  String get tarotUpCupsPage =>
      'Creative opportunities, intuitive messages, curiosity, possibility';

  @override
  String get tarotRevCupsPage =>
      'Creative blocks, emotional immaturity, insecurity';

  @override
  String get tarotUpCupsKnight =>
      'Creativity, romance, charm, imagination, beauty';

  @override
  String get tarotRevCupsKnight =>
      'Moodiness, disappointment, unrealistic expectations';

  @override
  String get tarotUpCupsQueen =>
      'Compassion, calm, comfort, emotional security, intuition';

  @override
  String get tarotRevCupsQueen =>
      'Martyrdom, insecurity, dependence, smothering';

  @override
  String get tarotUpCupsKing =>
      'Emotional balance, compassion, diplomacy, counselling';

  @override
  String get tarotRevCupsKing =>
      'Coldness, manipulation, moodiness, emotional instability';

  @override
  String get tarotUpPentaclesAce =>
      'New financial opportunity, manifestation, abundance, prosperity';

  @override
  String get tarotRevPentaclesAce =>
      'Lost opportunity, scarcity mindset, financial issues';

  @override
  String get tarotUpPentacles2 =>
      'Balancing decisions, priorities, adaptability, time management';

  @override
  String get tarotRevPentacles2 => 'Imbalance, disorganisation, overwhelm';

  @override
  String get tarotUpPentacles3 =>
      'Teamwork, collaboration, learning, implementation';

  @override
  String get tarotRevPentacles3 =>
      'Lack of teamwork, disorganisation, group conflict';

  @override
  String get tarotUpPentacles4 => 'Conservation, security, frugality, control';

  @override
  String get tarotRevPentacles4 =>
      'Greed, materialism, stinginess, possessiveness';

  @override
  String get tarotUpPentacles5 =>
      'Financial loss, poverty, isolation, worry, insecurity';

  @override
  String get tarotRevPentacles5 => 'Recovery, charity, improvement, generosity';

  @override
  String get tarotUpPentacles6 =>
      'Charity, generosity, giving, mentoring, gratitude';

  @override
  String get tarotRevPentacles6 =>
      'Strings attached, stinginess, power and domination';

  @override
  String get tarotUpPentacles7 =>
      'Vision, perseverance, profit, reward, investment';

  @override
  String get tarotRevPentacles7 =>
      'Lack of vision, limited success, distractions';

  @override
  String get tarotUpPentacles8 =>
      'Apprenticeship, skill development, mastery, high standards';

  @override
  String get tarotRevPentacles8 =>
      'Perfectionism, lack of ambition, low standards';

  @override
  String get tarotUpPentacles9 =>
      'Luxury, self-sufficiency, financial independence, abundance';

  @override
  String get tarotRevPentacles9 =>
      'Over-investment in work, hustling, hollow success';

  @override
  String get tarotUpPentacles10 =>
      'Legacy, culmination, inheritance, family, establishment';

  @override
  String get tarotRevPentacles10 =>
      'Family disputes, bankruptcy, debt, fleeting success';

  @override
  String get tarotUpPentaclesPage =>
      'Manifestation, financial opportunity, skill development';

  @override
  String get tarotRevPentaclesPage =>
      'Lack of progress, procrastination, learning from failure';

  @override
  String get tarotUpPentaclesKnight =>
      'Efficiency, hard work, responsibility, conservatism';

  @override
  String get tarotRevPentaclesKnight =>
      'Boredom, feeling stuck, perfectionism, overwork';

  @override
  String get tarotUpPentaclesQueen =>
      'Nurturing, practical, providing financially, security';

  @override
  String get tarotRevPentaclesQueen =>
      'Financial independence, self-care, work-home conflict';

  @override
  String get tarotUpPentaclesKing =>
      'Abundance, prosperity, security, discipline, leadership';

  @override
  String get tarotRevPentaclesKing =>
      'Poor with money, obsessed with wealth, stubbornness';

  @override
  String get tarotUpWandsAce =>
      'Inspiration, creative spark, new opportunities, potential';

  @override
  String get tarotRevWandsAce =>
      'Delays, blocks, lack of direction, distractions';

  @override
  String get tarotUpWands2 => 'Future planning, progress, decisions, discovery';

  @override
  String get tarotRevWands2 => 'Fear of change, playing it safe, bad planning';

  @override
  String get tarotUpWands3 =>
      'Expansion, foresight, overseas opportunities, leadership';

  @override
  String get tarotRevWands3 =>
      'Lack of foresight, obstacles, delays, frustration';

  @override
  String get tarotUpWands4 => 'Celebration, harmony, marriage, home, community';

  @override
  String get tarotRevWands4 =>
      'Breakdown in communication, transition, misalignment';

  @override
  String get tarotUpWands5 =>
      'Conflict, disagreements, competition, tension, diversity';

  @override
  String get tarotRevWands5 =>
      'End of conflict, resolution, compromise, release';

  @override
  String get tarotUpWands6 =>
      'Victory, success, public recognition, progress, self-confidence';

  @override
  String get tarotRevWands6 =>
      'Private achievement, personal definition of success, fall from grace';

  @override
  String get tarotUpWands7 =>
      'Challenge, competition, protection, perseverance';

  @override
  String get tarotRevWands7 => 'Exhaustion, giving up, overwhelmed';

  @override
  String get tarotUpWands8 =>
      'Speed, action, air travel, movement, quick decisions';

  @override
  String get tarotRevWands8 => 'Panic, waiting, slowdown, exhaustion';

  @override
  String get tarotUpWands9 =>
      'Resilience, courage, persistence, test of faith, boundaries';

  @override
  String get tarotRevWands9 => 'Exhaustion, fatigue, questioning motivations';

  @override
  String get tarotUpWands10 =>
      'Burden, responsibility, hard work, completion, success';

  @override
  String get tarotRevWands10 =>
      'Inability to delegate, micromanagement, burnout';

  @override
  String get tarotUpWandsPage =>
      'Exploration, excitement, freedom, adventure, potential';

  @override
  String get tarotRevWandsPage =>
      'Lack of direction, procrastination, creating conflict';

  @override
  String get tarotUpWandsKnight =>
      'Action, adventure, fearlessness, impulsiveness, travel';

  @override
  String get tarotRevWandsKnight =>
      'Anger, impulsivity, recklessness, impatience';

  @override
  String get tarotUpWandsQueen =>
      'Courage, determination, joy, vibrancy, warmth';

  @override
  String get tarotRevWandsQueen =>
      'Selfishness, jealousy, demands, vengefulness';

  @override
  String get tarotUpWandsKing =>
      'Leadership, vision, big picture, taking control, overcoming challenges';

  @override
  String get tarotRevWandsKing =>
      'Impulsiveness, overbearing manner, unachievable expectations';

  @override
  String get tarotUpSwordsAce =>
      'Breakthrough, clarity, sharp mind, truth, mental strength';

  @override
  String get tarotRevSwordsAce => 'Confusion, brutality, chaos, cloudiness';

  @override
  String get tarotUpSwords2 =>
      'Difficult choices, indecision, stalemate, denial, truce';

  @override
  String get tarotRevSwords2 =>
      'Lesser of two evils, no right choice, confusion';

  @override
  String get tarotUpSwords3 =>
      'Heartbreak, emotional pain, sorrow, grief, hurt';

  @override
  String get tarotRevSwords3 =>
      'Healing, forgiveness, recovery, reconciliation';

  @override
  String get tarotUpSwords4 =>
      'Rest, restoration, contemplation, recuperation, peace';

  @override
  String get tarotRevSwords4 => 'Restlessness, burnout, stress, no relaxation';

  @override
  String get tarotUpSwords5 =>
      'Conflict, disagreements, competition, defeat, winning at all costs';

  @override
  String get tarotRevSwords5 => 'Reconciliation, resolution, compromise';

  @override
  String get tarotUpSwords6 =>
      'Transition, change, rite of passage, releasing baggage';

  @override
  String get tarotRevSwords6 =>
      'Stuck in transition, unfinished business, no closure';

  @override
  String get tarotUpSwords7 =>
      'Deception, trickery, tactics, strategy, sneakiness';

  @override
  String get tarotRevSwords7 => 'Confession, clarity, conscience, honesty';

  @override
  String get tarotUpSwords8 =>
      'Imprisonment, restriction, isolation, self-victimization';

  @override
  String get tarotRevSwords8 => 'Freedom, release, self-acceptance, moving on';

  @override
  String get tarotUpSwords9 => 'Anxiety, worry, fear, depression, nightmares';

  @override
  String get tarotRevSwords9 =>
      'Recovery, learning from mistakes, facing fears';

  @override
  String get tarotUpSwords10 =>
      'Painful endings, deep wounds, betrayal, loss, crisis';

  @override
  String get tarotRevSwords10 =>
      'Recovery, regeneration, rebirth, inevitable end';

  @override
  String get tarotUpSwordsPage =>
      'New ideas, curiosity, thirst for knowledge, new ways of communicating';

  @override
  String get tarotRevSwordsPage =>
      'Poor self-expression, all talk and no action, hurtful words';

  @override
  String get tarotUpSwordsKnight =>
      'Action, impulsiveness, defending beliefs, swift change';

  @override
  String get tarotRevSwordsKnight =>
      'No direction, disregard for consequences, unpredictability';

  @override
  String get tarotUpSwordsQueen =>
      'Independence, unbiased judgement, clear boundaries, direct communication';

  @override
  String get tarotRevSwordsQueen =>
      'Harsh judgement, overly critical, cutting words';

  @override
  String get tarotUpSwordsKing =>
      'Mental clarity, intellectual power, authority, truth';

  @override
  String get tarotRevSwordsKing => 'Manipulation, tyranny, cold-heartedness';

  @override
  String get tarotPosPast => 'Past';

  @override
  String get tarotPosPresent => 'Present';

  @override
  String get tarotPosFuture => 'Future';

  @override
  String get tarotPosChallenge => 'Challenge';

  @override
  String get tarotPosAbove => 'Above';

  @override
  String get tarotPosBelow => 'Below';

  @override
  String get tarotPosAdvice => 'Advice';

  @override
  String get tarotPosExternal => 'External influences';

  @override
  String get tarotPosHopes => 'Hopes and fears';

  @override
  String get tarotPosOutcome => 'Outcome';

  @override
  String get tarotPosHidden => 'Hidden influences';

  @override
  String get tarotPosObstacles => 'Obstacles';

  @override
  String get tarotPosYou => 'You';

  @override
  String get tarotPosPartner => 'Partner';

  @override
  String get tarotPosFoundation => 'Foundation of the relationship';

  @override
  String get tarotPosRecent => 'Recent past';

  @override
  String get tarotPosDynamics => 'Current dynamics';

  @override
  String get tarotPosNear => 'Near future';

  @override
  String get tarotSpreadThree => 'Three cards';

  @override
  String get tarotSpreadCeltic => 'Celtic cross';

  @override
  String get tarotSpreadHorseshoe => 'Horseshoe';

  @override
  String get tarotSpreadRelationship => 'Relationship';

  @override
  String get tarotSumMajorHigh =>
      'Many major arcana: big forces and turning points are at work.';

  @override
  String get tarotSumMajorLow =>
      'Mostly minor arcana: this is about everyday matters, not upheaval.';

  @override
  String get tarotSumMajorBalanced =>
      'A mix of major and minor arcana: the everyday ties into the larger path.';

  @override
  String get tarotSumReversedMany =>
      'Many reversed cards: obstacles, delays or inner doubts to deal with first.';

  @override
  String get tarotSumReversedFew => 'Nearly all upright: the way looks clear.';

  @override
  String get tarotSumSuitCups =>
      'Cups stand out: feelings, relationships and intuition.';

  @override
  String get tarotSumSuitPentacles =>
      'Pentacles stand out: money, work and practical things.';

  @override
  String get tarotSumSuitWands =>
      'Wands stand out: drive, creativity and action.';

  @override
  String get tarotSumSuitSwords =>
      'Swords stand out: thought, decisions and conflict.';

  @override
  String get tarotForFun =>
      'For fun: the cards were drawn by the server at random.';

  @override
  String cmdNeedsText(String name) {
    return '/$name needs some text after it.';
  }

  @override
  String cmdTakesNothing(String name) {
    return '/$name takes nothing after it.';
  }

  @override
  String cmdNeedsOptions(String name) {
    return '/$name needs at least two options, separated by |.';
  }

  @override
  String cmdTooManyOptions(String name) {
    return '/$name takes at most 20 options.';
  }

  @override
  String cmdEmptyOption(String name) {
    return 'An option of /$name is empty.';
  }

  @override
  String get cmdBadDraw =>
      'Write /draw [count] [deck], for example /draw 3 tarot.';

  @override
  String cmdUnknownDeck(String decks) {
    return 'There is no such deck. The decks are: $decks.';
  }

  @override
  String get cmdTooManyCards => 'That deck does not have that many cards.';

  @override
  String cmdUnknownSpread(String spreads) {
    return 'There is no such spread. The spreads are: $spreads.';
  }

  @override
  String cmdBadCount(int max) {
    return 'Draw 1 to $max tiles.';
  }

  @override
  String cmdNeedsChannel(String name) {
    return '/$name needs a channel: type # and pick one.';
  }

  @override
  String cmdNeedsPerson(String name) {
    return '/$name needs a person: type @ and pick one.';
  }

  @override
  String cmdBadPerson(String name) {
    return '/$name takes one person, written @name.';
  }

  @override
  String get cmdBadDuration =>
      'Write a time like 30m, 2h, 1h30m, 1d or 1w (at most a year).';

  @override
  String cmdNotInSealedChance(String name) {
    return '/$name is drawn by the server, which cannot write into a sealed chat.';
  }

  @override
  String cmdNotInSealed(String name) {
    return '/$name is not available in a sealed chat.';
  }

  @override
  String cmdNotHere(String name) {
    return '/$name is not available here.';
  }

  @override
  String cmdNoSuchPerson(String person) {
    return 'Nobody here is called @$person.';
  }

  @override
  String cmdNoSuchChannel(String channel) {
    return 'No channel you can see is called “$channel”.';
  }

  @override
  String cmdSeveralChannels(String channel) {
    return 'Several channels are called “$channel”: type # and pick the one you mean.';
  }

  @override
  String cmdJoined(String channel) {
    return 'You joined $channel.';
  }

  @override
  String cmdLeft(String channel) {
    return 'You left $channel.';
  }

  @override
  String cmdTopicIs(String topic) {
    return 'The topic: $topic';
  }

  @override
  String get cmdNoTopic => 'This channel has no topic.';

  @override
  String get cmdTopicSet => 'Topic set.';

  @override
  String get cmdAway => 'You appear away.';

  @override
  String get cmdBack => 'You no longer appear away.';

  @override
  String get cmdDndOn => 'Do not disturb is on.';

  @override
  String get cmdDndOff => 'You are available now.';

  @override
  String cmdQuietOn(int minutes) {
    String _temp0 = intl.Intl.pluralLogic(
      minutes,
      locale: localeName,
      other: '$minutes minutes',
      one: '1 minute',
    );
    return 'This conversation is muted for $_temp0.';
  }

  @override
  String get cmdQuietOff => 'This conversation is no longer muted.';

  @override
  String cmdInvited(String person) {
    return '$person was added.';
  }

  @override
  String cmdKicked(String person) {
    return '$person was removed from this channel.';
  }

  @override
  String cmdBanned(String person) {
    return '$person is banned from this channel. They cannot come back until the ban is lifted.';
  }

  @override
  String cmdMuted(String person, int minutes) {
    String _temp0 = intl.Intl.pluralLogic(
      minutes,
      locale: localeName,
      other: '$minutes minutes',
      one: '1 minute',
    );
    return '$person cannot post here for $_temp0.';
  }

  @override
  String cmdOpped(String person) {
    return '$person is a manager of this channel.';
  }

  @override
  String cmdDeopped(String person) {
    return '$person is a member of this channel.';
  }

  @override
  String cmdIgnored(String person) {
    return 'You blocked $person. Their messages are collapsed for you.';
  }

  @override
  String cmdNick(String name) {
    return 'Your display name is now $name.';
  }

  @override
  String chanceRolled(String dice) {
    return 'Rolled $dice';
  }

  @override
  String get chanceFlipped => 'Flipped a coin';

  @override
  String chancePicked(int count) {
    return 'Picked from $count';
  }

  @override
  String chanceShuffled(int count) {
    return 'Shuffled $count';
  }

  @override
  String chanceDrew(int count, String deck) {
    return 'Drew $count from the $deck deck';
  }

  @override
  String chanceDrewTiles(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: 'Drew $count mahjong tiles',
      one: 'Drew 1 mahjong tile',
    );
    return '$_temp0';
  }

  @override
  String chanceTarot(String spread) {
    return 'Tarot: $spread';
  }

  @override
  String get chanceByServer => 'drawn by the server';

  @override
  String get chanceHeads => 'Heads';

  @override
  String get chanceTails => 'Tails';

  @override
  String chanceCoinValue(String face, String value) {
    String _temp0 = intl.Intl.selectLogic(face, {
      'tails': 'tails',
      'other': 'heads',
    });
    return '$_temp0 ($value)';
  }

  @override
  String get chanceDropped => 'dropped';

  @override
  String get chanceUpright => 'upright';

  @override
  String get chanceReversed => 'reversed';

  @override
  String get chanceReplay => 'Press to spin again';

  @override
  String chanceTotalSpoken(int total) {
    return 'total $total';
  }

  @override
  String cardRankOfSuit(String rank, String suit) {
    return '$rank of $suit';
  }

  @override
  String cardSuitAndRank(String suit, String rank) {
    return '$suit $rank';
  }

  @override
  String get announcementNoPost =>
      'This is an announcement channel: only its managers and the people they allow post here. You can read and react.';

  @override
  String get announcementReplyOnly =>
      'This is an announcement channel: only its managers and the people they allow post here. You can read, react and reply in threads.';

  @override
  String get announcementSetting => 'Announcement channel';

  @override
  String get announcementSettingHint =>
      'Only managers and the people they allow post. Everyone else reads and reacts. Who is in the channel is shown to managers only, and nobody is shown typing or online.';

  @override
  String get announcementRepliesSetting => 'Replies in threads';

  @override
  String get announcementRepliesHint =>
      'Let members who may not post reply in threads.';

  @override
  String get lazyDeliverySetting => 'Delivery when not open';

  @override
  String get lazyDeliveryHint =>
      'A very large channel is sent in full only to people who have it open; the others see that it changed and get the rest when they open it. The server decides by size unless you say.';

  @override
  String get lazyDeliveryServer => 'Follow the server';

  @override
  String get lazyDeliveryOn => 'Only to people who have it open';

  @override
  String get lazyDeliveryOff => 'To everyone, always';

  @override
  String get mayPostHere => 'Let them post here';

  @override
  String get mayNotPostHere => 'Stop them posting here';

  @override
  String get mayPostTag => 'posts here';

  @override
  String membersHidden(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count members',
      one: '1 member',
    );
    return '$_temp0. An announcement channel does not show who is in it.';
  }

  @override
  String onlineInChannel(int count) {
    return '$count online';
  }

  @override
  String get lastSeenRecently => 'Last seen recently';

  @override
  String get lastSeenWithinWeek => 'Last seen within a week';

  @override
  String get lastSeenLongAgo => 'Last seen a while ago';

  @override
  String get hideLastSeen => 'Hide when I was last here';

  @override
  String get hideLastSeenHint =>
      'Your profile otherwise says roughly when you were last here: recently, within a week, or a while ago. Never the time.';

  @override
  String get instantConverterTitle => 'Time converter';

  @override
  String get instantYourTime => 'Your time';

  @override
  String get instantAsWritten => 'As written';

  @override
  String get instantUtc => 'UTC';

  @override
  String instantTypedAs(String typed) {
    return 'Typed as “$typed”';
  }

  @override
  String get instantAddZone => 'Add a time zone';

  @override
  String get instantSearchZones => 'Search time zones and cities';

  @override
  String get instantNoZones => 'No time zone matches.';

  @override
  String instantRemoveZone(String zone) {
    return 'Remove $zone';
  }

  @override
  String get instantCopyIso => 'Copy as ISO 8601';

  @override
  String get instantCopied => 'Time copied.';

  @override
  String get instantRemindMe => 'Remind me';

  @override
  String instantChipLabel(String when, String zone, String typed) {
    return '$when, $zone. Written as “$typed”. Opens the time converter.';
  }

  @override
  String get instantToday => 'Today';

  @override
  String get instantTomorrow => 'Tomorrow';

  @override
  String get instantYesterday => 'Yesterday';

  @override
  String instantIn(String amount) {
    return 'in $amount';
  }

  @override
  String instantAgo(String amount) {
    return '$amount ago';
  }

  @override
  String instantSeconds(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count seconds',
      one: '1 second',
    );
    return '$_temp0';
  }

  @override
  String instantMinutes(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count minutes',
      one: '1 minute',
    );
    return '$_temp0';
  }

  @override
  String instantHours(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count hours',
      one: '1 hour',
    );
    return '$_temp0';
  }

  @override
  String instantDays(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count days',
      one: '1 day',
    );
    return '$_temp0';
  }

  @override
  String instantMonths(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count months',
      one: '1 month',
    );
    return '$_temp0';
  }

  @override
  String instantYears(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count years',
      one: '1 year',
    );
    return '$_temp0';
  }

  @override
  String instantComposerPhrase(String typed, String when) {
    return '“$typed” will show as $when your time';
  }

  @override
  String get instantDropTime => 'Not a time';

  @override
  String instantDropTimeLabel(String typed) {
    return 'Send “$typed” as plain words';
  }

  @override
  String get instantMakeTime => 'MAKE THIS A TIME';

  @override
  String instantMakeTimeLine(String when) {
    return 'Make this a time: $when (your time)';
  }

  @override
  String get instantsHere => 'Times in this message';

  @override
  String cmdUnbanned(String person) {
    return '$person may come back to this channel.';
  }

  @override
  String cmdExileTitle(String person) {
    return 'Exile $person from the server?';
  }

  @override
  String cmdExileBody(String person) {
    return '$person will be removed from the whole server, every channel and chat, and kept from coming back. To keep them out of this channel only, use /ban.';
  }

  @override
  String get cmdExileYes => 'Exile';

  @override
  String cmdExiled(String person) {
    return '$person is exiled from the server.';
  }

  @override
  String cmdNotExiled(String person) {
    return '@$person is not exiled.';
  }

  @override
  String cmdUnexiled(String person) {
    return '@$person may register again. Their old account is not brought back.';
  }

  @override
  String cmdUnignored(String person) {
    return 'You unblocked $person.';
  }

  @override
  String banNotice(String where) {
    return 'You were banned from $where.';
  }

  @override
  String banNoticeUntil(String where, String when) {
    return 'You were banned from $where until $when.';
  }

  @override
  String banNoticeReason(String notice, String reason) {
    return '$notice The reason given: “$reason”';
  }

  @override
  String get banNoticeAChat => 'a chat';

  @override
  String banTitle(String person) {
    return 'Ban $person from this channel';
  }

  @override
  String get banBody =>
      'They are removed from this channel and cannot join, be added or come back through a group until the ban is lifted. Only this channel: to keep someone off the whole server, an admin exiles them.';

  @override
  String get banReason => 'Reason (shown to them and to the managers)';

  @override
  String get banUntilLifted => 'Until it is lifted';

  @override
  String get banYes => 'Ban';

  @override
  String get banFromChannel => 'Ban from the channel…';

  @override
  String get bannedHeading => 'BANNED';

  @override
  String bannedBy(String person, String when) {
    return 'Banned by $person, $when';
  }

  @override
  String bannedUntil(String when) {
    return 'until $when';
  }

  @override
  String get banLifted => 'The ban is lifted. They can join or be added again.';

  @override
  String get liftBan => 'Lift';

  @override
  String objectWhatIs(String typed) {
    return 'WHAT IS “$typed”?';
  }

  @override
  String objectReferenceRow(String typed, String host) {
    return 'Reference $typed, opens $host';
  }

  @override
  String objectInstantRow(String when) {
    return 'Time: $when (your time)';
  }

  @override
  String objectPlainRow(String typed) {
    return 'Plain text: “$typed”';
  }

  @override
  String objectSaidObject(String typed) {
    return '$typed accepted. Backspace makes it plain text.';
  }

  @override
  String objectSaidPlain(String typed) {
    return '$typed kept as plain text.';
  }

  @override
  String get patternsTab => 'References';

  @override
  String get patternsAbout =>
      'Text such as PROJ-123 or #4521 becomes a link to where the thing it names is kept. These apply in this channel, ahead of the server’s with the same prefix. A change applies to messages posted after it.';

  @override
  String get patternsNone => 'No reference patterns in this channel.';

  @override
  String get patternsServerHere => 'Server-wide patterns also apply here:';

  @override
  String get patternName => 'Name';

  @override
  String get patternPrefix => 'Prefix';

  @override
  String get patternPrefixHelp =>
      'Such as PROJ- or #. The value follows it at once.';

  @override
  String get patternExpression => 'Value (optional)';

  @override
  String get patternExpressionHelp =>
      'Empty for 1 to 12 digits. Otherwise letters, digits, _ and -, classes such as [A-Z0-9], \\d and \\w, each with ?, +, * or a repeat count.';

  @override
  String get patternURL => 'Address';

  @override
  String patternURLHelp(Object value) {
    return 'https://… with $value where the matched value goes.';
  }

  @override
  String get patternAdd => 'Add a pattern';

  @override
  String get patternEdit => 'Change';

  @override
  String get patternSave => 'Save';

  @override
  String get patternRemove => 'Remove';

  @override
  String patternRemoveAsk(String name) {
    return 'Remove the pattern “$name”? Messages already sent keep their links.';
  }

  @override
  String watchFor(String person) {
    return 'Tell me when $person is around';
  }

  @override
  String stopWatchingFor(String person) {
    return 'Stop watching for $person';
  }

  @override
  String watchSet(String person) {
    return 'You will be told once when $person is next around. This lasts 48 hours.';
  }

  @override
  String watchIsAround(String person) {
    return '$person is around now.';
  }

  @override
  String watchStopped(String person) {
    return 'You stopped watching for $person.';
  }

  @override
  String get watchAuthor => 'Tell me when they are around';

  @override
  String get watchingTitle => 'Watching for';

  @override
  String watchUntil(String when) {
    return 'until $when';
  }

  @override
  String get stopWatching => 'Stop watching';

  @override
  String get messageLinkLoading => 'Loading the linked message…';

  @override
  String get messageLinkHidden => 'A message you can’t see';

  @override
  String get messageLinkDeleted => 'This message was deleted.';

  @override
  String messageLinkLabel(String typed, String said) {
    return 'Link to a message ($typed). $said';
  }

  @override
  String get objectMessageLinkRow => 'Link to a message, shown as a preview';

  @override
  String objectMeasureRow(String typed) {
    return 'Measurement: $typed';
  }

  @override
  String objectMeasureRowAlt(String typed, String alt) {
    return 'Measurement: $typed ($alt for readers who use it)';
  }

  @override
  String get measureSettings => 'Measurements';

  @override
  String get measureSettingsNote =>
      'Beside a measurement such as 5 km or 72°F, show what it is in your system. Kept on this device.';

  @override
  String measureRegion(String system) {
    return 'From this device’s region ($system)';
  }

  @override
  String get measureMetric => 'Metric';

  @override
  String get measureImperial => 'Imperial';

  @override
  String get measureOff => 'Off';

  @override
  String colourLabel(String typed) {
    return 'Colour $typed. Copy';
  }

  @override
  String objectColourRow(String value) {
    return 'Colour $value';
  }

  @override
  String eventLabel(String from, String to, String zone, String typed) {
    return 'Event from $from to $to, $zone. Typed as $typed';
  }

  @override
  String get eventAddToCalendar => 'Add to calendar';

  @override
  String get eventOpenConverter => 'Open the time converter';

  @override
  String objectEventRow(String range) {
    return 'Event: $range (your time)';
  }

  @override
  String phoneLabel(String typed) {
    return 'Phone number $typed';
  }

  @override
  String emailLabel(String typed) {
    return 'Email address $typed';
  }

  @override
  String get contactCall => 'Call';

  @override
  String get contactWrite => 'Write';

  @override
  String get contactCopy => 'Copy';

  @override
  String objectPhoneRow(String typed) {
    return 'Phone number $typed: call or copy';
  }

  @override
  String objectEmailRow(String typed) {
    return 'Email address $typed: write or copy';
  }

  @override
  String placeOpenLabel(String typed) {
    return 'Place $typed. Open in maps';
  }

  @override
  String placeCopyLabel(String typed) {
    return 'Place $typed. Copy';
  }

  @override
  String objectGeoRow(String lat, String lon) {
    return 'Place $lat, $lon: opens maps on phones';
  }

  @override
  String diceLabel(String typed, String said, String total) {
    return 'Dice $typed: $said; total $total';
  }

  @override
  String get diceRemoved => '(not rolled: written in an edit)';

  @override
  String diceRemovedLabel(String typed) {
    return 'Dice $typed, not rolled: written in an edit';
  }

  @override
  String objectDiceRow(String value) {
    return 'Roll $value when it is sent';
  }

  @override
  String get cmdBadBroadcastTime =>
      'Show it for 1m to 24h, written like 15m or 2h.';

  @override
  String get cmdNoBans => 'Nobody is banned from this channel.';

  @override
  String banListTitle(String channel) {
    return 'Banned from $channel';
  }

  @override
  String get cmdClosed =>
      'Hidden from the sidebar until something new arrives.';

  @override
  String cmdMotdTitle(String community) {
    return 'Welcome to $community';
  }

  @override
  String get cmdMotdNone => 'This server has no welcome text.';

  @override
  String get cmdMotdNoRules =>
      'Churr keeps no separate rules: this welcome text is where the admins write them.';

  @override
  String cmdAdmins(String names) {
    return 'Who runs this server: $names.';
  }

  @override
  String cmdOwnerMark(String name) {
    return '$name (owner)';
  }

  @override
  String cmdVersion(String server, String version) {
    return '$server runs Churr $version.';
  }

  @override
  String cmdPing(int ms) {
    return 'The delay to the server is $ms ms.';
  }

  @override
  String get cmdPingNone => 'Not connected to the server right now.';

  @override
  String cmdTime(String time, String person, String zone) {
    return 'It is $time for $person ($zone).';
  }

  @override
  String cmdTimeUnknown(String person) {
    return '$person\'s time zone is not known.';
  }

  @override
  String cmdLusersMembers(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count members here.',
      one: '1 member here.',
    );
    return '$_temp0';
  }

  @override
  String cmdLusersOnlineHere(int count) {
    return '$count of them online.';
  }

  @override
  String cmdLusersOnlineServer(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count people online on the server.',
      one: '1 person online on the server.',
    );
    return '$_temp0';
  }

  @override
  String get cmdInvisible =>
      'You appear away: Churr has no appear offline. Use /back to stop.';

  @override
  String cmdVoiced(String person) {
    return '$person may post in this announcement channel.';
  }

  @override
  String cmdDevoiced(String person) {
    return '$person may no longer post in this announcement channel.';
  }

  @override
  String get cmdNotAnnouncement =>
      'This is not an announcement channel, so everyone who may post here does already. A manager can make it one in the channel\'s settings.';

  @override
  String cmdKnocked(String channel) {
    return 'If $channel is a private channel, its managers were asked to add you. You will be told when they answer.';
  }

  @override
  String cmdKnockJoinable(String channel) {
    return '$channel is open to you: join it with /join.';
  }

  @override
  String cmdAlreadyIn(String channel) {
    return 'You are already in $channel.';
  }

  @override
  String knockAccepted(String channel) {
    return 'You were added to $channel.';
  }

  @override
  String knockDeclined(String channel) {
    return 'Your request to join $channel was declined.';
  }

  @override
  String knocksWaiting(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count people ask to join',
      one: '1 person asks to join',
    );
    return '$_temp0';
  }

  @override
  String get knocksReview => 'Review';

  @override
  String knocksTitle(String channel) {
    return 'Asking to join $channel';
  }

  @override
  String get knocksNone => 'Nobody is asking to join.';

  @override
  String get knockAccept => 'Add';

  @override
  String get knockDecline => 'Decline';

  @override
  String knockAdded(String person) {
    return '$person was added.';
  }

  @override
  String knockTurnedAway(String person) {
    return '$person was told no.';
  }

  @override
  String cmdBroadcastSent(int minutes) {
    String _temp0 = intl.Intl.pluralLogic(
      minutes,
      locale: localeName,
      other: '$minutes minutes',
      one: '1 minute',
    );
    return 'Shown to everyone connected for $_temp0.';
  }

  @override
  String get cmdBroadcastCleared => 'The broadcast is down.';

  @override
  String get broadcastLabel => 'Server notice';

  @override
  String broadcastFrom(String name) {
    return 'from $name';
  }

  @override
  String get broadcastDismiss => 'Dismiss';

  @override
  String broadcastAnnounce(String text) {
    return 'Server notice: $text';
  }

  @override
  String get helpTitle => 'Commands';

  @override
  String get helpSearch => 'Search commands';

  @override
  String get helpNone => 'No command matches.';

  @override
  String helpAlso(String names) {
    return 'also $names';
  }

  @override
  String helpInsert(String name) {
    return 'Put /$name in the message box';
  }

  @override
  String get cmdGroupPost => 'Posting';

  @override
  String get cmdGroupConversations => 'Conversations';

  @override
  String get cmdGroupMembers => 'Members and moderation';

  @override
  String get cmdGroupPeople => 'People';

  @override
  String get cmdGroupStatus => 'Status';

  @override
  String get cmdGroupServer => 'The server';

  @override
  String get cmdGroupInfo => 'Help';

  @override
  String get cmdGroupForms => 'Forms';

  @override
  String get cmdGroupReminders => 'Reminders';

  @override
  String get cmdGroupChance => 'Chance and games';

  @override
  String get cmdGroupSavedReplies => 'Saved replies and community commands';

  @override
  String get cmdGroupBots => 'Bots';

  @override
  String knockAsked(String channel) {
    return 'You asked to join $channel. You will be told when its managers answer.';
  }

  @override
  String get knockPickerTitle => 'Ask to join a channel';

  @override
  String get knockPickerNone =>
      'No channel you can see takes requests to join.';

  @override
  String knockManagers(String names) {
    return 'Managers: $names';
  }

  @override
  String get kindKnock => 'Knock channel';

  @override
  String get makeKnock => 'Make it a knock channel';

  @override
  String get makeKnockNote =>
      'Everyone here can see its name, topic, member count and managers and ask to join; only members read it. Non-members lose its history at once.';

  @override
  String get makePublicAgain => 'Make it public';

  @override
  String get makePublicAgainNote =>
      'Anyone here can join and read it, its history included.';

  @override
  String get notifyRequestBody => 'wants to chat with you';

  @override
  String get requests => 'Requests';

  @override
  String get requestsEmpty =>
      'No requests. When someone asks to start a chat with you, it waits here for your answer.';

  @override
  String get requestAsksDirect => 'Asks for a direct chat';

  @override
  String get requestAsksSealed => 'Asks for a sealed chat';

  @override
  String get requestAccept => 'Accept';

  @override
  String get requestDecline => 'Decline';

  @override
  String get requestDismiss => 'Dismiss';

  @override
  String get requestBlock => 'Block';

  @override
  String get requestAnswersNote =>
      'Decline tells them; dismiss and block do not.';

  @override
  String chatWaiting(String name) {
    return 'Waiting for $name to accept your request. You can write once they do.';
  }

  @override
  String chatDeclined(String name) {
    return '$name declined this chat.';
  }

  @override
  String get chatClosedNote =>
      'This chat is closed. You can both still read it.';

  @override
  String get askToChatAgain => 'Ask to chat again';

  @override
  String get withdrawRequest => 'Withdraw request';

  @override
  String get closeChat => 'Close chat';

  @override
  String get closeChatTitle => 'Close this chat?';

  @override
  String closeChatBody(String name) {
    return 'You and $name both keep what was written here to read. Neither of you can write in it again. Either of you can ask to chat again later.';
  }

  @override
  String get withdrawRequestTitle => 'Withdraw this request?';

  @override
  String withdrawRequestBody(String name) {
    return 'You cannot ask $name again for a day.';
  }

  @override
  String get chatStateWaiting => 'waiting';

  @override
  String get chatStateDeclined => 'declined';

  @override
  String get chatStateClosed => 'closed';

  @override
  String get autoApproveDirect => 'Auto approve new direct chats';

  @override
  String get autoApproveDirectHint =>
      'Off: someone starting a new direct chat with you asks first, and you accept, decline, dismiss or block. On: it opens straight away.';

  @override
  String get autoApproveSealed => 'Auto approve new sealed chats';

  @override
  String get autoApproveSealedHint => 'The same for new sealed chats.';

  @override
  String get scheduledTitle => 'Scheduled';

  @override
  String get scheduledMessagesHead => 'Messages waiting to be sent';

  @override
  String get scheduledNone =>
      'Nothing is waiting. Send later is the clock beside the send button.';

  @override
  String scheduledFor(String where) {
    return 'For $where';
  }

  @override
  String get aConversation => 'a conversation';

  @override
  String get scheduledFailed => 'failed';

  @override
  String get scheduledDone => 'done';

  @override
  String get scheduledNoText => 'No text';

  @override
  String get editScheduled => 'Edit';

  @override
  String get sendNow => 'Send now';

  @override
  String get cancelScheduled => 'Cancel it';

  @override
  String get cancelScheduledTitle => 'Cancel this?';

  @override
  String get cancelScheduledBody => 'It will not be sent.';

  @override
  String get scheduledCancelled => 'Cancelled.';

  @override
  String get scheduledSent => 'Sent.';

  @override
  String get scheduledTextLabel => 'Text';

  @override
  String scheduledWhen(String when) {
    return 'When: $when';
  }

  @override
  String get reschedule => 'Reschedule';

  @override
  String get remindersHead => 'Reminders';

  @override
  String get remindersNone =>
      'No reminders. Remind me about this is in a message\'s menu.';

  @override
  String get reminderWhere => 'Reminder';

  @override
  String get savedRepliesTitle => 'Saved replies';

  @override
  String get savedRepliesNone =>
      'A saved reply is text you post by typing /name.';

  @override
  String get newSavedReply => 'New saved reply';

  @override
  String get savedReplyName => 'Command';

  @override
  String get savedReplyNameHint =>
      'Typed as /name. Only a fun or game built-in (/shrug, /roll …) may be replaced; the others\' names are refused.';

  @override
  String get savedReplyText => 'What it posts';

  @override
  String get deleteSavedReply => 'Delete';

  @override
  String get groupsTitle => 'Groups';

  @override
  String get groupsNone => 'This server has no groups yet.';

  @override
  String groupMembers(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count members',
      one: '1 member',
    );
    return '$_temp0';
  }

  @override
  String get groupYoureIn => 'you are in it';

  @override
  String get groupClosed => 'admins choose who is in it';

  @override
  String get leaveGroup => 'Leave';

  @override
  String joinedGroup(String handle) {
    return 'You joined @$handle, and the channels it is in.';
  }

  @override
  String leftGroup(String handle) {
    return 'You left @$handle.';
  }

  @override
  String get addEmoji => 'Add emoji';

  @override
  String get addEmojiNote =>
      'A picture everyone on this server can use as :name:.';

  @override
  String get addEmojiTitle => 'Add an emoji';

  @override
  String get addEmojiName => 'Name';

  @override
  String get addEmojiNameHint =>
      'Typed as :name:. Lowercase letters, digits, underscore, plus and dash.';

  @override
  String get addEmojiPictureHint =>
      'A picture: at most 256 KB and 256 pixels square.';

  @override
  String get addEmojiChoose => 'Choose a picture';

  @override
  String get addEmojiAdd => 'Add';

  @override
  String emojiAdded(String name) {
    return ':$name: is added.';
  }

  @override
  String get blockedTitle => 'Blocked accounts';

  @override
  String get blockedIntro =>
      'A blocked account\'s messages are collapsed for you, it cannot start a chat with you, and its mentions do not notify you. It is told nothing. Block someone from their profile.';

  @override
  String get blockingOff => 'Blocking is turned off on this server.';

  @override
  String get blockedNone => 'You have blocked nobody.';

  @override
  String get blockedUnknown => 'An account you can no longer see';

  @override
  String get checkinsTitle => 'Check-ins';

  @override
  String get checkinsIntro =>
      'A check-in posts the same questions on a schedule, as a form. Each person answers once per run, and the answers are replies in that run\'s thread. Each run closes when the next is posted.';

  @override
  String get checkinsNone => 'No check-ins in this channel.';

  @override
  String get newCheckin => 'New check-in';

  @override
  String get stopCheckin => 'Stop';

  @override
  String get stopCheckinTitle => 'Stop this check-in?';

  @override
  String get stopCheckinBody =>
      'No more runs are posted. Runs already posted, and their answers, stay.';

  @override
  String checkinQuestions(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count questions',
      one: '1 question',
    );
    return '$_temp0';
  }

  @override
  String checkinFailed(String why) {
    return 'failed: $why';
  }

  @override
  String checkinNext(String when) {
    return 'next $when';
  }

  @override
  String checkinBy(String name) {
    return 'set up by $name';
  }

  @override
  String get checkinTitleLabel => 'Title';

  @override
  String get checkinTextLabel => 'Message with it (optional)';

  @override
  String get checkinQuestionsHead => 'Questions';

  @override
  String checkinQuestion(int n) {
    return 'Question $n';
  }

  @override
  String get checkinRemoveQuestion => 'Remove this question';

  @override
  String get checkinLong => 'Long';

  @override
  String get checkinRequired => 'Required';

  @override
  String get checkinAddQuestion => 'Add a question';

  @override
  String get checkinWhen => 'When';

  @override
  String get checkinAt => 'At';

  @override
  String get checkinZone => 'Time zone';

  @override
  String get checkinsNote =>
      'Questions posted on a schedule, answered in a thread';

  @override
  String get filesPostedHere => 'Files posted here';

  @override
  String get endOtherSessions => 'Sign out all other sessions';

  @override
  String get endOtherSessionsTitle => 'Sign out everywhere else?';

  @override
  String get endOtherSessionsBody =>
      'Every other browser and app signed in to this account on this server is signed out at once. Personal tokens too. This app stays signed in.';

  @override
  String get endOtherSessionsDone => 'Every other session is signed out.';
}
