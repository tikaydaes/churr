// Signatures, hashes and every label of docs/SEALED.md (section 2.4).

import 'dart:convert';
import 'dart:typed_data';

import 'prim.dart';

// Signature labels.
const labelDeviceProof = 'churr device proof v1';
const labelDeviceList = 'churr device list v1';
const labelDeviceListRollover = 'churr device list rollover v1';
const labelEpoch = 'churr epoch v1';
const labelMessage = 'churr sealed message v1';
const labelLockedCopy = 'churr locked copy v1';
const labelDeviceAttach = 'churr device attach v1';

// Hash labels.
const labelDeviceListHash = 'churr device list hash v1';
const labelEpochHash = 'churr epoch hash v1';
const labelDeviceApproval = 'churr device approval v1';
const labelCompare = 'churr compare v1';
const labelSealedUnlock = 'churr sealed unlock v1';

// Seal labels (section 2.5).
const labelSealEpoch = 'churr seal epoch v1';
const labelSealBackupKey = 'churr seal backup key v1';
const labelSealHandover = 'churr seal handover v1';
const labelSealPairing = 'churr seal pairing v1';

// Lock labels (section 2.6).
const labelLockMessage = 'churr lock message v1';
const labelLockAccountRecord = 'churr lock account record v1';
const labelLockBackupRecord = 'churr lock backup record v1';
const labelLockPairing = 'churr lock pairing v1';

// Derivation labels (sections 5.2 and 13.3).
const labelHistory = 'churr history v1';
const labelContent = 'churr content v1';
const labelEpochCheck = 'churr epoch check v1';
const labelSender = 'churr sender v1';
const labelCall = 'churr call v1';
const labelCallSender = 'churr call sender v1';
const labelUnlockPasskey = 'churr unlock passkey v1';
const labelUnlockPhrase = 'churr unlock phrase v1';
const labelUnlockAccessKey = 'churr unlock access key v1';

/// The associated data of every file chunk (section 9).
const labelFile = 'churr file v1';

/// Every label a signature is made under.
const signatureLabels = [
  labelDeviceProof,
  labelDeviceList,
  labelDeviceListRollover,
  labelEpoch,
  labelMessage,
  labelLockedCopy,
  labelDeviceAttach,
];

/// label || 0x00 || the rest, joined.
Uint8List labelled(String label, [List<List<int>> rest = const []]) => cat([
  ascii.encode(label),
  const [0],
  ...rest,
]);

/// Ed25519 over label || 0x00 || record.
Future<Uint8List> sign(SigningKey key, String label, List<int> record) =>
    key.signRaw(labelled(label, [record]));

/// Checks a signature made by [sign] under the same label. A signature is
/// only ever verified, never compared with another.
Future<bool> verify(
  List<int> public,
  String label,
  List<int> record,
  List<int> sig,
) => verifyRaw(public, labelled(label, [record]), sig);

/// SHA-256(label || 0x00 || record).
Uint8List hashOf(String label, List<int> record) =>
    sha256(labelled(label, [record]));
