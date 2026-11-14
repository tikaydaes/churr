// Every refusal the library makes. Each is a SealedError; the subclasses
// say which check failed so the page can show the right thing. The
// messages are the Go reference's, so the transcript's `why` can be
// compared.

export class SealedError extends Error {
  constructor(message) { super(message); this.name = 'SealedError'; }
}

// An X25519 result of 32 zero bytes, which every construction refuses.
export class ZeroSharedError extends SealedError {
  constructor() { super('the X25519 result is all zero'); }
}

// Something encrypted that does not open. The causes are not told apart.
export class OpenError extends SealedError {
  constructor() { super('the value does not open'); }
}

// A record the reader refuses (section 2.3).
export class RecordError extends SealedError {}
RecordError.short = () => new RecordError('the record is cut short');
RecordError.trailing = () => new RecordError('the record has bytes left over');
RecordError.choice = () => new RecordError('a choice is out of range');
RecordError.utf8 = () => new RecordError('a text is not valid UTF-8');
RecordError.order = () => new RecordError('a list is not in order, or repeats a key');
RecordError.tooLong = () => new RecordError('the record is longer than 64 KB');
RecordError.count = () => new RecordError('a list has the wrong number of items');

// A device list that does not check out (section 4.4). rule: 1 the chain,
// 2 proofs and repeats, 3 signatures and the kind of step; 0 a list that
// does not parse.
export class ListError extends SealedError {
  constructor(rule, why) { super('device list refused: ' + why); this.rule = rule; }
}

// An epoch record refused under rule 1 to 7 of section 5.4.
export class EpochError extends SealedError {
  constructor(rule, why) { super(`epoch record refused (rule ${rule}): ${why}`); this.rule = rule; }
}

// A sealed message that could not be checked (section 8.2).
export class MessageError extends SealedError {
  constructor(why) { super('the message could not be checked: ' + why); }
}

export class FileError extends SealedError {
  constructor() { super('the file does not check out'); }
}

export class PhraseError extends SealedError {
  constructor() { super('that is not a recovery phrase'); }
}

export class UnlockError extends SealedError {
  constructor(why) { super('the backup key could not be unlocked: ' + why); }
}

export class BackupRecordError extends SealedError {
  constructor() { super('the backup record does not check out'); }
}

export class LockedError extends SealedError {
  constructor() { super('the device is locked'); }
}

export class SecondRecordError extends SealedError {
  constructor() { super('the server gave two records for one key epoch'); }
}

export class ChainGapError extends SealedError {
  constructor() { super('an epoch record is missing before this one'); }
}

export class HeldError extends SealedError {
  constructor() { super('sending is held until the reset notice is acknowledged'); }
}

export class NeedsEpochError extends SealedError {
  constructor() { super('a new key epoch is needed before sending'); }
}

// A message in an epoch the device has no history secret for yet. Not a
// failed check: it is read once the secret arrives.
export class NoKeyError extends SealedError {
  constructor() { super('the device has no key for that epoch yet'); }
}

export class CodeError extends SealedError {
  constructor() { super('the code does not match the request'); }
}
