import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_localizations_en.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of L10n
/// returned by `L10n.of(context)`.
///
/// Applications need to include `L10n.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'l10n/app_localizations.dart';
///
/// return MaterialApp(
///   localizationsDelegates: L10n.localizationsDelegates,
///   supportedLocales: L10n.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the L10n.supportedLocales
/// property.
abstract class L10n {
  L10n(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static L10n of(BuildContext context) {
    return Localizations.of<L10n>(context, L10n)!;
  }

  static const LocalizationsDelegate<L10n> delegate = _L10nDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[Locale('en')];

  /// No description provided for @appTitle.
  ///
  /// In en, this message translates to:
  /// **'Churr'**
  String get appTitle;

  /// No description provided for @cancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get cancel;

  /// No description provided for @save.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get save;

  /// No description provided for @close.
  ///
  /// In en, this message translates to:
  /// **'Close'**
  String get close;

  /// No description provided for @done.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get done;

  /// No description provided for @retry.
  ///
  /// In en, this message translates to:
  /// **'Try again'**
  String get retry;

  /// No description provided for @didNotWork.
  ///
  /// In en, this message translates to:
  /// **'That did not work: {reason}'**
  String didNotWork(String reason);

  /// No description provided for @for30Minutes.
  ///
  /// In en, this message translates to:
  /// **'For 30 minutes'**
  String get for30Minutes;

  /// No description provided for @forAnHour.
  ///
  /// In en, this message translates to:
  /// **'For an hour'**
  String get forAnHour;

  /// No description provided for @for8Hours.
  ///
  /// In en, this message translates to:
  /// **'For 8 hours'**
  String get for8Hours;

  /// No description provided for @forADay.
  ///
  /// In en, this message translates to:
  /// **'For a day'**
  String get forADay;

  /// No description provided for @forAWeek.
  ///
  /// In en, this message translates to:
  /// **'For a week'**
  String get forAWeek;

  /// No description provided for @untilTomorrowMorning.
  ///
  /// In en, this message translates to:
  /// **'Until tomorrow morning'**
  String get untilTomorrowMorning;

  /// No description provided for @untilChosenTime.
  ///
  /// In en, this message translates to:
  /// **'Until a chosen time…'**
  String get untilChosenTime;

  /// No description provided for @untilTurnedOff.
  ///
  /// In en, this message translates to:
  /// **'Until turned off'**
  String get untilTurnedOff;

  /// No description provided for @untilTime.
  ///
  /// In en, this message translates to:
  /// **'until {time}'**
  String untilTime(String time);

  /// No description provided for @untilTimeCap.
  ///
  /// In en, this message translates to:
  /// **'Until {time}'**
  String untilTimeCap(String time);

  /// No description provided for @noEnd.
  ///
  /// In en, this message translates to:
  /// **'No end'**
  String get noEnd;

  /// No description provided for @chooseDate.
  ///
  /// In en, this message translates to:
  /// **'Choose the day'**
  String get chooseDate;

  /// No description provided for @chooseTime.
  ///
  /// In en, this message translates to:
  /// **'Choose the time'**
  String get chooseTime;

  /// No description provided for @refusedOn.
  ///
  /// In en, this message translates to:
  /// **'Not changed on {server}: {reason}'**
  String refusedOn(String server, String reason);

  /// No description provided for @waitingFor.
  ///
  /// In en, this message translates to:
  /// **'Will be sent to {server} when it is back'**
  String waitingFor(String server);

  /// No description provided for @signedOutShort.
  ///
  /// In en, this message translates to:
  /// **'Signed out'**
  String get signedOutShort;

  /// No description provided for @notifications.
  ///
  /// In en, this message translates to:
  /// **'Notifications'**
  String get notifications;

  /// No description provided for @threads.
  ///
  /// In en, this message translates to:
  /// **'Threads'**
  String get threads;

  /// No description provided for @bookmarks.
  ///
  /// In en, this message translates to:
  /// **'Bookmarks'**
  String get bookmarks;

  /// No description provided for @allServers.
  ///
  /// In en, this message translates to:
  /// **'All servers'**
  String get allServers;

  /// No description provided for @allKinds.
  ///
  /// In en, this message translates to:
  /// **'All'**
  String get allKinds;

  /// No description provided for @mentionsOnly.
  ///
  /// In en, this message translates to:
  /// **'Mentions'**
  String get mentionsOnly;

  /// No description provided for @threadsOnly.
  ///
  /// In en, this message translates to:
  /// **'Threads'**
  String get threadsOnly;

  /// No description provided for @markAllRead.
  ///
  /// In en, this message translates to:
  /// **'Mark all read'**
  String get markAllRead;

  /// No description provided for @markRead.
  ///
  /// In en, this message translates to:
  /// **'Mark read'**
  String get markRead;

  /// No description provided for @earlier.
  ///
  /// In en, this message translates to:
  /// **'Earlier'**
  String get earlier;

  /// No description provided for @nothingNew.
  ///
  /// In en, this message translates to:
  /// **'Nothing new. Mentions, replies in threads you follow and messages in conversations set to Any activity show up here.'**
  String get nothingNew;

  /// No description provided for @reasonMentionedYou.
  ///
  /// In en, this message translates to:
  /// **'mentioned you'**
  String get reasonMentionedYou;

  /// No description provided for @reasonMentionedGroup.
  ///
  /// In en, this message translates to:
  /// **'mentioned a group you are in'**
  String get reasonMentionedGroup;

  /// No description provided for @reasonMentionedChannel.
  ///
  /// In en, this message translates to:
  /// **'mentioned everyone here'**
  String get reasonMentionedChannel;

  /// No description provided for @reasonThreadReply.
  ///
  /// In en, this message translates to:
  /// **'reply in a thread you follow'**
  String get reasonThreadReply;

  /// No description provided for @reasonNewMessage.
  ///
  /// In en, this message translates to:
  /// **'new message'**
  String get reasonNewMessage;

  /// No description provided for @reasonMissedCall.
  ///
  /// In en, this message translates to:
  /// **'missed call'**
  String get reasonMissedCall;

  /// No description provided for @moreCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 more} other{{count} more}}'**
  String moreCount(int count);

  /// No description provided for @inThread.
  ///
  /// In en, this message translates to:
  /// **'in a thread'**
  String get inThread;

  /// No description provided for @sealedMessage.
  ///
  /// In en, this message translates to:
  /// **'Sealed message'**
  String get sealedMessage;

  /// No description provided for @someone.
  ///
  /// In en, this message translates to:
  /// **'Someone'**
  String get someone;

  /// No description provided for @conversationSettings.
  ///
  /// In en, this message translates to:
  /// **'Notifications and sidebar…'**
  String get conversationSettings;

  /// No description provided for @followThread.
  ///
  /// In en, this message translates to:
  /// **'Follow the thread'**
  String get followThread;

  /// No description provided for @ignoreThread.
  ///
  /// In en, this message translates to:
  /// **'Ignore the thread'**
  String get ignoreThread;

  /// No description provided for @stopFollowing.
  ///
  /// In en, this message translates to:
  /// **'Stop following'**
  String get stopFollowing;

  /// No description provided for @filterServer.
  ///
  /// In en, this message translates to:
  /// **'Server'**
  String get filterServer;

  /// No description provided for @filterKind.
  ///
  /// In en, this message translates to:
  /// **'Kind'**
  String get filterKind;

  /// No description provided for @itemLabel.
  ///
  /// In en, this message translates to:
  /// **'{who} in {where}, {why}, {when}'**
  String itemLabel(String who, String where, String why, String when);

  /// No description provided for @whatNotifies.
  ///
  /// In en, this message translates to:
  /// **'What notifies'**
  String get whatNotifies;

  /// No description provided for @showInSidebar.
  ///
  /// In en, this message translates to:
  /// **'Show in sidebar'**
  String get showInSidebar;

  /// No description provided for @modeAny.
  ///
  /// In en, this message translates to:
  /// **'Any activity'**
  String get modeAny;

  /// No description provided for @modeMentions.
  ///
  /// In en, this message translates to:
  /// **'Mentions'**
  String get modeMentions;

  /// No description provided for @modeNothing.
  ///
  /// In en, this message translates to:
  /// **'Nothing'**
  String get modeNothing;

  /// No description provided for @muteFor.
  ///
  /// In en, this message translates to:
  /// **'Mute for…'**
  String get muteFor;

  /// No description provided for @mutedUntil.
  ///
  /// In en, this message translates to:
  /// **'Muted until {time}'**
  String mutedUntil(String time);

  /// No description provided for @unmute.
  ///
  /// In en, this message translates to:
  /// **'Unmute'**
  String get unmute;

  /// No description provided for @showDefault.
  ///
  /// In en, this message translates to:
  /// **'App default ({current})'**
  String showDefault(String current);

  /// No description provided for @showAlways.
  ///
  /// In en, this message translates to:
  /// **'Always'**
  String get showAlways;

  /// No description provided for @showActivity.
  ///
  /// In en, this message translates to:
  /// **'When there is activity'**
  String get showActivity;

  /// No description provided for @showNotification.
  ///
  /// In en, this message translates to:
  /// **'When it notifies'**
  String get showNotification;

  /// No description provided for @showHidden.
  ///
  /// In en, this message translates to:
  /// **'Hidden'**
  String get showHidden;

  /// No description provided for @star.
  ///
  /// In en, this message translates to:
  /// **'Star'**
  String get star;

  /// No description provided for @starred.
  ///
  /// In en, this message translates to:
  /// **'Starred'**
  String get starred;

  /// No description provided for @threadView.
  ///
  /// In en, this message translates to:
  /// **'Thread view'**
  String get threadView;

  /// No description provided for @threadViewDefault.
  ///
  /// In en, this message translates to:
  /// **'Your default ({current})'**
  String threadViewDefault(String current);

  /// No description provided for @threadViewThreaded.
  ///
  /// In en, this message translates to:
  /// **'Threaded'**
  String get threadViewThreaded;

  /// No description provided for @threadViewFlat.
  ///
  /// In en, this message translates to:
  /// **'Flat'**
  String get threadViewFlat;

  /// No description provided for @sumAny.
  ///
  /// In en, this message translates to:
  /// **'Every message will notify you.'**
  String get sumAny;

  /// No description provided for @sumMentions.
  ///
  /// In en, this message translates to:
  /// **'Mentions and replies in threads you follow will notify you.'**
  String get sumMentions;

  /// No description provided for @sumNothing.
  ///
  /// In en, this message translates to:
  /// **'Nothing will notify you; a mention of you by name still counts in the badge.'**
  String get sumNothing;

  /// No description provided for @sumMuted.
  ///
  /// In en, this message translates to:
  /// **'Muted until {time}, then: {then}'**
  String sumMuted(String time, String then);

  /// No description provided for @sumAlways.
  ///
  /// In en, this message translates to:
  /// **'The conversation is always in the sidebar.'**
  String get sumAlways;

  /// No description provided for @sumActivity.
  ///
  /// In en, this message translates to:
  /// **'It appears in the sidebar only while it has something unread.'**
  String get sumActivity;

  /// No description provided for @sumNotification.
  ///
  /// In en, this message translates to:
  /// **'It appears in the sidebar only while it has a badge.'**
  String get sumNotification;

  /// No description provided for @sumHidden.
  ///
  /// In en, this message translates to:
  /// **'It has no row in the sidebar.'**
  String get sumHidden;

  /// No description provided for @sumHiddenNotifies.
  ///
  /// In en, this message translates to:
  /// **'Its notifications will be in the Notifications list.'**
  String get sumHiddenNotifies;

  /// No description provided for @muteTitle.
  ///
  /// In en, this message translates to:
  /// **'Mute this conversation'**
  String get muteTitle;

  /// No description provided for @showReadThreads.
  ///
  /// In en, this message translates to:
  /// **'Show read threads'**
  String get showReadThreads;

  /// No description provided for @noThreads.
  ///
  /// In en, this message translates to:
  /// **'No followed thread has anything new.'**
  String get noThreads;

  /// No description provided for @unreadReplies.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 unread reply} other{{count} unread replies}}'**
  String unreadReplies(int count);

  /// No description provided for @replyInPlace.
  ///
  /// In en, this message translates to:
  /// **'Reply…'**
  String get replyInPlace;

  /// No description provided for @replyHint.
  ///
  /// In en, this message translates to:
  /// **'Reply in the thread'**
  String get replyHint;

  /// No description provided for @send.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get send;

  /// No description provided for @openThread.
  ///
  /// In en, this message translates to:
  /// **'Open the thread'**
  String get openThread;

  /// No description provided for @noBookmarks.
  ///
  /// In en, this message translates to:
  /// **'No bookmarks yet. Bookmark a message from its menu to keep it here.'**
  String get noBookmarks;

  /// No description provided for @removeBookmark.
  ///
  /// In en, this message translates to:
  /// **'Remove the bookmark'**
  String get removeBookmark;

  /// No description provided for @openInPlace.
  ///
  /// In en, this message translates to:
  /// **'Open in its conversation'**
  String get openInPlace;

  /// No description provided for @bookmarkedAt.
  ///
  /// In en, this message translates to:
  /// **'Bookmarked {when}'**
  String bookmarkedAt(String when);

  /// No description provided for @messageNotLoaded.
  ///
  /// In en, this message translates to:
  /// **'The message is not on this device yet.'**
  String get messageNotLoaded;

  /// No description provided for @statusTitle.
  ///
  /// In en, this message translates to:
  /// **'Status'**
  String get statusTitle;

  /// No description provided for @statusEmoji.
  ///
  /// In en, this message translates to:
  /// **'Emoji'**
  String get statusEmoji;

  /// No description provided for @statusText.
  ///
  /// In en, this message translates to:
  /// **'What are you up to?'**
  String get statusText;

  /// No description provided for @statusClears.
  ///
  /// In en, this message translates to:
  /// **'Clears'**
  String get statusClears;

  /// No description provided for @statusClearsNever.
  ///
  /// In en, this message translates to:
  /// **'Does not clear'**
  String get statusClearsNever;

  /// No description provided for @statusClearsAt.
  ///
  /// In en, this message translates to:
  /// **'Clears {time}'**
  String statusClearsAt(String time);

  /// No description provided for @pauseNotifications.
  ///
  /// In en, this message translates to:
  /// **'Pause notifications'**
  String get pauseNotifications;

  /// No description provided for @appearAwaySwitch.
  ///
  /// In en, this message translates to:
  /// **'Appear away'**
  String get appearAwaySwitch;

  /// No description provided for @setStatus.
  ///
  /// In en, this message translates to:
  /// **'Set status'**
  String get setStatus;

  /// No description provided for @clearStatus.
  ///
  /// In en, this message translates to:
  /// **'Clear status'**
  String get clearStatus;

  /// No description provided for @saveAsPreset.
  ///
  /// In en, this message translates to:
  /// **'Save as preset'**
  String get saveAsPreset;

  /// No description provided for @presets.
  ///
  /// In en, this message translates to:
  /// **'Presets'**
  String get presets;

  /// No description provided for @sharedPresets.
  ///
  /// In en, this message translates to:
  /// **'Saved from the app'**
  String get sharedPresets;

  /// No description provided for @communityPresets.
  ///
  /// In en, this message translates to:
  /// **'From the community'**
  String get communityPresets;

  /// No description provided for @yourPresets.
  ///
  /// In en, this message translates to:
  /// **'Yours on {server}'**
  String yourPresets(String server);

  /// No description provided for @deletePreset.
  ///
  /// In en, this message translates to:
  /// **'Delete preset'**
  String get deletePreset;

  /// No description provided for @differsOn.
  ///
  /// In en, this message translates to:
  /// **'Differs on {servers}'**
  String differsOn(String servers);

  /// No description provided for @makeSame.
  ///
  /// In en, this message translates to:
  /// **'Make them the same'**
  String get makeSame;

  /// No description provided for @availability.
  ///
  /// In en, this message translates to:
  /// **'Availability'**
  String get availability;

  /// No description provided for @inMeeting.
  ///
  /// In en, this message translates to:
  /// **'In a meeting'**
  String get inMeeting;

  /// No description provided for @outOfOffice.
  ///
  /// In en, this message translates to:
  /// **'Out of office'**
  String get outOfOffice;

  /// No description provided for @availableNow.
  ///
  /// In en, this message translates to:
  /// **'Available'**
  String get availableNow;

  /// No description provided for @meetingPauses.
  ///
  /// In en, this message translates to:
  /// **'A meeting pauses my notifications'**
  String get meetingPauses;

  /// No description provided for @clearAvailability.
  ///
  /// In en, this message translates to:
  /// **'Clear'**
  String get clearAvailability;

  /// No description provided for @setUntil.
  ///
  /// In en, this message translates to:
  /// **'Until…'**
  String get setUntil;

  /// No description provided for @appearAway.
  ///
  /// In en, this message translates to:
  /// **'Appear away'**
  String get appearAway;

  /// No description provided for @appearAwayOff.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get appearAwayOff;

  /// No description provided for @appearAwayOn.
  ///
  /// In en, this message translates to:
  /// **'On, {until}'**
  String appearAwayOn(String until);

  /// No description provided for @appearAwayForever.
  ///
  /// In en, this message translates to:
  /// **'On until turned off'**
  String get appearAwayForever;

  /// No description provided for @turnOn.
  ///
  /// In en, this message translates to:
  /// **'Turn on…'**
  String get turnOn;

  /// No description provided for @turnOff.
  ///
  /// In en, this message translates to:
  /// **'Turn off'**
  String get turnOff;

  /// No description provided for @optedOutNote.
  ///
  /// In en, this message translates to:
  /// **'Not sent to servers opted out of shared status: {servers}'**
  String optedOutNote(String servers);

  /// No description provided for @dndTitle.
  ///
  /// In en, this message translates to:
  /// **'Do not disturb'**
  String get dndTitle;

  /// No description provided for @dndOnServers.
  ///
  /// In en, this message translates to:
  /// **'In do not disturb on {servers}'**
  String dndOnServers(String servers);

  /// No description provided for @dndOff.
  ///
  /// In en, this message translates to:
  /// **'You are available on every server.'**
  String get dndOff;

  /// No description provided for @dndWhyManual.
  ///
  /// In en, this message translates to:
  /// **'turned on by you'**
  String get dndWhyManual;

  /// No description provided for @dndWhyStatus.
  ///
  /// In en, this message translates to:
  /// **'your status pauses notifications'**
  String get dndWhyStatus;

  /// No description provided for @dndWhyMeeting.
  ///
  /// In en, this message translates to:
  /// **'a meeting pauses notifications'**
  String get dndWhyMeeting;

  /// No description provided for @dndWhyQuiet.
  ///
  /// In en, this message translates to:
  /// **'quiet hours'**
  String get dndWhyQuiet;

  /// No description provided for @dndWhyMuted.
  ///
  /// In en, this message translates to:
  /// **'this server is muted'**
  String get dndWhyMuted;

  /// No description provided for @dndTurnOffClears.
  ///
  /// In en, this message translates to:
  /// **'Turning it off makes you available now. It will clear: {what}'**
  String dndTurnOffClears(String what);

  /// No description provided for @dndTurnOnTitle.
  ///
  /// In en, this message translates to:
  /// **'Do not disturb'**
  String get dndTurnOnTitle;

  /// No description provided for @quietHours.
  ///
  /// In en, this message translates to:
  /// **'Quiet hours'**
  String get quietHours;

  /// No description provided for @quietOn.
  ///
  /// In en, this message translates to:
  /// **'Quiet hours on {server}'**
  String quietOn(String server);

  /// No description provided for @quietFrom.
  ///
  /// In en, this message translates to:
  /// **'From'**
  String get quietFrom;

  /// No description provided for @quietTo.
  ///
  /// In en, this message translates to:
  /// **'To'**
  String get quietTo;

  /// No description provided for @quietDays.
  ///
  /// In en, this message translates to:
  /// **'Days'**
  String get quietDays;

  /// No description provided for @quietZone.
  ///
  /// In en, this message translates to:
  /// **'Time zone: {zone}'**
  String quietZone(String zone);

  /// No description provided for @skipTonight.
  ///
  /// In en, this message translates to:
  /// **'Skip tonight'**
  String get skipTonight;

  /// No description provided for @skipped.
  ///
  /// In en, this message translates to:
  /// **'Skipped until {time}'**
  String skipped(String time);

  /// No description provided for @unskip.
  ///
  /// In en, this message translates to:
  /// **'Do not skip'**
  String get unskip;

  /// No description provided for @copyTo.
  ///
  /// In en, this message translates to:
  /// **'Copy to…'**
  String get copyTo;

  /// No description provided for @copyToTitle.
  ///
  /// In en, this message translates to:
  /// **'Copy this schedule to'**
  String get copyToTitle;

  /// No description provided for @copy.
  ///
  /// In en, this message translates to:
  /// **'Copy'**
  String get copy;

  /// No description provided for @dayMon.
  ///
  /// In en, this message translates to:
  /// **'Mon'**
  String get dayMon;

  /// No description provided for @dayTue.
  ///
  /// In en, this message translates to:
  /// **'Tue'**
  String get dayTue;

  /// No description provided for @dayWed.
  ///
  /// In en, this message translates to:
  /// **'Wed'**
  String get dayWed;

  /// No description provided for @dayThu.
  ///
  /// In en, this message translates to:
  /// **'Thu'**
  String get dayThu;

  /// No description provided for @dayFri.
  ///
  /// In en, this message translates to:
  /// **'Fri'**
  String get dayFri;

  /// No description provided for @daySat.
  ///
  /// In en, this message translates to:
  /// **'Sat'**
  String get daySat;

  /// No description provided for @daySun.
  ///
  /// In en, this message translates to:
  /// **'Sun'**
  String get daySun;

  /// No description provided for @statusIn.
  ///
  /// In en, this message translates to:
  /// **'Status on {server}'**
  String statusIn(String server);

  /// No description provided for @profile.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get profile;

  /// No description provided for @myProfile.
  ///
  /// In en, this message translates to:
  /// **'Your profile'**
  String get myProfile;

  /// No description provided for @displayName.
  ///
  /// In en, this message translates to:
  /// **'Display name'**
  String get displayName;

  /// No description provided for @title.
  ///
  /// In en, this message translates to:
  /// **'Title'**
  String get title;

  /// No description provided for @pronouns.
  ///
  /// In en, this message translates to:
  /// **'Pronouns'**
  String get pronouns;

  /// No description provided for @timeZone.
  ///
  /// In en, this message translates to:
  /// **'Time zone'**
  String get timeZone;

  /// No description provided for @useDeviceZone.
  ///
  /// In en, this message translates to:
  /// **'Use this device\'s ({zone})'**
  String useDeviceZone(String zone);

  /// No description provided for @username.
  ///
  /// In en, this message translates to:
  /// **'Username'**
  String get username;

  /// No description provided for @usernameNote.
  ///
  /// In en, this message translates to:
  /// **'A username belongs to one server and is changed on its account page.'**
  String get usernameNote;

  /// No description provided for @changeAvatar.
  ///
  /// In en, this message translates to:
  /// **'Change picture'**
  String get changeAvatar;

  /// No description provided for @removeAvatar.
  ///
  /// In en, this message translates to:
  /// **'Remove picture'**
  String get removeAvatar;

  /// No description provided for @applyTo.
  ///
  /// In en, this message translates to:
  /// **'Apply to'**
  String get applyTo;

  /// No description provided for @applyToNote.
  ///
  /// In en, this message translates to:
  /// **'Change these servers. Each server keeps its own copy of your profile.'**
  String get applyToNote;

  /// No description provided for @apply.
  ///
  /// In en, this message translates to:
  /// **'Apply'**
  String get apply;

  /// No description provided for @accountPage.
  ///
  /// In en, this message translates to:
  /// **'Passkeys, sessions and tokens…'**
  String get accountPage;

  /// No description provided for @accountPageNote.
  ///
  /// In en, this message translates to:
  /// **'Opens {server}\'s account page in your browser.'**
  String accountPageNote(String server);

  /// No description provided for @localTime.
  ///
  /// In en, this message translates to:
  /// **'Local time {time}'**
  String localTime(String time);

  /// No description provided for @presenceOnline.
  ///
  /// In en, this message translates to:
  /// **'Online'**
  String get presenceOnline;

  /// No description provided for @presenceAway.
  ///
  /// In en, this message translates to:
  /// **'Away'**
  String get presenceAway;

  /// No description provided for @presenceOffline.
  ///
  /// In en, this message translates to:
  /// **'Offline'**
  String get presenceOffline;

  /// No description provided for @inACall.
  ///
  /// In en, this message translates to:
  /// **'In a call'**
  String get inACall;

  /// No description provided for @outOfOfficeUntil.
  ///
  /// In en, this message translates to:
  /// **'Out of office until {date}'**
  String outOfOfficeUntil(String date);

  /// No description provided for @outOfOfficeNote.
  ///
  /// In en, this message translates to:
  /// **'{name} is out of office.'**
  String outOfOfficeNote(String name);

  /// No description provided for @inMeetingUntil.
  ///
  /// In en, this message translates to:
  /// **'In a meeting until {time}'**
  String inMeetingUntil(String time);

  /// No description provided for @roleLabel.
  ///
  /// In en, this message translates to:
  /// **'Role: {role}'**
  String roleLabel(String role);

  /// No description provided for @groupsLabel.
  ///
  /// In en, this message translates to:
  /// **'Groups: {groups}'**
  String groupsLabel(String groups);

  /// No description provided for @message.
  ///
  /// In en, this message translates to:
  /// **'Message'**
  String get message;

  /// No description provided for @block.
  ///
  /// In en, this message translates to:
  /// **'Block'**
  String get block;

  /// No description provided for @unblock.
  ///
  /// In en, this message translates to:
  /// **'Unblock'**
  String get unblock;

  /// No description provided for @blockConfirm.
  ///
  /// In en, this message translates to:
  /// **'Block {name}? Their messages will be collapsed for you, they cannot start a chat with you, and their mentions will not notify you. They are not told.'**
  String blockConfirm(String name);

  /// No description provided for @blocked.
  ///
  /// In en, this message translates to:
  /// **'Blocked'**
  String get blocked;

  /// No description provided for @doNotDisturbMark.
  ///
  /// In en, this message translates to:
  /// **'In do not disturb'**
  String get doNotDisturbMark;

  /// No description provided for @statusUntil.
  ///
  /// In en, this message translates to:
  /// **'{status}, until {time}'**
  String statusUntil(String status, String time);

  /// No description provided for @saved.
  ///
  /// In en, this message translates to:
  /// **'Saved.'**
  String get saved;

  /// No description provided for @serverSettings.
  ///
  /// In en, this message translates to:
  /// **'Server settings'**
  String get serverSettings;

  /// No description provided for @serverSettingsOf.
  ///
  /// In en, this message translates to:
  /// **'Settings for {server}'**
  String serverSettingsOf(String server);

  /// No description provided for @defaultThreadView.
  ///
  /// In en, this message translates to:
  /// **'Default thread view'**
  String get defaultThreadView;

  /// No description provided for @allowNotifyAnyway.
  ///
  /// In en, this message translates to:
  /// **'Let people push a message through my do not disturb (notify anyway)'**
  String get allowNotifyAnyway;

  /// No description provided for @muteServer.
  ///
  /// In en, this message translates to:
  /// **'Mute this server…'**
  String get muteServer;

  /// No description provided for @unmuteServer.
  ///
  /// In en, this message translates to:
  /// **'Unmute this server'**
  String get unmuteServer;

  /// No description provided for @serverMutedUntil.
  ///
  /// In en, this message translates to:
  /// **'Muted until {time}'**
  String serverMutedUntil(String time);

  /// No description provided for @muteServerTitle.
  ///
  /// In en, this message translates to:
  /// **'Mute {server}'**
  String muteServerTitle(String server);

  /// No description provided for @muteServerWarning.
  ///
  /// In en, this message translates to:
  /// **'People on {server} will see that you are in do not disturb.'**
  String muteServerWarning(String server);

  /// No description provided for @mute.
  ///
  /// In en, this message translates to:
  /// **'Mute'**
  String get mute;

  /// No description provided for @takePartShared.
  ///
  /// In en, this message translates to:
  /// **'Take part in shared status and do not disturb'**
  String get takePartShared;

  /// No description provided for @takePartSharedNote.
  ///
  /// In en, this message translates to:
  /// **'Off: status and do not disturb set from the app leave this server alone; set them here instead.'**
  String get takePartSharedNote;

  /// No description provided for @quietHoursLink.
  ///
  /// In en, this message translates to:
  /// **'Quiet hours and do not disturb…'**
  String get quietHoursLink;

  /// No description provided for @statusLink.
  ///
  /// In en, this message translates to:
  /// **'Status on this server…'**
  String get statusLink;

  /// No description provided for @conversationsTable.
  ///
  /// In en, this message translates to:
  /// **'Conversations'**
  String get conversationsTable;

  /// No description provided for @selectedCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 selected} other{{count} selected}}'**
  String selectedCount(int count);

  /// No description provided for @setModeFor.
  ///
  /// In en, this message translates to:
  /// **'What notifies…'**
  String get setModeFor;

  /// No description provided for @setShowFor.
  ///
  /// In en, this message translates to:
  /// **'Show in sidebar…'**
  String get setShowFor;

  /// No description provided for @sessionsAndDevices.
  ///
  /// In en, this message translates to:
  /// **'Sessions and devices…'**
  String get sessionsAndDevices;

  /// No description provided for @signOut.
  ///
  /// In en, this message translates to:
  /// **'Sign out'**
  String get signOut;

  /// No description provided for @signOutConfirm.
  ///
  /// In en, this message translates to:
  /// **'Sign out of {server}? Its messages and files are removed from this device.'**
  String signOutConfirm(String server);

  /// No description provided for @sessions.
  ///
  /// In en, this message translates to:
  /// **'Sessions'**
  String get sessions;

  /// No description provided for @devices.
  ///
  /// In en, this message translates to:
  /// **'Devices'**
  String get devices;

  /// No description provided for @thisDevice.
  ///
  /// In en, this message translates to:
  /// **'This device'**
  String get thisDevice;

  /// No description provided for @endSession.
  ///
  /// In en, this message translates to:
  /// **'Sign out'**
  String get endSession;

  /// No description provided for @removeDevice.
  ///
  /// In en, this message translates to:
  /// **'Remove'**
  String get removeDevice;

  /// No description provided for @lastSeen.
  ///
  /// In en, this message translates to:
  /// **'Last seen {when}'**
  String lastSeen(String when);

  /// No description provided for @retired.
  ///
  /// In en, this message translates to:
  /// **'Retired'**
  String get retired;

  /// No description provided for @kindWeb.
  ///
  /// In en, this message translates to:
  /// **'Browser'**
  String get kindWeb;

  /// No description provided for @kindApp.
  ///
  /// In en, this message translates to:
  /// **'App'**
  String get kindApp;

  /// No description provided for @kindPersonal.
  ///
  /// In en, this message translates to:
  /// **'Personal token'**
  String get kindPersonal;

  /// No description provided for @kindBot.
  ///
  /// In en, this message translates to:
  /// **'Bot'**
  String get kindBot;

  /// No description provided for @freshCheckNeeded.
  ///
  /// In en, this message translates to:
  /// **'This needs your passkey: it is done on the account page in your browser.'**
  String get freshCheckNeeded;

  /// No description provided for @openAccountPage.
  ///
  /// In en, this message translates to:
  /// **'Open the account page'**
  String get openAccountPage;

  /// No description provided for @signedOutBanner.
  ///
  /// In en, this message translates to:
  /// **'Signed out of {server}'**
  String signedOutBanner(String server);

  /// No description provided for @signInAgain.
  ///
  /// In en, this message translates to:
  /// **'Sign in again'**
  String get signInAgain;

  /// No description provided for @reSignInTitle.
  ///
  /// In en, this message translates to:
  /// **'Sign in to {server} again'**
  String reSignInTitle(String server);

  /// No description provided for @reSignInBody.
  ///
  /// In en, this message translates to:
  /// **'The server ended this device\'s session, as company sign-in does each day. What is on this device is kept until you sign in again as the same account.'**
  String get reSignInBody;

  /// No description provided for @reSignInOther.
  ///
  /// In en, this message translates to:
  /// **'You signed in as a different account, so what this device kept for the old one was removed.'**
  String get reSignInOther;

  /// No description provided for @signInWithBrowser.
  ///
  /// In en, this message translates to:
  /// **'Sign in with your browser'**
  String get signInWithBrowser;

  /// No description provided for @scanCode.
  ///
  /// In en, this message translates to:
  /// **'Scan a code'**
  String get scanCode;

  /// No description provided for @copyFromDevice.
  ///
  /// In en, this message translates to:
  /// **'Copy from another device'**
  String get copyFromDevice;

  /// No description provided for @inviteLink.
  ///
  /// In en, this message translates to:
  /// **'Invite link'**
  String get inviteLink;

  /// No description provided for @inviteJoinNote.
  ///
  /// In en, this message translates to:
  /// **'This is an invite. Join in your browser, then sign in here.'**
  String get inviteJoinNote;

  /// No description provided for @joinInBrowser.
  ///
  /// In en, this message translates to:
  /// **'Join in your browser'**
  String get joinInBrowser;

  /// No description provided for @pasteCode.
  ///
  /// In en, this message translates to:
  /// **'Or paste the code\'s text'**
  String get pasteCode;

  /// No description provided for @pairingWaiting.
  ///
  /// In en, this message translates to:
  /// **'Confirm “{device}” in your browser to finish signing in.'**
  String pairingWaiting(String device);

  /// No description provided for @pairingExpired.
  ///
  /// In en, this message translates to:
  /// **'The code ran out. Make a new one on the account page.'**
  String get pairingExpired;

  /// No description provided for @notAChurrCode.
  ///
  /// In en, this message translates to:
  /// **'That is not a Churr code.'**
  String get notAChurrCode;

  /// No description provided for @cameraUnavailable.
  ///
  /// In en, this message translates to:
  /// **'The camera is not available here. Paste the code\'s text instead.'**
  String get cameraUnavailable;

  /// No description provided for @setUpAnotherDevice.
  ///
  /// In en, this message translates to:
  /// **'Set up another device'**
  String get setUpAnotherDevice;

  /// No description provided for @setUpNote.
  ///
  /// In en, this message translates to:
  /// **'Scan this on the new device under Add a server → Copy from another device. It holds your app settings and the addresses of your servers. It holds no password, token or key.'**
  String get setUpNote;

  /// No description provided for @setupApplied.
  ///
  /// In en, this message translates to:
  /// **'Your settings are in place. Sign in to each server in turn.'**
  String get setupApplied;

  /// No description provided for @setupServers.
  ///
  /// In en, this message translates to:
  /// **'Servers to sign in to'**
  String get setupServers;

  /// No description provided for @skip.
  ///
  /// In en, this message translates to:
  /// **'Skip'**
  String get skip;

  /// No description provided for @signedIn.
  ///
  /// In en, this message translates to:
  /// **'Signed in'**
  String get signedIn;

  /// No description provided for @addAServer.
  ///
  /// In en, this message translates to:
  /// **'Add a server'**
  String get addAServer;

  /// No description provided for @codePart.
  ///
  /// In en, this message translates to:
  /// **'Code {n} of {count}'**
  String codePart(int n, int count);

  /// No description provided for @setupReceived.
  ///
  /// In en, this message translates to:
  /// **'{n} of {count} codes scanned'**
  String setupReceived(int n, int count);

  /// No description provided for @addServerToBegin.
  ///
  /// In en, this message translates to:
  /// **'Add a server to begin.'**
  String get addServerToBegin;

  /// No description provided for @serverAddress.
  ///
  /// In en, this message translates to:
  /// **'Server\'s address'**
  String get serverAddress;

  /// No description provided for @serverAddressHint.
  ///
  /// In en, this message translates to:
  /// **'chat.example.org, or an invite link'**
  String get serverAddressHint;

  /// No description provided for @continueButton.
  ///
  /// In en, this message translates to:
  /// **'Continue'**
  String get continueButton;

  /// No description provided for @couldNotReach.
  ///
  /// In en, this message translates to:
  /// **'Could not reach that server.'**
  String get couldNotReach;

  /// No description provided for @signInDidNotFinish.
  ///
  /// In en, this message translates to:
  /// **'Sign-in did not finish ({error}).'**
  String signInDidNotFinish(String error);

  /// No description provided for @enterAddress.
  ///
  /// In en, this message translates to:
  /// **'Enter the server\'s address, such as chat.example.org.'**
  String get enterAddress;

  /// No description provided for @notSetUp.
  ///
  /// In en, this message translates to:
  /// **'This server has not been set up yet.'**
  String get notSetUp;

  /// No description provided for @serverVersion.
  ///
  /// In en, this message translates to:
  /// **'{host} · Churr {version}'**
  String serverVersion(String host, String version);

  /// No description provided for @available.
  ///
  /// In en, this message translates to:
  /// **'available'**
  String get available;

  /// No description provided for @doNotDisturb.
  ///
  /// In en, this message translates to:
  /// **'do not disturb'**
  String get doNotDisturb;

  /// No description provided for @dndToggleOn.
  ///
  /// In en, this message translates to:
  /// **'Turn on do not disturb'**
  String get dndToggleOn;

  /// No description provided for @dndToggleOff.
  ///
  /// In en, this message translates to:
  /// **'Turn off do not disturb'**
  String get dndToggleOff;

  /// No description provided for @setAStatus.
  ///
  /// In en, this message translates to:
  /// **'Set a status'**
  String get setAStatus;

  /// No description provided for @yourStatus.
  ///
  /// In en, this message translates to:
  /// **'Status, availability and appear away'**
  String get yourStatus;

  /// No description provided for @serverAdminInBrowser.
  ///
  /// In en, this message translates to:
  /// **'Server settings in the browser'**
  String get serverAdminInBrowser;

  /// No description provided for @moveUp.
  ///
  /// In en, this message translates to:
  /// **'Move up'**
  String get moveUp;

  /// No description provided for @moveDown.
  ///
  /// In en, this message translates to:
  /// **'Move down'**
  String get moveDown;

  /// No description provided for @muted.
  ///
  /// In en, this message translates to:
  /// **'Muted'**
  String get muted;

  /// No description provided for @notConnected.
  ///
  /// In en, this message translates to:
  /// **'Not connected'**
  String get notConnected;

  /// No description provided for @serversMenu.
  ///
  /// In en, this message translates to:
  /// **'Servers'**
  String get serversMenu;

  /// No description provided for @everythingElse.
  ///
  /// In en, this message translates to:
  /// **'Everything else'**
  String get everythingElse;

  /// No description provided for @newGroup.
  ///
  /// In en, this message translates to:
  /// **'New group'**
  String get newGroup;

  /// No description provided for @groupName.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get groupName;

  /// No description provided for @create.
  ///
  /// In en, this message translates to:
  /// **'Create'**
  String get create;

  /// No description provided for @deleteGroup.
  ///
  /// In en, this message translates to:
  /// **'Delete group {name}'**
  String deleteGroup(String name);

  /// No description provided for @moveGroupUp.
  ///
  /// In en, this message translates to:
  /// **'Move group up'**
  String get moveGroupUp;

  /// No description provided for @moveGroupDown.
  ///
  /// In en, this message translates to:
  /// **'Move group down'**
  String get moveGroupDown;

  /// No description provided for @appSettings.
  ///
  /// In en, this message translates to:
  /// **'App settings'**
  String get appSettings;

  /// No description provided for @sidebarLayout.
  ///
  /// In en, this message translates to:
  /// **'Sidebar layout'**
  String get sidebarLayout;

  /// No description provided for @layoutMerged.
  ///
  /// In en, this message translates to:
  /// **'Merged tree'**
  String get layoutMerged;

  /// No description provided for @layoutRail.
  ///
  /// In en, this message translates to:
  /// **'Server rail'**
  String get layoutRail;

  /// No description provided for @layoutHybrid.
  ///
  /// In en, this message translates to:
  /// **'Hybrid'**
  String get layoutHybrid;

  /// No description provided for @showByDefault.
  ///
  /// In en, this message translates to:
  /// **'Show in sidebar, by default'**
  String get showByDefault;

  /// No description provided for @showHiddenSwitch.
  ///
  /// In en, this message translates to:
  /// **'Show hidden'**
  String get showHiddenSwitch;

  /// No description provided for @showHiddenNote.
  ///
  /// In en, this message translates to:
  /// **'Draw conversations set to Hidden, in italics with a hidden tag.'**
  String get showHiddenNote;

  /// No description provided for @serverOrder.
  ///
  /// In en, this message translates to:
  /// **'Order of servers'**
  String get serverOrder;

  /// No description provided for @servers.
  ///
  /// In en, this message translates to:
  /// **'Servers'**
  String get servers;

  /// No description provided for @palette.
  ///
  /// In en, this message translates to:
  /// **'Colours'**
  String get palette;

  /// No description provided for @paletteStandard.
  ///
  /// In en, this message translates to:
  /// **'Standard'**
  String get paletteStandard;

  /// No description provided for @paletteHighDistinction.
  ///
  /// In en, this message translates to:
  /// **'High distinction'**
  String get paletteHighDistinction;

  /// No description provided for @paletteNote.
  ///
  /// In en, this message translates to:
  /// **'High distinction swaps pink for orange and lavender for blue, so the colours stay apart with red-green colour blindness. The app also follows the system\'s \"increase contrast\" setting.'**
  String get paletteNote;

  /// No description provided for @theme.
  ///
  /// In en, this message translates to:
  /// **'Theme'**
  String get theme;

  /// No description provided for @themeDark.
  ///
  /// In en, this message translates to:
  /// **'Dark'**
  String get themeDark;

  /// No description provided for @themeLight.
  ///
  /// In en, this message translates to:
  /// **'Light'**
  String get themeLight;

  /// No description provided for @themeSystem.
  ///
  /// In en, this message translates to:
  /// **'Follow the system'**
  String get themeSystem;

  /// No description provided for @textSize.
  ///
  /// In en, this message translates to:
  /// **'Text size'**
  String get textSize;

  /// No description provided for @enterSends.
  ///
  /// In en, this message translates to:
  /// **'Enter sends'**
  String get enterSends;

  /// No description provided for @enterSendsNote.
  ///
  /// In en, this message translates to:
  /// **'Shift+Enter makes a new line. Phones always send with the button.'**
  String get enterSendsNote;

  /// No description provided for @sounds.
  ///
  /// In en, this message translates to:
  /// **'Sounds'**
  String get sounds;

  /// No description provided for @startupSound.
  ///
  /// In en, this message translates to:
  /// **'Play the start-up sound when the app opens'**
  String get startupSound;

  /// No description provided for @notificationsSection.
  ///
  /// In en, this message translates to:
  /// **'Notifications on this device'**
  String get notificationsSection;

  /// No description provided for @showNotifications.
  ///
  /// In en, this message translates to:
  /// **'Show notifications'**
  String get showNotifications;

  /// No description provided for @notificationSoundsSwitch.
  ///
  /// In en, this message translates to:
  /// **'Notification sounds'**
  String get notificationSoundsSwitch;

  /// No description provided for @notificationTextSwitch.
  ///
  /// In en, this message translates to:
  /// **'Show the message text (off: only who sent it)'**
  String get notificationTextSwitch;

  /// No description provided for @language.
  ///
  /// In en, this message translates to:
  /// **'Language'**
  String get language;

  /// No description provided for @languageSystem.
  ///
  /// In en, this message translates to:
  /// **'Follow the system'**
  String get languageSystem;

  /// No description provided for @languageEnglish.
  ///
  /// In en, this message translates to:
  /// **'English'**
  String get languageEnglish;

  /// No description provided for @fileCache.
  ///
  /// In en, this message translates to:
  /// **'File cache'**
  String get fileCache;

  /// No description provided for @fileCacheSize.
  ///
  /// In en, this message translates to:
  /// **'{size} used of {cap}'**
  String fileCacheSize(String size, String cap);

  /// No description provided for @fileCacheCap.
  ///
  /// In en, this message translates to:
  /// **'Keep at most'**
  String get fileCacheCap;

  /// No description provided for @clearCache.
  ///
  /// In en, this message translates to:
  /// **'Clear cache'**
  String get clearCache;

  /// No description provided for @cacheCleared.
  ///
  /// In en, this message translates to:
  /// **'Cache cleared.'**
  String get cacheCleared;

  /// No description provided for @about.
  ///
  /// In en, this message translates to:
  /// **'About'**
  String get about;

  /// No description provided for @licences.
  ///
  /// In en, this message translates to:
  /// **'Licences'**
  String get licences;

  /// No description provided for @sourceCode.
  ///
  /// In en, this message translates to:
  /// **'Source code'**
  String get sourceCode;

  /// No description provided for @appVersion.
  ///
  /// In en, this message translates to:
  /// **'Version {version}'**
  String appVersion(String version);

  /// No description provided for @settingsFor.
  ///
  /// In en, this message translates to:
  /// **'Settings…'**
  String get settingsFor;

  /// No description provided for @arrivedLate.
  ///
  /// In en, this message translates to:
  /// **'arrived late'**
  String get arrivedLate;

  /// No description provided for @arrivedLateExplained.
  ///
  /// In en, this message translates to:
  /// **'Arrived late, after newer messages were already here'**
  String get arrivedLateExplained;

  /// No description provided for @notSentToOtherServers.
  ///
  /// In en, this message translates to:
  /// **'Not yet passed on to the other servers'**
  String get notSentToOtherServers;

  /// No description provided for @themes.
  ///
  /// In en, this message translates to:
  /// **'Themes'**
  String get themes;

  /// No description provided for @themesAndEditor.
  ///
  /// In en, this message translates to:
  /// **'Themes and the theme editor'**
  String get themesAndEditor;

  /// No description provided for @themesNote.
  ///
  /// In en, this message translates to:
  /// **'Saved themes live on the server they were made on. The choices above stay on this device.'**
  String get themesNote;

  /// No description provided for @darkTheme.
  ///
  /// In en, this message translates to:
  /// **'Dark theme'**
  String get darkTheme;

  /// No description provided for @lightTheme.
  ///
  /// In en, this message translates to:
  /// **'Light theme'**
  String get lightTheme;

  /// No description provided for @change.
  ///
  /// In en, this message translates to:
  /// **'Change'**
  String get change;

  /// No description provided for @chooseDarkTheme.
  ///
  /// In en, this message translates to:
  /// **'Choose the dark theme'**
  String get chooseDarkTheme;

  /// No description provided for @chooseLightTheme.
  ///
  /// In en, this message translates to:
  /// **'Choose the light theme'**
  String get chooseLightTheme;

  /// No description provided for @themePerServer.
  ///
  /// In en, this message translates to:
  /// **'Use each server\'s default'**
  String get themePerServer;

  /// No description provided for @themePerServerNote.
  ///
  /// In en, this message translates to:
  /// **'Each server looks like itself: its own default theme while you are in it, where it has one, and your choice otherwise.'**
  String get themePerServerNote;

  /// No description provided for @themeOnServer.
  ///
  /// In en, this message translates to:
  /// **'{name} on {server}'**
  String themeOnServer(String name, String server);

  /// No description provided for @themeBuiltInDark.
  ///
  /// In en, this message translates to:
  /// **'Dark'**
  String get themeBuiltInDark;

  /// No description provided for @themeBuiltInLight.
  ///
  /// In en, this message translates to:
  /// **'Light'**
  String get themeBuiltInLight;

  /// No description provided for @themeBuiltInDarkHd.
  ///
  /// In en, this message translates to:
  /// **'Dark, high distinction'**
  String get themeBuiltInDarkHd;

  /// No description provided for @themeBuiltInLightHd.
  ///
  /// In en, this message translates to:
  /// **'Light, high distinction'**
  String get themeBuiltInLightHd;

  /// No description provided for @kindDarkTag.
  ///
  /// In en, this message translates to:
  /// **'dark'**
  String get kindDarkTag;

  /// No description provided for @kindLightTag.
  ///
  /// In en, this message translates to:
  /// **'light'**
  String get kindLightTag;

  /// No description provided for @builtInLabel.
  ///
  /// In en, this message translates to:
  /// **'Built in'**
  String get builtInLabel;

  /// No description provided for @builtInThemes.
  ///
  /// In en, this message translates to:
  /// **'Built-in themes'**
  String get builtInThemes;

  /// No description provided for @publishedLabel.
  ///
  /// In en, this message translates to:
  /// **'in the library'**
  String get publishedLabel;

  /// No description provided for @inUse.
  ///
  /// In en, this message translates to:
  /// **'in use'**
  String get inUse;

  /// No description provided for @savedOn.
  ///
  /// In en, this message translates to:
  /// **'Saved on {server}'**
  String savedOn(String server);

  /// No description provided for @themesCouldNotLoad.
  ///
  /// In en, this message translates to:
  /// **'Could not load the themes saved on this server.'**
  String get themesCouldNotLoad;

  /// No description provided for @loading.
  ///
  /// In en, this message translates to:
  /// **'Loading…'**
  String get loading;

  /// No description provided for @noSavedThemes.
  ///
  /// In en, this message translates to:
  /// **'No saved themes here yet.'**
  String get noSavedThemes;

  /// No description provided for @newTheme.
  ///
  /// In en, this message translates to:
  /// **'New theme'**
  String get newTheme;

  /// No description provided for @moreActions.
  ///
  /// In en, this message translates to:
  /// **'More actions'**
  String get moreActions;

  /// No description provided for @themeUse.
  ///
  /// In en, this message translates to:
  /// **'Use'**
  String get themeUse;

  /// No description provided for @themeTry.
  ///
  /// In en, this message translates to:
  /// **'Try'**
  String get themeTry;

  /// No description provided for @themeAdd.
  ///
  /// In en, this message translates to:
  /// **'Add'**
  String get themeAdd;

  /// No description provided for @themeEdit.
  ///
  /// In en, this message translates to:
  /// **'Edit'**
  String get themeEdit;

  /// No description provided for @themeEditACopy.
  ///
  /// In en, this message translates to:
  /// **'Edit a copy'**
  String get themeEditACopy;

  /// No description provided for @themeExport.
  ///
  /// In en, this message translates to:
  /// **'Export a file'**
  String get themeExport;

  /// No description provided for @themeImport.
  ///
  /// In en, this message translates to:
  /// **'Import a file'**
  String get themeImport;

  /// No description provided for @themeShareLink.
  ///
  /// In en, this message translates to:
  /// **'Copy its link'**
  String get themeShareLink;

  /// No description provided for @themePublish.
  ///
  /// In en, this message translates to:
  /// **'Publish'**
  String get themePublish;

  /// No description provided for @themeUnpublish.
  ///
  /// In en, this message translates to:
  /// **'Unpublish'**
  String get themeUnpublish;

  /// No description provided for @themeDelete.
  ///
  /// In en, this message translates to:
  /// **'Delete'**
  String get themeDelete;

  /// No description provided for @themeLibraryOf.
  ///
  /// In en, this message translates to:
  /// **'Theme library of {server}'**
  String themeLibraryOf(String server);

  /// No description provided for @themeCardLabel.
  ///
  /// In en, this message translates to:
  /// **'Theme'**
  String get themeCardLabel;

  /// No description provided for @themeBy.
  ///
  /// In en, this message translates to:
  /// **'by {name}'**
  String themeBy(String name);

  /// No description provided for @themeCannotApply.
  ///
  /// In en, this message translates to:
  /// **'This theme cannot be applied: {problem}'**
  String themeCannotApply(String problem);

  /// No description provided for @themeAdded.
  ///
  /// In en, this message translates to:
  /// **'Added to your themes on {server}.'**
  String themeAdded(String server);

  /// No description provided for @themeTrying.
  ///
  /// In en, this message translates to:
  /// **'Trying “{name}”. Nothing is saved.'**
  String themeTrying(String name);

  /// No description provided for @themeKeep.
  ///
  /// In en, this message translates to:
  /// **'Keep it'**
  String get themeKeep;

  /// No description provided for @themeStopTrying.
  ///
  /// In en, this message translates to:
  /// **'Stop'**
  String get themeStopTrying;

  /// No description provided for @themeGone.
  ///
  /// In en, this message translates to:
  /// **'This theme no longer exists.'**
  String get themeGone;

  /// No description provided for @themeFileLabel.
  ///
  /// In en, this message translates to:
  /// **'Churr theme'**
  String get themeFileLabel;

  /// No description provided for @themeExported.
  ///
  /// In en, this message translates to:
  /// **'Saved {file}.'**
  String themeExported(String file);

  /// No description provided for @themeImported.
  ///
  /// In en, this message translates to:
  /// **'Imported “{name}”. Save it to keep it.'**
  String themeImported(String name);

  /// No description provided for @themeLinkCopied.
  ///
  /// In en, this message translates to:
  /// **'The theme\'s link is copied. Anyone signed in to its server can open it.'**
  String get themeLinkCopied;

  /// No description provided for @themeEditor.
  ///
  /// In en, this message translates to:
  /// **'Theme editor'**
  String get themeEditor;

  /// No description provided for @themeNameLabel.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get themeNameLabel;

  /// No description provided for @themeCopyName.
  ///
  /// In en, this message translates to:
  /// **'{name} copy'**
  String themeCopyName(String name);

  /// No description provided for @themeCopyMade.
  ///
  /// In en, this message translates to:
  /// **'This is now a new, unsaved copy.'**
  String get themeCopyMade;

  /// No description provided for @themeKind.
  ///
  /// In en, this message translates to:
  /// **'Kind'**
  String get themeKind;

  /// No description provided for @themeKindDarkProblem.
  ///
  /// In en, this message translates to:
  /// **'A dark theme needs a ground darker than its text.'**
  String get themeKindDarkProblem;

  /// No description provided for @themeKindLightProblem.
  ///
  /// In en, this message translates to:
  /// **'A light theme needs a ground lighter than its text.'**
  String get themeKindLightProblem;

  /// No description provided for @themeColours.
  ///
  /// In en, this message translates to:
  /// **'Base colours'**
  String get themeColours;

  /// No description provided for @baseBg.
  ///
  /// In en, this message translates to:
  /// **'Ground'**
  String get baseBg;

  /// No description provided for @basePanel.
  ///
  /// In en, this message translates to:
  /// **'Panel'**
  String get basePanel;

  /// No description provided for @baseRaise.
  ///
  /// In en, this message translates to:
  /// **'Raised'**
  String get baseRaise;

  /// No description provided for @baseInk.
  ///
  /// In en, this message translates to:
  /// **'Text'**
  String get baseInk;

  /// No description provided for @baseDim.
  ///
  /// In en, this message translates to:
  /// **'Secondary text'**
  String get baseDim;

  /// No description provided for @baseLav.
  ///
  /// In en, this message translates to:
  /// **'Lavender (yours)'**
  String get baseLav;

  /// No description provided for @baseCyan.
  ///
  /// In en, this message translates to:
  /// **'Cyan (go)'**
  String get baseCyan;

  /// No description provided for @basePink.
  ///
  /// In en, this message translates to:
  /// **'Pink (attention)'**
  String get basePink;

  /// No description provided for @baseYel.
  ///
  /// In en, this message translates to:
  /// **'Yellow (counts)'**
  String get baseYel;

  /// No description provided for @hexInvalid.
  ///
  /// In en, this message translates to:
  /// **'#rrggbb'**
  String get hexInvalid;

  /// No description provided for @hslPicker.
  ///
  /// In en, this message translates to:
  /// **'Hue, saturation and lightness'**
  String get hslPicker;

  /// No description provided for @hue.
  ///
  /// In en, this message translates to:
  /// **'Hue'**
  String get hue;

  /// No description provided for @saturation.
  ///
  /// In en, this message translates to:
  /// **'Saturation'**
  String get saturation;

  /// No description provided for @lightness.
  ///
  /// In en, this message translates to:
  /// **'Lightness'**
  String get lightness;

  /// No description provided for @themeContrast.
  ///
  /// In en, this message translates to:
  /// **'Contrast'**
  String get themeContrast;

  /// No description provided for @themeAllPass.
  ///
  /// In en, this message translates to:
  /// **'All {count} pairings pass.'**
  String themeAllPass(int count);

  /// No description provided for @themeSomeFail.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 pairing fails. A theme that fails cannot be saved, shared or applied.} other{{count} pairings fail. A theme that fails cannot be saved, shared or applied.}}'**
  String themeSomeFail(int count);

  /// No description provided for @needText.
  ///
  /// In en, this message translates to:
  /// **'text: needs 4.5:1'**
  String get needText;

  /// No description provided for @needEdge.
  ///
  /// In en, this message translates to:
  /// **'edge or icon: needs 3:1'**
  String get needEdge;

  /// No description provided for @pass.
  ///
  /// In en, this message translates to:
  /// **'pass'**
  String get pass;

  /// No description provided for @fail.
  ///
  /// In en, this message translates to:
  /// **'fail'**
  String get fail;

  /// No description provided for @fix.
  ///
  /// In en, this message translates to:
  /// **'Fix'**
  String get fix;

  /// No description provided for @fixedColour.
  ///
  /// In en, this message translates to:
  /// **'{colour} is now {hex}: the smallest change that passes.'**
  String fixedColour(String colour, String hex);

  /// No description provided for @cannotFix.
  ///
  /// In en, this message translates to:
  /// **'No change to {colour} alone passes. Change another colour first.'**
  String cannotFix(String colour);

  /// No description provided for @themeSaveSection.
  ///
  /// In en, this message translates to:
  /// **'Save and share'**
  String get themeSaveSection;

  /// No description provided for @themeSaveTo.
  ///
  /// In en, this message translates to:
  /// **'Save to'**
  String get themeSaveTo;

  /// No description provided for @themeNoServer.
  ///
  /// In en, this message translates to:
  /// **'Choose a server'**
  String get themeNoServer;

  /// No description provided for @themeIsSaved.
  ///
  /// In en, this message translates to:
  /// **'Saved. Only you can see it.'**
  String get themeIsSaved;

  /// No description provided for @themeIsPublished.
  ///
  /// In en, this message translates to:
  /// **'Saved and in the server\'s theme library.'**
  String get themeIsPublished;

  /// No description provided for @themeSavedOn.
  ///
  /// In en, this message translates to:
  /// **'Saved on {server}.'**
  String themeSavedOn(String server);

  /// No description provided for @themeCannotSaveYet.
  ///
  /// In en, this message translates to:
  /// **'It can be saved once it has a name and every pairing passes.'**
  String get themeCannotSaveYet;

  /// No description provided for @themePublishedNote.
  ///
  /// In en, this message translates to:
  /// **'Published: everyone on the server can find it in the library.'**
  String get themePublishedNote;

  /// No description provided for @themeUnpublishedNote.
  ///
  /// In en, this message translates to:
  /// **'Taken out of the library. Copies people added stay theirs.'**
  String get themeUnpublishedNote;

  /// No description provided for @themeDeleteTitle.
  ///
  /// In en, this message translates to:
  /// **'Delete “{name}”?'**
  String themeDeleteTitle(String name);

  /// No description provided for @themeDeleteBody.
  ///
  /// In en, this message translates to:
  /// **'It leaves the library if it is there. Copies others added and theme cards in messages stay.'**
  String get themeDeleteBody;

  /// No description provided for @themeDeleted.
  ///
  /// In en, this message translates to:
  /// **'Deleted.'**
  String get themeDeleted;

  /// No description provided for @searchThemes.
  ///
  /// In en, this message translates to:
  /// **'Search by name'**
  String get searchThemes;

  /// No description provided for @libraryEmpty.
  ///
  /// In en, this message translates to:
  /// **'Nobody has published a theme here yet.'**
  String get libraryEmpty;

  /// No description provided for @libraryNoMatch.
  ///
  /// In en, this message translates to:
  /// **'No theme here has that in its name.'**
  String get libraryNoMatch;

  /// No description provided for @showMore.
  ///
  /// In en, this message translates to:
  /// **'Show more'**
  String get showMore;

  /// No description provided for @makeDefaultDark.
  ///
  /// In en, this message translates to:
  /// **'Make it the server\'s default dark theme'**
  String get makeDefaultDark;

  /// No description provided for @makeDefaultLight.
  ///
  /// In en, this message translates to:
  /// **'Make it the server\'s default light theme'**
  String get makeDefaultLight;

  /// No description provided for @removeFromLibrary.
  ///
  /// In en, this message translates to:
  /// **'Remove from the library'**
  String get removeFromLibrary;

  /// No description provided for @defaultSet.
  ///
  /// In en, this message translates to:
  /// **'“{name}” is now this server\'s default.'**
  String defaultSet(String name);

  /// No description provided for @previewCommunity.
  ///
  /// In en, this message translates to:
  /// **'Northwind'**
  String get previewCommunity;

  /// No description provided for @previewConnected.
  ///
  /// In en, this message translates to:
  /// **'connected'**
  String get previewConnected;

  /// No description provided for @previewSection.
  ///
  /// In en, this message translates to:
  /// **'Channels'**
  String get previewSection;

  /// No description provided for @previewHiddenTag.
  ///
  /// In en, this message translates to:
  /// **'hidden'**
  String get previewHiddenTag;

  /// No description provided for @previewTopic.
  ///
  /// In en, this message translates to:
  /// **'Dates, owners and the launch list'**
  String get previewTopic;

  /// No description provided for @previewMessage1.
  ///
  /// In en, this message translates to:
  /// **'The launch list is in the **planning doc**, see [the schedule](https://example.org) and `release.toml`.'**
  String get previewMessage1;

  /// No description provided for @previewMessage2.
  ///
  /// In en, this message translates to:
  /// **'can you check the dates by Friday?'**
  String get previewMessage2;

  /// No description provided for @previewThread.
  ///
  /// In en, this message translates to:
  /// **'2 new replies'**
  String get previewThread;

  /// No description provided for @previewPoll.
  ///
  /// In en, this message translates to:
  /// **'Which day for the review?'**
  String get previewPoll;

  /// No description provided for @previewPollA.
  ///
  /// In en, this message translates to:
  /// **'Thursday'**
  String get previewPollA;

  /// No description provided for @previewPollB.
  ///
  /// In en, this message translates to:
  /// **'Friday'**
  String get previewPollB;

  /// No description provided for @previewJoin.
  ///
  /// In en, this message translates to:
  /// **'Join'**
  String get previewJoin;

  /// No description provided for @previewSecondary.
  ///
  /// In en, this message translates to:
  /// **'Later'**
  String get previewSecondary;

  /// No description provided for @previewDanger.
  ///
  /// In en, this message translates to:
  /// **'Delete'**
  String get previewDanger;

  /// No description provided for @previewTagPrivate.
  ///
  /// In en, this message translates to:
  /// **'private'**
  String get previewTagPrivate;

  /// No description provided for @previewTagOn.
  ///
  /// In en, this message translates to:
  /// **'on'**
  String get previewTagOn;

  /// No description provided for @previewTagFailed.
  ///
  /// In en, this message translates to:
  /// **'failed'**
  String get previewTagFailed;

  /// No description provided for @previewTagNew.
  ///
  /// In en, this message translates to:
  /// **'new'**
  String get previewTagNew;

  /// No description provided for @previewComposer.
  ///
  /// In en, this message translates to:
  /// **'Message #planning'**
  String get previewComposer;

  /// No description provided for @hubsEntry.
  ///
  /// In en, this message translates to:
  /// **'Hubs'**
  String get hubsEntry;

  /// No description provided for @hubsEntryNote.
  ///
  /// In en, this message translates to:
  /// **'Channels on the hubs this server is on'**
  String get hubsEntryNote;

  /// No description provided for @hubsOf.
  ///
  /// In en, this message translates to:
  /// **'Hubs of {server}'**
  String hubsOf(String server);

  /// No description provided for @hubsNote.
  ///
  /// In en, this message translates to:
  /// **'A hub carries channels between many servers and owns the channels on it. Linking this server to a hub is done in the web admin console.'**
  String get hubsNote;

  /// No description provided for @linkInConsole.
  ///
  /// In en, this message translates to:
  /// **'Link servers in the admin console'**
  String get linkInConsole;

  /// No description provided for @noHubs.
  ///
  /// In en, this message translates to:
  /// **'This server is not on a hub.'**
  String get noHubs;

  /// No description provided for @hubConnected.
  ///
  /// In en, this message translates to:
  /// **'connected'**
  String get hubConnected;

  /// No description provided for @hubDirectory.
  ///
  /// In en, this message translates to:
  /// **'Directory'**
  String get hubDirectory;

  /// No description provided for @clockFigures.
  ///
  /// In en, this message translates to:
  /// **'The hub\'s clock: this server is {offset} ms off, within {uncertainty} ms; round trip {latency} ms, jitter {jitter} ms ({samples} samples).'**
  String clockFigures(
    int offset,
    int uncertainty,
    int latency,
    int jitter,
    int samples,
  );

  /// No description provided for @clockSynced.
  ///
  /// In en, this message translates to:
  /// **'The hub\'s clock: declared in step; round trip {latency} ms ({samples} samples).'**
  String clockSynced(int latency, int samples);

  /// No description provided for @directoryOf.
  ///
  /// In en, this message translates to:
  /// **'Directory of {hub}'**
  String directoryOf(String hub);

  /// No description provided for @directoryNote.
  ///
  /// In en, this message translates to:
  /// **'Channels this hub lists for its servers. Unlisted channels are never here; a moderator invites servers to those.'**
  String get directoryNote;

  /// No description provided for @directoryEmpty.
  ///
  /// In en, this message translates to:
  /// **'The hub lists no channels.'**
  String get directoryEmpty;

  /// No description provided for @listedChannels.
  ///
  /// In en, this message translates to:
  /// **'Listed channels'**
  String get listedChannels;

  /// No description provided for @directoryFacts.
  ///
  /// In en, this message translates to:
  /// **'{servers, plural, =1{1 server} other{{servers} servers}} · moderators: {moderators}'**
  String directoryFacts(int servers, String moderators);

  /// No description provided for @stateMember.
  ///
  /// In en, this message translates to:
  /// **'in it'**
  String get stateMember;

  /// No description provided for @stateRequested.
  ///
  /// In en, this message translates to:
  /// **'asked'**
  String get stateRequested;

  /// No description provided for @stateInvited.
  ///
  /// In en, this message translates to:
  /// **'invited'**
  String get stateInvited;

  /// No description provided for @joinChannel.
  ///
  /// In en, this message translates to:
  /// **'Join'**
  String get joinChannel;

  /// No description provided for @askToJoin.
  ///
  /// In en, this message translates to:
  /// **'Ask to join'**
  String get askToJoin;

  /// No description provided for @pendingRequests.
  ///
  /// In en, this message translates to:
  /// **'Waiting for a moderator'**
  String get pendingRequests;

  /// No description provided for @waitingForModerator.
  ///
  /// In en, this message translates to:
  /// **'asked, not answered yet'**
  String get waitingForModerator;

  /// No description provided for @declinedRequests.
  ///
  /// In en, this message translates to:
  /// **'Declined or removed'**
  String get declinedRequests;

  /// No description provided for @declinedNote.
  ///
  /// In en, this message translates to:
  /// **'kept here as an archived channel'**
  String get declinedNote;

  /// No description provided for @joinTitle.
  ///
  /// In en, this message translates to:
  /// **'Join {name}'**
  String joinTitle(String name);

  /// No description provided for @askTitle.
  ///
  /// In en, this message translates to:
  /// **'Ask to join {name}'**
  String askTitle(String name);

  /// No description provided for @joinNote.
  ///
  /// In en, this message translates to:
  /// **'A new channel is made here for it. Its messages come from every server in it.'**
  String get joinNote;

  /// No description provided for @askNote.
  ///
  /// In en, this message translates to:
  /// **'A moderator of the channel approves or declines. Until then the channel waits here, and if declined it stays as an archived channel.'**
  String get askNote;

  /// No description provided for @localName.
  ///
  /// In en, this message translates to:
  /// **'Name on this server'**
  String get localName;

  /// No description provided for @publicHere.
  ///
  /// In en, this message translates to:
  /// **'Public here'**
  String get publicHere;

  /// No description provided for @privateHere.
  ///
  /// In en, this message translates to:
  /// **'Private here'**
  String get privateHere;

  /// No description provided for @noSection.
  ///
  /// In en, this message translates to:
  /// **'No section'**
  String get noSection;

  /// No description provided for @joinedHubChannel.
  ///
  /// In en, this message translates to:
  /// **'Joined {name}.'**
  String joinedHubChannel(String name);

  /// No description provided for @askedToJoin.
  ///
  /// In en, this message translates to:
  /// **'Asked to join {name}.'**
  String askedToJoin(String name);

  /// No description provided for @shareToHub.
  ///
  /// In en, this message translates to:
  /// **'Share to a hub'**
  String get shareToHub;

  /// No description provided for @shareToHubNote.
  ///
  /// In en, this message translates to:
  /// **'The hub will own the channel; other servers on it can join.'**
  String get shareToHubNote;

  /// No description provided for @shareToHubTitle.
  ///
  /// In en, this message translates to:
  /// **'Share {name} to a hub'**
  String shareToHubTitle(String name);

  /// No description provided for @noHubsToShare.
  ///
  /// In en, this message translates to:
  /// **'This server is on no hub it is connected to. Link it to a hub in the web admin console first.'**
  String get noHubsToShare;

  /// No description provided for @whichHub.
  ///
  /// In en, this message translates to:
  /// **'Which hub'**
  String get whichHub;

  /// No description provided for @hubWillOwn.
  ///
  /// In en, this message translates to:
  /// **'{hub} will own this channel.'**
  String hubWillOwn(String hub);

  /// No description provided for @hubOperatorSees.
  ///
  /// In en, this message translates to:
  /// **'Its operator can see the channel\'s name, description, listing mode, moderators and which servers are in it, and can admit and remove servers and appoint moderators.'**
  String get hubOperatorSees;

  /// No description provided for @hubOperatorCannot.
  ///
  /// In en, this message translates to:
  /// **'Its operator cannot read messages, files or anything else inside the channel: the hub never holds its key.'**
  String get hubOperatorCannot;

  /// No description provided for @hubYouModerate.
  ///
  /// In en, this message translates to:
  /// **'You become its first moderator. This server keeps its own levels, pins and slow mode, but no longer decides who is in the channel.'**
  String get hubYouModerate;

  /// No description provided for @listingMode.
  ///
  /// In en, this message translates to:
  /// **'Listing mode'**
  String get listingMode;

  /// No description provided for @listingUnlisted.
  ///
  /// In en, this message translates to:
  /// **'Unlisted'**
  String get listingUnlisted;

  /// No description provided for @listingRequest.
  ///
  /// In en, this message translates to:
  /// **'By request'**
  String get listingRequest;

  /// No description provided for @listingOpen.
  ///
  /// In en, this message translates to:
  /// **'Open'**
  String get listingOpen;

  /// No description provided for @listingUnlistedNote.
  ///
  /// In en, this message translates to:
  /// **'Servers join only when a moderator invites them.'**
  String get listingUnlistedNote;

  /// No description provided for @listingRequestNote.
  ///
  /// In en, this message translates to:
  /// **'Every server on the hub sees it in the directory and may ask to join; a moderator approves or declines.'**
  String get listingRequestNote;

  /// No description provided for @listingOpenNote.
  ///
  /// In en, this message translates to:
  /// **'Every server on the hub sees it in the directory and may join without asking.'**
  String get listingOpenNote;

  /// No description provided for @serversToInvite.
  ///
  /// In en, this message translates to:
  /// **'Servers to invite'**
  String get serversToInvite;

  /// No description provided for @noOtherServers.
  ///
  /// In en, this message translates to:
  /// **'No other server is on this hub yet.'**
  String get noOtherServers;

  /// No description provided for @sharedToHub.
  ///
  /// In en, this message translates to:
  /// **'Shared. {hub} owns the channel now.'**
  String sharedToHub(String hub);

  /// No description provided for @onAHub.
  ///
  /// In en, this message translates to:
  /// **'On a hub'**
  String get onAHub;

  /// No description provided for @ownedByHub.
  ///
  /// In en, this message translates to:
  /// **'Owned by the hub {hub}'**
  String ownedByHub(String hub);

  /// No description provided for @moderatorBadge.
  ///
  /// In en, this message translates to:
  /// **'moderator'**
  String get moderatorBadge;

  /// No description provided for @hubChannelClosed.
  ///
  /// In en, this message translates to:
  /// **'Closed: the hub no longer carries this channel.'**
  String get hubChannelClosed;

  /// No description provided for @nameOnHub.
  ///
  /// In en, this message translates to:
  /// **'Name on the hub: {name}'**
  String nameOnHub(String name);

  /// No description provided for @memberServers.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 server} other{{count} servers}}'**
  String memberServers(int count);

  /// No description provided for @moderators.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 moderator} other{{count} moderators}}'**
  String moderators(int count);

  /// No description provided for @remove.
  ///
  /// In en, this message translates to:
  /// **'Remove'**
  String get remove;

  /// No description provided for @removeServerTitle.
  ///
  /// In en, this message translates to:
  /// **'Remove {server}?'**
  String removeServerTitle(String server);

  /// No description provided for @removeServerBody.
  ///
  /// In en, this message translates to:
  /// **'The hub takes {server} out of the channel, with its moderators. It keeps what it already has and receives nothing new.'**
  String removeServerBody(String server);

  /// No description provided for @joinRequests.
  ///
  /// In en, this message translates to:
  /// **'Asking to join'**
  String get joinRequests;

  /// No description provided for @approve.
  ///
  /// In en, this message translates to:
  /// **'Approve'**
  String get approve;

  /// No description provided for @decline.
  ///
  /// In en, this message translates to:
  /// **'Decline'**
  String get decline;

  /// No description provided for @invitedServers.
  ///
  /// In en, this message translates to:
  /// **'Invited'**
  String get invitedServers;

  /// No description provided for @withdraw.
  ///
  /// In en, this message translates to:
  /// **'Withdraw'**
  String get withdraw;

  /// No description provided for @moderatorTools.
  ///
  /// In en, this message translates to:
  /// **'Moderator tools'**
  String get moderatorTools;

  /// No description provided for @inviteServer.
  ///
  /// In en, this message translates to:
  /// **'Invite a server'**
  String get inviteServer;

  /// No description provided for @serverNameLabel.
  ///
  /// In en, this message translates to:
  /// **'The server\'s name'**
  String get serverNameLabel;

  /// No description provided for @renameOnHub.
  ///
  /// In en, this message translates to:
  /// **'Rename'**
  String get renameOnHub;

  /// No description provided for @nameLabel.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get nameLabel;

  /// No description provided for @describeOnHub.
  ///
  /// In en, this message translates to:
  /// **'Describe'**
  String get describeOnHub;

  /// No description provided for @descriptionLabel.
  ///
  /// In en, this message translates to:
  /// **'Description'**
  String get descriptionLabel;

  /// No description provided for @addModerator.
  ///
  /// In en, this message translates to:
  /// **'Add a moderator'**
  String get addModerator;

  /// No description provided for @closeChannel.
  ///
  /// In en, this message translates to:
  /// **'Close the channel'**
  String get closeChannel;

  /// No description provided for @closeHubChannelTitle.
  ///
  /// In en, this message translates to:
  /// **'Close this channel for every server?'**
  String get closeHubChannelTitle;

  /// No description provided for @closeHubChannelBody.
  ///
  /// In en, this message translates to:
  /// **'The hub stops carrying it. Every server keeps what it has. This cannot be undone.'**
  String get closeHubChannelBody;

  /// No description provided for @deleteForEveryone.
  ///
  /// In en, this message translates to:
  /// **'Delete for everyone'**
  String get deleteForEveryone;

  /// No description provided for @deleteForEveryoneTitle.
  ///
  /// In en, this message translates to:
  /// **'Delete this message for everyone?'**
  String get deleteForEveryoneTitle;

  /// No description provided for @deleteForEveryoneBody.
  ///
  /// In en, this message translates to:
  /// **'As a moderator, you delete it on every server in the channel. Each server checks that you are on the hub\'s moderator list.'**
  String get deleteForEveryoneBody;

  /// No description provided for @deleteHere.
  ///
  /// In en, this message translates to:
  /// **'Delete here'**
  String get deleteHere;

  /// No description provided for @deleteHereTitle.
  ///
  /// In en, this message translates to:
  /// **'Delete this message on this server?'**
  String get deleteHereTitle;

  /// No description provided for @deleteHereBody.
  ///
  /// In en, this message translates to:
  /// **'You manage this channel here but are not one of its moderators, so it is deleted on this server only. The other servers keep it.'**
  String get deleteHereBody;

  /// No description provided for @notifyTitleInChannel.
  ///
  /// In en, this message translates to:
  /// **'{sender} in #{channel}'**
  String notifyTitleInChannel(String sender, String channel);

  /// No description provided for @notifyTitleInChat.
  ///
  /// In en, this message translates to:
  /// **'{sender} in {chat}'**
  String notifyTitleInChat(String sender, String chat);

  /// No description provided for @notifyTitleOnServer.
  ///
  /// In en, this message translates to:
  /// **'{title} · {server}'**
  String notifyTitleOnServer(String title, String server);

  /// No description provided for @notifyNewMessage.
  ///
  /// In en, this message translates to:
  /// **'New message'**
  String get notifyNewMessage;

  /// No description provided for @notifySentFile.
  ///
  /// In en, this message translates to:
  /// **'Sent a file'**
  String get notifySentFile;

  /// No description provided for @notifyPostedForm.
  ///
  /// In en, this message translates to:
  /// **'Posted a poll or a form'**
  String get notifyPostedForm;

  /// No description provided for @notifySentCard.
  ///
  /// In en, this message translates to:
  /// **'Sent a card'**
  String get notifySentCard;

  /// No description provided for @notifyBodyWithMore.
  ///
  /// In en, this message translates to:
  /// **'{body} (+{count} more)'**
  String notifyBodyWithMore(String body, int count);

  /// No description provided for @notifyTestTitle.
  ///
  /// In en, this message translates to:
  /// **'Notifications work'**
  String get notifyTestTitle;

  /// No description provided for @notifyTestBody.
  ///
  /// In en, this message translates to:
  /// **'This is how a message from Churr will look.'**
  String get notifyTestBody;

  /// No description provided for @notifyCalling.
  ///
  /// In en, this message translates to:
  /// **'{name} is calling'**
  String notifyCalling(String name);

  /// No description provided for @notifyCallBody.
  ///
  /// In en, this message translates to:
  /// **'Open to join or decline'**
  String get notifyCallBody;

  /// No description provided for @notifyMissedCall.
  ///
  /// In en, this message translates to:
  /// **'Missed call from {name}'**
  String notifyMissedCall(String name);

  /// No description provided for @notifyNewActivity.
  ///
  /// In en, this message translates to:
  /// **'New activity on {server}'**
  String notifyNewActivity(String server);

  /// No description provided for @notifyChannelDirect.
  ///
  /// In en, this message translates to:
  /// **'Mentions and chats'**
  String get notifyChannelDirect;

  /// No description provided for @notifyChannelActivity.
  ///
  /// In en, this message translates to:
  /// **'Channel activity'**
  String get notifyChannelActivity;

  /// No description provided for @notifyChannelCalls.
  ///
  /// In en, this message translates to:
  /// **'Calls'**
  String get notifyChannelCalls;

  /// No description provided for @notifyChannelService.
  ///
  /// In en, this message translates to:
  /// **'Staying connected'**
  String get notifyChannelService;

  /// No description provided for @notifyServiceConnected.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Churr is connected to 1 server} other{Churr is connected to {count} servers}}'**
  String notifyServiceConnected(int count);

  /// No description provided for @trayOpen.
  ///
  /// In en, this message translates to:
  /// **'Open Churr'**
  String get trayOpen;

  /// No description provided for @trayQuit.
  ///
  /// In en, this message translates to:
  /// **'Quit Churr'**
  String get trayQuit;

  /// No description provided for @trayDnd.
  ///
  /// In en, this message translates to:
  /// **'Do not disturb'**
  String get trayDnd;

  /// No description provided for @trayDndFor30.
  ///
  /// In en, this message translates to:
  /// **'For 30 minutes'**
  String get trayDndFor30;

  /// No description provided for @trayDndForHour.
  ///
  /// In en, this message translates to:
  /// **'For an hour'**
  String get trayDndForHour;

  /// No description provided for @trayDndTomorrow.
  ///
  /// In en, this message translates to:
  /// **'Until tomorrow morning'**
  String get trayDndTomorrow;

  /// No description provided for @trayDndUntilOff.
  ///
  /// In en, this message translates to:
  /// **'Until I turn it off'**
  String get trayDndUntilOff;

  /// No description provided for @trayDndTurnOff.
  ///
  /// In en, this message translates to:
  /// **'Turn off do not disturb'**
  String get trayDndTurnOff;

  /// No description provided for @trayMute.
  ///
  /// In en, this message translates to:
  /// **'Mute a server'**
  String get trayMute;

  /// No description provided for @trayMuteFor1h.
  ///
  /// In en, this message translates to:
  /// **'For an hour'**
  String get trayMuteFor1h;

  /// No description provided for @trayMuteFor8h.
  ///
  /// In en, this message translates to:
  /// **'For 8 hours'**
  String get trayMuteFor8h;

  /// No description provided for @trayMuteForDay.
  ///
  /// In en, this message translates to:
  /// **'For a day'**
  String get trayMuteForDay;

  /// No description provided for @trayMuteForWeek.
  ///
  /// In en, this message translates to:
  /// **'For a week'**
  String get trayMuteForWeek;

  /// No description provided for @trayUnmute.
  ///
  /// In en, this message translates to:
  /// **'Unmute'**
  String get trayUnmute;

  /// No description provided for @trayServerCount.
  ///
  /// In en, this message translates to:
  /// **'{server}: {count}'**
  String trayServerCount(String server, int count);

  /// No description provided for @trayServerOffline.
  ///
  /// In en, this message translates to:
  /// **'{server}: not connected'**
  String trayServerOffline(String server);

  /// No description provided for @trayTooltip.
  ///
  /// In en, this message translates to:
  /// **'Churr'**
  String get trayTooltip;

  /// No description provided for @trayTooltipUnread.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Churr: 1 unread} other{Churr: {count} unread}}'**
  String trayTooltipUnread(int count);

  /// No description provided for @trayTooltipDnd.
  ///
  /// In en, this message translates to:
  /// **'Churr: do not disturb'**
  String get trayTooltipDnd;

  /// No description provided for @trayTooltipDisconnected.
  ///
  /// In en, this message translates to:
  /// **'Churr: not connected'**
  String get trayTooltipDisconnected;

  /// No description provided for @sendTestNotification.
  ///
  /// In en, this message translates to:
  /// **'Send a test notification'**
  String get sendTestNotification;

  /// No description provided for @testNotificationSent.
  ///
  /// In en, this message translates to:
  /// **'Sent. If nothing appeared, notifications for Churr are off in the system\'s settings.'**
  String get testNotificationSent;

  /// No description provided for @desktopSection.
  ///
  /// In en, this message translates to:
  /// **'On this computer'**
  String get desktopSection;

  /// No description provided for @startAtLogin.
  ///
  /// In en, this message translates to:
  /// **'Start Churr when you log in'**
  String get startAtLogin;

  /// No description provided for @startAtLoginNote.
  ///
  /// In en, this message translates to:
  /// **'The window stays hidden; Churr waits in the tray, connected.'**
  String get startAtLoginNote;

  /// No description provided for @closeToTray.
  ///
  /// In en, this message translates to:
  /// **'Closing the window keeps Churr running'**
  String get closeToTray;

  /// No description provided for @closeToTrayNote.
  ///
  /// In en, this message translates to:
  /// **'Churr stays connected in the tray so notifications keep coming. Quit is in the tray menu.'**
  String get closeToTrayNote;

  /// No description provided for @androidPushTitle.
  ///
  /// In en, this message translates to:
  /// **'How this phone hears about new messages'**
  String get androidPushTitle;

  /// No description provided for @androidPushAutomatic.
  ///
  /// In en, this message translates to:
  /// **'Automatic'**
  String get androidPushAutomatic;

  /// No description provided for @androidPushAutomaticNote.
  ///
  /// In en, this message translates to:
  /// **'Google\'s push service where this build has it, else UnifiedPush if a distributor is installed, else only when you open Churr.'**
  String get androidPushAutomaticNote;

  /// No description provided for @androidPushUnified.
  ///
  /// In en, this message translates to:
  /// **'UnifiedPush'**
  String get androidPushUnified;

  /// No description provided for @androidPushUnifiedNote.
  ///
  /// In en, this message translates to:
  /// **'Through the distributor app you chose. Each server must have UnifiedPush switched on.'**
  String get androidPushUnifiedNote;

  /// No description provided for @androidPushStay.
  ///
  /// In en, this message translates to:
  /// **'Stay connected'**
  String get androidPushStay;

  /// No description provided for @androidPushStayNote.
  ///
  /// In en, this message translates to:
  /// **'Keeps a connection open, with a permanent notification. It costs battery, and Android may stop it unless Churr may run in the background.'**
  String get androidPushStayNote;

  /// No description provided for @androidPushOff.
  ///
  /// In en, this message translates to:
  /// **'Only when Churr is open'**
  String get androidPushOff;

  /// No description provided for @androidPushOffNote.
  ///
  /// In en, this message translates to:
  /// **'Nothing arrives in the background. Badges are brought up to date when you open the app.'**
  String get androidPushOffNote;

  /// No description provided for @androidPushNoDistributor.
  ///
  /// In en, this message translates to:
  /// **'No UnifiedPush distributor is installed on this phone.'**
  String get androidPushNoDistributor;

  /// No description provided for @androidPushRefused.
  ///
  /// In en, this message translates to:
  /// **'{server} does not send this kind of wake-up. It is checked when you open Churr instead.'**
  String androidPushRefused(String server);

  /// No description provided for @allowBackground.
  ///
  /// In en, this message translates to:
  /// **'Allow running in the background'**
  String get allowBackground;

  /// No description provided for @devSecretsInFile.
  ///
  /// In en, this message translates to:
  /// **'Development build: secrets are stored unprotected in a file.'**
  String get devSecretsInFile;

  /// No description provided for @callAnswer.
  ///
  /// In en, this message translates to:
  /// **'Join'**
  String get callAnswer;

  /// No description provided for @callAnswerVideo.
  ///
  /// In en, this message translates to:
  /// **'Join with video'**
  String get callAnswerVideo;

  /// No description provided for @callBarTitle.
  ///
  /// In en, this message translates to:
  /// **'{conversation}: {count, plural, =1{1 person} other{{count} people}} in the call'**
  String callBarTitle(String conversation, int count);

  /// No description provided for @callCameraDevice.
  ///
  /// In en, this message translates to:
  /// **'Camera'**
  String get callCameraDevice;

  /// No description provided for @callCameraOff.
  ///
  /// In en, this message translates to:
  /// **'Turn camera off'**
  String get callCameraOff;

  /// No description provided for @callCameraOn.
  ///
  /// In en, this message translates to:
  /// **'Turn camera on'**
  String get callCameraOn;

  /// No description provided for @callCamerasFull.
  ///
  /// In en, this message translates to:
  /// **'The most cameras allowed are already on. Your microphone still works.'**
  String get callCamerasFull;

  /// No description provided for @callCannotDecrypt.
  ///
  /// In en, this message translates to:
  /// **'Cannot decrypt this call'**
  String get callCannotDecrypt;

  /// No description provided for @callConnecting.
  ///
  /// In en, this message translates to:
  /// **'Connecting…'**
  String get callConnecting;

  /// No description provided for @callDecline.
  ///
  /// In en, this message translates to:
  /// **'Decline'**
  String get callDecline;

  /// No description provided for @callDeclined.
  ///
  /// In en, this message translates to:
  /// **'{name} declined.'**
  String callDeclined(String name);

  /// No description provided for @callDevices.
  ///
  /// In en, this message translates to:
  /// **'Devices'**
  String get callDevices;

  /// No description provided for @callDnd.
  ///
  /// In en, this message translates to:
  /// **'Do not disturb'**
  String get callDnd;

  /// No description provided for @callE2E.
  ///
  /// In en, this message translates to:
  /// **'End-to-end encrypted'**
  String get callE2E;

  /// No description provided for @callEarpiece.
  ///
  /// In en, this message translates to:
  /// **'Use the earpiece'**
  String get callEarpiece;

  /// No description provided for @callEndedElsewhere.
  ///
  /// In en, this message translates to:
  /// **'You joined this call on another device.'**
  String get callEndedElsewhere;

  /// No description provided for @callEndedEnded.
  ///
  /// In en, this message translates to:
  /// **'The call ended.'**
  String get callEndedEnded;

  /// No description provided for @callEndedFailed.
  ///
  /// In en, this message translates to:
  /// **'The call stopped working ({code}).'**
  String callEndedFailed(String code);

  /// No description provided for @callEndedFull.
  ///
  /// In en, this message translates to:
  /// **'The call is full.'**
  String get callEndedFull;

  /// No description provided for @callEndedNetwork.
  ///
  /// In en, this message translates to:
  /// **'This network blocks calls. You can still read and write.'**
  String get callEndedNetwork;

  /// No description provided for @callEndedOff.
  ///
  /// In en, this message translates to:
  /// **'Calls are switched off on this server.'**
  String get callEndedOff;

  /// No description provided for @callEndedRefused.
  ///
  /// In en, this message translates to:
  /// **'You could not join the call ({code}).'**
  String callEndedRefused(String code);

  /// No description provided for @callEndedRemoved.
  ///
  /// In en, this message translates to:
  /// **'You were removed from the call.'**
  String get callEndedRemoved;

  /// No description provided for @callEndedServer.
  ///
  /// In en, this message translates to:
  /// **'The call ended because the server restarted.'**
  String get callEndedServer;

  /// No description provided for @callEndedSignedOut.
  ///
  /// In en, this message translates to:
  /// **'You were signed out, so you left the call.'**
  String get callEndedSignedOut;

  /// No description provided for @callEndedSummary.
  ///
  /// In en, this message translates to:
  /// **'{minutes, plural, =0{Call, less than a minute} =1{Call, 1 minute} other{Call, {minutes} minutes}}, {people, plural, =1{1 person} other{{people} people}}'**
  String callEndedSummary(int minutes, int people);

  /// No description provided for @callEndedTimeout.
  ///
  /// In en, this message translates to:
  /// **'You were disconnected from the call.'**
  String get callEndedTimeout;

  /// No description provided for @callExpand.
  ///
  /// In en, this message translates to:
  /// **'Back to the call'**
  String get callExpand;

  /// No description provided for @callHostedElsewhere.
  ///
  /// In en, this message translates to:
  /// **'This call is carried by {server}. Your device connects to that server, and its operator can see your device\'s network address.'**
  String callHostedElsewhere(String server);

  /// No description provided for @callInCallNow.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{Nobody else is here yet} =1{1 person in the call} other{{count} people in the call}}'**
  String callInCallNow(int count);

  /// No description provided for @callIncomingFrom.
  ///
  /// In en, this message translates to:
  /// **'{name} is calling'**
  String callIncomingFrom(String name);

  /// No description provided for @callInIt.
  ///
  /// In en, this message translates to:
  /// **'In the call'**
  String get callInIt;

  /// No description provided for @callJoinCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Join, 1 person} other{Join, {count} people}}'**
  String callJoinCount(int count);

  /// No description provided for @callJoining.
  ///
  /// In en, this message translates to:
  /// **'Joining…'**
  String get callJoining;

  /// No description provided for @callLayoutGrid.
  ///
  /// In en, this message translates to:
  /// **'Show everyone the same size'**
  String get callLayoutGrid;

  /// No description provided for @callLayoutSpeaker.
  ///
  /// In en, this message translates to:
  /// **'Show the speaker large'**
  String get callLayoutSpeaker;

  /// No description provided for @callLeave.
  ///
  /// In en, this message translates to:
  /// **'Leave the call'**
  String get callLeave;

  /// No description provided for @callMicrophoneDevice.
  ///
  /// In en, this message translates to:
  /// **'Microphone'**
  String get callMicrophoneDevice;

  /// No description provided for @callMinimize.
  ///
  /// In en, this message translates to:
  /// **'Keep the call going and use the app'**
  String get callMinimize;

  /// No description provided for @callMissedFrom.
  ///
  /// In en, this message translates to:
  /// **'Missed call from {name}'**
  String callMissedFrom(String name);

  /// No description provided for @callMutedMark.
  ///
  /// In en, this message translates to:
  /// **'microphone off'**
  String get callMutedMark;

  /// No description provided for @callMuteForMe.
  ///
  /// In en, this message translates to:
  /// **'Mute for me'**
  String get callMuteForMe;

  /// No description provided for @callMuteMic.
  ///
  /// In en, this message translates to:
  /// **'Turn microphone off'**
  String get callMuteMic;

  /// No description provided for @callOutputDevice.
  ///
  /// In en, this message translates to:
  /// **'Speaker'**
  String get callOutputDevice;

  /// No description provided for @callPeople.
  ///
  /// In en, this message translates to:
  /// **'People in the call'**
  String get callPeople;

  /// No description provided for @callPickScreen.
  ///
  /// In en, this message translates to:
  /// **'Choose what to share'**
  String get callPickScreen;

  /// No description provided for @callPin.
  ///
  /// In en, this message translates to:
  /// **'Pin'**
  String get callPin;

  /// No description provided for @callPinned.
  ///
  /// In en, this message translates to:
  /// **'pinned'**
  String get callPinned;

  /// No description provided for @callPreJoin.
  ///
  /// In en, this message translates to:
  /// **'Join the call'**
  String get callPreJoin;

  /// No description provided for @callProblem.
  ///
  /// In en, this message translates to:
  /// **'Something went wrong in the call ({code}).'**
  String callProblem(String code);

  /// No description provided for @callReconnecting.
  ///
  /// In en, this message translates to:
  /// **'Reconnecting…'**
  String get callReconnecting;

  /// No description provided for @callRinging.
  ///
  /// In en, this message translates to:
  /// **'Ringing'**
  String get callRinging;

  /// No description provided for @callScreenInUse.
  ///
  /// In en, this message translates to:
  /// **'{name} is already sharing a screen.'**
  String callScreenInUse(String name);

  /// No description provided for @callScreenOf.
  ///
  /// In en, this message translates to:
  /// **'{name}\'s screen'**
  String callScreenOf(String name);

  /// No description provided for @callScreens.
  ///
  /// In en, this message translates to:
  /// **'Screens'**
  String get callScreens;

  /// No description provided for @callSealedNoKeys.
  ///
  /// In en, this message translates to:
  /// **'This device cannot join this sealed chat\'s call: it does not hold the chat\'s call keys.'**
  String get callSealedNoKeys;

  /// No description provided for @callSealedUnavailable.
  ///
  /// In en, this message translates to:
  /// **'This device cannot take part in this sealed chat\'s call.'**
  String get callSealedUnavailable;

  /// No description provided for @callShareScreen.
  ///
  /// In en, this message translates to:
  /// **'Share your screen'**
  String get callShareScreen;

  /// No description provided for @callSomeone.
  ///
  /// In en, this message translates to:
  /// **'Someone'**
  String get callSomeone;

  /// No description provided for @callSpeaker.
  ///
  /// In en, this message translates to:
  /// **'Use the loudspeaker'**
  String get callSpeaker;

  /// No description provided for @callSpeakingMark.
  ///
  /// In en, this message translates to:
  /// **'speaking'**
  String get callSpeakingMark;

  /// No description provided for @callStart.
  ///
  /// In en, this message translates to:
  /// **'Start a call'**
  String get callStart;

  /// No description provided for @callStartedBy.
  ///
  /// In en, this message translates to:
  /// **'{name} started a call'**
  String callStartedBy(String name);

  /// No description provided for @callStopShare.
  ///
  /// In en, this message translates to:
  /// **'Stop sharing'**
  String get callStopShare;

  /// No description provided for @callSwitchCamera.
  ///
  /// In en, this message translates to:
  /// **'Switch camera'**
  String get callSwitchCamera;

  /// No description provided for @callUnmuteForMe.
  ///
  /// In en, this message translates to:
  /// **'Unmute for me'**
  String get callUnmuteForMe;

  /// No description provided for @callUnmuteMic.
  ///
  /// In en, this message translates to:
  /// **'Turn microphone on'**
  String get callUnmuteMic;

  /// No description provided for @callUnpin.
  ///
  /// In en, this message translates to:
  /// **'Unpin'**
  String get callUnpin;

  /// No description provided for @callVideoPaused.
  ///
  /// In en, this message translates to:
  /// **'Video paused'**
  String get callVideoPaused;

  /// No description provided for @callWindows.
  ///
  /// In en, this message translates to:
  /// **'Windows'**
  String get callWindows;

  /// No description provided for @callWithCamera.
  ///
  /// In en, this message translates to:
  /// **'Camera on'**
  String get callWithCamera;

  /// No description provided for @callWithMic.
  ///
  /// In en, this message translates to:
  /// **'Microphone on'**
  String get callWithMic;

  /// No description provided for @callYou.
  ///
  /// In en, this message translates to:
  /// **'You'**
  String get callYou;

  /// No description provided for @callMore.
  ///
  /// In en, this message translates to:
  /// **'+{count}'**
  String callMore(int count);

  /// No description provided for @back.
  ///
  /// In en, this message translates to:
  /// **'Back'**
  String get back;

  /// No description provided for @sealedChatTitle.
  ///
  /// In en, this message translates to:
  /// **'Sealed chat'**
  String get sealedChatTitle;

  /// No description provided for @sealedNotRunning.
  ///
  /// In en, this message translates to:
  /// **'Sealed chats are not running for this server on this device.'**
  String get sealedNotRunning;

  /// No description provided for @sealedEncrypted.
  ///
  /// In en, this message translates to:
  /// **'End-to-end encrypted'**
  String get sealedEncrypted;

  /// No description provided for @sealedChatMenu.
  ///
  /// In en, this message translates to:
  /// **'Sealed chat menu'**
  String get sealedChatMenu;

  /// No description provided for @sealedCompareCodes.
  ///
  /// In en, this message translates to:
  /// **'Compare codes'**
  String get sealedCompareCodes;

  /// No description provided for @sealedChatsAndDevices.
  ///
  /// In en, this message translates to:
  /// **'Sealed chats and devices'**
  String get sealedChatsAndDevices;

  /// No description provided for @sealedLockedHere.
  ///
  /// In en, this message translates to:
  /// **'This device is locked. Sealed chats are listed here but cannot be read.'**
  String get sealedLockedHere;

  /// No description provided for @sealedUnlock.
  ///
  /// In en, this message translates to:
  /// **'Unlock'**
  String get sealedUnlock;

  /// No description provided for @sealedReset.
  ///
  /// In en, this message translates to:
  /// **'{name}\'s sealed-chat identity was reset. Messages from now on go to new devices. If you did not expect this, compare codes before you send anything private.'**
  String sealedReset(String name);

  /// No description provided for @sealedAcknowledge.
  ///
  /// In en, this message translates to:
  /// **'I understand'**
  String get sealedAcknowledge;

  /// No description provided for @sealedBrowserAmong.
  ///
  /// In en, this message translates to:
  /// **'{name} also uses Churr in a browser. A browser runs whatever code the server sends each time it loads.'**
  String sealedBrowserAmong(String name);

  /// No description provided for @sealedWaiting.
  ///
  /// In en, this message translates to:
  /// **'Earlier messages will appear when {name}\'s device is next online.'**
  String sealedWaiting(String name);

  /// No description provided for @sealedUnchecked.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 message could not be checked.} other{{count} messages could not be checked.}}'**
  String sealedUnchecked(int count);

  /// No description provided for @sealedOutOfOrder.
  ///
  /// In en, this message translates to:
  /// **'The server delivered this out of order.'**
  String get sealedOutOfOrder;

  /// No description provided for @sealedReplyingTo.
  ///
  /// In en, this message translates to:
  /// **'Replying to {name}'**
  String sealedReplyingTo(String name);

  /// No description provided for @sealedVerified.
  ///
  /// In en, this message translates to:
  /// **'Verified'**
  String get sealedVerified;

  /// No description provided for @sealedEmpty.
  ///
  /// In en, this message translates to:
  /// **'Nothing here yet. Only the two of you can read what is written here.'**
  String get sealedEmpty;

  /// No description provided for @sealedMessageHint.
  ///
  /// In en, this message translates to:
  /// **'Sealed message to {name}'**
  String sealedMessageHint(String name);

  /// No description provided for @sealedRemovedHere.
  ///
  /// In en, this message translates to:
  /// **'Another of your devices removed this one from sealed chats. Its sealed keys and messages were wiped.'**
  String get sealedRemovedHere;

  /// No description provided for @sealedSetUpAgain.
  ///
  /// In en, this message translates to:
  /// **'Set up again'**
  String get sealedSetUpAgain;

  /// No description provided for @sealedFileDownload.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get sealedFileDownload;

  /// No description provided for @sealedFileFailed.
  ///
  /// In en, this message translates to:
  /// **'The file did not check out and was not saved.'**
  String get sealedFileFailed;

  /// No description provided for @sealedPlatformAndroid.
  ///
  /// In en, this message translates to:
  /// **'Android app'**
  String get sealedPlatformAndroid;

  /// No description provided for @sealedPlatformIos.
  ///
  /// In en, this message translates to:
  /// **'iPhone app'**
  String get sealedPlatformIos;

  /// No description provided for @sealedPlatformMac.
  ///
  /// In en, this message translates to:
  /// **'Mac app'**
  String get sealedPlatformMac;

  /// No description provided for @sealedPlatformWindows.
  ///
  /// In en, this message translates to:
  /// **'Windows app'**
  String get sealedPlatformWindows;

  /// No description provided for @sealedPlatformLinux.
  ///
  /// In en, this message translates to:
  /// **'Linux app'**
  String get sealedPlatformLinux;

  /// No description provided for @sealedPlatformBrowser.
  ///
  /// In en, this message translates to:
  /// **'a browser'**
  String get sealedPlatformBrowser;

  /// No description provided for @sealedNoticeAdded.
  ///
  /// In en, this message translates to:
  /// **'{name} added a device: {kind}.'**
  String sealedNoticeAdded(String name, String kind);

  /// No description provided for @sealedNoticeAddedMine.
  ///
  /// In en, this message translates to:
  /// **'A device was added to your account: {kind}.'**
  String sealedNoticeAddedMine(String kind);

  /// No description provided for @sealedNoticeRemoved.
  ///
  /// In en, this message translates to:
  /// **'{name} removed a device.'**
  String sealedNoticeRemoved(String name);

  /// No description provided for @sealedNoticeRemovedMine.
  ///
  /// In en, this message translates to:
  /// **'A device was removed from your account.'**
  String get sealedNoticeRemovedMine;

  /// No description provided for @sealedNoticeKey.
  ///
  /// In en, this message translates to:
  /// **'{name}\'s security key changed. Their old key signed the change.'**
  String sealedNoticeKey(String name);

  /// No description provided for @sealedNoticeKeyMine.
  ///
  /// In en, this message translates to:
  /// **'Your account\'s security key changed. Your old key signed the change.'**
  String get sealedNoticeKeyMine;

  /// No description provided for @sealedNoticeResetMine.
  ///
  /// In en, this message translates to:
  /// **'Your sealed-chat identity was reset.'**
  String get sealedNoticeResetMine;

  /// No description provided for @sealedNoticeListRefused.
  ///
  /// In en, this message translates to:
  /// **'The server sent a device list that does not check out.'**
  String get sealedNoticeListRefused;

  /// No description provided for @sealedNoticeOutOfStep.
  ///
  /// In en, this message translates to:
  /// **'This chat\'s keys are out of step.'**
  String get sealedNoticeOutOfStep;

  /// No description provided for @unlockTitle.
  ///
  /// In en, this message translates to:
  /// **'Unlock sealed chats'**
  String get unlockTitle;

  /// No description provided for @unlockIntro.
  ///
  /// In en, this message translates to:
  /// **'This device is signed in but cannot read sealed chats yet. Choose a way to unlock it.'**
  String get unlockIntro;

  /// No description provided for @unlockPasskey.
  ///
  /// In en, this message translates to:
  /// **'Use a passkey'**
  String get unlockPasskey;

  /// No description provided for @unlockPasskeyNote.
  ///
  /// In en, this message translates to:
  /// **'A passkey only works on a page served from the server\'s own address. So unlocking with a passkey passes your key through a page the server supplies, in your browser, in this app as in the web chat. The page hands the key back to this app sealed to this app\'s own key; after that, this app encrypts and decrypts in its own code.'**
  String get unlockPasskeyNote;

  /// No description provided for @unlockOpenPage.
  ///
  /// In en, this message translates to:
  /// **'Open the unlock page'**
  String get unlockOpenPage;

  /// No description provided for @unlockCheckAgain.
  ///
  /// In en, this message translates to:
  /// **'I have finished'**
  String get unlockCheckAgain;

  /// No description provided for @unlockPasskeyNotYet.
  ///
  /// In en, this message translates to:
  /// **'The unlock page has not unlocked this device yet.'**
  String get unlockPasskeyNotYet;

  /// No description provided for @unlockPhrase.
  ///
  /// In en, this message translates to:
  /// **'Recovery phrase'**
  String get unlockPhrase;

  /// No description provided for @unlockPhraseNote.
  ///
  /// In en, this message translates to:
  /// **'Type your twelve words. They stay in this app and do not pass through any server page.'**
  String get unlockPhraseNote;

  /// No description provided for @unlockPhraseField.
  ///
  /// In en, this message translates to:
  /// **'Twelve words'**
  String get unlockPhraseField;

  /// No description provided for @unlockAnotherDevice.
  ///
  /// In en, this message translates to:
  /// **'Another device'**
  String get unlockAnotherDevice;

  /// No description provided for @unlockAnotherNote.
  ///
  /// In en, this message translates to:
  /// **'Show this code to a device of yours that is already unlocked: scan it there, or type its 20 characters. It is long on purpose: it is the only thing that stops a server swapping in keys of its own.'**
  String get unlockAnotherNote;

  /// No description provided for @unlockShowCode.
  ///
  /// In en, this message translates to:
  /// **'Show the code'**
  String get unlockShowCode;

  /// No description provided for @unlockCodeQr.
  ///
  /// In en, this message translates to:
  /// **'Approval code'**
  String get unlockCodeQr;

  /// No description provided for @unlockWaiting.
  ///
  /// In en, this message translates to:
  /// **'Waiting for approval. The code lasts ten minutes.'**
  String get unlockWaiting;

  /// No description provided for @unlockNewRequest.
  ///
  /// In en, this message translates to:
  /// **'Make a new code'**
  String get unlockNewRequest;

  /// No description provided for @unlockCarryOn.
  ///
  /// In en, this message translates to:
  /// **'Carry on locked'**
  String get unlockCarryOn;

  /// No description provided for @unlockCarryOnNote.
  ///
  /// In en, this message translates to:
  /// **'Sealed chats are listed and cannot be read on this device.'**
  String get unlockCarryOnNote;

  /// No description provided for @unlockStartAgain.
  ///
  /// In en, this message translates to:
  /// **'Start again'**
  String get unlockStartAgain;

  /// No description provided for @unlockStartAgainWarning.
  ///
  /// In en, this message translates to:
  /// **'Only for when you have no passkey that can unlock, no recovery phrase and no other device. Earlier sealed messages can never be read again on any device of yours, and the other person in each sealed chat is shown that your keys were reset.'**
  String get unlockStartAgainWarning;

  /// No description provided for @unlockFailed.
  ///
  /// In en, this message translates to:
  /// **'That did not unlock this device. ({why})'**
  String unlockFailed(String why);

  /// No description provided for @sealedFirstChat.
  ///
  /// In en, this message translates to:
  /// **'This is your first sealed chat: it makes your recovery phrase, twelve words shown once.'**
  String get sealedFirstChat;

  /// No description provided for @sealedOtherWaysNote.
  ///
  /// In en, this message translates to:
  /// **'The recovery phrase and approval from another of your devices do not pass through any server page.'**
  String get sealedOtherWaysNote;

  /// No description provided for @sealedMakeBackup.
  ///
  /// In en, this message translates to:
  /// **'Make my recovery phrase'**
  String get sealedMakeBackup;

  /// No description provided for @phraseTitle.
  ///
  /// In en, this message translates to:
  /// **'Your recovery phrase'**
  String get phraseTitle;

  /// No description provided for @phraseIntro.
  ///
  /// In en, this message translates to:
  /// **'Write these twelve words down and keep them somewhere safe. They are shown once, never sent to the server. With them you can read your sealed chats on a new device; without them, and with no passkey or other device that can unlock, earlier sealed messages are lost.'**
  String get phraseIntro;

  /// No description provided for @phraseWrittenDown.
  ///
  /// In en, this message translates to:
  /// **'I have written them down'**
  String get phraseWrittenDown;

  /// No description provided for @phraseCheckIntro.
  ///
  /// In en, this message translates to:
  /// **'Check two of the words before going on.'**
  String get phraseCheckIntro;

  /// No description provided for @phraseCheck.
  ///
  /// In en, this message translates to:
  /// **'Word {n}'**
  String phraseCheck(int n);

  /// No description provided for @phraseCheckWrong.
  ///
  /// In en, this message translates to:
  /// **'Those words do not match. Look at what you wrote down, or go back.'**
  String get phraseCheckWrong;

  /// No description provided for @phraseContinue.
  ///
  /// In en, this message translates to:
  /// **'Continue'**
  String get phraseContinue;

  /// No description provided for @approveTitle.
  ///
  /// In en, this message translates to:
  /// **'Unlock a new device ({kind})?'**
  String approveTitle(String kind);

  /// No description provided for @approveSignedIn.
  ///
  /// In en, this message translates to:
  /// **'{name}, signed in {when}'**
  String approveSignedIn(String name, String when);

  /// No description provided for @approveWhyLong.
  ///
  /// In en, this message translates to:
  /// **'Scan the code the new device shows, or type its 20 characters. It is long on purpose: it is the only thing that stops a server swapping in keys of its own.'**
  String get approveWhyLong;

  /// No description provided for @approveCodeField.
  ///
  /// In en, this message translates to:
  /// **'The 20 characters it shows'**
  String get approveCodeField;

  /// No description provided for @approveScan.
  ///
  /// In en, this message translates to:
  /// **'Scan its code'**
  String get approveScan;

  /// No description provided for @approveYes.
  ///
  /// In en, this message translates to:
  /// **'Unlock it'**
  String get approveYes;

  /// No description provided for @approveNotNow.
  ///
  /// In en, this message translates to:
  /// **'Not now'**
  String get approveNotNow;

  /// No description provided for @approveWrongCode.
  ///
  /// In en, this message translates to:
  /// **'That code does not match this request. The request may have been tampered with.'**
  String get approveWrongCode;

  /// No description provided for @approveEndSession.
  ///
  /// In en, this message translates to:
  /// **'End that device\'s session'**
  String get approveEndSession;

  /// No description provided for @sealedStatusUnlocked.
  ///
  /// In en, this message translates to:
  /// **'This device can read your sealed chats.'**
  String get sealedStatusUnlocked;

  /// No description provided for @sealedStatusLocked.
  ///
  /// In en, this message translates to:
  /// **'This device is locked: it cannot read sealed chats.'**
  String get sealedStatusLocked;

  /// No description provided for @sealedStatusNoBackup.
  ///
  /// In en, this message translates to:
  /// **'You have no sealed chats yet. Your first one makes your recovery phrase.'**
  String get sealedStatusNoBackup;

  /// No description provided for @sealedRequestsTitle.
  ///
  /// In en, this message translates to:
  /// **'Devices asking to be unlocked'**
  String get sealedRequestsTitle;

  /// No description provided for @sealedYourDevices.
  ///
  /// In en, this message translates to:
  /// **'Your devices'**
  String get sealedYourDevices;

  /// No description provided for @sealedBrowserTag.
  ///
  /// In en, this message translates to:
  /// **'browser'**
  String get sealedBrowserTag;

  /// No description provided for @sealedThisDevice.
  ///
  /// In en, this message translates to:
  /// **'this device'**
  String get sealedThisDevice;

  /// No description provided for @sealedNotListed.
  ///
  /// In en, this message translates to:
  /// **'not in sealed chats'**
  String get sealedNotListed;

  /// No description provided for @sealedLastSeen.
  ///
  /// In en, this message translates to:
  /// **'Last seen {when}'**
  String sealedLastSeen(String when);

  /// No description provided for @sealedRemove.
  ///
  /// In en, this message translates to:
  /// **'Remove'**
  String get sealedRemove;

  /// No description provided for @sealedRemoveTitle.
  ///
  /// In en, this message translates to:
  /// **'Remove {name}?'**
  String sealedRemoveTitle(String name);

  /// No description provided for @sealedRemoveBody.
  ///
  /// In en, this message translates to:
  /// **'Its session ends and it is taken out of your sealed chats. This app then replaces your backup key and security key, because that device held both, and starts new keys in each chat. The other person is shown that your key changed.'**
  String get sealedRemoveBody;

  /// No description provided for @sealedRemoved.
  ///
  /// In en, this message translates to:
  /// **'Removed. Your backup key and security key were replaced.'**
  String get sealedRemoved;

  /// No description provided for @sealedPasskeys.
  ///
  /// In en, this message translates to:
  /// **'Passkeys'**
  String get sealedPasskeys;

  /// No description provided for @passkeyCanUnlock.
  ///
  /// In en, this message translates to:
  /// **'can unlock sealed chats'**
  String get passkeyCanUnlock;

  /// No description provided for @passkeyCannotUnlock.
  ///
  /// In en, this message translates to:
  /// **'cannot unlock sealed chats'**
  String get passkeyCannotUnlock;

  /// No description provided for @passkeyWaiting.
  ///
  /// In en, this message translates to:
  /// **'waiting for your approval to unlock'**
  String get passkeyWaiting;

  /// No description provided for @passkeyApproveTitle.
  ///
  /// In en, this message translates to:
  /// **'Let the passkey “{name}” unlock sealed chats?'**
  String passkeyApproveTitle(String name);

  /// No description provided for @passkeyApproveBody.
  ///
  /// In en, this message translates to:
  /// **'Say yes only for a passkey you have just added. Its key comes through the server, which could offer a key of its own under that name.'**
  String get passkeyApproveBody;

  /// No description provided for @passkeyApproveYes.
  ///
  /// In en, this message translates to:
  /// **'Let it unlock'**
  String get passkeyApproveYes;

  /// No description provided for @sealedReplaceKeys.
  ///
  /// In en, this message translates to:
  /// **'Replace my backup key and security key'**
  String get sealedReplaceKeys;

  /// No description provided for @sealedReplaced.
  ///
  /// In en, this message translates to:
  /// **'Your backup key and security key were replaced.'**
  String get sealedReplaced;

  /// No description provided for @compareIntro.
  ///
  /// In en, this message translates to:
  /// **'You and {name} see the same 60 digits if and only if nobody stands between you. Compare them in person or on a call you trust, or scan the other code.'**
  String compareIntro(String name);

  /// No description provided for @compareDigitsLabel.
  ///
  /// In en, this message translates to:
  /// **'Comparison code'**
  String get compareDigitsLabel;

  /// No description provided for @compareProves.
  ///
  /// In en, this message translates to:
  /// **'A match proves that your two devices hold each other\'s true account keys, so the devices reading this chat are ones you two added. It proves nothing about the code a browser is running, or about who is holding the other phone.'**
  String get compareProves;

  /// No description provided for @compareMark.
  ///
  /// In en, this message translates to:
  /// **'They match: mark {name} verified'**
  String compareMark(String name);

  /// No description provided for @compareMarked.
  ///
  /// In en, this message translates to:
  /// **'{name} is marked verified on this device. The mark is not synced; a new device starts without it.'**
  String compareMarked(String name);

  /// No description provided for @compareScan.
  ///
  /// In en, this message translates to:
  /// **'Scan their code'**
  String get compareScan;

  /// No description provided for @compareScanMatch.
  ///
  /// In en, this message translates to:
  /// **'The codes match.'**
  String get compareScanMatch;

  /// No description provided for @compareScanNoMatch.
  ///
  /// In en, this message translates to:
  /// **'The codes do not match. Do not mark them verified.'**
  String get compareScanNoMatch;

  /// No description provided for @compareNotYet.
  ///
  /// In en, this message translates to:
  /// **'The code appears once both device lists are known on this device.'**
  String get compareNotYet;

  /// No description provided for @newSealedTitle.
  ///
  /// In en, this message translates to:
  /// **'New sealed chat'**
  String get newSealedTitle;

  /// No description provided for @newSealedIntro.
  ///
  /// In en, this message translates to:
  /// **'A sealed chat is between two people only, and nobody can be added. It is encrypted on your devices: the server cannot read it, and it is never searched or exported. If you lose every way to unlock, its history is lost.'**
  String get newSealedIntro;

  /// No description provided for @newSealedLocked.
  ///
  /// In en, this message translates to:
  /// **'Unlock this device first.'**
  String get newSealedLocked;

  /// No description provided for @newSealedPick.
  ///
  /// In en, this message translates to:
  /// **'Who with?'**
  String get newSealedPick;

  /// No description provided for @reminderSet.
  ///
  /// In en, this message translates to:
  /// **'Reminder set for {when}.'**
  String reminderSet(String when);

  /// No description provided for @timedOutUntil.
  ///
  /// In en, this message translates to:
  /// **'You are timed out in this channel until {time}. You can read, but not post.'**
  String timedOutUntil(String time);

  /// No description provided for @slowModeWait.
  ///
  /// In en, this message translates to:
  /// **'Slow mode is on. You can post again in {time}.'**
  String slowModeWait(String time);

  /// No description provided for @channelCardPublic.
  ///
  /// In en, this message translates to:
  /// **'Public channel'**
  String get channelCardPublic;

  /// No description provided for @channelCardPrivate.
  ///
  /// In en, this message translates to:
  /// **'Private channel'**
  String get channelCardPrivate;

  /// No description provided for @channelCardLocked.
  ///
  /// In en, this message translates to:
  /// **'Locked channel'**
  String get channelCardLocked;

  /// No description provided for @channelCardArchived.
  ///
  /// In en, this message translates to:
  /// **'archived'**
  String get channelCardArchived;

  /// No description provided for @channelCardMembers.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 member} other{{count} members}}'**
  String channelCardMembers(int count);

  /// No description provided for @channelCardUnder.
  ///
  /// In en, this message translates to:
  /// **'Under #{parent}'**
  String channelCardUnder(String parent);

  /// No description provided for @channelCardOpen.
  ///
  /// In en, this message translates to:
  /// **'Open the channel'**
  String get channelCardOpen;

  /// No description provided for @channelCardView.
  ///
  /// In en, this message translates to:
  /// **'View the channel'**
  String get channelCardView;

  /// No description provided for @channelCardLabel.
  ///
  /// In en, this message translates to:
  /// **'About the channel {name}'**
  String channelCardLabel(String name);

  /// No description provided for @searchTitle.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get searchTitle;

  /// No description provided for @searchInWhich.
  ///
  /// In en, this message translates to:
  /// **'In which conversation?'**
  String get searchInWhich;

  /// No description provided for @searchFromWhom.
  ///
  /// In en, this message translates to:
  /// **'From whom?'**
  String get searchFromWhom;

  /// No description provided for @searchHint.
  ///
  /// In en, this message translates to:
  /// **'Words, \"a phrase\", depl*, -not, from:@sam, since:2026-11-01, has:file'**
  String get searchHint;

  /// No description provided for @searchAllServers.
  ///
  /// In en, this message translates to:
  /// **'All servers'**
  String get searchAllServers;

  /// No description provided for @searchIn.
  ///
  /// In en, this message translates to:
  /// **'In…'**
  String get searchIn;

  /// No description provided for @searchInNamed.
  ///
  /// In en, this message translates to:
  /// **'In {name}'**
  String searchInNamed(String name);

  /// No description provided for @searchFrom.
  ///
  /// In en, this message translates to:
  /// **'From…'**
  String get searchFrom;

  /// No description provided for @searchFromNamed.
  ///
  /// In en, this message translates to:
  /// **'From {name}'**
  String searchFromNamed(String name);

  /// No description provided for @searchSince.
  ///
  /// In en, this message translates to:
  /// **'Since…'**
  String get searchSince;

  /// No description provided for @searchSinceDate.
  ///
  /// In en, this message translates to:
  /// **'Since {date}'**
  String searchSinceDate(String date);

  /// No description provided for @searchUntil.
  ///
  /// In en, this message translates to:
  /// **'Until…'**
  String get searchUntil;

  /// No description provided for @searchUntilDate.
  ///
  /// In en, this message translates to:
  /// **'Until {date}'**
  String searchUntilDate(String date);

  /// No description provided for @searchHasFile.
  ///
  /// In en, this message translates to:
  /// **'Has a file'**
  String get searchHasFile;

  /// No description provided for @searchNoSealed.
  ///
  /// In en, this message translates to:
  /// **'Sealed chats are never searched, on any server.'**
  String get searchNoSealed;

  /// No description provided for @searchCouldNot.
  ///
  /// In en, this message translates to:
  /// **'Could not search {server}'**
  String searchCouldNot(String server);

  /// No description provided for @searchWaiting.
  ///
  /// In en, this message translates to:
  /// **'Still waiting for {servers}…'**
  String searchWaiting(String servers);

  /// No description provided for @searchNothing.
  ///
  /// In en, this message translates to:
  /// **'Nothing found.'**
  String get searchNothing;

  /// No description provided for @searchEmpty.
  ///
  /// In en, this message translates to:
  /// **'Search messages on every server.'**
  String get searchEmpty;

  /// No description provided for @searchMore.
  ///
  /// In en, this message translates to:
  /// **'More'**
  String get searchMore;

  /// No description provided for @searchAConversation.
  ///
  /// In en, this message translates to:
  /// **'a conversation'**
  String get searchAConversation;

  /// No description provided for @searchHitLabel.
  ///
  /// In en, this message translates to:
  /// **'{who} in {where}, {when}: {text}'**
  String searchHitLabel(String who, String where, String when, String text);

  /// No description provided for @searchHitLabelServer.
  ///
  /// In en, this message translates to:
  /// **'{who} in {where}, {when}, {server}: {text}'**
  String searchHitLabelServer(
    String who,
    String where,
    String when,
    String server,
    String text,
  );

  /// No description provided for @inAThread.
  ///
  /// In en, this message translates to:
  /// **'in a thread'**
  String get inAThread;

  /// No description provided for @filter.
  ///
  /// In en, this message translates to:
  /// **'Filter'**
  String get filter;

  /// No description provided for @goTo.
  ///
  /// In en, this message translates to:
  /// **'Go to'**
  String get goTo;

  /// No description provided for @switcherHint.
  ///
  /// In en, this message translates to:
  /// **'Find a conversation or a person'**
  String get switcherHint;

  /// No description provided for @nothingMatches.
  ///
  /// In en, this message translates to:
  /// **'Nothing matches.'**
  String get nothingMatches;

  /// No description provided for @sealedChat.
  ///
  /// In en, this message translates to:
  /// **'Sealed chat'**
  String get sealedChat;

  /// No description provided for @chat.
  ///
  /// In en, this message translates to:
  /// **'Chat'**
  String get chat;

  /// No description provided for @hiddenTag.
  ///
  /// In en, this message translates to:
  /// **'hidden'**
  String get hiddenTag;

  /// No description provided for @whichServer.
  ///
  /// In en, this message translates to:
  /// **'Which server?'**
  String get whichServer;

  /// No description provided for @refusalLocked.
  ///
  /// In en, this message translates to:
  /// **'This channel is locked: nobody can be added or join.'**
  String get refusalLocked;

  /// No description provided for @refusalArchived.
  ///
  /// In en, this message translates to:
  /// **'This channel is archived.'**
  String get refusalArchived;

  /// No description provided for @refusalForbidden.
  ///
  /// In en, this message translates to:
  /// **'You may not do that here.'**
  String get refusalForbidden;

  /// No description provided for @refusalTooLarge.
  ///
  /// In en, this message translates to:
  /// **'An ad-hoc channel holds at most 50 people. Make a named channel instead.'**
  String get refusalTooLarge;

  /// No description provided for @browseTitle.
  ///
  /// In en, this message translates to:
  /// **'Browse channels · {server}'**
  String browseTitle(String server);

  /// No description provided for @browseFilterHint.
  ///
  /// In en, this message translates to:
  /// **'Filter by name or topic'**
  String get browseFilterHint;

  /// No description provided for @browseIncludeArchived.
  ///
  /// In en, this message translates to:
  /// **'Include archived channels'**
  String get browseIncludeArchived;

  /// No description provided for @browseNone.
  ///
  /// In en, this message translates to:
  /// **'No channels.'**
  String get browseNone;

  /// No description provided for @archivedTag.
  ///
  /// In en, this message translates to:
  /// **'archived'**
  String get archivedTag;

  /// No description provided for @sameNameAsAnother.
  ///
  /// In en, this message translates to:
  /// **'same name as another'**
  String get sameNameAsAnother;

  /// No description provided for @open.
  ///
  /// In en, this message translates to:
  /// **'Open'**
  String get open;

  /// No description provided for @join.
  ///
  /// In en, this message translates to:
  /// **'Join'**
  String get join;

  /// No description provided for @subchannelsOf.
  ///
  /// In en, this message translates to:
  /// **'Sub-channels of {name}'**
  String subchannelsOf(String name);

  /// No description provided for @thisChannel.
  ///
  /// In en, this message translates to:
  /// **'this channel'**
  String get thisChannel;

  /// No description provided for @noPublicSubchannels.
  ///
  /// In en, this message translates to:
  /// **'There are no public sub-channels to join.'**
  String get noPublicSubchannels;

  /// No description provided for @joiningDoesNotJoinSubs.
  ///
  /// In en, this message translates to:
  /// **'Joining a channel does not join its sub-channels. Pick any you want.'**
  String get joiningDoesNotJoinSubs;

  /// No description provided for @memberCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 member} other{{count} members}}'**
  String memberCount(int count);

  /// No description provided for @joinSelected.
  ///
  /// In en, this message translates to:
  /// **'Join selected'**
  String get joinSelected;

  /// No description provided for @newMenuTitle.
  ///
  /// In en, this message translates to:
  /// **'New'**
  String get newMenuTitle;

  /// No description provided for @newChannelOption.
  ///
  /// In en, this message translates to:
  /// **'Channel'**
  String get newChannelOption;

  /// No description provided for @newChannelTitle.
  ///
  /// In en, this message translates to:
  /// **'New channel · {server}'**
  String newChannelTitle(String server);

  /// No description provided for @newSubchannelTitle.
  ///
  /// In en, this message translates to:
  /// **'New sub-channel'**
  String get newSubchannelTitle;

  /// No description provided for @channelNameLabel.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get channelNameLabel;

  /// No description provided for @channelNameHelp.
  ///
  /// In en, this message translates to:
  /// **'Any words, in any script. Spaces and capitals are fine.'**
  String get channelNameHelp;

  /// No description provided for @sameNameOne.
  ///
  /// In en, this message translates to:
  /// **'A channel you can see already has this name. You can still use it.'**
  String get sameNameOne;

  /// No description provided for @sameNameMany.
  ///
  /// In en, this message translates to:
  /// **'{count} channels you can see already have this name. You can still use it.'**
  String sameNameMany(int count);

  /// No description provided for @channelTopicOptional.
  ///
  /// In en, this message translates to:
  /// **'Topic (optional)'**
  String get channelTopicOptional;

  /// No description provided for @channelUnder.
  ///
  /// In en, this message translates to:
  /// **'Under'**
  String get channelUnder;

  /// No description provided for @channelTopLevel.
  ///
  /// In en, this message translates to:
  /// **'Nothing (top level)'**
  String get channelTopLevel;

  /// No description provided for @subchannelExplained.
  ///
  /// In en, this message translates to:
  /// **'A sub-channel is listed under its parent and keeps its own members and settings.'**
  String get subchannelExplained;

  /// No description provided for @channelSection.
  ///
  /// In en, this message translates to:
  /// **'Section'**
  String get channelSection;

  /// No description provided for @none.
  ///
  /// In en, this message translates to:
  /// **'None'**
  String get none;

  /// No description provided for @channelPrivate.
  ///
  /// In en, this message translates to:
  /// **'Private'**
  String get channelPrivate;

  /// No description provided for @channelPrivateNote.
  ///
  /// In en, this message translates to:
  /// **'Only people who are added can find and read it. A private channel can never be made public.'**
  String get channelPrivateNote;

  /// No description provided for @channelAnnouncements.
  ///
  /// In en, this message translates to:
  /// **'Announcements only'**
  String get channelAnnouncements;

  /// No description provided for @channelAnnouncementsNote.
  ///
  /// In en, this message translates to:
  /// **'Only its managers and the people they allow post. Everyone else reads and reacts.'**
  String get channelAnnouncementsNote;

  /// No description provided for @newChatTitle.
  ///
  /// In en, this message translates to:
  /// **'New chat · {server}'**
  String newChatTitle(String server);

  /// No description provided for @openChat.
  ///
  /// In en, this message translates to:
  /// **'Open chat'**
  String get openChat;

  /// No description provided for @chatHistoryNotice.
  ///
  /// In en, this message translates to:
  /// **'Anyone added to a chat later sees its whole history.'**
  String get chatHistoryNotice;

  /// No description provided for @findPeople.
  ///
  /// In en, this message translates to:
  /// **'Find people'**
  String get findPeople;

  /// No description provided for @botTooOld.
  ///
  /// In en, this message translates to:
  /// **'{bot} asked something this app is too old to show.'**
  String botTooOld(String bot);

  /// No description provided for @aBot.
  ///
  /// In en, this message translates to:
  /// **'A bot'**
  String get aBot;

  /// No description provided for @aBotAsks.
  ///
  /// In en, this message translates to:
  /// **'A bot asks'**
  String get aBotAsks;

  /// No description provided for @onlyYouSee.
  ///
  /// In en, this message translates to:
  /// **'Only you can see this.'**
  String get onlyYouSee;

  /// No description provided for @fromBotOnlyYou.
  ///
  /// In en, this message translates to:
  /// **'From {bot}. Only you can see this.'**
  String fromBotOnlyYou(String bot);

  /// No description provided for @botNotConnected.
  ///
  /// In en, this message translates to:
  /// **'The bot is not connected. Try again later.'**
  String get botNotConnected;

  /// No description provided for @answer.
  ///
  /// In en, this message translates to:
  /// **'Answer'**
  String get answer;

  /// No description provided for @pickADate.
  ///
  /// In en, this message translates to:
  /// **'Pick a date'**
  String get pickADate;

  /// No description provided for @pickATime.
  ///
  /// In en, this message translates to:
  /// **'Pick a time'**
  String get pickATime;

  /// No description provided for @choose.
  ///
  /// In en, this message translates to:
  /// **'Choose…'**
  String get choose;

  /// No description provided for @aChannel.
  ///
  /// In en, this message translates to:
  /// **'A channel'**
  String get aChannel;

  /// No description provided for @typeAName.
  ///
  /// In en, this message translates to:
  /// **'Type a name'**
  String get typeAName;

  /// No description provided for @typeAChannelName.
  ///
  /// In en, this message translates to:
  /// **'Type a channel name'**
  String get typeAChannelName;

  /// No description provided for @formNotPosted.
  ///
  /// In en, this message translates to:
  /// **'The form was not posted. {reason}'**
  String formNotPosted(String reason);

  /// No description provided for @newPoll.
  ///
  /// In en, this message translates to:
  /// **'New poll'**
  String get newPoll;

  /// No description provided for @newRsvp.
  ///
  /// In en, this message translates to:
  /// **'New RSVP'**
  String get newRsvp;

  /// No description provided for @newSignup.
  ///
  /// In en, this message translates to:
  /// **'New sign-up sheet'**
  String get newSignup;

  /// No description provided for @askApproval.
  ///
  /// In en, this message translates to:
  /// **'Ask for approval'**
  String get askApproval;

  /// No description provided for @newForm.
  ///
  /// In en, this message translates to:
  /// **'New form'**
  String get newForm;

  /// No description provided for @pick.
  ///
  /// In en, this message translates to:
  /// **'Pick'**
  String get pick;

  /// No description provided for @noValue.
  ///
  /// In en, this message translates to:
  /// **'No {label}'**
  String noValue(String label);

  /// No description provided for @question.
  ///
  /// In en, this message translates to:
  /// **'Question'**
  String get question;

  /// No description provided for @titleOptional.
  ///
  /// In en, this message translates to:
  /// **'Title (optional)'**
  String get titleOptional;

  /// No description provided for @what.
  ///
  /// In en, this message translates to:
  /// **'What'**
  String get what;

  /// No description provided for @optionN.
  ///
  /// In en, this message translates to:
  /// **'Option {n}'**
  String optionN(int n);

  /// No description provided for @slotN.
  ///
  /// In en, this message translates to:
  /// **'Slot {n}'**
  String slotN(int n);

  /// No description provided for @slotPlacesHelp.
  ///
  /// In en, this message translates to:
  /// **'Add places in brackets: Morning (3)'**
  String get slotPlacesHelp;

  /// No description provided for @addOption.
  ///
  /// In en, this message translates to:
  /// **'Add an option'**
  String get addOption;

  /// No description provided for @addSlot.
  ///
  /// In en, this message translates to:
  /// **'Add a slot'**
  String get addSlot;

  /// No description provided for @pickMoreThanOne.
  ///
  /// In en, this message translates to:
  /// **'People may pick more than one'**
  String get pickMoreThanOne;

  /// No description provided for @anonymous.
  ///
  /// In en, this message translates to:
  /// **'Anonymous'**
  String get anonymous;

  /// No description provided for @pollAnonymousNote.
  ///
  /// In en, this message translates to:
  /// **'Nobody, not even you, can see who voted for what. This cannot be changed later.'**
  String get pollAnonymousNote;

  /// No description provided for @mayAddOptions.
  ///
  /// In en, this message translates to:
  /// **'People may add options'**
  String get mayAddOptions;

  /// No description provided for @showResults.
  ///
  /// In en, this message translates to:
  /// **'Show results'**
  String get showResults;

  /// No description provided for @resultsLive.
  ///
  /// In en, this message translates to:
  /// **'Live'**
  String get resultsLive;

  /// No description provided for @resultsAfterAnswer.
  ///
  /// In en, this message translates to:
  /// **'After answering'**
  String get resultsAfterAnswer;

  /// No description provided for @resultsAfterClose.
  ///
  /// In en, this message translates to:
  /// **'After closing'**
  String get resultsAfterClose;

  /// No description provided for @when.
  ///
  /// In en, this message translates to:
  /// **'When'**
  String get when;

  /// No description provided for @whoMayAnswer.
  ///
  /// In en, this message translates to:
  /// **'Who may answer'**
  String get whoMayAnswer;

  /// No description provided for @anyoneHere.
  ///
  /// In en, this message translates to:
  /// **'Anyone here'**
  String get anyoneHere;

  /// No description provided for @closingTime.
  ///
  /// In en, this message translates to:
  /// **'Closing time'**
  String get closingTime;

  /// No description provided for @post.
  ///
  /// In en, this message translates to:
  /// **'Post'**
  String get post;

  /// No description provided for @fieldOneLine.
  ///
  /// In en, this message translates to:
  /// **'One line'**
  String get fieldOneLine;

  /// No description provided for @fieldSeveralLines.
  ///
  /// In en, this message translates to:
  /// **'Several lines'**
  String get fieldSeveralLines;

  /// No description provided for @fieldChoice.
  ///
  /// In en, this message translates to:
  /// **'Choice'**
  String get fieldChoice;

  /// No description provided for @fieldNumber.
  ///
  /// In en, this message translates to:
  /// **'Number'**
  String get fieldNumber;

  /// No description provided for @fieldDate.
  ///
  /// In en, this message translates to:
  /// **'Date'**
  String get fieldDate;

  /// No description provided for @fieldTime.
  ///
  /// In en, this message translates to:
  /// **'Time'**
  String get fieldTime;

  /// No description provided for @fieldPeople.
  ///
  /// In en, this message translates to:
  /// **'People'**
  String get fieldPeople;

  /// No description provided for @fieldChannels.
  ///
  /// In en, this message translates to:
  /// **'Channels'**
  String get fieldChannels;

  /// No description provided for @fieldTickBox.
  ///
  /// In en, this message translates to:
  /// **'Tick-box'**
  String get fieldTickBox;

  /// No description provided for @fieldN.
  ///
  /// In en, this message translates to:
  /// **'Field {n}'**
  String fieldN(int n);

  /// No description provided for @removeField.
  ///
  /// In en, this message translates to:
  /// **'Remove field'**
  String get removeField;

  /// No description provided for @fieldLabel.
  ///
  /// In en, this message translates to:
  /// **'Label'**
  String get fieldLabel;

  /// No description provided for @helpOptional.
  ///
  /// In en, this message translates to:
  /// **'Help (optional)'**
  String get helpOptional;

  /// No description provided for @optionsPerLine.
  ///
  /// In en, this message translates to:
  /// **'Options, one per line'**
  String get optionsPerLine;

  /// No description provided for @required.
  ///
  /// In en, this message translates to:
  /// **'Required'**
  String get required;

  /// No description provided for @moreThanOne.
  ///
  /// In en, this message translates to:
  /// **'More than one'**
  String get moreThanOne;

  /// No description provided for @addField.
  ///
  /// In en, this message translates to:
  /// **'Add a field'**
  String get addField;

  /// No description provided for @buttonN.
  ///
  /// In en, this message translates to:
  /// **'Button {n}'**
  String buttonN(int n);

  /// No description provided for @buttonPlain.
  ///
  /// In en, this message translates to:
  /// **'Plain'**
  String get buttonPlain;

  /// No description provided for @buttonMain.
  ///
  /// In en, this message translates to:
  /// **'Main'**
  String get buttonMain;

  /// No description provided for @buttonDanger.
  ///
  /// In en, this message translates to:
  /// **'Danger'**
  String get buttonDanger;

  /// No description provided for @removeButton.
  ///
  /// In en, this message translates to:
  /// **'Remove button'**
  String get removeButton;

  /// No description provided for @addButton.
  ///
  /// In en, this message translates to:
  /// **'Add a button'**
  String get addButton;

  /// No description provided for @formAnonymousNote.
  ///
  /// In en, this message translates to:
  /// **'Answers carry no names, for anyone. This cannot be changed later.'**
  String get formAnonymousNote;

  /// No description provided for @formAnswersGo.
  ///
  /// In en, this message translates to:
  /// **'Answers go to you and the channel\'s managers, and can be downloaded.'**
  String get formAnswersGo;

  /// No description provided for @channel.
  ///
  /// In en, this message translates to:
  /// **'Channel'**
  String get channel;

  /// No description provided for @levelReader.
  ///
  /// In en, this message translates to:
  /// **'Reader'**
  String get levelReader;

  /// No description provided for @levelMember.
  ///
  /// In en, this message translates to:
  /// **'Member'**
  String get levelMember;

  /// No description provided for @levelManager.
  ///
  /// In en, this message translates to:
  /// **'Manager'**
  String get levelManager;

  /// No description provided for @makeLevel.
  ///
  /// In en, this message translates to:
  /// **'Make {level}'**
  String makeLevel(String level);

  /// No description provided for @makeGroupLevel.
  ///
  /// In en, this message translates to:
  /// **'Make {level}s'**
  String makeGroupLevel(String level);

  /// No description provided for @off.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get off;

  /// No description provided for @seconds10.
  ///
  /// In en, this message translates to:
  /// **'10 seconds'**
  String get seconds10;

  /// No description provided for @seconds30.
  ///
  /// In en, this message translates to:
  /// **'30 seconds'**
  String get seconds30;

  /// No description provided for @minute1.
  ///
  /// In en, this message translates to:
  /// **'1 minute'**
  String get minute1;

  /// No description provided for @minutes5.
  ///
  /// In en, this message translates to:
  /// **'5 minutes'**
  String get minutes5;

  /// No description provided for @minutes15.
  ///
  /// In en, this message translates to:
  /// **'15 minutes'**
  String get minutes15;

  /// No description provided for @hour1.
  ///
  /// In en, this message translates to:
  /// **'1 hour'**
  String get hour1;

  /// No description provided for @retentionServer.
  ///
  /// In en, this message translates to:
  /// **'The server\'s period'**
  String get retentionServer;

  /// No description provided for @day1.
  ///
  /// In en, this message translates to:
  /// **'1 day'**
  String get day1;

  /// No description provided for @days7.
  ///
  /// In en, this message translates to:
  /// **'7 days'**
  String get days7;

  /// No description provided for @days30.
  ///
  /// In en, this message translates to:
  /// **'30 days'**
  String get days30;

  /// No description provided for @days90.
  ///
  /// In en, this message translates to:
  /// **'90 days'**
  String get days90;

  /// No description provided for @year1.
  ///
  /// In en, this message translates to:
  /// **'1 year'**
  String get year1;

  /// No description provided for @noLongerInConversation.
  ///
  /// In en, this message translates to:
  /// **'You are no longer in this conversation.'**
  String get noLongerInConversation;

  /// No description provided for @tabAbout.
  ///
  /// In en, this message translates to:
  /// **'About'**
  String get tabAbout;

  /// No description provided for @tabMembers.
  ///
  /// In en, this message translates to:
  /// **'Members ({count})'**
  String tabMembers(int count);

  /// No description provided for @tabPins.
  ///
  /// In en, this message translates to:
  /// **'Pins'**
  String get tabPins;

  /// No description provided for @nameInUse.
  ///
  /// In en, this message translates to:
  /// **'That name is in use'**
  String get nameInUse;

  /// No description provided for @nameInUseNote.
  ///
  /// In en, this message translates to:
  /// **'Channels you can see already have this name. Names need not be unique.'**
  String get nameInUseNote;

  /// No description provided for @useItAnyway.
  ///
  /// In en, this message translates to:
  /// **'Use it anyway'**
  String get useItAnyway;

  /// No description provided for @parentChannel.
  ///
  /// In en, this message translates to:
  /// **'Parent channel'**
  String get parentChannel;

  /// No description provided for @parentChangeNote.
  ///
  /// In en, this message translates to:
  /// **'This changes where the channel is listed and nothing else: not who is in it, not whether it is private.'**
  String get parentChangeNote;

  /// No description provided for @noneTopLevel.
  ///
  /// In en, this message translates to:
  /// **'None (top level)'**
  String get noneTopLevel;

  /// No description provided for @typeToConfirm.
  ///
  /// In en, this message translates to:
  /// **'Type \"{word}\" to confirm.'**
  String typeToConfirm(String word);

  /// No description provided for @makePrivateTitle.
  ///
  /// In en, this message translates to:
  /// **'Make this channel private?'**
  String get makePrivateTitle;

  /// No description provided for @makePrivateLine1.
  ///
  /// In en, this message translates to:
  /// **'People who are not members lose the channel at once: its history and its search results go with it.'**
  String get makePrivateLine1;

  /// No description provided for @makePrivateLine2.
  ///
  /// In en, this message translates to:
  /// **'A private channel can never be made public again.'**
  String get makePrivateLine2;

  /// No description provided for @makePrivate.
  ///
  /// In en, this message translates to:
  /// **'Make private'**
  String get makePrivate;

  /// No description provided for @lockTitle.
  ///
  /// In en, this message translates to:
  /// **'Lock this channel for good?'**
  String get lockTitle;

  /// No description provided for @lockLine1.
  ///
  /// In en, this message translates to:
  /// **'Nobody can be added or join, and a group attached to the channel brings in nobody new. The people here now stay.'**
  String get lockLine1;

  /// No description provided for @lockLine2.
  ///
  /// In en, this message translates to:
  /// **'People can still leave, and someone who leaves can never come back.'**
  String get lockLine2;

  /// No description provided for @lockLine3.
  ///
  /// In en, this message translates to:
  /// **'This cannot be undone.'**
  String get lockLine3;

  /// No description provided for @lockWord.
  ///
  /// In en, this message translates to:
  /// **'lock'**
  String get lockWord;

  /// No description provided for @lock.
  ///
  /// In en, this message translates to:
  /// **'Lock'**
  String get lock;

  /// No description provided for @unarchiveTitle.
  ///
  /// In en, this message translates to:
  /// **'Bring this channel back?'**
  String get unarchiveTitle;

  /// No description provided for @archiveTitle.
  ///
  /// In en, this message translates to:
  /// **'Archive this channel?'**
  String get archiveTitle;

  /// No description provided for @unarchiveNote.
  ///
  /// In en, this message translates to:
  /// **'People can post here again.'**
  String get unarchiveNote;

  /// No description provided for @archiveNote.
  ///
  /// In en, this message translates to:
  /// **'An archived channel can still be read and searched, and takes no new posts.'**
  String get archiveNote;

  /// No description provided for @unarchive.
  ///
  /// In en, this message translates to:
  /// **'Unarchive'**
  String get unarchive;

  /// No description provided for @archive.
  ///
  /// In en, this message translates to:
  /// **'Archive'**
  String get archive;

  /// No description provided for @cannotLeaveTitle.
  ///
  /// In en, this message translates to:
  /// **'You cannot leave on your own'**
  String get cannotLeaveTitle;

  /// No description provided for @cannotLeaveNote.
  ///
  /// In en, this message translates to:
  /// **'You are here through a group. Hide the channel or set it to notify nothing, or leave the group if it is open.'**
  String get cannotLeaveNote;

  /// No description provided for @ok.
  ///
  /// In en, this message translates to:
  /// **'OK'**
  String get ok;

  /// No description provided for @leaveTitle.
  ///
  /// In en, this message translates to:
  /// **'Leave this channel?'**
  String get leaveTitle;

  /// No description provided for @leaveLocked.
  ///
  /// In en, this message translates to:
  /// **'This channel is locked: once you leave, there is no way back.'**
  String get leaveLocked;

  /// No description provided for @leavePrivate.
  ///
  /// In en, this message translates to:
  /// **'You will need to be added again to come back.'**
  String get leavePrivate;

  /// No description provided for @leaveLast.
  ///
  /// In en, this message translates to:
  /// **'You are the last person here. The channel and everything in it will be deleted.'**
  String get leaveLast;

  /// No description provided for @leave.
  ///
  /// In en, this message translates to:
  /// **'Leave'**
  String get leave;

  /// No description provided for @requestWebhook.
  ///
  /// In en, this message translates to:
  /// **'Request a webhook'**
  String get requestWebhook;

  /// No description provided for @webhookNote.
  ///
  /// In en, this message translates to:
  /// **'An admin (or a manager, if allowed) approves it. Its messages appear under the name you give.'**
  String get webhookNote;

  /// No description provided for @webhookPurpose.
  ///
  /// In en, this message translates to:
  /// **'What it is for'**
  String get webhookPurpose;

  /// No description provided for @request.
  ///
  /// In en, this message translates to:
  /// **'Request'**
  String get request;

  /// No description provided for @webhookRequested.
  ///
  /// In en, this message translates to:
  /// **'Requested. You will hear when it is decided.'**
  String get webhookRequested;

  /// No description provided for @webhookTooMany.
  ///
  /// In en, this message translates to:
  /// **'You have three requests waiting already.'**
  String get webhookTooMany;

  /// No description provided for @chatPeople.
  ///
  /// In en, this message translates to:
  /// **'Chat · {count} people'**
  String chatPeople(int count);

  /// No description provided for @lockedNote.
  ///
  /// In en, this message translates to:
  /// **'Locked: nobody can be added or join, and someone who leaves cannot come back.'**
  String get lockedNote;

  /// No description provided for @archivedNote.
  ///
  /// In en, this message translates to:
  /// **'Archived: it can be read and searched, and takes no posts.'**
  String get archivedNote;

  /// No description provided for @nameChatNote.
  ///
  /// In en, this message translates to:
  /// **'Name (naming a chat makes it a named private channel)'**
  String get nameChatNote;

  /// No description provided for @topic.
  ///
  /// In en, this message translates to:
  /// **'Topic'**
  String get topic;

  /// No description provided for @welcomeText.
  ///
  /// In en, this message translates to:
  /// **'Welcome text (Churr Markdown, shown on joining)'**
  String get welcomeText;

  /// No description provided for @peopleJoinAs.
  ///
  /// In en, this message translates to:
  /// **'People join as'**
  String get peopleJoinAs;

  /// No description provided for @joinAsMembers.
  ///
  /// In en, this message translates to:
  /// **'Members'**
  String get joinAsMembers;

  /// No description provided for @joinAsReaders.
  ///
  /// In en, this message translates to:
  /// **'Readers (announcements)'**
  String get joinAsReaders;

  /// No description provided for @slowMode.
  ///
  /// In en, this message translates to:
  /// **'Slow mode'**
  String get slowMode;

  /// No description provided for @keepMessagesFor.
  ///
  /// In en, this message translates to:
  /// **'Keep messages for'**
  String get keepMessagesFor;

  /// No description provided for @subInParentSection.
  ///
  /// In en, this message translates to:
  /// **'A sub-channel is listed in its parent\'s section.'**
  String get subInParentSection;

  /// No description provided for @subchannels.
  ///
  /// In en, this message translates to:
  /// **'Sub-channels'**
  String get subchannels;

  /// No description provided for @subchannelsNote.
  ///
  /// In en, this message translates to:
  /// **'Public sub-channels you could join'**
  String get subchannelsNote;

  /// No description provided for @makePrivateNote.
  ///
  /// In en, this message translates to:
  /// **'People who are not members lose it at once. There is no way back to public.'**
  String get makePrivateNote;

  /// No description provided for @lockNote.
  ///
  /// In en, this message translates to:
  /// **'Nobody can be added or join again. This cannot be undone.'**
  String get lockNote;

  /// No description provided for @throughGroup.
  ///
  /// In en, this message translates to:
  /// **'You are here through a group.'**
  String get throughGroup;

  /// No description provided for @addPeople.
  ///
  /// In en, this message translates to:
  /// **'Add people'**
  String get addPeople;

  /// No description provided for @addAs.
  ///
  /// In en, this message translates to:
  /// **'Add as'**
  String get addAs;

  /// No description provided for @add.
  ///
  /// In en, this message translates to:
  /// **'Add'**
  String get add;

  /// No description provided for @addedSeesHistoryChat.
  ///
  /// In en, this message translates to:
  /// **'Anyone added to a chat sees its whole history.'**
  String get addedSeesHistoryChat;

  /// No description provided for @addedSeesHistory.
  ///
  /// In en, this message translates to:
  /// **'Whoever is added sees the whole history.'**
  String get addedSeesHistory;

  /// No description provided for @addGroup.
  ///
  /// In en, this message translates to:
  /// **'Add a group'**
  String get addGroup;

  /// No description provided for @addGroupNote.
  ///
  /// In en, this message translates to:
  /// **'Everyone in the group is in the channel at that level, and stays in step as the group changes.'**
  String get addGroupNote;

  /// No description provided for @pickGroup.
  ///
  /// In en, this message translates to:
  /// **'Pick a group'**
  String get pickGroup;

  /// No description provided for @removeViaGroup.
  ///
  /// In en, this message translates to:
  /// **'They are here through a group: remove the group, or take them out of it.'**
  String get removeViaGroup;

  /// No description provided for @removeName.
  ///
  /// In en, this message translates to:
  /// **'Remove {name}?'**
  String removeName(String name);

  /// No description provided for @them.
  ///
  /// In en, this message translates to:
  /// **'them'**
  String get them;

  /// No description provided for @removeLocked.
  ///
  /// In en, this message translates to:
  /// **'This channel is locked: they will not be able to come back.'**
  String get removeLocked;

  /// No description provided for @membersLocked.
  ///
  /// In en, this message translates to:
  /// **'This channel is locked, so nobody can be added.'**
  String get membersLocked;

  /// No description provided for @groupsHeading.
  ///
  /// In en, this message translates to:
  /// **'GROUPS'**
  String get groupsHeading;

  /// No description provided for @peopleHeading.
  ///
  /// In en, this message translates to:
  /// **'PEOPLE'**
  String get peopleHeading;

  /// No description provided for @group.
  ///
  /// In en, this message translates to:
  /// **'group'**
  String get group;

  /// No description provided for @groupOptions.
  ///
  /// In en, this message translates to:
  /// **'Group options'**
  String get groupOptions;

  /// No description provided for @removeGroup.
  ///
  /// In en, this message translates to:
  /// **'Remove group'**
  String get removeGroup;

  /// No description provided for @hereThroughGroup.
  ///
  /// In en, this message translates to:
  /// **'here through a group'**
  String get hereThroughGroup;

  /// No description provided for @alsoThroughGroup.
  ///
  /// In en, this message translates to:
  /// **'also through a group'**
  String get alsoThroughGroup;

  /// No description provided for @memberOptions.
  ///
  /// In en, this message translates to:
  /// **'Member options'**
  String get memberOptions;

  /// No description provided for @nothingPinned.
  ///
  /// In en, this message translates to:
  /// **'Nothing is pinned here.'**
  String get nothingPinned;

  /// No description provided for @aMessage.
  ///
  /// In en, this message translates to:
  /// **'A message'**
  String get aMessage;

  /// No description provided for @pinnedBy.
  ///
  /// In en, this message translates to:
  /// **'pinned by {name}'**
  String pinnedBy(String name);

  /// No description provided for @pinnedByAt.
  ///
  /// In en, this message translates to:
  /// **'pinned by {name} · {when}'**
  String pinnedByAt(String name, String when);

  /// No description provided for @someoneLower.
  ///
  /// In en, this message translates to:
  /// **'someone'**
  String get someoneLower;

  /// Shown while the live connection waits to try again; counts down each second.
  ///
  /// In en, this message translates to:
  /// **'Reconnecting in {seconds, plural, =1{1 second} other{{seconds} seconds}}'**
  String reconnectingIn(int seconds);

  /// No description provided for @notConnectedBanner.
  ///
  /// In en, this message translates to:
  /// **'Not connected. Messages you send will go when it is back.'**
  String get notConnectedBanner;

  /// No description provided for @reconnectingBanner.
  ///
  /// In en, this message translates to:
  /// **'Not connected. Reconnecting in {seconds, plural, =1{1 second} other{{seconds} seconds}}. Messages you send will go when it is back.'**
  String reconnectingBanner(int seconds);

  /// No description provided for @broadGoesPlain.
  ///
  /// In en, this message translates to:
  /// **'Only managers can use @channel, @here and @everyone here. This will be sent as plain text and will notify nobody.'**
  String get broadGoesPlain;

  /// No description provided for @broadWentPlain.
  ///
  /// In en, this message translates to:
  /// **'Only managers can use @channel, @here and @everyone here. Your message was sent with them as plain text and notified nobody.'**
  String get broadWentPlain;

  /// No description provided for @broadConfirmTitle.
  ///
  /// In en, this message translates to:
  /// **'Notify everyone?'**
  String get broadConfirmTitle;

  /// No description provided for @broadConfirmBody.
  ///
  /// In en, this message translates to:
  /// **'This will notify {count} people.'**
  String broadConfirmBody(int count);

  /// No description provided for @broadConfirmSend.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get broadConfirmSend;

  /// No description provided for @broadConfirmCancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get broadConfirmCancel;

  /// No description provided for @ignoreBroadMentions.
  ///
  /// In en, this message translates to:
  /// **'Ignore @channel, @here and @everyone'**
  String get ignoreBroadMentions;

  /// No description provided for @ignoreBroadMentionsHint.
  ///
  /// In en, this message translates to:
  /// **'They will not notify you here. A mention of you by name, or of another group you are in, still does.'**
  String get ignoreBroadMentionsHint;

  /// No description provided for @broadMentionsSetting.
  ///
  /// In en, this message translates to:
  /// **'@channel, @here and @everyone'**
  String get broadMentionsSetting;

  /// No description provided for @broadMentionsSettingHint.
  ///
  /// In en, this message translates to:
  /// **'Who may notify the whole channel. The server lets anyone, until a channel is larger than the size its admins set; then only managers. Anyone else\'s is posted as plain text.'**
  String get broadMentionsSettingHint;

  /// No description provided for @broadMentionsServer.
  ///
  /// In en, this message translates to:
  /// **'Follow the server'**
  String get broadMentionsServer;

  /// No description provided for @broadMentionsAnyone.
  ///
  /// In en, this message translates to:
  /// **'Anyone'**
  String get broadMentionsAnyone;

  /// No description provided for @broadMentionsManagers.
  ///
  /// In en, this message translates to:
  /// **'Managers only'**
  String get broadMentionsManagers;

  /// No description provided for @cardSuitAcorns.
  ///
  /// In en, this message translates to:
  /// **'acorns'**
  String get cardSuitAcorns;

  /// No description provided for @cardSuitBamboo.
  ///
  /// In en, this message translates to:
  /// **'bamboo'**
  String get cardSuitBamboo;

  /// No description provided for @cardSuitBatons.
  ///
  /// In en, this message translates to:
  /// **'clubs'**
  String get cardSuitBatons;

  /// No description provided for @cardSuitBells.
  ///
  /// In en, this message translates to:
  /// **'bells'**
  String get cardSuitBells;

  /// No description provided for @cardSuitCharacters.
  ///
  /// In en, this message translates to:
  /// **'characters'**
  String get cardSuitCharacters;

  /// No description provided for @cardSuitCherry.
  ///
  /// In en, this message translates to:
  /// **'cherry blossom'**
  String get cardSuitCherry;

  /// No description provided for @cardSuitChrysanthemum.
  ///
  /// In en, this message translates to:
  /// **'chrysanthemum'**
  String get cardSuitChrysanthemum;

  /// No description provided for @cardSuitClover.
  ///
  /// In en, this message translates to:
  /// **'bush clover'**
  String get cardSuitClover;

  /// No description provided for @cardSuitClubs.
  ///
  /// In en, this message translates to:
  /// **'clubs'**
  String get cardSuitClubs;

  /// No description provided for @cardSuitCoins.
  ///
  /// In en, this message translates to:
  /// **'coins'**
  String get cardSuitCoins;

  /// No description provided for @cardSuitCups.
  ///
  /// In en, this message translates to:
  /// **'cups'**
  String get cardSuitCups;

  /// No description provided for @cardSuitDiamonds.
  ///
  /// In en, this message translates to:
  /// **'diamonds'**
  String get cardSuitDiamonds;

  /// No description provided for @cardSuitDots.
  ///
  /// In en, this message translates to:
  /// **'dots'**
  String get cardSuitDots;

  /// No description provided for @cardSuitGheart.
  ///
  /// In en, this message translates to:
  /// **'hearts'**
  String get cardSuitGheart;

  /// No description provided for @cardSuitHearts.
  ///
  /// In en, this message translates to:
  /// **'hearts'**
  String get cardSuitHearts;

  /// No description provided for @cardSuitIris.
  ///
  /// In en, this message translates to:
  /// **'iris'**
  String get cardSuitIris;

  /// No description provided for @cardSuitLeaves.
  ///
  /// In en, this message translates to:
  /// **'leaves'**
  String get cardSuitLeaves;

  /// No description provided for @cardSuitMaple.
  ///
  /// In en, this message translates to:
  /// **'maple'**
  String get cardSuitMaple;

  /// No description provided for @cardSuitPampas.
  ///
  /// In en, this message translates to:
  /// **'pampas grass'**
  String get cardSuitPampas;

  /// No description provided for @cardSuitPaulownia.
  ///
  /// In en, this message translates to:
  /// **'paulownia'**
  String get cardSuitPaulownia;

  /// No description provided for @cardSuitPentacles.
  ///
  /// In en, this message translates to:
  /// **'pentacles'**
  String get cardSuitPentacles;

  /// No description provided for @cardSuitPeony.
  ///
  /// In en, this message translates to:
  /// **'peony'**
  String get cardSuitPeony;

  /// No description provided for @cardSuitPine.
  ///
  /// In en, this message translates to:
  /// **'pine'**
  String get cardSuitPine;

  /// No description provided for @cardSuitPlum.
  ///
  /// In en, this message translates to:
  /// **'plum blossom'**
  String get cardSuitPlum;

  /// No description provided for @cardSuitRoses.
  ///
  /// In en, this message translates to:
  /// **'roses'**
  String get cardSuitRoses;

  /// No description provided for @cardSuitShields.
  ///
  /// In en, this message translates to:
  /// **'shields'**
  String get cardSuitShields;

  /// No description provided for @cardSuitSpades.
  ///
  /// In en, this message translates to:
  /// **'spades'**
  String get cardSuitSpades;

  /// No description provided for @cardSuitSwords.
  ///
  /// In en, this message translates to:
  /// **'swords'**
  String get cardSuitSwords;

  /// No description provided for @cardSuitWands.
  ///
  /// In en, this message translates to:
  /// **'wands'**
  String get cardSuitWands;

  /// No description provided for @cardSuitWillow.
  ///
  /// In en, this message translates to:
  /// **'willow'**
  String get cardSuitWillow;

  /// No description provided for @cardSuitWisteria.
  ///
  /// In en, this message translates to:
  /// **'wisteria'**
  String get cardSuitWisteria;

  /// No description provided for @cardRankAce.
  ///
  /// In en, this message translates to:
  /// **'ace'**
  String get cardRankAce;

  /// No description provided for @cardRankBanner.
  ///
  /// In en, this message translates to:
  /// **'banner'**
  String get cardRankBanner;

  /// No description provided for @cardRankCaballo.
  ///
  /// In en, this message translates to:
  /// **'caballo'**
  String get cardRankCaballo;

  /// No description provided for @cardRankCavallo.
  ///
  /// In en, this message translates to:
  /// **'cavallo'**
  String get cardRankCavallo;

  /// No description provided for @cardRankFante.
  ///
  /// In en, this message translates to:
  /// **'fante'**
  String get cardRankFante;

  /// No description provided for @cardRankJack.
  ///
  /// In en, this message translates to:
  /// **'jack'**
  String get cardRankJack;

  /// No description provided for @cardRankKing.
  ///
  /// In en, this message translates to:
  /// **'king'**
  String get cardRankKing;

  /// No description provided for @cardRankKnight.
  ///
  /// In en, this message translates to:
  /// **'knight'**
  String get cardRankKnight;

  /// No description provided for @cardRankOber.
  ///
  /// In en, this message translates to:
  /// **'ober'**
  String get cardRankOber;

  /// No description provided for @cardRankPage.
  ///
  /// In en, this message translates to:
  /// **'page'**
  String get cardRankPage;

  /// No description provided for @cardRankQueen.
  ///
  /// In en, this message translates to:
  /// **'queen'**
  String get cardRankQueen;

  /// No description provided for @cardRankRe.
  ///
  /// In en, this message translates to:
  /// **'re'**
  String get cardRankRe;

  /// No description provided for @cardRankRey.
  ///
  /// In en, this message translates to:
  /// **'rey'**
  String get cardRankRey;

  /// No description provided for @cardRankSota.
  ///
  /// In en, this message translates to:
  /// **'sota'**
  String get cardRankSota;

  /// No description provided for @cardRankUnder.
  ///
  /// In en, this message translates to:
  /// **'under'**
  String get cardRankUnder;

  /// No description provided for @cardRankUnter.
  ///
  /// In en, this message translates to:
  /// **'unter'**
  String get cardRankUnter;

  /// No description provided for @cardExtraMajor0.
  ///
  /// In en, this message translates to:
  /// **'The Fool'**
  String get cardExtraMajor0;

  /// No description provided for @cardExtraMajor1.
  ///
  /// In en, this message translates to:
  /// **'The Magician'**
  String get cardExtraMajor1;

  /// No description provided for @cardExtraMajor2.
  ///
  /// In en, this message translates to:
  /// **'The High Priestess'**
  String get cardExtraMajor2;

  /// No description provided for @cardExtraMajor3.
  ///
  /// In en, this message translates to:
  /// **'The Empress'**
  String get cardExtraMajor3;

  /// No description provided for @cardExtraMajor4.
  ///
  /// In en, this message translates to:
  /// **'The Emperor'**
  String get cardExtraMajor4;

  /// No description provided for @cardExtraMajor5.
  ///
  /// In en, this message translates to:
  /// **'The Hierophant'**
  String get cardExtraMajor5;

  /// No description provided for @cardExtraMajor6.
  ///
  /// In en, this message translates to:
  /// **'The Lovers'**
  String get cardExtraMajor6;

  /// No description provided for @cardExtraMajor7.
  ///
  /// In en, this message translates to:
  /// **'The Chariot'**
  String get cardExtraMajor7;

  /// No description provided for @cardExtraMajor8.
  ///
  /// In en, this message translates to:
  /// **'Strength'**
  String get cardExtraMajor8;

  /// No description provided for @cardExtraMajor9.
  ///
  /// In en, this message translates to:
  /// **'The Hermit'**
  String get cardExtraMajor9;

  /// No description provided for @cardExtraMajor10.
  ///
  /// In en, this message translates to:
  /// **'Wheel of Fortune'**
  String get cardExtraMajor10;

  /// No description provided for @cardExtraMajor11.
  ///
  /// In en, this message translates to:
  /// **'Justice'**
  String get cardExtraMajor11;

  /// No description provided for @cardExtraMajor12.
  ///
  /// In en, this message translates to:
  /// **'The Hanged Man'**
  String get cardExtraMajor12;

  /// No description provided for @cardExtraMajor13.
  ///
  /// In en, this message translates to:
  /// **'Death'**
  String get cardExtraMajor13;

  /// No description provided for @cardExtraMajor14.
  ///
  /// In en, this message translates to:
  /// **'Temperance'**
  String get cardExtraMajor14;

  /// No description provided for @cardExtraMajor15.
  ///
  /// In en, this message translates to:
  /// **'The Devil'**
  String get cardExtraMajor15;

  /// No description provided for @cardExtraMajor16.
  ///
  /// In en, this message translates to:
  /// **'The Tower'**
  String get cardExtraMajor16;

  /// No description provided for @cardExtraMajor17.
  ///
  /// In en, this message translates to:
  /// **'The Star'**
  String get cardExtraMajor17;

  /// No description provided for @cardExtraMajor18.
  ///
  /// In en, this message translates to:
  /// **'The Moon'**
  String get cardExtraMajor18;

  /// No description provided for @cardExtraMajor19.
  ///
  /// In en, this message translates to:
  /// **'The Sun'**
  String get cardExtraMajor19;

  /// No description provided for @cardExtraMajor20.
  ///
  /// In en, this message translates to:
  /// **'Judgement'**
  String get cardExtraMajor20;

  /// No description provided for @cardExtraMajor21.
  ///
  /// In en, this message translates to:
  /// **'The World'**
  String get cardExtraMajor21;

  /// No description provided for @cardExtraWindEast.
  ///
  /// In en, this message translates to:
  /// **'east wind'**
  String get cardExtraWindEast;

  /// No description provided for @cardExtraWindSouth.
  ///
  /// In en, this message translates to:
  /// **'south wind'**
  String get cardExtraWindSouth;

  /// No description provided for @cardExtraWindWest.
  ///
  /// In en, this message translates to:
  /// **'west wind'**
  String get cardExtraWindWest;

  /// No description provided for @cardExtraWindNorth.
  ///
  /// In en, this message translates to:
  /// **'north wind'**
  String get cardExtraWindNorth;

  /// No description provided for @cardExtraDragonRed.
  ///
  /// In en, this message translates to:
  /// **'red dragon'**
  String get cardExtraDragonRed;

  /// No description provided for @cardExtraDragonGreen.
  ///
  /// In en, this message translates to:
  /// **'green dragon'**
  String get cardExtraDragonGreen;

  /// No description provided for @cardExtraDragonWhite.
  ///
  /// In en, this message translates to:
  /// **'white dragon'**
  String get cardExtraDragonWhite;

  /// No description provided for @cardRank1.
  ///
  /// In en, this message translates to:
  /// **'one'**
  String get cardRank1;

  /// No description provided for @cardRank2.
  ///
  /// In en, this message translates to:
  /// **'two'**
  String get cardRank2;

  /// No description provided for @cardRank3.
  ///
  /// In en, this message translates to:
  /// **'three'**
  String get cardRank3;

  /// No description provided for @cardRank4.
  ///
  /// In en, this message translates to:
  /// **'four'**
  String get cardRank4;

  /// No description provided for @cardRank5.
  ///
  /// In en, this message translates to:
  /// **'five'**
  String get cardRank5;

  /// No description provided for @cardRank6.
  ///
  /// In en, this message translates to:
  /// **'six'**
  String get cardRank6;

  /// No description provided for @cardRank7.
  ///
  /// In en, this message translates to:
  /// **'seven'**
  String get cardRank7;

  /// No description provided for @cardRank8.
  ///
  /// In en, this message translates to:
  /// **'eight'**
  String get cardRank8;

  /// No description provided for @cardRank9.
  ///
  /// In en, this message translates to:
  /// **'nine'**
  String get cardRank9;

  /// No description provided for @cardRank10.
  ///
  /// In en, this message translates to:
  /// **'ten'**
  String get cardRank10;

  /// No description provided for @cardRankAnimal.
  ///
  /// In en, this message translates to:
  /// **'animal'**
  String get cardRankAnimal;

  /// No description provided for @cardRankBright.
  ///
  /// In en, this message translates to:
  /// **'bright'**
  String get cardRankBright;

  /// No description provided for @cardRankChaff1.
  ///
  /// In en, this message translates to:
  /// **'chaff'**
  String get cardRankChaff1;

  /// No description provided for @cardRankChaff2.
  ///
  /// In en, this message translates to:
  /// **'chaff'**
  String get cardRankChaff2;

  /// No description provided for @cardRankChaff3.
  ///
  /// In en, this message translates to:
  /// **'chaff'**
  String get cardRankChaff3;

  /// No description provided for @cardRankRibbon.
  ///
  /// In en, this message translates to:
  /// **'ribbon'**
  String get cardRankRibbon;

  /// No description provided for @cardExtraJokerRed.
  ///
  /// In en, this message translates to:
  /// **'red joker'**
  String get cardExtraJokerRed;

  /// No description provided for @cardExtraJokerBlack.
  ///
  /// In en, this message translates to:
  /// **'black joker'**
  String get cardExtraJokerBlack;

  /// No description provided for @cardExtraJokerWhite.
  ///
  /// In en, this message translates to:
  /// **'white joker'**
  String get cardExtraJokerWhite;

  /// No description provided for @cardExtraFlowerPlum.
  ///
  /// In en, this message translates to:
  /// **'plum flower'**
  String get cardExtraFlowerPlum;

  /// No description provided for @cardExtraFlowerOrchid.
  ///
  /// In en, this message translates to:
  /// **'orchid flower'**
  String get cardExtraFlowerOrchid;

  /// No description provided for @cardExtraFlowerChrysanthemum.
  ///
  /// In en, this message translates to:
  /// **'chrysanthemum flower'**
  String get cardExtraFlowerChrysanthemum;

  /// No description provided for @cardExtraFlowerBamboo.
  ///
  /// In en, this message translates to:
  /// **'bamboo flower'**
  String get cardExtraFlowerBamboo;

  /// No description provided for @cardExtraSeasonSpring.
  ///
  /// In en, this message translates to:
  /// **'spring'**
  String get cardExtraSeasonSpring;

  /// No description provided for @cardExtraSeasonSummer.
  ///
  /// In en, this message translates to:
  /// **'summer'**
  String get cardExtraSeasonSummer;

  /// No description provided for @cardExtraSeasonAutumn.
  ///
  /// In en, this message translates to:
  /// **'autumn'**
  String get cardExtraSeasonAutumn;

  /// No description provided for @cardExtraSeasonWinter.
  ///
  /// In en, this message translates to:
  /// **'winter'**
  String get cardExtraSeasonWinter;

  /// No description provided for @tarotUpMajor0.
  ///
  /// In en, this message translates to:
  /// **'New beginnings, innocence, spontaneity, free spirit'**
  String get tarotUpMajor0;

  /// No description provided for @tarotRevMajor0.
  ///
  /// In en, this message translates to:
  /// **'Recklessness, risk-taking, foolishness, naivety'**
  String get tarotRevMajor0;

  /// No description provided for @tarotUpMajor1.
  ///
  /// In en, this message translates to:
  /// **'Manifestation, resourcefulness, power, inspired action'**
  String get tarotUpMajor1;

  /// No description provided for @tarotRevMajor1.
  ///
  /// In en, this message translates to:
  /// **'Manipulation, poor planning, untapped talents'**
  String get tarotRevMajor1;

  /// No description provided for @tarotUpMajor2.
  ///
  /// In en, this message translates to:
  /// **'Intuition, sacred knowledge, divine feminine, subconscious mind'**
  String get tarotUpMajor2;

  /// No description provided for @tarotRevMajor2.
  ///
  /// In en, this message translates to:
  /// **'Secrets, disconnected from intuition, withdrawal and silence'**
  String get tarotRevMajor2;

  /// No description provided for @tarotUpMajor3.
  ///
  /// In en, this message translates to:
  /// **'Femininity, beauty, nature, nurturing, abundance'**
  String get tarotUpMajor3;

  /// No description provided for @tarotRevMajor3.
  ///
  /// In en, this message translates to:
  /// **'Creative block, dependence on others, emptiness'**
  String get tarotRevMajor3;

  /// No description provided for @tarotUpMajor4.
  ///
  /// In en, this message translates to:
  /// **'Authority, structure, control, fatherhood, stability'**
  String get tarotUpMajor4;

  /// No description provided for @tarotRevMajor4.
  ///
  /// In en, this message translates to:
  /// **'Domination, excessive control, rigidity, inflexibility'**
  String get tarotRevMajor4;

  /// No description provided for @tarotUpMajor5.
  ///
  /// In en, this message translates to:
  /// **'Spiritual wisdom, tradition, conformity, morality, ethics'**
  String get tarotUpMajor5;

  /// No description provided for @tarotRevMajor5.
  ///
  /// In en, this message translates to:
  /// **'Rebellion, subversiveness, new approaches, unconventionality'**
  String get tarotRevMajor5;

  /// No description provided for @tarotUpMajor6.
  ///
  /// In en, this message translates to:
  /// **'Love, harmony, relationships, values alignment, choices'**
  String get tarotUpMajor6;

  /// No description provided for @tarotRevMajor6.
  ///
  /// In en, this message translates to:
  /// **'Self-love, disharmony, imbalance, misalignment of values'**
  String get tarotRevMajor6;

  /// No description provided for @tarotUpMajor7.
  ///
  /// In en, this message translates to:
  /// **'Control, willpower, success, action, determination'**
  String get tarotUpMajor7;

  /// No description provided for @tarotRevMajor7.
  ///
  /// In en, this message translates to:
  /// **'Self-discipline, opposition, lack of direction'**
  String get tarotRevMajor7;

  /// No description provided for @tarotUpMajor8.
  ///
  /// In en, this message translates to:
  /// **'Courage, persuasion, influence, compassion, inner strength'**
  String get tarotUpMajor8;

  /// No description provided for @tarotRevMajor8.
  ///
  /// In en, this message translates to:
  /// **'Self-doubt, weakness, insecurity, lack of self-discipline'**
  String get tarotRevMajor8;

  /// No description provided for @tarotUpMajor9.
  ///
  /// In en, this message translates to:
  /// **'Soul-searching, introspection, solitude, inner guidance'**
  String get tarotUpMajor9;

  /// No description provided for @tarotRevMajor9.
  ///
  /// In en, this message translates to:
  /// **'Isolation, loneliness, withdrawal, rejection'**
  String get tarotRevMajor9;

  /// No description provided for @tarotUpMajor10.
  ///
  /// In en, this message translates to:
  /// **'Good luck, karma, life cycles, destiny, turning point'**
  String get tarotUpMajor10;

  /// No description provided for @tarotRevMajor10.
  ///
  /// In en, this message translates to:
  /// **'Bad luck, resistance to change, breaking cycles'**
  String get tarotRevMajor10;

  /// No description provided for @tarotUpMajor11.
  ///
  /// In en, this message translates to:
  /// **'Justice, fairness, truth, cause and effect, law'**
  String get tarotUpMajor11;

  /// No description provided for @tarotRevMajor11.
  ///
  /// In en, this message translates to:
  /// **'Unfairness, lack of accountability, dishonesty'**
  String get tarotRevMajor11;

  /// No description provided for @tarotUpMajor12.
  ///
  /// In en, this message translates to:
  /// **'Surrender, letting go, new perspectives, enlightenment'**
  String get tarotUpMajor12;

  /// No description provided for @tarotRevMajor12.
  ///
  /// In en, this message translates to:
  /// **'Indecision, delay, resistance, stalling'**
  String get tarotRevMajor12;

  /// No description provided for @tarotUpMajor13.
  ///
  /// In en, this message translates to:
  /// **'End of cycle, beginnings, change, metamorphosis, transition'**
  String get tarotUpMajor13;

  /// No description provided for @tarotRevMajor13.
  ///
  /// In en, this message translates to:
  /// **'Resistance to change, inability to move on, stagnation'**
  String get tarotRevMajor13;

  /// No description provided for @tarotUpMajor14.
  ///
  /// In en, this message translates to:
  /// **'Balance, moderation, patience, purpose, meaning'**
  String get tarotUpMajor14;

  /// No description provided for @tarotRevMajor14.
  ///
  /// In en, this message translates to:
  /// **'Imbalance, excess, self-healing, realignment'**
  String get tarotRevMajor14;

  /// No description provided for @tarotUpMajor15.
  ///
  /// In en, this message translates to:
  /// **'Shadow self, attachment, addiction, restriction, sexuality'**
  String get tarotUpMajor15;

  /// No description provided for @tarotRevMajor15.
  ///
  /// In en, this message translates to:
  /// **'Releasing limiting beliefs, exploring dark thoughts, detachment'**
  String get tarotRevMajor15;

  /// No description provided for @tarotUpMajor16.
  ///
  /// In en, this message translates to:
  /// **'Sudden change, upheaval, chaos, revelation, awakening'**
  String get tarotUpMajor16;

  /// No description provided for @tarotRevMajor16.
  ///
  /// In en, this message translates to:
  /// **'Fear of change, avoiding disaster, delaying the inevitable'**
  String get tarotRevMajor16;

  /// No description provided for @tarotUpMajor17.
  ///
  /// In en, this message translates to:
  /// **'Hope, faith, purpose, renewal, spirituality'**
  String get tarotUpMajor17;

  /// No description provided for @tarotRevMajor17.
  ///
  /// In en, this message translates to:
  /// **'Lack of faith, despair, self-doubt, disconnection'**
  String get tarotRevMajor17;

  /// No description provided for @tarotUpMajor18.
  ///
  /// In en, this message translates to:
  /// **'Illusion, fear, anxiety, subconscious, intuition'**
  String get tarotUpMajor18;

  /// No description provided for @tarotRevMajor18.
  ///
  /// In en, this message translates to:
  /// **'Release of fear, repressed emotion, inner confusion'**
  String get tarotRevMajor18;

  /// No description provided for @tarotUpMajor19.
  ///
  /// In en, this message translates to:
  /// **'Positivity, fun, warmth, success, vitality'**
  String get tarotUpMajor19;

  /// No description provided for @tarotRevMajor19.
  ///
  /// In en, this message translates to:
  /// **'Inner child, feeling down, overly optimistic'**
  String get tarotRevMajor19;

  /// No description provided for @tarotUpMajor20.
  ///
  /// In en, this message translates to:
  /// **'Judgement, rebirth, inner calling, absolution'**
  String get tarotUpMajor20;

  /// No description provided for @tarotRevMajor20.
  ///
  /// In en, this message translates to:
  /// **'Self-doubt, inner critic, ignoring the call'**
  String get tarotRevMajor20;

  /// No description provided for @tarotUpMajor21.
  ///
  /// In en, this message translates to:
  /// **'Completion, integration, accomplishment, travel'**
  String get tarotUpMajor21;

  /// No description provided for @tarotRevMajor21.
  ///
  /// In en, this message translates to:
  /// **'Seeking personal closure, short-cuts, delays'**
  String get tarotRevMajor21;

  /// No description provided for @tarotUpCupsAce.
  ///
  /// In en, this message translates to:
  /// **'New feelings, intuition, intimacy, love, emotional fulfilment'**
  String get tarotUpCupsAce;

  /// No description provided for @tarotRevCupsAce.
  ///
  /// In en, this message translates to:
  /// **'Emotional loss, blocked creativity, emptiness'**
  String get tarotRevCupsAce;

  /// No description provided for @tarotUpCups2.
  ///
  /// In en, this message translates to:
  /// **'Unity, partnership, mutual attraction, connection'**
  String get tarotUpCups2;

  /// No description provided for @tarotRevCups2.
  ///
  /// In en, this message translates to:
  /// **'Imbalanced relationship, tension, disconnection'**
  String get tarotRevCups2;

  /// No description provided for @tarotUpCups3.
  ///
  /// In en, this message translates to:
  /// **'Celebration, friendship, creativity, community'**
  String get tarotUpCups3;

  /// No description provided for @tarotRevCups3.
  ///
  /// In en, this message translates to:
  /// **'Overindulgence, gossip, isolation'**
  String get tarotRevCups3;

  /// No description provided for @tarotUpCups4.
  ///
  /// In en, this message translates to:
  /// **'Apathy, contemplation, disillusionment, meditation'**
  String get tarotUpCups4;

  /// No description provided for @tarotRevCups4.
  ///
  /// In en, this message translates to:
  /// **'Clarity, renewal, moving forward, acceptance'**
  String get tarotRevCups4;

  /// No description provided for @tarotUpCups5.
  ///
  /// In en, this message translates to:
  /// **'Loss, grief, disappointment, self-pity, failure'**
  String get tarotUpCups5;

  /// No description provided for @tarotRevCups5.
  ///
  /// In en, this message translates to:
  /// **'Acceptance, moving on, finding peace'**
  String get tarotRevCups5;

  /// No description provided for @tarotUpCups6.
  ///
  /// In en, this message translates to:
  /// **'Nostalgia, memories, reunions, childhood, innocence'**
  String get tarotUpCups6;

  /// No description provided for @tarotRevCups6.
  ///
  /// In en, this message translates to:
  /// **'Living in the past, unrealistic expectations'**
  String get tarotRevCups6;

  /// No description provided for @tarotUpCups7.
  ///
  /// In en, this message translates to:
  /// **'Daydreaming, fantasy, illusion, wishful thinking, choices'**
  String get tarotUpCups7;

  /// No description provided for @tarotRevCups7.
  ///
  /// In en, this message translates to:
  /// **'Clarity, making choices, disillusionment'**
  String get tarotRevCups7;

  /// No description provided for @tarotUpCups8.
  ///
  /// In en, this message translates to:
  /// **'Walking away, disillusionment, leaving behind, searching for truth'**
  String get tarotUpCups8;

  /// No description provided for @tarotRevCups8.
  ///
  /// In en, this message translates to:
  /// **'Avoidance, fear of change, staying in a bad situation'**
  String get tarotRevCups8;

  /// No description provided for @tarotUpCups9.
  ///
  /// In en, this message translates to:
  /// **'Contentment, satisfaction, gratitude, wish come true'**
  String get tarotUpCups9;

  /// No description provided for @tarotRevCups9.
  ///
  /// In en, this message translates to:
  /// **'Unmet wishes, materialism, dissatisfaction'**
  String get tarotRevCups9;

  /// No description provided for @tarotUpCups10.
  ///
  /// In en, this message translates to:
  /// **'Divine love, harmony, alignment, perfect union, happiness'**
  String get tarotUpCups10;

  /// No description provided for @tarotRevCups10.
  ///
  /// In en, this message translates to:
  /// **'Broken relationships, disharmony, misalignment'**
  String get tarotRevCups10;

  /// No description provided for @tarotUpCupsPage.
  ///
  /// In en, this message translates to:
  /// **'Creative opportunities, intuitive messages, curiosity, possibility'**
  String get tarotUpCupsPage;

  /// No description provided for @tarotRevCupsPage.
  ///
  /// In en, this message translates to:
  /// **'Creative blocks, emotional immaturity, insecurity'**
  String get tarotRevCupsPage;

  /// No description provided for @tarotUpCupsKnight.
  ///
  /// In en, this message translates to:
  /// **'Creativity, romance, charm, imagination, beauty'**
  String get tarotUpCupsKnight;

  /// No description provided for @tarotRevCupsKnight.
  ///
  /// In en, this message translates to:
  /// **'Moodiness, disappointment, unrealistic expectations'**
  String get tarotRevCupsKnight;

  /// No description provided for @tarotUpCupsQueen.
  ///
  /// In en, this message translates to:
  /// **'Compassion, calm, comfort, emotional security, intuition'**
  String get tarotUpCupsQueen;

  /// No description provided for @tarotRevCupsQueen.
  ///
  /// In en, this message translates to:
  /// **'Martyrdom, insecurity, dependence, smothering'**
  String get tarotRevCupsQueen;

  /// No description provided for @tarotUpCupsKing.
  ///
  /// In en, this message translates to:
  /// **'Emotional balance, compassion, diplomacy, counselling'**
  String get tarotUpCupsKing;

  /// No description provided for @tarotRevCupsKing.
  ///
  /// In en, this message translates to:
  /// **'Coldness, manipulation, moodiness, emotional instability'**
  String get tarotRevCupsKing;

  /// No description provided for @tarotUpPentaclesAce.
  ///
  /// In en, this message translates to:
  /// **'New financial opportunity, manifestation, abundance, prosperity'**
  String get tarotUpPentaclesAce;

  /// No description provided for @tarotRevPentaclesAce.
  ///
  /// In en, this message translates to:
  /// **'Lost opportunity, scarcity mindset, financial issues'**
  String get tarotRevPentaclesAce;

  /// No description provided for @tarotUpPentacles2.
  ///
  /// In en, this message translates to:
  /// **'Balancing decisions, priorities, adaptability, time management'**
  String get tarotUpPentacles2;

  /// No description provided for @tarotRevPentacles2.
  ///
  /// In en, this message translates to:
  /// **'Imbalance, disorganisation, overwhelm'**
  String get tarotRevPentacles2;

  /// No description provided for @tarotUpPentacles3.
  ///
  /// In en, this message translates to:
  /// **'Teamwork, collaboration, learning, implementation'**
  String get tarotUpPentacles3;

  /// No description provided for @tarotRevPentacles3.
  ///
  /// In en, this message translates to:
  /// **'Lack of teamwork, disorganisation, group conflict'**
  String get tarotRevPentacles3;

  /// No description provided for @tarotUpPentacles4.
  ///
  /// In en, this message translates to:
  /// **'Conservation, security, frugality, control'**
  String get tarotUpPentacles4;

  /// No description provided for @tarotRevPentacles4.
  ///
  /// In en, this message translates to:
  /// **'Greed, materialism, stinginess, possessiveness'**
  String get tarotRevPentacles4;

  /// No description provided for @tarotUpPentacles5.
  ///
  /// In en, this message translates to:
  /// **'Financial loss, poverty, isolation, worry, insecurity'**
  String get tarotUpPentacles5;

  /// No description provided for @tarotRevPentacles5.
  ///
  /// In en, this message translates to:
  /// **'Recovery, charity, improvement, generosity'**
  String get tarotRevPentacles5;

  /// No description provided for @tarotUpPentacles6.
  ///
  /// In en, this message translates to:
  /// **'Charity, generosity, giving, mentoring, gratitude'**
  String get tarotUpPentacles6;

  /// No description provided for @tarotRevPentacles6.
  ///
  /// In en, this message translates to:
  /// **'Strings attached, stinginess, power and domination'**
  String get tarotRevPentacles6;

  /// No description provided for @tarotUpPentacles7.
  ///
  /// In en, this message translates to:
  /// **'Vision, perseverance, profit, reward, investment'**
  String get tarotUpPentacles7;

  /// No description provided for @tarotRevPentacles7.
  ///
  /// In en, this message translates to:
  /// **'Lack of vision, limited success, distractions'**
  String get tarotRevPentacles7;

  /// No description provided for @tarotUpPentacles8.
  ///
  /// In en, this message translates to:
  /// **'Apprenticeship, skill development, mastery, high standards'**
  String get tarotUpPentacles8;

  /// No description provided for @tarotRevPentacles8.
  ///
  /// In en, this message translates to:
  /// **'Perfectionism, lack of ambition, low standards'**
  String get tarotRevPentacles8;

  /// No description provided for @tarotUpPentacles9.
  ///
  /// In en, this message translates to:
  /// **'Luxury, self-sufficiency, financial independence, abundance'**
  String get tarotUpPentacles9;

  /// No description provided for @tarotRevPentacles9.
  ///
  /// In en, this message translates to:
  /// **'Over-investment in work, hustling, hollow success'**
  String get tarotRevPentacles9;

  /// No description provided for @tarotUpPentacles10.
  ///
  /// In en, this message translates to:
  /// **'Legacy, culmination, inheritance, family, establishment'**
  String get tarotUpPentacles10;

  /// No description provided for @tarotRevPentacles10.
  ///
  /// In en, this message translates to:
  /// **'Family disputes, bankruptcy, debt, fleeting success'**
  String get tarotRevPentacles10;

  /// No description provided for @tarotUpPentaclesPage.
  ///
  /// In en, this message translates to:
  /// **'Manifestation, financial opportunity, skill development'**
  String get tarotUpPentaclesPage;

  /// No description provided for @tarotRevPentaclesPage.
  ///
  /// In en, this message translates to:
  /// **'Lack of progress, procrastination, learning from failure'**
  String get tarotRevPentaclesPage;

  /// No description provided for @tarotUpPentaclesKnight.
  ///
  /// In en, this message translates to:
  /// **'Efficiency, hard work, responsibility, conservatism'**
  String get tarotUpPentaclesKnight;

  /// No description provided for @tarotRevPentaclesKnight.
  ///
  /// In en, this message translates to:
  /// **'Boredom, feeling stuck, perfectionism, overwork'**
  String get tarotRevPentaclesKnight;

  /// No description provided for @tarotUpPentaclesQueen.
  ///
  /// In en, this message translates to:
  /// **'Nurturing, practical, providing financially, security'**
  String get tarotUpPentaclesQueen;

  /// No description provided for @tarotRevPentaclesQueen.
  ///
  /// In en, this message translates to:
  /// **'Financial independence, self-care, work-home conflict'**
  String get tarotRevPentaclesQueen;

  /// No description provided for @tarotUpPentaclesKing.
  ///
  /// In en, this message translates to:
  /// **'Abundance, prosperity, security, discipline, leadership'**
  String get tarotUpPentaclesKing;

  /// No description provided for @tarotRevPentaclesKing.
  ///
  /// In en, this message translates to:
  /// **'Poor with money, obsessed with wealth, stubbornness'**
  String get tarotRevPentaclesKing;

  /// No description provided for @tarotUpWandsAce.
  ///
  /// In en, this message translates to:
  /// **'Inspiration, creative spark, new opportunities, potential'**
  String get tarotUpWandsAce;

  /// No description provided for @tarotRevWandsAce.
  ///
  /// In en, this message translates to:
  /// **'Delays, blocks, lack of direction, distractions'**
  String get tarotRevWandsAce;

  /// No description provided for @tarotUpWands2.
  ///
  /// In en, this message translates to:
  /// **'Future planning, progress, decisions, discovery'**
  String get tarotUpWands2;

  /// No description provided for @tarotRevWands2.
  ///
  /// In en, this message translates to:
  /// **'Fear of change, playing it safe, bad planning'**
  String get tarotRevWands2;

  /// No description provided for @tarotUpWands3.
  ///
  /// In en, this message translates to:
  /// **'Expansion, foresight, overseas opportunities, leadership'**
  String get tarotUpWands3;

  /// No description provided for @tarotRevWands3.
  ///
  /// In en, this message translates to:
  /// **'Lack of foresight, obstacles, delays, frustration'**
  String get tarotRevWands3;

  /// No description provided for @tarotUpWands4.
  ///
  /// In en, this message translates to:
  /// **'Celebration, harmony, marriage, home, community'**
  String get tarotUpWands4;

  /// No description provided for @tarotRevWands4.
  ///
  /// In en, this message translates to:
  /// **'Breakdown in communication, transition, misalignment'**
  String get tarotRevWands4;

  /// No description provided for @tarotUpWands5.
  ///
  /// In en, this message translates to:
  /// **'Conflict, disagreements, competition, tension, diversity'**
  String get tarotUpWands5;

  /// No description provided for @tarotRevWands5.
  ///
  /// In en, this message translates to:
  /// **'End of conflict, resolution, compromise, release'**
  String get tarotRevWands5;

  /// No description provided for @tarotUpWands6.
  ///
  /// In en, this message translates to:
  /// **'Victory, success, public recognition, progress, self-confidence'**
  String get tarotUpWands6;

  /// No description provided for @tarotRevWands6.
  ///
  /// In en, this message translates to:
  /// **'Private achievement, personal definition of success, fall from grace'**
  String get tarotRevWands6;

  /// No description provided for @tarotUpWands7.
  ///
  /// In en, this message translates to:
  /// **'Challenge, competition, protection, perseverance'**
  String get tarotUpWands7;

  /// No description provided for @tarotRevWands7.
  ///
  /// In en, this message translates to:
  /// **'Exhaustion, giving up, overwhelmed'**
  String get tarotRevWands7;

  /// No description provided for @tarotUpWands8.
  ///
  /// In en, this message translates to:
  /// **'Speed, action, air travel, movement, quick decisions'**
  String get tarotUpWands8;

  /// No description provided for @tarotRevWands8.
  ///
  /// In en, this message translates to:
  /// **'Panic, waiting, slowdown, exhaustion'**
  String get tarotRevWands8;

  /// No description provided for @tarotUpWands9.
  ///
  /// In en, this message translates to:
  /// **'Resilience, courage, persistence, test of faith, boundaries'**
  String get tarotUpWands9;

  /// No description provided for @tarotRevWands9.
  ///
  /// In en, this message translates to:
  /// **'Exhaustion, fatigue, questioning motivations'**
  String get tarotRevWands9;

  /// No description provided for @tarotUpWands10.
  ///
  /// In en, this message translates to:
  /// **'Burden, responsibility, hard work, completion, success'**
  String get tarotUpWands10;

  /// No description provided for @tarotRevWands10.
  ///
  /// In en, this message translates to:
  /// **'Inability to delegate, micromanagement, burnout'**
  String get tarotRevWands10;

  /// No description provided for @tarotUpWandsPage.
  ///
  /// In en, this message translates to:
  /// **'Exploration, excitement, freedom, adventure, potential'**
  String get tarotUpWandsPage;

  /// No description provided for @tarotRevWandsPage.
  ///
  /// In en, this message translates to:
  /// **'Lack of direction, procrastination, creating conflict'**
  String get tarotRevWandsPage;

  /// No description provided for @tarotUpWandsKnight.
  ///
  /// In en, this message translates to:
  /// **'Action, adventure, fearlessness, impulsiveness, travel'**
  String get tarotUpWandsKnight;

  /// No description provided for @tarotRevWandsKnight.
  ///
  /// In en, this message translates to:
  /// **'Anger, impulsivity, recklessness, impatience'**
  String get tarotRevWandsKnight;

  /// No description provided for @tarotUpWandsQueen.
  ///
  /// In en, this message translates to:
  /// **'Courage, determination, joy, vibrancy, warmth'**
  String get tarotUpWandsQueen;

  /// No description provided for @tarotRevWandsQueen.
  ///
  /// In en, this message translates to:
  /// **'Selfishness, jealousy, demands, vengefulness'**
  String get tarotRevWandsQueen;

  /// No description provided for @tarotUpWandsKing.
  ///
  /// In en, this message translates to:
  /// **'Leadership, vision, big picture, taking control, overcoming challenges'**
  String get tarotUpWandsKing;

  /// No description provided for @tarotRevWandsKing.
  ///
  /// In en, this message translates to:
  /// **'Impulsiveness, overbearing manner, unachievable expectations'**
  String get tarotRevWandsKing;

  /// No description provided for @tarotUpSwordsAce.
  ///
  /// In en, this message translates to:
  /// **'Breakthrough, clarity, sharp mind, truth, mental strength'**
  String get tarotUpSwordsAce;

  /// No description provided for @tarotRevSwordsAce.
  ///
  /// In en, this message translates to:
  /// **'Confusion, brutality, chaos, cloudiness'**
  String get tarotRevSwordsAce;

  /// No description provided for @tarotUpSwords2.
  ///
  /// In en, this message translates to:
  /// **'Difficult choices, indecision, stalemate, denial, truce'**
  String get tarotUpSwords2;

  /// No description provided for @tarotRevSwords2.
  ///
  /// In en, this message translates to:
  /// **'Lesser of two evils, no right choice, confusion'**
  String get tarotRevSwords2;

  /// No description provided for @tarotUpSwords3.
  ///
  /// In en, this message translates to:
  /// **'Heartbreak, emotional pain, sorrow, grief, hurt'**
  String get tarotUpSwords3;

  /// No description provided for @tarotRevSwords3.
  ///
  /// In en, this message translates to:
  /// **'Healing, forgiveness, recovery, reconciliation'**
  String get tarotRevSwords3;

  /// No description provided for @tarotUpSwords4.
  ///
  /// In en, this message translates to:
  /// **'Rest, restoration, contemplation, recuperation, peace'**
  String get tarotUpSwords4;

  /// No description provided for @tarotRevSwords4.
  ///
  /// In en, this message translates to:
  /// **'Restlessness, burnout, stress, no relaxation'**
  String get tarotRevSwords4;

  /// No description provided for @tarotUpSwords5.
  ///
  /// In en, this message translates to:
  /// **'Conflict, disagreements, competition, defeat, winning at all costs'**
  String get tarotUpSwords5;

  /// No description provided for @tarotRevSwords5.
  ///
  /// In en, this message translates to:
  /// **'Reconciliation, resolution, compromise'**
  String get tarotRevSwords5;

  /// No description provided for @tarotUpSwords6.
  ///
  /// In en, this message translates to:
  /// **'Transition, change, rite of passage, releasing baggage'**
  String get tarotUpSwords6;

  /// No description provided for @tarotRevSwords6.
  ///
  /// In en, this message translates to:
  /// **'Stuck in transition, unfinished business, no closure'**
  String get tarotRevSwords6;

  /// No description provided for @tarotUpSwords7.
  ///
  /// In en, this message translates to:
  /// **'Deception, trickery, tactics, strategy, sneakiness'**
  String get tarotUpSwords7;

  /// No description provided for @tarotRevSwords7.
  ///
  /// In en, this message translates to:
  /// **'Confession, clarity, conscience, honesty'**
  String get tarotRevSwords7;

  /// No description provided for @tarotUpSwords8.
  ///
  /// In en, this message translates to:
  /// **'Imprisonment, restriction, isolation, self-victimization'**
  String get tarotUpSwords8;

  /// No description provided for @tarotRevSwords8.
  ///
  /// In en, this message translates to:
  /// **'Freedom, release, self-acceptance, moving on'**
  String get tarotRevSwords8;

  /// No description provided for @tarotUpSwords9.
  ///
  /// In en, this message translates to:
  /// **'Anxiety, worry, fear, depression, nightmares'**
  String get tarotUpSwords9;

  /// No description provided for @tarotRevSwords9.
  ///
  /// In en, this message translates to:
  /// **'Recovery, learning from mistakes, facing fears'**
  String get tarotRevSwords9;

  /// No description provided for @tarotUpSwords10.
  ///
  /// In en, this message translates to:
  /// **'Painful endings, deep wounds, betrayal, loss, crisis'**
  String get tarotUpSwords10;

  /// No description provided for @tarotRevSwords10.
  ///
  /// In en, this message translates to:
  /// **'Recovery, regeneration, rebirth, inevitable end'**
  String get tarotRevSwords10;

  /// No description provided for @tarotUpSwordsPage.
  ///
  /// In en, this message translates to:
  /// **'New ideas, curiosity, thirst for knowledge, new ways of communicating'**
  String get tarotUpSwordsPage;

  /// No description provided for @tarotRevSwordsPage.
  ///
  /// In en, this message translates to:
  /// **'Poor self-expression, all talk and no action, hurtful words'**
  String get tarotRevSwordsPage;

  /// No description provided for @tarotUpSwordsKnight.
  ///
  /// In en, this message translates to:
  /// **'Action, impulsiveness, defending beliefs, swift change'**
  String get tarotUpSwordsKnight;

  /// No description provided for @tarotRevSwordsKnight.
  ///
  /// In en, this message translates to:
  /// **'No direction, disregard for consequences, unpredictability'**
  String get tarotRevSwordsKnight;

  /// No description provided for @tarotUpSwordsQueen.
  ///
  /// In en, this message translates to:
  /// **'Independence, unbiased judgement, clear boundaries, direct communication'**
  String get tarotUpSwordsQueen;

  /// No description provided for @tarotRevSwordsQueen.
  ///
  /// In en, this message translates to:
  /// **'Harsh judgement, overly critical, cutting words'**
  String get tarotRevSwordsQueen;

  /// No description provided for @tarotUpSwordsKing.
  ///
  /// In en, this message translates to:
  /// **'Mental clarity, intellectual power, authority, truth'**
  String get tarotUpSwordsKing;

  /// No description provided for @tarotRevSwordsKing.
  ///
  /// In en, this message translates to:
  /// **'Manipulation, tyranny, cold-heartedness'**
  String get tarotRevSwordsKing;

  /// No description provided for @tarotPosPast.
  ///
  /// In en, this message translates to:
  /// **'Past'**
  String get tarotPosPast;

  /// No description provided for @tarotPosPresent.
  ///
  /// In en, this message translates to:
  /// **'Present'**
  String get tarotPosPresent;

  /// No description provided for @tarotPosFuture.
  ///
  /// In en, this message translates to:
  /// **'Future'**
  String get tarotPosFuture;

  /// No description provided for @tarotPosChallenge.
  ///
  /// In en, this message translates to:
  /// **'Challenge'**
  String get tarotPosChallenge;

  /// No description provided for @tarotPosAbove.
  ///
  /// In en, this message translates to:
  /// **'Above'**
  String get tarotPosAbove;

  /// No description provided for @tarotPosBelow.
  ///
  /// In en, this message translates to:
  /// **'Below'**
  String get tarotPosBelow;

  /// No description provided for @tarotPosAdvice.
  ///
  /// In en, this message translates to:
  /// **'Advice'**
  String get tarotPosAdvice;

  /// No description provided for @tarotPosExternal.
  ///
  /// In en, this message translates to:
  /// **'External influences'**
  String get tarotPosExternal;

  /// No description provided for @tarotPosHopes.
  ///
  /// In en, this message translates to:
  /// **'Hopes and fears'**
  String get tarotPosHopes;

  /// No description provided for @tarotPosOutcome.
  ///
  /// In en, this message translates to:
  /// **'Outcome'**
  String get tarotPosOutcome;

  /// No description provided for @tarotPosHidden.
  ///
  /// In en, this message translates to:
  /// **'Hidden influences'**
  String get tarotPosHidden;

  /// No description provided for @tarotPosObstacles.
  ///
  /// In en, this message translates to:
  /// **'Obstacles'**
  String get tarotPosObstacles;

  /// No description provided for @tarotPosYou.
  ///
  /// In en, this message translates to:
  /// **'You'**
  String get tarotPosYou;

  /// No description provided for @tarotPosPartner.
  ///
  /// In en, this message translates to:
  /// **'Partner'**
  String get tarotPosPartner;

  /// No description provided for @tarotPosFoundation.
  ///
  /// In en, this message translates to:
  /// **'Foundation of the relationship'**
  String get tarotPosFoundation;

  /// No description provided for @tarotPosRecent.
  ///
  /// In en, this message translates to:
  /// **'Recent past'**
  String get tarotPosRecent;

  /// No description provided for @tarotPosDynamics.
  ///
  /// In en, this message translates to:
  /// **'Current dynamics'**
  String get tarotPosDynamics;

  /// No description provided for @tarotPosNear.
  ///
  /// In en, this message translates to:
  /// **'Near future'**
  String get tarotPosNear;

  /// No description provided for @tarotSpreadThree.
  ///
  /// In en, this message translates to:
  /// **'Three cards'**
  String get tarotSpreadThree;

  /// No description provided for @tarotSpreadCeltic.
  ///
  /// In en, this message translates to:
  /// **'Celtic cross'**
  String get tarotSpreadCeltic;

  /// No description provided for @tarotSpreadHorseshoe.
  ///
  /// In en, this message translates to:
  /// **'Horseshoe'**
  String get tarotSpreadHorseshoe;

  /// No description provided for @tarotSpreadRelationship.
  ///
  /// In en, this message translates to:
  /// **'Relationship'**
  String get tarotSpreadRelationship;

  /// No description provided for @tarotSumMajorHigh.
  ///
  /// In en, this message translates to:
  /// **'Many major arcana: big forces and turning points are at work.'**
  String get tarotSumMajorHigh;

  /// No description provided for @tarotSumMajorLow.
  ///
  /// In en, this message translates to:
  /// **'Mostly minor arcana: this is about everyday matters, not upheaval.'**
  String get tarotSumMajorLow;

  /// No description provided for @tarotSumMajorBalanced.
  ///
  /// In en, this message translates to:
  /// **'A mix of major and minor arcana: the everyday ties into the larger path.'**
  String get tarotSumMajorBalanced;

  /// No description provided for @tarotSumReversedMany.
  ///
  /// In en, this message translates to:
  /// **'Many reversed cards: obstacles, delays or inner doubts to deal with first.'**
  String get tarotSumReversedMany;

  /// No description provided for @tarotSumReversedFew.
  ///
  /// In en, this message translates to:
  /// **'Nearly all upright: the way looks clear.'**
  String get tarotSumReversedFew;

  /// No description provided for @tarotSumSuitCups.
  ///
  /// In en, this message translates to:
  /// **'Cups stand out: feelings, relationships and intuition.'**
  String get tarotSumSuitCups;

  /// No description provided for @tarotSumSuitPentacles.
  ///
  /// In en, this message translates to:
  /// **'Pentacles stand out: money, work and practical things.'**
  String get tarotSumSuitPentacles;

  /// No description provided for @tarotSumSuitWands.
  ///
  /// In en, this message translates to:
  /// **'Wands stand out: drive, creativity and action.'**
  String get tarotSumSuitWands;

  /// No description provided for @tarotSumSuitSwords.
  ///
  /// In en, this message translates to:
  /// **'Swords stand out: thought, decisions and conflict.'**
  String get tarotSumSuitSwords;

  /// No description provided for @tarotForFun.
  ///
  /// In en, this message translates to:
  /// **'For fun: the cards were drawn by the server at random.'**
  String get tarotForFun;

  /// No description provided for @cmdNeedsText.
  ///
  /// In en, this message translates to:
  /// **'/{name} needs some text after it.'**
  String cmdNeedsText(String name);

  /// No description provided for @cmdTakesNothing.
  ///
  /// In en, this message translates to:
  /// **'/{name} takes nothing after it.'**
  String cmdTakesNothing(String name);

  /// No description provided for @cmdNeedsOptions.
  ///
  /// In en, this message translates to:
  /// **'/{name} needs at least two options, separated by |.'**
  String cmdNeedsOptions(String name);

  /// No description provided for @cmdTooManyOptions.
  ///
  /// In en, this message translates to:
  /// **'/{name} takes at most 20 options.'**
  String cmdTooManyOptions(String name);

  /// No description provided for @cmdEmptyOption.
  ///
  /// In en, this message translates to:
  /// **'An option of /{name} is empty.'**
  String cmdEmptyOption(String name);

  /// No description provided for @cmdBadDraw.
  ///
  /// In en, this message translates to:
  /// **'Write /draw [count] [deck], for example /draw 3 tarot.'**
  String get cmdBadDraw;

  /// No description provided for @cmdUnknownDeck.
  ///
  /// In en, this message translates to:
  /// **'There is no such deck. The decks are: {decks}.'**
  String cmdUnknownDeck(String decks);

  /// No description provided for @cmdTooManyCards.
  ///
  /// In en, this message translates to:
  /// **'That deck does not have that many cards.'**
  String get cmdTooManyCards;

  /// No description provided for @cmdUnknownSpread.
  ///
  /// In en, this message translates to:
  /// **'There is no such spread. The spreads are: {spreads}.'**
  String cmdUnknownSpread(String spreads);

  /// No description provided for @cmdBadCount.
  ///
  /// In en, this message translates to:
  /// **'Draw 1 to {max} tiles.'**
  String cmdBadCount(int max);

  /// No description provided for @cmdNeedsChannel.
  ///
  /// In en, this message translates to:
  /// **'/{name} needs a channel: type # and pick one.'**
  String cmdNeedsChannel(String name);

  /// No description provided for @cmdNeedsPerson.
  ///
  /// In en, this message translates to:
  /// **'/{name} needs a person: type @ and pick one.'**
  String cmdNeedsPerson(String name);

  /// No description provided for @cmdBadPerson.
  ///
  /// In en, this message translates to:
  /// **'/{name} takes one person, written @name.'**
  String cmdBadPerson(String name);

  /// No description provided for @cmdBadDuration.
  ///
  /// In en, this message translates to:
  /// **'Write a time like 30m, 2h, 1h30m, 1d or 1w (at most a year).'**
  String get cmdBadDuration;

  /// No description provided for @cmdNotInSealedChance.
  ///
  /// In en, this message translates to:
  /// **'/{name} is drawn by the server, which cannot write into a sealed chat.'**
  String cmdNotInSealedChance(String name);

  /// No description provided for @cmdNotInSealed.
  ///
  /// In en, this message translates to:
  /// **'/{name} is not available in a sealed chat.'**
  String cmdNotInSealed(String name);

  /// No description provided for @cmdNotHere.
  ///
  /// In en, this message translates to:
  /// **'/{name} is not available here.'**
  String cmdNotHere(String name);

  /// No description provided for @cmdNoSuchPerson.
  ///
  /// In en, this message translates to:
  /// **'Nobody here is called @{person}.'**
  String cmdNoSuchPerson(String person);

  /// No description provided for @cmdNoSuchChannel.
  ///
  /// In en, this message translates to:
  /// **'No channel you can see is called “{channel}”.'**
  String cmdNoSuchChannel(String channel);

  /// No description provided for @cmdSeveralChannels.
  ///
  /// In en, this message translates to:
  /// **'Several channels are called “{channel}”: type # and pick the one you mean.'**
  String cmdSeveralChannels(String channel);

  /// No description provided for @cmdJoined.
  ///
  /// In en, this message translates to:
  /// **'You joined {channel}.'**
  String cmdJoined(String channel);

  /// No description provided for @cmdLeft.
  ///
  /// In en, this message translates to:
  /// **'You left {channel}.'**
  String cmdLeft(String channel);

  /// No description provided for @cmdTopicIs.
  ///
  /// In en, this message translates to:
  /// **'The topic: {topic}'**
  String cmdTopicIs(String topic);

  /// No description provided for @cmdNoTopic.
  ///
  /// In en, this message translates to:
  /// **'This channel has no topic.'**
  String get cmdNoTopic;

  /// No description provided for @cmdTopicSet.
  ///
  /// In en, this message translates to:
  /// **'Topic set.'**
  String get cmdTopicSet;

  /// No description provided for @cmdAway.
  ///
  /// In en, this message translates to:
  /// **'You appear away.'**
  String get cmdAway;

  /// No description provided for @cmdBack.
  ///
  /// In en, this message translates to:
  /// **'You no longer appear away.'**
  String get cmdBack;

  /// No description provided for @cmdDndOn.
  ///
  /// In en, this message translates to:
  /// **'Do not disturb is on.'**
  String get cmdDndOn;

  /// No description provided for @cmdDndOff.
  ///
  /// In en, this message translates to:
  /// **'You are available now.'**
  String get cmdDndOff;

  /// No description provided for @cmdQuietOn.
  ///
  /// In en, this message translates to:
  /// **'This conversation is muted for {minutes, plural, =1{1 minute} other{{minutes} minutes}}.'**
  String cmdQuietOn(int minutes);

  /// No description provided for @cmdQuietOff.
  ///
  /// In en, this message translates to:
  /// **'This conversation is no longer muted.'**
  String get cmdQuietOff;

  /// No description provided for @cmdInvited.
  ///
  /// In en, this message translates to:
  /// **'{person} was added.'**
  String cmdInvited(String person);

  /// No description provided for @cmdKicked.
  ///
  /// In en, this message translates to:
  /// **'{person} was removed from this channel.'**
  String cmdKicked(String person);

  /// No description provided for @cmdBanTitle.
  ///
  /// In en, this message translates to:
  /// **'Ban from the server?'**
  String get cmdBanTitle;

  /// No description provided for @cmdBanBody.
  ///
  /// In en, this message translates to:
  /// **'{person} will be removed from the server and kept from coming back. This is not only this channel.'**
  String cmdBanBody(String person);

  /// No description provided for @cmdBanYes.
  ///
  /// In en, this message translates to:
  /// **'Ban'**
  String get cmdBanYes;

  /// No description provided for @cmdBanned.
  ///
  /// In en, this message translates to:
  /// **'{person} is banned.'**
  String cmdBanned(String person);

  /// No description provided for @cmdMuted.
  ///
  /// In en, this message translates to:
  /// **'{person} cannot post here for {minutes, plural, =1{1 minute} other{{minutes} minutes}}.'**
  String cmdMuted(String person, int minutes);

  /// No description provided for @cmdOpped.
  ///
  /// In en, this message translates to:
  /// **'{person} is a manager of this channel.'**
  String cmdOpped(String person);

  /// No description provided for @cmdDeopped.
  ///
  /// In en, this message translates to:
  /// **'{person} is a member of this channel.'**
  String cmdDeopped(String person);

  /// No description provided for @cmdIgnored.
  ///
  /// In en, this message translates to:
  /// **'You blocked {person}. Their messages are collapsed for you.'**
  String cmdIgnored(String person);

  /// No description provided for @cmdNick.
  ///
  /// In en, this message translates to:
  /// **'Your display name is now {name}.'**
  String cmdNick(String name);

  /// No description provided for @chanceRolled.
  ///
  /// In en, this message translates to:
  /// **'Rolled {dice}'**
  String chanceRolled(String dice);

  /// No description provided for @chanceFlipped.
  ///
  /// In en, this message translates to:
  /// **'Flipped a coin'**
  String get chanceFlipped;

  /// No description provided for @chancePicked.
  ///
  /// In en, this message translates to:
  /// **'Picked from {count}'**
  String chancePicked(int count);

  /// No description provided for @chanceShuffled.
  ///
  /// In en, this message translates to:
  /// **'Shuffled {count}'**
  String chanceShuffled(int count);

  /// No description provided for @chanceDrew.
  ///
  /// In en, this message translates to:
  /// **'Drew {count} from the {deck} deck'**
  String chanceDrew(int count, String deck);

  /// No description provided for @chanceDrewTiles.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{Drew 1 mahjong tile} other{Drew {count} mahjong tiles}}'**
  String chanceDrewTiles(int count);

  /// No description provided for @chanceTarot.
  ///
  /// In en, this message translates to:
  /// **'Tarot: {spread}'**
  String chanceTarot(String spread);

  /// No description provided for @chanceByServer.
  ///
  /// In en, this message translates to:
  /// **'drawn by the server'**
  String get chanceByServer;

  /// No description provided for @chanceHeads.
  ///
  /// In en, this message translates to:
  /// **'Heads'**
  String get chanceHeads;

  /// No description provided for @chanceTails.
  ///
  /// In en, this message translates to:
  /// **'Tails'**
  String get chanceTails;

  /// No description provided for @chanceDropped.
  ///
  /// In en, this message translates to:
  /// **'dropped'**
  String get chanceDropped;

  /// No description provided for @chanceUpright.
  ///
  /// In en, this message translates to:
  /// **'upright'**
  String get chanceUpright;

  /// No description provided for @chanceReversed.
  ///
  /// In en, this message translates to:
  /// **'reversed'**
  String get chanceReversed;

  /// No description provided for @chanceReplay.
  ///
  /// In en, this message translates to:
  /// **'Press to spin again'**
  String get chanceReplay;

  /// No description provided for @chanceTotalSpoken.
  ///
  /// In en, this message translates to:
  /// **'total {total}'**
  String chanceTotalSpoken(int total);

  /// No description provided for @cardRankOfSuit.
  ///
  /// In en, this message translates to:
  /// **'{rank} of {suit}'**
  String cardRankOfSuit(String rank, String suit);

  /// No description provided for @cardSuitAndRank.
  ///
  /// In en, this message translates to:
  /// **'{suit} {rank}'**
  String cardSuitAndRank(String suit, String rank);

  /// No description provided for @announcementNoPost.
  ///
  /// In en, this message translates to:
  /// **'This is an announcement channel: only its managers and the people they allow post here. You can read and react.'**
  String get announcementNoPost;

  /// No description provided for @announcementReplyOnly.
  ///
  /// In en, this message translates to:
  /// **'This is an announcement channel: only its managers and the people they allow post here. You can read, react and reply in threads.'**
  String get announcementReplyOnly;

  /// No description provided for @announcementSetting.
  ///
  /// In en, this message translates to:
  /// **'Announcement channel'**
  String get announcementSetting;

  /// No description provided for @announcementSettingHint.
  ///
  /// In en, this message translates to:
  /// **'Only managers and the people they allow post. Everyone else reads and reacts. Who is in the channel is shown to managers only, and nobody is shown typing or online.'**
  String get announcementSettingHint;

  /// No description provided for @announcementRepliesSetting.
  ///
  /// In en, this message translates to:
  /// **'Replies in threads'**
  String get announcementRepliesSetting;

  /// No description provided for @announcementRepliesHint.
  ///
  /// In en, this message translates to:
  /// **'Let members who may not post reply in threads.'**
  String get announcementRepliesHint;

  /// No description provided for @lazyDeliverySetting.
  ///
  /// In en, this message translates to:
  /// **'Delivery when not open'**
  String get lazyDeliverySetting;

  /// No description provided for @lazyDeliveryHint.
  ///
  /// In en, this message translates to:
  /// **'A very large channel is sent in full only to people who have it open; the others see that it changed and get the rest when they open it. The server decides by size unless you say.'**
  String get lazyDeliveryHint;

  /// No description provided for @lazyDeliveryServer.
  ///
  /// In en, this message translates to:
  /// **'Follow the server'**
  String get lazyDeliveryServer;

  /// No description provided for @lazyDeliveryOn.
  ///
  /// In en, this message translates to:
  /// **'Only to people who have it open'**
  String get lazyDeliveryOn;

  /// No description provided for @lazyDeliveryOff.
  ///
  /// In en, this message translates to:
  /// **'To everyone, always'**
  String get lazyDeliveryOff;

  /// No description provided for @mayPostHere.
  ///
  /// In en, this message translates to:
  /// **'Let them post here'**
  String get mayPostHere;

  /// No description provided for @mayNotPostHere.
  ///
  /// In en, this message translates to:
  /// **'Stop them posting here'**
  String get mayNotPostHere;

  /// No description provided for @mayPostTag.
  ///
  /// In en, this message translates to:
  /// **'posts here'**
  String get mayPostTag;

  /// No description provided for @membersHidden.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 member} other{{count} members}}. An announcement channel does not show who is in it.'**
  String membersHidden(int count);

  /// No description provided for @onlineInChannel.
  ///
  /// In en, this message translates to:
  /// **'{count} online'**
  String onlineInChannel(int count);

  /// No description provided for @lastSeenRecently.
  ///
  /// In en, this message translates to:
  /// **'Last seen recently'**
  String get lastSeenRecently;

  /// No description provided for @lastSeenWithinWeek.
  ///
  /// In en, this message translates to:
  /// **'Last seen within a week'**
  String get lastSeenWithinWeek;

  /// No description provided for @lastSeenLongAgo.
  ///
  /// In en, this message translates to:
  /// **'Last seen a while ago'**
  String get lastSeenLongAgo;

  /// No description provided for @hideLastSeen.
  ///
  /// In en, this message translates to:
  /// **'Hide when I was last here'**
  String get hideLastSeen;

  /// No description provided for @hideLastSeenHint.
  ///
  /// In en, this message translates to:
  /// **'Your profile otherwise says roughly when you were last here: recently, within a week, or a while ago. Never the time.'**
  String get hideLastSeenHint;

  /// No description provided for @instantConverterTitle.
  ///
  /// In en, this message translates to:
  /// **'Time converter'**
  String get instantConverterTitle;

  /// No description provided for @instantYourTime.
  ///
  /// In en, this message translates to:
  /// **'Your time'**
  String get instantYourTime;

  /// No description provided for @instantAsWritten.
  ///
  /// In en, this message translates to:
  /// **'As written'**
  String get instantAsWritten;

  /// No description provided for @instantUtc.
  ///
  /// In en, this message translates to:
  /// **'UTC'**
  String get instantUtc;

  /// No description provided for @instantTypedAs.
  ///
  /// In en, this message translates to:
  /// **'Typed as “{typed}”'**
  String instantTypedAs(String typed);

  /// No description provided for @instantAddZone.
  ///
  /// In en, this message translates to:
  /// **'Add a time zone'**
  String get instantAddZone;

  /// No description provided for @instantSearchZones.
  ///
  /// In en, this message translates to:
  /// **'Search time zones and cities'**
  String get instantSearchZones;

  /// No description provided for @instantNoZones.
  ///
  /// In en, this message translates to:
  /// **'No time zone matches.'**
  String get instantNoZones;

  /// No description provided for @instantRemoveZone.
  ///
  /// In en, this message translates to:
  /// **'Remove {zone}'**
  String instantRemoveZone(String zone);

  /// No description provided for @instantCopyIso.
  ///
  /// In en, this message translates to:
  /// **'Copy as ISO 8601'**
  String get instantCopyIso;

  /// No description provided for @instantCopied.
  ///
  /// In en, this message translates to:
  /// **'Time copied.'**
  String get instantCopied;

  /// No description provided for @instantRemindMe.
  ///
  /// In en, this message translates to:
  /// **'Remind me'**
  String get instantRemindMe;

  /// No description provided for @instantChipLabel.
  ///
  /// In en, this message translates to:
  /// **'{when}, {zone}. Written as “{typed}”. Opens the time converter.'**
  String instantChipLabel(String when, String zone, String typed);

  /// No description provided for @instantToday.
  ///
  /// In en, this message translates to:
  /// **'Today'**
  String get instantToday;

  /// No description provided for @instantTomorrow.
  ///
  /// In en, this message translates to:
  /// **'Tomorrow'**
  String get instantTomorrow;

  /// No description provided for @instantYesterday.
  ///
  /// In en, this message translates to:
  /// **'Yesterday'**
  String get instantYesterday;

  /// No description provided for @instantIn.
  ///
  /// In en, this message translates to:
  /// **'in {amount}'**
  String instantIn(String amount);

  /// No description provided for @instantAgo.
  ///
  /// In en, this message translates to:
  /// **'{amount} ago'**
  String instantAgo(String amount);

  /// No description provided for @instantSeconds.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 second} other{{count} seconds}}'**
  String instantSeconds(int count);

  /// No description provided for @instantMinutes.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 minute} other{{count} minutes}}'**
  String instantMinutes(int count);

  /// No description provided for @instantHours.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 hour} other{{count} hours}}'**
  String instantHours(int count);

  /// No description provided for @instantDays.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 day} other{{count} days}}'**
  String instantDays(int count);

  /// No description provided for @instantMonths.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 month} other{{count} months}}'**
  String instantMonths(int count);

  /// No description provided for @instantYears.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 year} other{{count} years}}'**
  String instantYears(int count);

  /// No description provided for @instantComposerSays.
  ///
  /// In en, this message translates to:
  /// **'{typed} will show as {when} your time'**
  String instantComposerSays(String typed, String when);

  /// No description provided for @instantComposerPhrase.
  ///
  /// In en, this message translates to:
  /// **'“{typed}” will show as {when} your time'**
  String instantComposerPhrase(String typed, String when);

  /// No description provided for @instantKeepAsText.
  ///
  /// In en, this message translates to:
  /// **'Keep as text'**
  String get instantKeepAsText;

  /// No description provided for @instantKeepAsTextLabel.
  ///
  /// In en, this message translates to:
  /// **'Send “{typed}” as plain text'**
  String instantKeepAsTextLabel(String typed);

  /// No description provided for @instantDropTime.
  ///
  /// In en, this message translates to:
  /// **'Not a time'**
  String get instantDropTime;

  /// No description provided for @instantDropTimeLabel.
  ///
  /// In en, this message translates to:
  /// **'Send “{typed}” as plain words'**
  String instantDropTimeLabel(String typed);

  /// No description provided for @instantMakeTime.
  ///
  /// In en, this message translates to:
  /// **'MAKE THIS A TIME'**
  String get instantMakeTime;

  /// No description provided for @instantMakeTimeLine.
  ///
  /// In en, this message translates to:
  /// **'Make this a time: {when} (your time)'**
  String instantMakeTimeLine(String when);

  /// No description provided for @instantsHere.
  ///
  /// In en, this message translates to:
  /// **'Times in this message'**
  String get instantsHere;
}

class _L10nDelegate extends LocalizationsDelegate<L10n> {
  const _L10nDelegate();

  @override
  Future<L10n> load(Locale locale) {
    return SynchronousFuture<L10n>(lookupL10n(locale));
  }

  @override
  bool isSupported(Locale locale) =>
      <String>['en'].contains(locale.languageCode);

  @override
  bool shouldReload(_L10nDelegate old) => false;
}

L10n lookupL10n(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'en':
      return L10nEn();
  }

  throw FlutterError(
    'L10n.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}
