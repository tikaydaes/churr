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
  String get accountPage => 'Passkeys, sessions and tokens…';

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
      'This needs your passkey: it is done on the account page in your browser.';

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
      'People join as readers; managers post.';

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
}
