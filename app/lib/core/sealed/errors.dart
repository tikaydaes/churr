// Every refusal the library makes. Each is a [SealedError]; the subclasses
// say which check failed so callers can show the right thing.

class SealedError implements Exception {
  final String message;
  const SealedError(this.message);
  @override
  String toString() => 'sealed: $message';
}

/// An X25519 result of 32 zero bytes, which every construction refuses.
class ZeroSharedError extends SealedError {
  const ZeroSharedError() : super('the X25519 result is all zero');
}

/// Something encrypted that does not open. The causes are not told apart.
class OpenError extends SealedError {
  const OpenError() : super('the value does not open');
}

/// A record the reader refuses (section 2.3).
class RecordError extends SealedError {
  const RecordError(super.message);
  static const short = RecordError('the record is cut short');
  static const trailing = RecordError('the record has bytes left over');
  static const choice = RecordError('a choice is out of range');
  static const utf8 = RecordError('a text is not valid UTF-8');
  static const order = RecordError('a list is not in order, or repeats a key');
  static const tooLong = RecordError('the record is longer than 64 KB');
  static const count = RecordError('a list has the wrong number of items');
}

/// A device list that does not check out (section 4.4). [rule] is the rule
/// that failed: 1 the chain, 2 proofs and repeats, 3 signatures and the
/// kind of step; 0 a list that does not parse.
class ListError extends SealedError {
  final int rule;
  const ListError(this.rule, String why) : super('device list refused: $why');
}

/// An epoch record refused under rule [rule] (1 to 7) of section 5.4.
class EpochError extends SealedError {
  final int rule;
  const EpochError(this.rule, String why)
    : super('epoch record refused (rule $rule): $why');
}

/// A sealed message that could not be checked (section 8.2).
class MessageError extends SealedError {
  const MessageError(String why)
    : super('the message could not be checked: $why');
}

/// An encrypted file that does not check out (section 9).
class FileError extends SealedError {
  const FileError() : super('the file does not check out');
}

/// A typed recovery phrase that is not one.
class PhraseError extends SealedError {
  const PhraseError() : super('that is not a recovery phrase');
}

/// A way of unlocking that was refused.
class UnlockError extends SealedError {
  const UnlockError(String why)
    : super('the backup key could not be unlocked: $why');
}

/// A backup record that does not check out.
class BackupRecordError extends SealedError {
  const BackupRecordError() : super('the backup record does not check out');
}

/// Something that needs the backup key on a locked device.
class LockedError extends SealedError {
  const LockedError() : super('the device is locked');
}

/// A second, different record for a key epoch the device already has.
class SecondRecordError extends SealedError {
  const SecondRecordError()
    : super('the server gave two records for one key epoch');
}

/// A record that does not follow the newest one the device holds.
class ChainGapError extends SealedError {
  const ChainGapError() : super('an epoch record is missing before this one');
}

/// Sending is held until a reset notice is acknowledged.
class HeldError extends SealedError {
  const HeldError()
    : super('sending is held until the reset notice is acknowledged');
}

/// A new epoch is needed before sending.
class NeedsEpochError extends SealedError {
  const NeedsEpochError() : super('a new key epoch is needed before sending');
}

/// A message in an epoch the device has no history secret for yet. Not a
/// failed check: it is read once the secret arrives.
class NoKeyError extends SealedError {
  const NoKeyError() : super('the device has no key for that epoch yet');
}

/// The code typed or scanned is not the code of the request.
class CodeError extends SealedError {
  const CodeError() : super('the code does not match the request');
}
