// Signatures, hashes and every label of docs/SEALED.md (section 2.4).

import { concat, utf8 } from './bytes.js';
import { sha256, verifyRaw } from './prim.js';

// Signature labels.
export const labelDeviceProof = 'churr device proof v1';
export const labelDeviceList = 'churr device list v1';
export const labelDeviceListRollover = 'churr device list rollover v1';
export const labelEpoch = 'churr epoch v1';
export const labelMessage = 'churr sealed message v1';
export const labelLockedCopy = 'churr locked copy v1';
export const labelDeviceAttach = 'churr device attach v1';

// Hash labels.
export const labelDeviceListHash = 'churr device list hash v1';
export const labelEpochHash = 'churr epoch hash v1';
export const labelDeviceApproval = 'churr device approval v1';
export const labelCompare = 'churr compare v1';
export const labelSealedUnlock = 'churr sealed unlock v1';

// Seal labels (section 2.5).
export const labelSealEpoch = 'churr seal epoch v1';
export const labelSealBackupKey = 'churr seal backup key v1';
export const labelSealHandover = 'churr seal handover v1';
export const labelSealPairing = 'churr seal pairing v1';

// Lock labels (section 2.6).
export const labelLockMessage = 'churr lock message v1';
export const labelLockAccountRecord = 'churr lock account record v1';
export const labelLockBackupRecord = 'churr lock backup record v1';
export const labelLockPairing = 'churr lock pairing v1';

// Derivation labels (sections 5.2 and 13.3).
export const labelHistory = 'churr history v1';
export const labelContent = 'churr content v1';
export const labelEpochCheck = 'churr epoch check v1';
export const labelSender = 'churr sender v1';
export const labelCall = 'churr call v1';
export const labelCallSender = 'churr call sender v1';
export const labelUnlockPasskey = 'churr unlock passkey v1';
export const labelUnlockPhrase = 'churr unlock phrase v1';
export const labelUnlockAccessKey = 'churr unlock access key v1';

// The associated data of every file chunk (section 9).
export const labelFile = 'churr file v1';

export const signatureLabels = [labelDeviceProof, labelDeviceList, labelDeviceListRollover, labelEpoch, labelMessage, labelLockedCopy, labelDeviceAttach];

// label || 0x00 || the rest, joined.
export function labelled(label, ...rest) {
  return concat(utf8(label), new Uint8Array(1), ...rest);
}

// Ed25519 over label || 0x00 || record.
export function sign(key, label, record) {
  return key.signRaw(labelled(label, record));
}

// A signature is only ever verified, never compared with another.
export function verify(pub, label, record, sig) {
  return verifyRaw(pub, labelled(label, record), sig);
}

// SHA-256(label || 0x00 || record).
export function hashOf(label, record) {
  return sha256(labelled(label, record));
}
