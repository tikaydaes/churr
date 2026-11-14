// What a device does when `call.ring_started` arrives (docs/CALLS.md
// section 15). The server decides who is rung: members at level `member`
// whose mode for the conversation is Any activity, who have not blocked
// the caller and are not in do not disturb. The device checks again with
// what it knows itself, which can be newer or wider (a server muted from
// the app on this device counts as do not disturb).

enum RingAction {
  /// Nothing: already in this call, or in do not disturb.
  none,

  /// The incoming-call prompt without the ring sound: already in another
  /// call ("a second ring shows as a banner without the ring sound").
  banner,

  /// The prompt and the ring.
  ring,
}

RingAction ringAction({
  required bool inThisCall,
  required bool inAnotherCall,
  required bool doNotDisturb,
  required bool serverMuted,
}) {
  if (inThisCall) return RingAction.none;
  // Nothing pushes a ring through do not disturb; notify anyway is for
  // messages only.
  if (doNotDisturb || serverMuted) return RingAction.none;
  if (inAnotherCall) return RingAction.banner;
  return RingAction.ring;
}

/// Whether starting a call in a conversation of this kind can ring
/// anyone, by the default modes (docs/CALLS.md section 15): sealed chats
/// and ad-hoc channels ring every other member unless they changed their
/// mode; named channels only those who chose Any activity. The call
/// screen uses it to list who is being rung while the caller waits.
bool kindRings({required bool sealed, required bool named}) => sealed || !named;
