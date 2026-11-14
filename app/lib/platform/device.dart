import 'package:file_selector/file_selector.dart';
import 'package:flutter_timezone/flutter_timezone.dart';
import 'package:timezone/data/latest.dart' as tzdata;
import 'package:timezone/timezone.dart' as tz;

/// What the account screens ask of the device: its time zone, another
/// zone's time of day, and a picture from its files.

/// The device's IANA time zone name, such as `Europe/London`, or null if
/// the platform will not say.
Future<String?> deviceTimeZone() async {
  try {
    return (await FlutterTimezone.getLocalTimezone()).identifier;
  } on Object {
    return null;
  }
}

bool _zonesLoaded = false;

/// The wall-clock time now in [zone], or null for a zone the database
/// does not know. Used for "local time" on someone's profile.
DateTime? timeIn(String zone, {DateTime? now}) {
  if (!_zonesLoaded) {
    tzdata.initializeTimeZones();
    _zonesLoaded = true;
  }
  try {
    final loc = tz.getLocation(zone);
    return tz.TZDateTime.from(now ?? DateTime.now(), loc);
  } on Object {
    return null;
  }
}

/// A picture chosen by the person, or null if they backed out.
Future<(String name, List<int> bytes)?> pickImage() async {
  const images = XTypeGroup(
    label: 'Images',
    extensions: ['png', 'jpg', 'jpeg', 'gif', 'webp'],
    uniformTypeIdentifiers: ['public.image'],
    mimeTypes: ['image/*'],
  );
  final f = await openFile(acceptedTypeGroups: [images]);
  if (f == null) return null;
  return (f.name, await f.readAsBytes());
}
